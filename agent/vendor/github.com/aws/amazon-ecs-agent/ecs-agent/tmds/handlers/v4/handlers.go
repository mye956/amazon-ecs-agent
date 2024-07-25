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
package v4

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"net/http"
	"os/exec"
	"strings"
	"time"

	"github.com/aws/amazon-ecs-agent/ecs-agent/logger"
	"github.com/aws/amazon-ecs-agent/ecs-agent/logger/field"
	"github.com/aws/amazon-ecs-agent/ecs-agent/metrics"
	"github.com/aws/amazon-ecs-agent/ecs-agent/tmds/handlers/utils"
	state "github.com/aws/amazon-ecs-agent/ecs-agent/tmds/handlers/v4/state"

	"github.com/gorilla/mux"
)

// v3EndpointIDMuxName is the key that's used in gorilla/mux to get the v3 endpoint ID.
const (
	EndpointContainerIDMuxName = "endpointContainerIDMuxName"
	version                    = "v4"
	containerStatsErrorPrefix  = "V4 container stats handler"
	taskStatsErrorPrefix       = "V4 task stats handler"
)

var (
	//used to determine when to start/stop
	requestCount = 0
)

// ContainerMetadataPath specifies the relative URI path for serving container metadata.
func ContainerMetadataPath() string {
	return "/v4/" + utils.ConstructMuxVar(EndpointContainerIDMuxName, utils.AnythingButSlashRegEx)
}

// Returns the standard URI path for task metadata endpoint.
func TaskMetadataPath() string {
	return fmt.Sprintf(
		"/v4/%s/task",
		utils.ConstructMuxVar(EndpointContainerIDMuxName, utils.AnythingButSlashRegEx))
}

// Returns the standard URI path for task metadata with tags endpoint.
func TaskMetadataWithTagsPath() string {
	return fmt.Sprintf(
		"/v4/%s/taskWithTags",
		utils.ConstructMuxVar(EndpointContainerIDMuxName, utils.AnythingButSlashRegEx))
}

// Returns a standard URI path for v4 container stats endpoint.
func ContainerStatsPath() string {
	return fmt.Sprintf("/v4/%s/stats",
		utils.ConstructMuxVar(EndpointContainerIDMuxName, utils.AnythingButSlashRegEx))
}

// Returns a standard URI path for v4 task stats endpoint.
func TaskStatsPath() string {
	return fmt.Sprintf("/v4/%s/task/stats",
		utils.ConstructMuxVar(EndpointContainerIDMuxName, utils.AnythingButSlashRegEx))
}

func FISBlackholeFaultPath() string {
	return fmt.Sprintf("/v4/%s/fis",
		utils.ConstructMuxVar(EndpointContainerIDMuxName, utils.AnythingButSlashRegEx))
}

// ContainerMetadataHandler returns the HTTP handler function for handling container metadata requests.
func ContainerMetadataHandler(
	agentState state.AgentState,
	metricsFactory metrics.EntryFactory,
) func(http.ResponseWriter, *http.Request) {
	return func(w http.ResponseWriter, r *http.Request) {
		endpointContainerID := mux.Vars(r)[EndpointContainerIDMuxName]
		containerMetadata, err := agentState.GetContainerMetadata(endpointContainerID)
		if err != nil {
			logger.Error("Failed to get v4 container metadata", logger.Fields{
				field.TMDSEndpointContainerID: endpointContainerID,
				field.Error:                   err,
			})

			responseCode, responseBody := getContainerErrorResponse(endpointContainerID, err)
			utils.WriteJSONResponse(w, responseCode, responseBody, utils.RequestTypeContainerMetadata)

			if utils.Is5XXStatus(responseCode) {
				metricsFactory.New(metrics.InternalServerErrorMetricName).Done(err)
			}

			return
		}

		logger.Info("Writing response for v4 container metadata", logger.Fields{
			field.TMDSEndpointContainerID: endpointContainerID,
			field.Container:               containerMetadata.ID,
		})
		utils.WriteJSONResponse(w, http.StatusOK, containerMetadata, utils.RequestTypeContainerMetadata)
	}
}

