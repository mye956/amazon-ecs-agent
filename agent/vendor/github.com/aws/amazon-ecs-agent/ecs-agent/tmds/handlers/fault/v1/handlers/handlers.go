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

package handlers

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"os/exec"
	"runtime"
	"time"

	// "runtime"
	"strconv"
	"strings"
	"sync"

	"github.com/aws/amazon-ecs-agent/ecs-agent/api/ecs/model/ecs"
	"github.com/aws/amazon-ecs-agent/ecs-agent/logger"
	"github.com/aws/amazon-ecs-agent/ecs-agent/logger/field"
	"github.com/aws/amazon-ecs-agent/ecs-agent/metrics"
	"github.com/aws/amazon-ecs-agent/ecs-agent/tmds/handlers/fault/v1/types"
	"github.com/aws/amazon-ecs-agent/ecs-agent/tmds/handlers/utils"
	v4 "github.com/aws/amazon-ecs-agent/ecs-agent/tmds/handlers/v4"
	state "github.com/aws/amazon-ecs-agent/ecs-agent/tmds/handlers/v4/state"
	"github.com/aws/amazon-ecs-agent/ecs-agent/utils/iptableswrapper"
	"github.com/aws/amazon-ecs-agent/ecs-agent/utils/netnswrapper"

	"github.com/gorilla/mux"
)

const (
	startFaultRequestType       = "start %s"
	stopFaultRequestType        = "stop %s"
	checkStatusFaultRequestType = "check status %s"
	invalidNetworkModeError     = "%s mode is not supported. Please use either host or awsvpc mode."
	faultInjectionEnabledError  = "fault injection is not enabled for task: %s"
)

var (
	nsenterCommandString = "nsenter --net=%s"
)

type FaultHandler struct {
	// mutexMap is used to avoid multiple clients to manipulate same resource at same
	// time. The 'key' is the the network namespace path and 'value' is the RWMutex.
	// Using concurrent map here because the handler is shared by all requests.
	mutexMap        sync.Map
	AgentState      state.AgentState
	MetricsFactory  metrics.EntryFactory
	netNsWrapper    netnswrapper.NetNsWrapper
	iptablesWrapper iptableswrapper.IPTables
}

func New(agentState state.AgentState, mf metrics.EntryFactory) *FaultHandler {
	return &FaultHandler{
		AgentState:      agentState,
		MetricsFactory:  mf,
		mutexMap:        sync.Map{},
		netNsWrapper:    netnswrapper.New(),
		iptablesWrapper: iptableswrapper.NewWrapper(),
	}
}

// NetworkFaultPath will take in a fault type and return the TMDS endpoint path
func NetworkFaultPath(fault string) string {
	return fmt.Sprintf("/api/%s/fault/v1/%s",
		utils.ConstructMuxVar(v4.EndpointContainerIDMuxName, utils.AnythingButSlashRegEx), fault)
}

// loadLock returns the lock associated with given key.
func (h *FaultHandler) loadLock(key string) *sync.RWMutex {
	mu := new(sync.RWMutex)
	actualMu, _ := h.mutexMap.LoadOrStore(key, mu)
	return actualMu.(*sync.RWMutex)
}

// StartNetworkBlackholePort will return the request handler function for starting a network blackhole port fault
func (h *FaultHandler) StartNetworkBlackholePort() func(http.ResponseWriter, *http.Request) {
	return func(w http.ResponseWriter, r *http.Request) {
		var request types.NetworkBlackholePortRequest
		requestType := fmt.Sprintf(startFaultRequestType, types.BlackHolePortFaultType)

		// Parse the fault request
		err := decodeRequest(w, &request, requestType, r)
		if err != nil {
			return
		}
		// Validate the fault request
		err = validateRequest(w, request, requestType)
		if err != nil {
			return
		}

		// Obtain the task metadata via the endpoint container ID
		taskMetadata, err := validateTaskMetadata(w, h.AgentState, requestType, r)
		if err != nil {
			return
		}

		// To avoid multiple requests to manipulate same network resource
		networkNSPath := taskMetadata.TaskNetworkConfig.NetworkNamespaces[0].Path
		rwMu := h.loadLock(networkNSPath)
		rwMu.Lock()
		defer rwMu.Unlock()

		// TODO: Check status of current fault injection
		// TODO: Invoke the start fault injection functionality if not running

		var responseBody types.NetworkFaultInjectionResponse
		chainName := fmt.Sprintf("%s%s%s", *request.TrafficType, *request.Protocol, strconv.FormatUint(uint64(*request.Port), 10))
		// _, err = h.startBlackHolePortFault(request, chainName, taskMetadata.TaskNetworkConfig.NetworkNamespaces[0].Path)
		_, err = h.startBlackHolePortFault2(request, chainName, taskMetadata.TaskNetworkConfig.NetworkNamespaces[0].Path)

		if err != nil {
			errResponse := fmt.Sprintf("%v", err)
			responseBody = types.NewNetworkFaultInjectionErrorResponse(errResponse)
		} else {
			responseBody = types.NewNetworkFaultInjectionSuccessResponse("running")
		}

		logger.Info("Successfully started fault", logger.Fields{
			field.RequestType: requestType,
			field.Request:     request.ToString(),
			field.Response:    responseBody.ToString(),
		})
		utils.WriteJSONResponse(
			w,
			http.StatusOK,
			responseBody,
			requestType,
		)
	}
}

