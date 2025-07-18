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
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/ioutil"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"
	"time"

	apicontainer "github.com/aws/amazon-ecs-agent/agent/api/container"
	apitask "github.com/aws/amazon-ecs-agent/agent/api/task"
	"github.com/aws/amazon-ecs-agent/agent/config"
	mock_dockerstate "github.com/aws/amazon-ecs-agent/agent/engine/dockerstate/mocks"
	v3 "github.com/aws/amazon-ecs-agent/agent/handlers/v3"
	agentV4 "github.com/aws/amazon-ecs-agent/agent/handlers/v4"
	mock_stats "github.com/aws/amazon-ecs-agent/agent/stats/mock"
	apicontainerstatus "github.com/aws/amazon-ecs-agent/ecs-agent/api/container/status"
	mock_ecs "github.com/aws/amazon-ecs-agent/ecs-agent/api/ecs/mocks"
	apierrors "github.com/aws/amazon-ecs-agent/ecs-agent/api/errors"
	apitaskstatus "github.com/aws/amazon-ecs-agent/ecs-agent/api/task/status"
	"github.com/aws/amazon-ecs-agent/ecs-agent/credentials"
	mock_credentials "github.com/aws/amazon-ecs-agent/ecs-agent/credentials/mocks"
	mock_audit "github.com/aws/amazon-ecs-agent/ecs-agent/logger/audit/mocks"
	mock_metrics "github.com/aws/amazon-ecs-agent/ecs-agent/metrics/mocks"
	ni "github.com/aws/amazon-ecs-agent/ecs-agent/netlib/model/networkinterface"
	"github.com/aws/amazon-ecs-agent/ecs-agent/stats"
	faulthandler "github.com/aws/amazon-ecs-agent/ecs-agent/tmds/handlers/fault/v1/handlers"
	faulttype "github.com/aws/amazon-ecs-agent/ecs-agent/tmds/handlers/fault/v1/types"
	tmdsresponse "github.com/aws/amazon-ecs-agent/ecs-agent/tmds/handlers/response"
	tp "github.com/aws/amazon-ecs-agent/ecs-agent/tmds/handlers/taskprotection/v1/handlers"
	tptypes "github.com/aws/amazon-ecs-agent/ecs-agent/tmds/handlers/taskprotection/v1/types"
	"github.com/aws/amazon-ecs-agent/ecs-agent/tmds/handlers/utils"
	tmdsv1 "github.com/aws/amazon-ecs-agent/ecs-agent/tmds/handlers/v1"
	v2 "github.com/aws/amazon-ecs-agent/ecs-agent/tmds/handlers/v2"
	v4 "github.com/aws/amazon-ecs-agent/ecs-agent/tmds/handlers/v4/state"
	mock_execwrapper "github.com/aws/amazon-ecs-agent/ecs-agent/utils/execwrapper/mocks"

	"github.com/aws/aws-sdk-go-v2/aws"
	awshttp "github.com/aws/aws-sdk-go-v2/aws/transport/http"
	"github.com/aws/aws-sdk-go-v2/service/ecs"
	ecstypes "github.com/aws/aws-sdk-go-v2/service/ecs/types"
	smithy "github.com/aws/smithy-go"
	smithyhttp "github.com/aws/smithy-go/transport/http"
	"github.com/docker/docker/api/types"
	"github.com/docker/docker/api/types/network"
	"github.com/golang/mock/gomock"
	"github.com/gorilla/mux"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const (
	clusterName                = "default"
	remoteIP                   = "169.254.170.3"
	remotePort                 = "32146"
	taskARN                    = "t1"
	family                     = "sleep"
	version                    = "1"
	containerID                = "cid"
	containerName              = "sleepy"
	pulledContainerName        = "pulled"
	imageName                  = "busybox"
	imageID                    = "bUsYbOx"
	cpu                        = 1024
	memory                     = 512
	statusRunning              = "RUNNING"
	statusPulled               = "PULLED"
	statusNone                 = "NONE"
	containerType              = "NORMAL"
	containerPort              = 80
	containerPortProtocol      = "tcp"
	eniIPv4Address             = "10.0.0.2"
	roleArn                    = "r1"
	accessKeyID                = "ak"
	secretAccessKey            = "sk"
	credentialsID              = "credentialsId"
	v2BaseStatsPath            = "/v2/stats"
	v2BaseMetadataPath         = "/v2/metadata"
	v2BaseMetadataWithTagsPath = "/v2/metadataWithTags"
	v3BasePath                 = "/v3/"
	v4BasePath                 = "/v4/"
	v3EndpointID               = "v3eid"
	availabilityzone           = "us-west-2b"
	vpcID                      = "test-vpc-id"
	containerInstanceArn       = "containerInstanceArn-test"
	associationType            = "elastic-inference"
	associationName            = "dev1"
	associationEncoding        = "base64"
	associationValue           = "val"
	bridgeMode                 = "bridge"
	bridgeIPAddr               = "17.0.0.3"
	attachmentIndex            = 0
	iPv4SubnetCIDRBlock        = "172.31.32.0/20"
	macAddress                 = "06:96:9a:ce:a6:ce"
	privateDNSName             = "ip-172-31-47-69.us-west-2.compute.internal"
	subnetGatewayIpv4Address   = "172.31.32.1/20"
	taskCredentialsID          = "taskCredentialsId"
	endpointId                 = "endpointId"
	networkNamespace           = "/path"
	hostNetworkNamespace       = "host"
	defaultIfname              = "eth0"

	port                              = 1234
	protocol                          = "tcp"
	trafficType                       = "ingress"
	delayMilliseconds                 = 123456789
	jitterMilliseconds                = 4567
	lossPercent                       = 6
	invalidNetworkMode                = "invalid"
	iptablesChainNotFoundError        = "iptables: Bad rule (does a matching rule exist in that chain?)."
	iptablesChainAlreadyExistError    = "iptables: Chain already exists."
	tcLossFaultExistsCommandOutput    = `[{"kind":"netem","handle":"10:","dev":"eth0","parent":"1:1","options":{"limit":1000,"loss-random":{"loss":0.06,"correlation":0},"ecn":false,"gap":0}}]`
	tcLatencyFaultExistsCommandOutput = `[{"kind":"netem","handle":"10:","parent":"1:1","options":{"limit":1000,"delay":{"delay":123456789,"jitter":4567,"correlation":0},"ecn":false,"gap":0}}]`
	tcCommandEmptyOutput              = `[]`
	requestTimeoutDuration            = 5 * time.Second
	durationMetricPrefix              = "MetadataServer.%s%sDuration"
)

var (
	now         = time.Now()
	association = apitask.Association{
		Containers: []string{containerName},
		Content: apitask.EncodedString{
			Encoding: associationEncoding,
			Value:    associationValue,
		},
		Name: associationName,
		Type: associationType,
	}
	pulledAssociation = apitask.Association{
		Containers: []string{containerName, pulledContainerName},
		Content: apitask.EncodedString{
			Encoding: associationEncoding,
			Value:    associationValue,
		},
		Name: associationName,
		Type: associationType,
	}
	pulledTask = &apitask.Task{
		Arn:                 taskARN,
		Associations:        []apitask.Association{pulledAssociation},
		Family:              family,
		Version:             version,
		DesiredStatusUnsafe: apitaskstatus.TaskRunning,
		KnownStatusUnsafe:   apitaskstatus.TaskStatusNone,
		NetworkMode:         apitask.AWSVPCNetworkMode,
		ENIs: []*ni.NetworkInterface{
			{
				IPV4Addresses: []*ni.IPV4Address{
					{
						Address: eniIPv4Address,
					},
				},
				MacAddress:               macAddress,
				PrivateDNSName:           privateDNSName,
				SubnetGatewayIPV4Address: subnetGatewayIpv4Address,
			},
		},
		CPU:                      cpu,
		Memory:                   memory,
		PullStartedAtUnsafe:      now,
		PullStoppedAtUnsafe:      now,
		ExecutionStoppedAtUnsafe: now,
		LaunchType:               "EC2",
	}
	container = &apicontainer.Container{
		Name:                containerName,
		Image:               imageName,
		ImageID:             imageID,
		DesiredStatusUnsafe: apicontainerstatus.ContainerRunning,
		KnownStatusUnsafe:   apicontainerstatus.ContainerRunning,
		CPU:                 cpu,
		Memory:              memory,
		Type:                apicontainer.ContainerNormal,
		ContainerArn:        "arn:aws:ecs:ap-northnorth-1:NNN:container/NNNNNNNN-aaaa-4444-bbbb-00000000000",
		KnownPortBindingsUnsafe: []apicontainer.PortBinding{
			{
				ContainerPort: containerPort,
				Protocol:      apicontainer.TransportProtocolTCP,
			},
		},
	}
	pulledContainer = &apicontainer.Container{
		Name:                pulledContainerName,
		Image:               imageName,
		ImageID:             imageID,
		DesiredStatusUnsafe: apicontainerstatus.ContainerRunning,
		KnownStatusUnsafe:   apicontainerstatus.ContainerPulled,
		CPU:                 cpu,
		Memory:              memory,
		Type:                apicontainer.ContainerNormal,
		ContainerArn:        "arn:aws:ecs:ap-northnorth-1:NNN:container/NNNNNNNN-aaaa-4444-bbbb-00000000000",
	}
	dockerContainer = &apicontainer.DockerContainer{
		DockerID:   containerID,
		DockerName: containerName,
		Container:  container,
	}
	pulledDockerContainer = &apicontainer.DockerContainer{
		Container: pulledContainer,
	}
	containerNameToDockerContainer = map[string]*apicontainer.DockerContainer{
		taskARN: dockerContainer,
	}
	pulledContainerNameToDockerContainer = map[string]*apicontainer.DockerContainer{
		taskARN: pulledDockerContainer,
	}
	labels = map[string]string{
		"foo": "bar",
	}
	expectedContainerResponse = v2.ContainerResponse{
		ID:            containerID,
		Name:          containerName,
		DockerName:    containerName,
		Image:         imageName,
		ImageID:       imageID,
		DesiredStatus: statusRunning,
		KnownStatus:   statusRunning,
		Limits: v2.LimitsResponse{
			CPU:    aws.Float64(cpu),
			Memory: aws.Int64(memory),
		},
		Type:   containerType,
		Labels: labels,
		Ports: []tmdsresponse.PortResponse{
			{
				ContainerPort: containerPort,
				Protocol:      containerPortProtocol,
				HostPort:      containerPort,
			},
		},
		Networks: []tmdsresponse.Network{
			{
				NetworkMode:   utils.NetworkModeAWSVPC,
				IPv4Addresses: []string{eniIPv4Address},
			},
		},
	}
	expectedPulledContainerResponse = v2.ContainerResponse{
		Name:          pulledContainerName,
		Image:         imageName,
		ImageID:       imageID,
		DesiredStatus: statusRunning,
		KnownStatus:   statusPulled,
		Limits: v2.LimitsResponse{
			CPU:    aws.Float64(cpu),
			Memory: aws.Int64(memory),
		},
		Type: containerType,
	}
	expectedTaskResponseNoContainers = stripContainersFromV2TaskResponse(expectedTaskResponse())
	expectedAssociationsResponse     = v3.AssociationsResponse{
		Associations: []string{associationName},
	}
	expectedAssociationResponse = associationValue

	bridgeTask = &apitask.Task{
		Arn:                      taskARN,
		Associations:             []apitask.Association{association},
		Family:                   family,
		Version:                  version,
		DesiredStatusUnsafe:      apitaskstatus.TaskRunning,
		KnownStatusUnsafe:        apitaskstatus.TaskRunning,
		CPU:                      cpu,
		Memory:                   memory,
		PullStartedAtUnsafe:      now,
		PullStoppedAtUnsafe:      now,
		ExecutionStoppedAtUnsafe: now,
		LaunchType:               "EC2",
		NetworkMode:              bridgeMode,
	}
	container1 = &apicontainer.Container{
		Name:                containerName,
		Image:               imageName,
		ImageID:             imageID,
		DesiredStatusUnsafe: apicontainerstatus.ContainerRunning,
		KnownStatusUnsafe:   apicontainerstatus.ContainerRunning,
		CPU:                 cpu,
		Memory:              memory,
		Type:                apicontainer.ContainerNormal,
		KnownPortBindingsUnsafe: []apicontainer.PortBinding{
			{
				ContainerPort: containerPort,
				Protocol:      apicontainer.TransportProtocolTCP,
			},
		},
		NetworkModeUnsafe: bridgeMode,
		NetworkSettingsUnsafe: &types.NetworkSettings{
			DefaultNetworkSettings: types.DefaultNetworkSettings{
				IPAddress: bridgeIPAddr,
			},
		},
	}
	bridgeContainer = &apicontainer.DockerContainer{
		DockerID:   containerID,
		DockerName: containerName,
		Container:  container1,
	}
	bridgeContainerNoNetwork = &apicontainer.DockerContainer{
		DockerID:   containerID,
		DockerName: containerName,
		Container:  container,
	}
	containerNameToBridgeContainer = map[string]*apicontainer.DockerContainer{
		taskARN: bridgeContainer,
	}
	expectedBridgeContainerResponse = v2.ContainerResponse{
		ID:            containerID,
		Name:          containerName,
		DockerName:    containerName,
		Image:         imageName,
		ImageID:       imageID,
		DesiredStatus: statusRunning,
		KnownStatus:   statusRunning,
		Limits: v2.LimitsResponse{
			CPU:    aws.Float64(cpu),
			Memory: aws.Int64(memory),
		},
		Type:   containerType,
		Labels: labels,
		Ports: []tmdsresponse.PortResponse{
			{
				ContainerPort: containerPort,
				Protocol:      containerPortProtocol,
			},
		},
		Networks: []tmdsresponse.Network{
			{
				NetworkMode:   bridgeMode,
				IPv4Addresses: []string{bridgeIPAddr},
			},
		},
	}
	expectedBridgeTaskResponse = v2.TaskResponse{
		Cluster:       clusterName,
		TaskARN:       taskARN,
		Family:        family,
		Revision:      version,
		DesiredStatus: statusRunning,
		KnownStatus:   statusRunning,
		Containers:    []v2.ContainerResponse{expectedBridgeContainerResponse},
		Limits: &v2.LimitsResponse{
			CPU:    aws.Float64(cpu),
			Memory: aws.Int64(memory),
		},
		PullStartedAt:      aws.Time(now.UTC()),
		PullStoppedAt:      aws.Time(now.UTC()),
		ExecutionStoppedAt: aws.Time(now.UTC()),
		AvailabilityZone:   availabilityzone,
	}
	attachmentIndexVar          = attachmentIndex
	expectedV4ContainerResponse = v4.ContainerResponse{
		ContainerResponse: &v2.ContainerResponse{
			ID:            containerID,
			Name:          containerName,
			DockerName:    containerName,
			Image:         imageName,
			ImageID:       imageID,
			DesiredStatus: statusRunning,
			KnownStatus:   statusRunning,
			ContainerARN:  "arn:aws:ecs:ap-northnorth-1:NNN:container/NNNNNNNN-aaaa-4444-bbbb-00000000000",
			Limits: v2.LimitsResponse{
				CPU:    aws.Float64(cpu),
				Memory: aws.Int64(memory),
			},
			Type:   containerType,
			Labels: labels,
			Ports: []tmdsresponse.PortResponse{
				{
					ContainerPort: containerPort,
					Protocol:      containerPortProtocol,
					HostPort:      containerPort,
				},
			},
		},
		Networks: []v4.Network{{
			Network: tmdsresponse.Network{
				NetworkMode:   utils.NetworkModeAWSVPC,
				IPv4Addresses: []string{eniIPv4Address},
			},
			NetworkInterfaceProperties: v4.NetworkInterfaceProperties{
				AttachmentIndex:          &attachmentIndexVar,
				IPV4SubnetCIDRBlock:      iPv4SubnetCIDRBlock,
				MACAddress:               macAddress,
				PrivateDNSName:           privateDNSName,
				SubnetGatewayIPV4Address: subnetGatewayIpv4Address,
			}},
		},
	}
	expectedV4PulledContainerResponse = v4.ContainerResponse{
		ContainerResponse: &v2.ContainerResponse{
			Name:          pulledContainerName,
			Image:         imageName,
			ImageID:       imageID,
			DesiredStatus: statusRunning,
			KnownStatus:   statusPulled,
			ContainerARN:  "arn:aws:ecs:ap-northnorth-1:NNN:container/NNNNNNNN-aaaa-4444-bbbb-00000000000",
			Limits: v2.LimitsResponse{
				CPU:    aws.Float64(cpu),
				Memory: aws.Int64(memory),
			},
			Type: containerType,
		},
	}
	expectedV4HostContainerResponse = v4.ContainerResponse{
		ContainerResponse: &v2.ContainerResponse{
			ID:            containerID,
			Name:          containerName,
			DockerName:    containerName,
			Image:         imageName,
			ImageID:       imageID,
			DesiredStatus: statusRunning,
			KnownStatus:   statusRunning,
			ContainerARN:  "arn:aws:ecs:ap-northnorth-1:NNN:container/NNNNNNNN-aaaa-4444-bbbb-00000000000",
			Limits: v2.LimitsResponse{
				CPU:    aws.Float64(cpu),
				Memory: aws.Int64(memory),
			},
			Type:   containerType,
			Labels: labels,
			Ports: []tmdsresponse.PortResponse{
				{
					ContainerPort: containerPort,
					Protocol:      containerPortProtocol,
				},
			},
		},
	}
	expectedV4BridgeContainerResponse = v4ContainerResponseFromV2(expectedBridgeContainerResponse, []v4.Network{{
		Network: tmdsresponse.Network{
			NetworkMode:   bridgeMode,
			IPv4Addresses: []string{bridgeIPAddr},
		},
		NetworkInterfaceProperties: v4.NetworkInterfaceProperties{
			AttachmentIndex:          nil,
			IPV4SubnetCIDRBlock:      "",
			MACAddress:               "",
			PrivateDNSName:           "",
			SubnetGatewayIPV4Address: "",
		}},
	})

	agentStateExpectations = func(state *mock_dockerstate.MockTaskEngineState, enableFaultInjection bool, networkMode string) {
		task := standardTask()
		task.EnableFaultInjection = enableFaultInjection
		task.NetworkMode = networkMode
		task.NetworkNamespace = networkNamespace
		task.DefaultIfname = defaultIfname
		gomock.InOrder(
			state.EXPECT().TaskARNByV3EndpointID(endpointId).Return(taskARN, true),
			state.EXPECT().TaskByArn(taskARN).Return(task, true).Times(2),
			state.EXPECT().ContainerMapByArn(taskARN).Return(containerNameToDockerContainer, true),
			state.EXPECT().TaskByArn(taskARN).Return(task, true),
			state.EXPECT().ContainerByID(containerID).Return(dockerContainer, true).AnyTimes(),
			state.EXPECT().PulledContainerMapByArn(taskARN).Return(nil, true),
		)
	}

	ipSources = []string{"52.95.154.1", "52.95.154.2"}

	ipSourcesToFilter = []string{"8.8.8.8"}

	happyBlackHolePortReqBody = map[string]interface{}{
		"Port":        port,
		"Protocol":    protocol,
		"TrafficType": trafficType,
	}

	happyNetworkLatencyReqBody = map[string]interface{}{
		"DelayMilliseconds":  delayMilliseconds,
		"JitterMilliseconds": jitterMilliseconds,
		"Sources":            ipSources,
		"SourcesToFilter":    ipSourcesToFilter,
	}

	happyNetworkPacketLossReqBody = map[string]interface{}{
		"LossPercent":     lossPercent,
		"Sources":         ipSources,
		"SourcesToFilter": ipSourcesToFilter,
	}
)

func standardTask() *apitask.Task {
	task := apitask.Task{
		Arn:                 taskARN,
		Associations:        []apitask.Association{association},
		Family:              family,
		Version:             version,
		DesiredStatusUnsafe: apitaskstatus.TaskRunning,
		KnownStatusUnsafe:   apitaskstatus.TaskRunning,
		NetworkMode:         apitask.AWSVPCNetworkMode,
		ENIs: []*ni.NetworkInterface{
			{
				IPV4Addresses: []*ni.IPV4Address{
					{
						Address: eniIPv4Address,
					},
				},
				MacAddress:               macAddress,
				PrivateDNSName:           privateDNSName,
				SubnetGatewayIPV4Address: subnetGatewayIpv4Address,
			},
		},
		CPU:                      cpu,
		Memory:                   memory,
		PullStartedAtUnsafe:      now,
		PullStoppedAtUnsafe:      now,
		ExecutionStoppedAtUnsafe: now,
		LaunchType:               "EC2",
	}
	task.SetCredentialsID(taskCredentialsID)
	return &task
}

func standardHostTask() *apitask.Task {
	task := standardTask()
	task.ENIs = nil
	task.NetworkMode = apitask.HostNetworkMode
	return task
}

func standardBridgeTask() *apitask.Task {
	container1 = &apicontainer.Container{
		Name:                containerName,
		Image:               imageName,
		ImageID:             imageID,
		DesiredStatusUnsafe: apicontainerstatus.ContainerRunning,
		KnownStatusUnsafe:   apicontainerstatus.ContainerRunning,
		CPU:                 cpu,
		Memory:              memory,
		Type:                apicontainer.ContainerNormal,
		KnownPortBindingsUnsafe: []apicontainer.PortBinding{
			{
				ContainerPort: containerPort,
				Protocol:      apicontainer.TransportProtocolTCP,
			},
		},
		NetworkModeUnsafe: bridgeMode,
		NetworkSettingsUnsafe: &types.NetworkSettings{
			DefaultNetworkSettings: types.DefaultNetworkSettings{
				IPAddress: bridgeIPAddr,
			},
		},
	}
	container1.SetLabels(labels)
	return &apitask.Task{
		Arn:                      taskARN,
		Associations:             []apitask.Association{association},
		Containers:               []*apicontainer.Container{container1},
		Family:                   family,
		Version:                  version,
		DesiredStatusUnsafe:      apitaskstatus.TaskRunning,
		KnownStatusUnsafe:        apitaskstatus.TaskRunning,
		CPU:                      cpu,
		Memory:                   memory,
		PullStartedAtUnsafe:      now,
		PullStoppedAtUnsafe:      now,
		ExecutionStoppedAtUnsafe: now,
		LaunchType:               "EC2",
		NetworkMode:              bridgeMode,
	}
}

func standardBridgeDockerContainer() *apicontainer.DockerContainer {
	return &apicontainer.DockerContainer{
		DockerID:   containerID,
		DockerName: containerName,
		Container:  standardBridgeTask().Containers[0],
	}
}

func standardV4BridgeContainerResponse() *v4.ContainerResponse {
	return &v4.ContainerResponse{
		ContainerResponse: &v2.ContainerResponse{ID: containerID,
			Name:          containerName,
			DockerName:    containerName,
			Image:         imageName,
			ImageID:       imageID,
			DesiredStatus: statusRunning,
			KnownStatus:   statusRunning,
			Limits: v2.LimitsResponse{
				CPU:    aws.Float64(cpu),
				Memory: aws.Int64(memory),
			},
			Type:   containerType,
			Labels: labels,
			Ports: []tmdsresponse.PortResponse{
				{
					ContainerPort: containerPort,
					Protocol:      containerPortProtocol,
				},
			},
		},
		Networks: []v4.Network{
			{
				Network: tmdsresponse.Network{
					NetworkMode:   bridgeMode,
					IPv4Addresses: []string{bridgeIPAddr},
				},
				NetworkInterfaceProperties: v4.NetworkInterfaceProperties{},
			},
		},
	}
}

// Returns a standard v2 task response. This getter function protects against tests mutating the
// response.
func expectedTaskResponse() v2.TaskResponse {
	return v2.TaskResponse{
		Cluster:       clusterName,
		TaskARN:       taskARN,
		Family:        family,
		Revision:      version,
		DesiredStatus: statusRunning,
		KnownStatus:   statusRunning,
		Containers:    []v2.ContainerResponse{expectedContainerResponse},
		Limits: &v2.LimitsResponse{
			CPU:    aws.Float64(cpu),
			Memory: aws.Int64(memory),
		},
		PullStartedAt:      aws.Time(now.UTC()),
		PullStoppedAt:      aws.Time(now.UTC()),
		ExecutionStoppedAt: aws.Time(now.UTC()),
		AvailabilityZone:   availabilityzone,
	}
}

// standardV4ContainerResponseAWSVPC returns a standard TMDS v4 container response for testing.
// The response corresponds to the standard awsvpc container defined in this package.
func standardV4ContainerResponseAWSVPC() *v4.ContainerResponse {
	return &v4.ContainerResponse{
		ContainerResponse: &v2.ContainerResponse{
			ID:            containerID,
			Name:          containerName,
			DockerName:    containerName,
			Image:         imageName,
			ImageID:       imageID,
			DesiredStatus: statusRunning,
			KnownStatus:   statusRunning,
			ContainerARN:  "arn:aws:ecs:ap-northnorth-1:NNN:container/NNNNNNNN-aaaa-4444-bbbb-00000000000",
			Limits: v2.LimitsResponse{
				CPU:    aws.Float64(cpu),
				Memory: aws.Int64(memory),
			},
			Type:   containerType,
			Labels: labels,
			Ports: []tmdsresponse.PortResponse{
				{
					ContainerPort: containerPort,
					Protocol:      containerPortProtocol,
					HostPort:      containerPort,
				},
			},
		},
		Networks: []v4.Network{{
			Network: tmdsresponse.Network{
				NetworkMode:   utils.NetworkModeAWSVPC,
				IPv4Addresses: []string{eniIPv4Address},
			},
			NetworkInterfaceProperties: v4.NetworkInterfaceProperties{
				AttachmentIndex:          &attachmentIndexVar,
				IPV4SubnetCIDRBlock:      iPv4SubnetCIDRBlock,
				MACAddress:               macAddress,
				PrivateDNSName:           privateDNSName,
				SubnetGatewayIPV4Address: subnetGatewayIpv4Address,
			}},
		},
	}
}

