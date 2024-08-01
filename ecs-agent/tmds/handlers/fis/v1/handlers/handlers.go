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
	"encoding/json"
	"errors"
	"fmt"
	"net/http"

	"github.com/aws/amazon-ecs-agent/ecs-agent/logger"
	"github.com/aws/amazon-ecs-agent/ecs-agent/logger/field"
	"github.com/aws/amazon-ecs-agent/ecs-agent/metrics"
	"github.com/aws/amazon-ecs-agent/ecs-agent/tmds/handlers/fis/v1/types"
	"github.com/aws/amazon-ecs-agent/ecs-agent/tmds/handlers/utils"
	v4 "github.com/aws/amazon-ecs-agent/ecs-agent/tmds/handlers/v4"
	state "github.com/aws/amazon-ecs-agent/ecs-agent/tmds/handlers/v4/state"

	"github.com/gorilla/mux"
)

type handler struct {
	// TODO: Will be using this mutex in a future PR
	// mu             sync.Mutex
	agentState     state.AgentState
	metricsFactory metrics.EntryFactory
}

// RegisterFISHandlers will initialize all of the Start/Stop/Check fault injection endpoints and register them to the TMDS router
func RegisterFISHandlers(muxRouter *mux.Router, agentState state.AgentState, metricsFactory metrics.EntryFactory) {
	handler := handler{
		agentState:     agentState,
		metricsFactory: metricsFactory,
	}

	// Setting up handler endpoints for network blackhole port fault injections
	muxRouter.HandleFunc(
		FISNetworkFaultPath(types.BlackHolePortFaultType),
		handler.StartNetworkBlackholePortHandler(),
	).Methods("PUT")
	muxRouter.HandleFunc(
		FISNetworkFaultPath(types.BlackHolePortFaultType),
		handler.StopBlackHolePortHandler(),
	).Methods("DELETE")
	muxRouter.HandleFunc(
		FISNetworkFaultPath(types.BlackHolePortFaultType),
		handler.CheckBlackHolePortStatusHandler(),
	).Methods("GET")

	// Setting up handler endpoints for network latency fault injections
	muxRouter.HandleFunc(
		FISNetworkFaultPath(types.LatencyFaultType),
		handler.StartLatencyHandler(),
	).Methods("PUT")
	muxRouter.HandleFunc(
		FISNetworkFaultPath(types.LatencyFaultType),
		handler.StopLatencyHandler(),
	).Methods("DELETE")
	muxRouter.HandleFunc(
		FISNetworkFaultPath(types.LatencyFaultType),
		handler.CheckLatencyStatusHandler(),
	).Methods("GET")

	// Setting up handler endpoints for network packet loss fault injections
	muxRouter.HandleFunc(
		FISNetworkFaultPath(types.PacketLossFaultType),
		handler.StartPacketLossHandler(),
	).Methods("PUT")
	muxRouter.HandleFunc(
		FISNetworkFaultPath(types.PacketLossFaultType),
		handler.StopPacketLossHandler(),
	).Methods("DELETE")
	muxRouter.HandleFunc(
		FISNetworkFaultPath(types.PacketLossFaultType),
		handler.CheckPacketLossStatusHandler(),
	).Methods("GET")

	logger.Debug("FIS Handlers have been set up")

}

// FISNetworkFaultPath will take in a fault type and returns the TMDS endpoint path for the corresponding fault type
func FISNetworkFaultPath(fault string) string {
	return fmt.Sprintf("/api/%s/fis/v1/%s",
		utils.ConstructMuxVar(v4.EndpointContainerIDMuxName, utils.AnythingButSlashRegEx), fault)
}

// func FISBlackholeFaultPath() string {
// 	return fmt.Sprintf("/api/%s/fis/v1/network-blackhole-port",
// 		utils.ConstructMuxVar(v4.EndpointContainerIDMuxName, utils.AnythingButSlashRegEx))
// }
// func FISLatencyFaultPath() string {
// 	return fmt.Sprintf("/api/%s/fis/v1/network-latency",
// 		utils.ConstructMuxVar(v4.EndpointContainerIDMuxName, utils.AnythingButSlashRegEx))
// }
// func FISPacketLossFaultPath() string {
// 	return fmt.Sprintf("/api/%s/fis/v1/network-packet-loss",
// 		utils.ConstructMuxVar(v4.EndpointContainerIDMuxName, utils.AnythingButSlashRegEx))
// }

