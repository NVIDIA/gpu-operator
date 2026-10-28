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
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/rest"

	"github.com/NVIDIA/gpu-operator/internal/consts"
)

func TestGetRuntimeString(t *testing.T) {
	tests := []struct {
		description string
		runtimeVer  string
		expected    string
		expectError bool
	}{
		{
			description: "docker runtime",
			runtimeVer:  "docker://20.10.7",
			expected:    consts.Docker,
		},
		{
			description: "containerd runtime",
			runtimeVer:  "containerd://1.6.8",
			expected:    consts.Containerd,
		},
		{
			description: "cri-o runtime",
			runtimeVer:  "cri-o://1.24.1",
			expected:    consts.CRIO,
		},
		{
			description: "unrecognized runtime returns error",
			runtimeVer:  "unknown-runtime://1.0.0",
			expectError: true,
		},
		{
			description: "empty runtime version returns error",
			runtimeVer:  "",
			expectError: true,
		},
	}

	for _, tc := range tests {
		t.Run(tc.description, func(t *testing.T) {
			node := corev1.Node{
				Status: corev1.NodeStatus{
					NodeInfo: corev1.NodeSystemInfo{
						ContainerRuntimeVersion: tc.runtimeVer,
					},
				},
			}

			result, err := getRuntimeString(node)

			if tc.expectError {
				require.Error(t, err)
				return
			}
			require.NoError(t, err)
			require.Equal(t, tc.expected, result)
		})
	}
}

// TestGetOpenshiftVersionDetection confirms that a Kubernetes deployment
// requires no OpenShift ClusterVersion permission, while actual OpenShift
// discovery and permission errors retain their meaning.
func TestGetOpenshiftVersionDetection(t *testing.T) {
	tests := []struct {
		name             string
		discoveryStatus  int
		resourcePresent  bool
		versionStatus    int
		wantVersion      string
		wantError        bool
		wantVersionCalls int
	}{
		{name: "plain Kubernetes", discoveryStatus: 404},
		{name: "group exists without ClusterVersion", discoveryStatus: 200},
		{name: "OpenShift", discoveryStatus: 200, resourcePresent: true, versionStatus: 200, wantVersion: "4.19", wantVersionCalls: 1},
		{name: "OpenShift resource forbidden", discoveryStatus: 200, resourcePresent: true, versionStatus: 403, wantError: true, wantVersionCalls: 1},
		{name: "discovery forbidden", discoveryStatus: 403, wantError: true},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			versionCalls := 0
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				switch r.URL.Path {
				case "/apis/config.openshift.io/v1":
					w.WriteHeader(tc.discoveryStatus)
					if tc.discoveryStatus != 200 {
						fmt.Fprintf(w, `{"kind":"Status","apiVersion":"v1","status":"Failure","reason":%q,"code":%d}`, http.StatusText(tc.discoveryStatus), tc.discoveryStatus)
						return
					}
					resources := []metav1.APIResource{}
					if tc.resourcePresent {
						resources = append(resources, metav1.APIResource{Name: "clusterversions", Kind: "ClusterVersion", Namespaced: false})
					}
					require.NoError(t, json.NewEncoder(w).Encode(metav1.APIResourceList{TypeMeta: metav1.TypeMeta{Kind: "APIResourceList", APIVersion: "v1"}, GroupVersion: "config.openshift.io/v1", APIResources: resources}))
				case "/apis/config.openshift.io/v1/clusterversions/version":
					versionCalls++
					w.WriteHeader(tc.versionStatus)
					if tc.versionStatus != 200 {
						fmt.Fprintf(w, `{"kind":"Status","apiVersion":"v1","status":"Failure","reason":"Forbidden","code":403}`)
						return
					}
					fmt.Fprint(w, `{"apiVersion":"config.openshift.io/v1","kind":"ClusterVersion","status":{"history":[{"state":"Completed","version":"4.19.7"}]}}`)
				default:
					t.Errorf("unexpected request to %s", r.URL.Path)
					w.WriteHeader(http.StatusNotFound)
				}
			}))
			defer server.Close()
			version, err := getOpenshiftVersion(context.Background(), &rest.Config{Host: server.URL})
			if tc.wantError {
				require.Error(t, err)
			} else {
				require.NoError(t, err)
				require.Equal(t, tc.wantVersion, version)
			}
			require.Equal(t, tc.wantVersionCalls, versionCalls)
		})
	}
}