// Returns an appropriate HTTP response status code and body for the error.
func getContainerErrorResponse(endpointContainerID string, err error) (int, string) {
	var errLookupFailure *state.ErrorLookupFailure
	if errors.As(err, &errLookupFailure) {
		return http.StatusNotFound, fmt.Sprintf("V4 container metadata handler: %s",
			errLookupFailure.ExternalReason())
	}

	var errMetadataFetchFailure *state.ErrorMetadataFetchFailure
	if errors.As(err, &errMetadataFetchFailure) {
		return http.StatusInternalServerError, errMetadataFetchFailure.ExternalReason()
	}

	logger.Error("Unknown error encountered when handling container metadata fetch failure",
		logger.Fields{field.Error: err})
	return http.StatusInternalServerError, "failed to get container metadata"
}

// TaskMetadataHandler returns the HTTP handler function for handling task metadata requests.
func TaskMetadataHandler(
	agentState state.AgentState,
	metricsFactory metrics.EntryFactory,
) func(http.ResponseWriter, *http.Request) {
	return taskMetadataHandler(agentState, metricsFactory, false)
}

// TaskMetadataHandler returns the HTTP handler function for handling task metadata with tags requests.
func TaskMetadataWithTagsHandler(
	agentState state.AgentState,
	metricsFactory metrics.EntryFactory,
) func(http.ResponseWriter, *http.Request) {
	return taskMetadataHandler(agentState, metricsFactory, true)
}

func taskMetadataHandler(
	agentState state.AgentState,
	metricsFactory metrics.EntryFactory,
	includeTags bool,
) func(http.ResponseWriter, *http.Request) {
	return func(w http.ResponseWriter, r *http.Request) {
		endpointContainerID := mux.Vars(r)[EndpointContainerIDMuxName]
		var taskMetadata state.TaskResponse
		var err error
		if includeTags {
			taskMetadata, err = agentState.GetTaskMetadataWithTags(endpointContainerID)
		} else {
			taskMetadata, err = agentState.GetTaskMetadata(endpointContainerID)
		}
		if err != nil {
			logger.Error("Failed to get v4 task metadata", logger.Fields{
				field.TMDSEndpointContainerID: endpointContainerID,
				field.Error:                   err,
			})

			responseCode, responseBody := getTaskErrorResponse(endpointContainerID, err)
			utils.WriteJSONResponse(w, responseCode, responseBody, utils.RequestTypeTaskMetadata)

			if utils.Is5XXStatus(responseCode) {
				metricsFactory.New(metrics.InternalServerErrorMetricName).Done(err)
			}

			return
		}

		logger.Info("Writing response for v4 task metadata", logger.Fields{
			field.TMDSEndpointContainerID: endpointContainerID,
			field.TaskARN:                 taskMetadata.TaskARN,
		})
		utils.WriteJSONResponse(w, http.StatusOK, taskMetadata, utils.RequestTypeTaskMetadata)
	}
}

// Returns an appropriate HTTP response status code and body for the task metadata error.
func getTaskErrorResponse(endpointContainerID string, err error) (int, string) {
	var errContainerLookupFailed *state.ErrorLookupFailure
	if errors.As(err, &errContainerLookupFailed) {
		return http.StatusNotFound, fmt.Sprintf("V4 task metadata handler: %s",
			errContainerLookupFailed.ExternalReason())
	}

	var errFailedToGetContainerMetadata *state.ErrorMetadataFetchFailure
	if errors.As(err, &errFailedToGetContainerMetadata) {
		return http.StatusInternalServerError, errFailedToGetContainerMetadata.ExternalReason()
	}

	logger.Error("Unknown error encountered when handling task metadata fetch failure", logger.Fields{
		field.Error: err,
	})
	return http.StatusInternalServerError, "failed to get task metadata"
}

// Returns an HTTP handler for v4 container stats endpoint
func ContainerStatsHandler(
	agentState state.AgentState,
	metricsFactory metrics.EntryFactory,
) func(http.ResponseWriter, *http.Request) {
	return statsHandler(agentState.GetContainerStats, metricsFactory,
		utils.RequestTypeContainerStats, containerStatsErrorPrefix)
}

// Returns an HTTP handler for v4 task stats endpoint
func TaskStatsHandler(
	agentState state.AgentState,
	metricsFactory metrics.EntryFactory,
) func(http.ResponseWriter, *http.Request) {
	return statsHandler(agentState.GetTaskStats, metricsFactory,
		utils.RequestTypeTaskStats, taskStatsErrorPrefix)
}