// Creates a v4 ContainerResponse given a v2 ContainerResponse and v4 networks
func v4ContainerResponseFromV2(
	v2ContainerResponse v2.ContainerResponse, networks []v4.Network) v4.ContainerResponse {
	v2ContainerResponse.Networks = nil
	return v4.ContainerResponse{
		ContainerResponse: &v2ContainerResponse,
		Networks:          networks,
	}
}

// expectedV4TaskResponseWithFaultInjectionEnabled returns a standard v4 task response with
// FaultInjection enabled.
func expectedV4TaskResponseWithFaultInjectionEnabled() v4.TaskResponse {
	taskResp := expectedV4TaskResponse()
	taskResp.FaultInjectionEnabled = true
	return taskResp
}

// Returns a standard v4 task response. This getter function protects against tests mutating
// the response.
func expectedV4TaskResponse() v4.TaskResponse {
	return v4TaskResponseFromV2(
		v2.TaskResponse{
			Cluster:       clusterName,
			TaskARN:       taskARN,
			Family:        family,
			Revision:      version,
			DesiredStatus: statusRunning,
			KnownStatus:   statusRunning,
			Limits: &v2.LimitsResponse{
				CPU:    aws.Float64(cpu),
				Memory: aws.Int64(memory),
			},
			PullStartedAt:      aws.Time(now.UTC()),
			PullStoppedAt:      aws.Time(now.UTC()),
			ExecutionStoppedAt: aws.Time(now.UTC()),
			AvailabilityZone:   availabilityzone,
			LaunchType:         "EC2",
		},
		[]v4.ContainerResponse{expectedV4ContainerResponse},
		vpcID,
	)
}

// expectedV4TaskResponseHostModeWithFaultInjectionEnabled returns a standard v4 task response with
// FaultInjection enabled.
func expectedV4TaskResponseHostModeWithFaultInjectionEnabled() v4.TaskResponse {
	taskResp := expectedV4TaskResponseHostMode()
	taskResp.FaultInjectionEnabled = true
	return taskResp
}

func expectedV4TaskResponseHostMode() v4.TaskResponse {
	return v4TaskResponseFromV2(
		v2.TaskResponse{
			Cluster:       clusterName,
			TaskARN:       taskARN,
			Family:        family,
			Revision:      version,
			DesiredStatus: statusRunning,
			KnownStatus:   statusRunning,
			Limits: &v2.LimitsResponse{
				CPU:    aws.Float64(cpu),
				Memory: aws.Int64(memory),
			},
			PullStartedAt:      aws.Time(now.UTC()),
			PullStoppedAt:      aws.Time(now.UTC()),
			ExecutionStoppedAt: aws.Time(now.UTC()),
			AvailabilityZone:   availabilityzone,
			LaunchType:         "EC2",
		},
		[]v4.ContainerResponse{expectedV4HostContainerResponse},
		vpcID,
	)
}

// Returns a standard v4 task response including pulled containers response. This getter function
// protects against tests mutating the response.
func expectedV4PulledTaskResponse() v4.TaskResponse {
	return v4TaskResponseFromV2(
		v2.TaskResponse{
			Cluster:       clusterName,
			TaskARN:       taskARN,
			Family:        family,
			Revision:      version,
			DesiredStatus: statusRunning,
			KnownStatus:   statusNone,
			Limits: &v2.LimitsResponse{
				CPU:    aws.Float64(cpu),
				Memory: aws.Int64(memory),
			},
			PullStartedAt:      aws.Time(now.UTC()),
			PullStoppedAt:      aws.Time(now.UTC()),
			ExecutionStoppedAt: aws.Time(now.UTC()),
			AvailabilityZone:   availabilityzone,
			LaunchType:         "EC2",
		},
		[]v4.ContainerResponse{expectedV4ContainerResponse, expectedV4PulledContainerResponse},
		vpcID,
	)
}

// Returns a standard v4 bridge task response including pulled containers response. This getter function
// protects against tests mutating the response.
func expectedV4BridgeTaskResponse() v4.TaskResponse {
	return v4TaskResponseFromV2(
		v2.TaskResponse{
			Cluster:       clusterName,
			TaskARN:       taskARN,
			Family:        family,
			Revision:      version,
			DesiredStatus: statusRunning,
			KnownStatus:   statusRunning,
			Limits: &v2.LimitsResponse{
				CPU:    aws.Float64(cpu),
				Memory: aws.Int64(memory),
			},
			PullStartedAt:      aws.Time(now.UTC()),
			PullStoppedAt:      aws.Time(now.UTC()),
			ExecutionStoppedAt: aws.Time(now.UTC()),
			AvailabilityZone:   availabilityzone,
			LaunchType:         "EC2",
		},
		[]v4.ContainerResponse{expectedV4BridgeContainerResponse},
		vpcID,
	)
}

// Returns a standard v4 bridge task response whose container does not have any network populated.
func expectedV4BridgeTaskResponseNoNetwork() v4.TaskResponse {
	taskResponse := expectedV4BridgeTaskResponse()
	containers := []v4.ContainerResponse{}
	for _, c := range taskResponse.Containers {
		c.Networks = nil
		containers = append(containers, c)
	}
	taskResponse.Containers = containers
	return taskResponse
}

func expectedV4TaskResponseNoContainers() v4.TaskResponse {
	taskResponse := expectedV4TaskResponse()
	taskResponse.Containers = nil
	return taskResponse
}

func taskRoleCredentials() credentials.TaskIAMRoleCredentials {
	return credentials.TaskIAMRoleCredentials{
		ARN: taskARN,
		IAMRoleCredentials: credentials.IAMRoleCredentials{
			RoleArn:         "roleArn",
			AccessKeyID:     "accessKeyID",
			SecretAccessKey: "secretAccessKey",
			SessionToken:    "sessionToken",
			Expiration:      "expiration",
		},
	}
}

func v4TaskResponseFromV2(
	v2TaskResponse v2.TaskResponse,
	containers []v4.ContainerResponse,
	vcpID string,
) v4.TaskResponse {
	v2TaskResponse.Containers = nil
	return v4.TaskResponse{
		TaskResponse:          &v2TaskResponse,
		Containers:            containers,
		VPCID:                 vpcID,
		FaultInjectionEnabled: false,
	}
}

// Returns a new v2 task response by stripping the "containers" field from the provided
// task response.
func stripContainersFromV2TaskResponse(response v2.TaskResponse) v2.TaskResponse {
	response.Containers = nil
	return response
}

// Returns a standard container instance tags map for testing
func standardContainerInstanceTags() map[string]string {
	return map[string]string{
		"ContainerInstanceTag1": "firstTag",
		"ContainerInstanceTag2": "secondTag",
	}
}

// Returns a standard task tags map for testing
func standardTaskTags() map[string]string {
	return map[string]string{
		"TaskTag1": "firstTag",
		"TaskTag2": "secondTag",
	}
}

// Returns standard container instance tags in ECS format
func standardECSContainerInstanceTags() []ecstypes.Tag {
	ecsTags := []ecstypes.Tag{}
	tags := standardContainerInstanceTags()
	for k, v := range tags {
		ecsTags = append(ecsTags, ecstypes.Tag{Key: aws.String(k), Value: aws.String(v)})
	}
	return ecsTags
}

// Returns standard task instance tags in ECS format
func standardECSTaskTags() []ecstypes.Tag {
	ecsTags := []ecstypes.Tag{}
	tags := standardTaskTags()
	for k, v := range tags {
		ecsTags = append(ecsTags, ecstypes.Tag{Key: aws.String(k), Value: aws.String(v)})
	}
	return ecsTags
}

func init() {
	container.SetLabels(labels)
	container1.SetLabels(labels)
}

// TestInvalidPath tests if HTTP status code 404 is returned when invalid path is queried.
func TestInvalidPath(t *testing.T) {
	testErrorResponsesFromServer(t, "/", nil)
}

// TestCredentialsV1RequestWithNoArguments tests if HTTP status code 400 is returned when
// query parameters are not specified for the credentials endpoint.
func TestCredentialsV1RequestWithNoArguments(t *testing.T) {
	msg := &utils.ErrorMessage{
		Code:          tmdsv1.ErrNoIDInRequest,
		Message:       "CredentialsV1Request: No ID in the request",
		HTTPErrorCode: http.StatusBadRequest,
	}
	testErrorResponsesFromServer(t, credentials.V1CredentialsPath, msg)
}

// TestCredentialsV2RequestWithNoArguments tests if HTTP status code 400 is returned when
// query parameters are not specified for the credentials endpoint.
func TestCredentialsV2RequestWithNoArguments(t *testing.T) {
	msg := &utils.ErrorMessage{
		Code:          tmdsv1.ErrNoIDInRequest,
		Message:       "CredentialsV2Request: No ID in the request",
		HTTPErrorCode: http.StatusBadRequest,
	}
	testErrorResponsesFromServer(t, credentials.V2CredentialsPath+"/", msg)
}

// TestCredentialsV1RequestWhenCredentialsIdNotFound tests if HTTP status code 400 is returned when
// the credentials manager does not contain the credentials id specified in the query.
func TestCredentialsV1RequestWhenCredentialsIdNotFound(t *testing.T) {
	expectedErrorMessage := &utils.ErrorMessage{
		Code:          tmdsv1.ErrInvalidIDInRequest,
		Message:       fmt.Sprintf("CredentialsV1Request: Credentials not found"),
		HTTPErrorCode: http.StatusBadRequest,
	}
	path := credentials.V1CredentialsPath + "?id=" + credentialsID
	_, err := getResponseForCredentialsRequest(t, expectedErrorMessage.HTTPErrorCode,
		expectedErrorMessage, path, func() (credentials.TaskIAMRoleCredentials, bool) { return credentials.TaskIAMRoleCredentials{}, false })
	assert.NoError(t, err, "Error getting response body")
}

// TestCredentialsV2RequestWhenCredentialsIdNotFound tests if HTTP status code 400 is returned when
// the credentials manager does not contain the credentials id specified in the query.
func TestCredentialsV2RequestWhenCredentialsIdNotFound(t *testing.T) {
	expectedErrorMessage := &utils.ErrorMessage{
		Code:          tmdsv1.ErrInvalidIDInRequest,
		Message:       fmt.Sprintf("CredentialsV2Request: Credentials not found"),
		HTTPErrorCode: http.StatusBadRequest,
	}
	path := credentials.V2CredentialsPath + "/" + credentialsID
	_, err := getResponseForCredentialsRequest(t, expectedErrorMessage.HTTPErrorCode,
		expectedErrorMessage, path, func() (credentials.TaskIAMRoleCredentials, bool) { return credentials.TaskIAMRoleCredentials{}, false })
	assert.NoError(t, err, "Error getting response body")
}

// TestCredentialsV1RequestWhenCredentialsUninitialized tests if HTTP status code 500 is returned when
// the credentials manager returns empty credentials.
func TestCredentialsV1RequestWhenCredentialsUninitialized(t *testing.T) {
	expectedErrorMessage := &utils.ErrorMessage{
		Code:          tmdsv1.ErrCredentialsUninitialized,
		Message:       fmt.Sprintf("CredentialsV1Request: Credentials uninitialized for ID"),
		HTTPErrorCode: http.StatusServiceUnavailable,
	}
	path := credentials.V1CredentialsPath + "?id=" + credentialsID
	_, err := getResponseForCredentialsRequest(t, expectedErrorMessage.HTTPErrorCode,
		expectedErrorMessage, path, func() (credentials.TaskIAMRoleCredentials, bool) { return credentials.TaskIAMRoleCredentials{}, true })
	assert.NoError(t, err, "Error getting response body")
}

// TestCredentialsV2RequestWhenCredentialsUninitialized tests if HTTP status code 500 is returned when
// the credentials manager returns empty credentials.
func TestCredentialsV2RequestWhenCredentialsUninitialized(t *testing.T) {
	expectedErrorMessage := &utils.ErrorMessage{
		Code:          tmdsv1.ErrCredentialsUninitialized,
		Message:       fmt.Sprintf("CredentialsV2Request: Credentials uninitialized for ID"),
		HTTPErrorCode: http.StatusServiceUnavailable,
	}
	path := credentials.V2CredentialsPath + "/" + credentialsID
	_, err := getResponseForCredentialsRequest(t, expectedErrorMessage.HTTPErrorCode,
		expectedErrorMessage, path, func() (credentials.TaskIAMRoleCredentials, bool) { return credentials.TaskIAMRoleCredentials{}, true })
	assert.NoError(t, err, "Error getting response body")
}

// TestCredentialsV1RequestWhenCredentialsFound tests if HTTP status code 200 is returned when
// the credentials manager contains the credentials id specified in the query.
func TestCredentialsV1RequestWhenCredentialsFound(t *testing.T) {
	creds := credentials.TaskIAMRoleCredentials{
		ARN: "arn",
		IAMRoleCredentials: credentials.IAMRoleCredentials{
			RoleArn:         roleArn,
			AccessKeyID:     accessKeyID,
			SecretAccessKey: secretAccessKey,
		},
	}
	path := credentials.V1CredentialsPath + "?id=" + credentialsID
	body, err := getResponseForCredentialsRequest(t, http.StatusOK, nil, path, func() (credentials.TaskIAMRoleCredentials, bool) { return creds, true })
	assert.NoError(t, err)

	credentials, err := parseResponseBody(body)
	assert.NoError(t, err, "Error retrieving credentials")

	assert.Equal(t, roleArn, credentials.RoleArn, "Incorrect credentials received: role ARN")
	assert.Equal(t, accessKeyID, credentials.AccessKeyID, "Incorrect credentials received: access key ID")
	assert.Equal(t, secretAccessKey, credentials.SecretAccessKey, "Incorrect credentials received: secret access key")
}

// TestCredentialsV2RequestWhenCredentialsFound tests if HTTP status code 200 is returned when
// the credentials manager contains the credentials id specified in the query.
func TestCredentialsV2RequestWhenCredentialsFound(t *testing.T) {
	creds := credentials.TaskIAMRoleCredentials{
		ARN: "arn",
		IAMRoleCredentials: credentials.IAMRoleCredentials{
			RoleArn:         roleArn,
			AccessKeyID:     accessKeyID,
			SecretAccessKey: secretAccessKey,
		},
	}
	path := credentials.V2CredentialsPath + "/" + credentialsID
	body, err := getResponseForCredentialsRequest(t, http.StatusOK, nil, path, func() (credentials.TaskIAMRoleCredentials, bool) { return creds, true })
	if err != nil {
		t.Fatalf("Error retrieving credentials response: %v", err)
	}

	credentials, err := parseResponseBody(body)
	assert.NoError(t, err, "Error retrieving credentials")

	assert.Equal(t, roleArn, credentials.RoleArn, "Incorrect credentials received: role ARN")
	assert.Equal(t, accessKeyID, credentials.AccessKeyID, "Incorrect credentials received: access key ID")
	assert.Equal(t, secretAccessKey, credentials.SecretAccessKey, "Incorrect credentials received: secret access key")
}

func testErrorResponsesFromServer(t *testing.T, path string, expectedErrorMessage *utils.ErrorMessage) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	credentialsManager := mock_credentials.NewMockManager(ctrl)
	auditLog := mock_audit.NewMockAuditLogger(ctrl)
	ecsClient := mock_ecs.NewMockECSClient(ctrl)
	server, err := taskServerSetup(credentialsManager, auditLog, nil, ecsClient, "", nil,
		config.DefaultTaskMetadataSteadyStateRate, config.DefaultTaskMetadataBurstRate, "", vpcID,
		containerInstanceArn, tp.NewMockTaskProtectionClientFactoryInterface(ctrl))
	require.NoError(t, err)

	recorder := httptest.NewRecorder()
	req, _ := http.NewRequest("GET", path, nil)
	if path == credentials.V1CredentialsPath || path == credentials.V2CredentialsPath+"/" {
		auditLog.EXPECT().Log(gomock.Any(), gomock.Any(), gomock.Any())
	}

	server.Handler.ServeHTTP(recorder, req)
	HTTPErrorCode := http.StatusNotFound
	if expectedErrorMessage != nil {
		HTTPErrorCode = expectedErrorMessage.HTTPErrorCode
	}
	assert.Equal(t, HTTPErrorCode, recorder.Code, "Incorrect return code")

	// Only paths that are equal to /v1/credentials will return valid error responses.
	if path == credentials.CredentialsPath {
		errorMessage := &utils.ErrorMessage{}
		json.Unmarshal(recorder.Body.Bytes(), errorMessage)
		assert.Equal(t, expectedErrorMessage.Code, errorMessage.Code, "Incorrect error code")
		assert.Equal(t, expectedErrorMessage.Message, errorMessage.Message, "Incorrect error message")
	}
}

// getResponseForCredentialsRequestWithParameters queries credentials for the
// given id. The getCredentials function is used to simulate getting the
// credentials object from the CredentialsManager
func getResponseForCredentialsRequest(t *testing.T, expectedStatus int,
	expectedErrorMessage *utils.ErrorMessage, path string, getCredentials func() (credentials.TaskIAMRoleCredentials, bool)) (*bytes.Buffer, error) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()
	credentialsManager := mock_credentials.NewMockManager(ctrl)
	auditLog := mock_audit.NewMockAuditLogger(ctrl)
	ecsClient := mock_ecs.NewMockECSClient(ctrl)
	server, err := taskServerSetup(credentialsManager, auditLog, nil, ecsClient, "", nil,
		config.DefaultTaskMetadataSteadyStateRate, config.DefaultTaskMetadataBurstRate, "", vpcID,
		containerInstanceArn, tp.NewMockTaskProtectionClientFactoryInterface(ctrl))
	require.NoError(t, err)

	recorder := httptest.NewRecorder()

	creds, ok := getCredentials()
	credentialsManager.EXPECT().GetTaskCredentials(gomock.Any()).Return(creds, ok)
	auditLog.EXPECT().Log(gomock.Any(), gomock.Any(), gomock.Any())

	params := make(url.Values)
	params[credentials.CredentialsIDQueryParameterName] = []string{credentialsID}

	req, _ := http.NewRequest("GET", path, nil)
	server.Handler.ServeHTTP(recorder, req)

	assert.Equal(t, expectedStatus, recorder.Code, "Incorrect return code")

	if recorder.Code != http.StatusOK {
		errorMessage := &utils.ErrorMessage{}
		json.Unmarshal(recorder.Body.Bytes(), errorMessage)

		assert.Equal(t, expectedErrorMessage.Code, errorMessage.Code, "Incorrect error code")
		assert.Equal(t, expectedErrorMessage.Message, errorMessage.Message, "Incorrect error message")
	}

	return recorder.Body, nil
}

// parseResponseBody parses the HTTP response body into an IAMRoleCredentials object.
func parseResponseBody(body *bytes.Buffer) (*credentials.IAMRoleCredentials, error) {
	response, err := ioutil.ReadAll(body)
	if err != nil {
		return nil, fmt.Errorf("Error reading response body: %v", err)
	}
	var creds credentials.IAMRoleCredentials
	err = json.Unmarshal(response, &creds)
	if err != nil {
		return nil, fmt.Errorf("Error unmarshaling: %v", err)
	}
	return &creds, nil
}

func TestV3ContainerAssociations(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	state := mock_dockerstate.NewMockTaskEngineState(ctrl)
	auditLog := mock_audit.NewMockAuditLogger(ctrl)
	statsEngine := mock_stats.NewMockEngine(ctrl)
	ecsClient := mock_ecs.NewMockECSClient(ctrl)

	gomock.InOrder(
		state.EXPECT().DockerIDByV3EndpointID(v3EndpointID).Return(containerID, true),
		state.EXPECT().TaskARNByV3EndpointID(v3EndpointID).Return(taskARN, true),
		state.EXPECT().ContainerByID(containerID).Return(dockerContainer, true),
		state.EXPECT().TaskByArn(taskARN).Return(standardTask(), true),
	)
	server, err := taskServerSetup(credentials.NewManager(), auditLog, state, ecsClient, clusterName, statsEngine,
		config.DefaultTaskMetadataSteadyStateRate, config.DefaultTaskMetadataBurstRate, "", vpcID,
		containerInstanceArn, tp.NewMockTaskProtectionClientFactoryInterface(ctrl))
	require.NoError(t, err)
	recorder := httptest.NewRecorder()
	req, _ := http.NewRequest("GET", v3BasePath+v3EndpointID+"/associations/"+associationType, nil)
	server.Handler.ServeHTTP(recorder, req)
	res, err := ioutil.ReadAll(recorder.Body)
	assert.NoError(t, err)
	assert.Equal(t, http.StatusOK, recorder.Code)

	var associationsResponse v3.AssociationsResponse
	err = json.Unmarshal(res, &associationsResponse)
	assert.NoError(t, err)
	assert.Equal(t, expectedAssociationsResponse, associationsResponse)
}

func TestV3ContainerAssociation(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	task := standardTask()

	state := mock_dockerstate.NewMockTaskEngineState(ctrl)
	auditLog := mock_audit.NewMockAuditLogger(ctrl)
	statsEngine := mock_stats.NewMockEngine(ctrl)
	ecsClient := mock_ecs.NewMockECSClient(ctrl)

	gomock.InOrder(
		state.EXPECT().TaskARNByV3EndpointID(v3EndpointID).Return(taskARN, true),
		state.EXPECT().TaskByArn(taskARN).Return(task, true),
	)
	server, err := taskServerSetup(credentials.NewManager(), auditLog, state, ecsClient, clusterName, statsEngine,
		config.DefaultTaskMetadataSteadyStateRate, config.DefaultTaskMetadataBurstRate, "", vpcID,
		containerInstanceArn, tp.NewMockTaskProtectionClientFactoryInterface(ctrl))
	require.NoError(t, err)
	recorder := httptest.NewRecorder()
	req, _ := http.NewRequest("GET", v3BasePath+v3EndpointID+"/associations/"+associationType+"/"+associationName, nil)
	server.Handler.ServeHTTP(recorder, req)
	res, err := ioutil.ReadAll(recorder.Body)
	assert.NoError(t, err)
	assert.Equal(t, http.StatusOK, recorder.Code)

	assert.Equal(t, expectedAssociationResponse, string(res))
}

func TestV4ContainerAssociations(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	task := standardTask()

	state := mock_dockerstate.NewMockTaskEngineState(ctrl)
	auditLog := mock_audit.NewMockAuditLogger(ctrl)
	statsEngine := mock_stats.NewMockEngine(ctrl)
	ecsClient := mock_ecs.NewMockECSClient(ctrl)

	gomock.InOrder(
		state.EXPECT().DockerIDByV3EndpointID(v3EndpointID).Return(containerID, true),
		state.EXPECT().TaskARNByV3EndpointID(v3EndpointID).Return(taskARN, true),
		state.EXPECT().ContainerByID(containerID).Return(dockerContainer, true),
		state.EXPECT().TaskByArn(taskARN).Return(task, true),
	)
	server, err := taskServerSetup(credentials.NewManager(), auditLog, state, ecsClient, clusterName, statsEngine,
		config.DefaultTaskMetadataSteadyStateRate, config.DefaultTaskMetadataBurstRate, "", vpcID,
		containerInstanceArn, tp.NewMockTaskProtectionClientFactoryInterface(ctrl))
	require.NoError(t, err)
	recorder := httptest.NewRecorder()
	req, _ := http.NewRequest("GET", v4BasePath+v3EndpointID+"/associations/"+associationType, nil)
	server.Handler.ServeHTTP(recorder, req)
	res, err := ioutil.ReadAll(recorder.Body)
	assert.NoError(t, err)
	assert.Equal(t, http.StatusOK, recorder.Code)

	var associationsResponse v3.AssociationsResponse
	err = json.Unmarshal(res, &associationsResponse)
	assert.NoError(t, err)
	assert.Equal(t, expectedAssociationsResponse, associationsResponse)
}

func TestV4ContainerAssociation(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	task := standardTask()

	state := mock_dockerstate.NewMockTaskEngineState(ctrl)
	auditLog := mock_audit.NewMockAuditLogger(ctrl)
	statsEngine := mock_stats.NewMockEngine(ctrl)
	ecsClient := mock_ecs.NewMockECSClient(ctrl)

	gomock.InOrder(
		state.EXPECT().TaskARNByV3EndpointID(v3EndpointID).Return(taskARN, true),
		state.EXPECT().TaskByArn(taskARN).Return(task, true),
	)
	server, err := taskServerSetup(credentials.NewManager(), auditLog, state, ecsClient, clusterName, statsEngine,
		config.DefaultTaskMetadataSteadyStateRate, config.DefaultTaskMetadataBurstRate, "", vpcID,
		containerInstanceArn, tp.NewMockTaskProtectionClientFactoryInterface(ctrl))
	require.NoError(t, err)
	recorder := httptest.NewRecorder()
	req, _ := http.NewRequest("GET", v4BasePath+v3EndpointID+"/associations/"+associationType+"/"+associationName, nil)
	server.Handler.ServeHTTP(recorder, req)
	res, err := ioutil.ReadAll(recorder.Body)
	assert.NoError(t, err)
	assert.Equal(t, http.StatusOK, recorder.Code)
	assert.Equal(t, expectedAssociationResponse, string(res))
}