// StopNetworkBlackHolePort will return the request handler function for stopping a network blackhole port fault
func (h *FaultHandler) StopNetworkBlackHolePort() func(http.ResponseWriter, *http.Request) {
	return func(w http.ResponseWriter, r *http.Request) {
		var request types.NetworkBlackholePortRequest
		requestType := fmt.Sprintf(stopFaultRequestType, types.BlackHolePortFaultType)

		// Parse the fault request
		err := decodeRequest(w, &request, requestType, r)
		if err != nil {
			return
		}
		// Validate the fault request
		err = validateRequest(w, request, requestType)
		if err != nil {
			return
		}

		logger.Debug("Successfully parsed fault request payload", logger.Fields{
			field.Request: request.ToString(),
		})

		// Obtain the task metadata via the endpoint container ID
		taskMetadata, err := validateTaskMetadata(w, h.AgentState, requestType, r)
		if err != nil {
			return
		}

		// To avoid multiple requests to manipulate same network resource
		networkNSPath := taskMetadata.TaskNetworkConfig.NetworkNamespaces[0].Path
		rwMu := h.loadLock(networkNSPath)
		rwMu.Lock()
		defer rwMu.Unlock()

		// TODO: Check status of current fault injection
		// TODO: Invoke the stop fault injection functionality if running

		var responseBody types.NetworkFaultInjectionResponse
		chainName := fmt.Sprintf("%s%s%s", *request.TrafficType, *request.Protocol, strconv.FormatUint(uint64(*request.Port), 10))
		// status, err := h.stopBlackHoldPortFault(request, chainName, taskMetadata.TaskNetworkConfig.NetworkNamespaces[0].Path)
		status, err := h.stopBlackHoldPortFault2(request, chainName, taskMetadata.TaskNetworkConfig.NetworkNamespaces[0].Path)

		if err != nil {
			errResponse := fmt.Sprintf("%v", err)
			responseBody = types.NewNetworkFaultInjectionErrorResponse(errResponse)
		} else if !status {
			responseBody = types.NewNetworkFaultInjectionSuccessResponse("stopped")
		} else {
			responseBody = types.NewNetworkFaultInjectionSuccessResponse("running")
		}

		// responseBody := types.NewNetworkFaultInjectionSuccessResponse("stopped")
		logger.Info("Successfully stopped fault", logger.Fields{
			field.RequestType: requestType,
			field.Request:     request.ToString(),
			field.Response:    responseBody.ToString(),
		})
		utils.WriteJSONResponse(
			w,
			http.StatusOK,
			responseBody,
			requestType,
		)
	}
}

// CheckNetworkBlackHolePort will return the request handler function for checking the status of a network blackhole port fault
func (h *FaultHandler) CheckNetworkBlackHolePort() func(http.ResponseWriter, *http.Request) {
	return func(w http.ResponseWriter, r *http.Request) {
		var request types.NetworkBlackholePortRequest
		requestType := fmt.Sprintf(checkStatusFaultRequestType, types.BlackHolePortFaultType)

		// Parse the fault request
		err := decodeRequest(w, &request, requestType, r)
		if err != nil {
			return
		}
		// Validate the fault request
		err = validateRequest(w, request, requestType)
		if err != nil {
			return
		}

		logger.Debug("Successfully parsed fault request payload", logger.Fields{
			field.Request: request.ToString(),
		})

		// Obtain the task metadata via the endpoint container ID
		taskMetadata, err := validateTaskMetadata(w, h.AgentState, requestType, r)
		if err != nil {
			return
		}

		// To avoid multiple requests to manipulate same network resource
		networkNSPath := taskMetadata.TaskNetworkConfig.NetworkNamespaces[0].Path
		rwMu := h.loadLock(networkNSPath)
		rwMu.RLock()
		defer rwMu.RUnlock()

		var responseBody types.NetworkFaultInjectionResponse
		chainName := fmt.Sprintf("%s%s%s", *request.TrafficType, *request.Protocol, strconv.FormatUint(uint64(*request.Port), 10))
		// status, err := h.checkStatusNetworkBlackholePort(request, chainName, taskMetadata.TaskNetworkConfig.NetworkNamespaces[0].Path)
		// status, err := h.setNs(request, chainName, taskMetadata.TaskNetworkConfig.NetworkNamespaces[0].Path)
		status, err := h.checkStatusNetworkBlackholePort2(request, chainName, taskMetadata.TaskNetworkConfig.NetworkNamespaces[0].Path)
		if err != nil {
			errResponse := fmt.Sprintf("%v", err)
			utils.WriteJSONResponse(
				w,
				http.StatusInternalServerError,
				types.NewNetworkFaultInjectionErrorResponse(errResponse),
				requestType,
			)
		}

		if status {
			responseBody = types.NewNetworkFaultInjectionSuccessResponse("running")
		} else {
			responseBody = types.NewNetworkFaultInjectionSuccessResponse("not-running")
		}

		// TODO: Check status of current fault injection
		// TODO: Return the correct status state

		// responseBody := types.NewNetworkFaultInjectionSuccessResponse("running")

		logger.Info("Successfully checked status for fault", logger.Fields{
			field.RequestType: requestType,
			field.Request:     request.ToString(),
			field.Response:    responseBody.ToString(),
		})
		utils.WriteJSONResponse(
			w,
			http.StatusOK,
			responseBody,
			requestType,
		)
	}
}

// StartNetworkLatency starts a network latency fault in the associated ENI if no existing same fault.
func (h *FaultHandler) StartNetworkLatency() func(http.ResponseWriter, *http.Request) {
	return func(w http.ResponseWriter, r *http.Request) {
		var request types.NetworkLatencyRequest
		requestType := fmt.Sprintf(startFaultRequestType, types.LatencyFaultType)
		// Parse the fault request
		err := decodeRequest(w, &request, requestType, r)
		if err != nil {
			return
		}

		// Validate the fault request
		err = validateRequest(w, request, requestType)
		if err != nil {
			return
		}

		// Obtain the task metadata via the endpoint container ID
		taskMetadata, err := validateTaskMetadata(w, h.AgentState, requestType, r)
		if err != nil {
			return
		}

		// To avoid multiple requests to manipulate same network resource
		networkNSPath := taskMetadata.TaskNetworkConfig.NetworkNamespaces[0].Path
		rwMu := h.loadLock(networkNSPath)
		rwMu.Lock()
		defer rwMu.Unlock()

		// TODO: Check status of current fault injection
		// TODO: Invoke the start fault injection functionality if not running

		responseBody := types.NewNetworkFaultInjectionSuccessResponse("running")
		logger.Info("Successfully started fault", logger.Fields{
			field.RequestType: requestType,
			field.Request:     request.ToString(),
			field.Response:    responseBody.ToString(),
		})
		utils.WriteJSONResponse(
			w,
			http.StatusOK,
			responseBody,
			requestType,
		)
	}
}

