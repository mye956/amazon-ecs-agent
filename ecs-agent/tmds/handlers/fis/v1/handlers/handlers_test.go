//go:build unit
// +build unit

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
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	mock_metrics "github.com/aws/amazon-ecs-agent/ecs-agent/metrics/mocks"
	"github.com/aws/amazon-ecs-agent/ecs-agent/tmds/handlers/fis/v1/types"
	state "github.com/aws/amazon-ecs-agent/ecs-agent/tmds/handlers/v4/state"
	mock_state "github.com/aws/amazon-ecs-agent/ecs-agent/tmds/handlers/v4/state/mocks"
	"github.com/aws/aws-sdk-go/aws"

	"github.com/golang/mock/gomock"
	"github.com/gorilla/mux"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const (
	endpointId  = "endpointId"
	port        = 1234
	protocol    = "tcp"
	trafficType = "ingress"
)

// Tests the path for FIS Network Faults API
func TestFISBlackholeFaultPath(t *testing.T) {
	assert.Equal(t, "/api/{endpointContainerIDMuxName:[^/]*}/fis/v1/network-blackhole-port", FISNetworkFaultPath(types.BlackHolePortFaultType))
}

func TestFISLatencyFaultPath(t *testing.T) {
	assert.Equal(t, "/api/{endpointContainerIDMuxName:[^/]*}/fis/v1/network-latency", FISNetworkFaultPath(types.LatencyFaultType))
}

func TestFISPacketLossFaultPath(t *testing.T) {
	assert.Equal(t, "/api/{endpointContainerIDMuxName:[^/]*}/fis/v1/network-packet-loss", FISNetworkFaultPath(types.PacketLossFaultType))
}