func TestTaskHTTPEndpoint301Redirect(t *testing.T) {
	testPathsMap := map[string]string{
		"http://127.0.0.1/v3///task/":           "http://127.0.0.1/v3/task/",
		"http://127.0.0.1//v2/credentials/test": "http://127.0.0.1/v2/credentials/test",
	}

	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	state := mock_dockerstate.NewMockTaskEngineState(ctrl)
	auditLog := mock_audit.NewMockAuditLogger(ctrl)
	statsEngine := mock_stats.NewMockEngine(ctrl)
	ecsClient := mock_ecs.NewMockECSClient(ctrl)

	server, err := taskServerSetup(credentials.NewManager(), auditLog, state, ecsClient, clusterName, statsEngine,
		config.DefaultTaskMetadataSteadyStateRate, config.DefaultTaskMetadataBurstRate, "", vpcID,
		containerInstanceArn, tp.NewMockTaskProtectionClientFactoryInterface(ctrl))
	require.NoError(t, err)

	for testPath, expectedPath := range testPathsMap {
		t.Run(fmt.Sprintf("Test path: %s", testPath), func(t *testing.T) {
			recorder := httptest.NewRecorder()
			req, _ := http.NewRequest("GET", testPath, nil)
			server.Handler.ServeHTTP(recorder, req)
			assert.Equal(t, http.StatusMovedPermanently, recorder.Code)
			assert.Equal(t, expectedPath, recorder.Header().Get("Location"))
		})
	}
}

func TestTaskHTTPEndpointErrorCode404(t *testing.T) {
	testPaths := []string{
		"/",
		"/v2/meta",
		"/v2/statss",
		"/v3",
		"/v3/v3-endpoint-id/",
		"/v3/v3-endpoint-id/wrong-path",
		"/v3/v3-endpoint-id/task/",
		"/v3/v3-endpoint-id/task/stats/",
		"/v3/v3-endpoint-id/task/stats/wrong-path",
		"/v3/v3-endpoint-id/associtions-with-typo/elastic-inference/dev1",
		"/v4/v3-endpoint-id/",
		"/v4/v3-endpoint-id/wrong-path",
		"/v4/v3-endpoint-id/task/",
		"/v4/v3-endpoint-id/task/stats/",
		"/v4/v3-endpoint-id/task/stats/wrong-path",
	}

	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	state := mock_dockerstate.NewMockTaskEngineState(ctrl)
	auditLog := mock_audit.NewMockAuditLogger(ctrl)
	statsEngine := mock_stats.NewMockEngine(ctrl)
	ecsClient := mock_ecs.NewMockECSClient(ctrl)

	server, err := taskServerSetup(credentials.NewManager(), auditLog, state, ecsClient, clusterName, statsEngine,
		config.DefaultTaskMetadataSteadyStateRate, config.DefaultTaskMetadataBurstRate, "", vpcID,
		containerInstanceArn, tp.NewMockTaskProtectionClientFactoryInterface(ctrl))
	require.NoError(t, err)

	for _, testPath := range testPaths {
		t.Run(fmt.Sprintf("Test path: %s", testPath), func(t *testing.T) {
			recorder := httptest.NewRecorder()
			req, _ := http.NewRequest("GET", testPath, nil)
			server.Handler.ServeHTTP(recorder, req)
			assert.Equal(t, http.StatusNotFound, recorder.Code)
		})
	}
}

func TestTaskHTTPEndpointErrorCode400(t *testing.T) {
	testPaths := []string{
		"/v2/metadata",
		"/v2/metadata/",
		"/v2/metadata/wrong-container-id",
		"/v2/metadata/container-id/other-path",
		"/v2/stats",
		"/v2/stats/",
		"/v2/stats/wrong-container-id",
		"/v2/stats/container-id/other-path",
		"/v3/wrong-v3-endpoint-id/stats",
		"/v3/wrong-v3-endpoint-id/task/stats",
		"/v3/task/stats",
		"/v3/wrong-v3-endpoint-id/associations/elastic-inference",
		"/v3/wrong-v3-endpoint-id/associations/elastic-inference/dev1",
	}

	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	state := mock_dockerstate.NewMockTaskEngineState(ctrl)
	auditLog := mock_audit.NewMockAuditLogger(ctrl)
	statsEngine := mock_stats.NewMockEngine(ctrl)
	ecsClient := mock_ecs.NewMockECSClient(ctrl)

	server, err := taskServerSetup(credentials.NewManager(), auditLog, state, ecsClient, clusterName, statsEngine,
		config.DefaultTaskMetadataSteadyStateRate, config.DefaultTaskMetadataBurstRate, "", vpcID,
		containerInstanceArn, tp.NewMockTaskProtectionClientFactoryInterface(ctrl))
	require.NoError(t, err)

	for _, testPath := range testPaths {
		t.Run(fmt.Sprintf("Test path: %s", testPath), func(t *testing.T) {
			// Make every possible call to state fail
			state.EXPECT().TaskARNByV3EndpointID(gomock.Any()).Return("", false).AnyTimes()
			state.EXPECT().DockerIDByV3EndpointID(gomock.Any()).Return("", false).AnyTimes()
			state.EXPECT().TaskARNByV3EndpointID(gomock.Any()).Return("", false).AnyTimes()
			state.EXPECT().GetTaskByIPAddress(gomock.Any()).Return("", false).AnyTimes()

			recorder := httptest.NewRecorder()
			req, _ := http.NewRequest("GET", testPath, nil)
			req.RemoteAddr = remoteIP + ":" + remotePort
			server.Handler.ServeHTTP(recorder, req)
			assert.Equal(t, http.StatusBadRequest, recorder.Code)
		})
	}
}

func TestTaskHTTPEndpointErrorCode500(t *testing.T) {
	testPaths := []string{
		"/v3/wrong-v3-endpoint-id",
		"/v3/",
		"/v3/stats",
		"/v3/wrong-v3-endpoint-id/task",
		"/v3/task",
	}

	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	state := mock_dockerstate.NewMockTaskEngineState(ctrl)
	auditLog := mock_audit.NewMockAuditLogger(ctrl)
	statsEngine := mock_stats.NewMockEngine(ctrl)
	ecsClient := mock_ecs.NewMockECSClient(ctrl)

	server, err := taskServerSetup(credentials.NewManager(), auditLog, state, ecsClient, clusterName, statsEngine,
		config.DefaultTaskMetadataSteadyStateRate, config.DefaultTaskMetadataBurstRate, "", vpcID,
		containerInstanceArn, tp.NewMockTaskProtectionClientFactoryInterface(ctrl))
	require.NoError(t, err)

	for _, testPath := range testPaths {
		t.Run(fmt.Sprintf("Test path: %s", testPath), func(t *testing.T) {
			// Make every possible call to state fail
			state.EXPECT().TaskARNByV3EndpointID(gomock.Any()).Return("", false).AnyTimes()
			state.EXPECT().DockerIDByV3EndpointID(gomock.Any()).Return("", false).AnyTimes()
			state.EXPECT().TaskARNByV3EndpointID(gomock.Any()).Return("", false).AnyTimes()
			state.EXPECT().GetTaskByIPAddress(gomock.Any()).Return("", false).AnyTimes()

			recorder := httptest.NewRecorder()
			req, _ := http.NewRequest("GET", testPath, nil)
			req.RemoteAddr = remoteIP + ":" + remotePort
			server.Handler.ServeHTTP(recorder, req)
			assert.Equal(t, http.StatusInternalServerError, recorder.Code)
		})
	}
}

// Tests that v4 metadata endpoints return a 404 error when the v3EndpointID in the request is invalid.
func TestV4TaskNotFoundError404(t *testing.T) {
	testCases := []struct {
		testPath     string
		expectedBody string
		taskFound    bool // Mock result for task lookup
	}{
		{
			testPath:     "/v4/bad/task",
			expectedBody: "\"V4 task metadata handler: unable to get task arn from request: unable to get task Arn from v3 endpoint ID: bad\"",
		},
		{
			testPath:     "/v4/bad/taskWithTags",
			expectedBody: "\"V4 task metadata handler: unable to get task arn from request: unable to get task Arn from v3 endpoint ID: bad\"",
		},
		{
			testPath:     "/v4/bad",
			expectedBody: "\"V4 container metadata handler: unable to get container ID from request: unable to get docker ID from v3 endpoint ID: bad\"",
		},
		{
			testPath:     "/v4/",
			expectedBody: "\"V4 container metadata handler: unable to get container ID from request: unable to get docker ID from v3 endpoint ID: \"",
		},
		{
			testPath:     "/v4/bad/stats",
			expectedBody: "\"V4 container stats handler: unable to get task arn from request: unable to get task Arn from v3 endpoint ID: bad\"",
		},
		{
			testPath:     "/v4/bad/stats",
			expectedBody: "\"V4 container stats handler: unable to get container ID from request: unable to get docker ID from v3 endpoint ID: bad\"",
			taskFound:    true,
		},
		{
			testPath:     "/v4/bad/task/stats",
			expectedBody: "\"V4 task stats handler: unable to get task arn from request: unable to get task Arn from v3 endpoint ID: bad\"",
		},
	}

	for _, tc := range testCases {
		t.Run(fmt.Sprintf("Test path: %s", tc.testPath), func(t *testing.T) {
			ctrl := gomock.NewController(t)
			defer ctrl.Finish()

			state := mock_dockerstate.NewMockTaskEngineState(ctrl)
			auditLog := mock_audit.NewMockAuditLogger(ctrl)
			statsEngine := mock_stats.NewMockEngine(ctrl)
			ecsClient := mock_ecs.NewMockECSClient(ctrl)

			server, err := taskServerSetup(credentials.NewManager(), auditLog, state, ecsClient, clusterName, statsEngine,
				config.DefaultTaskMetadataSteadyStateRate, config.DefaultTaskMetadataBurstRate, "", vpcID,
				containerInstanceArn, tp.NewMockTaskProtectionClientFactoryInterface(ctrl))
			require.NoError(t, err)

			state.EXPECT().TaskARNByV3EndpointID(gomock.Any()).Return("", tc.taskFound).AnyTimes()
			state.EXPECT().DockerIDByV3EndpointID(gomock.Any()).Return("", false).AnyTimes()

			recorder := httptest.NewRecorder()
			req, _ := http.NewRequest("GET", tc.testPath, nil)
			req.RemoteAddr = remoteIP + ":" + remotePort
			server.Handler.ServeHTTP(recorder, req)
			res, err := ioutil.ReadAll(recorder.Body)

			require.NoError(t, err)
			assert.Equal(t, http.StatusNotFound, recorder.Code)
			assert.Equal(t, tc.expectedBody, string(res))
		})
	}
}

// Tests that v4 metadata and stats endpoints return a 500 error on unexpected failures
// like tasks or container unexpectedly missing from the state.
func TestV4Unexpected500Error(t *testing.T) {
	testCases := []struct {
		testPath     string
		expectedBody string
	}{
		{
			testPath:     fmt.Sprintf("/v4/%s/stats", v3EndpointID),
			expectedBody: fmt.Sprintf("\"Unable to get container stats for: %s\"", containerID),
		},
		{
			testPath:     fmt.Sprintf("/v4/%s/task/stats", v3EndpointID),
			expectedBody: fmt.Sprintf("\"Unable to get task stats for: %s\"", taskARN),
		},
		{
			testPath:     fmt.Sprintf("/v4/%s", v3EndpointID),
			expectedBody: fmt.Sprintf("\"unable to generate metadata for container '%s'\"", containerID),
		},
		{
			testPath:     fmt.Sprintf("/v4/%s/task", v3EndpointID),
			expectedBody: fmt.Sprintf("\"Unable to generate metadata for v4 task: '%s'\"", taskARN),
		},
	}

	for _, tc := range testCases {
		t.Run(fmt.Sprintf("Test path: %s", tc.testPath), func(t *testing.T) {
			ctrl := gomock.NewController(t)
			defer ctrl.Finish()

			state := mock_dockerstate.NewMockTaskEngineState(ctrl)
			auditLog := mock_audit.NewMockAuditLogger(ctrl)
			statsEngine := mock_stats.NewMockEngine(ctrl)
			ecsClient := mock_ecs.NewMockECSClient(ctrl)

			server, err := taskServerSetup(credentials.NewManager(), auditLog, state, ecsClient, clusterName, statsEngine,
				config.DefaultTaskMetadataSteadyStateRate, config.DefaultTaskMetadataBurstRate, "", vpcID,
				containerInstanceArn, tp.NewMockTaskProtectionClientFactoryInterface(ctrl))
			require.NoError(t, err)

			// Initial lookups succeed
			state.EXPECT().TaskARNByV3EndpointID(v3EndpointID).Return(taskARN, true).AnyTimes()
			state.EXPECT().DockerIDByV3EndpointID(v3EndpointID).Return(containerID, true).AnyTimes()

			// Failures when getting metadata or stats
			state.EXPECT().ContainerMapByArn(taskARN).Return(nil, false).AnyTimes()
			state.EXPECT().ContainerByID(containerID).Return(nil, false).AnyTimes()
			state.EXPECT().TaskByArn(taskARN).Return(nil, false).AnyTimes()
			statsEngine.EXPECT().ContainerDockerStats(taskARN, containerID).
				Return(nil, nil, errors.New("failed")).AnyTimes()

			recorder := httptest.NewRecorder()
			req, _ := http.NewRequest("GET", tc.testPath, nil)
			req.RemoteAddr = remoteIP + ":" + remotePort
			server.Handler.ServeHTTP(recorder, req)
			res, err := ioutil.ReadAll(recorder.Body)

			require.NoError(t, err)
			assert.Equal(t, http.StatusInternalServerError, recorder.Code)
			assert.Equal(t, tc.expectedBody, string(res))
		})
	}
}

// Types of TMDS responses, add more types as needed
type TMDSResponse interface {
	v2.ContainerResponse |
		v2.TaskResponse |
		v4.ContainerResponse |
		v4.TaskResponse |
		tptypes.TaskProtectionResponse |
		types.StatsJSON |
		v4.StatsResponse |
		map[string]*types.StatsJSON |
		map[string]*v4.StatsResponse |
		string
}

// Represents a test case for TMDS. Supports generic TMDS response body types using type parametesrs.
type TMDSTestCase[R TMDSResponse] struct {
	// Request path
	path string
	// Method to use for the request, defaults to GET
	method string
	// Optional request body
	requestBody interface{}
	// Function to set expectations on mock task engine state
	setStateExpectations func(state *mock_dockerstate.MockTaskEngineState)
	// Function to set expectations on mock stats engine
	setStatsEngineExpectations func(engine *mock_stats.MockEngine)
	// Function to set expectations on mock ECS Client
	setECSClientExpectations func(ecsClient *mock_ecs.MockECSClient)
	// Function to set expectations on mock Task Protection Client Factory
	setTaskProtectionClientFactoryExpectations func(
		ctrl *gomock.Controller, factory *tp.MockTaskProtectionClientFactoryInterface)
	// Function to set expectations on mock Credentials Manager
	setCredentialsManagerExpectations func(credsManager *mock_credentials.MockManager)
	// Expected HTTP status code of the response
	expectedStatusCode int
	// Expected response body, all JSON compatible types are accepted
	expectedResponseBody R
}

// Tests a TMDS request as per the provided test case.
// This function can be used to test all metadata and stats endpoints.
// It -
// 1. Initializes a TMDS server
// 2. Creates a request as per the test case and sends it to the server
// 3. Unmarshals the JSON response body
// 4. Asserts that the response status code is as expected
// 5. Asserts that the unmarshaled resopnse body is as expected
func testTMDSRequest[R TMDSResponse](t *testing.T, tc TMDSTestCase[R]) {
	// Define mocks
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	state := mock_dockerstate.NewMockTaskEngineState(ctrl)
	auditLog := mock_audit.NewMockAuditLogger(ctrl)
	statsEngine := mock_stats.NewMockEngine(ctrl)
	ecsClient := mock_ecs.NewMockECSClient(ctrl)
	credsManager := mock_credentials.NewMockManager(ctrl)
	taskProtectionClientFactory := tp.NewMockTaskProtectionClientFactoryInterface(ctrl)

	// Set expectations on mocks
	auditLog.EXPECT().Log(gomock.Any(), gomock.Any(), gomock.Any()).AnyTimes()
	if tc.setStateExpectations != nil {
		tc.setStateExpectations(state)
	}
	if tc.setStatsEngineExpectations != nil {
		tc.setStatsEngineExpectations(statsEngine)
	}
	if tc.setECSClientExpectations != nil {
		tc.setECSClientExpectations(ecsClient)
	}
	if tc.setTaskProtectionClientFactoryExpectations != nil {
		tc.setTaskProtectionClientFactoryExpectations(ctrl, taskProtectionClientFactory)
	}
	if tc.setCredentialsManagerExpectations != nil {
		tc.setCredentialsManagerExpectations(credsManager)
	}

	// Initialize server
	server, err := taskServerSetup(credsManager, auditLog, state, ecsClient,
		clusterName, statsEngine,
		config.DefaultTaskMetadataSteadyStateRate, config.DefaultTaskMetadataBurstRate, availabilityzone, vpcID,
		containerInstanceArn, taskProtectionClientFactory)
	require.NoError(t, err)

	// Create the request
	var reqBody io.Reader
	if tc.requestBody != nil {
		reqBodyBytes, err := json.Marshal(tc.requestBody)
		require.NoError(t, err)
		reqBody = bytes.NewReader(reqBodyBytes)
	}
	if tc.method == "" {
		tc.method = "GET"
	}
	req, err := http.NewRequest(tc.method, tc.path, reqBody)
	require.NoError(t, err)
	req.RemoteAddr = remoteIP + ":" + remotePort

	// Send the request and record the response
	recorder := httptest.NewRecorder()
	server.Handler.ServeHTTP(recorder, req)

	// Parse the response body
	var actualResponseBody R
	err = json.Unmarshal(recorder.Body.Bytes(), &actualResponseBody)
	require.NoError(t, err, recorder.Body.String())

	// Assert status code and body
	assert.Equal(t, tc.expectedStatusCode, recorder.Code)
	assert.Equal(t, tc.expectedResponseBody, actualResponseBody)
}

// Tests for v2 container metadata endpoint
func TestV2ContainerMetadata(t *testing.T) {
	task := standardTask()

	t.Run("task not found by IP", func(t *testing.T) {
		testTMDSRequest(t, TMDSTestCase[string]{
			path: v2BaseMetadataPath + "/" + containerID,
			setStateExpectations: func(state *mock_dockerstate.MockTaskEngineState) {
				gomock.InOrder(
					state.EXPECT().GetTaskByIPAddress(remoteIP).Return("", false),
				)
			},
			expectedStatusCode: http.StatusBadRequest,
			expectedResponseBody: fmt.Sprintf(
				"Unable to get task arn from request: unable to associate '%s' with task",
				remoteIP),
		})
	})
	t.Run("invalid container ID", func(t *testing.T) {
		testTMDSRequest(t, TMDSTestCase[string]{
			path: v2BaseMetadataPath + "/" + containerID,
			setStateExpectations: func(state *mock_dockerstate.MockTaskEngineState) {
				gomock.InOrder(
					state.EXPECT().GetTaskByIPAddress(remoteIP).Return(taskARN, true),
					state.EXPECT().ContainerByID(containerID).Return(nil, false),
				)
			},
			expectedStatusCode: http.StatusBadRequest,
			expectedResponseBody: fmt.Sprintf(
				"Unable to generate metadata for container '%s'", containerID),
		})
	})
	t.Run("task not found but container ID is valid", func(t *testing.T) {
		testTMDSRequest(t, TMDSTestCase[string]{
			path: v2BaseMetadataPath + "/" + containerID,
			setStateExpectations: func(state *mock_dockerstate.MockTaskEngineState) {
				gomock.InOrder(
					state.EXPECT().GetTaskByIPAddress(remoteIP).Return(taskARN, true),
					state.EXPECT().ContainerByID(containerID).Return(dockerContainer, true),
					state.EXPECT().TaskByID(containerID).Return(nil, false),
				)
			},
			expectedStatusCode: http.StatusBadRequest,
			expectedResponseBody: fmt.Sprintf(
				"Unable to generate metadata for container '%s'", containerID),
		})
	})
	t.Run("happy case", func(t *testing.T) {
		testTMDSRequest(t, TMDSTestCase[v2.ContainerResponse]{
			path: v2BaseMetadataPath + "/" + containerID,
			setStateExpectations: func(state *mock_dockerstate.MockTaskEngineState) {
				gomock.InOrder(
					state.EXPECT().GetTaskByIPAddress(remoteIP).Return(taskARN, true),
					state.EXPECT().ContainerByID(containerID).Return(dockerContainer, true),
					state.EXPECT().TaskByID(containerID).Return(task, true),
				)
			},
			expectedStatusCode:   http.StatusOK,
			expectedResponseBody: expectedContainerResponse,
		})
	})
}

func TestV2TaskMetadata(t *testing.T) {
	task := standardTask()

	t.Run("task not found by IP", func(t *testing.T) {
		testTMDSRequest(t, TMDSTestCase[string]{
			path: v2BaseMetadataPath,
			setStateExpectations: func(state *mock_dockerstate.MockTaskEngineState) {
				gomock.InOrder(
					state.EXPECT().GetTaskByIPAddress(remoteIP).Return("", false),
				)
			},
			expectedStatusCode: http.StatusBadRequest,
			expectedResponseBody: fmt.Sprintf(
				"Unable to get task arn from request: unable to associate '%s' with task",
				remoteIP),
		})
	})
	t.Run("task not found by taskARN", func(t *testing.T) {
		testTMDSRequest(t, TMDSTestCase[string]{
			path: v2BaseMetadataPath,
			setStateExpectations: func(state *mock_dockerstate.MockTaskEngineState) {
				gomock.InOrder(
					state.EXPECT().GetTaskByIPAddress(remoteIP).Return(taskARN, true),
					state.EXPECT().TaskByArn(taskARN).Return(nil, false),
				)
			},
			expectedStatusCode: http.StatusBadRequest,
			expectedResponseBody: fmt.Sprintf(
				"Unable to generate metadata for task: '%s'",
				taskARN),
		})
	})
	t.Run("containerMap not found by ARN", func(t *testing.T) {
		testTMDSRequest(t, TMDSTestCase[v2.TaskResponse]{
			path: v2BaseMetadataPath,
			setStateExpectations: func(state *mock_dockerstate.MockTaskEngineState) {
				gomock.InOrder(
					state.EXPECT().GetTaskByIPAddress(remoteIP).Return(taskARN, true),
					state.EXPECT().TaskByArn(taskARN).Return(task, true),
					state.EXPECT().ContainerMapByArn(taskARN).Return(nil, false),
				)
			},
			expectedStatusCode:   http.StatusOK,
			expectedResponseBody: expectedTaskResponseNoContainers,
		})
	})
	t.Run("happy case", func(t *testing.T) {
		testTMDSRequest(t, TMDSTestCase[v2.TaskResponse]{
			path: v2BaseMetadataPath,
			setStateExpectations: func(state *mock_dockerstate.MockTaskEngineState) {
				gomock.InOrder(
					state.EXPECT().GetTaskByIPAddress(remoteIP).Return(taskARN, true),
					state.EXPECT().TaskByArn(taskARN).Return(task, true),
					state.EXPECT().ContainerMapByArn(taskARN).Return(containerNameToDockerContainer, true),
				)
			},
			expectedStatusCode:   http.StatusOK,
			expectedResponseBody: expectedTaskResponse(),
		})
	})
	t.Run("happy case with slash", func(t *testing.T) {
		testTMDSRequest(t, TMDSTestCase[v2.TaskResponse]{
			path: v2BaseMetadataPath + "/",
			setStateExpectations: func(state *mock_dockerstate.MockTaskEngineState) {
				gomock.InOrder(
					state.EXPECT().GetTaskByIPAddress(remoteIP).Return(taskARN, true),
					state.EXPECT().TaskByArn(taskARN).Return(task, true),
					state.EXPECT().ContainerMapByArn(taskARN).Return(containerNameToDockerContainer, true),
				)
			},
			expectedStatusCode:   http.StatusOK,
			expectedResponseBody: expectedTaskResponse(),
		})
	})
}