// Generic function that returns an HTTP handler for container or task stats endpoint
// depending on the parameters.
func statsHandler[R state.StatsResponse | map[string]*state.StatsResponse](
	getStats func(string) (R, error), // container stats or task stats getter function
	metricsFactory metrics.EntryFactory,
	requestType string, // container stats or task stats request type
	errorPrefix string,
) func(http.ResponseWriter, *http.Request) {
	return func(w http.ResponseWriter, r *http.Request) {
		// Extract endpoint container ID
		endpointContainerID := mux.Vars(r)[EndpointContainerIDMuxName]

		// Get stats
		stats, err := getStats(endpointContainerID)
		if err != nil {
			logger.Error("Failed to get v4 stats", logger.Fields{
				field.TMDSEndpointContainerID: endpointContainerID,
				field.Error:                   err,
				field.RequestType:             requestType,
			})

			responseCode, responseBody := getStatsErrorResponse(endpointContainerID, err, errorPrefix)
			utils.WriteJSONResponse(w, responseCode, responseBody, requestType)

			if utils.Is5XXStatus(responseCode) {
				metricsFactory.New(metrics.InternalServerErrorMetricName).Done(err)
			}

			return
		}

		// Write stats response
		logger.Info("Writing response for v4 stats", logger.Fields{
			field.TMDSEndpointContainerID: endpointContainerID,
			field.RequestType:             requestType,
		})
		utils.WriteJSONResponse(w, http.StatusOK, stats, requestType)
	}
}

// Returns appropriate HTTP status code and response body for stats endpoint error cases.
func getStatsErrorResponse(endpointContainerID string, err error, errorPrefix string) (int, string) {
	// 404 if lookup failure
	var errLookupFailure *state.ErrorStatsLookupFailure
	if errors.As(err, &errLookupFailure) {
		return http.StatusNotFound, fmt.Sprintf(
			"%s: %s", errorPrefix, errLookupFailure.ExternalReason())
	}

	// 500 if any other known failure
	var errStatsFetchFailure *state.ErrorStatsFetchFailure
	if errors.As(err, &errStatsFetchFailure) {
		return http.StatusInternalServerError, errStatsFetchFailure.ExternalReason()
	}

	// 500 if unknown failure
	logger.Error("Unknown error encountered when handling stats fetch error", logger.Fields{
		field.Error: err,
	})
	return http.StatusInternalServerError, "failed to get stats"
}

func FISBlackHoleHandler(
	agentState state.AgentState,
	metricsFactory metrics.EntryFactory,
) func(http.ResponseWriter, *http.Request) {
	return func(w http.ResponseWriter, r *http.Request) {
		endpointContainerID := mux.Vars(r)[EndpointContainerIDMuxName]
		containerMetadata, err := agentState.GetContainerMetadata(endpointContainerID)
		if err != nil {
			logger.Error("Failed to get v4 container metadata", logger.Fields{
				field.TMDSEndpointContainerID: endpointContainerID,
				field.Error:                   err,
			})

			responseCode, responseBody := getContainerErrorResponse(endpointContainerID, err)
			utils.WriteJSONResponse(w, responseCode, responseBody, utils.RequestTypeContainerMetadata)

			if utils.Is5XXStatus(responseCode) {
				metricsFactory.New(metrics.InternalServerErrorMetricName).Done(err)
			}

			return
		}

		taskMetadata, err := agentState.GetTaskMetadata(endpointContainerID)
		if err != nil {
			logger.Error("error not able to get task metadata", logger.Fields{
				"error":               err,
				"endpointContainerId": endpointContainerID,
			})
		}
		
		if taskMetadata.Netns != "" {
			if requestCount%2 == 0 {
				res, err := startFault(taskMetadata)
				if err != nil {
					logger.Error("Unable to start fault", logger.Fields{
						"err": err,
					})
				}
				logger.Info("Successfully started fault", logger.Fields{
					"output": res,
				})
			} else {
				res, err := stopFault(taskMetadata)
				if err != nil {
					logger.Error("Unable to stop fault", logger.Fields{
						"err": err,
					})
				}
				logger.Info("Successfully stopped fault", logger.Fields{
					"output": res,
				})
			}
			res, err := checkBlackHoleFault(taskMetadata)
			if err != nil {
				logger.Error("Unable to check fault status", logger.Fields{
					"err": err,
				})
			}
			logger.Info("Status of fault", logger.Fields{
				"output": res,
			})
		} else {
			logger.Warn("Task Network namespace is not set")
		}

		logger.Info("Writing response for v4 container metadata", logger.Fields{
			field.TMDSEndpointContainerID: endpointContainerID,
			field.Container:               containerMetadata.ID,
		})
		requestCount++
		utils.WriteJSONResponse(w, http.StatusOK, containerMetadata, utils.RequestTypeContainerMetadata)
	}
}

