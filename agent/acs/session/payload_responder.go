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

package session

import (
	"context"
	"fmt"

	"github.com/aws/amazon-ecs-agent/agent/api"
	apitask "github.com/aws/amazon-ecs-agent/agent/api/task"
	"github.com/aws/amazon-ecs-agent/agent/data"
	"github.com/aws/amazon-ecs-agent/agent/engine"
	"github.com/aws/amazon-ecs-agent/agent/eventhandler"
	"github.com/aws/amazon-ecs-agent/ecs-agent/acs/model/ecsacs"
	apiresource "github.com/aws/amazon-ecs-agent/ecs-agent/api/attachment/resource"
	"github.com/aws/amazon-ecs-agent/ecs-agent/api/ecs"
	apitaskstatus "github.com/aws/amazon-ecs-agent/ecs-agent/api/task/status"
	"github.com/aws/amazon-ecs-agent/ecs-agent/credentials"
	"github.com/aws/amazon-ecs-agent/ecs-agent/logger"
	loggerfield "github.com/aws/amazon-ecs-agent/ecs-agent/logger/field"
	nlappmesh "github.com/aws/amazon-ecs-agent/ecs-agent/netlib/model/appmesh"
	ni "github.com/aws/amazon-ecs-agent/ecs-agent/netlib/model/networkinterface"
	"github.com/aws/amazon-ecs-agent/ecs-agent/utils"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/pkg/errors"
)

// skipAddTaskComparatorFunc defines the function pointer that accepts task status
// and returns the boolean comparison result.
type skipAddTaskComparatorFunc func(apitaskstatus.TaskStatus) bool

// payloadMessageQueueBufferSize bounds the number of payload messages that may be
// buffered between the ACS read goroutine and the persistence writer goroutine.
// A bounded buffer caps memory use; when it fills, enqueue blocks the read
// goroutine (see ProcessMessage).
const payloadMessageQueueBufferSize = 100

// payloadMessageRequest is a unit of work handed from the ACS read goroutine to
// the persistence writer goroutine.
type payloadMessageRequest struct {
	message *ecsacs.PayloadMessage
	ackFunc func(*ecsacs.AckRequest, []*ecsacs.IAMRoleCredentialsAckRequest)
}

// payloadMessageHandler implements PayloadMessageHandler interface defined in ecs-agent module.
type payloadMessageHandler struct {
	taskEngine                  engine.TaskEngine
	ecsClient                   ecs.ECSClient
	dataClient                  data.Client
	taskHandler                 *eventhandler.TaskHandler
	credentialsManager          credentials.Manager
	latestSeqNumberTaskManifest *int64
	// payloadQueue decouples task conversion, engine insertion, and the boltdb
	// commit from the ACS read goroutine. A single writer goroutine drains it,
	// so the read loop keeps draining the websocket even when a payload's
	// persistence is slow (e.g. an fsync stalled by a contended disk).
	payloadQueue chan *payloadMessageRequest
}

// NewPayloadMessageHandler creates a new payloadMessageHandler and starts the
// single writer goroutine that processes and persists payloads off the ACS read
// goroutine. The writer runs until ctx is cancelled.
func NewPayloadMessageHandler(ctx context.Context,
	taskEngine engine.TaskEngine,
	ecsClient ecs.ECSClient,
	dataClient data.Client,
	taskHandler *eventhandler.TaskHandler,
	credentialsManager credentials.Manager,
	latestSeqNumberTaskManifest *int64) *payloadMessageHandler {
	pmHandler := &payloadMessageHandler{
		taskEngine:                  taskEngine,
		ecsClient:                   ecsClient,
		dataClient:                  dataClient,
		taskHandler:                 taskHandler,
		credentialsManager:          credentialsManager,
		latestSeqNumberTaskManifest: latestSeqNumberTaskManifest,
		payloadQueue:                make(chan *payloadMessageRequest, payloadMessageQueueBufferSize),
	}
	go pmHandler.startProcessingPayloads(ctx)
	return pmHandler
}