// StopNetworkLatency stops a network latency fault in the associated ENI if there is one existing same fault.
func (h *FaultHandler) StopNetworkLatency() func(http.ResponseWriter, *http.Request) {
	return func(w http.ResponseWriter, r *http.Request) {
		var request types.NetworkLatencyRequest
		requestType := fmt.Sprintf(stopFaultRequestType, types.LatencyFaultType)

		// Parse the fault request
		err := decodeRequest(w, &request, requestType, r)
		if err != nil {
			return
		}
		// Validate the fault request
		err = validateRequest(w, request, requestType)
		if err != nil {
			return
		}

		// Obtain the task metadata via the endpoint container ID
		taskMetadata, err := validateTaskMetadata(w, h.AgentState, requestType, r)
		if err != nil {
			return
		}

		// To avoid multiple requests to manipulate same network resource
		networkNSPath := taskMetadata.TaskNetworkConfig.NetworkNamespaces[0].Path
		rwMu := h.loadLock(networkNSPath)
		rwMu.Lock()
		defer rwMu.Unlock()

		// TODO: Check status of current fault injection
		// TODO: Invoke the stop fault injection functionality if running

		responseBody := types.NewNetworkFaultInjectionSuccessResponse("stopped")
		logger.Info("Successfully stopped fault", logger.Fields{
			field.RequestType: requestType,
			field.Request:     request.ToString(),
			field.Response:    responseBody.ToString(),
		})
		utils.WriteJSONResponse(
			w,
			http.StatusOK,
			responseBody,
			requestType,
		)
	}
}

// CheckNetworkLatency checks the status of given network latency fault.
func (h *FaultHandler) CheckNetworkLatency() func(http.ResponseWriter, *http.Request) {
	return func(w http.ResponseWriter, r *http.Request) {
		var request types.NetworkLatencyRequest
		requestType := fmt.Sprintf(checkStatusFaultRequestType, types.LatencyFaultType)

		// Parse the fault request
		err := decodeRequest(w, &request, requestType, r)
		if err != nil {
			return
		}
		// Validate the fault request
		err = validateRequest(w, request, requestType)
		if err != nil {
			return
		}

		// Obtain the task metadata via the endpoint container ID
		taskMetadata, err := validateTaskMetadata(w, h.AgentState, requestType, r)
		if err != nil {
			return
		}

		// To avoid multiple requests to manipulate same network resource
		networkNSPath := taskMetadata.TaskNetworkConfig.NetworkNamespaces[0].Path
		rwMu := h.loadLock(networkNSPath)
		rwMu.RLock()
		defer rwMu.RUnlock()

		// TODO: Check status of current fault injection
		// TODO: Return the correct status state
		responseBody := types.NewNetworkFaultInjectionSuccessResponse("running")
		logger.Info("Successfully checked status for fault", logger.Fields{
			field.RequestType: requestType,
			field.Request:     request.ToString(),
			field.Response:    responseBody.ToString(),
		})
		utils.WriteJSONResponse(
			w,
			http.StatusOK,
			responseBody,
			requestType,
		)
	}
}

// StartNetworkPacketLoss starts a network packet loss fault in the associated ENI if no existing same fault.
func (h *FaultHandler) StartNetworkPacketLoss() func(http.ResponseWriter, *http.Request) {
	return func(w http.ResponseWriter, r *http.Request) {
		var request types.NetworkPacketLossRequest
		requestType := fmt.Sprintf(startFaultRequestType, types.PacketLossFaultType)
		// Parse the fault request
		err := decodeRequest(w, &request, requestType, r)
		if err != nil {
			return
		}

		// Validate the fault request
		err = validateRequest(w, request, requestType)
		if err != nil {
			return
		}

		// Obtain the task metadata via the endpoint container ID
		taskMetadata, err := validateTaskMetadata(w, h.AgentState, requestType, r)
		if err != nil {
			return
		}

		// To avoid multiple requests to manipulate same network resource
		networkNSPath := taskMetadata.TaskNetworkConfig.NetworkNamespaces[0].Path
		rwMu := h.loadLock(networkNSPath)
		rwMu.Lock()
		defer rwMu.Unlock()

		// TODO: Check status of current fault injection
		// TODO: Invoke the start fault injection functionality if not running

		responseBody := types.NewNetworkFaultInjectionSuccessResponse("running")
		logger.Info("Successfully started fault", logger.Fields{
			field.RequestType: requestType,
			field.Request:     request.ToString(),
			field.Response:    responseBody.ToString(),
		})
		utils.WriteJSONResponse(
			w,
			http.StatusOK,
			responseBody,
			requestType,
		)
	}
}

// StopNetworkPacketLoss stops a network packet loss fault in the associated ENI if there is one existing same fault.
func (h *FaultHandler) StopNetworkPacketLoss() func(http.ResponseWriter, *http.Request) {
	return func(w http.ResponseWriter, r *http.Request) {
		var request types.NetworkPacketLossRequest
		requestType := fmt.Sprintf(startFaultRequestType, types.PacketLossFaultType)

		// Parse the fault request
		err := decodeRequest(w, &request, requestType, r)
		if err != nil {
			return
		}
		// Validate the fault request
		err = validateRequest(w, request, requestType)
		if err != nil {
			return
		}

		// Obtain the task metadata via the endpoint container ID
		taskMetadata, err := validateTaskMetadata(w, h.AgentState, requestType, r)
		if err != nil {
			return
		}

		// To avoid multiple requests to manipulate same network resource
		networkNSPath := taskMetadata.TaskNetworkConfig.NetworkNamespaces[0].Path
		rwMu := h.loadLock(networkNSPath)
		rwMu.Lock()
		defer rwMu.Unlock()

		// TODO: Check status of current fault injection
		// TODO: Invoke the stop fault injection functionality if running

		responseBody := types.NewNetworkFaultInjectionSuccessResponse("stopped")
		logger.Info("Successfully stopped fault", logger.Fields{
			field.RequestType: requestType,
			field.Request:     request.ToString(),
			field.Response:    responseBody.ToString(),
		})
		utils.WriteJSONResponse(
			w,
			http.StatusOK,
			responseBody,
			requestType,
		)
	}
}

// CheckNetworkPacketLoss checks the status of given network packet loss fault.
func (h *FaultHandler) CheckNetworkPacketLoss() func(http.ResponseWriter, *http.Request) {
	return func(w http.ResponseWriter, r *http.Request) {
		var request types.NetworkPacketLossRequest
		requestType := fmt.Sprintf(startFaultRequestType, types.PacketLossFaultType)

		// Parse the fault request
		err := decodeRequest(w, &request, requestType, r)
		if err != nil {
			return
		}
		// Validate the fault request
		err = validateRequest(w, request, requestType)
		if err != nil {
			return
		}

		// Obtain the task metadata via the endpoint container ID
		// TODO: Will be using the returned task metadata in a future PR
		taskMetadata, err := validateTaskMetadata(w, h.AgentState, requestType, r)
		if err != nil {
			return
		}

		// To avoid multiple requests to manipulate same network resource
		networkNSPath := taskMetadata.TaskNetworkConfig.NetworkNamespaces[0].Path
		rwMu := h.loadLock(networkNSPath)
		rwMu.RLock()
		defer rwMu.RUnlock()

		// TODO: Check status of current fault injection
		// TODO: Return the correct status state
		responseBody := types.NewNetworkFaultInjectionSuccessResponse("running")
		logger.Info("Successfully checked status for fault", logger.Fields{
			field.RequestType: requestType,
			field.Request:     request.ToString(),
			field.Response:    responseBody.ToString(),
		})
		utils.WriteJSONResponse(
			w,
			http.StatusOK,
			responseBody,
			requestType,
		)
	}
}

