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
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"

	"github.com/NVIDIA/gpu-operator/internal/consts"
)

// Characterization tests: every test in this file locks in behaviour that looks wrong, so that a
// change to it surfaces in review instead of silently altering what the operator does. They are
// deliberately not assertions of intended behaviour, and a failure here is as likely to mean the
// implementation was fixed as that it regressed — read the referenced bug before touching the test.

// expectedErrorLogRecord pins the error value alongside the message, because suspected bug #1 is
// that the failed lookup survives only as a log line, so the value it carries is the whole record.
type expectedErrorLogRecord struct {
	message      string
	statusReason metav1.StatusReason
	statusCode   int32
}

func TestGetContainerRuntimeCharacterization(t *testing.T) {
	testCases := []struct {
		description              string
		handlers                 map[string]http.HandlerFunc
		expectedContainerRuntime string
		expectedErrorLogs        []expectedErrorLogRecord
		expectedRequestPaths     []string
	}{
		{
			// Suspected bug #1: getContainerRuntime logs the getOpenshiftVersion error and carries on with the
			// empty version, so the crio short-circuit is skipped and the failed read is only ever a log line.
			description: "an OpenShift version lookup failure is swallowed and node inspection continues",
			handlers: map[string]http.HandlerFunc{
				pathClusterVersion: respondServerError(),
				pathNodes:          respondWithJSON(http.StatusOK, gpuNodeList("containerd://1.7.11")),
			},
			expectedContainerRuntime: consts.Containerd,
			expectedErrorLogs: []expectedErrorLogRecord{{
				message:      "failed to retrieve",
				statusReason: metav1.StatusReasonInternalError,
				statusCode:   http.StatusInternalServerError,
			}},
			expectedRequestPaths: []string{pathClusterVersion, pathNodes},
		},
		{
			// Suspected bug #8: a Completed history entry with an empty version returns the same ("", nil)
			// pair as "not OpenShift", so a real OpenShift cluster has its runtime read off its nodes instead.
			description: "an OpenShift cluster with an empty completed version falls through to node inspection",
			handlers: map[string]http.HandlerFunc{
				pathClusterVersion: respondWithJSON(http.StatusOK,
					clusterVersion(updateHistory(configv1.CompletedUpdate, ""))),
				pathNodes: respondWithJSON(http.StatusOK, gpuNodeList("docker://20.10.23")),
			},
			expectedContainerRuntime: consts.Docker,
			expectedRequestPaths:     []string{pathClusterVersion, pathNodes},
		},
		{
			// Suspected bug #9: only a containerd node breaks out of the scan, so every later node that parses
			// overwrites the verdict and a docker/cri-o cluster's runtime flips with the apiserver's order.
			description: "without a containerd node the last node listed decides: docker then cri-o",
			handlers: map[string]http.HandlerFunc{
				pathClusterVersion: respondNotFound(),
				pathNodes: respondWithJSON(http.StatusOK,
					gpuNodeList("docker://20.10.23", "cri-o://1.28.2")),
			},
			expectedContainerRuntime: consts.CRIO,
			expectedRequestPaths:     []string{pathClusterVersion, pathNodes},
		},
		{
			// Suspected bug #9 with the same nodes reversed: the scan's last write wins either way, so the
			// verdict tracks list order and no precedence between docker and cri-o is being applied.
			description: "without a containerd node the last node listed decides: cri-o then docker",
			handlers: map[string]http.HandlerFunc{
				pathClusterVersion: respondNotFound(),
				pathNodes: respondWithJSON(http.StatusOK,
					gpuNodeList("cri-o://1.28.2", "docker://20.10.23")),
			},
			expectedContainerRuntime: consts.Docker,
			expectedRequestPaths:     []string{pathClusterVersion, pathNodes},
		},
	}

	for _, testCase := range testCases {
		t.Run(testCase.description, func(t *testing.T) {
			ctx, recorder := testContextWithCapturedLogs(t)
			apiServer := newStubAPIServer(t, testCase.handlers)

			containerRuntime, err := getContainerRuntime(ctx, apiServer.restConfig)

			require.NoError(t, err)
			require.Equal(t, testCase.expectedContainerRuntime, containerRuntime)
			capturedErrorRecords := recorder.capturedRecordsAtLevel(logRecordLevelError)
			require.Len(t, capturedErrorRecords, len(testCase.expectedErrorLogs))
			for index, expectedErrorLog := range testCase.expectedErrorLogs {
				require.Equal(t, expectedErrorLog.message, capturedErrorRecords[index].message)
				requireAPIStatusError(t, capturedErrorRecords[index].err,
					expectedErrorLog.statusReason, expectedErrorLog.statusCode)
			}
			assertRequestedPathSequence(t, apiServer, testCase.expectedRequestPaths)
		})
	}
}

