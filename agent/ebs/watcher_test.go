//go:build unit
// +build unit

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
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/aws/amazon-ecs-agent/agent/engine/dockerstate"
	"github.com/aws/amazon-ecs-agent/agent/statechange"
	"github.com/aws/amazon-ecs-agent/ecs-agent/acs/session/testconst"
	"github.com/aws/amazon-ecs-agent/ecs-agent/api/attachmentinfo"
	apiebs "github.com/aws/amazon-ecs-agent/ecs-agent/api/resource"
	mock_ebs_discovery "github.com/aws/amazon-ecs-agent/ecs-agent/api/resource/mocks"
	"github.com/aws/amazon-ecs-agent/ecs-agent/api/status"

	"github.com/golang/mock/gomock"
	"github.com/stretchr/testify/assert"
)

const (
	resourceAttachmentARN = "arn:aws:ecs:us-west-2:123456789012:attachment/a1b2c3d4-5678-90ab-cdef-11111EXAMPLE"
	containerInstanceARN  = "arn:aws:ecs:us-west-2:123456789012:container-instance/a1b2c3d4-5678-90ab-cdef-11111EXAMPLE"
	taskARN               = "task1"
	taskClusterARN        = "arn:aws:ecs:us-west-2:123456789012:cluster/customer-task-cluster"
	deviceName            = "/dev/xvdba"
	volumeID              = "vol-1234"
)

func newTestEBSWatcher(ctx context.Context, agentState dockerstate.TaskEngineState,
	ebsChangeEvent chan<- statechange.Event, discoveryClient apiebs.EBSDiscovery) *EBSWatcher {
	derivedContext, cancel := context.WithCancel(ctx)
	return &EBSWatcher{
		ctx:             derivedContext,
		cancel:          cancel,
		agentState:      agentState,
		ebsChangeEvent:  ebsChangeEvent,
		discoveryClient: discoveryClient,
	}
}

func TestHandleEBSAttachmentHappyCase(t *testing.T) {
	mockCtrl := gomock.NewController(t)
	defer mockCtrl.Finish()

	ctx := context.Background()
	taskEngineState := dockerstate.NewTaskEngineState()
	eventChannel := make(chan statechange.Event)
	mockDiscoveryClient := mock_ebs_discovery.NewMockEBSDiscovery(mockCtrl)

	watcher := newTestEBSWatcher(ctx, taskEngineState, eventChannel, mockDiscoveryClient)

	testAttachmentProperties := map[string]string{
		apiebs.ResourceTypeName: apiebs.ElasticBlockStorage,
		apiebs.DeviceName:       deviceName,
		apiebs.VolumeIdName:     volumeID,
	}

	expiresAt := time.Now().Add(time.Millisecond * testconst.WaitTimeoutMillis)
	ebsAttachment := &apiebs.ResourceAttachment{
		AttachmentInfo: attachmentinfo.AttachmentInfo{
			TaskARN:              taskARN,
			TaskClusterARN:       taskClusterARN,
			ContainerInstanceARN: containerInstanceARN,
			ExpiresAt:            expiresAt,
			Status:               status.AttachmentNone,
			AttachmentARN:        resourceAttachmentARN,
		},
		AttachmentProperties: testAttachmentProperties,
	}

	var wg sync.WaitGroup
	wg.Add(1)
	mockDiscoveryClient.EXPECT().ConfirmEBSVolumeIsAttached(deviceName, volumeID).
		Do(func(deviceName, volumeID string) {
			wg.Done()
		}).
		Return(nil).
		MinTimes(1)

	err := watcher.HandleResourceAttachment(ebsAttachment)
	assert.NoError(t, err)

	// We're mocking a scan tick of the EBS watcher here instead of actually starting up the EBS watcher.
	wg.Add(1)
	go func() {
		defer wg.Done()
		pendingEBS := watcher.agentState.GetAllPendingEBSAttachmentWithKey()
		if len(pendingEBS) > 0 {
			foundVolumes := apiebs.ScanEBSVolumes(pendingEBS, watcher.discoveryClient)
			watcher.NotifyFound(foundVolumes)
		}
	}()
	wg.Wait()

	assert.Len(t, taskEngineState.(*dockerstate.DockerTaskEngineState).GetAllEBSAttachments(), 1)
	ebsAttachment, ok := taskEngineState.(*dockerstate.DockerTaskEngineState).GetEBSByVolumeId(volumeID)
	assert.True(t, ok)
	assert.True(t, ebsAttachment.IsAttached())
}