// decodeRequest will translate/unmarshal an incoming fault injection request into one of the network fault structs
func decodeRequest(w http.ResponseWriter, request types.NetworkFaultRequest, requestType string, r *http.Request) error {
	logRequest(requestType, r)
	jsonDecoder := json.NewDecoder(r.Body)
	if err := jsonDecoder.Decode(request); err != nil {
		responseBody := types.NewNetworkFaultInjectionErrorResponse(fmt.Sprintf("%v", err))
		logger.Error("Error: failed to decode request", logger.Fields{
			field.Error:       err,
			field.RequestType: requestType,
			field.Request:     request.ToString(),
			field.Response:    responseBody.ToString(),
		})

		utils.WriteJSONResponse(
			w,
			http.StatusBadRequest,
			responseBody,
			requestType,
		)
		return err
	}
	return nil
}

// validateRequest will validate that the incoming fault injection request will have the required fields.
func validateRequest(w http.ResponseWriter, request types.NetworkFaultRequest, requestType string) error {
	if err := request.ValidateRequest(); err != nil {
		responseBody := types.NewNetworkFaultInjectionErrorResponse(fmt.Sprintf("%v", err))
		logger.Error("Error: missing required payload fields", logger.Fields{
			field.Error:       err,
			field.RequestType: requestType,
			field.Request:     request.ToString(),
			field.Response:    responseBody.ToString(),
		})
		utils.WriteJSONResponse(
			w,
			http.StatusBadRequest,
			responseBody,
			requestType,
		)

		return err
	}
	return nil
}

// validateTaskMetadata will first fetch the associated task metadata and then validate it to make sure
// the task has enabled fault injection and the corresponding network mode is supported.
func validateTaskMetadata(w http.ResponseWriter, agentState state.AgentState, requestType string, r *http.Request) (*state.TaskResponse, error) {
	var taskMetadata state.TaskResponse
	endpointContainerID := mux.Vars(r)[v4.EndpointContainerIDMuxName]
	taskMetadata, err := agentState.GetTaskMetadata(endpointContainerID)
	if err != nil {
		code, errResponse := getTaskMetadataErrorResponse(endpointContainerID, requestType, err)
		responseBody := types.NewNetworkFaultInjectionErrorResponse(fmt.Sprintf("%v", errResponse))
		logger.Error("Error: Unable to obtain task metadata", logger.Fields{
			field.Error:       errResponse,
			field.RequestType: requestType,
			field.Response:    responseBody.ToString(),
		})
		utils.WriteJSONResponse(
			w,
			code,
			responseBody,
			requestType,
		)
		return nil, errResponse
	}

	// Check if task is FIS-enabled
	if !taskMetadata.FaultInjectionEnabled {
		errResponse := fmt.Sprintf(faultInjectionEnabledError, taskMetadata.TaskARN)
		responseBody := types.NewNetworkFaultInjectionErrorResponse(errResponse)
		logger.Error("Error: Task is not fault injection enabled.", logger.Fields{
			field.RequestType:             requestType,
			field.TMDSEndpointContainerID: endpointContainerID,
			field.Response:                responseBody.ToString(),
			field.TaskARN:                 taskMetadata.TaskARN,
			field.Error:                   errResponse,
		})
		utils.WriteJSONResponse(
			w,
			http.StatusBadRequest,
			responseBody,
			requestType,
		)
		return nil, errors.New(errResponse)
	}

	if err := validateTaskNetworkConfig(taskMetadata.TaskNetworkConfig); err != nil {
		code, errResponse := getTaskMetadataErrorResponse(endpointContainerID, requestType, err)
		responseBody := types.NewNetworkFaultInjectionErrorResponse(fmt.Sprintf("%v", errResponse))
		logger.Error("Error: Unable to resolve task network config within task metadata", logger.Fields{
			field.Error:                   err,
			field.RequestType:             requestType,
			field.Response:                responseBody.ToString(),
			field.TMDSEndpointContainerID: endpointContainerID,
		})
		utils.WriteJSONResponse(
			w,
			code,
			responseBody,
			requestType,
		)
		return nil, errResponse
	}

	// Check if task is using a valid network mode
	networkMode := taskMetadata.TaskNetworkConfig.NetworkMode
	if networkMode != ecs.NetworkModeHost && networkMode != ecs.NetworkModeAwsvpc {
		errResponse := fmt.Sprintf(invalidNetworkModeError, networkMode)
		responseBody := types.NewNetworkFaultInjectionErrorResponse(errResponse)
		logger.Error("Error: Invalid network mode for fault injection", logger.Fields{
			field.RequestType: requestType,
			field.NetworkMode: networkMode,
			field.Response:    responseBody.ToString(),
		})
		utils.WriteJSONResponse(
			w,
			http.StatusBadRequest,
			responseBody,
			requestType,
		)
		return nil, errors.New(errResponse)
	}

	return &taskMetadata, nil
}

// getTaskMetadataErrorResponse will be used to classify certain errors that was returned from a GetTaskMetadata function call.
func getTaskMetadataErrorResponse(endpointContainerID, requestType string, err error) (int, error) {
	var errContainerLookupFailed *state.ErrorLookupFailure
	if errors.As(err, &errContainerLookupFailed) {
		return http.StatusNotFound, fmt.Errorf("unable to lookup container: %s", endpointContainerID)
	}

	var errFailedToGetContainerMetadata *state.ErrorMetadataFetchFailure
	if errors.As(err, &errFailedToGetContainerMetadata) {
		return http.StatusInternalServerError, fmt.Errorf("unable to obtain container metadata for container: %s", endpointContainerID)
	}

	logger.Error("Unknown error encountered when handling task metadata fetch failure", logger.Fields{
		field.Error:       err,
		field.RequestType: requestType,
	})
	return http.StatusInternalServerError, fmt.Errorf("failed to get task metadata due to internal server error for container: %s", endpointContainerID)
}

