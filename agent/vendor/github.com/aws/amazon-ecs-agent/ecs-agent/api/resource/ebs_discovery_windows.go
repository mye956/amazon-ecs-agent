//go:build windows
// +build windows

package resource

import (
	"context"
	"time"

	"github.com/pkg/errors"
)

const (
	ebsnvmeIDTimeoutDuration = 5 * time.Second
)

var (
	ErrInvalidVolumeID = errors.New("EBS volume IDs do not match")
)

// TODO
func ConfirmEBSVolumeIsAttached(ctx context.Context, deviceName, volumeID string) error {
	return nil
}