func TestNewCharacterization(t *testing.T) {
	// Suspected bug #1 from the caller: getContainerRuntime swallows the denied lookup, so New lists nodes
	// first and only its own second lookup aborts, under the "failed to get openshift version" wrapper.
	t.Run("a denied OpenShift lookup aborts New only after nodes have been listed", func(t *testing.T) {
		ctx := testContext(t)
		apiServer := newStubAPIServer(t, map[string]http.HandlerFunc{
			pathClusterVersion: respondForbidden(),
			pathNodes:          respondWithJSON(http.StatusOK, gpuNodeList("containerd://1.7.11")),
		})

		clusterInfoAPI, err := New(ctx, WithKubernetesConfig(apiServer.restConfig), WithOneShot(true))

		require.Nil(t, clusterInfoAPI)
		require.ErrorContains(t, err, "failed to get openshift version")
		requireAPIStatusError(t, err, metav1.StatusReasonForbidden, http.StatusForbidden)
		assertRequestedPathSequence(t, apiServer, []string{pathClusterVersion, pathNodes, pathClusterVersion})
	})
}

func TestGetOpenshiftVersionCharacterization(t *testing.T) {
	// Suspected bug #8: strings.Split("", ".") yields one empty element, so a Completed entry carrying no
	// version returns ("", nil) and an OpenShift cluster is indistinguishable from a plain Kubernetes one.
	t.Run("a completed entry with an empty version yields the not-OpenShift sentinel", func(t *testing.T) {
		ctx := testContext(t)
		apiServer := newStubAPIServer(t, map[string]http.HandlerFunc{
			pathClusterVersion: respondWithJSON(http.StatusOK,
				clusterVersion(updateHistory(configv1.CompletedUpdate, ""))),
		})

		openshiftVersion, err := getOpenshiftVersion(ctx, apiServer.restConfig)

		require.NoError(t, err)
		require.Empty(t, openshiftVersion)
		assertRequestedPathSequence(t, apiServer, []string{pathClusterVersion})
	})
}