func TestV3ContainerMetadata(t *testing.T) {
	task := standardTask()

	t.Run("v3EndpointID invalid", func(t *testing.T) {
		testTMDSRequest(t, TMDSTestCase[string]{
			path: v3BasePath + v3EndpointID,
			setStateExpectations: func(state *mock_dockerstate.MockTaskEngineState) {
				gomock.InOrder(
					state.EXPECT().DockerIDByV3EndpointID(v3EndpointID).Return("", false),
				)
			},
			expectedStatusCode: http.StatusInternalServerError,
			expectedResponseBody: fmt.Sprintf(
				"V3 container metadata handler: unable to get container ID from request: unable to get docker ID from v3 endpoint ID: %s",
				v3EndpointID),
		})
	})
	t.Run("container not found but ID is valid", func(t *testing.T) {
		testTMDSRequest(t, TMDSTestCase[string]{
			path: v3BasePath + v3EndpointID,
			setStateExpectations: func(state *mock_dockerstate.MockTaskEngineState) {
				gomock.InOrder(
					state.EXPECT().DockerIDByV3EndpointID(v3EndpointID).Return(containerID, true),
					state.EXPECT().ContainerByID(containerID).Return(nil, false),
				)
			},
			expectedStatusCode: http.StatusInternalServerError,
			expectedResponseBody: fmt.Sprintf(
				"Unable to generate metadata for container '%s'", containerID),
		})
	})
	t.Run("happy case", func(t *testing.T) {
		testTMDSRequest(t, TMDSTestCase[v2.ContainerResponse]{
			path: v3BasePath + v3EndpointID,
			setStateExpectations: func(state *mock_dockerstate.MockTaskEngineState) {
				gomock.InOrder(
					state.EXPECT().DockerIDByV3EndpointID(v3EndpointID).Return(containerID, true),
					state.EXPECT().ContainerByID(containerID).Return(dockerContainer, true),
					state.EXPECT().TaskByID(containerID).Return(task, true),
				)
			},
			expectedStatusCode:   http.StatusOK,
			expectedResponseBody: expectedContainerResponse,
		})
	})
	t.Run("bridge mode container not found when looking up network settings", func(t *testing.T) {
		testTMDSRequest(t, TMDSTestCase[string]{
			path: v3BasePath + v3EndpointID,
			setStateExpectations: func(state *mock_dockerstate.MockTaskEngineState) {
				gomock.InOrder(
					state.EXPECT().DockerIDByV3EndpointID(v3EndpointID).Return(containerID, true),
					state.EXPECT().ContainerByID(containerID).Return(bridgeContainerNoNetwork, true),
					state.EXPECT().TaskByID(containerID).Return(bridgeTask, true),
					state.EXPECT().ContainerByID(containerID).Return(nil, false),
				)
			},
			expectedStatusCode:   http.StatusInternalServerError,
			expectedResponseBody: fmt.Sprintf("Unable to find container '%s'", containerID),
		})
	})
	t.Run("bridge mode container no network settings", func(t *testing.T) {
		testTMDSRequest(t, TMDSTestCase[string]{
			path: v3BasePath + v3EndpointID,
			setStateExpectations: func(state *mock_dockerstate.MockTaskEngineState) {
				gomock.InOrder(
					state.EXPECT().DockerIDByV3EndpointID(v3EndpointID).Return(containerID, true),
					state.EXPECT().ContainerByID(containerID).Return(bridgeContainerNoNetwork, true),
					state.EXPECT().TaskByID(containerID).Return(bridgeTask, true),
					state.EXPECT().ContainerByID(containerID).Return(bridgeContainerNoNetwork, true),
				)
			},
			expectedStatusCode: http.StatusInternalServerError,
			expectedResponseBody: fmt.Sprintf(
				"Unable to generate network response for container '%s'", containerID),
		})
	})
	t.Run("happy case bridge mode", func(t *testing.T) {
		testTMDSRequest(t, TMDSTestCase[v2.ContainerResponse]{
			path: v3BasePath + v3EndpointID,
			setStateExpectations: func(state *mock_dockerstate.MockTaskEngineState) {
				gomock.InOrder(
					state.EXPECT().DockerIDByV3EndpointID(v3EndpointID).Return(containerID, true),
					state.EXPECT().ContainerByID(containerID).Return(bridgeContainer, true),
					state.EXPECT().TaskByID(containerID).Return(bridgeTask, true),
					state.EXPECT().ContainerByID(containerID).Return(bridgeContainer, true),
				)
			},
			expectedStatusCode:   http.StatusOK,
			expectedResponseBody: expectedBridgeContainerResponse,
		})
	})
}

func TestV3TaskMetadata(t *testing.T) {
	task := standardTask()

	t.Run("taskARN not found for v3EndpointID", func(t *testing.T) {
		testTMDSRequest(t, TMDSTestCase[string]{
			path: v3BasePath + v3EndpointID + "/task",
			setStateExpectations: func(state *mock_dockerstate.MockTaskEngineState) {
				gomock.InOrder(
					state.EXPECT().TaskARNByV3EndpointID(v3EndpointID).Return("", false),
				)
			},
			expectedStatusCode: http.StatusInternalServerError,
			expectedResponseBody: fmt.Sprintf(
				"V3 task metadata handler: unable to get task arn from request: unable to get task Arn from v3 endpoint ID: %s",
				v3EndpointID),
		})
	})
	t.Run("task not found by taskARN", func(t *testing.T) {
		testTMDSRequest(t, TMDSTestCase[string]{
			path: v3BasePath + v3EndpointID + "/task",
			setStateExpectations: func(state *mock_dockerstate.MockTaskEngineState) {
				gomock.InOrder(
					state.EXPECT().TaskARNByV3EndpointID(v3EndpointID).Return(taskARN, true),
					state.EXPECT().TaskByArn(taskARN).Return(nil, false),
				)
			},
			expectedStatusCode: http.StatusInternalServerError,
			expectedResponseBody: fmt.Sprintf(
				"Unable to generate metadata for task: '%s'",
				taskARN),
		})
	})
	t.Run("containerMap not found for Arn", func(t *testing.T) {
		testTMDSRequest(t, TMDSTestCase[v2.TaskResponse]{
			path: v3BasePath + v3EndpointID + "/task",
			setStateExpectations: func(state *mock_dockerstate.MockTaskEngineState) {
				gomock.InOrder(
					state.EXPECT().TaskARNByV3EndpointID(v3EndpointID).Return(taskARN, true),
					state.EXPECT().TaskByArn(taskARN).Return(task, true),
					state.EXPECT().ContainerMapByArn(taskARN).Return(nil, false),
					state.EXPECT().TaskByArn(taskARN).Return(task, true),
				)
			},
			expectedStatusCode:   http.StatusOK,
			expectedResponseBody: expectedTaskResponseNoContainers,
		})
	})
	t.Run("happy case", func(t *testing.T) {
		testTMDSRequest(t, TMDSTestCase[v2.TaskResponse]{
			path: v3BasePath + v3EndpointID + "/task",
			setStateExpectations: func(state *mock_dockerstate.MockTaskEngineState) {
				gomock.InOrder(
					state.EXPECT().TaskARNByV3EndpointID(v3EndpointID).Return(taskARN, true),
					state.EXPECT().TaskByArn(taskARN).Return(task, true),
					state.EXPECT().ContainerMapByArn(taskARN).Return(containerNameToDockerContainer, true),
					state.EXPECT().TaskByArn(taskARN).Return(task, true),
				)
			},
			expectedStatusCode:   http.StatusOK,
			expectedResponseBody: expectedTaskResponse(),
		})
	})
	t.Run("bridge mode container not found", func(t *testing.T) {
		testTMDSRequest(t, TMDSTestCase[string]{
			path: v3BasePath + v3EndpointID + "/task",
			setStateExpectations: func(state *mock_dockerstate.MockTaskEngineState) {
				gomock.InOrder(
					state.EXPECT().TaskARNByV3EndpointID(v3EndpointID).Return(taskARN, true),
					state.EXPECT().TaskByArn(taskARN).Return(bridgeTask, true),
					state.EXPECT().ContainerMapByArn(taskARN).Return(containerNameToBridgeContainer, true),
					state.EXPECT().TaskByArn(taskARN).Return(bridgeTask, true),
					state.EXPECT().ContainerByID(containerID).Return(nil, false),
				)
			},
			expectedStatusCode:   http.StatusInternalServerError,
			expectedResponseBody: fmt.Sprintf("Unable to find container '%s'", containerID),
		})
	})
	t.Run("bridge mode container no network settings", func(t *testing.T) {
		testTMDSRequest(t, TMDSTestCase[string]{
			path: v3BasePath + v3EndpointID + "/task",
			setStateExpectations: func(state *mock_dockerstate.MockTaskEngineState) {
				gomock.InOrder(
					state.EXPECT().TaskARNByV3EndpointID(v3EndpointID).Return(taskARN, true),
					state.EXPECT().TaskByArn(taskARN).Return(bridgeTask, true),
					state.EXPECT().ContainerMapByArn(taskARN).Return(containerNameToBridgeContainer, true),
					state.EXPECT().TaskByArn(taskARN).Return(bridgeTask, true),
					state.EXPECT().ContainerByID(containerID).Return(bridgeContainerNoNetwork, true),
				)
			},
			expectedStatusCode: http.StatusInternalServerError,
			expectedResponseBody: fmt.Sprintf(
				"Unable to generate network response for container '%s'", containerID),
		})
	})
	t.Run("happy case bridge mode", func(t *testing.T) {
		testTMDSRequest(t, TMDSTestCase[v2.TaskResponse]{
			path: v3BasePath + v3EndpointID + "/task",
			setStateExpectations: func(state *mock_dockerstate.MockTaskEngineState) {
				gomock.InOrder(
					state.EXPECT().TaskARNByV3EndpointID(v3EndpointID).Return(taskARN, true),
					state.EXPECT().TaskByArn(taskARN).Return(bridgeTask, true),
					state.EXPECT().ContainerMapByArn(taskARN).Return(containerNameToBridgeContainer, true),
					state.EXPECT().TaskByArn(taskARN).Return(bridgeTask, true),
					state.EXPECT().ContainerByID(containerID).Return(bridgeContainer, true),
				)
			},
			expectedStatusCode:   http.StatusOK,
			expectedResponseBody: expectedBridgeTaskResponse,
		})
	})
}

func TestV4ContainerMetadata(t *testing.T) {
	task := standardTask()

	t.Run("v3EndpointID is invalid", func(t *testing.T) {
		testTMDSRequest(t, TMDSTestCase[string]{
			path: v4BasePath + v3EndpointID,
			setStateExpectations: func(state *mock_dockerstate.MockTaskEngineState) {
				gomock.InOrder(
					state.EXPECT().DockerIDByV3EndpointID(v3EndpointID).Return("", false),
				)
			},
			expectedStatusCode: http.StatusNotFound,
			expectedResponseBody: fmt.Sprintf(
				"V4 container metadata handler: unable to get container ID from request: unable to get docker ID from v3 endpoint ID: %s",
				v3EndpointID),
		})
	})
	t.Run("container not found", func(t *testing.T) {
		testTMDSRequest(t, TMDSTestCase[string]{
			path: v4BasePath + v3EndpointID,
			setStateExpectations: func(state *mock_dockerstate.MockTaskEngineState) {
				gomock.InOrder(
					state.EXPECT().DockerIDByV3EndpointID(v3EndpointID).Return(containerID, true),
					state.EXPECT().ContainerByID(containerID).Return(nil, false),
				)
			},
			expectedStatusCode: http.StatusInternalServerError,
			expectedResponseBody: fmt.Sprintf(
				"unable to generate metadata for container '%s'", containerID),
		})
	})
	t.Run("task not found", func(t *testing.T) {
		testTMDSRequest(t, TMDSTestCase[string]{
			path: v4BasePath + v3EndpointID,
			setStateExpectations: func(state *mock_dockerstate.MockTaskEngineState) {
				gomock.InOrder(
					state.EXPECT().DockerIDByV3EndpointID(v3EndpointID).Return(containerID, true),
					state.EXPECT().ContainerByID(containerID).Return(dockerContainer, true),
					state.EXPECT().TaskByID(containerID).Return(nil, false),
				)
			},
			expectedStatusCode: http.StatusInternalServerError,
			expectedResponseBody: fmt.Sprintf(
				"unable to generate metadata for container '%s'", containerID),
		})
	})
	t.Run("awsvpc task not found on second lookup", func(t *testing.T) {
		testTMDSRequest(t, TMDSTestCase[string]{
			path: v4BasePath + v3EndpointID,
			setStateExpectations: func(state *mock_dockerstate.MockTaskEngineState) {
				gomock.InOrder(
					state.EXPECT().DockerIDByV3EndpointID(v3EndpointID).Return(containerID, true),
					state.EXPECT().ContainerByID(containerID).Return(dockerContainer, true),
					state.EXPECT().TaskByID(containerID).Return(task, true),
					state.EXPECT().TaskByID(containerID).Return(nil, false),
				)
			},
			expectedStatusCode: http.StatusInternalServerError,
			expectedResponseBody: fmt.Sprintf(
				"unable to generate metadata for container '%s'", containerID),
		})
	})
	t.Run("happy case awsvpc task", func(t *testing.T) {
		testTMDSRequest(t, TMDSTestCase[v4.ContainerResponse]{
			path: v4BasePath + v3EndpointID,
			setStateExpectations: func(state *mock_dockerstate.MockTaskEngineState) {
				gomock.InOrder(
					state.EXPECT().DockerIDByV3EndpointID(v3EndpointID).Return(containerID, true),
					state.EXPECT().ContainerByID(containerID).Return(dockerContainer, true).AnyTimes(),
					state.EXPECT().TaskByID(containerID).Return(task, true).Times(2),
					state.EXPECT().ContainerByID(containerID).Return(dockerContainer, true).AnyTimes(),
				)
			},
			expectedStatusCode:   http.StatusOK,
			expectedResponseBody: expectedV4ContainerResponse,
		})
	})
	t.Run("happy case awsvpc task - dual stack case", func(t *testing.T) {
		testTMDSRequest(t, TMDSTestCase[v4.ContainerResponse]{
			path: v4BasePath + v3EndpointID,
			setStateExpectations: func(state *mock_dockerstate.MockTaskEngineState) {
				task := standardTask()
				task.ENIs[0].SubnetGatewayIPV4Address = "1.2.3.1/24"
				task.ENIs[0].SubnetGatewayIPV6Address = "8:3:1:1:1::/64"
				task.ENIs[0].IPV4Addresses = []*ni.IPV4Address{{Address: "1.2.3.5"}}
				task.ENIs[0].IPV6Addresses = []*ni.IPV6Address{{Address: "8:3:1:1:a::"}}
				gomock.InOrder(
					state.EXPECT().DockerIDByV3EndpointID(v3EndpointID).Return(containerID, true),
					state.EXPECT().ContainerByID(containerID).Return(dockerContainer, true).AnyTimes(),
					state.EXPECT().TaskByID(containerID).Return(task, true).Times(2),
					state.EXPECT().ContainerByID(containerID).Return(dockerContainer, true).AnyTimes(),
				)
			},
			expectedStatusCode: http.StatusOK,
			expectedResponseBody: func() v4.ContainerResponse {
				response := standardV4ContainerResponseAWSVPC()
				response.Networks[0].SubnetGatewayIPV4Address = "1.2.3.1/24"
				// subnet gateway IPv6 address not populated for dual-stack ENIs for historical reasons
				response.Networks[0].SubnetGatewayIPV6Address = ""
				response.Networks[0].IPV4SubnetCIDRBlock = "1.2.3.0/24"
				response.Networks[0].IPv6SubnetCIDRBlock = "8:3:1:1::/64"
				response.Networks[0].IPv4Addresses = []string{"1.2.3.5"}
				response.Networks[0].IPv6Addresses = []string{"8:3:1:1:a::"}
				return *response
			}(),
		})
	})
	t.Run("happy case awsvpc task - IPv6-only case", func(t *testing.T) {
		testTMDSRequest(t, TMDSTestCase[v4.ContainerResponse]{
			path: v4BasePath + v3EndpointID,
			setStateExpectations: func(state *mock_dockerstate.MockTaskEngineState) {
				task := standardTask()
				task.ENIs[0].SubnetGatewayIPV4Address = ""
				task.ENIs[0].SubnetGatewayIPV6Address = "8:3:1:1::/48"
				task.ENIs[0].IPV4Addresses = nil
				task.ENIs[0].IPV6Addresses = []*ni.IPV6Address{{Address: "8:3:1:9::"}}
				gomock.InOrder(
					state.EXPECT().DockerIDByV3EndpointID(v3EndpointID).Return(containerID, true),
					state.EXPECT().ContainerByID(containerID).Return(dockerContainer, true).AnyTimes(),
					state.EXPECT().TaskByID(containerID).Return(task, true).Times(2),
					state.EXPECT().ContainerByID(containerID).Return(dockerContainer, true).AnyTimes(),
				)
			},
			expectedStatusCode: http.StatusOK,
			expectedResponseBody: func() v4.ContainerResponse {
				response := standardV4ContainerResponseAWSVPC()
				response.Networks[0].SubnetGatewayIPV4Address = ""
				response.Networks[0].SubnetGatewayIPV6Address = "8:3:1:1::/48"
				response.Networks[0].IPV4SubnetCIDRBlock = ""
				response.Networks[0].IPv6SubnetCIDRBlock = "8:3:1::/48"
				response.Networks[0].IPv4Addresses = nil
				response.Networks[0].IPv6Addresses = []string{"8:3:1:9::"}
				return *response
			}(),
		})
	})
	t.Run("bridge mode container not found during network population", func(t *testing.T) {
		testTMDSRequest(t, TMDSTestCase[string]{
			path: v4BasePath + v3EndpointID,
			setStateExpectations: func(state *mock_dockerstate.MockTaskEngineState) {
				gomock.InOrder(
					state.EXPECT().DockerIDByV3EndpointID(v3EndpointID).Return(containerID, true),
					state.EXPECT().ContainerByID(containerID).Return(bridgeContainer, true),
					state.EXPECT().TaskByID(containerID).Return(bridgeTask, true),
					state.EXPECT().ContainerByID(containerID).Return(nil, false).AnyTimes(),
				)
			},
			expectedStatusCode:   http.StatusInternalServerError,
			expectedResponseBody: fmt.Sprintf("unable to find container '%s'", containerID),
		})
	})
	t.Run("bridge mode no network settings", func(t *testing.T) {
		testTMDSRequest(t, TMDSTestCase[string]{
			path: v4BasePath + v3EndpointID,
			setStateExpectations: func(state *mock_dockerstate.MockTaskEngineState) {
				gomock.InOrder(
					state.EXPECT().DockerIDByV3EndpointID(v3EndpointID).Return(containerID, true),
					state.EXPECT().ContainerByID(containerID).Return(bridgeContainerNoNetwork, true),
					state.EXPECT().TaskByID(containerID).Return(bridgeTask, true),
					state.EXPECT().ContainerByID(containerID).Return(bridgeContainerNoNetwork, true).AnyTimes(),
				)
			},
			expectedStatusCode: http.StatusInternalServerError,
			expectedResponseBody: fmt.Sprintf(
				"unable to generate network response for container '%s'", containerID),
		})
	})
	t.Run("happy case bridge mode", func(t *testing.T) {
		testTMDSRequest(t, TMDSTestCase[v4.ContainerResponse]{
			path: v4BasePath + v3EndpointID,
			setStateExpectations: func(state *mock_dockerstate.MockTaskEngineState) {
				gomock.InOrder(
					state.EXPECT().DockerIDByV3EndpointID(v3EndpointID).Return(containerID, true),
					state.EXPECT().ContainerByID(containerID).Return(bridgeContainer, true).AnyTimes(),
					state.EXPECT().TaskByID(containerID).Return(bridgeTask, true),
					state.EXPECT().ContainerByID(containerID).Return(bridgeContainer, true).AnyTimes(),
				)
			},
			expectedStatusCode:   http.StatusOK,
			expectedResponseBody: expectedV4BridgeContainerResponse,
		})
	})
	t.Run("happy case bridge mode including IPv6 from network settings", func(t *testing.T) {
		bridgeTask := standardBridgeTask()
		bridgeContainer := standardBridgeDockerContainer()
		networkSettings := bridgeContainer.Container.GetNetworkSettings()
		networkSettings.GlobalIPv6Address = "5:6:7:8::"
		bridgeContainer.Container.SetNetworkSettings(networkSettings)

		expectedResponse := standardV4BridgeContainerResponse()
		expectedNetwork := expectedResponse.Networks[0]
		expectedNetwork.Network.IPv6Addresses = []string{"5:6:7:8::"}
		expectedResponse.Networks = []v4.Network{{Network: expectedNetwork.Network}}

		testTMDSRequest(t, TMDSTestCase[v4.ContainerResponse]{
			path: v4BasePath + v3EndpointID,
			setStateExpectations: func(state *mock_dockerstate.MockTaskEngineState) {
				state.EXPECT().ContainerByID(containerID).Return(bridgeContainer, true).AnyTimes()
				state.EXPECT().DockerIDByV3EndpointID(v3EndpointID).Return(containerID, true)
				state.EXPECT().TaskByID(containerID).Return(bridgeTask, true)
			},
			expectedStatusCode:   http.StatusOK,
			expectedResponseBody: *expectedResponse,
		})
	})
	t.Run("happy case bridge mode including IPv6 from docker networks", func(t *testing.T) {
		bridgeTask := standardBridgeTask()
		bridgeContainer := standardBridgeDockerContainer()
		bridgeContainer.Container.SetNetworkSettings(&types.NetworkSettings{
			Networks: map[string]*network.EndpointSettings{
				"bridge": {IPAddress: "1.2.3.4", GlobalIPv6Address: "5:6:7:8::"},
			},
		})

		expectedResponse := standardV4BridgeContainerResponse()
		expectedNetwork := expectedResponse.Networks[0]
		expectedNetwork.Network.IPv4Addresses = []string{"1.2.3.4"}
		expectedNetwork.Network.IPv6Addresses = []string{"5:6:7:8::"}
		expectedResponse.Networks = []v4.Network{{Network: expectedNetwork.Network}}

		testTMDSRequest(t, TMDSTestCase[v4.ContainerResponse]{
			path: v4BasePath + v3EndpointID,
			setStateExpectations: func(state *mock_dockerstate.MockTaskEngineState) {
				state.EXPECT().ContainerByID(containerID).Return(bridgeContainer, true).AnyTimes()
				state.EXPECT().DockerIDByV3EndpointID(v3EndpointID).Return(containerID, true)
				state.EXPECT().TaskByID(containerID).Return(bridgeTask, true)
			},
			expectedStatusCode:   http.StatusOK,
			expectedResponseBody: *expectedResponse,
		})
	})
}