func (h *handler) StartNetworkBlackholePortHandler() func(http.ResponseWriter, *http.Request) {
	return func(w http.ResponseWriter, r *http.Request) {
		requestType := "start network blackhole port fault"

		var request types.NetworkBlackholePortRequest
		err := decodeRequest(&request, requestType, r)
		if err != nil {
			utils.WriteJSONResponse(
				w,
				http.StatusBadRequest,
				types.NetworkBlackHolePortResponse{
					Error: fmt.Sprintf("%v", err),
				},
				requestType,
			)
			return
		}

		logger.Debug("Successfully decoded request body", logger.Fields{
			"requestBody": request.ToString(),
		})

		endpointContainerID := mux.Vars(r)[v4.EndpointContainerIDMuxName]
		taskMetadata, err := h.agentState.GetTaskMetadata(endpointContainerID)
		if err != nil {
			logger.Error("Error: unable to obtain task metadata", logger.Fields{
				field.Error:       err,
				field.RequestType: requestType,
			})
			responseCode, responseError := getTaskMetadataErrorResponse(endpointContainerID, requestType, err)
			utils.WriteJSONResponse(
				w,
				responseCode,
				types.NetworkBlackHolePortResponse{
					Error: responseError,
				},
				requestType,
			)
			return
		}

		logger.Debug("Obtained task metadata", logger.Fields{
			"Task metadata": taskMetadata,
		})

		// TODO: Add check if task is FIS-enabled

		// TODO: Add check if fault is running already
		// TODO: Call start fault injection functionality

		utils.WriteJSONResponse(
			w,
			http.StatusOK,
			types.NetworkBlackHolePortResponse{
				Status: "running",
			},
			requestType,
		)
	}
}

func (h *handler) StopBlackHolePortHandler() func(http.ResponseWriter, *http.Request) {
	return func(w http.ResponseWriter, r *http.Request) {
		requestType := "stop network blackhole port fault"

		var request types.NetworkBlackholePortRequest
		err := decodeRequest(&request, requestType, r)
		if err != nil {
			utils.WriteJSONResponse(
				w,
				http.StatusBadRequest,
				types.NetworkBlackHolePortResponse{
					Error: "failed to decode request",
				},
				requestType,
			)
			return
		}
		logger.Debug("Successfully decoded request body", logger.Fields{
			"requestBody": request,
		})

		endpointContainerID := mux.Vars(r)[v4.EndpointContainerIDMuxName]
		taskMetadata, err := h.agentState.GetTaskMetadata(endpointContainerID)
		if err != nil {
			logger.Error("Error: unable to obtain task metadata", logger.Fields{
				field.Error:       err,
				field.RequestType: requestType,
			})
			responseCode, responseError := getTaskMetadataErrorResponse(endpointContainerID, requestType, err)
			utils.WriteJSONResponse(
				w,
				responseCode,
				types.NetworkBlackHolePortResponse{
					Error: responseError,
				},
				requestType,
			)
			return
		}

		logger.Debug("Obtained task metadata", logger.Fields{
			"Task metadata": taskMetadata,
		})

		// TODO: Add check if task is FIS-enabled

		// TODO: Add check if fault has stopped already
		// TODO: Call stop fault injection functionality

		utils.WriteJSONResponse(
			w,
			http.StatusOK,
			types.NetworkBlackHolePortResponse{
				Status: "stopped",
			},
			requestType,
		)
	}
}

