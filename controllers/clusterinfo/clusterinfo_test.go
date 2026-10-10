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
	"context"
	"net/http"
	"testing"

	configv1 "github.com/openshift/api/config/v1"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/rest"

	"github.com/NVIDIA/gpu-operator/internal/consts"
)

func requireClusterInfo(t *testing.T, clusterInfoAPI Interface) *clusterInfo {
	t.Helper()

	clusterInfoUnderTest, ok := clusterInfoAPI.(*clusterInfo)
	require.True(t, ok, "New returned %T, not *clusterInfo", clusterInfoAPI)
	return clusterInfoUnderTest
}

func TestNew(t *testing.T) {
	testCases := []struct {
		description                 string
		handlers                    map[string]http.HandlerFunc
		options                     []Option
		expectedErrorContains       string
		expectedErrorReason         metav1.StatusReason
		expectedErrorCode           int32
		expectedContainerRuntime    string
		expectedOpenshiftVersion    string
		expectedDriverToolkitImages map[string]string
		expectedDRAResourceGVR      schema.GroupVersionResource
		expectedDRASupported        bool
		expectedRequestPaths        []string
	}{
		{
			description: "non-oneshot construction queries nothing",
			handlers:    openshiftClusterHandlers,
			options:     []Option{WithOneShot(false)},
		},
		{
			description: "oneshot defaults to off",
			handlers:    openshiftClusterHandlers,
		},
		{
			description: "oneshot on a cluster that is not OpenShift",
			handlers: mergeHandlers(discoveryHandlers(), map[string]http.HandlerFunc{
				pathClusterVersion:           respondNotFound(),
				pathNodes:                    respondWithJSON(http.StatusOK, gpuNodeList("containerd://1.7.11")),
				pathDriverToolkitImageStream: respondNotFound(),
				pathProxy:                    respondWithJSON(http.StatusOK, clusterProxy(configv1.ProxySpec{})),
			}),
			options:                     []Option{WithOneShot(true)},
			expectedContainerRuntime:    consts.Containerd,
			expectedOpenshiftVersion:    "",
			expectedDriverToolkitImages: nil,
			expectedDRASupported:        false,
			expectedRequestPaths: append([]string{pathClusterVersion, pathNodes, pathDriverToolkitImageStream},
				discoveryPaths()...),
		},
		{
			description:              "oneshot on an OpenShift cluster never lists nodes",
			handlers:                 openshiftClusterHandlers,
			options:                  []Option{WithOneShot(true)},
			expectedContainerRuntime: consts.CRIO,
			expectedOpenshiftVersion: "4.14",
			expectedDriverToolkitImages: map[string]string{
				"410.84": "quay.io/openshift/driver-toolkit@sha256:aaa",
				"412.86": "quay.io/openshift/driver-toolkit@sha256:bbb",
			},
			expectedDRAResourceGVR: draGVR("v1"),
			expectedDRASupported:   true,
			expectedRequestPaths: append([]string{pathClusterVersion, pathDriverToolkitImageStream},
				discoveryPaths(openshiftClusterDRAGroups...)...),
		},
		{
			description: "oneshot fails when the container runtime cannot be determined",
			handlers: map[string]http.HandlerFunc{
				pathClusterVersion: respondNotFound(),
				pathNodes:          respondServerError(),
			},
			options:               []Option{WithOneShot(true)},
			expectedErrorContains: "failed to get container runtime",
			expectedErrorReason:   metav1.StatusReasonInternalError,
			expectedErrorCode:     http.StatusInternalServerError,
			expectedRequestPaths:  []string{pathClusterVersion, pathNodes},
		},
		{
			description: "oneshot tolerates a DriverToolkit lookup failure",
			handlers: mergeHandlers(openshiftClusterHandlers, map[string]http.HandlerFunc{
				pathDriverToolkitImageStream: respondServerError(),
			}),
			options:                     []Option{WithOneShot(true)},
			expectedContainerRuntime:    consts.CRIO,
			expectedOpenshiftVersion:    "4.14",
			expectedDriverToolkitImages: nil,
			expectedDRAResourceGVR:      draGVR("v1"),
			expectedDRASupported:        true,
			expectedRequestPaths: append([]string{pathClusterVersion, pathDriverToolkitImageStream},
				discoveryPaths(openshiftClusterDRAGroups...)...),
		},
		{
			description: "oneshot fails when DRA support cannot be determined",
			handlers: mergeHandlers(openshiftClusterHandlers, map[string]http.HandlerFunc{
				pathDriverToolkitImageStream: respondNotFound(),
				pathAPIGroups:                respondServerError(),
			}),
			options:               []Option{WithOneShot(true)},
			expectedErrorContains: "failed to determine DRA support",
			expectedErrorReason:   metav1.StatusReasonInternalError,
			expectedErrorCode:     http.StatusInternalServerError,
			expectedRequestPaths: []string{
				pathClusterVersion, pathDriverToolkitImageStream, pathAPIVersions, pathAPIGroups,
			},
		},
	}

	// Suspected bug #5 owns whether New reaches the Proxy endpoint. Cases that could reach it still
	// register a Proxy handler, because this exclusion does not reach the unhandled-path tripwire.
	pathsOwnedByCharacterizationTests := []string{pathProxy}

	for _, testCase := range testCases {
		t.Run(testCase.description, func(t *testing.T) {
			ctx := testContext(t)
			apiServer := newStubAPIServer(t, testCase.handlers)

			options := append([]Option{WithKubernetesConfig(apiServer.restConfig)}, testCase.options...)

			clusterInfoAPI, err := New(ctx, options...)

			if testCase.expectedErrorContains != "" {
				require.ErrorContains(t, err, testCase.expectedErrorContains)
				if testCase.expectedErrorReason != "" {
					requireAPIStatusError(t, err, testCase.expectedErrorReason, testCase.expectedErrorCode)
				}
				require.Nil(t, clusterInfoAPI)
				assertRequestedPathSetExcluding(t, apiServer, testCase.expectedRequestPaths, pathsOwnedByCharacterizationTests...)
				return
			}

			require.NoError(t, err)
			require.NotNil(t, clusterInfoAPI)
			clusterInfoUnderTest := requireClusterInfo(t, clusterInfoAPI)

			assert.Equal(t, testCase.expectedContainerRuntime, clusterInfoUnderTest.containerRuntime)
			assert.Equal(t, testCase.expectedOpenshiftVersion, clusterInfoUnderTest.openshiftVersion)
			assert.Equal(t, testCase.expectedDriverToolkitImages, clusterInfoUnderTest.openshiftDriverToolkitImages)
			assert.Equal(t, testCase.expectedDRAResourceGVR, clusterInfoUnderTest.draResourceGVR)
			assert.Equal(t, testCase.expectedDRASupported, clusterInfoUnderTest.draSupported)
			assertRequestedPathSetExcluding(t, apiServer, testCase.expectedRequestPaths, pathsOwnedByCharacterizationTests...)
		})
	}

	// Only the abort is contractual: the wording and which endpoints are reached on the way are
	// suspected bug #1, owned by clusterinfo_characterization_test.go.
	t.Run("oneshot fails when the OpenShift version cannot be determined", func(t *testing.T) {
		ctx := testContext(t)
		apiServer := newStubAPIServer(t, map[string]http.HandlerFunc{
			pathClusterVersion: respondWithJSON(http.StatusOK,
				clusterVersion(updateHistory(configv1.PartialUpdate, "4.15.0"))),
			pathNodes: respondWithJSON(http.StatusOK, gpuNodeList("containerd://1.7.11")),
		})

		clusterInfoAPI, err := New(ctx, WithKubernetesConfig(apiServer.restConfig), WithOneShot(true))

		require.Error(t, err)
		require.Nil(t, clusterInfoAPI)
		assertEveryRequestWasARead(t, apiServer)
	})

	t.Run("oneshot fails when the OpenShift version lookup is denied", func(t *testing.T) {
		ctx := testContext(t)
		apiServer := newStubAPIServer(t, map[string]http.HandlerFunc{
			pathClusterVersion: respondForbidden(),
			pathNodes:          respondWithJSON(http.StatusOK, gpuNodeList("containerd://1.7.11")),
		})

		clusterInfoAPI, err := New(ctx, WithKubernetesConfig(apiServer.restConfig), WithOneShot(true))

		require.Error(t, err)
		requireAPIStatusError(t, err, metav1.StatusReasonForbidden, http.StatusForbidden)
		require.Nil(t, clusterInfoAPI)
		assertEveryRequestWasARead(t, apiServer)
	})

	t.Run("options are applied in order and the last one wins", func(t *testing.T) {
		ctx := testContext(t)
		firstConfig := &rest.Config{Host: "https://first.example.com:6443"}
		secondConfig := &rest.Config{Host: "https://second.example.com:6443"}

		clusterInfoAPI, err := New(ctx,
			WithKubernetesConfig(firstConfig),
			WithKubernetesConfig(secondConfig),
			WithOneShot(true),
			WithOneShot(false),
		)

		require.NoError(t, err)
		clusterInfoUnderTest := requireClusterInfo(t, clusterInfoAPI)
		require.Equal(t, secondConfig, clusterInfoUnderTest.config)
		require.False(t, clusterInfoUnderTest.oneshot)
	})

	t.Run("New stores the context it was given", func(t *testing.T) {
		ctx := testContext(t)

		clusterInfoAPI, err := New(ctx, WithKubernetesConfig(&rest.Config{Host: "https://example.com:6443"}))

		require.NoError(t, err)
		clusterInfoUnderTest := requireClusterInfo(t, clusterInfoAPI)
		require.Equal(t, ctx, clusterInfoUnderTest.ctx)
	})
}