func TestHandleExpiredEBSAttachment(t *testing.T) {
	mockCtrl := gomock.NewController(t)
	defer mockCtrl.Finish()

	ctx := context.Background()
	taskEngineState := dockerstate.NewTaskEngineState()
	eventChannel := make(chan statechange.Event)
	mockDiscoveryClient := mock_ebs_discovery.NewMockEBSDiscovery(mockCtrl)

	testAttachmentProperties := map[string]string{
		apiebs.ResourceTypeName: apiebs.ElasticBlockStorage,
		apiebs.DeviceName:       deviceName,
		apiebs.VolumeIdName:     volumeID,
	}

	expiresAt := time.Now().Add(-1 * time.Millisecond)
	ebsAttachment := &apiebs.ResourceAttachment{
		AttachmentInfo: attachmentinfo.AttachmentInfo{
			TaskARN:              taskARN,
			TaskClusterARN:       taskClusterARN,
			ContainerInstanceARN: containerInstanceARN,
			ExpiresAt:            expiresAt,
			Status:               status.AttachmentNone,
			AttachmentARN:        resourceAttachmentARN,
		},
		AttachmentProperties: testAttachmentProperties,
	}
	watcher := newTestEBSWatcher(ctx, taskEngineState, eventChannel, mockDiscoveryClient)

	err := watcher.HandleResourceAttachment(ebsAttachment)
	assert.Error(t, err)
	assert.Len(t, taskEngineState.(*dockerstate.DockerTaskEngineState).GetAllEBSAttachments(), 0)
	_, ok := taskEngineState.(*dockerstate.DockerTaskEngineState).GetEBSByVolumeId(volumeID)
	assert.False(t, ok)
}

func TestHandleDuplicateEBSAttachment(t *testing.T) {
	mockCtrl := gomock.NewController(t)
	defer mockCtrl.Finish()

	ctx := context.Background()
	taskEngineState := dockerstate.NewTaskEngineState()
	eventChannel := make(chan statechange.Event)
	mockDiscoveryClient := mock_ebs_discovery.NewMockEBSDiscovery(mockCtrl)

	testAttachmentProperties1 := map[string]string{
		apiebs.ResourceTypeName: apiebs.ElasticBlockStorage,
		apiebs.DeviceName:       deviceName,
		apiebs.VolumeIdName:     volumeID,
	}

	expiresAt := time.Now().Add(time.Millisecond * testconst.WaitTimeoutMillis)
	ebsAttachment1 := &apiebs.ResourceAttachment{
		AttachmentInfo: attachmentinfo.AttachmentInfo{
			TaskARN:              taskARN,
			TaskClusterARN:       taskClusterARN,
			ContainerInstanceARN: containerInstanceARN,
			ExpiresAt:            expiresAt,
			Status:               status.AttachmentNone,
			AttachmentARN:        resourceAttachmentARN,
		},
		AttachmentProperties: testAttachmentProperties1,
	}

	testAttachmentProperties2 := map[string]string{
		apiebs.ResourceTypeName: apiebs.ElasticBlockStorage,
		apiebs.DeviceName:       deviceName,
		apiebs.VolumeIdName:     volumeID,
	}

	ebsAttachment2 := &apiebs.ResourceAttachment{
		AttachmentInfo: attachmentinfo.AttachmentInfo{
			TaskARN:              taskARN,
			TaskClusterARN:       taskClusterARN,
			ContainerInstanceARN: containerInstanceARN,
			ExpiresAt:            expiresAt,
			Status:               status.AttachmentNone,
			AttachmentARN:        resourceAttachmentARN,
		},
		AttachmentProperties: testAttachmentProperties2,
	}

	watcher := newTestEBSWatcher(ctx, taskEngineState, eventChannel, mockDiscoveryClient)

	var wg sync.WaitGroup
	wg.Add(1)
	mockDiscoveryClient.EXPECT().ConfirmEBSVolumeIsAttached(deviceName, volumeID).
		Do(func(deviceName, volumeID string) {
			wg.Done()
		}).
		Return(nil).
		MinTimes(1)

	watcher.HandleResourceAttachment(ebsAttachment1)
	watcher.HandleResourceAttachment(ebsAttachment2)

	wg.Add(1)
	go func() {
		defer wg.Done()
		pendingEBS := watcher.agentState.GetAllPendingEBSAttachmentWithKey()
		if len(pendingEBS) > 0 {
			foundVolumes := apiebs.ScanEBSVolumes(pendingEBS, watcher.discoveryClient)
			watcher.NotifyFound(foundVolumes)
		}
	}()

	wg.Wait()

	assert.Len(t, taskEngineState.(*dockerstate.DockerTaskEngineState).GetAllEBSAttachments(), 1)
	ebsAttachment, ok := taskEngineState.(*dockerstate.DockerTaskEngineState).GetEBSByVolumeId(volumeID)
	assert.True(t, ok)
	assert.True(t, ebsAttachment.IsAttached())
}

