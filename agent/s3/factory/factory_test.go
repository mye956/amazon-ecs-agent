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
package factory

import (
	"context"
	"testing"

	"github.com/aws/amazon-ecs-agent/agent/config"
	"github.com/aws/amazon-ecs-agent/ecs-agent/credentials"
	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestCreateAWSConfig(t *testing.T) {
	tcs := []struct {
		name                    string
		region                  string
		useFIPSEndpoint         bool
		expectedUseFIPSEndpoint int
	}{
		{
			name:                    "config without FIPS enabled",
			region:                  "us-west-2",
			useFIPSEndpoint:         false,
			expectedUseFIPSEndpoint: aws.FIPSEndpointStateDisabled,
		},
		{
			name:                    "config with FIPS enabled in a non-FIPS compliant region",
			region:                  "us-west-2",
			useFIPSEndpoint:         true,
			expectedUseFIPSEndpoint: aws.FIPSEndpointStateEnabled,
		},
		{
			name:                    "config with FIPS enabled",
			region:                  "us-gov-west-1",
			useFIPSEndpoint:         true,
			expectedUseFIPSEndpoint: aws.FIPSEndpointStateEnabled,
		},
	}
	creds := credentials.IAMRoleCredentials{
		AccessKeyID:     "dummyAccessKeyID",
		SecretAccessKey: "dummySecretAccessKey",
		SessionToken:    "dummySessionToken",
	}
	region := "us-west-2"

	for _, tc := range tcs {
		config.SetFIPSEnabled(tc.useFIPSEndpoint)
		cfg, err := createAWSConfig(region, creds, tc.useFIPSEndpoint)
		require.NoError(t, err)
		client := s3.NewFromConfig(cfg)
		assertEqual(t, tc.region, client.Options.Region, "Region should be set")
		credsValue, err := client.Options.Credentials.Retrieve(context.TODO())
		require.NoError(t, err)
		assert.Equal(t, "dummyAccessKeyID", credsValue.AccessKeyID, "AccessKeyID should be set")
		assert.Equal(t, "dummySecretAccessKey", credsValue.SecretAccessKey, "SecretAccessKey should be set")
		assert.Equal(t, "dummySessionToken", credsValue.SessionToken, "SessionToken should be set")
		assert.Equal(t, tc.expectedUseFIPSEndpoint, client.Options().EndpointOptions.GetUseFIPSEndpoint())
		if tc.useFIPSEndpoint {
			assert.False(t, client.Options.UsePathStyle)
		} else {
			assert.True(t, client.Options.UsePathStyle)
		}

	}

	// Test without FIPS enabled
	config.SetFIPSEnabled(false)
	cfg, err := createAWSConfig(region, creds, false)
	require.NoError(t, err)
	client := s3.NewFromConfig(cfg)
	assertEqual(t)
	// assert.Equal(t, roundtripTimeout, cfg.HTTPClient.Timeout, "HTTPClient timeout should be set")
	assert.Equal(t, region, cfg.Region, "Region should be set")
	credsValue, err := cfg.Credentials.Retrieve(context.TODO())
	assert.NoError(t, err)
	assert.Equal(t, "dummyAccessKeyID", credsValue.AccessKeyID, "AccessKeyID should be set")
	assert.Equal(t, "dummySecretAccessKey", credsValue.SecretAccessKey, "SecretAccessKey should be set")
	assert.Equal(t, "dummySessionToken", credsValue.SessionToken, "SessionToken should be set")
	assert.Equal(t, aws.FIPSEndpointStateUnset, cfg.UseFIPSEndpoint, "UseFIPSEndpoint should not be set")
	assert.Nil(t, cfg.S3ForcePathStyle, "S3ForcePathStyle should not be set")
	// // Test with FIPS enabled in a non-FIPS compliant region
	// config.SetFIPSEnabled(true)
	// cfg, err = createAWSConfig(region, creds, false)
	// require.NoError(t, err)
	// assert.Equal(t, roundtripTimeout, cfg.HTTPClient.Timeout, "HTTPClient timeout should be set")
	// assert.Equal(t, region, aws.ToString(cfg.Region), "Region should be set")
	// credsValue, err = cfg.Credentials.Get()
	// assert.NoError(t, err)
	// assert.Equal(t, "dummyAccessKeyID", credsValue.AccessKeyID, "AccessKeyID should be set")
	// assert.Equal(t, "dummySecretAccessKey", credsValue.SecretAccessKey, "SecretAccessKey should be set")
	// assert.Equal(t, "dummySessionToken", credsValue.SessionToken, "SessionToken should be set")
	// assert.Equal(t, aws.FIPSEndpointStateUnset, cfg.UseFIPSEndpoint, "UseFIPSEndpoint should not be set")
	// assert.Nil(t, cfg.S3ForcePathStyle, "S3ForcePathStyle should not be set")
	// // Test with FIPS enabled in a FIPS compliant region
	// fipsRegion := "us-gov-west-1"
	// cfg, err = createAWSConfig(fipsRegion, creds, true)
	// require.NoError(t, err)
	// assert.Equal(t, roundtripTimeout, cfg.HTTPClient.Timeout, "HTTPClient timeout should be set")
	// assert.Equal(t, fipsRegion, aws.ToString(cfg.Region), "Region should be set")
	// credsValue, err = cfg.Credentials.Get()
	// assert.NoError(t, err)
	// assert.Equal(t, "dummyAccessKeyID", credsValue.AccessKeyID, "AccessKeyID should be set")
	// assert.Equal(t, "dummySecretAccessKey", credsValue.SecretAccessKey, "SecretAccessKey should be set")
	// assert.Equal(t, "dummySessionToken", credsValue.SessionToken, "SessionToken should be set")
	// assert.Equal(t, aws.FIPSEndpointStateEnabled, cfg.UseFIPSEndpoint, "UseFIPSEndpoint should be set to FIPSEndpointStateEnabled")
	// assert.False(t, aws.ToBool(cfg.S3ForcePathStyle), "S3ForcePathStyle should be set to false")
}