// Nothing in an accessor's return values reveals which context it passed down, so each subtest
// below spends the stored context and asserts the consequence, making a context.Background() visible.
func TestClusterInfoAccessorsUseTheStoredContext(t *testing.T) {
	// The stub registers no handler but the catch-all, whose tripwire fires during cleanup if a
	// request arrives: a cancelled context must stop each accessor before it reaches the wire.
	newClusterInfoWithCancelledContext := func(t *testing.T) *clusterInfo {
		t.Helper()

		ctx, cancel := context.WithCancel(testContext(t))
		cancel()
		apiServer := newStubAPIServer(t, map[string]http.HandlerFunc{})
		return &clusterInfo{ctx: ctx, config: apiServer.restConfig}
	}

	t.Run("GetContainerRuntime fails on a cancelled context", func(t *testing.T) {
		containerRuntime, err := newClusterInfoWithCancelledContext(t).GetContainerRuntime()

		require.ErrorIs(t, err, context.Canceled)
		require.Empty(t, containerRuntime)
	})

	t.Run("GetOpenshiftVersion fails on a cancelled context", func(t *testing.T) {
		openshiftVersion, err := newClusterInfoWithCancelledContext(t).GetOpenshiftVersion()

		require.ErrorIs(t, err, context.Canceled)
		require.Empty(t, openshiftVersion)
	})

	t.Run("GetOpenshiftProxySpec fails on a cancelled context", func(t *testing.T) {
		openshiftProxySpec, err := newClusterInfoWithCancelledContext(t).GetOpenshiftProxySpec()

		require.ErrorIs(t, err, context.Canceled)
		require.Nil(t, openshiftProxySpec)
	})

	t.Run("GetOpenshiftDriverToolkitImages yields no images on a cancelled context", func(t *testing.T) {
		require.Nil(t, newClusterInfoWithCancelledContext(t).GetOpenshiftDriverToolkitImages())
	})

	// Discovery builds its own requests with context.TODO, so cancelling the stored context cannot
	// stop this accessor; the logger it carries is the only trace that the stored context was used.
	t.Run("GetDRAResourceGVR logs through the stored context", func(t *testing.T) {
		ctx, recorder := testContextWithCapturedLogs(t)
		apiServer := newStubAPIServer(t, discoveryHandlers(draAPIGroup("v1")))
		clusterInfoUnderTest := &clusterInfo{ctx: ctx, config: apiServer.restConfig}

		draResourceGVR, draSupported, err := clusterInfoUnderTest.GetDRAResourceGVR()

		require.NoError(t, err)
		require.True(t, draSupported)
		require.Equal(t, draGVR("v1"), draResourceGVR)
		require.Equal(t,
			[]string{"Discovered DeviceClass resource"},
			recorder.capturedMessages(logRecordLevelInfo))
	})
}

