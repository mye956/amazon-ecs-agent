package ebs

import (
	"fmt"

	"github.com/aws/amazon-ecs-agent/ecs-agent/acs/model/ecsacs"
	"github.com/sirupsen/logrus"

	log "github.com/aws/amazon-ecs-agent/ecs-agent/logger"

	"github.com/aws/aws-sdk-go/aws"
)

func ParseEBSAttachment(ebsAttachment *ecsacs.Attachment) (*EBSVolumeConfig, error) {
	var ebsVolumeConfig *EBSVolumeConfig
	for _, property := range ebsAttachment.AttachmentProperties {
		if property == nil {
			return nil, fmt.Errorf("failed to parse task ebs attachment, encountered nil property")
		}

		if property.Value == nil {
			return nil, fmt.Errorf("failed to parse task ebs attachment, encountered nil property.value")
		}
		switch aws.StringValue(property.Name) {
		case VolumeId:
			ebsVolumeConfig.VolumeId = aws.StringValue(property.Value)
		case VolumeSizeGib:
			ebsVolumeConfig.VolumeSizeGib = aws.StringValue(property.Value)
		case DeviceName:
			ebsVolumeConfig.DeviceName = aws.StringValue(property.Value)
		case SourceVolumeHostPath:
			ebsVolumeConfig.SourceVolumeHostPath = aws.StringValue(property.Value)
		case VolumeName:
			ebsVolumeConfig.VolumeName = aws.StringValue(property.Value)
		case FileSystem:
			ebsVolumeConfig.FileSystem = aws.StringValue(property.Value)
		default:
			log.WithFields(logrus.Fields{
				"PropertyName":  aws.StringValue(property.Name),
				"PropertyValue": aws.StringValue(property.Value),
			}).Warn("Unknown property received from ACS")

		}
	}
	return ebsVolumeConfig, nil
}