func TestV4TaskMetadata(t *testing.T) {
	task := standardTask()

	t.Run("taskARN not found for v3EndpointID", func(t *testing.T) {
		testTMDSRequest(t, TMDSTestCase[string]{
			path: v4BasePath + v3EndpointID + "/task",
			setStateExpectations: func(state *mock_dockerstate.MockTaskEngineState) {
				gomock.InOrder(
					state.EXPECT().TaskARNByV3EndpointID(v3EndpointID).Return("", false),
				)
			},
			expectedStatusCode: http.StatusNotFound,
			expectedResponseBody: fmt.Sprintf(
				"V4 task metadata handler: unable to get task arn from request: unable to get task Arn from v3 endpoint ID: %s",
				v3EndpointID),
		})
	})
	t.Run("task not found for taskARN", func(t *testing.T) {
		testTMDSRequest(t, TMDSTestCase[string]{
			path: v4BasePath + v3EndpointID + "/task",
			setStateExpectations: func(state *mock_dockerstate.MockTaskEngineState) {
				gomock.InOrder(
					state.EXPECT().TaskARNByV3EndpointID(v3EndpointID).Return(taskARN, true),
					state.EXPECT().TaskByArn(taskARN).Return(nil, false),
				)
			},
			expectedStatusCode:   http.StatusInternalServerError,
			expectedResponseBody: fmt.Sprintf("Unable to generate metadata for v4 task: '%s'", taskARN),
		})
	})
	t.Run("task not found on second lookup", func(t *testing.T) {
		testTMDSRequest(t, TMDSTestCase[string]{
			path: v4BasePath + v3EndpointID + "/task",
			setStateExpectations: func(state *mock_dockerstate.MockTaskEngineState) {
				gomock.InOrder(
					state.EXPECT().TaskARNByV3EndpointID(v3EndpointID).Return(taskARN, true),
					state.EXPECT().TaskByArn(taskARN).Return(task, true),
					state.EXPECT().TaskByArn(taskARN).Return(nil, false),
				)
			},
			expectedStatusCode:   http.StatusInternalServerError,
			expectedResponseBody: fmt.Sprintf("Unable to generate metadata for v4 task: '%s'", taskARN),
		})
	})
	t.Run("containers not found for taskARN", func(t *testing.T) {
		testTMDSRequest(t, TMDSTestCase[v4.TaskResponse]{
			path: v4BasePath + v3EndpointID + "/task",
			setStateExpectations: func(state *mock_dockerstate.MockTaskEngineState) {
				gomock.InOrder(
					state.EXPECT().TaskARNByV3EndpointID(v3EndpointID).Return(taskARN, true),
					state.EXPECT().TaskByArn(taskARN).Return(task, true).Times(2),
					state.EXPECT().ContainerMapByArn(taskARN).Return(nil, false),
					state.EXPECT().PulledContainerMapByArn(taskARN).Return(nil, true),
				)
			},
			expectedStatusCode:   http.StatusOK,
			expectedResponseBody: expectedV4TaskResponseNoContainers(),
		})
	})
	t.Run("task not found on third lookup", func(t *testing.T) {
		testTMDSRequest(t, TMDSTestCase[string]{
			path: v4BasePath + v3EndpointID + "/task",
			setStateExpectations: func(state *mock_dockerstate.MockTaskEngineState) {
				gomock.InOrder(
					state.EXPECT().TaskARNByV3EndpointID(v3EndpointID).Return(taskARN, true),
					state.EXPECT().TaskByArn(taskARN).Return(task, true).Times(2),
					state.EXPECT().ContainerMapByArn(taskARN).Return(containerNameToDockerContainer, true),
					state.EXPECT().TaskByArn(taskARN).Return(nil, false),
				)
			},
			expectedStatusCode: http.StatusInternalServerError,
			expectedResponseBody: fmt.Sprintf(
				"Unable to generate metadata for v4 task: '%s'", taskARN),
		})
	})
	t.Run("happy case", func(t *testing.T) {
		testTMDSRequest(t, TMDSTestCase[v4.TaskResponse]{
			path: v4BasePath + v3EndpointID + "/task",
			setStateExpectations: func(state *mock_dockerstate.MockTaskEngineState) {
				gomock.InOrder(
					state.EXPECT().TaskARNByV3EndpointID(v3EndpointID).Return(taskARN, true),
					state.EXPECT().TaskByArn(taskARN).Return(task, true).Times(2),
					state.EXPECT().ContainerByID(containerID).Return(dockerContainer, true).AnyTimes(),
					state.EXPECT().ContainerMapByArn(taskARN).Return(containerNameToDockerContainer, true),
					state.EXPECT().TaskByArn(taskARN).Return(task, true),
					state.EXPECT().ContainerByID(containerID).Return(dockerContainer, true).AnyTimes(),
					state.EXPECT().PulledContainerMapByArn(taskARN).Return(nil, true),
				)
			},
			expectedStatusCode:   http.StatusOK,
			expectedResponseBody: expectedV4TaskResponse(),
		})
	})
	t.Run("happy case pulled containers", func(t *testing.T) {
		testTMDSRequest(t, TMDSTestCase[v4.TaskResponse]{
			path: v4BasePath + v3EndpointID + "/task",
			setStateExpectations: func(state *mock_dockerstate.MockTaskEngineState) {
				gomock.InOrder(
					state.EXPECT().TaskARNByV3EndpointID(v3EndpointID).Return(taskARN, true),
					state.EXPECT().TaskByArn(taskARN).Return(pulledTask, true).Times(2),
					state.EXPECT().ContainerByID(containerID).Return(dockerContainer, true).AnyTimes(),
					state.EXPECT().ContainerMapByArn(taskARN).Return(containerNameToDockerContainer, true),
					state.EXPECT().TaskByArn(taskARN).Return(pulledTask, true),
					state.EXPECT().ContainerByID(containerID).Return(dockerContainer, true).AnyTimes(),
					state.EXPECT().PulledContainerMapByArn(taskARN).Return(pulledContainerNameToDockerContainer, true),
				)
			},
			expectedStatusCode:   http.StatusOK,
			expectedResponseBody: expectedV4PulledTaskResponse(),
		})
	})

	t.Run("happy case with fault injection enabled using awsvpc mode", func(t *testing.T) {
		testTMDSRequest(t, TMDSTestCase[v4.TaskResponse]{
			path: v4BasePath + v3EndpointID + "/task",
			setStateExpectations: func(state *mock_dockerstate.MockTaskEngineState) {
				task.EnableFaultInjection = true
				task.NetworkNamespace = networkNamespace
				task.DefaultIfname = defaultIfname
				gomock.InOrder(
					state.EXPECT().TaskARNByV3EndpointID(v3EndpointID).Return(taskARN, true),
					state.EXPECT().TaskByArn(taskARN).Return(task, true).Times(2),
					state.EXPECT().ContainerByID(containerID).Return(dockerContainer, true).AnyTimes(),
					state.EXPECT().ContainerMapByArn(taskARN).Return(containerNameToDockerContainer, true),
					state.EXPECT().TaskByArn(taskARN).Return(task, true),
					state.EXPECT().ContainerByID(containerID).Return(dockerContainer, true).AnyTimes(),
					state.EXPECT().PulledContainerMapByArn(taskARN).Return(nil, true),
				)
			},
			expectedStatusCode:   http.StatusOK,
			expectedResponseBody: expectedV4TaskResponseWithFaultInjectionEnabled(),
		})
	})

	t.Run("happy case with fault injection enabled using host mode", func(t *testing.T) {
		testTMDSRequest(t, TMDSTestCase[v4.TaskResponse]{
			path: v4BasePath + v3EndpointID + "/task",
			setStateExpectations: func(state *mock_dockerstate.MockTaskEngineState) {
				hostTask := standardHostTask()
				hostTask.EnableFaultInjection = true
				hostTask.NetworkNamespace = networkNamespace
				hostTask.DefaultIfname = defaultIfname
				gomock.InOrder(
					state.EXPECT().TaskARNByV3EndpointID(v3EndpointID).Return(taskARN, true),
					state.EXPECT().TaskByArn(taskARN).Return(hostTask, true).Times(2),
					state.EXPECT().ContainerMapByArn(taskARN).Return(containerNameToDockerContainer, true),
					state.EXPECT().ContainerByID(containerID).Return(nil, false).AnyTimes(),
					state.EXPECT().PulledContainerMapByArn(taskARN).Return(nil, true),
					state.EXPECT().ContainerByID(containerID).Return(nil, false).AnyTimes(),
				)
			},
			expectedStatusCode:   http.StatusOK,
			expectedResponseBody: expectedV4TaskResponseHostModeWithFaultInjectionEnabled(),
		})
	})

	t.Run("bridge mode container not found", func(t *testing.T) {
		testTMDSRequest(t, TMDSTestCase[v4.TaskResponse]{
			path: v4BasePath + v3EndpointID + "/task",
			setStateExpectations: func(state *mock_dockerstate.MockTaskEngineState) {
				gomock.InOrder(
					state.EXPECT().TaskARNByV3EndpointID(v3EndpointID).Return(taskARN, true),
					state.EXPECT().TaskByArn(taskARN).Return(bridgeTask, true).Times(2),
					state.EXPECT().ContainerMapByArn(taskARN).Return(containerNameToBridgeContainer, true),
					state.EXPECT().ContainerByID(containerID).Return(nil, false).AnyTimes(),
					state.EXPECT().PulledContainerMapByArn(taskARN).Return(nil, true),
					state.EXPECT().ContainerByID(containerID).Return(nil, false).AnyTimes(),
				)
			},
			expectedStatusCode:   http.StatusOK,
			expectedResponseBody: expectedV4BridgeTaskResponseNoNetwork(),
		})
	})
	t.Run("bridge mode container no network settings", func(t *testing.T) {
		testTMDSRequest(t, TMDSTestCase[v4.TaskResponse]{
			path: v4BasePath + v3EndpointID + "/task",
			setStateExpectations: func(state *mock_dockerstate.MockTaskEngineState) {
				gomock.InOrder(
					state.EXPECT().TaskARNByV3EndpointID(v3EndpointID).Return(taskARN, true),
					state.EXPECT().TaskByArn(taskARN).Return(bridgeTask, true).Times(2),
					state.EXPECT().ContainerMapByArn(taskARN).Return(containerNameToBridgeContainer, true),
					state.EXPECT().ContainerByID(containerID).Return(bridgeContainerNoNetwork, true).AnyTimes(),
					state.EXPECT().PulledContainerMapByArn(taskARN).Return(nil, true),
					state.EXPECT().ContainerByID(containerID).Return(bridgeContainerNoNetwork, true).AnyTimes(),
				)
			},
			expectedStatusCode:   http.StatusOK,
			expectedResponseBody: expectedV4BridgeTaskResponseNoNetwork(),
		})
	})
	t.Run("happy case bridge mode", func(t *testing.T) {
		testTMDSRequest(t, TMDSTestCase[v4.TaskResponse]{
			path: v4BasePath + v3EndpointID + "/task",
			setStateExpectations: func(state *mock_dockerstate.MockTaskEngineState) {
				gomock.InOrder(
					state.EXPECT().TaskARNByV3EndpointID(v3EndpointID).Return(taskARN, true),
					state.EXPECT().TaskByArn(taskARN).Return(bridgeTask, true).Times(2),
					state.EXPECT().ContainerMapByArn(taskARN).Return(containerNameToBridgeContainer, true),
					state.EXPECT().ContainerByID(containerID).Return(bridgeContainer, true).AnyTimes(),
					state.EXPECT().PulledContainerMapByArn(taskARN).Return(nil, true),
					state.EXPECT().ContainerByID(containerID).Return(bridgeContainer, true).AnyTimes(),
				)
			},
			expectedStatusCode:   http.StatusOK,
			expectedResponseBody: expectedV4BridgeTaskResponse(),
		})
	})
}

func TestV2TaskMetadataWithTags(t *testing.T) {
	task := standardTask()

	containerInstanceTags := standardContainerInstanceTags()
	taskTags := standardTaskTags()

	ecsInstanceTags := standardECSContainerInstanceTags()
	ecsTaskTags := standardECSTaskTags()

	happyStateExpectations := func(state *mock_dockerstate.MockTaskEngineState) {
		gomock.InOrder(
			state.EXPECT().GetTaskByIPAddress(remoteIP).Return(taskARN, true),
			state.EXPECT().TaskByArn(taskARN).Return(task, true),
			state.EXPECT().ContainerMapByArn(taskARN).Return(containerNameToDockerContainer, true),
		)
	}

	happyCasePaths := []string{v2BaseMetadataWithTagsPath, v2BaseMetadataWithTagsPath + "/"}
	for _, path := range happyCasePaths {
		t.Run("happy case "+path, func(t *testing.T) {
			expectedTaskResponseWithTags := expectedTaskResponse()
			expectedTaskResponseWithTags.ContainerInstanceTags = containerInstanceTags
			expectedTaskResponseWithTags.TaskTags = taskTags

			testTMDSRequest(t, TMDSTestCase[v2.TaskResponse]{
				path:                 path,
				setStateExpectations: happyStateExpectations,
				setECSClientExpectations: func(ecsClient *mock_ecs.MockECSClient) {
					gomock.InOrder(
						ecsClient.EXPECT().GetResourceTags(containerInstanceArn).
							Return(ecsInstanceTags, nil),
						ecsClient.EXPECT().GetResourceTags(taskARN).Return(ecsTaskTags, nil),
					)
				},
				expectedStatusCode:   http.StatusOK,
				expectedResponseBody: expectedTaskResponseWithTags,
			})
		})
	}

	t.Run("failed to get task tags", func(t *testing.T) {
		expectedTaskResponseWithTags := expectedTaskResponse()
		expectedTaskResponseWithTags.ContainerInstanceTags = containerInstanceTags
		testTMDSRequest(t, TMDSTestCase[v2.TaskResponse]{
			path:                 v2BaseMetadataWithTagsPath,
			setStateExpectations: happyStateExpectations,
			setECSClientExpectations: func(ecsClient *mock_ecs.MockECSClient) {
				gomock.InOrder(
					ecsClient.EXPECT().GetResourceTags(containerInstanceArn).Return(ecsInstanceTags, nil),
					ecsClient.EXPECT().GetResourceTags(taskARN).Return(nil, errors.New("error")),
				)
			},
			expectedStatusCode:   http.StatusOK,
			expectedResponseBody: expectedTaskResponseWithTags,
		})
	})
	t.Run("failed to get container instance tags", func(t *testing.T) {
		expectedTaskResponseWithTags := expectedTaskResponse()
		expectedTaskResponseWithTags.TaskTags = taskTags
		testTMDSRequest(t, TMDSTestCase[v2.TaskResponse]{
			path:                 v2BaseMetadataWithTagsPath,
			setStateExpectations: happyStateExpectations,
			setECSClientExpectations: func(ecsClient *mock_ecs.MockECSClient) {
				gomock.InOrder(
					ecsClient.EXPECT().GetResourceTags(containerInstanceArn).Return(nil, errors.New("error")),
					ecsClient.EXPECT().GetResourceTags(taskARN).Return(ecsTaskTags, nil),
				)
			},
			expectedStatusCode:   http.StatusOK,
			expectedResponseBody: expectedTaskResponseWithTags,
		})
	})
	t.Run("failed to get container instance and task tags", func(t *testing.T) {
		testTMDSRequest(t, TMDSTestCase[v2.TaskResponse]{
			path:                 v2BaseMetadataWithTagsPath,
			setStateExpectations: happyStateExpectations,
			setECSClientExpectations: func(ecsClient *mock_ecs.MockECSClient) {
				gomock.InOrder(
					ecsClient.EXPECT().GetResourceTags(containerInstanceArn).Return(nil, errors.New("error")),
					ecsClient.EXPECT().GetResourceTags(taskARN).Return(nil, errors.New("error")),
				)
			},
			expectedStatusCode:   http.StatusOK,
			expectedResponseBody: expectedTaskResponse(),
		})
	})
	t.Run("task not found by IP", func(t *testing.T) {
		testTMDSRequest(t, TMDSTestCase[string]{
			path: v2BaseMetadataWithTagsPath,
			setStateExpectations: func(state *mock_dockerstate.MockTaskEngineState) {
				gomock.InOrder(
					state.EXPECT().GetTaskByIPAddress(remoteIP).Return("", false),
				)
			},
			expectedStatusCode: http.StatusBadRequest,
			expectedResponseBody: fmt.Sprintf(
				"Unable to get task arn from request: unable to associate '%s' with task",
				remoteIP),
		})
	})
	t.Run("task not found by taskARN", func(t *testing.T) {
		testTMDSRequest(t, TMDSTestCase[string]{
			path: v2BaseMetadataWithTagsPath,
			setStateExpectations: func(state *mock_dockerstate.MockTaskEngineState) {
				gomock.InOrder(
					state.EXPECT().GetTaskByIPAddress(remoteIP).Return(taskARN, true),
					state.EXPECT().TaskByArn(taskARN).Return(nil, false),
				)
			},
			expectedStatusCode: http.StatusBadRequest,
			expectedResponseBody: fmt.Sprintf(
				"Unable to generate metadata for task: '%s'",
				taskARN),
		})
	})
	t.Run("containerMap not found by ARN", func(t *testing.T) {
		testTMDSRequest(t, TMDSTestCase[v2.TaskResponse]{
			path: v2BaseMetadataWithTagsPath,
			setStateExpectations: func(state *mock_dockerstate.MockTaskEngineState) {
				gomock.InOrder(
					state.EXPECT().GetTaskByIPAddress(remoteIP).Return(taskARN, true),
					state.EXPECT().TaskByArn(taskARN).Return(task, true),
					state.EXPECT().ContainerMapByArn(taskARN).Return(nil, false),
				)
			},
			expectedStatusCode:   http.StatusOK,
			expectedResponseBody: expectedTaskResponseNoContainers,
		})
	})
}

func TestV3TaskMetadataWithTags(t *testing.T) {
	task := standardTask()

	containerInstanceTags := standardContainerInstanceTags()
	taskTags := standardTaskTags()

	ecsInstanceTags := standardECSContainerInstanceTags()
	ecsTaskTags := standardECSTaskTags()

	path := v3BasePath + v3EndpointID + "/taskWithTags"

	happyECSClientExpectations := func(ecsClient *mock_ecs.MockECSClient) {
		gomock.InOrder(
			ecsClient.EXPECT().GetResourceTags(containerInstanceArn).Return(ecsInstanceTags, nil),
			ecsClient.EXPECT().GetResourceTags(taskARN).Return(ecsTaskTags, nil),
		)
	}
	happyStateExpectations := func(state *mock_dockerstate.MockTaskEngineState) {
		gomock.InOrder(
			state.EXPECT().TaskARNByV3EndpointID(v3EndpointID).Return(taskARN, true),
			state.EXPECT().TaskByArn(taskARN).Return(task, true),
			state.EXPECT().ContainerMapByArn(taskARN).Return(containerNameToDockerContainer, true),
			state.EXPECT().TaskByArn(taskARN).Return(task, true),
		)
	}

	t.Run("happy case", func(t *testing.T) {
		expectedTaskResponseWithTags := expectedTaskResponse()
		expectedTaskResponseWithTags.ContainerInstanceTags = containerInstanceTags
		expectedTaskResponseWithTags.TaskTags = taskTags
		testTMDSRequest(t, TMDSTestCase[v2.TaskResponse]{
			path:                     path,
			setStateExpectations:     happyStateExpectations,
			setECSClientExpectations: happyECSClientExpectations,
			expectedStatusCode:       http.StatusOK,
			expectedResponseBody:     expectedTaskResponseWithTags,
		})
	})
	t.Run("failed to get container instance tags", func(t *testing.T) {
		expectedTaskResponseWithTags := expectedTaskResponse()
		expectedTaskResponseWithTags.TaskTags = taskTags
		testTMDSRequest(t, TMDSTestCase[v2.TaskResponse]{
			path:                 path,
			setStateExpectations: happyStateExpectations,
			setECSClientExpectations: func(ecsClient *mock_ecs.MockECSClient) {
				gomock.InOrder(
					ecsClient.EXPECT().GetResourceTags(containerInstanceArn).Return(nil, errors.New("error")),
					ecsClient.EXPECT().GetResourceTags(taskARN).Return(ecsTaskTags, nil),
				)
			},
			expectedStatusCode:   http.StatusOK,
			expectedResponseBody: expectedTaskResponseWithTags,
		})
	})
	t.Run("failed to get task tags", func(t *testing.T) {
		expectedTaskResponseWithTags := expectedTaskResponse()
		expectedTaskResponseWithTags.ContainerInstanceTags = containerInstanceTags
		testTMDSRequest(t, TMDSTestCase[v2.TaskResponse]{
			path:                 path,
			setStateExpectations: happyStateExpectations,
			setECSClientExpectations: func(ecsClient *mock_ecs.MockECSClient) {
				gomock.InOrder(
					ecsClient.EXPECT().GetResourceTags(containerInstanceArn).Return(ecsInstanceTags, nil),
					ecsClient.EXPECT().GetResourceTags(taskARN).Return(nil, errors.New("error")),
				)
			},
			expectedStatusCode:   http.StatusOK,
			expectedResponseBody: expectedTaskResponseWithTags,
		})
	})
	t.Run("failed to get container instance and task tags", func(t *testing.T) {
		testTMDSRequest(t, TMDSTestCase[v2.TaskResponse]{
			path:                 path,
			setStateExpectations: happyStateExpectations,
			setECSClientExpectations: func(ecsClient *mock_ecs.MockECSClient) {
				gomock.InOrder(
					ecsClient.EXPECT().GetResourceTags(containerInstanceArn).Return(nil, errors.New("error")),
					ecsClient.EXPECT().GetResourceTags(taskARN).Return(nil, errors.New("error")),
				)
			},
			expectedStatusCode:   http.StatusOK,
			expectedResponseBody: expectedTaskResponse(),
		})
	})
	t.Run("taskARN not found for v3EndpointID", func(t *testing.T) {
		testTMDSRequest(t, TMDSTestCase[string]{
			path: path,
			setStateExpectations: func(state *mock_dockerstate.MockTaskEngineState) {
				gomock.InOrder(
					state.EXPECT().TaskARNByV3EndpointID(v3EndpointID).Return("", false),
				)
			},
			expectedStatusCode: http.StatusInternalServerError,
			expectedResponseBody: fmt.Sprintf(
				"V3 task metadata handler: unable to get task arn from request: unable to get task Arn from v3 endpoint ID: %s",
				v3EndpointID),
		})
	})
	t.Run("task not found by taskARN", func(t *testing.T) {
		testTMDSRequest(t, TMDSTestCase[string]{
			path: path,
			setStateExpectations: func(state *mock_dockerstate.MockTaskEngineState) {
				gomock.InOrder(
					state.EXPECT().TaskARNByV3EndpointID(v3EndpointID).Return(taskARN, true),
					state.EXPECT().TaskByArn(taskARN).Return(nil, false),
				)
			},
			expectedStatusCode: http.StatusInternalServerError,
			expectedResponseBody: fmt.Sprintf(
				"Unable to generate metadata for task: '%s'",
				taskARN),
		})
	})
	t.Run("containerMap not found for Arn", func(t *testing.T) {
		testTMDSRequest(t, TMDSTestCase[v2.TaskResponse]{
			path: path,
			setStateExpectations: func(state *mock_dockerstate.MockTaskEngineState) {
				gomock.InOrder(
					state.EXPECT().TaskARNByV3EndpointID(v3EndpointID).Return(taskARN, true),
					state.EXPECT().TaskByArn(taskARN).Return(task, true),
					state.EXPECT().ContainerMapByArn(taskARN).Return(nil, false),
					state.EXPECT().TaskByArn(taskARN).Return(task, true),
				)
			},
			expectedStatusCode:   http.StatusOK,
			expectedResponseBody: expectedTaskResponseNoContainers,
		})
	})
	t.Run("bridge mode container not found", func(t *testing.T) {
		testTMDSRequest(t, TMDSTestCase[string]{
			path:                     path,
			setECSClientExpectations: happyECSClientExpectations,
			setStateExpectations: func(state *mock_dockerstate.MockTaskEngineState) {
				gomock.InOrder(
					state.EXPECT().TaskARNByV3EndpointID(v3EndpointID).Return(taskARN, true),
					state.EXPECT().TaskByArn(taskARN).Return(bridgeTask, true),
					state.EXPECT().ContainerMapByArn(taskARN).Return(containerNameToBridgeContainer, true),
					state.EXPECT().TaskByArn(taskARN).Return(bridgeTask, true),
					state.EXPECT().ContainerByID(containerID).Return(nil, false),
				)
			},
			expectedStatusCode:   http.StatusInternalServerError,
			expectedResponseBody: fmt.Sprintf("Unable to find container '%s'", containerID),
		})
	})
	t.Run("bridge mode container no network settings", func(t *testing.T) {
		testTMDSRequest(t, TMDSTestCase[string]{
			path:                     path,
			setECSClientExpectations: happyECSClientExpectations,
			setStateExpectations: func(state *mock_dockerstate.MockTaskEngineState) {
				gomock.InOrder(
					state.EXPECT().TaskARNByV3EndpointID(v3EndpointID).Return(taskARN, true),
					state.EXPECT().TaskByArn(taskARN).Return(bridgeTask, true),
					state.EXPECT().ContainerMapByArn(taskARN).Return(containerNameToBridgeContainer, true),
					state.EXPECT().TaskByArn(taskARN).Return(bridgeTask, true),
					state.EXPECT().ContainerByID(containerID).Return(bridgeContainerNoNetwork, true),
				)
			},
			expectedStatusCode: http.StatusInternalServerError,
			expectedResponseBody: fmt.Sprintf(
				"Unable to generate network response for container '%s'", containerID),
		})
	})
}

