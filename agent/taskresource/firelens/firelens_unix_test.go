//go:build linux && unit
// +build linux,unit

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

package firelens

import (
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"testing"

	"github.com/aws/amazon-ecs-agent/agent/config"
	mockfactory "github.com/aws/amazon-ecs-agent/agent/s3/factory/mocks"
	mocks3 "github.com/aws/amazon-ecs-agent/agent/s3/mocks/s3manager"
	"github.com/aws/amazon-ecs-agent/agent/taskresource"
	resourcestatus "github.com/aws/amazon-ecs-agent/agent/taskresource/status"
	mockioutilwrapper "github.com/aws/amazon-ecs-agent/agent/utils/ioutilwrapper/mocks"
	"github.com/aws/amazon-ecs-agent/agent/utils/oswrapper"
	mockoswrapper "github.com/aws/amazon-ecs-agent/agent/utils/oswrapper/mocks"
	"github.com/aws/amazon-ecs-agent/ecs-agent/api/task/status"
	"github.com/aws/amazon-ecs-agent/ecs-agent/credentials"
	mockcredentials "github.com/aws/amazon-ecs-agent/ecs-agent/credentials/mocks"
	"github.com/aws/amazon-ecs-agent/ecs-agent/ipcompatibility"

	"github.com/aws/aws-sdk-go-v2/aws"
	s3manager "github.com/aws/aws-sdk-go-v2/feature/s3/manager"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/golang/mock/gomock"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const (
	testCluster                = "mycluster"
	testTaskARN                = "arn:aws:ecs:us-east-2:01234567891011:task/mycluster/3de392df-6bfa-470b-97ed-aa6f482cd7a"
	testTaskDefinition         = "taskdefinition:1"
	testEC2InstanceID          = "i-123456789a"
	testDataDir                = "testdatadir"
	testResourceDir            = "testresourcedir"
	testTerminalResason        = "testterminalreason"
	testTempFile               = "testtempfile"
	testRegion                 = "us-west-2"
	testExecutionCredentialsID = "testexecutioncredentialsid"
	testExternalConfigType     = "testexternalconfigtype"
	testExternalConfigValue    = "testexternalconfigvalue"
	testContainerMemoryLimit   = 100
)

var (
	testFirelensOptionsFile = map[string]string{
		"enable-ecs-log-metadata": "true",
		"config-file-type":        "file",
		"config-file-value":       "/tmp/dummy.conf",
	}

	testFirelensOptionsS3 = map[string]string{
		"enable-ecs-log-metadata": "true",
		"config-file-type":        "s3",
		"config-file-value":       "arn:aws:s3:::bucket/key",
	}

	testIPCompatibility = ipcompatibility.NewIPCompatibility(true, true)
)

func setup(t *testing.T) (oswrapper.File, *mockioutilwrapper.MockIOUtil,
	*mockcredentials.MockManager, *mockfactory.MockS3ClientCreator, *mocks3.MockS3ManagerClient, func()) {
	ctrl := gomock.NewController(t)

	mockFile := mockoswrapper.NewMockFile()
	mockIOUtil := mockioutilwrapper.NewMockIOUtil(ctrl)
	mockCredentialsManager := mockcredentials.NewMockManager(ctrl)
	mockS3ClientCreator := mockfactory.NewMockS3ClientCreator(ctrl)
	mockS3Client := mocks3.NewMockS3ManagerClient(ctrl)

	return mockFile, mockIOUtil, mockCredentialsManager, mockS3ClientCreator, mockS3Client, ctrl.Finish
}

func mockRename() func() {
	rename = func(oldpath, newpath string) error {
		return nil
	}

	return func() {
		rename = os.Rename
	}
}

func mockMkdirAllError() func() {
	mkdirAll = func(path string, perm os.FileMode) error {
		return errors.New("test error")
	}

	return func() {
		mkdirAll = os.MkdirAll
	}
}

