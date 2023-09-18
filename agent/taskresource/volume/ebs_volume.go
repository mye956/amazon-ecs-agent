package volume

import (
	"fmt"

	"github.com/aws/amazon-ecs-agent/ecs-agent/acs/model/ecsacs"
	"github.com/aws/amazon-ecs-agent/ecs-agent/logger"
	"github.com/aws/aws-sdk-go/aws"
)

const (
	VolumeId             = "volumeId"
	VolumeSizeGib        = "volumeSizeGib"
	DeviceName           = "deviceName"
	SourceVolumeHostPath = "sourceVolumeHostPath"
	VolumeName           = "volumeName"
	FileSystem           = "fileSystem"
)

type EBSTaskVolumeConfig struct {
	VolumeId             string `json:"volumeId"`
	VolumeName           string `json:"volumeName"`
	VolumeSizeGib        string `json:"volumeSizeGib"`
	SourceVolumeHostPath string `json:"sourceVolumeHostPath"`
	DeviceName           string `json:"deviceName"`
	FileSystem           string `json:"fileSystem"`
}

func ParseEBSTaskVolumeAttachment(ebsAttachment *ecsacs.Attachment) (*EBSTaskVolumeConfig, error) {
	var ebsTaskVolumeConfig *EBSTaskVolumeConfig
	for _, property := range ebsAttachment.AttachmentProperties {
		if property == nil {
			return nil, fmt.Errorf("failed to parse task ebs attachment, encountered nil property")
		}

		if property.Value == nil {
			return nil, fmt.Errorf("failed to parse task ebs attachment, encountered nil property.value")
		}
		switch aws.StringValue(property.Name) {
		case VolumeId:
			ebsTaskVolumeConfig.VolumeId = aws.StringValue(property.Value)
		case VolumeSizeGib:
			ebsTaskVolumeConfig.VolumeSizeGib = aws.StringValue(property.Value)
		case DeviceName:
			ebsTaskVolumeConfig.DeviceName = aws.StringValue(property.Value)
		case SourceVolumeHostPath:
			ebsTaskVolumeConfig.SourceVolumeHostPath = aws.StringValue(property.Value)
		case VolumeName:
			ebsTaskVolumeConfig.VolumeName = aws.StringValue(property.Value)
		case FileSystem:
			ebsTaskVolumeConfig.FileSystem = aws.StringValue(property.Value)
		default:
			logger.Warn("Received an unrecognized attachment property", logger.Fields{
				"attachmentProperty": property.String(),
			})
		}
	}
	return ebsTaskVolumeConfig, nil
}

func (ebs *EBSTaskVolumeConfig) Source () string {
	return ebs.SourceVolumeHostPath
}