func TestV4TaskMetadataWithTags(t *testing.T) {
	task := standardTask()

	containerInstanceTags := standardContainerInstanceTags()
	taskTags := standardTaskTags()

	ecsInstanceTags := standardECSContainerInstanceTags()
	ecsTaskTags := standardECSTaskTags()

	containerInstanceTagsError := v2.ErrorResponse{
		ErrorField:   "ContainerInstanceTags",
		ErrorMessage: "error",
		ResourceARN:  containerInstanceArn,
	}
	taskTagsError := v2.ErrorResponse{
		ErrorField:   "TaskTags",
		ErrorMessage: "error",
		ResourceARN:  taskARN,
	}

	happyECSClientExpectations := func(ecsClient *mock_ecs.MockECSClient) {
		gomock.InOrder(
			ecsClient.EXPECT().GetResourceTags(containerInstanceArn).Return(ecsInstanceTags, nil),
			ecsClient.EXPECT().GetResourceTags(taskARN).Return(ecsTaskTags, nil),
		)
	}
	happyStateExpectations := func(state *mock_dockerstate.MockTaskEngineState) {
		gomock.InOrder(
			state.EXPECT().TaskARNByV3EndpointID(v3EndpointID).Return(taskARN, true),
			state.EXPECT().TaskByArn(taskARN).Return(task, true).AnyTimes(),
			state.EXPECT().ContainerByID(containerID).Return(bridgeContainer, true).AnyTimes(),
			state.EXPECT().ContainerMapByArn(taskARN).Return(containerNameToDockerContainer, true),
			state.EXPECT().TaskByArn(taskARN).Return(task, true).AnyTimes(),
			state.EXPECT().ContainerByID(containerID).Return(bridgeContainer, true).AnyTimes(),
			state.EXPECT().PulledContainerMapByArn(taskARN).Return(nil, true),
		)
	}

	path := v4BasePath + v3EndpointID + "/taskWithTags"

	t.Run("happy case", func(t *testing.T) {
		expectedV4TaskResponseWithTags := expectedV4TaskResponse()
		expectedV4TaskResponseWithTags.ContainerInstanceTags = containerInstanceTags
		expectedV4TaskResponseWithTags.TaskTags = taskTags
		testTMDSRequest(t, TMDSTestCase[v4.TaskResponse]{
			path:                     path,
			setStateExpectations:     happyStateExpectations,
			setECSClientExpectations: happyECSClientExpectations,
			expectedStatusCode:       http.StatusOK,
			expectedResponseBody:     expectedV4TaskResponseWithTags,
		})
	})
	t.Run("failed to get container instance tags", func(t *testing.T) {
		expectedV4TaskResponseWithTags := expectedV4TaskResponse()
		expectedV4TaskResponseWithTags.TaskTags = taskTags
		expectedV4TaskResponseWithTags.Errors = []v2.ErrorResponse{containerInstanceTagsError}
		testTMDSRequest(t, TMDSTestCase[v4.TaskResponse]{
			path:                 path,
			setStateExpectations: happyStateExpectations,
			setECSClientExpectations: func(ecsClient *mock_ecs.MockECSClient) {
				gomock.InOrder(
					ecsClient.EXPECT().GetResourceTags(containerInstanceArn).Return(nil, errors.New("error")),
					ecsClient.EXPECT().GetResourceTags(taskARN).Return(ecsTaskTags, nil),
				)
			},
			expectedStatusCode:   http.StatusOK,
			expectedResponseBody: expectedV4TaskResponseWithTags,
		})
	})
	t.Run("failed to get task tags", func(t *testing.T) {
		expectedV4TaskResponseWithTags := expectedV4TaskResponse()
		expectedV4TaskResponseWithTags.ContainerInstanceTags = containerInstanceTags
		expectedV4TaskResponseWithTags.Errors = []v2.ErrorResponse{taskTagsError}
		testTMDSRequest(t, TMDSTestCase[v4.TaskResponse]{
			path:                 path,
			setStateExpectations: happyStateExpectations,
			setECSClientExpectations: func(ecsClient *mock_ecs.MockECSClient) {
				gomock.InOrder(
					ecsClient.EXPECT().GetResourceTags(containerInstanceArn).Return(ecsInstanceTags, nil),
					ecsClient.EXPECT().GetResourceTags(taskARN).Return(nil, errors.New("error")),
				)
			},
			expectedStatusCode:   http.StatusOK,
			expectedResponseBody: expectedV4TaskResponseWithTags,
		})
	})
	t.Run("failed to get container instance tags and task tags", func(t *testing.T) {
		expectedV4TaskResponseWithTags := expectedV4TaskResponse()
		expectedV4TaskResponseWithTags.Errors = []v2.ErrorResponse{
			containerInstanceTagsError, taskTagsError}
		testTMDSRequest(t, TMDSTestCase[v4.TaskResponse]{
			path:                 path,
			setStateExpectations: happyStateExpectations,
			setECSClientExpectations: func(ecsClient *mock_ecs.MockECSClient) {
				gomock.InOrder(
					ecsClient.EXPECT().GetResourceTags(containerInstanceArn).Return(nil, errors.New("error")),
					ecsClient.EXPECT().GetResourceTags(taskARN).Return(nil, errors.New("error")),
				)
			},
			expectedStatusCode:   http.StatusOK,
			expectedResponseBody: expectedV4TaskResponseWithTags,
		})
	})
	t.Run("taskARN not found for v3EndpointID", func(t *testing.T) {
		testTMDSRequest(t, TMDSTestCase[string]{
			path: path,
			setStateExpectations: func(state *mock_dockerstate.MockTaskEngineState) {
				gomock.InOrder(
					state.EXPECT().TaskARNByV3EndpointID(v3EndpointID).Return("", false),
				)
			},
			expectedStatusCode: http.StatusNotFound,
			expectedResponseBody: fmt.Sprintf(
				"V4 task metadata handler: unable to get task arn from request: unable to get task Arn from v3 endpoint ID: %s",
				v3EndpointID),
		})
	})
	t.Run("task not found for taskARN", func(t *testing.T) {
		testTMDSRequest(t, TMDSTestCase[string]{
			path: path,
			setStateExpectations: func(state *mock_dockerstate.MockTaskEngineState) {
				gomock.InOrder(
					state.EXPECT().TaskARNByV3EndpointID(v3EndpointID).Return(taskARN, true),
					state.EXPECT().TaskByArn(taskARN).Return(nil, false),
				)
			},
			expectedStatusCode:   http.StatusInternalServerError,
			expectedResponseBody: fmt.Sprintf("Unable to generate metadata for v4 task: '%s'", taskARN),
		})
	})
	t.Run("task not found on second lookup", func(t *testing.T) {
		testTMDSRequest(t, TMDSTestCase[string]{
			path: path,
			setStateExpectations: func(state *mock_dockerstate.MockTaskEngineState) {
				gomock.InOrder(
					state.EXPECT().TaskARNByV3EndpointID(v3EndpointID).Return(taskARN, true),
					state.EXPECT().TaskByArn(taskARN).Return(task, true),
					state.EXPECT().TaskByArn(taskARN).Return(nil, false),
				)
			},
			expectedStatusCode:   http.StatusInternalServerError,
			expectedResponseBody: fmt.Sprintf("Unable to generate metadata for v4 task: '%s'", taskARN),
		})
	})
	t.Run("containers not found for taskARN", func(t *testing.T) {
		testTMDSRequest(t, TMDSTestCase[v4.TaskResponse]{
			path: path,
			setStateExpectations: func(state *mock_dockerstate.MockTaskEngineState) {
				gomock.InOrder(
					state.EXPECT().TaskARNByV3EndpointID(v3EndpointID).Return(taskARN, true),
					state.EXPECT().TaskByArn(taskARN).Return(task, true).Times(2),
					state.EXPECT().ContainerMapByArn(taskARN).Return(nil, false),
					state.EXPECT().PulledContainerMapByArn(taskARN).Return(nil, true),
				)
			},
			expectedStatusCode:   http.StatusOK,
			expectedResponseBody: expectedV4TaskResponseNoContainers(),
		})
	})
	t.Run("task not found on third lookup", func(t *testing.T) {
		testTMDSRequest(t, TMDSTestCase[string]{
			path: path,
			setStateExpectations: func(state *mock_dockerstate.MockTaskEngineState) {
				gomock.InOrder(
					state.EXPECT().TaskARNByV3EndpointID(v3EndpointID).Return(taskARN, true),
					state.EXPECT().TaskByArn(taskARN).Return(task, true).Times(2),
					state.EXPECT().ContainerMapByArn(taskARN).Return(containerNameToDockerContainer, true),
					state.EXPECT().TaskByArn(taskARN).Return(nil, false),
				)
			},
			setECSClientExpectations: happyECSClientExpectations,
			expectedStatusCode:       http.StatusInternalServerError,
			expectedResponseBody: fmt.Sprintf(
				"Unable to generate metadata for v4 task: '%s'", taskARN),
		})
	})
	t.Run("bridge mode container not found", func(t *testing.T) {
		expectedV4TaskResponseWithTags := expectedV4BridgeTaskResponseNoNetwork()
		expectedV4TaskResponseWithTags.ContainerInstanceTags = containerInstanceTags
		expectedV4TaskResponseWithTags.TaskTags = taskTags
		testTMDSRequest(t, TMDSTestCase[v4.TaskResponse]{
			path: path,
			setStateExpectations: func(state *mock_dockerstate.MockTaskEngineState) {
				gomock.InOrder(
					state.EXPECT().TaskARNByV3EndpointID(v3EndpointID).Return(taskARN, true),
					state.EXPECT().TaskByArn(taskARN).Return(bridgeTask, true).Times(2),
					state.EXPECT().ContainerMapByArn(taskARN).Return(containerNameToBridgeContainer, true),
					state.EXPECT().ContainerByID(containerID).Return(nil, false).AnyTimes(),
					state.EXPECT().PulledContainerMapByArn(taskARN).Return(nil, true),
					state.EXPECT().ContainerByID(containerID).Return(nil, false).AnyTimes(),
				)
			},
			setECSClientExpectations: happyECSClientExpectations,
			expectedStatusCode:       http.StatusOK,
			expectedResponseBody:     expectedV4TaskResponseWithTags,
		})
	})
	t.Run("bridge mode container no network settings", func(t *testing.T) {
		expectedV4TaskResponseWithTags := expectedV4BridgeTaskResponseNoNetwork()
		expectedV4TaskResponseWithTags.ContainerInstanceTags = containerInstanceTags
		expectedV4TaskResponseWithTags.TaskTags = taskTags
		testTMDSRequest(t, TMDSTestCase[v4.TaskResponse]{
			path: path,
			setStateExpectations: func(state *mock_dockerstate.MockTaskEngineState) {
				gomock.InOrder(
					state.EXPECT().TaskARNByV3EndpointID(v3EndpointID).Return(taskARN, true),
					state.EXPECT().TaskByArn(taskARN).Return(bridgeTask, true).Times(2),
					state.EXPECT().ContainerMapByArn(taskARN).Return(containerNameToBridgeContainer, true),
					state.EXPECT().ContainerByID(containerID).Return(bridgeContainerNoNetwork, true).AnyTimes(),
					state.EXPECT().PulledContainerMapByArn(taskARN).Return(nil, true),
					state.EXPECT().ContainerByID(containerID).Return(bridgeContainerNoNetwork, true).AnyTimes(),
				)
			},
			setECSClientExpectations: happyECSClientExpectations,
			expectedStatusCode:       http.StatusOK,
			expectedResponseBody:     expectedV4TaskResponseWithTags,
		})
	})
}

func TestV2ContainerStats(t *testing.T) {
	path := v2BaseStatsPath + "/" + containerID
	t.Run("task not found", func(t *testing.T) {
		testTMDSRequest(t, TMDSTestCase[string]{
			path: path,
			setStateExpectations: func(state *mock_dockerstate.MockTaskEngineState) {
				state.EXPECT().GetTaskByIPAddress(remoteIP).Return("", false)
			},
			expectedStatusCode: http.StatusBadRequest,
			expectedResponseBody: fmt.Sprintf(
				"Unable to get task arn from request: unable to associate '%s' with task", remoteIP),
		})
	})
	t.Run("stats not found", func(t *testing.T) {
		testTMDSRequest(t, TMDSTestCase[string]{
			path: path,
			setStateExpectations: func(state *mock_dockerstate.MockTaskEngineState) {
				state.EXPECT().GetTaskByIPAddress(remoteIP).Return(taskARN, true)
			},
			setStatsEngineExpectations: func(engine *mock_stats.MockEngine) {
				engine.EXPECT().ContainerDockerStats(taskARN, containerID).
					Return(nil, nil, errors.New("some error"))
			},
			expectedStatusCode:   http.StatusBadRequest,
			expectedResponseBody: fmt.Sprintf("Unable to get container stats for: %s", containerID),
		})
	})
	t.Run("happy case", func(t *testing.T) {
		dockerStats := types.StatsJSON{Stats: types.Stats{NumProcs: 2}}
		testTMDSRequest(t, TMDSTestCase[types.StatsJSON]{
			path: path,
			setStateExpectations: func(state *mock_dockerstate.MockTaskEngineState) {
				state.EXPECT().GetTaskByIPAddress(remoteIP).Return(taskARN, true)
			},
			setStatsEngineExpectations: func(engine *mock_stats.MockEngine) {
				engine.EXPECT().ContainerDockerStats(taskARN, containerID).
					Return(&dockerStats, &stats.NetworkStatsPerSec{}, nil)
			},
			expectedStatusCode:   http.StatusOK,
			expectedResponseBody: dockerStats,
		})
	})
}

func TestV2TaskStats(t *testing.T) {
	t.Run("task not found", func(t *testing.T) {
		testTMDSRequest(t, TMDSTestCase[string]{
			path: v2BaseStatsPath,
			setStateExpectations: func(state *mock_dockerstate.MockTaskEngineState) {
				state.EXPECT().GetTaskByIPAddress(remoteIP).Return("", false)
			},
			expectedStatusCode: http.StatusBadRequest,
			expectedResponseBody: fmt.Sprintf(
				"Unable to get task arn from request: unable to associate '%s' with task", remoteIP),
		})
	})
	t.Run("container map not found", func(t *testing.T) {
		testTMDSRequest(t, TMDSTestCase[string]{
			path: v2BaseStatsPath,
			setStateExpectations: func(state *mock_dockerstate.MockTaskEngineState) {
				gomock.InOrder(
					state.EXPECT().GetTaskByIPAddress(remoteIP).Return(taskARN, true),
					state.EXPECT().ContainerMapByArn(taskARN).Return(nil, false),
				)
			},
			expectedStatusCode:   http.StatusBadRequest,
			expectedResponseBody: "Unable to get task stats for: " + taskARN,
		})
	})
	t.Run("container map empty", func(t *testing.T) {
		testTMDSRequest(t, TMDSTestCase[map[string]*types.StatsJSON]{
			path: v2BaseStatsPath,
			setStateExpectations: func(state *mock_dockerstate.MockTaskEngineState) {
				gomock.InOrder(
					state.EXPECT().GetTaskByIPAddress(remoteIP).Return(taskARN, true),
					state.EXPECT().ContainerMapByArn(taskARN).
						Return(map[string]*apicontainer.DockerContainer{}, true),
				)
			},
			expectedStatusCode:   http.StatusOK,
			expectedResponseBody: map[string]*types.StatsJSON{},
		})
	})
	t.Run("stats not found for a container", func(t *testing.T) {
		containerMap := map[string]*apicontainer.DockerContainer{
			containerName: {DockerID: containerID},
		}
		testTMDSRequest(t, TMDSTestCase[map[string]*types.StatsJSON]{
			path: v2BaseStatsPath,
			setStateExpectations: func(state *mock_dockerstate.MockTaskEngineState) {
				gomock.InOrder(
					state.EXPECT().GetTaskByIPAddress(remoteIP).Return(taskARN, true),
					state.EXPECT().ContainerMapByArn(taskARN).Return(containerMap, true),
				)
			},
			setStatsEngineExpectations: func(engine *mock_stats.MockEngine) {
				engine.EXPECT().ContainerDockerStats(taskARN, containerID).
					Return(nil, nil, errors.New("some error"))
			},
			expectedStatusCode:   http.StatusOK,
			expectedResponseBody: map[string]*types.StatsJSON{containerID: nil},
		})
	})

	happyCasePaths := []string{v2BaseStatsPath, v2BaseStatsPath + "/"}
	for _, path := range happyCasePaths {
		t.Run("happy case", func(t *testing.T) {
			dockerStats := types.StatsJSON{Stats: types.Stats{NumProcs: 2}}
			containerMap := map[string]*apicontainer.DockerContainer{
				containerName: {DockerID: containerID},
			}
			taskStats := map[string]*types.StatsJSON{containerID: &dockerStats}
			testTMDSRequest(t, TMDSTestCase[map[string]*types.StatsJSON]{
				path: path,
				setStateExpectations: func(state *mock_dockerstate.MockTaskEngineState) {
					gomock.InOrder(
						state.EXPECT().GetTaskByIPAddress(remoteIP).Return(taskARN, true),
						state.EXPECT().ContainerMapByArn(taskARN).Return(containerMap, true),
					)
				},
				setStatsEngineExpectations: func(engine *mock_stats.MockEngine) {
					engine.EXPECT().ContainerDockerStats(taskARN, containerID).
						Return(&dockerStats, &stats.NetworkStatsPerSec{}, nil)
				},
				expectedStatusCode:   http.StatusOK,
				expectedResponseBody: taskStats,
			})
		})
	}
}

func TestV3ContainerStats(t *testing.T) {
	path := v3BasePath + v3EndpointID + "/stats"
	t.Run("task not found", func(t *testing.T) {
		testTMDSRequest(t, TMDSTestCase[string]{
			path: path,
			setStateExpectations: func(state *mock_dockerstate.MockTaskEngineState) {
				state.EXPECT().TaskARNByV3EndpointID(v3EndpointID).Return("", false)
			},
			expectedStatusCode: http.StatusBadRequest,
			expectedResponseBody: fmt.Sprintf(
				"V3 container stats handler: unable to get task arn from request: unable to get task Arn from v3 endpoint ID: %s",
				v3EndpointID),
		})
	})
	t.Run("Docker ID not found", func(t *testing.T) {
		testTMDSRequest(t, TMDSTestCase[string]{
			path: path,
			setStateExpectations: func(state *mock_dockerstate.MockTaskEngineState) {
				gomock.InOrder(
					state.EXPECT().TaskARNByV3EndpointID(v3EndpointID).Return(taskARN, true),
					state.EXPECT().DockerIDByV3EndpointID(v3EndpointID).Return("", false),
				)
			},
			expectedStatusCode: http.StatusBadRequest,
			expectedResponseBody: fmt.Sprintf(
				"V3 container stats handler: unable to get container ID from request: unable to get docker ID from v3 endpoint ID: %s",
				v3EndpointID),
		})
	})
	t.Run("stats not found", func(t *testing.T) {
		testTMDSRequest(t, TMDSTestCase[string]{
			path: path,
			setStateExpectations: func(state *mock_dockerstate.MockTaskEngineState) {
				gomock.InOrder(
					state.EXPECT().TaskARNByV3EndpointID(v3EndpointID).Return(taskARN, true),
					state.EXPECT().DockerIDByV3EndpointID(v3EndpointID).Return(containerID, true),
				)
			},
			setStatsEngineExpectations: func(engine *mock_stats.MockEngine) {
				engine.EXPECT().ContainerDockerStats(taskARN, containerID).
					Return(nil, nil, errors.New("some error"))
			},
			expectedStatusCode:   http.StatusBadRequest,
			expectedResponseBody: fmt.Sprintf("Unable to get container stats for: %s", containerID),
		})
	})
	t.Run("happy case", func(t *testing.T) {
		dockerStats := types.StatsJSON{Stats: types.Stats{NumProcs: 2}}
		testTMDSRequest(t, TMDSTestCase[types.StatsJSON]{
			path: path,
			setStateExpectations: func(state *mock_dockerstate.MockTaskEngineState) {
				gomock.InOrder(
					state.EXPECT().TaskARNByV3EndpointID(v3EndpointID).Return(taskARN, true),
					state.EXPECT().DockerIDByV3EndpointID(v3EndpointID).Return(containerID, true),
				)
			},
			setStatsEngineExpectations: func(engine *mock_stats.MockEngine) {
				engine.EXPECT().ContainerDockerStats(taskARN, containerID).
					Return(&dockerStats, &stats.NetworkStatsPerSec{}, nil)
			},
			expectedStatusCode:   http.StatusOK,
			expectedResponseBody: dockerStats,
		})
	})
}

func TestV3TaskStats(t *testing.T) {
	path := v3BasePath + v3EndpointID + "/task/stats"
	t.Run("task not found", func(t *testing.T) {
		testTMDSRequest(t, TMDSTestCase[string]{
			path: path,
			setStateExpectations: func(state *mock_dockerstate.MockTaskEngineState) {
				state.EXPECT().TaskARNByV3EndpointID(v3EndpointID).Return("", false)
			},
			expectedStatusCode: http.StatusBadRequest,
			expectedResponseBody: fmt.Sprintf(
				"V3 task stats handler: unable to get task arn from request: unable to get task Arn from v3 endpoint ID: %s",
				v3EndpointID),
		})
	})
	t.Run("container map not found", func(t *testing.T) {
		testTMDSRequest(t, TMDSTestCase[string]{
			path: path,
			setStateExpectations: func(state *mock_dockerstate.MockTaskEngineState) {
				gomock.InOrder(
					state.EXPECT().TaskARNByV3EndpointID(v3EndpointID).Return(taskARN, true),
					state.EXPECT().ContainerMapByArn(taskARN).Return(nil, false),
				)
			},
			expectedStatusCode:   http.StatusBadRequest,
			expectedResponseBody: "Unable to get task stats for: " + taskARN,
		})
	})
	t.Run("container map empty", func(t *testing.T) {
		testTMDSRequest(t, TMDSTestCase[map[string]*types.StatsJSON]{
			path: path,
			setStateExpectations: func(state *mock_dockerstate.MockTaskEngineState) {
				gomock.InOrder(
					state.EXPECT().TaskARNByV3EndpointID(v3EndpointID).Return(taskARN, true),
					state.EXPECT().ContainerMapByArn(taskARN).
						Return(map[string]*apicontainer.DockerContainer{}, true),
				)
			},
			expectedStatusCode:   http.StatusOK,
			expectedResponseBody: map[string]*types.StatsJSON{},
		})
	})
	t.Run("stats not found for a container", func(t *testing.T) {
		containerMap := map[string]*apicontainer.DockerContainer{
			containerName: {DockerID: containerID},
		}
		testTMDSRequest(t, TMDSTestCase[map[string]*types.StatsJSON]{
			path: path,
			setStateExpectations: func(state *mock_dockerstate.MockTaskEngineState) {
				gomock.InOrder(
					state.EXPECT().TaskARNByV3EndpointID(v3EndpointID).Return(taskARN, true),
					state.EXPECT().ContainerMapByArn(taskARN).Return(containerMap, true),
				)
			},
			setStatsEngineExpectations: func(engine *mock_stats.MockEngine) {
				engine.EXPECT().ContainerDockerStats(taskARN, containerID).
					Return(nil, nil, errors.New("some error"))
			},
			expectedStatusCode:   http.StatusOK,
			expectedResponseBody: map[string]*types.StatsJSON{containerID: nil},
		})
	})
	t.Run("happy case", func(t *testing.T) {
		dockerStats := types.StatsJSON{Stats: types.Stats{NumProcs: 2}}
		containerMap := map[string]*apicontainer.DockerContainer{
			containerName: {DockerID: containerID},
		}
		testTMDSRequest(t, TMDSTestCase[map[string]*types.StatsJSON]{
			path: path,
			setStateExpectations: func(state *mock_dockerstate.MockTaskEngineState) {
				gomock.InOrder(
					state.EXPECT().TaskARNByV3EndpointID(v3EndpointID).Return(taskARN, true),
					state.EXPECT().ContainerMapByArn(taskARN).Return(containerMap, true),
				)
			},
			setStatsEngineExpectations: func(engine *mock_stats.MockEngine) {
				engine.EXPECT().ContainerDockerStats(taskARN, containerID).
					Return(&dockerStats, &stats.NetworkStatsPerSec{}, nil)
			},
			expectedStatusCode:   http.StatusOK,
			expectedResponseBody: map[string]*types.StatsJSON{containerID: &dockerStats},
		})
	})
}

func TestV4ContainerStats(t *testing.T) {
	path := v4BasePath + v3EndpointID + "/stats"
	t.Run("task not found", func(t *testing.T) {
		testTMDSRequest(t, TMDSTestCase[string]{
			path: path,
			setStateExpectations: func(state *mock_dockerstate.MockTaskEngineState) {
				state.EXPECT().TaskARNByV3EndpointID(v3EndpointID).Return("", false)
			},
			expectedStatusCode: http.StatusNotFound,
			expectedResponseBody: fmt.Sprintf(
				"V4 container stats handler: unable to get task arn from request: unable to get task Arn from v3 endpoint ID: %s",
				v3EndpointID),
		})
	})
	t.Run("Docker ID not found", func(t *testing.T) {
		testTMDSRequest(t, TMDSTestCase[string]{
			path: path,
			setStateExpectations: func(state *mock_dockerstate.MockTaskEngineState) {
				gomock.InOrder(
					state.EXPECT().TaskARNByV3EndpointID(v3EndpointID).Return(taskARN, true),
					state.EXPECT().DockerIDByV3EndpointID(v3EndpointID).Return("", false),
				)
			},
			expectedStatusCode: http.StatusNotFound,
			expectedResponseBody: fmt.Sprintf(
				"V4 container stats handler: unable to get container ID from request: unable to get docker ID from v3 endpoint ID: %s",
				v3EndpointID),
		})
	})
	t.Run("stats not found", func(t *testing.T) {
		testTMDSRequest(t, TMDSTestCase[string]{
			path: path,
			setStateExpectations: func(state *mock_dockerstate.MockTaskEngineState) {
				gomock.InOrder(
					state.EXPECT().TaskARNByV3EndpointID(v3EndpointID).Return(taskARN, true),
					state.EXPECT().DockerIDByV3EndpointID(v3EndpointID).Return(containerID, true),
				)
			},
			setStatsEngineExpectations: func(engine *mock_stats.MockEngine) {
				engine.EXPECT().ContainerDockerStats(taskARN, containerID).
					Return(nil, nil, errors.New("some error"))
			},
			expectedStatusCode:   http.StatusInternalServerError,
			expectedResponseBody: "Unable to get container stats for: " + containerID,
		})
	})
	t.Run("happy case", func(t *testing.T) {
		dockerStats := types.StatsJSON{Stats: types.Stats{NumProcs: 2}}
		networkStats := stats.NetworkStatsPerSec{
			RxBytesPerSecond: 52,
			TxBytesPerSecond: 84,
		}
		testTMDSRequest(t, TMDSTestCase[v4.StatsResponse]{
			path: path,
			setStateExpectations: func(state *mock_dockerstate.MockTaskEngineState) {
				gomock.InOrder(
					state.EXPECT().TaskARNByV3EndpointID(v3EndpointID).Return(taskARN, true),
					state.EXPECT().DockerIDByV3EndpointID(v3EndpointID).Return(containerID, true),
				)
			},
			setStatsEngineExpectations: func(engine *mock_stats.MockEngine) {
				engine.EXPECT().ContainerDockerStats(taskARN, containerID).
					Return(&dockerStats, &networkStats, nil)
			},
			expectedStatusCode: http.StatusOK,
			expectedResponseBody: v4.StatsResponse{
				StatsJSON:          &dockerStats,
				Network_rate_stats: &networkStats,
			},
		})
	})
}

