package resource

import (
	"context"
	"os/exec"
	"strings"
	"time"

	"github.com/pkg/errors"
)

const (
	ebsnvmeIDTimeoutDuration = 5 * time.Second
)

var (
	ErrInvalidVolumeID = errors.New("EBS volume IDs do not match")
)

func ConfirmEBSVolumeIsAttached(ctx context.Context, deviceName, volumeID string) error {
	ctxWithTimeout, cancel := context.WithTimeout(ctx, ebsnvmeIDTimeoutDuration)
	defer cancel()
	output, err := exec.CommandContext(ctxWithTimeout, "ebsnvme-id", "-v", deviceName).CombinedOutput()
	if err != nil {
		return errors.Wrapf(err, "failed to run ebsnvme-id: %s", string(output))
	}

	actualVolumeID, err := parseEBSNVMeIDOutput(output)
	if err != nil {
		return err
	}

	// On bare metal instances, we can reuse device names (after a task stops). Our control plane should guarantee
	// that the device name is not reused until the previous task's EBS volume has been detached. However, because
	// we are dealing with customer data, it is better to be certain.
	// If ebsnvme-id reports a different volume ID, a previous task's volume is still attached to the host.
	if volumeID != actualVolumeID {
		return errors.Wrapf(ErrInvalidVolumeID, "expected EBS volume %s but found %s", volumeID, actualVolumeID)
	}

	return nil
}

func parseEBSNVMeIDOutput(output []byte) (string, error) {
	// The output of the "ebsnvme-id -v /dev/xvda" command looks like the following:
	// Volume ID: vol-0a5620f3403272844
	out := string(output)
	volumeInfo := strings.Fields(out)
	if len(volumeInfo) != 3 {
		return "", errors.New("cannot find the volume ID: " + out)
	}
	return volumeInfo[2], nil
}