// logRequest is used to log incoming fault injection requests.
func logRequest(requestType string, r *http.Request) {
	body, err := io.ReadAll(r.Body)
	if err != nil {
		logger.Error("Error: Unable to decode request body", logger.Fields{
			field.RequestType: requestType,
			field.Error:       err,
		})
		return
	}
	logger.Info(fmt.Sprintf("Received new request for request type: %s", requestType), logger.Fields{
		field.Request:     string(body),
		field.RequestType: requestType,
	})
	r.Body = io.NopCloser(bytes.NewBuffer(body))
}

// validateTaskNetworkConfig validates the passed in task network config for any null/empty values.
func validateTaskNetworkConfig(taskNetworkConfig *state.TaskNetworkConfig) error {
	if taskNetworkConfig == nil {
		return errors.New("TaskNetworkConfig is empty within task metadata")
	}

	if len(taskNetworkConfig.NetworkNamespaces) == 0 || taskNetworkConfig.NetworkNamespaces[0] == nil {
		return errors.New("empty network namespaces within task network config")
	}

	// Task network namespace path is required to inject faults in the associated task.
	if taskNetworkConfig.NetworkNamespaces[0].Path == "" {
		return errors.New("no path in the network namespace within task network config")
	}

	if len(taskNetworkConfig.NetworkNamespaces[0].NetworkInterfaces) == 0 || taskNetworkConfig.NetworkNamespaces[0].NetworkInterfaces == nil {
		return errors.New("empty network interfaces within task network config")
	}

	// Device name is required to inject network faults to given ENI in the task.
	if taskNetworkConfig.NetworkNamespaces[0].NetworkInterfaces[0].DeviceName == "" {
		return errors.New("no ENI device name in the network namespace within task network config")
	}

	return nil
}

// func (h *FaultHandler) setNs(request types.NetworkBlackholePortRequest, chain, netNs string) (bool, error) {
// 	runtime.LockOSThread()
// 	defer runtime.UnlockOSThread()
// 	origns, err := h.netNsWrapper.Get()
// 	if err != nil {
// 		return false, err
// 	}
// 	defer h.netNsWrapper.CloseHandle(&origns)

// 	if netNs != "host" {
// 		logger.Info("[DEBUG] Trying to switch network namespace", logger.Fields{
// 			"netns": netNs,
// 		})
// 		nsHandle, err := h.netNsWrapper.GetFromPath(netNs)
// 		if err != nil {
// 			return false, err
// 		}

// 		err = h.netNsWrapper.Set(nsHandle)
// 		if err != nil {
// 			return false, err
// 		}
// 		logger.Info("[DEBUG] Switched network namespace", logger.Fields{
// 			"netns": netNs,
// 		})
// 		status, err := h.checkStatusNetworkBlackholePort(request, chain, netNs)
// 		logger.Info("[DEBUG] IN TASK NETNS Checking fault is running", logger.Fields{
// 			"status": status,
// 			"err":    err,
// 		})

// 		h.netNsWrapper.CloseHandle(&nsHandle)
// 	}

// 	err = h.netNsWrapper.Set(origns)
// 	if err != nil {
// 		return false, err
// 	}
// 	logger.Info("[DEBUG] Back in host network namespace", logger.Fields{
// 		"netns": netNs,
// 	})
// 	return h.checkStatusNetworkBlackholePort(request, chain, netNs)
// }

func (h *FaultHandler) startBlackHolePortFault(request types.NetworkBlackholePortRequest, chain, netNs string) (bool, error) {
	// Lock the OS thread so we don't accidentally switch namespaces
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()

	originalNS, err := h.netNsWrapper.Get()
	if err != nil {
		return false, err
	}

	logger.Info("Original namespace", logger.Fields{
		"orignalNS": originalNS,
	})
	defer h.netNsWrapper.CloseHandle(&originalNS)

	if netNs != "host" {
		taskNS, err := h.netNsWrapper.GetFromPath(netNs)
		if err != nil {
			return false, err
		}
		logger.Info("Task ENI namespace", logger.Fields{
			"taskNS": taskNS,
		})
		defer h.netNsWrapper.CloseHandle(&taskNS)

		err = h.netNsWrapper.Set(taskNS)
		if err != nil {
			return false, err
		}
		logger.Info("Task Network NS has been set")
	}

	cmd := exec.Command("iptables", "-nL")
	stdErr := &bytes.Buffer{}
	stdOut := &bytes.Buffer{}
	cmd.Stderr = stdErr
	cmd.Stdout = stdOut
	err = cmd.Run()
	if err != nil {
		logger.Info("Unable to directly call iptables", logger.Fields{
			"stdErr": stdErr.String(),
			"stdOut": stdOut.String(),
			"err":    err,
		})
	}

	logger.Info("Successfully called iptables via os exec before starting fault", logger.Fields{
		"stdErr": stdErr.String(),
		"stdOut": stdOut.String(),
	})

	chains, err := h.iptablesWrapper.ListChains()
	if err != nil {
		logger.Error("[ERROR] Unable to list chains", logger.Fields{
			"err": err,
		})
	}
	logger.Info("Obtained chains", logger.Fields{
		"chains": strings.Join(chains, " "),
	})

	err = h.iptablesWrapper.NewChain(chain)
	if err != nil {
		return false, err
	}

	logger.Info("Added new chain", logger.Fields{
		"chain": chain,
	})

	err = h.iptablesWrapper.Append(chain, *request.Protocol, *request.Port)
	if err != nil {
		return false, err
	}
	logger.Info("Added DROP rule to chain", logger.Fields{
		"chain":    chain,
		"protocol": *request.Protocol,
		"port":     *request.Port,
	})

	insertChain := "OUTPUT"
	if *request.TrafficType == "ingress" {
		insertChain = "INPUT"
	}
	err = h.iptablesWrapper.Insert(chain, insertChain)
	if err != nil {
		return false, err
	}

	logger.Info("Inserted chain to built in iptables chain", logger.Fields{
		"chain":       chain,
		"insertChain": insertChain,
	})

	ifaces, err := net.Interfaces()
	if err != nil {
		return false, err
	}
	for _, iface := range ifaces {
		logger.Info("[DEBUG] Obtained task network interface", logger.Fields{
			"interfaces": iface.Name,
		})
	}
	chains, err = h.iptablesWrapper.ListChains()
	if err != nil {
		logger.Error("[ERROR] Unable to list chains", logger.Fields{
			"err": err,
		})
		return false, err
	}
	logger.Info("Obtained chains", logger.Fields{
		"chains": strings.Join(chains, " "),
	})

	exist, err := h.iptablesWrapper.Exists(chain, *request.Protocol, *request.Port)
	logger.Info("[DEBUG] Checked status of running black hole port", logger.Fields{
		"exists":    exist,
		"error":     err,
		"chainName": chain,
		"netNs":     netNs,
		"port":      *request.Port,
		"protocol":  *request.Protocol,
	})

	cmd = exec.Command("iptables", "-nL")
	stdErr = &bytes.Buffer{}
	stdOut = &bytes.Buffer{}
	cmd.Stderr = stdErr
	cmd.Stdout = stdOut
	errCmd := cmd.Run()
	if errCmd != nil {
		logger.Info("Unable to directly call iptables", logger.Fields{
			"stdErr": stdErr.String(),
			"stdOut": stdOut.String(),
			"err":    errCmd,
		})
	}

	logger.Info("Successfully called iptables via os exec before starting fault", logger.Fields{
		"stdErr": stdErr.String(),
		"stdOut": stdOut.String(),
	})

	if netNs != "host" {
		h.netNsWrapper.Set(originalNS)
		logger.Info("Going back to the orignal NS")
	}

	chains, err2 := h.iptablesWrapper.ListChains()
	if err2 != nil {
		logger.Error("[ERROR] Unable to list chains", logger.Fields{
			"err": err2,
		})
		return false, err2
	}
	logger.Info("Obtained chains", logger.Fields{
		"chains": strings.Join(chains, " "),
	})

	return exist, err
}

