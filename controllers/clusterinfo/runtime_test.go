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
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	corev1client "k8s.io/client-go/kubernetes/typed/core/v1"

	"github.com/NVIDIA/gpu-operator/internal/consts"
)

func TestGetRuntimeString(t *testing.T) {
	testCases := []struct {
		description              string
		containerRuntimeVersion  string
		expectedContainerRuntime string
		expectedErrorContains    string
	}{
		{
			description:              "docker runtime",
			containerRuntimeVersion:  "docker://20.10.23",
			expectedContainerRuntime: consts.Docker,
		},
		{
			description:              "containerd runtime",
			containerRuntimeVersion:  "containerd://1.7.11",
			expectedContainerRuntime: consts.Containerd,
		},
		{
			description:              "cri-o runtime maps to the crio constant",
			containerRuntimeVersion:  "cri-o://1.28.2",
			expectedContainerRuntime: consts.CRIO,
		},
		{
			description:             "unrecognised runtime",
			containerRuntimeVersion: "rkt://1.0.0",
			expectedErrorContains:   "runtime not recognized: rkt://1.0.0",
		},
		{
			description:             "node has not reported a runtime yet",
			containerRuntimeVersion: "",
			expectedErrorContains:   "runtime not recognized: ",
		},
		{
			description:              "bare prefix without a version suffix",
			containerRuntimeVersion:  "containerd",
			expectedContainerRuntime: consts.Containerd,
		},
		{
			description:             "runtime name appearing as a substring but not a prefix",
			containerRuntimeVersion: "my-docker://1.0",
			expectedErrorContains:   "runtime not recognized: my-docker://1.0",
		},
		{
			description:             "matching is case sensitive",
			containerRuntimeVersion: "Docker://20.10",
			expectedErrorContains:   "runtime not recognized: Docker://20.10",
		},
	}

	for _, testCase := range testCases {
		t.Run(testCase.description, func(t *testing.T) {
			node := corev1.Node{
				Status: corev1.NodeStatus{
					NodeInfo: corev1.NodeSystemInfo{ContainerRuntimeVersion: testCase.containerRuntimeVersion},
				},
			}

			containerRuntime, err := getRuntimeString(node)

			if testCase.expectedErrorContains != "" {
				require.ErrorContains(t, err, testCase.expectedErrorContains)
				require.Empty(t, containerRuntime)
				return
			}
			require.NoError(t, err)
			require.Equal(t, testCase.expectedContainerRuntime, containerRuntime)
		})
	}
}