func TestStartFISBlackHoleRequests(t *testing.T) {
	tcs := []struct {
		name                      string
		expectedStatusCode        int
		requestBody               interface{}
		expectedResponseBody      types.NetworkBlackHolePortResponse
		setAgentStateExpectations func(agentState *mock_state.MockAgentState)
	}{
		{
			name:               "start blackhole port success",
			expectedStatusCode: 200,
			requestBody: map[string]interface{}{
				"Port":        aws.Int(port),
				"Protocol":    aws.String(protocol),
				"TrafficType": aws.String(trafficType),
			},
			expectedResponseBody: types.NetworkBlackHolePortResponse{
				Status: "running",
			},
			setAgentStateExpectations: func(agentState *mock_state.MockAgentState) {
				agentState.EXPECT().GetTaskMetadata(endpointId).Return(state.TaskResponse{}, nil)
			},
		},
		{
			name:               "start blackhole port unknown request body",
			expectedStatusCode: 400,
			requestBody: map[string]interface{}{
				"Port":        aws.Int(port),
				"Protocol":    aws.String(protocol),
				"TrafficType": aws.String(trafficType),
				"Unknown":     "",
			},
			expectedResponseBody: types.NetworkBlackHolePortResponse{
				Error: "json: unknown field \"Unknown\"",
			},
			setAgentStateExpectations: func(agentState *mock_state.MockAgentState) {
				agentState.EXPECT().GetTaskMetadata(endpointId).Return(state.TaskResponse{}, nil).Times(0)
			},
		},
		{
			name:               "start blackhole port malformed request body",
			expectedStatusCode: 400,
			requestBody: map[string]interface{}{
				"Port":        aws.String("incorrect typing"),
				"Protocol":    aws.String(protocol),
				"TrafficType": aws.String(trafficType),
			},
			expectedResponseBody: types.NetworkBlackHolePortResponse{
				Error: "json: cannot unmarshal string into Go struct field NetworkBlackholePortRequest.Port of type int",
			},
			setAgentStateExpectations: func(agentState *mock_state.MockAgentState) {
				agentState.EXPECT().GetTaskMetadata(endpointId).Return(state.TaskResponse{}, nil).Times(0)
			},
		},
		{
			name:               "start blackhole port incomplete request body",
			expectedStatusCode: 400,
			requestBody: map[string]interface{}{
				"Port":     aws.Int(port),
				"Protocol": aws.String(protocol),
			},
			expectedResponseBody: types.NetworkBlackHolePortResponse{
				Error: "required parameter TrafficType is missing",
			},
			setAgentStateExpectations: func(agentState *mock_state.MockAgentState) {
				agentState.EXPECT().GetTaskMetadata(endpointId).Return(state.TaskResponse{}, nil).Times(0)
			},
		},
		{
			name:               "start blackhole port empty value request body",
			expectedStatusCode: 400,
			requestBody: map[string]interface{}{
				"Port":        aws.Int(port),
				"Protocol":    aws.String(protocol),
				"TrafficType": aws.String(""),
			},
			expectedResponseBody: types.NetworkBlackHolePortResponse{
				Error: "required parameter TrafficType is missing",
			},
			setAgentStateExpectations: func(agentState *mock_state.MockAgentState) {
				agentState.EXPECT().GetTaskMetadata(endpointId).Return(state.TaskResponse{}, nil).Times(0)
			},
		},
		{
			name:               "start blackhole port task lookup fail",
			expectedStatusCode: 404,
			requestBody: &types.NetworkBlackholePortRequest{
				Port:        aws.Int(port),
				Protocol:    aws.String(protocol),
				TrafficType: aws.String(trafficType),
			},
			expectedResponseBody: types.NetworkBlackHolePortResponse{
				Error: fmt.Sprintf("unable to lookup container: %s", endpointId),
			},
			setAgentStateExpectations: func(agentState *mock_state.MockAgentState) {
				agentState.EXPECT().GetTaskMetadata(endpointId).Return(state.TaskResponse{}, state.NewErrorLookupFailure(
					"unable to get task arn from request"))
			},
		},
		{
			name:               "start blackhole port task metadata fetch fail",
			expectedStatusCode: 500,
			requestBody: &types.NetworkBlackholePortRequest{
				Port:        aws.Int(port),
				Protocol:    aws.String(protocol),
				TrafficType: aws.String(trafficType),
			},
			expectedResponseBody: types.NetworkBlackHolePortResponse{
				Error: fmt.Sprintf("unable to obtain container metadata for container: %s", endpointId),
			},
			setAgentStateExpectations: func(agentState *mock_state.MockAgentState) {
				agentState.EXPECT().GetTaskMetadata(endpointId).Return(state.TaskResponse{}, state.NewErrorMetadataFetchFailure(
					"Unable to generate metadata for task"))
			},
		},
		{
			name:               "start blackhole port task metadata unknown fail",
			expectedStatusCode: 500,
			requestBody: &types.NetworkBlackholePortRequest{
				Port:        aws.Int(port),
				Protocol:    aws.String(protocol),
				TrafficType: aws.String(trafficType),
			},
			expectedResponseBody: types.NetworkBlackHolePortResponse{
				Error: fmt.Sprintf("failed to get task metadata due to internal server error for container: %s", endpointId),
			},
			setAgentStateExpectations: func(agentState *mock_state.MockAgentState) {
				agentState.EXPECT().GetTaskMetadata(endpointId).Return(state.TaskResponse{}, errors.New("unknown error"))
			},
		},
	}

	for _, tc := range tcs {
		t.Run(tc.name, func(t *testing.T) {
			// Mocks
			ctrl := gomock.NewController(t)
			defer ctrl.Finish()

			agentState := mock_state.NewMockAgentState(ctrl)
			metricsFactory := mock_metrics.NewMockEntryFactory(ctrl)

			if tc.setAgentStateExpectations != nil {
				tc.setAgentStateExpectations(agentState)
			}

			router := mux.NewRouter()

			RegisterFISHandlers(router, agentState, metricsFactory)

			method := "PUT"
			var requestBody io.Reader
			if tc.requestBody != nil {
				reqBodyBytes, err := json.Marshal(tc.requestBody)
				require.NoError(t, err)
				requestBody = bytes.NewReader(reqBodyBytes)
			}
			req, err := http.NewRequest(method, fmt.Sprintf("/api/%s/fis/v1/network-blackhole-port", endpointId),
				requestBody)
			require.NoError(t, err)

			// Send the request and record the response
			recorder := httptest.NewRecorder()
			router.ServeHTTP(recorder, req)

			// Parse the response body
			var actualResponseBody types.NetworkBlackHolePortResponse
			err = json.Unmarshal(recorder.Body.Bytes(), &actualResponseBody)
			require.NoError(t, err)

			assert.Equal(t, tc.expectedStatusCode, recorder.Code)
			assert.Equal(t, tc.expectedResponseBody, actualResponseBody)

		})
	}
}