func (h *FaultHandler) stopBlackHoldPortFault(request types.NetworkBlackholePortRequest, chain, netNs string) (bool, error) {
	// Lock the OS thread so we don't accidentally switch namespaces
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()

	originalNS, err := h.netNsWrapper.Get()
	if err != nil {
		return false, err
	}

	logger.Info("Original namespace", logger.Fields{
		"orignalNS": originalNS,
	})
	defer h.netNsWrapper.CloseHandle(&originalNS)

	if netNs != "host" {
		taskNS, err := h.netNsWrapper.GetFromPath(netNs)
		if err != nil {
			return false, err
		}
		logger.Info("Task ENI namespace", logger.Fields{
			"taskNS": taskNS,
		})
		defer h.netNsWrapper.CloseHandle(&taskNS)

		err = h.netNsWrapper.Set(taskNS)
		if err != nil {
			return false, err
		}
		logger.Info("Task Network NS has been set")
	}
	chains, err := h.iptablesWrapper.ListChains()
	if err != nil {
		logger.Error("[ERROR] Unable to list chains", logger.Fields{
			"err": err,
		})
	}
	logger.Info("Obtained chains", logger.Fields{
		"chains": strings.Join(chains, " "),
	})

	exist, err := h.iptablesWrapper.Exists(chain, *request.Protocol, *request.Port)
	logger.Info("[DEBUG] Checked status of running black hole port", logger.Fields{
		"exists":    exist,
		"error":     err,
		"chainName": chain,
		"netNs":     netNs,
		"port":      *request.Port,
		"protocol":  *request.Protocol,
	})

	err = h.iptablesWrapper.ClearChain(chain)
	if err != nil {
		return false, err
	}
	logger.Info("Cleared chain", logger.Fields{
		"chain": chain,
	})

	insertChain := "OUTPUT"
	if *request.TrafficType == "ingress" {
		insertChain = "INPUT"
	}
	err = h.iptablesWrapper.Delete(chain, insertChain)
	if err != nil {
		return false, err
	}
	logger.Info("Deleted built in iptables chain", logger.Fields{
		"chain":       chain,
		"insertChain": insertChain,
	})

	err = h.iptablesWrapper.DeleteChain(chain)
	if err != nil {
		return false, err
	}
	logger.Info("Deleted chain", logger.Fields{
		"chain": chain,
	})

	exist, err = h.iptablesWrapper.Exists(chain, *request.Protocol, *request.Port)
	logger.Info("[DEBUG] Checked status of running black hole port", logger.Fields{
		"exists":    exist,
		"error":     err,
		"chainName": chain,
		"netNs":     netNs,
		"port":      *request.Port,
		"protocol":  *request.Protocol,
	})

	if netNs != "host" {
		h.netNsWrapper.Set(originalNS)
		logger.Info("Going back to the orignal NS")
	}

	chains, err2 := h.iptablesWrapper.ListChains()
	if err2 != nil {
		logger.Error("[ERROR] Unable to list chains", logger.Fields{
			"err": err2,
		})
		return false, err2
	}
	logger.Info("Obtained chains", logger.Fields{
		"chains": strings.Join(chains, " "),
	})

	exist2, err2 := h.iptablesWrapper.Exists(chain, *request.Protocol, *request.Port)
	logger.Info("[DEBUG] Checked status of running black hole port", logger.Fields{
		"exists":    exist2,
		"error":     err,
		"chainName": chain,
		"netNs":     netNs,
		"port":      *request.Port,
		"protocol":  *request.Protocol,
		"err":       err2,
	})

	return exist, err

}

