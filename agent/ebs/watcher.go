// Copyright Amazon.com Inc. or its affiliates. All Rights Reserved.
//
// Licensed under the Apache License, Version 2.0 (the "License"). You may
// not use this file except in compliance with the License. A copy of the
// License is located at
//
//	http://aws.amazon.com/apache2.0/
//
// or in the "license" file accompanying this file. This file is distributed
// on an "AS IS" BASIS, WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either
// express or implied. See the License for the specific language governing
// permissions and limitations under the License.

package ebs

import (
	"context"
	"time"

	"github.com/aws/amazon-ecs-agent/agent/engine/dockerstate"
	"github.com/aws/amazon-ecs-agent/agent/statechange"
	apiebs "github.com/aws/amazon-ecs-agent/ecs-agent/api/resource"
	log "github.com/cihub/seelog"
	"github.com/pkg/errors"
)

const (
	// // sendEBSStateChangeRetryTimeout specifies the timeout before giving up
	// // when looking for EBS in agent's state. If for whatever reason, the message
	// // from ACS is received after the EBS has been attached to the instance, this
	// // timeout duration will be used to wait for EBS message to be sent from ACS
	// sendEBSStateChangeRetryTimeout = 6 * time.Second

	// // sendEBSStateChangeBackoffMin specifies minimum value for backoff when
	// // waiting for attachment message from ACS
	// sendEBSStateChangeBackoffMin = 100 * time.Millisecond

	// // sendEBSStateChangeBackoffMax specifies maximum value for backoff when
	// // waiting for attachment message from ACS
	// sendEBSStateChangeBackoffMax = 250 * time.Millisecond

	// // sendEBSStateChangeBackoffJitter specifies the jitter multiple percentage
	// // when waiting for attachment message from ACS
	// sendEBSStateChangeBackoffJitter = 0.2

	// // sendEBSStateChangeBackoffMultiple specifies the backoff duration multipler
	// // when waiting for the attachment message from ACS
	// sendEBSStateChangeBackoffMultiple = 1.5

	// // volumeIDRetryTimeout specifies the timeout before giving up when
	// // looking for an EBS's volume ID on the host.
	// // We are capping off this duration to 1s assuming worst-case behavior
	// volumeIDRetryTimeout = 2 * time.Second

	// // ebsStatusSentMsg is the error message to use when trying to send an ebs status that's
	// // already been sent
	// ebsStatusSentMsg = "ebs status already sent"

	scanPeriod = 500 * time.Millisecond
)

type EBSWatcher struct {
	ctx        context.Context
	cancel     context.CancelFunc
	scanTicker *time.Ticker
	agentState dockerstate.TaskEngineState
	// dataClient     data.Client
	ebsChangeEvent chan<- statechange.Event
	mailbox        chan func()
}

func NewWatcher(ctx context.Context,
	state dockerstate.TaskEngineState,
	stateChangeEvents chan<- statechange.Event) (*EBSWatcher, error) {
	derivedContext, cancel := context.WithCancel(ctx)
	log.Info("eni watcher has been initialized")
	return &EBSWatcher{
		ctx:            derivedContext,
		cancel:         cancel,
		agentState:     state,
		ebsChangeEvent: stateChangeEvents,
	}, nil
}

func (w *EBSWatcher) Start() {
	log.Info("Starting EBS watcher")

	w.scanTicker = time.NewTicker(scanPeriod)

	if len(w.agentState.AllPendingEBSAttachments()) == 0 {
		w.scanTicker.Stop()
	}

	for {
		select {
		case f := <-w.mailbox:
			f()
		case <-w.scanTicker.C:
			w.scanEBSVolumes()
		case <-w.ctx.Done():
			w.scanTicker.Stop()
			log.Info("EBS watcher stopped")
		}
	}
}

func (w *EBSWatcher) Stop() {
	log.Info("Stopping EBS watcher")
	w.cancel()
}

func (w *EBSWatcher) HandleResourceAttachment(ebs *apiebs.ResourceAttachment) {
	w.mailbox <- func() {
		empty := len(w.agentState.AllPendingEBSAttachments()) == 0

		err := w.handleEBSAttachment(ebs)
		if err != nil {
			log.Info("Failed to handle resource attachment")
		}

		if empty && len(w.agentState.AllPendingEBSAttachments()) == 1 {
			w.scanTicker.Stop()
			w.scanTicker = time.NewTicker(scanPeriod)
		}
	}
}

func (w *EBSWatcher) handleEBSAttachment(ebs *apiebs.ResourceAttachment) error {
	if ebs.AttachmentProperties[apiebs.ResourceTypeName] != apiebs.ElasticBlockStorage {
		log.Info("Resource type not Elastic Block Storage. Skip handling resource attachment.")
		return nil
	}
	volumeID := ebs.AttachmentProperties[apiebs.VolumeIdName]
	_, ok := w.agentState.EBSByVolumeId(volumeID)

	if ok {
		log.Info("EBS Volume attachment already exists. Skip handling EBS attachment.")
		return nil
	}

	if ebs.IsSent() {
		log.Info("Resource already attached. Skip handling EBS attachment.")
		return nil
	}

	duration := time.Until(ebs.ExpiresAt)
	if duration <= 0 {
		log.Info("Attachment expiration time has past. Skip handling EBS attachment")
		return nil
	}

	ebs.Initialize(func() {
		log.Info("EBS Volume timed out: %v", volumeID)
		w.RemoveAttachment(volumeID)
	})

	w.agentState.AddEBSAttachment(ebs)

	return nil
}

func (w *EBSWatcher) notifyFoundEBS(volumeId string) {
	w.mailbox <- func() {
		ebs, ok := w.agentState.EBSByVolumeId(volumeId)
		if !ok {
			return
		}
		log.Info("Found EBS volume with volumd ID: %v and device name: %v", volumeId, ebs.AttachmentProperties[apiebs.DeviceName])
		ebs.StopAckTimer()
		w.agentState.RemoveEBSAttachment(volumeId)
	}
}

func (w *EBSWatcher) RemoveAttachment(volumeID string) {
	w.mailbox <- func() {
		w.agentState.RemoveEBSAttachment(volumeID)
	}
}

func (w *EBSWatcher) scanEBSVolumes() {
	for _, ebs := range w.agentState.AllPendingEBSAttachments() {
		volumeId := ebs.AttachmentProperties[apiebs.VolumeIdName]
		deviceName := ebs.AttachmentProperties[apiebs.DeviceName]
		err := apiebs.ConfirmEBSVolumeIsAttached(w.ctx, deviceName, volumeId)
		if err != nil {
			log.Infof("Unable to find EBS volume with volume ID: %v and device name: %v", volumeId, deviceName)
			if err == apiebs.ErrInvalidVolumeID || errors.Cause(err) == apiebs.ErrInvalidVolumeID {
				log.Info("Found a different EBS volume attached to the host")
				w.agentState.RemoveEBSAttachment(volumeId)
			}
			continue
		}
		w.notifyFoundEBS(volumeId)
	}
}