func TestStopFISBlackholePortRequests(t *testing.T) {
	tcs := []struct {
		name                      string
		expectedStatusCode        int
		requestBody               interface{}
		expectedResponseBody      types.NetworkBlackHolePortResponse
		setAgentStateExpectations func(agentState *mock_state.MockAgentState)
	}{
		{
			name:               "stop blackhole port success",
			expectedStatusCode: 200,
			requestBody: map[string]interface{}{
				"Port":        aws.Int(port),
				"Protocol":    aws.String(protocol),
				"TrafficType": aws.String(trafficType),
			},
			expectedResponseBody: types.NetworkBlackHolePortResponse{
				Status: "stopped",
			},
			setAgentStateExpectations: func(agentState *mock_state.MockAgentState) {
				agentState.EXPECT().GetTaskMetadata(endpointId).Return(state.TaskResponse{}, nil)
			},
		},
		// {
		// 	name:               "start blackhole port unknown request body",
		// 	expectedStatusCode: 400,
		// 	requestBody: map[string]interface{}{
		// 		"Port":        aws.Int(1234),
		// 		"Protocol":    aws.String("tcp"),
		// 		"TrafficType": aws.String(""),
		// 		"Unknown":     "",
		// 	},
		// 	expectedResponseBody: types.NetworkBlackHolePortResponse{
		// 		Error: "failed to decode request",
		// 	},
		// 	setAgentStateExpectations: func(agentState *mock_state.MockAgentState) {
		// 		agentState.EXPECT().GetTaskMetadata(endpointId).Return(state.TaskResponse{}, nil).Times(0)
		// 	},
		// },
		// {
		// 	name:               "start blackhole port malformed request body",
		// 	expectedStatusCode: 400,
		// 	requestBody: map[string]interface{}{
		// 		"Port":        aws.String("1234"),
		// 		"Protocol":    aws.String("tcp"),
		// 		"TrafficType": aws.String(""),
		// 	},
		// 	expectedResponseBody: types.NetworkBlackHolePortResponse{
		// 		Error: "failed to decode request",
		// 	},
		// 	setAgentStateExpectations: func(agentState *mock_state.MockAgentState) {
		// 		agentState.EXPECT().GetTaskMetadata(endpointId).Return(state.TaskResponse{}, nil).Times(0)
		// 	},
		// },
		// {
		// 	name:               "start blackhole port task lookup fail",
		// 	expectedStatusCode: 404,
		// 	requestBody: &types.NetworkBlackholePortRequest{
		// 		Port:        aws.Int(1234),
		// 		Protocol:    aws.String("tcp"),
		// 		TrafficType: aws.String(""),
		// 	},
		// 	expectedResponseBody: types.NetworkBlackHolePortResponse{
		// 		Error: fmt.Sprintf("unable to lookup container: %s", endpointId),
		// 	},
		// 	setAgentStateExpectations: func(agentState *mock_state.MockAgentState) {
		// 		agentState.EXPECT().GetTaskMetadata(endpointId).Return(state.TaskResponse{}, state.NewErrorLookupFailure(
		// 			"unable to get task arn from request"))
		// 	},
		// },
		// {
		// 	name:               "start blackhole port task metadata fetch fail",
		// 	expectedStatusCode: 500,
		// 	requestBody: &types.NetworkBlackholePortRequest{
		// 		Port:        aws.Int(1234),
		// 		Protocol:    aws.String("tcp"),
		// 		TrafficType: aws.String(""),
		// 	},
		// 	expectedResponseBody: types.NetworkBlackHolePortResponse{
		// 		Error: fmt.Sprintf("unable to obtain container metadata for container: %s", endpointId),
		// 	},
		// 	setAgentStateExpectations: func(agentState *mock_state.MockAgentState) {
		// 		agentState.EXPECT().GetTaskMetadata(endpointId).Return(state.TaskResponse{}, state.NewErrorMetadataFetchFailure(
		// 			"Unable to generate metadata for task"))
		// 	},
		// },
		// {
		// 	name:               "start blackhole port task metadata unknown fail",
		// 	expectedStatusCode: 500,
		// 	requestBody: &types.NetworkBlackholePortRequest{
		// 		Port:        aws.Int(1234),
		// 		Protocol:    aws.String("tcp"),
		// 		TrafficType: aws.String(""),
		// 	},
		// 	expectedResponseBody: types.NetworkBlackHolePortResponse{
		// 		Error: fmt.Sprintf("failed to get task metadata due to internal server error for container: %s", endpointId),
		// 	},
		// 	setAgentStateExpectations: func(agentState *mock_state.MockAgentState) {
		// 		agentState.EXPECT().GetTaskMetadata(endpointId).Return(state.TaskResponse{}, errors.New("unknown error"))
		// 	},
		// },
	}

	for _, tc := range tcs {
		t.Run(tc.name, func(t *testing.T) {
			// Mocks
			ctrl := gomock.NewController(t)
			defer ctrl.Finish()

			agentState := mock_state.NewMockAgentState(ctrl)
			metricsFactory := mock_metrics.NewMockEntryFactory(ctrl)

			if tc.setAgentStateExpectations != nil {
				tc.setAgentStateExpectations(agentState)
			}

			router := mux.NewRouter()

			RegisterFISHandlers(router, agentState, metricsFactory)

			method := "DELETE"
			var requestBody io.Reader
			if tc.requestBody != nil {
				reqBodyBytes, err := json.Marshal(tc.requestBody)
				require.NoError(t, err)
				requestBody = bytes.NewReader(reqBodyBytes)
			}
			req, err := http.NewRequest(method, fmt.Sprintf("/api/%s/fis/v1/network-blackhole-port", endpointId),
				requestBody)
			require.NoError(t, err)

			// Send the request and record the response
			recorder := httptest.NewRecorder()
			router.ServeHTTP(recorder, req)

			// Parse the response body
			var actualResponseBody types.NetworkBlackHolePortResponse
			err = json.Unmarshal(recorder.Body.Bytes(), &actualResponseBody)
			require.NoError(t, err)

			assert.Equal(t, tc.expectedStatusCode, recorder.Code)
			assert.Equal(t, tc.expectedResponseBody, actualResponseBody)

		})
	}
}