func (h *FaultHandler) checkStatusNetworkBlackholePort(request types.NetworkBlackholePortRequest, chain, netNs string) (bool, error) {

	// Lock the OS thread so we don't accidentally switch namespaces
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()

	originalNS, err := h.netNsWrapper.Get()
	if err != nil {
		return false, err
	}

	logger.Info("Original namespace", logger.Fields{
		"orignalNS": originalNS,
	})
	defer h.netNsWrapper.CloseHandle(&originalNS)

	if netNs != "host" {
		taskNS, err := h.netNsWrapper.GetFromPath(netNs)
		if err != nil {
			return false, err
		}
		logger.Info("Task ENI namespace", logger.Fields{
			"taskNS": taskNS,
		})
		defer h.netNsWrapper.CloseHandle(&taskNS)

		err = h.netNsWrapper.Set(taskNS)
		if err != nil {
			return false, err
		}
		logger.Info("Task Network NS has been set")
	}
	ifaces, err := net.Interfaces()
	if err != nil {
		return false, err
	}
	for _, iface := range ifaces {
		logger.Info("[DEBUG] Obtained task network interface", logger.Fields{
			"interfaces": iface.Name,
		})
	}

	exist, err := h.iptablesWrapper.Exists(chain, *request.Protocol, *request.Port)
	logger.Info("[DEBUG] Checked status of running black hole port", logger.Fields{
		"exists":    exist,
		"error":     err,
		"chainName": chain,
		"netNs":     netNs,
		"port":      *request.Port,
		"protocol":  *request.Protocol,
	})

	chains, err := h.iptablesWrapper.ListChains()
	if err != nil {
		logger.Error("[ERROR] Unable to list chains", logger.Fields{
			"err": err,
		})
		return false, err
	}
	logger.Info("Obtained chains", logger.Fields{
		"chains": strings.Join(chains, " "),
	})

	ifaces, err = net.Interfaces()
	if err != nil {
		return false, err
	}
	for _, iface := range ifaces {
		logger.Info("[DEBUG] Obtained task network interface", logger.Fields{
			"interfaces": iface.Name,
		})
	}

	if netNs != "host" {
		h.netNsWrapper.Set(originalNS)
		logger.Info("Going back to the orignal NS")
	}
	ifaces, err = net.Interfaces()
	if err != nil {
		return false, err
	}
	for _, iface := range ifaces {
		logger.Info("[DEBUG] Obtained task network interface", logger.Fields{
			"interfaces": iface.Name,
		})
	}

	exist2, err := h.iptablesWrapper.Exists(chain, *request.Protocol, *request.Port)
	logger.Info("[DEBUG] NOW SHOULD BE IN HOST NETNS Checked status of running black hole port", logger.Fields{
		"exists":    exist2,
		"error":     err,
		"chainName": chain,
		"netNs":     netNs,
		"port":      *request.Port,
		"protocol":  *request.Protocol,
	})
	chains, err = h.iptablesWrapper.ListChains()
	if err != nil {
		logger.Error("[ERROR] Unable to list chains", logger.Fields{
			"err": err,
		})
		return false, err
	}
	logger.Info("Obtained chains", logger.Fields{
		"chains": strings.Join(chains, " "),
	})

	return exist, err
}

func (h *FaultHandler) startBlackHolePortFault2(request types.NetworkBlackholePortRequest, chain, netNs string) (bool, error) {
	ctx := context.Background()
	ctxWithTimeout, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()

	nsenterCmd := ""
	if netNs != "host" {
		nsenterCmd = fmt.Sprintf(nsenterCommandString, netNs)
	}

	cmdList := []string{"iptables", "-N", chain}
	if nsenterCmd != "" {
		cmdList = append(strings.Split(nsenterCmd, " "), cmdList...)
	}

	cmd := exec.CommandContext(ctxWithTimeout, cmdList[0], cmdList[1:]...)
	stdErr := &bytes.Buffer{}
	stdOut := &bytes.Buffer{}
	cmd.Stderr = stdErr
	cmd.Stdout = stdOut

	err := cmd.Run()

	if err != nil {
		logger.Error("Unable to run command", logger.Fields{
			"netns":   netNs,
			"command": strings.Join(cmdList, " "),
			"stdErr":  stdErr.String(),
			"stdOut":  stdOut.String(),
			"err":     err,
		})
		return false, err
	}

	logger.Info("Successfully created new chain", logger.Fields{
		"netns":   netNs,
		"command": strings.Join(cmdList, " "),
		"stdErr":  stdErr.String(),
		"stdOut":  stdOut.String(),
		"chain":   chain,
	})

	cmdList = []string{"iptables", "-A", chain, "-p", *request.Protocol, "--dport", strconv.FormatUint(uint64(*request.Port), 10), "-j", "DROP"}
	if nsenterCmd != "" {
		cmdList = append(strings.Split(nsenterCmd, " "), cmdList...)
	}
	cmd = exec.CommandContext(ctxWithTimeout, cmdList[0], cmdList[1:]...)
	stdErr = &bytes.Buffer{}
	stdOut = &bytes.Buffer{}
	cmd.Stderr = stdErr
	cmd.Stdout = stdOut

	err = cmd.Run()

	if err != nil {
		logger.Error("Unable to run command", logger.Fields{
			"netns":   netNs,
			"command": strings.Join(cmdList, " "),
			"stdErr":  stdErr.String(),
			"stdOut":  stdOut.String(),
			"err":     err,
		})
		return false, err
	}

	logger.Info("Successfully added new rule to chain", logger.Fields{
		"netns":   netNs,
		"command": strings.Join(cmdList, " "),
		"stdErr":  stdErr.String(),
		"stdOut":  stdOut.String(),
	})

	insertChain := "INPUT"
	if *request.TrafficType == "egress" {
		insertChain = "OUTPUT"
	}

	cmdList = []string{"iptables", "-I", insertChain, "-j", chain}
	if nsenterCmd != "" {
		cmdList = append(strings.Split(nsenterCmd, " "), cmdList...)
	}
	cmd = exec.CommandContext(ctxWithTimeout, cmdList[0], cmdList[1:]...)
	stdErr = &bytes.Buffer{}
	stdOut = &bytes.Buffer{}
	cmd.Stderr = stdErr
	cmd.Stdout = stdOut

	err = cmd.Run()

	if err != nil {
		logger.Error("Unable to run command", logger.Fields{
			"netns":   netNs,
			"command": strings.Join(cmdList, " "),
			"stdErr":  stdErr.String(),
			"stdOut":  stdOut.String(),
			"err":     err,
		})
		return false, err
	}
	logger.Info("Successfully inserted chain to built in iptables chain", logger.Fields{
		"netns":       netNs,
		"command":     strings.Join(cmdList, " "),
		"stdErr":      stdErr.String(),
		"stdOut":      stdOut.String(),
		"insertChain": insertChain,
	})

	cmdList = []string{"iptables", "-C", chain, "-p", *request.Protocol, "--dport", strconv.FormatUint(uint64(*request.Port), 10), "-j", "DROP"}
	if nsenterCmd != "" {
		cmdList = append(strings.Split(nsenterCmd, " "), cmdList...)
	}
	cmd = exec.CommandContext(ctxWithTimeout, cmdList[0], cmdList[1:]...)
	// cmd = exec.CommandContext(ctxWithTimeout, "/bin/sh", "-c", strings.Join(cmdList, " "))
	stdErr = &bytes.Buffer{}
	stdOut = &bytes.Buffer{}
	cmd.Stderr = stdErr
	cmd.Stdout = stdOut

	err = cmd.Run()
	if err != nil {
		logger.Error("Unable to run command", logger.Fields{
			"netns":   netNs,
			"command": strings.Join(cmdList, " "),
			"stdErr":  stdErr.String(),
			"stdOut":  stdOut.String(),
			"err":     err,
		})
		return false, err
	}

	logger.Info("Successfully ran the command", logger.Fields{
		"netns":   netNs,
		"command": strings.Join(cmdList, " "),
		"stdErr":  stdErr.String(),
		"stdOut":  stdOut.String(),
		"err":     err,
	})

	return true, nil
}