func TestGetContainerRuntime(t *testing.T) {
	testCases := []struct {
		description              string
		handlers                 map[string]http.HandlerFunc
		expectedContainerRuntime string
		expectedErrorContains    string
		expectedErrorReason      metav1.StatusReason
		expectedErrorCode        int32
		expectedRequestPaths     []string
	}{
		{
			description: "OpenShift short-circuits to crio without listing nodes",
			handlers: map[string]http.HandlerFunc{
				pathClusterVersion: respondWithJSON(http.StatusOK,
					clusterVersion(updateHistory(configv1.CompletedUpdate, "4.14.10"))),
				pathNodes: respondWithJSON(http.StatusOK, gpuNodeList("docker://20.10.23")),
			},
			expectedContainerRuntime: consts.CRIO,
			expectedRequestPaths:     []string{pathClusterVersion},
		},
		{
			description: "single containerd node",
			handlers: map[string]http.HandlerFunc{
				pathClusterVersion: respondNotFound(),
				pathNodes:          respondWithJSON(http.StatusOK, gpuNodeList("containerd://1.7.11")),
			},
			expectedContainerRuntime: consts.Containerd,
			expectedRequestPaths:     []string{pathClusterVersion, pathNodes},
		},
		{
			description: "single docker node",
			handlers: map[string]http.HandlerFunc{
				pathClusterVersion: respondNotFound(),
				pathNodes:          respondWithJSON(http.StatusOK, gpuNodeList("docker://20.10.23")),
			},
			expectedContainerRuntime: consts.Docker,
			expectedRequestPaths:     []string{pathClusterVersion, pathNodes},
		},
		{
			description: "single cri-o node on a cluster that is not OpenShift",
			handlers: map[string]http.HandlerFunc{
				pathClusterVersion: respondNotFound(),
				pathNodes:          respondWithJSON(http.StatusOK, gpuNodeList("cri-o://1.28.2")),
			},
			expectedContainerRuntime: consts.CRIO,
			expectedRequestPaths:     []string{pathClusterVersion, pathNodes},
		},
		{
			description: "containerd wins when it appears last",
			handlers: map[string]http.HandlerFunc{
				pathClusterVersion: respondNotFound(),
				pathNodes: respondWithJSON(http.StatusOK,
					gpuNodeList("docker://20.10.23", "containerd://1.7.11")),
			},
			expectedContainerRuntime: consts.Containerd,
			expectedRequestPaths:     []string{pathClusterVersion, pathNodes},
		},
		{
			description: "containerd wins when it appears first",
			handlers: map[string]http.HandlerFunc{
				pathClusterVersion: respondNotFound(),
				pathNodes: respondWithJSON(http.StatusOK,
					gpuNodeList("containerd://1.7.11", "docker://20.10.23")),
			},
			expectedContainerRuntime: consts.Containerd,
			expectedRequestPaths:     []string{pathClusterVersion, pathNodes},
		},
		{
			description: "no GPU nodes defaults to containerd",
			handlers: map[string]http.HandlerFunc{
				pathClusterVersion: respondNotFound(),
				pathNodes:          respondWithJSON(http.StatusOK, gpuNodeList()),
			},
			expectedContainerRuntime: consts.Containerd,
			expectedRequestPaths:     []string{pathClusterVersion, pathNodes},
		},
		{
			description: "every node reports an unrecognised runtime, defaulting to containerd",
			handlers: map[string]http.HandlerFunc{
				pathClusterVersion: respondNotFound(),
				pathNodes: respondWithJSON(http.StatusOK,
					gpuNodeList("rkt://1.0", "kata://3.2.0")),
			},
			expectedContainerRuntime: consts.Containerd,
			expectedRequestPaths:     []string{pathClusterVersion, pathNodes},
		},
		{
			description: "an unrecognised node does not abort the scan",
			handlers: map[string]http.HandlerFunc{
				pathClusterVersion: respondNotFound(),
				pathNodes: respondWithJSON(http.StatusOK,
					gpuNodeList("rkt://1.0", "docker://20.10.23")),
			},
			expectedContainerRuntime: consts.Docker,
			expectedRequestPaths:     []string{pathClusterVersion, pathNodes},
		},
		{
			// Only this ordering catches a missing skip; in the case above the later node overwrites it anyway.
			description: "an unrecognised node after a recognised one does not overwrite the result",
			handlers: map[string]http.HandlerFunc{
				pathClusterVersion: respondNotFound(),
				pathNodes: respondWithJSON(http.StatusOK,
					gpuNodeList("docker://20.10.23", "rkt://1.0")),
			},
			expectedContainerRuntime: consts.Docker,
			expectedRequestPaths:     []string{pathClusterVersion, pathNodes},
		},
		{
			description: "node listing failure is fatal",
			handlers: map[string]http.HandlerFunc{
				pathClusterVersion: respondNotFound(),
				pathNodes:          respondServerError(),
			},
			expectedErrorContains: "unable to list nodes prior to checking container runtime",
			expectedErrorReason:   metav1.StatusReasonInternalError,
			expectedErrorCode:     http.StatusInternalServerError,
			expectedRequestPaths:  []string{pathClusterVersion, pathNodes},
		},
		{
			description: "node listing denied by RBAC is fatal",
			handlers: map[string]http.HandlerFunc{
				pathClusterVersion: respondNotFound(),
				pathNodes:          respondForbidden(),
			},
			expectedErrorContains: "unable to list nodes prior to checking container runtime",
			expectedErrorReason:   metav1.StatusReasonForbidden,
			expectedErrorCode:     http.StatusForbidden,
			expectedRequestPaths:  []string{pathClusterVersion, pathNodes},
		},
	}

	for _, testCase := range testCases {
		t.Run(testCase.description, func(t *testing.T) {
			ctx := testContext(t)
			apiServer := newStubAPIServer(t, testCase.handlers)

			containerRuntime, err := getContainerRuntime(ctx, apiServer.restConfig)

			if testCase.expectedErrorContains != "" {
				require.ErrorContains(t, err, testCase.expectedErrorContains)
				requireAPIStatusError(t, err, testCase.expectedErrorReason, testCase.expectedErrorCode)
				require.Empty(t, containerRuntime)
			} else {
				require.NoError(t, err)
				require.Equal(t, testCase.expectedContainerRuntime, containerRuntime)
			}

			assertRequestedPathSequence(t, apiServer, testCase.expectedRequestPaths)
			for _, request := range apiServer.requestsTo(pathNodes) {
				assert.Equal(t, consts.GPUPresentLabel+"=true", request.query.Get("labelSelector"))
			}
		})
	}

	// client-go's TLS failure is a bare fmt.Errorf with no sentinel or type to match, so the cause is
	// pinned by comparing against the error the same constructor produces for the same config.
	t.Run("client construction failure returns the client's own error", func(t *testing.T) {
		_, expectedClientError := corev1client.NewForConfig(brokenRESTConfig())
		require.Error(t, expectedClientError)

		containerRuntime, err := getContainerRuntime(testContext(t), brokenRESTConfig())

		require.EqualError(t, err, expectedClientError.Error())
		require.Empty(t, containerRuntime)
	})
}