func TestHandleInvalidTypeEBSAttachment(t *testing.T) {
	mockCtrl := gomock.NewController(t)
	defer mockCtrl.Finish()

	ctx := context.Background()
	taskEngineState := dockerstate.NewTaskEngineState()
	eventChannel := make(chan statechange.Event)
	// scanTickerController := apiebs.NewScanTickerController()
	mockDiscoveryClient := mock_ebs_discovery.NewMockEBSDiscovery(mockCtrl)

	testAttachmentProperties := map[string]string{
		apiebs.ResourceTypeName: "InvalidResourceType",
		apiebs.DeviceName:       deviceName,
		apiebs.VolumeIdName:     volumeID,
	}

	expiresAt := time.Now().Add(time.Millisecond * testconst.WaitTimeoutMillis)
	ebsAttachment := &apiebs.ResourceAttachment{
		AttachmentInfo: attachmentinfo.AttachmentInfo{
			TaskARN:              taskARN,
			TaskClusterARN:       taskClusterARN,
			ContainerInstanceARN: containerInstanceARN,
			ExpiresAt:            expiresAt,
			Status:               status.AttachmentNone,
			AttachmentARN:        resourceAttachmentARN,
		},
		AttachmentProperties: testAttachmentProperties,
	}
	watcher := newTestEBSWatcher(ctx, taskEngineState, eventChannel, mockDiscoveryClient)

	watcher.HandleResourceAttachment(ebsAttachment)

	assert.Len(t, taskEngineState.(*dockerstate.DockerTaskEngineState).GetAllEBSAttachments(), 0)
	_, ok := taskEngineState.(*dockerstate.DockerTaskEngineState).GetEBSByVolumeId(volumeID)
	assert.False(t, ok)
}

func TestHandleEBSAckTimeout(t *testing.T) {
	mockCtrl := gomock.NewController(t)
	defer mockCtrl.Finish()

	ctx := context.Background()
	taskEngineState := dockerstate.NewTaskEngineState()
	eventChannel := make(chan statechange.Event)

	mockDiscoveryClient := mock_ebs_discovery.NewMockEBSDiscovery(mockCtrl)

	testAttachmentProperties := map[string]string{
		apiebs.ResourceTypeName: apiebs.ElasticBlockStorage,
		apiebs.DeviceName:       deviceName,
		apiebs.VolumeIdName:     volumeID,
	}

	expiresAt := time.Now().Add(time.Millisecond * 5)
	ebsAttachment := &apiebs.ResourceAttachment{
		AttachmentInfo: attachmentinfo.AttachmentInfo{
			TaskARN:              taskARN,
			TaskClusterARN:       taskClusterARN,
			ContainerInstanceARN: containerInstanceARN,
			ExpiresAt:            expiresAt,
			Status:               status.AttachmentNone,
			AttachmentARN:        resourceAttachmentARN,
		},
		AttachmentProperties: testAttachmentProperties,
	}
	watcher := newTestEBSWatcher(ctx, taskEngineState, eventChannel, mockDiscoveryClient)

	watcher.HandleResourceAttachment(ebsAttachment)
	for {
		time.Sleep(time.Millisecond * 5)
		if len(taskEngineState.(*dockerstate.DockerTaskEngineState).GetAllEBSAttachments()) == 0 {
			// TODO Include data client check, this will be done in a near follow up PR
			assert.Len(t, taskEngineState.(*dockerstate.DockerTaskEngineState).GetAllEBSAttachments(), 0)
			ebsAttachment, ok := taskEngineState.(*dockerstate.DockerTaskEngineState).GetEBSByVolumeId(volumeID)
			assert.False(t, ok)
			break
		}
	}
}