func TestOptions(t *testing.T) {
	t.Run("WithKubernetesConfig stores the config it was given", func(t *testing.T) {
		config := &rest.Config{Host: "https://example.com:6443"}
		clusterInfoUnderTest := &clusterInfo{}

		WithKubernetesConfig(config)(clusterInfoUnderTest)

		require.Equal(t, config, clusterInfoUnderTest.config)
	})

	// A nil config is what makes New fall back to config.GetConfigOrDie, which calls os.Exit(1).
	// Every test in this package therefore passes WithKubernetesConfig explicitly.
	t.Run("WithKubernetesConfig accepts nil", func(t *testing.T) {
		clusterInfoUnderTest := &clusterInfo{config: &rest.Config{Host: "https://example.com:6443"}}

		WithKubernetesConfig(nil)(clusterInfoUnderTest)

		require.Nil(t, clusterInfoUnderTest.config)
	})

	t.Run("WithOneShot sets the flag", func(t *testing.T) {
		clusterInfoUnderTest := &clusterInfo{}

		WithOneShot(true)(clusterInfoUnderTest)

		require.True(t, clusterInfoUnderTest.oneshot)
	})

	t.Run("WithOneShot clears the flag", func(t *testing.T) {
		clusterInfoUnderTest := &clusterInfo{oneshot: true}

		WithOneShot(false)(clusterInfoUnderTest)

		require.False(t, clusterInfoUnderTest.oneshot)
	})
}