func TestV4TaskStats(t *testing.T) {
	path := v4BasePath + v3EndpointID + "/task/stats"
	t.Run("task not found", func(t *testing.T) {
		testTMDSRequest(t, TMDSTestCase[string]{
			path: path,
			setStateExpectations: func(state *mock_dockerstate.MockTaskEngineState) {
				gomock.InOrder(
					state.EXPECT().TaskARNByV3EndpointID(v3EndpointID).Return("", false),
				)
			},
			expectedStatusCode: http.StatusNotFound,
			expectedResponseBody: fmt.Sprintf(
				"V4 task stats handler: unable to get task arn from request: unable to get task Arn from v3 endpoint ID: %s",
				v3EndpointID),
		})
	})
	t.Run("containerMap not found", func(t *testing.T) {
		testTMDSRequest(t, TMDSTestCase[string]{
			path: path,
			setStateExpectations: func(state *mock_dockerstate.MockTaskEngineState) {
				gomock.InOrder(
					state.EXPECT().TaskARNByV3EndpointID(v3EndpointID).Return(taskARN, true),
					state.EXPECT().ContainerMapByArn(taskARN).Return(nil, false),
				)
			},
			expectedStatusCode:   http.StatusInternalServerError,
			expectedResponseBody: "Unable to get task stats for: " + taskARN,
		})
	})
	t.Run("containerMap empty", func(t *testing.T) {
		containerMap := map[string]*apicontainer.DockerContainer{}
		testTMDSRequest(t, TMDSTestCase[map[string]*v4.StatsResponse]{
			path: path,
			setStateExpectations: func(state *mock_dockerstate.MockTaskEngineState) {
				gomock.InOrder(
					state.EXPECT().TaskARNByV3EndpointID(v3EndpointID).Return(taskARN, true),
					state.EXPECT().ContainerMapByArn(taskARN).Return(containerMap, true),
				)
			},
			expectedStatusCode:   http.StatusOK,
			expectedResponseBody: map[string]*v4.StatsResponse{},
		})
	})
	t.Run("stats not found for a container", func(t *testing.T) {
		containerMap := map[string]*apicontainer.DockerContainer{
			containerName: {DockerID: containerID},
		}
		testTMDSRequest(t, TMDSTestCase[map[string]*v4.StatsResponse]{
			path: path,
			setStateExpectations: func(state *mock_dockerstate.MockTaskEngineState) {
				gomock.InOrder(
					state.EXPECT().TaskARNByV3EndpointID(v3EndpointID).Return(taskARN, true),
					state.EXPECT().ContainerMapByArn(taskARN).Return(containerMap, true),
				)
			},
			setStatsEngineExpectations: func(engine *mock_stats.MockEngine) {
				engine.EXPECT().ContainerDockerStats(taskARN, containerID).
					Return(nil, nil, errors.New("some error"))
			},
			expectedStatusCode: http.StatusOK,
			expectedResponseBody: map[string]*v4.StatsResponse{
				containerID: {
					StatsJSON: nil, Network_rate_stats: nil,
				}},
		})
	})
	t.Run("happy case", func(t *testing.T) {
		containerMap := map[string]*apicontainer.DockerContainer{
			containerName: {DockerID: containerID},
		}
		networkStats := stats.NetworkStatsPerSec{
			RxBytesPerSecond: 52,
			TxBytesPerSecond: 84,
		}
		dockerStats := types.StatsJSON{Stats: types.Stats{NumProcs: 2}}
		testTMDSRequest(t, TMDSTestCase[map[string]*v4.StatsResponse]{
			path: path,
			setStateExpectations: func(state *mock_dockerstate.MockTaskEngineState) {
				gomock.InOrder(
					state.EXPECT().TaskARNByV3EndpointID(v3EndpointID).Return(taskARN, true),
					state.EXPECT().ContainerMapByArn(taskARN).Return(containerMap, true),
				)
			},
			setStatsEngineExpectations: func(engine *mock_stats.MockEngine) {
				engine.EXPECT().ContainerDockerStats(taskARN, containerID).
					Return(&dockerStats, &networkStats, nil)
			},
			expectedStatusCode: http.StatusOK,
			expectedResponseBody: map[string]*v4.StatsResponse{containerID: {
				StatsJSON:          &dockerStats,
				Network_rate_stats: &networkStats,
			}},
		})
	})
}

func TestGetTaskProtection(t *testing.T) {
	path := fmt.Sprintf("/api/%s/task-protection/v1/state", v3EndpointID)

	// Set up some fake data
	task := standardTask()
	ecsInput := ecs.GetTaskProtectionInput{
		Cluster: aws.String(clusterName),
		Tasks:   []string{taskARN},
	}
	protectedTask := ecstypes.ProtectedTask{
		ProtectionEnabled: true,
		TaskArn:           aws.String(taskARN),
	}
	ecsOutput := ecs.GetTaskProtectionOutput{
		ProtectedTasks: []ecstypes.ProtectedTask{protectedTask},
	}
	ecsRequestID := "reqid"
	ecsErrMessage := "ecs error message"

	// Helper functions to set expectation on mocks
	happyStateExpectations := func(state *mock_dockerstate.MockTaskEngineState) {
		gomock.InOrder(
			state.EXPECT().TaskARNByV3EndpointID(v3EndpointID).Return(taskARN, true),
			state.EXPECT().TaskByArn(taskARN).Return(task, true).Times(2),
			state.EXPECT().ContainerMapByArn(taskARN).Return(containerNameToDockerContainer, true),
			state.EXPECT().TaskByArn(taskARN).Return(task, true),
			state.EXPECT().ContainerByID(containerID).Return(dockerContainer, true).AnyTimes(),
			state.EXPECT().PulledContainerMapByArn(taskARN).Return(nil, true),
		)
	}
	happyCredentialsManagerExpectations := func(credsManager *mock_credentials.MockManager) {
		credsManager.EXPECT().
			GetTaskCredentials(task.GetCredentialsID()).
			Return(taskRoleCredentials(), true)
	}
	taskProtectionClientFactoryExpectations := func(output *ecs.GetTaskProtectionOutput, err error) func(
		*gomock.Controller, *tp.MockTaskProtectionClientFactoryInterface,
	) {
		return func(
			ctrl *gomock.Controller,
			factory *tp.MockTaskProtectionClientFactoryInterface,
		) {
			client := mock_ecs.NewMockECSTaskProtectionSDK(ctrl)
			client.EXPECT().GetTaskProtection(gomock.Any(), &ecsInput).Return(output, err)
			factory.EXPECT().NewTaskProtectionClient(taskRoleCredentials()).Return(client, nil)
		}
	}

	// Test cases start here
	t.Run("task ARN not found", func(t *testing.T) {
		testTMDSRequest(t, TMDSTestCase[tptypes.TaskProtectionResponse]{
			path: path,
			setStateExpectations: func(state *mock_dockerstate.MockTaskEngineState) {
				gomock.InOrder(
					state.EXPECT().TaskARNByV3EndpointID(v3EndpointID).Return("", false),
				)
			},
			expectedStatusCode: http.StatusNotFound,
			expectedResponseBody: tptypes.TaskProtectionResponse{
				Error: &tptypes.ErrorResponse{
					Code:    apierrors.ErrCodeResourceNotFoundException,
					Message: "Failed to find a task for the request",
				},
			},
		})
	})
	t.Run("task not found", func(t *testing.T) {
		testTMDSRequest(t, TMDSTestCase[tptypes.TaskProtectionResponse]{
			path: path,
			setStateExpectations: func(state *mock_dockerstate.MockTaskEngineState) {
				gomock.InOrder(
					state.EXPECT().TaskARNByV3EndpointID(v3EndpointID).Return(taskARN, true),
					state.EXPECT().TaskByArn(taskARN).Return(nil, false),
				)
			},
			expectedStatusCode: http.StatusInternalServerError,
			expectedResponseBody: tptypes.TaskProtectionResponse{
				Error: &tptypes.ErrorResponse{
					Code:    apierrors.ErrCodeServerException,
					Message: "Failed to find a task for the request",
				},
			},
		})
	})
	t.Run("task credentials not found", func(t *testing.T) {
		testTMDSRequest(t, TMDSTestCase[tptypes.TaskProtectionResponse]{
			path:                 path,
			setStateExpectations: happyStateExpectations,
			setCredentialsManagerExpectations: func(credsManager *mock_credentials.MockManager) {
				credsManager.
					EXPECT().GetTaskCredentials(taskCredentialsID).
					Return(credentials.TaskIAMRoleCredentials{}, false)
			},
			expectedStatusCode: http.StatusForbidden,
			expectedResponseBody: tptypes.TaskProtectionResponse{
				Error: &tptypes.ErrorResponse{
					Arn:     taskARN,
					Code:    apierrors.ErrCodeAccessDeniedException,
					Message: "Invalid Request: no task IAM role credentials available for task",
				},
			},
		})
	})
	t.Run("ecs call server exception", func(t *testing.T) {
		testTMDSRequest(t, TMDSTestCase[tptypes.TaskProtectionResponse]{
			path:                              path,
			setStateExpectations:              happyStateExpectations,
			setCredentialsManagerExpectations: happyCredentialsManagerExpectations,
			setTaskProtectionClientFactoryExpectations: taskProtectionClientFactoryExpectations(
				nil,
				&awshttp.ResponseError{
					ResponseError: &smithyhttp.ResponseError{
						Response: &smithyhttp.Response{
							Response: &http.Response{
								StatusCode: http.StatusInternalServerError,
							},
						},
						Err: &ecstypes.ServerException{Message: &ecsErrMessage},
					},
					RequestID: ecsRequestID,
				},
			),
			expectedStatusCode: http.StatusInternalServerError,
			expectedResponseBody: tptypes.TaskProtectionResponse{
				RequestID: &ecsRequestID,
				Error: &tptypes.ErrorResponse{
					Arn:     taskARN,
					Code:    apierrors.ErrCodeServerException,
					Message: ecsErrMessage,
				},
			},
		})
	})
	t.Run("ecs call access denied exception", func(t *testing.T) {
		testTMDSRequest(t, TMDSTestCase[tptypes.TaskProtectionResponse]{
			path:                              path,
			setStateExpectations:              happyStateExpectations,
			setCredentialsManagerExpectations: happyCredentialsManagerExpectations,
			setTaskProtectionClientFactoryExpectations: taskProtectionClientFactoryExpectations(
				nil,
				&awshttp.ResponseError{
					ResponseError: &smithyhttp.ResponseError{
						Response: &smithyhttp.Response{
							Response: &http.Response{
								StatusCode: http.StatusBadRequest,
							},
						},
						Err: &ecstypes.AccessDeniedException{Message: &ecsErrMessage},
					},
					RequestID: ecsRequestID,
				},
			),
			expectedStatusCode: http.StatusBadRequest,
			expectedResponseBody: tptypes.TaskProtectionResponse{
				RequestID: &ecsRequestID,
				Error: &tptypes.ErrorResponse{
					Arn:     taskARN,
					Code:    apierrors.ErrCodeAccessDeniedException,
					Message: ecsErrMessage,
				},
			},
		})
	})
	t.Run("ecs call non-request-failure aws error", func(t *testing.T) {
		testTMDSRequest(t, TMDSTestCase[tptypes.TaskProtectionResponse]{
			path:                              path,
			setStateExpectations:              happyStateExpectations,
			setCredentialsManagerExpectations: happyCredentialsManagerExpectations,
			setTaskProtectionClientFactoryExpectations: taskProtectionClientFactoryExpectations(
				nil,
				&ecstypes.InvalidParameterException{Message: &ecsErrMessage}),
			expectedStatusCode: http.StatusInternalServerError,
			expectedResponseBody: tptypes.TaskProtectionResponse{
				Error: &tptypes.ErrorResponse{
					Arn:     taskARN,
					Code:    apierrors.ErrCodeInvalidParameterException,
					Message: ecsErrMessage,
				},
			},
		})
	})
	t.Run("agent timeout", func(t *testing.T) {
		testTMDSRequest(t, TMDSTestCase[tptypes.TaskProtectionResponse]{
			path:                              path,
			setStateExpectations:              happyStateExpectations,
			setCredentialsManagerExpectations: happyCredentialsManagerExpectations,
			setTaskProtectionClientFactoryExpectations: taskProtectionClientFactoryExpectations(
				nil, &smithy.CanceledError{}),
			expectedStatusCode: http.StatusGatewayTimeout,
			expectedResponseBody: tptypes.TaskProtectionResponse{
				Error: &tptypes.ErrorResponse{
					Arn:     taskARN,
					Code:    apierrors.ErrCodeRequestCanceled,
					Message: "Timed out calling ECS Task Protection API",
				},
			},
		})
	})
	t.Run("non-aws error", func(t *testing.T) {
		testTMDSRequest(t, TMDSTestCase[tptypes.TaskProtectionResponse]{
			path:                              path,
			setStateExpectations:              happyStateExpectations,
			setCredentialsManagerExpectations: happyCredentialsManagerExpectations,
			setTaskProtectionClientFactoryExpectations: taskProtectionClientFactoryExpectations(
				nil, errors.New("some error")),
			expectedStatusCode: http.StatusInternalServerError,
			expectedResponseBody: tptypes.TaskProtectionResponse{
				Error: &tptypes.ErrorResponse{
					Arn:     taskARN,
					Code:    apierrors.ErrCodeServerException,
					Message: "some error",
				},
			},
		})
	})
	t.Run("ecs failure", func(t *testing.T) {
		testTMDSRequest(t, TMDSTestCase[tptypes.TaskProtectionResponse]{
			path:                              path,
			setStateExpectations:              happyStateExpectations,
			setCredentialsManagerExpectations: happyCredentialsManagerExpectations,
			setTaskProtectionClientFactoryExpectations: taskProtectionClientFactoryExpectations(
				&ecs.GetTaskProtectionOutput{
					Failures: []ecstypes.Failure{{
						Arn:    aws.String(taskARN),
						Reason: aws.String("ecs failure"),
					}},
				}, nil),
			expectedStatusCode: http.StatusOK,
			expectedResponseBody: tptypes.TaskProtectionResponse{
				Failure: &ecstypes.Failure{
					Arn:    aws.String(taskARN),
					Reason: aws.String("ecs failure"),
				},
			},
		})
	})
	t.Run("more than one ecs failure", func(t *testing.T) {
		testTMDSRequest(t, TMDSTestCase[tptypes.TaskProtectionResponse]{
			path:                              path,
			setStateExpectations:              happyStateExpectations,
			setCredentialsManagerExpectations: happyCredentialsManagerExpectations,
			setTaskProtectionClientFactoryExpectations: taskProtectionClientFactoryExpectations(
				&ecs.GetTaskProtectionOutput{
					Failures: []ecstypes.Failure{
						{
							Arn:    aws.String(taskARN),
							Reason: aws.String("ecs failure 1"),
						},
						{
							Arn:    aws.String(taskARN),
							Reason: aws.String("ecs failure 2"),
						},
					},
				}, nil),
			expectedStatusCode: http.StatusInternalServerError,
			expectedResponseBody: tptypes.TaskProtectionResponse{
				Error: &tptypes.ErrorResponse{
					Arn:     taskARN,
					Code:    apierrors.ErrCodeServerException,
					Message: "Unexpected error occurred",
				},
			},
		})
	})
	t.Run("happy case", func(t *testing.T) {
		testTMDSRequest(t, TMDSTestCase[tptypes.TaskProtectionResponse]{
			path:                              path,
			setStateExpectations:              happyStateExpectations,
			setCredentialsManagerExpectations: happyCredentialsManagerExpectations,
			setTaskProtectionClientFactoryExpectations: taskProtectionClientFactoryExpectations(&ecsOutput, nil),
			expectedStatusCode:                         http.StatusOK,
			expectedResponseBody: tptypes.TaskProtectionResponse{
				Protection: &protectedTask,
			},
		})
	})
}

func TestUpdateTaskProtection(t *testing.T) {
	// Set up some fake data
	task := standardTask()
	protectionEnabled := true
	expirationMinutes := int32(5)
	ecsInput := ecs.UpdateTaskProtectionInput{
		Cluster:           aws.String(clusterName),
		ProtectionEnabled: protectionEnabled,
		ExpiresInMinutes:  aws.Int32(expirationMinutes),
		Tasks:             []string{taskARN},
	}
	protectedTask := ecstypes.ProtectedTask{
		ProtectionEnabled: protectionEnabled,
		TaskArn:           aws.String(taskARN),
	}
	ecsOutput := ecs.UpdateTaskProtectionOutput{
		ProtectedTasks: []ecstypes.ProtectedTask{protectedTask},
	}
	ecsRequestID := "reqid"
	ecsErrMessage := "ecs error message"
	happyReqBody := &tp.TaskProtectionRequest{
		ProtectionEnabled: aws.Bool(protectionEnabled),
		ExpiresInMinutes:  aws.Int64(int64(expirationMinutes)),
	}

	// Helper functions to set expectation on mocks
	happyStateExpectations := func(state *mock_dockerstate.MockTaskEngineState) {
		gomock.InOrder(
			state.EXPECT().TaskARNByV3EndpointID(v3EndpointID).Return(taskARN, true),
			state.EXPECT().TaskByArn(taskARN).Return(task, true).Times(2),
			state.EXPECT().ContainerMapByArn(taskARN).Return(containerNameToDockerContainer, true),
			state.EXPECT().TaskByArn(taskARN).Return(task, true),
			state.EXPECT().ContainerByID(containerID).Return(dockerContainer, true).AnyTimes(),
			state.EXPECT().PulledContainerMapByArn(taskARN).Return(nil, true),
		)
	}
	happyCredentialsManagerExpectations := func(credsManager *mock_credentials.MockManager) {
		credsManager.EXPECT().
			GetTaskCredentials(task.GetCredentialsID()).
			Return(taskRoleCredentials(), true)
	}
	taskProtectionClientFactoryExpectations := func(output *ecs.UpdateTaskProtectionOutput, err error) func(
		*gomock.Controller, *tp.MockTaskProtectionClientFactoryInterface,
	) {
		return func(
			ctrl *gomock.Controller,
			factory *tp.MockTaskProtectionClientFactoryInterface,
		) {
			client := mock_ecs.NewMockECSTaskProtectionSDK(ctrl)
			client.EXPECT().UpdateTaskProtection(gomock.Any(), &ecsInput).Return(output, err)
			factory.EXPECT().NewTaskProtectionClient(taskRoleCredentials()).Return(client, nil)
		}
	}

	// Helper function for creating a function that runs a test case
	runTest := func(t *testing.T, tc TMDSTestCase[tptypes.TaskProtectionResponse]) func(*testing.T) {
		return func(t *testing.T) {
			tc.path = fmt.Sprintf("/api/%s/task-protection/v1/state", v3EndpointID)
			tc.method = "PUT"
			testTMDSRequest(t, tc)
		}
	}

	// Test cases start here
	t.Run("task ARN not found", runTest(t, TMDSTestCase[tptypes.TaskProtectionResponse]{
		requestBody: happyReqBody,
		setStateExpectations: func(state *mock_dockerstate.MockTaskEngineState) {
			gomock.InOrder(
				state.EXPECT().TaskARNByV3EndpointID(v3EndpointID).Return("", false),
			)
		},
		expectedStatusCode: http.StatusNotFound,
		expectedResponseBody: tptypes.TaskProtectionResponse{
			Error: &tptypes.ErrorResponse{
				Code:    apierrors.ErrCodeResourceNotFoundException,
				Message: "Failed to find a task for the request",
			},
		},
	}))
	t.Run("task not found", runTest(t, TMDSTestCase[tptypes.TaskProtectionResponse]{
		requestBody: happyReqBody,
		setStateExpectations: func(state *mock_dockerstate.MockTaskEngineState) {
			gomock.InOrder(
				state.EXPECT().TaskARNByV3EndpointID(v3EndpointID).Return(taskARN, true),
				state.EXPECT().TaskByArn(taskARN).Return(nil, false),
			)
		},
		expectedStatusCode: http.StatusInternalServerError,
		expectedResponseBody: tptypes.TaskProtectionResponse{
			Error: &tptypes.ErrorResponse{
				Code:    apierrors.ErrCodeServerException,
				Message: "Failed to find a task for the request",
			},
		},
	}))
	t.Run("task credentials not found", runTest(t, TMDSTestCase[tptypes.TaskProtectionResponse]{
		requestBody:          happyReqBody,
		setStateExpectations: happyStateExpectations,
		setCredentialsManagerExpectations: func(credsManager *mock_credentials.MockManager) {
			credsManager.
				EXPECT().GetTaskCredentials(taskCredentialsID).
				Return(credentials.TaskIAMRoleCredentials{}, false)
		},
		expectedStatusCode: http.StatusForbidden,
		expectedResponseBody: tptypes.TaskProtectionResponse{
			Error: &tptypes.ErrorResponse{
				Arn:     taskARN,
				Code:    apierrors.ErrCodeAccessDeniedException,
				Message: "Invalid Request: no task IAM role credentials available for task",
			},
		},
	}))
	t.Run("ecs call server exception", runTest(t, TMDSTestCase[tptypes.TaskProtectionResponse]{
		requestBody:                       happyReqBody,
		setStateExpectations:              happyStateExpectations,
		setCredentialsManagerExpectations: happyCredentialsManagerExpectations,
		setTaskProtectionClientFactoryExpectations: taskProtectionClientFactoryExpectations(
			nil,
			&awshttp.ResponseError{
				ResponseError: &smithyhttp.ResponseError{
					Response: &smithyhttp.Response{
						Response: &http.Response{
							StatusCode: http.StatusInternalServerError,
						},
					},
					Err: &ecstypes.ServerException{Message: &ecsErrMessage},
				},
				RequestID: ecsRequestID,
			},
		),
		expectedStatusCode: http.StatusInternalServerError,
		expectedResponseBody: tptypes.TaskProtectionResponse{
			RequestID: &ecsRequestID,
			Error: &tptypes.ErrorResponse{
				Arn:     taskARN,
				Code:    apierrors.ErrCodeServerException,
				Message: ecsErrMessage,
			},
		},
	}))
	t.Run("ecs call access denied exception", runTest(t, TMDSTestCase[tptypes.TaskProtectionResponse]{
		requestBody:                       happyReqBody,
		setStateExpectations:              happyStateExpectations,
		setCredentialsManagerExpectations: happyCredentialsManagerExpectations,
		setTaskProtectionClientFactoryExpectations: taskProtectionClientFactoryExpectations(
			nil,
			&awshttp.ResponseError{
				ResponseError: &smithyhttp.ResponseError{
					Response: &smithyhttp.Response{
						Response: &http.Response{
							StatusCode: http.StatusBadRequest,
						},
					},
					Err: &ecstypes.AccessDeniedException{Message: &ecsErrMessage},
				},
				RequestID: ecsRequestID,
			},
		),
		expectedStatusCode: http.StatusBadRequest,
		expectedResponseBody: tptypes.TaskProtectionResponse{
			RequestID: &ecsRequestID,
			Error: &tptypes.ErrorResponse{
				Arn:     taskARN,
				Code:    apierrors.ErrCodeAccessDeniedException,
				Message: ecsErrMessage,
			},
		},
	}))
	t.Run("ecs call non-request-failure aws error", runTest(t, TMDSTestCase[tptypes.TaskProtectionResponse]{
		requestBody:                       happyReqBody,
		setStateExpectations:              happyStateExpectations,
		setCredentialsManagerExpectations: happyCredentialsManagerExpectations,
		setTaskProtectionClientFactoryExpectations: taskProtectionClientFactoryExpectations(
			nil,
			&ecstypes.InvalidParameterException{Message: &ecsErrMessage}),
		expectedStatusCode: http.StatusInternalServerError,
		expectedResponseBody: tptypes.TaskProtectionResponse{
			Error: &tptypes.ErrorResponse{
				Arn:     taskARN,
				Code:    apierrors.ErrCodeInvalidParameterException,
				Message: ecsErrMessage,
			},
		},
	}))
	t.Run("agent timeout", runTest(t, TMDSTestCase[tptypes.TaskProtectionResponse]{
		requestBody:                       happyReqBody,
		setStateExpectations:              happyStateExpectations,
		setCredentialsManagerExpectations: happyCredentialsManagerExpectations,
		setTaskProtectionClientFactoryExpectations: taskProtectionClientFactoryExpectations(
			nil, &smithy.CanceledError{}),
		expectedStatusCode: http.StatusGatewayTimeout,
		expectedResponseBody: tptypes.TaskProtectionResponse{
			Error: &tptypes.ErrorResponse{
				Arn:     taskARN,
				Code:    apierrors.ErrCodeRequestCanceled,
				Message: "Timed out calling ECS Task Protection API",
			},
		},
	}))
	t.Run("non-aws error", runTest(t, TMDSTestCase[tptypes.TaskProtectionResponse]{
		requestBody:                       happyReqBody,
		setStateExpectations:              happyStateExpectations,
		setCredentialsManagerExpectations: happyCredentialsManagerExpectations,
		setTaskProtectionClientFactoryExpectations: taskProtectionClientFactoryExpectations(
			nil, errors.New("some error")),
		expectedStatusCode: http.StatusInternalServerError,
		expectedResponseBody: tptypes.TaskProtectionResponse{
			Error: &tptypes.ErrorResponse{
				Arn:     taskARN,
				Code:    apierrors.ErrCodeServerException,
				Message: "some error",
			},
		},
	}))
	t.Run("ecs failure", runTest(t, TMDSTestCase[tptypes.TaskProtectionResponse]{
		requestBody:                       happyReqBody,
		setStateExpectations:              happyStateExpectations,
		setCredentialsManagerExpectations: happyCredentialsManagerExpectations,
		setTaskProtectionClientFactoryExpectations: taskProtectionClientFactoryExpectations(
			&ecs.UpdateTaskProtectionOutput{
				Failures: []ecstypes.Failure{{
					Arn:    aws.String(taskARN),
					Reason: aws.String("ecs failure"),
				}},
			}, nil),
		expectedStatusCode: http.StatusOK,
		expectedResponseBody: tptypes.TaskProtectionResponse{
			Failure: &ecstypes.Failure{
				Arn:    aws.String(taskARN),
				Reason: aws.String("ecs failure"),
			},
		},
	}))
	t.Run("more than on ecs failure", runTest(t, TMDSTestCase[tptypes.TaskProtectionResponse]{
		requestBody:                       happyReqBody,
		setStateExpectations:              happyStateExpectations,
		setCredentialsManagerExpectations: happyCredentialsManagerExpectations,
		setTaskProtectionClientFactoryExpectations: taskProtectionClientFactoryExpectations(
			&ecs.UpdateTaskProtectionOutput{
				Failures: []ecstypes.Failure{
					{
						Arn:    aws.String(taskARN),
						Reason: aws.String("ecs failure 1"),
					},
					{
						Arn:    aws.String(taskARN),
						Reason: aws.String("ecs failure 2"),
					},
				},
			}, nil),
		expectedStatusCode: http.StatusInternalServerError,
		expectedResponseBody: tptypes.TaskProtectionResponse{
			Error: &tptypes.ErrorResponse{
				Arn:     taskARN,
				Code:    apierrors.ErrCodeServerException,
				Message: "Unexpected error occurred",
			},
		},
	}))
	t.Run("empty request", runTest(t, TMDSTestCase[tptypes.TaskProtectionResponse]{
		requestBody:          map[string]string{},
		setStateExpectations: happyStateExpectations,
		expectedStatusCode:   http.StatusBadRequest,
		expectedResponseBody: tptypes.TaskProtectionResponse{
			Error: &tptypes.ErrorResponse{
				Arn:     taskARN,
				Code:    apierrors.ErrCodeInvalidParameterException,
				Message: "Invalid request: does not contain 'ProtectionEnabled' field",
			},
		},
	}))
	t.Run("invalid type in request", runTest(t, TMDSTestCase[tptypes.TaskProtectionResponse]{
		requestBody: map[string]interface{}{
			"ProtectionEnabled": true,
			"ExpiresInMinutes":  "bad",
		},
		expectedStatusCode: http.StatusBadRequest,
		expectedResponseBody: tptypes.TaskProtectionResponse{
			Error: &tptypes.ErrorResponse{
				Code:    apierrors.ErrCodeInvalidParameterException,
				Message: "UpdateTaskProtection: failed to decode request",
			},
		},
	}))
	t.Run("unknown fields in the request", runTest(t, TMDSTestCase[tptypes.TaskProtectionResponse]{
		requestBody: map[string]interface{}{
			"ProtectionEnabled": true,
			"ExpiresInMinutes":  5,
			"Unknown":           "unknown",
		},
		expectedStatusCode: http.StatusBadRequest,
		expectedResponseBody: tptypes.TaskProtectionResponse{
			Error: &tptypes.ErrorResponse{
				Code:    apierrors.ErrCodeInvalidParameterException,
				Message: "UpdateTaskProtection: failed to decode request",
			},
		},
	}))
	t.Run("non-JSON object request", runTest(t, TMDSTestCase[tptypes.TaskProtectionResponse]{
		requestBody:        "bad",
		expectedStatusCode: http.StatusBadRequest,
		expectedResponseBody: tptypes.TaskProtectionResponse{
			Error: &tptypes.ErrorResponse{
				Code:    apierrors.ErrCodeInvalidParameterException,
				Message: "UpdateTaskProtection: failed to decode request",
			},
		},
	}))
	t.Run("happy case", runTest(t, TMDSTestCase[tptypes.TaskProtectionResponse]{
		requestBody:                                happyReqBody,
		setStateExpectations:                       happyStateExpectations,
		setCredentialsManagerExpectations:          happyCredentialsManagerExpectations,
		setTaskProtectionClientFactoryExpectations: taskProtectionClientFactoryExpectations(&ecsOutput, nil),
		expectedStatusCode:                         http.StatusOK,
		expectedResponseBody: tptypes.TaskProtectionResponse{
			Protection: &protectedTask,
		},
	}))
}