func newMockFirelensResource(firelensConfigType, networkMode string, lopOptions map[string]string,
	mockIOUtil *mockioutilwrapper.MockIOUtil, mockCredentialsManager *mockcredentials.MockManager,
	mockS3ClientCreator *mockfactory.MockS3ClientCreator, containerMemoryReservation int64, ipCompatibility ipcompatibility.IPCompatibility) *FirelensResource {
	return &FirelensResource{
		cluster:            testCluster,
		taskARN:            testTaskARN,
		taskDefinition:     testTaskDefinition,
		ec2InstanceID:      testEC2InstanceID,
		resourceDir:        testResourceDir,
		firelensConfigType: firelensConfigType,
		region:             testRegion,
		networkMode:        networkMode,
		containerToLogOptions: map[string]map[string]string{
			"container": lopOptions,
		},
		executionCredentialsID: testExecutionCredentialsID,
		credentialsManager:     mockCredentialsManager,
		ioutil:                 mockIOUtil,
		s3ClientCreator:        mockS3ClientCreator,
		containerMemoryLimit:   containerMemoryReservation,
		ipCompatibility:        ipCompatibility,
	}
}

func TestParseOptions(t *testing.T) {
	firelensResource := FirelensResource{}
	err := firelensResource.parseOptions(testFirelensOptionsFile)
	assert.NoError(t, err)
	assert.Equal(t, true, firelensResource.ecsMetadataEnabled)
	assert.Equal(t, "file", firelensResource.externalConfigType)
	assert.Equal(t, "/tmp/dummy.conf", firelensResource.externalConfigValue)
}

func TestParseOptionsInvalidType(t *testing.T) {
	options := map[string]string{
		"enable-ecs-log-metadata": "true",
		"config-file-type":        "invalid",
		"config-file-value":       "xxx",
	}
	firelensResource := FirelensResource{}
	assert.Error(t, firelensResource.parseOptions(options))
}

func TestParseOptionsNoValue(t *testing.T) {
	options := map[string]string{
		"enable-ecs-log-metadata": "true",
		"config-file-type":        "file",
	}
	firelensResource := FirelensResource{}
	assert.Error(t, firelensResource.parseOptions(options))
}

func TestCreateFirelensResourceFluentdBridgeMode(t *testing.T) {
	mockFile, mockIOUtil, mockCredentialsManager, mockS3ClientCreator, _, done := setup(t)
	defer done()

	firelensResource := newMockFirelensResource(FirelensConfigTypeFluentd, bridgeNetworkMode, testFluentdOptions, mockIOUtil,
		mockCredentialsManager, mockS3ClientCreator, testContainerMemoryLimit, testIPCompatibility)

	defer mockRename()()
	gomock.InOrder(
		mockIOUtil.EXPECT().TempFile(testResourceDir, tempFile).Return(mockFile, nil),
	)

	assert.NoError(t, firelensResource.Create())
}

func TestCreateFirelensResourceFluentdAWSVPCMode(t *testing.T) {
	mockFile, mockIOUtil, mockCredentialsManager, mockS3ClientCreator, _, done := setup(t)
	defer done()

	firelensResource := newMockFirelensResource(FirelensConfigTypeFluentd, awsvpcNetworkMode, testFluentdOptions, mockIOUtil,
		mockCredentialsManager, mockS3ClientCreator, testContainerMemoryLimit, testIPCompatibility)

	defer mockRename()()
	gomock.InOrder(
		mockIOUtil.EXPECT().TempFile(testResourceDir, tempFile).Return(mockFile, nil),
	)

	assert.NoError(t, firelensResource.Create())
}

func TestCreateFirelensResourceFluentdDefaultMode(t *testing.T) {
	mockFile, mockIOUtil, mockCredentialsManager, mockS3ClientCreator, _, done := setup(t)
	defer done()

	firelensResource := newMockFirelensResource(FirelensConfigTypeFluentd, "", testFluentdOptions, mockIOUtil,
		mockCredentialsManager, mockS3ClientCreator, testContainerMemoryLimit, testIPCompatibility)

	defer mockRename()()
	gomock.InOrder(
		mockIOUtil.EXPECT().TempFile(testResourceDir, tempFile).Return(mockFile, nil),
	)

	assert.NoError(t, firelensResource.Create())
}

