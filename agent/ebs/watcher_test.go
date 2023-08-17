//go:build unit
// +build unit

package ebs

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/aws/amazon-ecs-agent/agent/engine/dockerstate"
	"github.com/aws/amazon-ecs-agent/agent/statechange"
	apiebs "github.com/aws/amazon-ecs-agent/ecs-agent/api/resource"

	"github.com/golang/mock/gomock"
	"github.com/stretchr/testify/assert"
)

const (
	resourceAttachmentARN = "arn:aws:ecs:us-west-2:123456789012:attachment/a1b2c3d4-5678-90ab-cdef-11111EXAMPLE"
	containerInstanceARN  = "arn:aws:ecs:us-west-2:123456789012:container-instance/a1b2c3d4-5678-90ab-cdef-11111EXAMPLE"
	taskARN               = "task1"
	taskClusterARN        = "arn:aws:ecs:us-west-2:123456789012:cluster/customer-task-cluster"
	deviceName            = "/dev/nvme0n1"
	volumeID              = "vol-1234"
)

type TestGroup struct {
	wg     sync.WaitGroup
	cancel context.CancelFunc
}

// NewTestGroup creates a TestGroup with the given cancellation function.
func NewTestGroup(cancel context.CancelFunc) TestGroup {
	return TestGroup{cancel: cancel}
}

// Start the given actor in a new goroutine. The actor must be initialized with
// the group's context.
func (tg *TestGroup) Start(actor Actor) {
	tg.wg.Add(1)
	go func() {
		defer tg.wg.Done()
		actor.Start()
	}()
}

// Cancel all actors started by Start() by calling the associated context and wait its completion.
func (tg *TestGroup) Cancel() {
	// Stop the actors.
	tg.cancel()

	// Make sure all actors have been stopped.
	tg.wg.Wait()
}

func setupWatcher(ctx context.Context, cancel context.CancelFunc, agentState dockerstate.TaskEngineState,
	ebsChangeEvent chan<- statechange.Event) *EBSWatcher {
	return &EBSWatcher{
		ctx:            ctx,
		cancel:         cancel,
		agentState:     agentState,
		ebsChangeEvent: ebsChangeEvent,
		mailbox:        make(chan func(), 1),
	}
}

// func TestEBSWatcher(t *testing.T) {
// 	ctrl := gomock.NewController(t)
// 	defer ctrl.Finish()

// 	testAttachmentProperties := map[string]string{
// 		apiebs.ResourceTypeName: apiebs.ElasticBlockStorage,
// 		apiebs.DeviceName:       deviceName,
// 		apiebs.VolumeIdName:     volumeID,
// 	}
// 	taskEngineState := dockerstate.NewTaskEngineState()
// 	eventChannel := make(chan statechange.Event)

// 	ebsWatcher := setupWatcher(ctx, cancel, taskEngineState, eventChannel)
// 	assert.True(true)
// }

func TestHandleResourceAttachment(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	testGroup := NewTestGroup(cancel)
	defer testGroup.Cancel()

	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	testAttachmentProperties := map[string]string{
		apiebs.ResourceTypeName: apiebs.ElasticBlockStorage,
		apiebs.DeviceName:       deviceName,
		apiebs.VolumeIdName:     volumeID,
	}

	taskEngineState := dockerstate.NewTaskEngineState()
	eventChannel := make(chan statechange.Event)

	mockPlatform.EXPECT().ConfirmEBSVolumeIsAttached(gomock.Any(), deviceName, volumeID).Return(nil).MinTimes(1)

	ebsWatcher := setupWatcher(ctx, cancel, taskEngineState, eventChannel)
	testGroup.Start(ebsWatcher)

	expiresAt := time.Now().Add(time.Millisecond * testconst.WaitTimeoutMillis)
	ebsAttachment := &apiebs.ResourceAttachment{
		AttachmentInfo: attachmentinfo.AttachmentInfo{
			TaskARN:              taskARN1,
			TaskClusterARN:       taskClusterARN,
			ClusterARN:           cluster,
			ContainerInstanceARN: containerInstanceARN,
			ExpiresAt:            expiresAt,
			AttachmentARN:        resourceAttachmentARN,
		},
		AttachmentProperties: testAttachmentProperties,
	}
	err := ebsWatcher.HandleResourceAttachment(ebsAttachment)
	assert.NoError(t, err)
	assert.Len(t, taskEngineState.(*dockerstate.DockerTaskEngineState).AllEBSAttachments(), 1)
	ebsAttachment, ok := taskEngineState.(*dockerstate.DockerTaskEngineState).GetEBSByVolumeId(volumeID)
	assert.True(t, ok)
}
