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
	ocpconfigv1 "github.com/openshift/client-go/config/clientset/versioned/typed/config/v1"
	"github.com/stretchr/testify/require"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func TestGetOpenshiftVersion(t *testing.T) {
	testCases := []struct {
		description              string
		handlers                 map[string]http.HandlerFunc
		expectedOpenshiftVersion string
		expectsError             bool
		expectedErrorContains    string
		expectedErrorReason      metav1.StatusReason
		expectedErrorCode        int32
	}{
		{
			description: "completed history entry yields major.minor",
			handlers: map[string]http.HandlerFunc{
				pathClusterVersion: respondWithJSON(http.StatusOK,
					clusterVersion(updateHistory(configv1.CompletedUpdate, "4.14.10"))),
			},
			expectedOpenshiftVersion: "4.14",
		},
		{
			description: "two-component version is returned unchanged",
			handlers: map[string]http.HandlerFunc{
				pathClusterVersion: respondWithJSON(http.StatusOK,
					clusterVersion(updateHistory(configv1.CompletedUpdate, "4.14"))),
			},
			expectedOpenshiftVersion: "4.14",
		},
		{
			description: "single-component version yields the major only",
			handlers: map[string]http.HandlerFunc{
				pathClusterVersion: respondWithJSON(http.StatusOK,
					clusterVersion(updateHistory(configv1.CompletedUpdate, "4"))),
			},
			expectedOpenshiftVersion: "4",
		},
		{
			description: "pre-release version keeps only the first two segments",
			handlers: map[string]http.HandlerFunc{
				pathClusterVersion: respondWithJSON(http.StatusOK,
					clusterVersion(updateHistory(configv1.CompletedUpdate, "4.14.10-rc.1"))),
			},
			expectedOpenshiftVersion: "4.14",
		},
		{
			description: "partial entries are skipped in favour of the completed one",
			handlers: map[string]http.HandlerFunc{
				pathClusterVersion: respondWithJSON(http.StatusOK, clusterVersion(
					updateHistory(configv1.PartialUpdate, "4.15.0"),
					updateHistory(configv1.CompletedUpdate, "4.14.10"),
				)),
			},
			expectedOpenshiftVersion: "4.14",
		},
		{
			description: "an entry with an unset state is skipped in favour of the completed one",
			handlers: map[string]http.HandlerFunc{
				pathClusterVersion: respondWithJSON(http.StatusOK, clusterVersion(
					updateHistory("", "4.15.0"),
					updateHistory(configv1.CompletedUpdate, "4.14.10"),
				)),
			},
			expectedOpenshiftVersion: "4.14",
		},
		{
			description: "the first completed entry wins, not the newest",
			handlers: map[string]http.HandlerFunc{
				pathClusterVersion: respondWithJSON(http.StatusOK, clusterVersion(
					updateHistory(configv1.CompletedUpdate, "4.14.10"),
					updateHistory(configv1.CompletedUpdate, "4.12.1"),
				)),
			},
			expectedOpenshiftVersion: "4.14",
		},
		{
			description: "no completed entry in history",
			handlers: map[string]http.HandlerFunc{
				pathClusterVersion: respondWithJSON(http.StatusOK,
					clusterVersion(updateHistory(configv1.PartialUpdate, "4.15.0"))),
			},
			expectsError:          true,
			expectedErrorContains: "failed to find Completed Cluster Version",
		},
		{
			description: "empty history",
			handlers: map[string]http.HandlerFunc{
				pathClusterVersion: respondWithJSON(http.StatusOK, clusterVersion()),
			},
			expectsError:          true,
			expectedErrorContains: "failed to find Completed Cluster Version",
		},
		{
			description: "no ClusterVersion resource means the cluster is not OpenShift",
			handlers: map[string]http.HandlerFunc{
				pathClusterVersion: respondNotFound(),
			},
			expectedOpenshiftVersion: "",
		},
		{
			description: "server error is reported, not treated as not-OpenShift",
			handlers: map[string]http.HandlerFunc{
				pathClusterVersion: respondServerError(),
			},
			expectsError:        true,
			expectedErrorReason: metav1.StatusReasonInternalError,
			expectedErrorCode:   http.StatusInternalServerError,
		},
		{
			description: "forbidden is reported, not treated as not-OpenShift",
			handlers: map[string]http.HandlerFunc{
				pathClusterVersion: respondForbidden(),
			},
			expectsError:        true,
			expectedErrorReason: metav1.StatusReasonForbidden,
			expectedErrorCode:   http.StatusForbidden,
		},
		{
			description: "malformed response body",
			handlers: map[string]http.HandlerFunc{
				pathClusterVersion: respondWithRawBody(http.StatusOK, "{"),
			},
			expectsError: true,
		},
	}

	for _, testCase := range testCases {
		t.Run(testCase.description, func(t *testing.T) {
			ctx := testContext(t)
			apiServer := newStubAPIServer(t, testCase.handlers)

			openshiftVersion, err := getOpenshiftVersion(ctx, apiServer.restConfig)

			if testCase.expectsError {
				require.Error(t, err)
				if testCase.expectedErrorContains != "" {
					require.ErrorContains(t, err, testCase.expectedErrorContains)
				}
				if testCase.expectedErrorReason != "" {
					requireAPIStatusError(t, err, testCase.expectedErrorReason, testCase.expectedErrorCode)
				}
				// NotFound is the one status this function must never surface: it is converted to
				// the not-OpenShift sentinel, so seeing it here would mean the conversion was lost.
				require.False(t, apierrors.IsNotFound(err))
				require.Empty(t, openshiftVersion)
			} else {
				require.NoError(t, err)
				require.Equal(t, testCase.expectedOpenshiftVersion, openshiftVersion)
			}

			assertRequestedPathSequence(t, apiServer, []string{pathClusterVersion})
		})
	}

	// client-go's TLS failure is a bare fmt.Errorf with no sentinel or type to match, so the cause is
	// pinned by comparing against the error the same constructor produces for the same config.
	t.Run("client construction failure returns the client's own error", func(t *testing.T) {
		_, expectedClientError := ocpconfigv1.NewForConfig(brokenRESTConfig())
		require.Error(t, expectedClientError)

		openshiftVersion, err := getOpenshiftVersion(testContext(t), brokenRESTConfig())

		require.EqualError(t, err, expectedClientError.Error())
		require.False(t, apierrors.IsNotFound(err))
		require.Empty(t, openshiftVersion)
	})
}