func TestCreateFirelensResourceFluentbit(t *testing.T) {
	mockFile, mockIOUtil, mockCredentialsManager, mockS3ClientCreator, _, done := setup(t)
	defer done()

	firelensResource := newMockFirelensResource(FirelensConfigTypeFluentbit, bridgeNetworkMode, testFluentbitOptions, mockIOUtil,
		mockCredentialsManager, mockS3ClientCreator, testContainerMemoryLimit, testIPCompatibility)

	defer mockRename()()
	gomock.InOrder(
		mockIOUtil.EXPECT().TempFile(testResourceDir, tempFile).Return(mockFile, nil),
	)

	assert.NoError(t, firelensResource.Create())
}

func TestCreateFirelensResourceInvalidType(t *testing.T) {
	_, mockIOUtil, mockCredentialsManager, mockS3ClientCreator, _, done := setup(t)
	defer done()

	firelensResource := newMockFirelensResource(FirelensConfigTypeFluentd, bridgeNetworkMode, testFluentdOptions, mockIOUtil,
		mockCredentialsManager, mockS3ClientCreator, testContainerMemoryLimit, testIPCompatibility)
	firelensResource.firelensConfigType = "invalid"

	assert.Error(t, firelensResource.Create())
	assert.NotEmpty(t, firelensResource.terminalReason)
}

func TestCreateFirelensResourceCreateConfigDirError(t *testing.T) {
	_, mockIOUtil, mockCredentialsManager, mockS3ClientCreator, _, done := setup(t)
	defer done()

	firelensResource := newMockFirelensResource(FirelensConfigTypeFluentd, bridgeNetworkMode, testFluentdOptions, mockIOUtil,
		mockCredentialsManager, mockS3ClientCreator, testContainerMemoryLimit, testIPCompatibility)

	defer mockMkdirAllError()()

	assert.Error(t, firelensResource.Create())
	assert.NotEmpty(t, firelensResource.terminalReason)
}

func TestCreateFirelensResourceCreateSocketDirError(t *testing.T) {
	_, mockIOUtil, mockCredentialsManager, mockS3ClientCreator, _, done := setup(t)
	defer done()

	firelensResource := newMockFirelensResource(FirelensConfigTypeFluentd, bridgeNetworkMode, testFluentdOptions, mockIOUtil,
		mockCredentialsManager, mockS3ClientCreator, testContainerMemoryLimit, testIPCompatibility)

	defer mockMkdirAllError()()

	assert.Error(t, firelensResource.Create())
	assert.NotEmpty(t, firelensResource.terminalReason)
}

func TestCreateFirelensResourceGenerateConfigError(t *testing.T) {
	_, mockIOUtil, mockCredentialsManager, mockS3ClientCreator, _, done := setup(t)
	defer done()

	firelensResource := newMockFirelensResource(FirelensConfigTypeFluentd, bridgeNetworkMode, testFluentdOptions, mockIOUtil,
		mockCredentialsManager, mockS3ClientCreator, testContainerMemoryLimit, testIPCompatibility)
	firelensResource.containerToLogOptions = map[string]map[string]string{
		"container": {
			"invalid": "invalid",
		},
	}

	assert.Error(t, firelensResource.Create())
	assert.NotEmpty(t, firelensResource.terminalReason)
}