func (h *handler) CheckBlackHolePortStatusHandler() func(http.ResponseWriter, *http.Request) {
	return func(w http.ResponseWriter, r *http.Request) {
		requestType := "check status network blackhole port fault"

		var request types.NetworkBlackholePortRequest
		err := decodeRequest(&request, requestType, r)
		if err != nil {
			utils.WriteJSONResponse(
				w,
				http.StatusBadRequest,
				types.NetworkBlackHolePortResponse{
					Error: fmt.Sprintf("%v", err),
				},
				requestType,
			)
			return
		}
		logger.Debug("Successfully decoded request body", logger.Fields{
			"requestBody": request,
		})

		endpointContainerID := mux.Vars(r)[v4.EndpointContainerIDMuxName]
		taskMetadata, err := h.agentState.GetTaskMetadata(endpointContainerID)
		if err != nil {
			logger.Error("Error: unable to obtain task metadata", logger.Fields{
				field.Error:       err,
				field.RequestType: requestType,
			})
			responseCode, responseError := getTaskMetadataErrorResponse(endpointContainerID, requestType, err)
			utils.WriteJSONResponse(
				w,
				responseCode,
				types.NetworkBlackHolePortResponse{
					Error: responseError,
				},
				requestType,
			)
			return
		}

		logger.Debug("Obtained task metadata", logger.Fields{
			"Task metadata": taskMetadata,
		})

		// TODO: Add check if task is FIS-enabled

		// TODO: Call check status fault injection functionality

		utils.WriteJSONResponse(
			w,
			http.StatusOK,
			types.NetworkBlackHolePortResponse{
				Status: "running",
			},
			requestType,
		)
	}
}

func (h *handler) StartLatencyHandler() func(http.ResponseWriter, *http.Request) {
	return func(w http.ResponseWriter, r *http.Request) {

	}
}

func (h *handler) StopLatencyHandler() func(http.ResponseWriter, *http.Request) {
	return func(w http.ResponseWriter, r *http.Request) {

	}
}

func (h *handler) CheckLatencyStatusHandler() func(http.ResponseWriter, *http.Request) {
	return func(w http.ResponseWriter, r *http.Request) {

	}
}

func (h *handler) StartPacketLossHandler() func(http.ResponseWriter, *http.Request) {
	return func(w http.ResponseWriter, r *http.Request) {

	}
}

func (h *handler) StopPacketLossHandler() func(http.ResponseWriter, *http.Request) {
	return func(w http.ResponseWriter, r *http.Request) {

	}
}

func (h *handler) CheckPacketLossStatusHandler() func(http.ResponseWriter, *http.Request) {
	return func(w http.ResponseWriter, r *http.Request) {

	}
}

func getTaskMetadataErrorResponse(endpointContainerID, requestType string, err error) (int, string) {
	var errContainerLookupFailed *state.ErrorLookupFailure
	if errors.As(err, &errContainerLookupFailed) {
		return http.StatusNotFound, fmt.Sprintf("unable to lookup container: %s", endpointContainerID)
	}

	var errFailedToGetContainerMetadata *state.ErrorMetadataFetchFailure
	if errors.As(err, &errFailedToGetContainerMetadata) {
		return http.StatusInternalServerError, fmt.Sprintf("unable to obtain container metadata for container: %s", endpointContainerID)
	}

	logger.Error("Unknown error encountered when handling task metadata fetch failure", logger.Fields{
		field.Error:       err,
		field.RequestType: requestType,
	})
	return http.StatusInternalServerError, fmt.Sprintf("failed to get task metadata due to internal server error for container: %s", endpointContainerID)
}

func decodeRequest(request types.NetworkFISRequestInterface, requestType string, r *http.Request) error {
	jsonDecoder := json.NewDecoder(r.Body)
	jsonDecoder.DisallowUnknownFields()
	if err := jsonDecoder.Decode(request); err != nil {
		logger.Error("Error: failed to decode request", logger.Fields{
			field.Error:   err,
			"requestType": requestType,
			"body":        request,
		})
		return err
	}

	if err := request.ValidateRequest(); err != nil {
		logger.Error("Error: missing required payload fields", logger.Fields{
			field.Error:   err,
			"requestType": requestType,
			"body":        request,
		})
		return err
	}

	return nil
}
