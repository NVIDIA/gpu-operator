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
	"errors"
	"net/http"
	"testing"

	"github.com/stretchr/testify/require"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/discovery"
)

func TestGetDRAResourceGVR(t *testing.T) {
	draGroupWithoutDeviceClass := discoveredAPIGroup{
		name:                      "resource.k8s.io",
		versionsInPreferenceOrder: []string{"v1"},
		resources:                 []metav1.APIResource{{Name: "resourceclaims", Kind: "ResourceClaim"}},
	}
	unreachableMetricsGroup := discoveredAPIGroup{
		name:                      "metrics.k8s.io",
		versionsInPreferenceOrder: []string{"v1beta1"},
		versionEndpointsFail:      true,
	}
	// This group and draGroupNamingDeviceClassDifferently are the two halves of "the scan matches on
	// Kind, never on the resource name".
	deviceClassLookalikeGroup := discoveredAPIGroup{
		name:                      "policy.example.com",
		versionsInPreferenceOrder: []string{"v1"},
		resources:                 []metav1.APIResource{{Name: "deviceclasses", Kind: "DeviceClassPolicy"}},
	}
	draGroupNamingDeviceClassDifferently := discoveredAPIGroup{
		name:                      "resource.k8s.io",
		versionsInPreferenceOrder: []string{"v1"},
		resources:                 []metav1.APIResource{{Name: "gpudeviceclasses", Kind: "DeviceClass"}},
	}
	// Two DeviceClass-kind resources under different names are distinct GroupResources, so discovery
	// keeps v1beta1's deviceclasses and v1's gpudeviceclasses side by side instead of collapsing them.
	draGroupServingTwoDeviceClassNames := discoveredAPIGroup{
		name:                      "resource.k8s.io",
		versionsInPreferenceOrder: []string{"v1beta1", "v1"},
		resourcesByVersion: map[string][]metav1.APIResource{
			"v1beta1": {deviceClassAPIResource()},
			"v1":      {{Name: "gpudeviceclasses", Kind: "DeviceClass"}},
		},
	}

	testCases := []struct {
		description            string
		groups                 []discoveredAPIGroup
		handlerOverrides       map[string]http.HandlerFunc
		expectedDRAResourceGVR schema.GroupVersionResource
		expectedDRASupported   bool
		expectedErrorContains  string
		expectedRequestPaths   []string
	}{
		{
			description:            "DeviceClass served at v1",
			groups:                 []discoveredAPIGroup{draAPIGroup("v1")},
			expectedDRAResourceGVR: draGVR("v1"),
			expectedDRASupported:   true,
		},
		{
			description:            "DeviceClass served at v1beta2",
			groups:                 []discoveredAPIGroup{draAPIGroup("v1beta2")},
			expectedDRAResourceGVR: draGVR("v1beta2"),
			expectedDRASupported:   true,
		},
		{
			description:            "DeviceClass served at v1beta1",
			groups:                 []discoveredAPIGroup{draAPIGroup("v1beta1")},
			expectedDRAResourceGVR: draGVR("v1beta1"),
			expectedDRASupported:   true,
		},
		{
			description:            "one deviceclasses at several versions collapses to the preferred v1",
			groups:                 []discoveredAPIGroup{draAPIGroup("v1", "v1beta1")},
			expectedDRAResourceGVR: draGVR("v1"),
			expectedDRASupported:   true,
		},
		{
			description:            "one deviceclasses at several versions collapses to the preferred v1beta1",
			groups:                 []discoveredAPIGroup{draAPIGroup("v1beta1", "v1")},
			expectedDRAResourceGVR: draGVR("v1beta1"),
			expectedDRASupported:   true,
		},
		{
			description:            "the ordered version preference outranks the group's preferred version",
			groups:                 []discoveredAPIGroup{draGroupServingTwoDeviceClassNames},
			expectedDRAResourceGVR: draGVR("v1"),
			expectedDRASupported:   true,
		},
		{
			description:          "a cluster without the resource.k8s.io group does not support DRA",
			expectedDRASupported: false,
		},
		{
			description:          "resource.k8s.io without a DeviceClass resource does not support DRA",
			groups:               []discoveredAPIGroup{draGroupWithoutDeviceClass},
			expectedDRASupported: false,
		},
		{
			description:          "a resource named deviceclasses under another Kind does not support DRA",
			groups:               []discoveredAPIGroup{deviceClassLookalikeGroup},
			expectedDRASupported: false,
		},
		{
			// The Resource of the returned GVR is a constant, so it stays "deviceclasses" whatever
			// name the group listed the Kind under.
			description:            "a DeviceClass listed under another resource name still supports DRA",
			groups:                 []discoveredAPIGroup{draGroupNamingDeviceClassDifferently},
			expectedDRAResourceGVR: draGVR("v1"),
			expectedDRASupported:   true,
		},
		{
			description:            "a DeviceClass in an unrelated group does not displace resource.k8s.io",
			groups:                 []discoveredAPIGroup{draAPIGroup("v1"), unrelatedDeviceClassAPIGroup()},
			expectedDRAResourceGVR: draGVR("v1"),
			expectedDRASupported:   true,
		},
		{
			description:           "a DeviceClass served only at an unsupported version is an error",
			groups:                []discoveredAPIGroup{draAPIGroup("v1alpha3")},
			expectedErrorContains: "could not determine the GVR for the DeviceClass resource",
		},
		{
			description:            "an unreachable API group does not mask resource.k8s.io",
			groups:                 []discoveredAPIGroup{draAPIGroup("v1"), unreachableMetricsGroup},
			expectedDRAResourceGVR: draGVR("v1"),
			expectedDRASupported:   true,
		},
		{
			// A failure listing the groups themselves is not an ErrGroupDiscoveryFailed, so there
			// are no partial results to keep and discovery stops before any group is fetched.
			description:           "a failure listing the API groups is fatal",
			groups:                []discoveredAPIGroup{draAPIGroup("v1")},
			handlerOverrides:      map[string]http.HandlerFunc{pathAPIGroups: respondServerError()},
			expectedErrorContains: "error getting server resources from discovery client",
			expectedRequestPaths:  []string{pathAPIVersions, pathAPIGroups},
		},
		{
			description:           "a failure listing the legacy API versions is fatal",
			groups:                []discoveredAPIGroup{draAPIGroup("v1")},
			handlerOverrides:      map[string]http.HandlerFunc{pathAPIVersions: respondServerError()},
			expectedErrorContains: "error getting server resources from discovery client",
			expectedRequestPaths:  []string{pathAPIVersions},
		},
	}

	for _, testCase := range testCases {
		t.Run(testCase.description, func(t *testing.T) {
			ctx := testContext(t)
			apiServer := newStubAPIServer(t, mergeHandlers(discoveryHandlers(testCase.groups...), testCase.handlerOverrides))

			draResourceGVR, draSupported, err := getDRAResourceGVR(ctx, apiServer.restConfig)

			if testCase.expectedErrorContains != "" {
				require.ErrorContains(t, err, testCase.expectedErrorContains)
				require.False(t, draSupported)
				require.Equal(t, schema.GroupVersionResource{}, draResourceGVR)
			} else {
				require.NoError(t, err)
				require.Equal(t, testCase.expectedDRASupported, draSupported)
				require.Equal(t, testCase.expectedDRAResourceGVR, draResourceGVR)
			}

			expectedPaths := testCase.expectedRequestPaths
			if expectedPaths == nil {
				expectedPaths = discoveryPaths(testCase.groups...)
			}
			assertRequestedPathSet(t, apiServer, expectedPaths)
		})
	}

	// client-go's TLS failure is a bare fmt.Errorf with no sentinel or type to match, so the cause
	// is pinned by unwrapping it and comparing against the error the same constructor produces.
	t.Run("client construction failure", func(t *testing.T) {
		_, expectedClientError := discovery.NewDiscoveryClientForConfig(brokenRESTConfig())
		require.Error(t, expectedClientError)

		draResourceGVR, draSupported, err := getDRAResourceGVR(testContext(t), brokenRESTConfig())

		require.ErrorContains(t, err, "error building discovery client")
		require.EqualError(t, errors.Unwrap(err), expectedClientError.Error())
		require.False(t, draSupported)
		require.Equal(t, schema.GroupVersionResource{}, draResourceGVR)
	})
}