func TestCreateFirelensResourceCreateTempFileError(t *testing.T) {
	_, mockIOUtil, mockCredentialsManager, mockS3ClientCreator, _, done := setup(t)
	defer done()

	firelensResource := newMockFirelensResource(FirelensConfigTypeFluentd, bridgeNetworkMode, testFluentdOptions, mockIOUtil,
		mockCredentialsManager, mockS3ClientCreator, testContainerMemoryLimit, testIPCompatibility)

	gomock.InOrder(
		mockIOUtil.EXPECT().TempFile(testResourceDir, tempFile).Return(nil, errors.New("test error")),
	)

	assert.Error(t, firelensResource.Create())
	assert.NotEmpty(t, firelensResource.terminalReason)
}

func TestCreateFirelensResourceWriteConfigFileError(t *testing.T) {
	mockFile, mockIOUtil, mockCredentialsManager, mockS3ClientCreator, _, done := setup(t)
	defer done()

	firelensResource := newMockFirelensResource(FirelensConfigTypeFluentd, bridgeNetworkMode, testFluentdOptions, mockIOUtil,
		mockCredentialsManager, mockS3ClientCreator, testContainerMemoryLimit, testIPCompatibility)

	mockFile.(*mockoswrapper.MockFile).WriteImpl = func(bytes []byte) (i int, e error) {
		return 0, errors.New("test error")
	}

	gomock.InOrder(
		mockIOUtil.EXPECT().TempFile(testResourceDir, tempFile).Return(mockFile, nil),
	)

	assert.Error(t, firelensResource.Create())
	assert.NotEmpty(t, firelensResource.terminalReason)
}

func TestCreateFirelensResourceChmodError(t *testing.T) {
	mockFile, mockIOUtil, mockCredentialsManager, mockS3ClientCreator, _, done := setup(t)
	defer done()

	firelensResource := newMockFirelensResource(FirelensConfigTypeFluentd, bridgeNetworkMode, testFluentdOptions, mockIOUtil,
		mockCredentialsManager, mockS3ClientCreator, testContainerMemoryLimit, testIPCompatibility)

	mockFile.(*mockoswrapper.MockFile).ChmodImpl = func(mode os.FileMode) error {
		return errors.New("test error")
	}

	gomock.InOrder(
		mockIOUtil.EXPECT().TempFile(testResourceDir, tempFile).Return(mockFile, nil),
	)

	assert.Error(t, firelensResource.Create())
	assert.NotEmpty(t, firelensResource.terminalReason)
}

func TestCreateFirelensResourceRenameError(t *testing.T) {
	mockFile, mockIOUtil, mockCredentialsManager, mockS3ClientCreator, _, done := setup(t)
	defer done()

	firelensResource := newMockFirelensResource(FirelensConfigTypeFluentd, bridgeNetworkMode, testFluentdOptions, mockIOUtil,
		mockCredentialsManager, mockS3ClientCreator, testContainerMemoryLimit, testIPCompatibility)

	gomock.InOrder(
		mockIOUtil.EXPECT().TempFile(testResourceDir, tempFile).Return(mockFile, nil),
	)

	rename = func(oldpath, newpath string) error {
		return errors.New("test error")
	}
	defer func() {
		rename = os.Rename
	}()

	assert.Error(t, firelensResource.Create())
	assert.NotEmpty(t, firelensResource.terminalReason)
}