type execExpectations func(exec *mock_execwrapper.MockExec, ctrl *gomock.Controller)

type networkFaultTestCase struct {
	name                  string
	expectedStatusCode    int
	requestBody           interface{}
	expectedFaultResponse faulttype.NetworkFaultInjectionResponse
	setStateExpectations  func(state *mock_dockerstate.MockTaskEngineState, enableFaultInjection bool, networkMode string)
	setExecExpectations   execExpectations
	enableFaultInjection  bool
	networkMode           string
}

// generateCommonNetworkFaultInjectionTestCases generates and returns the happy cases for all network fault injection requests
// Note: A more robust test cases is defined in the actual HTTP handler directory
func generateCommonNetworkFaultInjectionTestCases(requestType, successResponse string, exec execExpectations, requestBody interface{}) []networkFaultTestCase {
	tcs := []networkFaultTestCase{
		{
			name:                  fmt.Sprintf("%s success host mode", requestType),
			expectedStatusCode:    200,
			requestBody:           requestBody,
			expectedFaultResponse: faulttype.NewNetworkFaultInjectionSuccessResponse(successResponse),
			setStateExpectations:  agentStateExpectations,
			setExecExpectations:   exec,
			enableFaultInjection:  true,
			networkMode:           apitask.HostNetworkMode,
		},
		{
			name:                  fmt.Sprintf("%s success awsvpc mode", requestType),
			expectedStatusCode:    200,
			requestBody:           requestBody,
			expectedFaultResponse: faulttype.NewNetworkFaultInjectionSuccessResponse(successResponse),
			setStateExpectations:  agentStateExpectations,
			setExecExpectations:   exec,
			enableFaultInjection:  true,
			networkMode:           apitask.AWSVPCNetworkMode,
		},
	}
	return tcs
}

func TestRegisterStartBlackholePortFaultHandler(t *testing.T) {
	setExecExpectations := func(exec *mock_execwrapper.MockExec, ctrl *gomock.Controller) {
		ctx, cancel := context.WithTimeout(context.Background(), requestTimeoutDuration)
		cmdExec := mock_execwrapper.NewMockCmd(ctrl)
		gomock.InOrder(
			exec.EXPECT().NewExecContextWithTimeout(gomock.Any(), gomock.Any()).Times(1).Return(ctx, cancel),
			exec.EXPECT().CommandContext(gomock.Any(), gomock.Any(), gomock.Any()).Times(1).Return(cmdExec),
			cmdExec.EXPECT().CombinedOutput().Times(1).Return([]byte(iptablesChainNotFoundError), errors.New("exit status 1")),
			exec.EXPECT().ConvertToExitError(gomock.Any()).Times(1).Return(nil, true),
			exec.EXPECT().GetExitCode(gomock.Any()).Times(1).Return(1),
			// Inject the fault for IPv4
			exec.EXPECT().CommandContext(gomock.Any(), gomock.Any(), gomock.Any()).Times(1).Return(cmdExec),
			cmdExec.EXPECT().CombinedOutput().Times(1).Return([]byte{}, nil),
			exec.EXPECT().CommandContext(gomock.Any(), gomock.Any(), gomock.Any()).Times(1).Return(cmdExec),
			cmdExec.EXPECT().CombinedOutput().Times(1).Return([]byte{}, nil),
			exec.EXPECT().CommandContext(gomock.Any(), gomock.Any(), gomock.Any()).Times(1).Return(cmdExec),
			cmdExec.EXPECT().CombinedOutput().Times(1).Return([]byte{}, nil),
			// Inject the fault for IPv6
			exec.EXPECT().CommandContext(gomock.Any(), gomock.Any(), gomock.Any()).Times(1).Return(cmdExec),
			cmdExec.EXPECT().CombinedOutput().Times(1).Return([]byte{}, nil),
			exec.EXPECT().CommandContext(gomock.Any(), gomock.Any(), gomock.Any()).Times(1).Return(cmdExec),
			cmdExec.EXPECT().CombinedOutput().Times(1).Return([]byte{}, nil),
			exec.EXPECT().CommandContext(gomock.Any(), gomock.Any(), gomock.Any()).Times(1).Return(cmdExec),
			cmdExec.EXPECT().CombinedOutput().Times(1).Return([]byte{}, nil),
		)
	}
	tcs := generateCommonNetworkFaultInjectionTestCases("start blackhole port", "running", setExecExpectations, happyBlackHolePortReqBody)
	testRegisterFaultHandler(t, tcs, faulthandler.NetworkFaultPath(faulttype.BlackHolePortFaultType, faulttype.StartNetworkFaultPostfix), faulttype.StartNetworkFaultPostfix, faulttype.BlackHolePortFaultType)
}

func TestRegisterStopBlackholePortFaultHandler(t *testing.T) {
	setExecExpectations := func(exec *mock_execwrapper.MockExec, ctrl *gomock.Controller) {
		ctx, cancel := context.WithTimeout(context.Background(), requestTimeoutDuration)
		cmdExec := mock_execwrapper.NewMockCmd(ctrl)
		gomock.InOrder(
			exec.EXPECT().NewExecContextWithTimeout(gomock.Any(), gomock.Any()).Times(1).Return(ctx, cancel),
			exec.EXPECT().CommandContext(gomock.Any(), gomock.Any(), gomock.Any()).Times(1).Return(cmdExec),
			cmdExec.EXPECT().CombinedOutput().Times(1).Return([]byte{}, nil),
			// Remove the fault for IPv4
			exec.EXPECT().CommandContext(gomock.Any(), gomock.Any(), gomock.Any()).Times(1).Return(cmdExec),
			cmdExec.EXPECT().CombinedOutput().Times(1).Return([]byte{}, nil),
			exec.EXPECT().CommandContext(gomock.Any(), gomock.Any(), gomock.Any()).Times(1).Return(cmdExec),
			cmdExec.EXPECT().CombinedOutput().Times(1).Return([]byte{}, nil),
			exec.EXPECT().CommandContext(gomock.Any(), gomock.Any(), gomock.Any()).Times(1).Return(cmdExec),
			cmdExec.EXPECT().CombinedOutput().Times(1).Return([]byte{}, nil),
			// Remove the fault for IPv6
			exec.EXPECT().CommandContext(gomock.Any(), gomock.Any(), gomock.Any()).Times(1).Return(cmdExec),
			cmdExec.EXPECT().CombinedOutput().Times(1).Return([]byte{}, nil),
			exec.EXPECT().CommandContext(gomock.Any(), gomock.Any(), gomock.Any()).Times(1).Return(cmdExec),
			cmdExec.EXPECT().CombinedOutput().Times(1).Return([]byte{}, nil),
			exec.EXPECT().CommandContext(gomock.Any(), gomock.Any(), gomock.Any()).Times(1).Return(cmdExec),
			cmdExec.EXPECT().CombinedOutput().Times(1).Return([]byte{}, nil),
		)
	}
	tcs := generateCommonNetworkFaultInjectionTestCases("stop blackhole port", "stopped", setExecExpectations, happyBlackHolePortReqBody)
	testRegisterFaultHandler(t, tcs, faulthandler.NetworkFaultPath(faulttype.BlackHolePortFaultType, faulttype.StopNetworkFaultPostfix), faulttype.StopNetworkFaultPostfix, faulttype.BlackHolePortFaultType)
}

func TestRegisterCheckBlackholePortFaultHandler(t *testing.T) {
	setExecExpectations := func(exec *mock_execwrapper.MockExec, ctrl *gomock.Controller) {
		ctx, cancel := context.WithTimeout(context.Background(), requestTimeoutDuration)
		cmdExec := mock_execwrapper.NewMockCmd(ctrl)
		gomock.InOrder(
			exec.EXPECT().NewExecContextWithTimeout(gomock.Any(), gomock.Any()).Times(1).Return(ctx, cancel),
			exec.EXPECT().CommandContext(gomock.Any(), gomock.Any(), gomock.Any()).Times(1).Return(cmdExec),
			cmdExec.EXPECT().CombinedOutput().Times(1).Return([]byte{}, nil),
		)
	}
	tcs := generateCommonNetworkFaultInjectionTestCases("check blackhole port", "running", setExecExpectations, happyBlackHolePortReqBody)
	testRegisterFaultHandler(t, tcs, faulthandler.NetworkFaultPath(faulttype.BlackHolePortFaultType, faulttype.CheckNetworkFaultPostfix), faulttype.CheckNetworkFaultPostfix, faulttype.BlackHolePortFaultType)
}

func TestRegisterStartLatencyFaultHandler(t *testing.T) {
	setExecExpectations := func(exec *mock_execwrapper.MockExec, ctrl *gomock.Controller) {
		ctx, cancel := context.WithTimeout(context.Background(), requestTimeoutDuration)
		mockCMD := mock_execwrapper.NewMockCmd(ctrl)
		gomock.InOrder(
			exec.EXPECT().NewExecContextWithTimeout(gomock.Any(), gomock.Any()).Times(1).Return(ctx, cancel),
			exec.EXPECT().CommandContext(gomock.Any(), gomock.Any(), gomock.Any()).Times(1).Return(mockCMD),
			mockCMD.EXPECT().CombinedOutput().Times(1).Return([]byte(tcCommandEmptyOutput), nil),
		)
		exec.EXPECT().CommandContext(gomock.Any(), gomock.Any(), gomock.Any()).Times(5).Return(mockCMD)
		mockCMD.EXPECT().CombinedOutput().Times(5).Return([]byte(tcCommandEmptyOutput), nil)
	}
	tcs := generateCommonNetworkFaultInjectionTestCases("start latency", "running", setExecExpectations, happyNetworkLatencyReqBody)
	testRegisterFaultHandler(t, tcs, faulthandler.NetworkFaultPath(faulttype.LatencyFaultType, faulttype.StartNetworkFaultPostfix), faulttype.StartNetworkFaultPostfix, faulttype.LatencyFaultType)
}

func TestRegisterStopLatencyFaultHandler(t *testing.T) {
	setExecExpectations := func(exec *mock_execwrapper.MockExec, ctrl *gomock.Controller) {
		ctx, cancel := context.WithTimeout(context.Background(), requestTimeoutDuration)
		mockCMD := mock_execwrapper.NewMockCmd(ctrl)
		gomock.InOrder(
			exec.EXPECT().NewExecContextWithTimeout(gomock.Any(), gomock.Any()).Times(1).Return(ctx, cancel),
			exec.EXPECT().CommandContext(gomock.Any(), gomock.Any(), gomock.Any()).Times(1).Return(mockCMD),
			mockCMD.EXPECT().CombinedOutput().Times(1).Return([]byte(tcCommandEmptyOutput), nil),
		)
	}
	tcs := generateCommonNetworkFaultInjectionTestCases("stop latency", "stopped", setExecExpectations, nil)
	testRegisterFaultHandler(t, tcs, faulthandler.NetworkFaultPath(faulttype.LatencyFaultType, faulttype.StopNetworkFaultPostfix), faulttype.StopNetworkFaultPostfix, faulttype.LatencyFaultType)
}

func TestRegisterCheckLatencyFaultHandler(t *testing.T) {
	setExecExpectations := func(exec *mock_execwrapper.MockExec, ctrl *gomock.Controller) {
		ctx, cancel := context.WithTimeout(context.Background(), requestTimeoutDuration)
		mockCMD := mock_execwrapper.NewMockCmd(ctrl)
		gomock.InOrder(
			exec.EXPECT().NewExecContextWithTimeout(gomock.Any(), gomock.Any()).Times(1).Return(ctx, cancel),
			exec.EXPECT().CommandContext(gomock.Any(), gomock.Any(), gomock.Any()).Times(1).Return(mockCMD),
			mockCMD.EXPECT().CombinedOutput().Times(1).Return([]byte(tcLatencyFaultExistsCommandOutput), nil),
		)
	}
	tcs := generateCommonNetworkFaultInjectionTestCases("check latency", "running", setExecExpectations, nil)
	testRegisterFaultHandler(t, tcs, faulthandler.NetworkFaultPath(faulttype.LatencyFaultType, faulttype.CheckNetworkFaultPostfix), faulttype.CheckNetworkFaultPostfix, faulttype.LatencyFaultType)
}

func TestRegisterStartPacketLossFaultHandler(t *testing.T) {
	setExecExpectations := func(exec *mock_execwrapper.MockExec, ctrl *gomock.Controller) {
		ctx, cancel := context.WithTimeout(context.Background(), requestTimeoutDuration)
		mockCMD := mock_execwrapper.NewMockCmd(ctrl)
		gomock.InOrder(
			exec.EXPECT().NewExecContextWithTimeout(gomock.Any(), gomock.Any()).Times(1).Return(ctx, cancel),
			exec.EXPECT().CommandContext(gomock.Any(), gomock.Any(), gomock.Any()).Times(1).Return(mockCMD),
			mockCMD.EXPECT().CombinedOutput().Times(1).Return([]byte(tcCommandEmptyOutput), nil),
		)
		exec.EXPECT().CommandContext(gomock.Any(), gomock.Any(), gomock.Any()).Times(5).Return(mockCMD)
		mockCMD.EXPECT().CombinedOutput().Times(5).Return([]byte(tcCommandEmptyOutput), nil)
	}
	tcs := generateCommonNetworkFaultInjectionTestCases("start packet loss", "running", setExecExpectations, happyNetworkPacketLossReqBody)
	testRegisterFaultHandler(t, tcs, faulthandler.NetworkFaultPath(faulttype.PacketLossFaultType, faulttype.StartNetworkFaultPostfix), faulttype.StartNetworkFaultPostfix, faulttype.PacketLossFaultType)
}

func TestRegisterStopPacketLossFaultHandler(t *testing.T) {
	setExecExpectations := func(exec *mock_execwrapper.MockExec, ctrl *gomock.Controller) {
		ctx, cancel := context.WithTimeout(context.Background(), requestTimeoutDuration)
		mockCMD := mock_execwrapper.NewMockCmd(ctrl)
		gomock.InOrder(
			exec.EXPECT().NewExecContextWithTimeout(gomock.Any(), gomock.Any()).Times(1).Return(ctx, cancel),
			exec.EXPECT().CommandContext(gomock.Any(), gomock.Any(), gomock.Any()).Times(1).Return(mockCMD),
			mockCMD.EXPECT().CombinedOutput().Times(1).Return([]byte(tcCommandEmptyOutput), nil),
		)
	}
	tcs := generateCommonNetworkFaultInjectionTestCases("stop packet loss", "stopped", setExecExpectations, nil)
	testRegisterFaultHandler(t, tcs, faulthandler.NetworkFaultPath(faulttype.PacketLossFaultType, faulttype.StopNetworkFaultPostfix), faulttype.StopNetworkFaultPostfix, faulttype.PacketLossFaultType)
}

func TestRegisterCheckPacketLossFaultHandler(t *testing.T) {
	setExecExpectations := func(exec *mock_execwrapper.MockExec, ctrl *gomock.Controller) {
		ctx, cancel := context.WithTimeout(context.Background(), requestTimeoutDuration)
		mockCMD := mock_execwrapper.NewMockCmd(ctrl)
		gomock.InOrder(
			exec.EXPECT().NewExecContextWithTimeout(gomock.Any(), gomock.Any()).Times(1).Return(ctx, cancel),
			exec.EXPECT().CommandContext(gomock.Any(), gomock.Any(), gomock.Any()).Times(1).Return(mockCMD),
			mockCMD.EXPECT().CombinedOutput().Times(1).Return([]byte(tcLossFaultExistsCommandOutput), nil),
		)
	}
	tcs := generateCommonNetworkFaultInjectionTestCases("check packet loss", "running", setExecExpectations, nil)
	testRegisterFaultHandler(t, tcs, faulthandler.NetworkFaultPath(faulttype.PacketLossFaultType, faulttype.CheckNetworkFaultPostfix), faulttype.CheckNetworkFaultPostfix, faulttype.PacketLossFaultType)
}

func testRegisterFaultHandler(t *testing.T, tcs []networkFaultTestCase, tmdsEndpoint, faultOperation, faultType string) {
	for _, tc := range tcs {
		t.Run(tc.name, func(t *testing.T) {
			// Mocks
			ctrl := gomock.NewController(t)
			defer ctrl.Finish()

			state := mock_dockerstate.NewMockTaskEngineState(ctrl)
			statsEngine := mock_stats.NewMockEngine(ctrl)
			ecsClient := mock_ecs.NewMockECSClient(ctrl)

			agentState := agentV4.NewTMDSAgentState(state, statsEngine, ecsClient, clusterName, availabilityzone, vpcID, containerInstanceArn)
			metricsFactory := mock_metrics.NewMockEntryFactory(ctrl)
			durationMetricEntry := mock_metrics.NewMockEntry(ctrl)
			gomock.InOrder(
				metricsFactory.EXPECT().New(fmt.Sprintf(durationMetricPrefix, faultOperation, faultType)).Return(durationMetricEntry).Times(1),
				durationMetricEntry.EXPECT().WithFields(gomock.Any()).Return(durationMetricEntry).Times(1),
				durationMetricEntry.EXPECT().WithGauge(gomock.Any()).Return(durationMetricEntry).Times(1),
				durationMetricEntry.EXPECT().Done(nil).Times(1),
			)
			execWrapper := mock_execwrapper.NewMockExec(ctrl)

			if tc.setStateExpectations != nil {
				tc.setStateExpectations(state, tc.enableFaultInjection, tc.networkMode)
			}

			if tc.setExecExpectations != nil {
				tc.setExecExpectations(execWrapper, ctrl)
			}

			var tmdsAPI string
			switch tmdsEndpoint {
			case faulthandler.NetworkFaultPath(faulttype.BlackHolePortFaultType, faulttype.StartNetworkFaultPostfix):
				tmdsAPI = "/api/%s/fault/v1/network-blackhole-port/start"
			case faulthandler.NetworkFaultPath(faulttype.BlackHolePortFaultType, faulttype.StopNetworkFaultPostfix):
				tmdsAPI = "/api/%s/fault/v1/network-blackhole-port/stop"
			case faulthandler.NetworkFaultPath(faulttype.BlackHolePortFaultType, faulttype.CheckNetworkFaultPostfix):
				tmdsAPI = "/api/%s/fault/v1/network-blackhole-port/status"
			case faulthandler.NetworkFaultPath(faulttype.LatencyFaultType, faulttype.StartNetworkFaultPostfix):
				tmdsAPI = "/api/%s/fault/v1/network-latency/start"
			case faulthandler.NetworkFaultPath(faulttype.LatencyFaultType, faulttype.StopNetworkFaultPostfix):
				tmdsAPI = "/api/%s/fault/v1/network-latency/stop"
			case faulthandler.NetworkFaultPath(faulttype.LatencyFaultType, faulttype.CheckNetworkFaultPostfix):
				tmdsAPI = "/api/%s/fault/v1/network-latency/status"
			case faulthandler.NetworkFaultPath(faulttype.PacketLossFaultType, faulttype.StartNetworkFaultPostfix):
				tmdsAPI = "/api/%s/fault/v1/network-packet-loss/start"
			case faulthandler.NetworkFaultPath(faulttype.PacketLossFaultType, faulttype.StopNetworkFaultPostfix):
				tmdsAPI = "/api/%s/fault/v1/network-packet-loss/stop"
			case faulthandler.NetworkFaultPath(faulttype.PacketLossFaultType, faulttype.CheckNetworkFaultPostfix):
				tmdsAPI = "/api/%s/fault/v1/network-packet-loss/status"
			default:
				t.Error("Unrecognized TMDS Endpoint")
			}

			router := mux.NewRouter()
			registerFaultHandlers(router, agentState, metricsFactory, execWrapper)
			var requestBody io.Reader
			if tc.requestBody != "" {
				reqBodyBytes, err := json.Marshal(tc.requestBody)
				require.NoError(t, err)
				requestBody = bytes.NewReader(reqBodyBytes)
			}

			req, err := http.NewRequest("POST", fmt.Sprintf(tmdsAPI, endpointId),
				requestBody)
			require.NoError(t, err)

			// Send the request and record the response
			recorder := httptest.NewRecorder()
			router.ServeHTTP(recorder, req)

			var actualResponseBody faulttype.NetworkFaultInjectionResponse
			err = json.Unmarshal(recorder.Body.Bytes(), &actualResponseBody)
			require.NoError(t, err)

			assert.Equal(t, tc.expectedStatusCode, recorder.Code)
			assert.Equal(t, tc.expectedFaultResponse, actualResponseBody)
		})
	}
}