func startFault(taskMetadata state.TaskResponse) (string, error) {
	chainName := "fault-in"
	protocol := "tcp"
	port := "1234"
	iptablesInsert := "IPTABLES_INSERT"

	ctx := context.Background()
	ctxWithTimeout, cancel := context.WithDeadline(ctx, time.Now().Add(time.Second*5))
	defer cancel()

	cmdName := []string{"nsenter"}
	netNsArg := "--net="+taskMetadata.Netns
	// cmdList := []string{"nsenter", "--net="+taskMetadata.Netns}
	// parameterString := "/faults/network_blackhole_port_start.sh --port 80 --protocol tcp ingress --assertion-script-path assertion-script.sh"
	// parameterList := strings.Split(parameterString, " ")

	parameterList := []string{netNsArg, "iptables", "-N", chainName}
	// cmdList = append(cmdList, parameterList...)
	parameterList = append(cmdName, parameterList...)
	cmd := exec.CommandContext(ctxWithTimeout, parameterList[0], parameterList[1:]...)

	stdErr := &bytes.Buffer{}
	stdOut := &bytes.Buffer{}
	cmd.Stderr = stdErr
	cmd.Stdout = stdOut

	err := cmd.Run()

	if err != nil {
		logger.Error("Can't run command", logger.Fields{
			"pid":     taskMetadata.PauseContainerPid,
			"netns":   taskMetadata.Netns,
			"command":  strings.Join(parameterList, " "),
			"stdErr":  stdErr.String(),
			"stdOut":  stdOut.String(),
			"err":     err,
		})
		return "", err
	}

	logger.Debug("Running command", logger.Fields{
		"pid":     taskMetadata.PauseContainerPid,
		"netns":   taskMetadata.Netns,
		"command": strings.Join(parameterList, " "),
		"stdErr":  stdErr.String(),
		"stdOut":  stdOut.String(),
		"err":     err,
	})

	parameterList = []string{netNsArg, "iptables", "-A", chainName, "-p", protocol, "--dport", port, "-j", "DROP"}
	parameterList = append(cmdName, parameterList...)
	cmd = exec.CommandContext(ctxWithTimeout, parameterList[0], parameterList[1:]...)
	err = cmd.Run()
	if err != nil {
		logger.Error("Can't run command", logger.Fields{
			"pid":     taskMetadata.PauseContainerPid,
			"netns":   taskMetadata.Netns,
			"command": strings.Join(parameterList, " "),
			"stdErr":  stdErr.String(),
			"stdOut":  stdOut.String(),
			"err":     err,
		})
		return "", err
	}
	logger.Debug("Running command", logger.Fields{
		"pid":     taskMetadata.PauseContainerPid,
		"netns":   taskMetadata.Netns,
		"command": strings.Join(parameterList, " "),
		"stdErr":  stdErr.String(),
		"stdOut":  stdOut.String(),
		"err":     err,
	})

	parameterList = []string{netNsArg, "iptables", "-I", iptablesInsert, "-j", chainName}
	parameterList = append(cmdName, parameterList...)
	cmd = exec.CommandContext(ctxWithTimeout, parameterList[0], parameterList[1:]...)
	err = cmd.Run()
	if err != nil {
		logger.Error("Can't run command", logger.Fields{
			"pid":     taskMetadata.PauseContainerPid,
			"netns":   taskMetadata.Netns,
			"command": strings.Join(parameterList, " "),
			"stdErr":  stdErr.String(),
			"stdOut":  stdOut.String(),
			"err":     err,
		})
		return "", err
	}
	logger.Debug("Running command", logger.Fields{
		"pid":     taskMetadata.PauseContainerPid,
		"netns":   taskMetadata.Netns,
		"command": strings.Join(parameterList, " "),
		"stdErr":  stdErr.String(),
		"stdOut":  stdOut.String(),
		"err":     err,
	})

	return "fault started", nil

}