func TestClusterInfoGetDRAResourceGVR(t *testing.T) {
	testCases := []struct {
		description            string
		oneshot                bool
		cachedDRAResourceGVR   schema.GroupVersionResource
		cachedDRASupported     bool
		groups                 []discoveredAPIGroup
		handlerOverrides       map[string]http.HandlerFunc
		expectedDRAResourceGVR schema.GroupVersionResource
		expectedDRASupported   bool
		expectedErrorContains  string
		expectedRequestPaths   []string
	}{
		{
			// A nil rest.Config is the assertion: any API call would fail loudly.
			description:            "oneshot returns the cached GVR without any API call",
			oneshot:                true,
			cachedDRAResourceGVR:   draGVR("v1beta1"),
			cachedDRASupported:     true,
			expectedDRAResourceGVR: draGVR("v1beta1"),
			expectedDRASupported:   true,
		},
		{
			description:          "oneshot reports a cluster cached as not supporting DRA",
			oneshot:              true,
			expectedDRASupported: false,
		},
		{
			description:            "non-oneshot queries the cluster",
			groups:                 []discoveredAPIGroup{draAPIGroup("v1")},
			expectedDRAResourceGVR: draGVR("v1"),
			expectedDRASupported:   true,
		},
		{
			description:            "non-oneshot ignores the cached GVR",
			cachedDRAResourceGVR:   draGVR("v1beta1"),
			cachedDRASupported:     true,
			groups:                 []discoveredAPIGroup{draAPIGroup("v1")},
			expectedDRAResourceGVR: draGVR("v1"),
			expectedDRASupported:   true,
		},
		{
			description:           "non-oneshot propagates errors",
			groups:                []discoveredAPIGroup{draAPIGroup("v1")},
			handlerOverrides:      map[string]http.HandlerFunc{pathAPIGroups: respondServerError()},
			expectedErrorContains: "error getting server resources from discovery client",
			expectedRequestPaths:  []string{pathAPIVersions, pathAPIGroups},
		},
	}

	for _, testCase := range testCases {
		t.Run(testCase.description, func(t *testing.T) {
			clusterInfoUnderTest := &clusterInfo{
				ctx:            testContext(t),
				oneshot:        testCase.oneshot,
				draResourceGVR: testCase.cachedDRAResourceGVR,
				draSupported:   testCase.cachedDRASupported,
			}
			var apiServer *stubAPIServer
			if !testCase.oneshot {
				apiServer = newStubAPIServer(t, mergeHandlers(discoveryHandlers(testCase.groups...), testCase.handlerOverrides))
				clusterInfoUnderTest.config = apiServer.restConfig
			}

			draResourceGVR, draSupported, err := clusterInfoUnderTest.GetDRAResourceGVR()

			if testCase.expectedErrorContains != "" {
				require.ErrorContains(t, err, testCase.expectedErrorContains)
				require.False(t, draSupported)
				require.Equal(t, schema.GroupVersionResource{}, draResourceGVR)
			} else {
				require.NoError(t, err)
				require.Equal(t, testCase.expectedDRASupported, draSupported)
				require.Equal(t, testCase.expectedDRAResourceGVR, draResourceGVR)
			}

			if apiServer != nil {
				expectedPaths := testCase.expectedRequestPaths
				if expectedPaths == nil {
					expectedPaths = discoveryPaths(testCase.groups...)
				}
				assertRequestedPathSet(t, apiServer, expectedPaths)
			}
		})
	}
}