// ProcessMessage runs on the ACS read goroutine. It performs only cheap,
// in-memory bookkeeping (the sequence number high-water mark, kept here so it
// stays serialized with the task manifest responder) and then hands the payload
// to the writer goroutine. It intentionally does no disk I/O so the read loop is
// never blocked draining the websocket.
func (pmHandler *payloadMessageHandler) ProcessMessage(message *ecsacs.PayloadMessage,
	ackFunc func(*ecsacs.AckRequest, []*ecsacs.IAMRoleCredentialsAckRequest)) error {

	// Update latestSeqNumberTaskManifest for it to get updated in state file.
	if pmHandler.latestSeqNumberTaskManifest != nil && message.SeqNum != nil &&
		*pmHandler.latestSeqNumberTaskManifest < *message.SeqNum {
		*pmHandler.latestSeqNumberTaskManifest = *message.SeqNum
	}

	// Hand off to the writer goroutine. The buffered channel absorbs bursts; if
	// it fills, this blocks the read goroutine until the writer drains one entry.
	// Ordering is preserved because a single writer consumes the channel FIFO.
	pmHandler.payloadQueue <- &payloadMessageRequest{message: message, ackFunc: ackFunc}

	return nil
}

// startProcessingPayloads is the single writer goroutine. Consuming the queue
// FIFO with one goroutine preserves payload ordering and keeps a single writer
// of task state, matching the previous synchronous behavior.
func (pmHandler *payloadMessageHandler) startProcessingPayloads(ctx context.Context) {
	for {
		select {
		case <-ctx.Done():
			return
		case req := <-pmHandler.payloadQueue:
			pmHandler.handlePayloadMessage(req)
		}
	}
}

// handlePayloadMessage processes a single payload: it converts, inserts, and
// persists the tasks, then ACKs only if every task was handled. Persisting
// before ACKing preserves the durability contract; a not-fully-handled payload
// is left unACKed so ACS redelivers it.
func (pmHandler *payloadMessageHandler) handlePayloadMessage(req *payloadMessageRequest) {
	message := req.message
	credentialsAcks, allTasksHandled := pmHandler.addPayloadTasks(message)

	if !allTasksHandled {
		logger.Critical("Unable to handle all tasks in payload message; not ACKing so ACS redelivers", logger.Fields{
			loggerfield.MessageID: aws.ToString(message.MessageId),
		})
		return
	}

	// Send ACKs - do it in async such that it does not block handling more tasks.
	go req.ackFunc(&ecsacs.AckRequest{
		Cluster:           message.ClusterArn,
		ContainerInstance: message.ContainerInstanceArn,
		MessageId:         message.MessageId,
	}, credentialsAcks)
}