func TestCreateFirelensResourceWithS3Config(t *testing.T) {
	mockFile, mockIOUtil, mockCredentialsManager, mockS3ClientCreator, mockS3Client, done := setup(t)
	defer done()

	firelensResource := newMockFirelensResource(FirelensConfigTypeFluentd, bridgeNetworkMode, testFluentdOptions, mockIOUtil,
		mockCredentialsManager, mockS3ClientCreator, testContainerMemoryLimit, testIPCompatibility)

	err := firelensResource.parseOptions(testFirelensOptionsS3)
	require.NoError(t, err)

	creds := credentials.TaskIAMRoleCredentials{
		ARN: "arn",
		IAMRoleCredentials: credentials.IAMRoleCredentials{
			AccessKeyID:     "id",
			SecretAccessKey: "key",
		},
	}

	defer mockRename()()

	gomock.InOrder(
		mockCredentialsManager.EXPECT().GetTaskCredentials(testExecutionCredentialsID).Return(creds, true),
		mockS3ClientCreator.EXPECT().NewS3ManagerClient("bucket", testRegion, creds.IAMRoleCredentials, testIPCompatibility).Return(mockS3Client, nil),
		// write external config file downloaded from s3
		mockIOUtil.EXPECT().TempFile(testResourceDir, tempFile).Return(mockFile, nil),
		mockS3Client.EXPECT().Download(gomock.Any(), mockFile, gomock.Any(), gomock.Any()).Do(
			func(ctx context.Context, w io.WriterAt, input *s3.GetObjectInput, options ...func(*s3manager.Downloader)) {
				assert.Equal(t, "bucket", aws.ToString(input.Bucket))
				assert.Equal(t, "key", aws.ToString(input.Key))
			}).Return(int64(0), nil),

		// write main config file
		mockIOUtil.EXPECT().TempFile(testResourceDir, tempFile).Return(mockFile, nil),
	)

	assert.NoError(t, firelensResource.Create())
}

func TestCreateFirelensResourceWithS3ConfigMissingCredentials(t *testing.T) {
	_, mockIOUtil, mockCredentialsManager, mockS3ClientCreator, _, done := setup(t)
	defer done()

	firelensResource := newMockFirelensResource(FirelensConfigTypeFluentd, bridgeNetworkMode, testFluentdOptions, mockIOUtil,
		mockCredentialsManager, mockS3ClientCreator, testContainerMemoryLimit, testIPCompatibility)

	err := firelensResource.parseOptions(testFirelensOptionsS3)
	require.NoError(t, err)

	gomock.InOrder(
		mockCredentialsManager.EXPECT().GetTaskCredentials(testExecutionCredentialsID).Return(credentials.TaskIAMRoleCredentials{}, false),
	)

	assert.Error(t, firelensResource.Create())
	assert.NotEmpty(t, firelensResource.terminalReason)
}

func TestCreateFirelensResourceWithS3ConfigInvalidS3ARN(t *testing.T) {
	_, mockIOUtil, mockCredentialsManager, mockS3ClientCreator, _, done := setup(t)
	defer done()

	firelensResource := newMockFirelensResource(FirelensConfigTypeFluentd, bridgeNetworkMode, testFluentdOptions, mockIOUtil,
		mockCredentialsManager, mockS3ClientCreator, testContainerMemoryLimit, testIPCompatibility)

	err := firelensResource.parseOptions(testFirelensOptionsS3)
	require.NoError(t, err)
	firelensResource.externalConfigValue = "arn:s3:::xxx"

	gomock.InOrder(
		mockCredentialsManager.EXPECT().GetTaskCredentials(testExecutionCredentialsID).Return(credentials.TaskIAMRoleCredentials{}, true),
	)

	assert.Error(t, firelensResource.Create())
	assert.NotEmpty(t, firelensResource.terminalReason)
}

func TestCreateFirelensResourceWithS3ConfigDownloadFailure(t *testing.T) {
	mockFile, mockIOUtil, mockCredentialsManager, mockS3ClientCreator, mockS3Client, done := setup(t)
	defer done()

	firelensResource := newMockFirelensResource(FirelensConfigTypeFluentd, bridgeNetworkMode, testFluentdOptions, mockIOUtil,
		mockCredentialsManager, mockS3ClientCreator, testContainerMemoryLimit, testIPCompatibility)

	err := firelensResource.parseOptions(testFirelensOptionsS3)
	require.NoError(t, err)

	creds := credentials.TaskIAMRoleCredentials{
		ARN: "arn",
		IAMRoleCredentials: credentials.IAMRoleCredentials{
			AccessKeyID:     "id",
			SecretAccessKey: "key",
		},
	}
	gomock.InOrder(
		mockCredentialsManager.EXPECT().GetTaskCredentials(testExecutionCredentialsID).Return(creds, true),
		mockS3ClientCreator.EXPECT().NewS3ManagerClient("bucket", testRegion, creds.IAMRoleCredentials, testIPCompatibility).Return(mockS3Client, nil),
		mockIOUtil.EXPECT().TempFile(testResourceDir, tempFile).Return(mockFile, nil),
		mockS3Client.EXPECT().Download(gomock.Any(), mockFile, gomock.Any()).Return(int64(0), errors.New("test error")),
	)

	assert.Error(t, firelensResource.Create())
	assert.NotEmpty(t, firelensResource.terminalReason)
}

