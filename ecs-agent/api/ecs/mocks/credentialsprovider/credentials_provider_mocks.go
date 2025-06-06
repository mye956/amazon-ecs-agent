// Copyright Amazon.com Inc. or its affiliates. All Rights Reserved.
//
// Licensed under the Apache License, Version 2.0 (the "License"). You may
// not use this file except in compliance with the License. A copy of the
// License is located at
//
//     http://aws.amazon.com/apache2.0/
//
// or in the "license" file accompanying this file. This file is distributed
// on an "AS IS" BASIS, WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either
// express or implied. See the License for the specific language governing
// permissions and limitations under the License.
//

// Code generated by MockGen. DO NOT EDIT.
// Source: github.com/aws/aws-sdk-go-v2/aws (interfaces: CredentialsProvider)

// Package mock_credentialsprovider is a generated GoMock package.
package mock_credentialsprovider

import (
	context "context"
	reflect "reflect"

	aws "github.com/aws/aws-sdk-go-v2/aws"
	gomock "github.com/golang/mock/gomock"
)

// MockCredentialsProvider is a mock of CredentialsProvider interface.
type MockCredentialsProvider struct {
	ctrl     *gomock.Controller
	recorder *MockCredentialsProviderMockRecorder
}

// MockCredentialsProviderMockRecorder is the mock recorder for MockCredentialsProvider.
type MockCredentialsProviderMockRecorder struct {
	mock *MockCredentialsProvider
}

// NewMockCredentialsProvider creates a new mock instance.
func NewMockCredentialsProvider(ctrl *gomock.Controller) *MockCredentialsProvider {
	mock := &MockCredentialsProvider{ctrl: ctrl}
	mock.recorder = &MockCredentialsProviderMockRecorder{mock}
	return mock
}

// EXPECT returns an object that allows the caller to indicate expected use.
func (m *MockCredentialsProvider) EXPECT() *MockCredentialsProviderMockRecorder {
	return m.recorder
}

// Retrieve mocks base method.
func (m *MockCredentialsProvider) Retrieve(arg0 context.Context) (aws.Credentials, error) {
	m.ctrl.T.Helper()
	ret := m.ctrl.Call(m, "Retrieve", arg0)
	ret0, _ := ret[0].(aws.Credentials)
	ret1, _ := ret[1].(error)
	return ret0, ret1
}

// Retrieve indicates an expected call of Retrieve.
func (mr *MockCredentialsProviderMockRecorder) Retrieve(arg0 interface{}) *gomock.Call {
	mr.mock.ctrl.T.Helper()
	return mr.mock.ctrl.RecordCallWithMethodType(mr.mock, "Retrieve", reflect.TypeOf((*MockCredentialsProvider)(nil).Retrieve), arg0)
}