func TestHandleMismatchEBSAttachment(t *testing.T) {
	mockCtrl := gomock.NewController(t)
	defer mockCtrl.Finish()

	ctx := context.Background()
	taskEngineState := dockerstate.NewTaskEngineState()
	eventChannel := make(chan statechange.Event)
	mockDiscoveryClient := mock_ebs_discovery.NewMockEBSDiscovery(mockCtrl)

	watcher := newTestEBSWatcher(ctx, taskEngineState, eventChannel, mockDiscoveryClient)

	testAttachmentProperties := map[string]string{
		apiebs.ResourceTypeName: apiebs.ElasticBlockStorage,
		apiebs.DeviceName:       deviceName,
		apiebs.VolumeIdName:     volumeID,
	}

	expiresAt := time.Now().Add(time.Millisecond * testconst.WaitTimeoutMillis)
	ebsAttachment := &apiebs.ResourceAttachment{
		AttachmentInfo: attachmentinfo.AttachmentInfo{
			TaskARN:              taskARN,
			TaskClusterARN:       taskClusterARN,
			ContainerInstanceARN: containerInstanceARN,
			ExpiresAt:            expiresAt,
			Status:               status.AttachmentNone,
			AttachmentARN:        resourceAttachmentARN,
		},
		AttachmentProperties: testAttachmentProperties,
	}

	var wg sync.WaitGroup
	wg.Add(1)
	mockDiscoveryClient.EXPECT().ConfirmEBSVolumeIsAttached(deviceName, volumeID).
		Do(func(deviceName, volumeID string) {
			wg.Done()
		}).
		Return(fmt.Errorf("%w; expected EBS volume %s but found %s", apiebs.ErrInvalidVolumeID, volumeID, "vol-321")).
		MinTimes(1)

	err := watcher.HandleResourceAttachment(ebsAttachment)
	assert.NoError(t, err)

	pendingEBS := watcher.agentState.GetAllPendingEBSAttachmentWithKey()
	foundVolumes := apiebs.ScanEBSVolumes(pendingEBS, watcher.discoveryClient)

	assert.Empty(t, foundVolumes)
	ebsAttachment, ok := taskEngineState.(*dockerstate.DockerTaskEngineState).GetEBSByVolumeId(volumeID)
	assert.True(t, ok)
	assert.ErrorIs(t, ebsAttachment.GetError(), apiebs.ErrInvalidVolumeID)
}

// func TestHandleMismatchEBSAttachment(t *testing.T) {
// 	mockCtrl := gomock.NewController(t)
// 	defer mockCtrl.Finish()

// 	ctx := context.Background()
// 	taskEngineState := dockerstate.NewTaskEngineState()
// 	eventChannel := make(chan statechange.Event)
// 	mockDiscoveryClient := mock_ebs_discovery.NewMockEBSDiscovery(mockCtrl)

// 	watcher := newTestEBSWatcher(ctx, taskEngineState, eventChannel, mockDiscoveryClient)

// 	testAttachmentProperties := map[string]string{
// 		apiebs.ResourceTypeName: apiebs.ElasticBlockStorage,
// 		apiebs.DeviceName:       deviceName,
// 		apiebs.VolumeIdName:     volumeID,
// 	}

// 	expiresAt := time.Now().Add(time.Millisecond * testconst.WaitTimeoutMillis)
// 	ebsAttachment := &apiebs.ResourceAttachment{
// 		AttachmentInfo: attachmentinfo.AttachmentInfo{
// 			TaskARN:              taskARN,
// 			TaskClusterARN:       taskClusterARN,
// 			ContainerInstanceARN: containerInstanceARN,
// 			ExpiresAt:            expiresAt,
// 			Status:               status.AttachmentNone,
// 			AttachmentARN:        resourceAttachmentARN,
// 		},
// 		AttachmentProperties: testAttachmentProperties,
// 	}

// 	var wg sync.WaitGroup
// 	wg.Add(1)
// 	mockDiscoveryClient.EXPECT().ConfirmEBSVolumeIsAttached(deviceName, volumeID).
// 		Do(func(deviceName, volumeID string) {
// 			wg.Done()
// 		}).
// 		Return(fmt.Errorf("%w; expected EBS volume %s but found %s", apiebs.ErrInvalidVolumeID, volumeID, "vol-321")).
// 		MinTimes(1)

// 	err := watcher.HandleResourceAttachment(ebsAttachment)
// 	assert.NoError(t, err)

// 	pendingEBS := watcher.agentState.GetAllPendingEBSAttachmentWithKey()
// 	foundVolumes := apiebs.ScanEBSVolumes(pendingEBS, watcher.discoveryClient)

// 	assert.Empty(t, foundVolumes)
// 	ebsAttachment, ok := taskEngineState.(*dockerstate.DockerTaskEngineState).GetEBSByVolumeId(volumeID)
// 	assert.True(t, ok)
// 	assert.ErrorIs(t, ebsAttachment.GetError(), apiebs.ErrInvalidVolumeID)

// }