func TestGetDRAResourceGVRCharacterization(t *testing.T) {
	// Suspected bug #3: the DeviceClass scan spans every API group before the resource.k8s.io filter, so an
	// unrelated CRD of that Kind bypasses the unsupported early return and fails a GPUCluster reconcile.
	t.Run("a DeviceClass only in an unrelated group is an error, not an unsupported verdict", func(t *testing.T) {
		ctx := testContext(t)
		clusterWithoutAnyDeviceClass := newStubAPIServer(t, discoveryHandlers())
		unrelatedDeviceClassGroup := unrelatedDeviceClassAPIGroup()
		clusterWithOnlyAnUnrelatedDeviceClass := newStubAPIServer(t, discoveryHandlers(unrelatedDeviceClassGroup))

		absentDeviceClassGVR, absentDeviceClassDRASupported, absentDeviceClassErr :=
			getDRAResourceGVR(ctx, clusterWithoutAnyDeviceClass.restConfig)

		require.NoError(t, absentDeviceClassErr)
		require.False(t, absentDeviceClassDRASupported)
		require.Equal(t, schema.GroupVersionResource{}, absentDeviceClassGVR)

		unrelatedDeviceClassGVR, unrelatedDeviceClassDRASupported, unrelatedDeviceClassErr :=
			getDRAResourceGVR(ctx, clusterWithOnlyAnUnrelatedDeviceClass.restConfig)

		require.ErrorContains(t, unrelatedDeviceClassErr,
			"could not determine the GVR for the DeviceClass resource from discovered group/versions: devices.example.com/v1")
		require.False(t, unrelatedDeviceClassDRASupported)
		require.Equal(t, schema.GroupVersionResource{}, unrelatedDeviceClassGVR)
		assertRequestedPathSet(t, clusterWithoutAnyDeviceClass, discoveryPaths())
		assertRequestedPathSet(t, clusterWithOnlyAnUnrelatedDeviceClass, discoveryPaths(unrelatedDeviceClassGroup))
	})

	// Suspected bug #7: an ErrGroupDiscoveryFailed is treated as continuable without checking which group
	// failed, so an unreachable resource.k8s.io reads as "DRA not supported" with a nil error, not a retry.
	t.Run("an unreachable resource.k8s.io reports DRA unsupported with no error", func(t *testing.T) {
		ctx, recorder := testContextWithCapturedLogs(t)
		unreachableDRAGroup := unreachableDRAAPIGroup("v1")
		apiServer := newStubAPIServer(t, discoveryHandlers(unreachableDRAGroup))

		draResourceGVR, draSupported, err := getDRAResourceGVR(ctx, apiServer.restConfig)

		require.NoError(t, err)
		require.False(t, draSupported)
		require.Equal(t, schema.GroupVersionResource{}, draResourceGVR)
		// The log line is the only operator-visible sign that the unsupported verdict was reached by
		// giving up rather than by looking, so its wording and the cause it carries are behaviour.
		capturedRecords := recorder.capturedRecords()
		require.Len(t, capturedRecords, 1)
		partialDiscoveryFailureRecord := capturedRecords[0]
		require.Equal(t, logRecordLevelInfo, partialDiscoveryFailureRecord.level)
		require.Equal(t, "partial API discovery failure; continuing with discovered groups",
			partialDiscoveryFailureRecord.message)
		// logr clamps V's negative level to zero before the sink sees it, so the warning level is
		// unobservable; the recorded zero still catches a raise to debug, which would hide the line.
		require.Equal(t, 0, partialDiscoveryFailureRecord.verbosity)
		require.Len(t, partialDiscoveryFailureRecord.keysAndValues, 2)
		require.Equal(t, "error", partialDiscoveryFailureRecord.keysAndValues[0])
		require.Contains(t, partialDiscoveryFailureRecord.keysAndValues[1], "resource.k8s.io/v1")
		assertRequestedPathSet(t, apiServer, discoveryPaths(unreachableDRAGroup))
	})
}

func TestClusterInfoGetDRAResourceGVRCharacterization(t *testing.T) {
	// Suspected bug #7 from the caller: the unsupported verdict carries no error, so a WithOneShot(true)
	// instance caches it for life. main.go passes false, so today's accessor re-runs discovery every call.
	t.Run("oneshot caches an unsupported verdict caused by a transient discovery failure", func(t *testing.T) {
		ctx := testContext(t)
		apiServer := newStubAPIServer(t, mergeHandlers(discoveryHandlers(unreachableDRAAPIGroup("v1")),
			map[string]http.HandlerFunc{
				pathClusterVersion:           respondNotFound(),
				pathNodes:                    respondWithJSON(http.StatusOK, gpuNodeList("containerd://1.7.11")),
				pathDriverToolkitImageStream: respondNotFound(),
			}))

		clusterInfoAPI, err := New(ctx, WithKubernetesConfig(apiServer.restConfig), WithOneShot(true))
		require.NoError(t, err)
		groupListRequestCountDuringNew := len(apiServer.requestsTo(pathAPIGroups))
		// The exact count is client-go's, not the operator's: ServerPreferredResources repeats the whole
		// walk defaultRetries times on a partial failure, so only "New ran discovery at all" is pinned here.
		require.NotZero(t, groupListRequestCountDuringNew)

		draResourceGVR, draSupported, err := clusterInfoAPI.GetDRAResourceGVR()

		require.NoError(t, err)
		require.False(t, draSupported)
		require.Equal(t, schema.GroupVersionResource{}, draResourceGVR)
		require.Len(t, apiServer.requestsTo(pathAPIGroups), groupListRequestCountDuringNew)
	})
}