func TestCleanupFirelensResource(t *testing.T) {
	_, mockIOUtil, mockCredentialsManager, mockS3ClientCreator, _, done := setup(t)
	defer done()

	firelensResource := newMockFirelensResource(FirelensConfigTypeFluentd, bridgeNetworkMode, testFluentdOptions, mockIOUtil,
		mockCredentialsManager, mockS3ClientCreator, testContainerMemoryLimit, testIPCompatibility)

	assert.NoError(t, firelensResource.Cleanup())
}

func TestCleanupFirelensResourceError(t *testing.T) {
	_, mockIOUtil, mockCredentialsManager, mockS3ClientCreator, _, done := setup(t)
	defer done()

	firelensResource := newMockFirelensResource(FirelensConfigTypeFluentd, bridgeNetworkMode, testFluentdOptions, mockIOUtil,
		mockCredentialsManager, mockS3ClientCreator, testContainerMemoryLimit, testIPCompatibility)

	removeAll = func(path string) error {
		return errors.New("test error")
	}

	defer func() {
		removeAll = os.RemoveAll
	}()

	assert.Error(t, firelensResource.Cleanup())
}

func TestInitializeFirelensResource(t *testing.T) {
	_, _, mockCredentialsManager, _, _, done := setup(t)
	defer done()

	testConfig := &config.Config{InstanceIPCompatibility: testIPCompatibility}

	firelensResource := newMockFirelensResource(FirelensConfigTypeFluentd, bridgeNetworkMode, testFluentdOptions, nil, nil,
		nil, testContainerMemoryLimit, testIPCompatibility)
	firelensResource.Initialize(
		testConfig,
		&taskresource.ResourceFields{
			ResourceFieldsCommon: &taskresource.ResourceFieldsCommon{
				CredentialsManager: mockCredentialsManager,
			},
		}, status.TaskRunning, status.TaskRunning)

	assert.NotNil(t, firelensResource.statusToTransitions)
	assert.Equal(t, 1, len(firelensResource.statusToTransitions))
	assert.NotNil(t, firelensResource.ioutil)
	assert.NotNil(t, firelensResource.s3ClientCreator)
	assert.NotNil(t, firelensResource.credentialsManager)
	assert.NotNil(t, testIPCompatibility, firelensResource.ipCompatibility)
}

func TestSetKnownStatus(t *testing.T) {
	firelensResource := newMockFirelensResource(FirelensConfigTypeFluentd, bridgeNetworkMode, testFluentdOptions, nil, nil,
		nil, testContainerMemoryLimit, testIPCompatibility)
	firelensResource.appliedStatusUnsafe = resourcestatus.ResourceStatus(FirelensCreated)

	firelensResource.SetKnownStatus(resourcestatus.ResourceStatus(FirelensCreated))
	assert.Equal(t, resourcestatus.ResourceStatus(FirelensCreated), firelensResource.knownStatusUnsafe)
	assert.Equal(t, resourcestatus.ResourceStatus(FirelensStatusNone), firelensResource.appliedStatusUnsafe)
}