func TestClusterInfoGetContainerRuntime(t *testing.T) {
	testCases := []struct {
		description              string
		oneshot                  bool
		cachedContainerRuntime   string
		handlers                 map[string]http.HandlerFunc
		expectedContainerRuntime string
		expectedErrorContains    string
		expectedRequestPaths     []string
	}{
		{
			// A nil rest.Config is the assertion: any API call would fail loudly.
			description:              "oneshot returns the cached runtime without any API call",
			oneshot:                  true,
			cachedContainerRuntime:   consts.Containerd,
			expectedContainerRuntime: consts.Containerd,
		},
		{
			description:              "oneshot returns an empty cached runtime rather than re-fetching",
			oneshot:                  true,
			cachedContainerRuntime:   "",
			expectedContainerRuntime: "",
		},
		{
			description: "non-oneshot queries the cluster",
			handlers: map[string]http.HandlerFunc{
				pathClusterVersion: respondNotFound(),
				pathNodes:          respondWithJSON(http.StatusOK, gpuNodeList("containerd://1.7.11")),
			},
			expectedContainerRuntime: consts.Containerd,
			expectedRequestPaths:     []string{pathClusterVersion, pathNodes},
		},
		{
			description: "non-oneshot propagates errors",
			handlers: map[string]http.HandlerFunc{
				pathClusterVersion: respondNotFound(),
				pathNodes:          respondServerError(),
			},
			expectedErrorContains: "unable to list nodes prior to checking container runtime",
			expectedRequestPaths:  []string{pathClusterVersion, pathNodes},
		},
		{
			description:            "non-oneshot ignores the cached runtime",
			cachedContainerRuntime: consts.Docker,
			handlers: map[string]http.HandlerFunc{
				pathClusterVersion: respondNotFound(),
				pathNodes:          respondWithJSON(http.StatusOK, gpuNodeList("containerd://1.7.11")),
			},
			expectedContainerRuntime: consts.Containerd,
			expectedRequestPaths:     []string{pathClusterVersion, pathNodes},
		},
	}

	for _, testCase := range testCases {
		t.Run(testCase.description, func(t *testing.T) {
			clusterInfoUnderTest := &clusterInfo{
				ctx:              testContext(t),
				oneshot:          testCase.oneshot,
				containerRuntime: testCase.cachedContainerRuntime,
			}
			var apiServer *stubAPIServer
			if testCase.handlers != nil {
				apiServer = newStubAPIServer(t, testCase.handlers)
				clusterInfoUnderTest.config = apiServer.restConfig
			}

			containerRuntime, err := clusterInfoUnderTest.GetContainerRuntime()

			if testCase.expectedErrorContains != "" {
				require.ErrorContains(t, err, testCase.expectedErrorContains)
				require.Empty(t, containerRuntime)
			} else {
				require.NoError(t, err)
				require.Equal(t, testCase.expectedContainerRuntime, containerRuntime)
			}

			if apiServer != nil {
				assertRequestedPathSequence(t, apiServer, testCase.expectedRequestPaths)
			}
		})
	}
}