func (h *FaultHandler) stopBlackHoldPortFault2(request types.NetworkBlackholePortRequest, chain, netNs string) (bool, error) {
	ctx := context.Background()
	ctxWithTimeout, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()

	nsenterCmd := ""
	if netNs != "host" {
		nsenterCmd = fmt.Sprintf(nsenterCommandString, netNs)
	}

	cmdList := []string{"iptables", "-F", chain}
	if nsenterCmd != "" {
		cmdList = append(strings.Split(nsenterCmd, " "), cmdList...)
	}

	cmd := exec.CommandContext(ctxWithTimeout, cmdList[0], cmdList[1:]...)
	stdErr := &bytes.Buffer{}
	stdOut := &bytes.Buffer{}
	cmd.Stderr = stdErr
	cmd.Stdout = stdOut

	err := cmd.Run()

	if err != nil {
		logger.Error("Unable to run command", logger.Fields{
			"netns":   netNs,
			"command": strings.Join(cmdList, " "),
			"stdErr":  stdErr.String(),
			"stdOut":  stdOut.String(),
			"err":     err,
		})
		return false, err
	}
	logger.Info("Successfully cleared chain", logger.Fields{
		"netns":   netNs,
		"command": strings.Join(cmdList, " "),
		"stdErr":  stdErr.String(),
		"stdOut":  stdOut.String(),
		"chain":   chain,
	})

	insertChain := "INPUT"
	if *request.TrafficType == "egress" {
		insertChain = "OUTPUT"
	}

	cmdList = []string{"iptables", "-D", insertChain, "-j", chain}
	if nsenterCmd != "" {
		cmdList = append(strings.Split(nsenterCmd, " "), cmdList...)
	}
	cmd = exec.CommandContext(ctxWithTimeout, cmdList[0], cmdList[1:]...)
	stdErr = &bytes.Buffer{}
	stdOut = &bytes.Buffer{}
	cmd.Stderr = stdErr
	cmd.Stdout = stdOut

	err = cmd.Run()

	if err != nil {
		logger.Error("Unable to run command", logger.Fields{
			"netns":   netNs,
			"command": strings.Join(cmdList, " "),
			"stdErr":  stdErr.String(),
			"stdOut":  stdOut.String(),
			"err":     err,
		})
		return false, err
	}
	logger.Info("Successfully deleted chain from builtin iptables chain", logger.Fields{
		"netns":       netNs,
		"command":     strings.Join(cmdList, " "),
		"stdErr":      stdErr.String(),
		"stdOut":      stdOut.String(),
		"insertChain": insertChain,
		"chain":       chain,
	})

	cmdList = []string{"iptables", "-X", chain}
	if nsenterCmd != "" {
		cmdList = append(strings.Split(nsenterCmd, " "), cmdList...)
	}
	cmd = exec.CommandContext(ctxWithTimeout, cmdList[0], cmdList[1:]...)
	stdErr = &bytes.Buffer{}
	stdOut = &bytes.Buffer{}
	cmd.Stderr = stdErr
	cmd.Stdout = stdOut

	err = cmd.Run()

	if err != nil {
		logger.Error("Unable to run command", logger.Fields{
			"netns":   netNs,
			"command": strings.Join(cmdList, " "),
			"stdErr":  stdErr.String(),
			"stdOut":  stdOut.String(),
			"err":     err,
		})
		return false, err
	}
	logger.Info("Successfully deleted chain", logger.Fields{
		"netns":       netNs,
		"command":     strings.Join(cmdList, " "),
		"stdErr":      stdErr.String(),
		"stdOut":      stdOut.String(),
		"insertChain": insertChain,
		"chain":       chain,
	})

	cmdList = []string{"iptables", "-C", chain, "-p", *request.Protocol, "--dport", strconv.FormatUint(uint64(*request.Port), 10), "-j", "DROP"}
	if nsenterCmd != "" {
		cmdList = append(strings.Split(nsenterCmd, " "), cmdList...)
	}
	cmd = exec.CommandContext(ctxWithTimeout, cmdList[0], cmdList[1:]...)
	// cmd = exec.CommandContext(ctxWithTimeout, "/bin/sh", "-c", strings.Join(cmdList, " "))
	stdErr = &bytes.Buffer{}
	stdOut = &bytes.Buffer{}
	cmd.Stderr = stdErr
	cmd.Stdout = stdOut

	err = cmd.Run()
	if err != nil {
		logger.Error("Unable to run command", logger.Fields{
			"netns":   netNs,
			"command": strings.Join(cmdList, " "),
			"stdErr":  stdErr.String(),
			"stdOut":  stdOut.String(),
			"err":     err,
		})
		return false, err
	} else if strings.HasPrefix(stdErr.String(), "iptables: Bad rule (does a matching rule exist in that chain?).") {
		return true, nil
	} else {
		logger.Error("Black hole port is still running", logger.Fields{
			"netns":   netNs,
			"command": strings.Join(cmdList, " "),
			"stdErr":  stdErr.String(),
			"stdOut":  stdOut.String(),
			"err":     err,
		})
		return false, nil
	}
}

func (h *FaultHandler) checkStatusNetworkBlackholePort2(request types.NetworkBlackholePortRequest, chain, netNs string) (bool, error) {
	ctx := context.Background()
	ctxWithTimeout, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	cmdList := []string{"iptables", "-C", chain, "-p", *request.Protocol, "--dport", strconv.FormatUint(uint64(*request.Port), 10), "-j", "DROP"}

	if netNs != "host" {
		cmdList = append(strings.Split(fmt.Sprintf(nsenterCommandString, netNs), " "), cmdList...)
	}

	cmd := exec.CommandContext(ctxWithTimeout, cmdList[0], cmdList[1:]...)

	stdErr := &bytes.Buffer{}
	stdOut := &bytes.Buffer{}
	cmd.Stderr = stdErr
	cmd.Stdout = stdOut

	err := cmd.Run()

	if err != nil {
		logger.Error("Unable to run command", logger.Fields{
			"netns":   netNs,
			"command": strings.Join(cmdList, " "),
			"stdErr":  stdErr.String(),
			"stdOut":  stdOut.String(),
			"err":     err,
		})
		return false, err
	}

	logger.Info("Successfully ran the command", logger.Fields{
		"netns":   netNs,
		"command": strings.Join(cmdList, " "),
		"stdErr":  stdErr.String(),
		"stdOut":  stdOut.String(),
		"err":     err,
	})
	return true, nil
}