func TestSetKnownStatusNoAppliedStatusUpdate(t *testing.T) {
	firelensResource := newMockFirelensResource(FirelensConfigTypeFluentd, bridgeNetworkMode, testFluentdOptions, nil, nil,
		nil, testContainerMemoryLimit, testIPCompatibility)
	firelensResource.appliedStatusUnsafe = resourcestatus.ResourceStatus(FirelensCreated)

	firelensResource.SetKnownStatus(resourcestatus.ResourceStatus(FirelensStatusNone))
	assert.Equal(t, resourcestatus.ResourceStatus(FirelensStatusNone), firelensResource.knownStatusUnsafe)
	assert.Equal(t, resourcestatus.ResourceStatus(FirelensCreated), firelensResource.appliedStatusUnsafe)
}

func TestCreateDirectories(t *testing.T) {
	// Store original functions to restore later
	originalMkdirAll := mkdirAll
	originalSetOwnership := setOwnership
	defer func() {
		// Restore original functions after test
		mkdirAll = originalMkdirAll
		setOwnership = originalSetOwnership
	}()

	tests := []struct {
		name             string
		user             string
		mkdirAllCalls    []string
		mockMkdirAll     func(string, os.FileMode) error
		mockSetOwnership func(string, string) error
		expectedError    bool
	}{
		{
			name: "success, no user defined",
			mockMkdirAll: func(path string, perm os.FileMode) error {
				return nil
			},
			mockSetOwnership: func(path, user string) error {
				return nil
			},
			expectedError: false,
		},
		{
			name: "success, valid root UID defined",
			user: "0",
			mockMkdirAll: func(path string, perm os.FileMode) error {
				return nil
			},
			mockSetOwnership: func(path, user string) error {
				return nil
			},
			expectedError: false,
		},
		{
			name: "success, valid non-root UID defined",
			user: "12345",
			mockMkdirAll: func(path string, perm os.FileMode) error {
				return nil
			},
			mockSetOwnership: func(path, user string) error {
				return nil
			},
			expectedError: false,
		},
		{
			name: "success, valid UID:group defined",
			user: "12345",
			mockMkdirAll: func(path string, perm os.FileMode) error {
				return nil
			},
			mockSetOwnership: func(path, user string) error {
				return nil
			},
			expectedError: false,
		},
		{
			name: "error, mkdirAll config directory",
			mockMkdirAll: func(path string, perm os.FileMode) error {
				return errors.New("mkdir error")
			},
			expectedError: true,
		},
		{
			name: "error, mkdirAll socket directory",
			mockMkdirAll: func(path string, perm os.FileMode) error {
				if filepath.Base(filepath.Dir(path)) == "config" {
					return nil
				}
				return errors.New("mkdir socket error")
			},
			expectedError: true,
		},
		{
			name: "error, setOwnership invalid user format",

			user: "foo:12345",
			mockMkdirAll: func(path string, perm os.FileMode) error {
				return nil
			},
			mockSetOwnership: func(path, user string) error {
				return errors.New("ownership error")
			},
			expectedError: true,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			// Track calls to mkdirAll
			var mkdirAllCallCounter int
			mkdirAll = func(path string, perm os.FileMode) error {
				mkdirAllCallCounter += 1
				return tc.mockMkdirAll(path, perm)
			}
			// Track calls to setOwnership
			var setOwnershipPath, setOwnershipUser string
			var setOwnershipCallCounter int
			setOwnership = func(path string, user string) error {
				setOwnershipPath = path
				setOwnershipUser = user
				if tc.mockSetOwnership != nil {
					setOwnershipCallCounter += 1
					return tc.mockSetOwnership(path, user)
				}
				return nil
			}
			firelens := &FirelensResource{
				user:        tc.user,
				resourceDir: testResourceDir,
			}

			err := firelens.createDirectories()
			// Verify error condition
			assert.Equal(t, tc.expectedError, err != nil)
			// Verify successful cases
			if err == nil {
				assert.Equal(t, 2, mkdirAllCallCounter)
				assert.Equal(t, 1, setOwnershipCallCounter)
				expectedSocketDir := filepath.Join(testResourceDir, "socket")
				assert.Equal(t, expectedSocketDir, setOwnershipPath)
				assert.Equal(t, tc.user, setOwnershipUser)
			}
		})
	}
}
