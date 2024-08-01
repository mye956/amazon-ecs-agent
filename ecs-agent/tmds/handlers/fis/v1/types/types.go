package types

import (
	"fmt"
)

const (
	missingRequiredFieldError = "required parameter %s is missing"
	BlackHolePortFaultType    = "network-blackhole-port"
	LatencyFaultType          = "network-latency"
	PacketLossFaultType       = "network-packet-loss"
)

type NetworkFISRequestInterface interface {
	ValidateRequest() error
	ToString() string
}

type NetworkBlackholePortRequest struct {
	Port        *int
	Protocol    *string
	TrafficType *string
}

type NetworkBlackHolePortResponse struct {
	Status string `json:"status,omitempty"`
	Error  string `json:"error,omitempty"`
}

func (request NetworkBlackholePortRequest) ValidateRequest() error {
	if request.Port == nil {
		return fmt.Errorf(missingRequiredFieldError, "Port")
	}
	if request.Protocol == nil || *request.Protocol == "" {
		return fmt.Errorf(missingRequiredFieldError, "Protocol")
	}
	if request.TrafficType == nil || *request.TrafficType == "" {
		return fmt.Errorf(missingRequiredFieldError, "TrafficType")
	}
	return nil
}

func (request NetworkBlackholePortRequest) ToString() string {
	return fmt.Sprintf("NetworkBlackHolePortRequest: port=%d protocol=%s trafficType=%s", *request.Port, *request.Protocol, *request.TrafficType)
}