// addPayloadTasks does validation on each task and, for all valid ones, adds
// it to the task engine. It returns a bool indicating if it could add every
// task to the taskEngine and a slice of credential ack requests.
func (pmHandler *payloadMessageHandler) addPayloadTasks(payload *ecsacs.PayloadMessage) (
	[]*ecsacs.IAMRoleCredentialsAckRequest, bool) {
	// Verify that we were able to work with all tasks in this payload.
	// This is so we know whether to ACK the whole thing or not.
	allTasksOK := true

	validTasks := make([]*apitask.Task, 0, len(payload.Tasks))
	for _, task := range payload.Tasks {
		if task == nil {
			logger.Critical("Received nil task for message", logger.Fields{
				loggerfield.MessageID: aws.ToString(payload.MessageId),
			})
			allTasksOK = false
			continue
		}

		// Note: If we receive an EBS-backed task, we'll also receive an incomplete volume configuration in the list of Volumes
		// To accommodate this, we'll first check if the task IS EBS-backed then we'll mark the corresponding Volume object to be
		// of type "attachment". This volume object will be replaced by the newly created EBS volume configuration when we parse
		// through the task attachments.
		volName, ok := hasEBSAttachment(task)
		if ok {
			initializeAttachmentTypeVolume(task, volName)
		}

		// Same for S3 Files attachments - mark matching volumes as "attachment" type
		// so they pass through UnmarshalJSON without error.
		s3filesVolNames := getS3FilesAttachmentVolumeNames(task)
		for _, volName := range s3filesVolNames {
			initializeAttachmentTypeVolume(task, volName)
		}

		apiTask, err := apitask.TaskFromACS(task, payload)
		if err != nil {
			pmHandler.handleInvalidTask(task, err, payload)
			allTasksOK = false
			continue
		}

		logger.Info("Received task payload from ACS", logger.Fields{
			loggerfield.TaskARN:       apiTask.Arn,
			loggerfield.TaskVersion:   apiTask.Version,
			loggerfield.DesiredStatus: apiTask.GetDesiredStatus().String(),
		})

		if apiTask.IsFaultInjectionEnabled() {
			logger.Info("Fault Injection Enabled for task", logger.Fields{
				loggerfield.TaskARN: apiTask.Arn,
			})
		}

		if task.RoleCredentials != nil {
			// The payload from ACS for the task has credentials for the
			// task. Add those to the credentials manager and set the
			// credentials id for the task as well.
			taskIAMRoleCredentials := credentials.IAMRoleCredentialsFromACS(task.RoleCredentials,
				credentials.ApplicationRoleType)
			err = pmHandler.credentialsManager.SetTaskCredentials(
				&(credentials.TaskIAMRoleCredentials{
					ARN:                aws.ToString(task.Arn),
					IAMRoleCredentials: taskIAMRoleCredentials,
				}))
			if err != nil {
				pmHandler.handleInvalidTask(task, err, payload)
				allTasksOK = false
				continue
			}
			logger.Info("Found application credentials for task", logger.Fields{
				loggerfield.TaskARN:       apiTask.Arn,
				loggerfield.TaskVersion:   apiTask.Version,
				loggerfield.RoleARN:       taskIAMRoleCredentials.RoleArn,
				loggerfield.RoleType:      taskIAMRoleCredentials.RoleType,
				loggerfield.CredentialsID: utils.TruncateString(taskIAMRoleCredentials.CredentialsID, utils.CredentialsIDLogTruncationLen),
			})
			apiTask.SetCredentialsID(taskIAMRoleCredentials.CredentialsID)
			apiTask.SetTaskRoleArn(taskIAMRoleCredentials.RoleArn)
		}

		// Add ENI information to the task struct.
		for _, acsENI := range task.ElasticNetworkInterfaces {
			eni, err := ni.InterfaceFromACS(acsENI)
			if err != nil {
				pmHandler.handleInvalidTask(task, err, payload)
				allTasksOK = false
				continue
			}
			apiTask.AddTaskENI(eni)
		}

		// Add the app mesh information to task struct.
		if task.ProxyConfiguration != nil {
			appmesh, err := nlappmesh.AppMeshFromACS(task.ProxyConfiguration)
			if err != nil {
				pmHandler.handleInvalidTask(task, err, payload)
				allTasksOK = false
				continue
			}
			apiTask.SetAppMesh(appmesh)
		}

		if task.ExecutionRoleCredentials != nil {
			// The payload message contains execution credentials for the task.
			// Add the credentials to the credentials manager and set the
			// task executionCredentials id.
			taskExecutionIAMRoleCredentials := credentials.IAMRoleCredentialsFromACS(task.ExecutionRoleCredentials,
				credentials.ExecutionRoleType)
			err = pmHandler.credentialsManager.SetTaskCredentials(
				&(credentials.TaskIAMRoleCredentials{
					ARN:                aws.ToString(task.Arn),
					IAMRoleCredentials: taskExecutionIAMRoleCredentials,
				}))
			if err != nil {
				pmHandler.handleInvalidTask(task, err, payload)
				allTasksOK = false
				continue
			}
			logger.Info("Found execution credentials for task", logger.Fields{
				loggerfield.TaskARN:       apiTask.Arn,
				loggerfield.TaskVersion:   apiTask.Version,
				loggerfield.RoleARN:       taskExecutionIAMRoleCredentials.RoleArn,
				loggerfield.RoleType:      taskExecutionIAMRoleCredentials.RoleType,
				loggerfield.CredentialsID: utils.TruncateString(taskExecutionIAMRoleCredentials.CredentialsID, utils.CredentialsIDLogTruncationLen),
			})
			apiTask.SetExecutionRoleCredentialsID(taskExecutionIAMRoleCredentials.CredentialsID)
			apiTask.SetExecutionRoleArn(taskExecutionIAMRoleCredentials.RoleArn)
		}

		validTasks = append(validTasks, apiTask)
	}

	// Add 'stop' transitions first to allow seqnum ordering to work out
	// Because a 'start' sequence number should only be proceeded if all 'stop's
	// of the same sequence number have completed, the 'start' events need to be
	// added after the 'stop' events are there to block them.
	stoppedTasksCredentialsAcks, stoppedTasksAddedOK := pmHandler.addTasks(payload, validTasks, isTaskStatusNotStopped)
	newTasksCredentialsAcks, newTasksAddedOK := pmHandler.addTasks(payload, validTasks, isTaskStatusStopped)
	if !stoppedTasksAddedOK || !newTasksAddedOK {
		allTasksOK = false
	}

	// Construct a slice with credentials acks from all tasks.
	credentialsAcks := append(stoppedTasksCredentialsAcks, newTasksCredentialsAcks...)
	return credentialsAcks, allTasksOK
}