func TestGetOpenshiftDTKImagesCharacterization(t *testing.T) {
	// Suspected bug #2: the IsNotFound branch logs and then falls through to logger.Error instead of
	// returning, so a plain Kubernetes cluster logs an OpenShift-only ImageStream's absence as an error.
	t.Run("a missing ImageStream is logged as an error as well as a plain absence", func(t *testing.T) {
		ctx, recorder := testContextWithCapturedLogs(t)
		apiServer := newStubAPIServer(t, map[string]http.HandlerFunc{pathDriverToolkitImageStream: respondNotFound()})

		require.Nil(t, getOpenshiftDTKImages(ctx, apiServer.restConfig))

		require.Equal(t,
			[]string{"ocpHasDriverToolkitImageStream: driver-toolkit imagestream not found"},
			recorder.capturedMessages(logRecordLevelInfo))
		require.Equal(t,
			[]string{"Couldn't get the driver-toolkit imagestream"},
			recorder.capturedMessages(logRecordLevelError))
		assertRequestedPathSequence(t, apiServer, []string{pathDriverToolkitImageStream})
	})
}

func TestGetOpenshiftProxySpecCharacterization(t *testing.T) {
	// Suspected bug #4: the ocpconfigv1.NewForConfig error is logged but not returned, so a nil
	// *ConfigV1Client reaches Proxies().Get and the operator panics instead of reporting the failure.
	t.Run("client construction failure panics instead of returning an error", func(t *testing.T) {
		ctx := testContext(t)

		require.Panics(t, func() {
			_, _ = getOpenshiftProxySpec(ctx, brokenRESTConfig())
		})
	})
}

func TestClusterInfoGetOpenshiftProxySpecCharacterization(t *testing.T) {
	// Suspected bug #5: New's oneshot path never calls getOpenshiftProxySpec, so proxySpec stays nil and a
	// future WithOneShot(true) caller would drop a configured cluster Proxy from the driver DaemonSet.
	t.Run("a oneshot instance built by New never fetches the proxy", func(t *testing.T) {
		ctx := testContext(t)
		apiServer := newStubAPIServer(t, openshiftClusterHandlers)

		clusterInfoAPI, err := New(ctx, WithKubernetesConfig(apiServer.restConfig), WithOneShot(true))
		require.NoError(t, err)

		openshiftProxySpec, err := clusterInfoAPI.GetOpenshiftProxySpec()

		require.NoError(t, err)
		require.Nil(t, openshiftProxySpec)
		require.Empty(t, apiServer.requestsTo(pathProxy))
	})
}

func TestClusterInfoGetOpenshiftDriverToolkitImagesCharacterization(t *testing.T) {
	// Suspected bug #6: the oneshot accessor returns the cached map itself rather than a copy. No current
	// caller mutates it, so this is a trap for a future one rather than a defect an operator sees today.
	t.Run("oneshot hands out the cached map itself, not a copy", func(t *testing.T) {
		clusterInfoUnderTest := &clusterInfo{
			ctx:                          testContext(t),
			oneshot:                      true,
			openshiftDriverToolkitImages: map[string]string{"410.84": "quay.io/openshift/driver-toolkit@sha256:aaa"},
		}

		driverToolkitImages := clusterInfoUnderTest.GetOpenshiftDriverToolkitImages()
		driverToolkitImages["412.86"] = "quay.io/openshift/driver-toolkit@sha256:bbb"

		require.Equal(t, driverToolkitImages, clusterInfoUnderTest.openshiftDriverToolkitImages)
	})
}
