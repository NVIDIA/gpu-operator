/**
# Copyright (c) NVIDIA CORPORATION.  All rights reserved.
#
# Licensed under the Apache License, Version 2.0 (the "License");
# you may not use this file except in compliance with the License.
# You may obtain a copy of the License at
#
#     http://www.apache.org/licenses/LICENSE-2.0
#
# Unless required by applicable law or agreed to in writing, software
# distributed under the License is distributed on an "AS IS" BASIS,
# WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
# See the License for the specific language governing permissions and
# limitations under the License.
**/

package clusterinfo

import (
	"net/http"
	"testing"

	configv1 "github.com/openshift/api/config/v1"
	"github.com/stretchr/testify/require"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func TestGetOpenshiftProxySpec(t *testing.T) {
	populatedOpenshiftProxySpec := configv1.ProxySpec{
		HTTPProxy:  "http://proxy.example.com:3128",
		HTTPSProxy: "https://proxy.example.com:3129",
		NoProxy:    ".cluster.local,10.0.0.0/8",
		TrustedCA:  configv1.ConfigMapNameReference{Name: "user-ca-bundle"},
	}

	testCases := []struct {
		description                string
		handlers                   map[string]http.HandlerFunc
		expectedOpenshiftProxySpec *configv1.ProxySpec
		expectedErrorReason        metav1.StatusReason
		expectedErrorCode          int32
		expectedErrorIsNotFound    bool
	}{
		{
			description: "fully populated proxy",
			handlers: map[string]http.HandlerFunc{
				pathProxy: respondWithJSON(http.StatusOK, clusterProxy(populatedOpenshiftProxySpec)),
			},
			expectedOpenshiftProxySpec: &populatedOpenshiftProxySpec,
		},
		{
			// A zero spec behind a non-nil pointer does not trip the nil guard in internal/state/driver.go.
			description: "a cluster with no proxy configured yields a zero spec, not nil",
			handlers: map[string]http.HandlerFunc{
				pathProxy: respondWithJSON(http.StatusOK, clusterProxy(configv1.ProxySpec{})),
			},
			expectedOpenshiftProxySpec: &configv1.ProxySpec{},
		},
		{
			// Unlike getOpenshiftVersion, this function does not convert NotFound into a sentinel.
			description: "a missing Proxy resource is an error",
			handlers: map[string]http.HandlerFunc{
				pathProxy: respondNotFound(),
			},
			expectedErrorReason:     metav1.StatusReasonNotFound,
			expectedErrorCode:       http.StatusNotFound,
			expectedErrorIsNotFound: true,
		},
		{
			description: "server error",
			handlers: map[string]http.HandlerFunc{
				pathProxy: respondServerError(),
			},
			expectedErrorReason: metav1.StatusReasonInternalError,
			expectedErrorCode:   http.StatusInternalServerError,
		},
		{
			description: "forbidden",
			handlers: map[string]http.HandlerFunc{
				pathProxy: respondForbidden(),
			},
			expectedErrorReason: metav1.StatusReasonForbidden,
			expectedErrorCode:   http.StatusForbidden,
		},
	}

	for _, testCase := range testCases {
		t.Run(testCase.description, func(t *testing.T) {
			ctx := testContext(t)
			apiServer := newStubAPIServer(t, testCase.handlers)

			openshiftProxySpec, err := getOpenshiftProxySpec(ctx, apiServer.restConfig)

			if testCase.expectedErrorReason != "" {
				requireAPIStatusError(t, err, testCase.expectedErrorReason, testCase.expectedErrorCode)
				require.Equal(t, testCase.expectedErrorIsNotFound, apierrors.IsNotFound(err))
				require.Nil(t, openshiftProxySpec)
			} else {
				require.NoError(t, err)
				require.NotNil(t, openshiftProxySpec)
				require.Equal(t, *testCase.expectedOpenshiftProxySpec, *openshiftProxySpec)
			}

			assertRequestedPathSequence(t, apiServer, []string{pathProxy})
		})
	}
}

func TestClusterInfoGetOpenshiftProxySpec(t *testing.T) {
	cachedOpenshiftProxySpec := &configv1.ProxySpec{HTTPProxy: "http://proxy.example.com:3128"}
	fetchedOpenshiftProxySpec := configv1.ProxySpec{
		HTTPProxy:  "http://proxy.example.com:3128",
		HTTPSProxy: "https://proxy.example.com:3129",
		NoProxy:    ".cluster.local",
		TrustedCA:  configv1.ConfigMapNameReference{Name: "user-ca-bundle"},
	}

	testCases := []struct {
		description                string
		oneshot                    bool
		cachedOpenshiftProxySpec   *configv1.ProxySpec
		handlers                   map[string]http.HandlerFunc
		expectedOpenshiftProxySpec *configv1.ProxySpec
		expectedErrorReason        metav1.StatusReason
		expectedErrorCode          int32
		expectedErrorIsNotFound    bool
		expectedRequestPaths       []string
	}{
		{
			description:                "oneshot returns the cached spec without any API call",
			oneshot:                    true,
			cachedOpenshiftProxySpec:   cachedOpenshiftProxySpec,
			expectedOpenshiftProxySpec: cachedOpenshiftProxySpec,
		},
		{
			description:                "oneshot returns a nil cached spec without an error",
			oneshot:                    true,
			cachedOpenshiftProxySpec:   nil,
			expectedOpenshiftProxySpec: nil,
		},
		{
			description: "non-oneshot queries the cluster",
			handlers: map[string]http.HandlerFunc{
				pathProxy: respondWithJSON(http.StatusOK, clusterProxy(fetchedOpenshiftProxySpec)),
			},
			expectedOpenshiftProxySpec: &fetchedOpenshiftProxySpec,
			expectedRequestPaths:       []string{pathProxy},
		},
		{
			description: "non-oneshot propagates a missing Proxy resource as an error",
			handlers: map[string]http.HandlerFunc{
				pathProxy: respondNotFound(),
			},
			expectedErrorReason:     metav1.StatusReasonNotFound,
			expectedErrorCode:       http.StatusNotFound,
			expectedErrorIsNotFound: true,
			expectedRequestPaths:    []string{pathProxy},
		},
	}

	for _, testCase := range testCases {
		t.Run(testCase.description, func(t *testing.T) {
			clusterInfoUnderTest := &clusterInfo{
				ctx:       testContext(t),
				oneshot:   testCase.oneshot,
				proxySpec: testCase.cachedOpenshiftProxySpec,
			}
			var apiServer *stubAPIServer
			if testCase.handlers != nil {
				apiServer = newStubAPIServer(t, testCase.handlers)
				clusterInfoUnderTest.config = apiServer.restConfig
			}

			openshiftProxySpec, err := clusterInfoUnderTest.GetOpenshiftProxySpec()

			switch {
			case testCase.expectedErrorReason != "":
				requireAPIStatusError(t, err, testCase.expectedErrorReason, testCase.expectedErrorCode)
				require.Equal(t, testCase.expectedErrorIsNotFound, apierrors.IsNotFound(err))
				require.Nil(t, openshiftProxySpec)
			case testCase.oneshot:
				require.NoError(t, err)
				require.Equal(t, testCase.expectedOpenshiftProxySpec, openshiftProxySpec)
			default:
				require.NoError(t, err)
				require.NotNil(t, openshiftProxySpec)
				require.Equal(t, *testCase.expectedOpenshiftProxySpec, *openshiftProxySpec)
			}

			if apiServer != nil {
				assertRequestedPathSequence(t, apiServer, testCase.expectedRequestPaths)
			}
		})
	}
}