func stopFault(taskMetadata state.TaskResponse) (string, error) {
	chainName := "fault-in"
	iptablesInsert := "IPTABLES_INSERT"
	
	ctx := context.Background()
	ctxWithTimeout, cancel := context.WithDeadline(ctx, time.Now().Add(time.Second*5))
	defer cancel()

	cmdName := []string{"nsenter"}
	netNsArg := "--net="+taskMetadata.Netns
	parameterList := []string{netNsArg, "iptables", "-F", chainName}
	parameterList = append(cmdName, parameterList...)
	cmd := exec.CommandContext(ctxWithTimeout, parameterList[0], parameterList[1:]...)

	stdErr := &bytes.Buffer{}
	stdOut := &bytes.Buffer{}
	cmd.Stderr = stdErr
	cmd.Stdout = stdOut

	err := cmd.Run()

	if err != nil {
		logger.Error("Can't run command", logger.Fields{
			"pid":     taskMetadata.PauseContainerPid,
			"netns":   taskMetadata.Netns,
			"command": strings.Join(parameterList, " "),
			"stdErr":  stdErr.String(),
			"stdOut":  stdOut.String(),
			"err":     err,
		})
		return stdOut.String(), err
	}
	logger.Debug("Running command", logger.Fields{
		"pid":     taskMetadata.PauseContainerPid,
		"netns":   taskMetadata.Netns,
		"command": strings.Join(parameterList, " "),
		"stdErr":  stdErr.String(),
		"stdOut":  stdOut.String(),
		"err":     err,
	})

	parameterList = []string{netNsArg, "iptables", "-D", iptablesInsert, "-j", chainName}
	parameterList = append(cmdName, parameterList...)
	cmd = exec.CommandContext(ctxWithTimeout, parameterList[0], parameterList[1:]...)
	err = cmd.Run()
	if err != nil {
		logger.Error("Can't run command", logger.Fields{
			"pid":     taskMetadata.PauseContainerPid,
			"netns":   taskMetadata.Netns,
			"command": strings.Join(parameterList, " "),
			"stdErr":  stdErr.String(),
			"stdOut":  stdOut.String(),
			"err":     err,
		})
		return "", err
	}
	logger.Debug("Running command", logger.Fields{
		"pid":     taskMetadata.PauseContainerPid,
		"netns":   taskMetadata.Netns,
		"command": strings.Join(parameterList, " "),
		"stdErr":  stdErr.String(),
		"stdOut":  stdOut.String(),
		"err":     err,
	})

	parameterList = []string{netNsArg, "iptables", "-X", chainName}
	parameterList = append(cmdName, parameterList...)
	cmd = exec.CommandContext(ctxWithTimeout, parameterList[0], parameterList[1:]...)
	err = cmd.Run()
	if err != nil {
		logger.Error("Can't run command", logger.Fields{
			"pid":     taskMetadata.PauseContainerPid,
			"netns":   taskMetadata.Netns,
			"command": strings.Join(parameterList, " "),
			"stdErr":  stdErr.String(),
			"stdOut":  stdOut.String(),
			"err":     err,
		})
		return "", err
	}
	logger.Debug("Running command", logger.Fields{
		"pid":     taskMetadata.PauseContainerPid,
		"netns":   taskMetadata.Netns,
		"command": strings.Join(parameterList, " "),
		"stdErr":  stdErr.String(),
		"stdOut":  stdOut.String(),
		"err":     err,
	})

	return "success", nil

}

func checkBlackHoleFault(taskMetadata state.TaskResponse) (string, error) {
	ctx := context.Background()
	ctxWithTimeout, cancel := context.WithDeadline(ctx, time.Now().Add(time.Second*5))
	defer cancel()

	cmdList := []string{"nsenter", "--net="+taskMetadata.Netns}
	parameterString := "iptables -nL"
	parameterList := strings.Split(parameterString, " ")
	cmdList = append(cmdList, parameterList...)
	cmd := exec.CommandContext(ctxWithTimeout, cmdList[0], cmdList[1:]...)

	stdErr := &bytes.Buffer{}
	stdOut := &bytes.Buffer{}
	cmd.Stderr = stdErr
	cmd.Stdout = stdOut

	err := cmd.Run()

	if err != nil {
		logger.Error("Can't run command", logger.Fields{
			"pid":     taskMetadata.PauseContainerPid,
			"netns":   taskMetadata.Netns,
			"command": strings.Join(cmdList, " "),
			"stdErr":  stdErr.String(),
			"stdOut":  stdOut.String(),
			"err":     err,
		})
		return "", err
	}

	logger.Debug("Running command", logger.Fields{
		"pid":     taskMetadata.PauseContainerPid,
		"netns":   taskMetadata.Netns,
		"command": strings.Join(cmdList, " "),
		"stdErr":  stdErr.String(),
		"stdOut":  stdOut.String(),
		"err":     err,
	})

	return stdOut.String(), nil

}