func TestClusterInfoGetOpenshiftVersion(t *testing.T) {
	testCases := []struct {
		description              string
		oneshot                  bool
		cachedOpenshiftVersion   string
		handlers                 map[string]http.HandlerFunc
		expectedOpenshiftVersion string
		expectedErrorReason      metav1.StatusReason
		expectedErrorCode        int32
		expectedRequestPaths     []string
	}{
		{
			description:              "oneshot returns the cached version without any API call",
			oneshot:                  true,
			cachedOpenshiftVersion:   "4.14",
			expectedOpenshiftVersion: "4.14",
		},
		{
			description:              "oneshot returns an empty cached version",
			oneshot:                  true,
			cachedOpenshiftVersion:   "",
			expectedOpenshiftVersion: "",
		},
		{
			description: "non-oneshot queries the cluster",
			handlers: map[string]http.HandlerFunc{
				pathClusterVersion: respondWithJSON(http.StatusOK,
					clusterVersion(updateHistory(configv1.CompletedUpdate, "4.14.10"))),
			},
			expectedOpenshiftVersion: "4.14",
			expectedRequestPaths:     []string{pathClusterVersion},
		},
		{
			description: "non-oneshot reports a cluster that is not OpenShift as an empty version",
			handlers: map[string]http.HandlerFunc{
				pathClusterVersion: respondNotFound(),
			},
			expectedOpenshiftVersion: "",
			expectedRequestPaths:     []string{pathClusterVersion},
		},
		{
			description: "non-oneshot propagates errors",
			handlers: map[string]http.HandlerFunc{
				pathClusterVersion: respondServerError(),
			},
			expectedErrorReason:  metav1.StatusReasonInternalError,
			expectedErrorCode:    http.StatusInternalServerError,
			expectedRequestPaths: []string{pathClusterVersion},
		},
	}

	for _, testCase := range testCases {
		t.Run(testCase.description, func(t *testing.T) {
			clusterInfoUnderTest := &clusterInfo{
				ctx:              testContext(t),
				oneshot:          testCase.oneshot,
				openshiftVersion: testCase.cachedOpenshiftVersion,
			}
			var apiServer *stubAPIServer
			if testCase.handlers != nil {
				apiServer = newStubAPIServer(t, testCase.handlers)
				clusterInfoUnderTest.config = apiServer.restConfig
			}

			openshiftVersion, err := clusterInfoUnderTest.GetOpenshiftVersion()

			if testCase.expectedErrorReason != "" {
				requireAPIStatusError(t, err, testCase.expectedErrorReason, testCase.expectedErrorCode)
				require.Empty(t, openshiftVersion)
			} else {
				require.NoError(t, err)
				require.Equal(t, testCase.expectedOpenshiftVersion, openshiftVersion)
			}

			if apiServer != nil {
				assertRequestedPathSequence(t, apiServer, testCase.expectedRequestPaths)
			}
		})
	}
}
