package resource

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"
	// "github.com/pkg/errors"
)

const (
	ebsnvmeIDTimeoutDuration = 300 * time.Second
	ebsResourceKeyPrefix     = "ebs-volume:"
	ScanPeriod               = 500 * time.Millisecond
)

var (
	ErrInvalidVolumeID = errors.New("EBS volume IDs do not match")
)

type EBSDiscoveryClient struct {
	ctx context.Context
}

// type ScanTickerController struct {
// 	ScanTicker *time.Ticker
// 	Running    bool
// 	TickerLock sync.Mutex
// 	Done       chan bool
// }

func NewDiscoveryClient(ctx context.Context) *EBSDiscoveryClient {
	return &EBSDiscoveryClient{
		ctx: ctx,
	}
}

// func NewScanTickerController() *ScanTickerController {
// 	return &ScanTickerController{
// 		ScanTicker: nil,
// 		Running:    false,
// 		TickerLock: sync.Mutex{},
// 		Done:       make(chan bool),
// 	}
// }

// func (c *ScanTickerController) StopScanTicker() {
// 	c.TickerLock.Lock()
// 	defer c.TickerLock.Unlock()
// 	if !c.Running {
// 		return
// 	}
// 	log.Info("No more attachments to scan for. Stopping scan ticker.")
// 	c.Done <- true
// }

func ScanEBSVolumes[T GenericEBSAttachmentObject](pendingAttachments map[string]T, dc EBSDiscovery) []string {
	var err error
	var foundVolumes []string
	for key, ebs := range pendingAttachments {
		volumeId := strings.TrimPrefix(key, ebsResourceKeyPrefix)
		deviceName := ebs.GetAttachmentProperties(DeviceName)
		err = dc.ConfirmEBSVolumeIsAttached(deviceName, volumeId)
		if err != nil {
			if !errors.Is(err, ErrInvalidVolumeID) {
				err = fmt.Errorf("%w; failed to confirm if EBS volume is attached to the host", err)
				// errors.New(fmt.Sprintf("%v: failed to confirm if EBS volume is attached to the host."))
			}
			ebs.SetError(err)
			continue
		}
		foundVolumes = append(foundVolumes, key)
	}
	return foundVolumes
}