// handleInvalidTask handles invalid tasks by sending 'stopped' with
// a suitable reason to the backend.
func (pmHandler *payloadMessageHandler) handleInvalidTask(task *ecsacs.Task, err error,
	payload *ecsacs.PayloadMessage) {
	logger.Warn("Received unexpected ACS message", logger.Fields{
		loggerfield.MessageID: aws.ToString(payload.MessageId),
		loggerfield.TaskARN:   aws.ToString(task.Arn),
		loggerfield.Error:     err,
	})

	if aws.ToString(task.Arn) == "" {
		logger.Critical("Received task with no ARN for payload message", logger.Fields{
			loggerfield.MessageID: aws.ToString(payload.MessageId),
		})
		return
	}

	// Only need to stop the task; it brings down the containers too.
	taskEvent := api.TaskStateChange{
		TaskARN: *task.Arn,
		Status:  apitaskstatus.TaskStopped,
		Reason:  UnrecognizedTaskError{err}.Error(),
		// The real task cannot be extracted from payload message, so we send an empty task.
		// This is necessary because the task handler will not send an event whose
		// Task is nil.
		Task: &apitask.Task{},
	}

	pmHandler.taskHandler.AddStateChangeEvent(taskEvent, pmHandler.ecsClient)
}

// addTasks adds the tasks to the task engine based on the skipAddTask condition.
// This is used to add non-stopped tasks before adding stopped tasks.
func (pmHandler *payloadMessageHandler) addTasks(payload *ecsacs.PayloadMessage, tasks []*apitask.Task,
	skipAddTask skipAddTaskComparatorFunc) ([]*ecsacs.IAMRoleCredentialsAckRequest, bool) {
	allTasksOK := true
	var credentialsAcks []*ecsacs.IAMRoleCredentialsAckRequest
	// tasksToSave accumulates new (desired RUNNING) tasks so they can be
	// persisted in a single transaction after the loop, incurring one fsync for
	// the whole payload rather than one per task.
	var tasksToSave []*apitask.Task
	for _, task := range tasks {
		if skipAddTask(task.GetDesiredStatus()) {
			continue
		}
		pmHandler.taskEngine.AddTask(task)
		// Only need to save task to DB when its desired status is RUNNING (i.e. this is a new task that we are going
		// to manage). When its desired status is STOPPED, the task is already in the DB and the desired status change
		// will be saved by task manager.
		if task.GetDesiredStatus() == apitaskstatus.TaskRunning {
			tasksToSave = append(tasksToSave, task)
		}

		ackCredentials := func(id string, description string) {
			ack, err := pmHandler.ackCredentials(payload.MessageId, id)
			if err != nil {
				allTasksOK = false
				logger.Error(fmt.Sprintf("Failed to acknowledge %s credentials for task",
					description), logger.Fields{
					"task":            task.String(),
					loggerfield.Error: err,
				})
				return
			}
			credentialsAcks = append(credentialsAcks, ack)
		}

		// Generate an ack request for the credentials in the task, if the
		// task is associated with an IAM role or the execution role.
		taskCredentialsID := task.GetCredentialsID()
		if taskCredentialsID != "" {
			ackCredentials(taskCredentialsID, "task iam role")
		}

		taskExecutionCredentialsID := task.GetExecutionCredentialsID()
		if taskExecutionCredentialsID != "" {
			ackCredentials(taskExecutionCredentialsID, "task execution role")
		}
	}

	// Persist all new tasks in one transaction (one fsync for the payload). On
	// failure none are persisted; allTasksOK is cleared so the payload is not
	// ACKed and ACS redelivers it.
	if len(tasksToSave) > 0 {
		if err := pmHandler.dataClient.SaveTasks(tasksToSave); err != nil {
			logger.Error("Failed to save data for tasks", logger.Fields{
				loggerfield.Error: err,
			})
			allTasksOK = false
		}
	}

	return credentialsAcks, allTasksOK
}

func (pmHandler *payloadMessageHandler) ackCredentials(messageID *string, credentialsID string) (
	*ecsacs.IAMRoleCredentialsAckRequest, error) {
	creds, ok := pmHandler.credentialsManager.GetTaskCredentials(credentialsID)
	if !ok {
		return nil, errors.Errorf("credentials could not be retrieved")

	} else {
		return &ecsacs.IAMRoleCredentialsAckRequest{
			MessageId:     messageID,
			Expiration:    aws.String(creds.IAMRoleCredentials.Expiration),
			CredentialsId: aws.String(creds.IAMRoleCredentials.CredentialsID),
		}, nil
	}
}

// isTaskStatusStopped returns true if the task status == STOPPED.
func isTaskStatusStopped(status apitaskstatus.TaskStatus) bool {
	return status == apitaskstatus.TaskStopped
}

// isTaskStatusNotStopped returns true if the task status != STOPPED.
func isTaskStatusNotStopped(status apitaskstatus.TaskStatus) bool {
	return status != apitaskstatus.TaskStopped
}

func hasEBSAttachment(acsTask *ecsacs.Task) (string, bool) {
	// TODO: This will only work if there's one EBS volume per task. If we there is a case where we have multi-attach for a task, this needs to be modified
	for _, attachment := range acsTask.Attachments {
		if *attachment.AttachmentType == apiresource.EBSTaskAttach {
			for _, property := range attachment.AttachmentProperties {
				if *property.Name == apiresource.VolumeNameKey {
					return *property.Value, true
				}
			}
		}
	}
	return "", false
}

func initializeAttachmentTypeVolume(acsTask *ecsacs.Task, volName string) {
	for _, volume := range acsTask.Volumes {
		if *volume.Name == volName && volume.Type == nil {
			newType := "attachment"
			volume.Type = &newType
		}
	}
}

// getS3FilesAttachmentVolumeNames returns the volume names from all S3 Files attachments in the task.
func getS3FilesAttachmentVolumeNames(acsTask *ecsacs.Task) []string {
	var volNames []string
	for _, attachment := range acsTask.Attachments {
		if aws.ToString(attachment.AttachmentType) == apiresource.S3FilesTaskAttach {
			for _, property := range attachment.AttachmentProperties {
				if aws.ToString(property.Name) == apiresource.S3FilesVolumeNameKey {
					volNames = append(volNames, aws.ToString(property.Value))
				}
			}
		}
	}
	return volNames
}
