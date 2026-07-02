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

package state

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/utils/ptr"

	nvidiav1 "github.com/NVIDIA/gpu-operator/api/nvidia/v1"
)

// draSupportedOpenshiftCatalog mirrors draSupportedCatalog on an OpenShift cluster,
// where the operand states additionally render SecurityContextConstraints.
func draSupportedOpenshiftCatalog() InfoCatalog {
	catalog := NewInfoCatalog()
	catalog.Add(InfoTypeClusterInfo, testClusterInfo{
		openshiftVersion: "4.22",
		draSupported:     true,
		draResourceGVR:   schema.GroupVersionResource{Group: "resource.k8s.io", Version: "v1", Resource: "deviceclasses"},
	})
	return catalog
}

func findSCC(t *testing.T, objs []*unstructured.Unstructured, name string) *unstructured.Unstructured {
	t.Helper()
	for _, obj := range objs {
		if obj.GetKind() == "SecurityContextConstraints" && obj.GetName() == name {
			return obj
		}
	}
	return nil
}

func sccUsers(t *testing.T, scc *unstructured.Unstructured) []string {
	t.Helper()
	users, found, err := unstructured.NestedStringSlice(scc.Object, "users")
	require.NoError(t, err)
	require.True(t, found)
	return users
}

func TestGPUClusterOperandSCCs(t *testing.T) {
	testCases := []struct {
		name    string
		sccName string
		users   []string
		render  func(t *testing.T, catalog InfoCatalog) []*unstructured.Unstructured
	}{
		{
			name:    "dra-driver",
			sccName: "nvidia-dra-driver",
			users: []string{
				"system:serviceaccount:test-operator:nvidia-dra-driver-kubeletplugin",
				"system:serviceaccount:test-operator:compute-domain-daemon-service-account",
			},
			render: func(t *testing.T, catalog InfoCatalog) []*unstructured.Unstructured {
				s := newTestDRAState(t)
				objs, err := s.getManifestObjects(context.Background(), sampleGPUCluster(), catalog)
				require.NoError(t, err)
				return objs
			},
		},
		{
			name:    "dra-validation",
			sccName: "nvidia-dra-validator",
			users:   []string{"system:serviceaccount:test-operator:nvidia-dra-validator"},
			render: func(t *testing.T, catalog InfoCatalog) []*unstructured.Unstructured {
				s := newTestDRAValidationState(t)
				objs, err := s.getManifestObjects(context.Background(), sampleGPUCluster(), catalog)
				require.NoError(t, err)
				return objs
			},
		},
		{
			name:    "dcgm",
			sccName: "nvidia-dcgm-dra",
			users:   []string{"system:serviceaccount:test-operator:nvidia-dcgm-dra"},
			render: func(t *testing.T, catalog InfoCatalog) []*unstructured.Unstructured {
				s := newTestDCGMState(t)
				cr := sampleGPUCluster()
				cr.Spec.DCGM = &nvidiav1.DCGMSpec{Enabled: ptr.To(true)}
				objs, err := s.getManifestObjects(context.Background(), cr, catalog)
				require.NoError(t, err)
				return objs
			},
		},
		{
			name:    "dcgm-exporter",
			sccName: "nvidia-dcgm-exporter-dra",
			users:   []string{"system:serviceaccount:test-operator:nvidia-dcgm-exporter-dra"},
			render: func(t *testing.T, catalog InfoCatalog) []*unstructured.Unstructured {
				s := newTestDCGMExporterState(t, false)
				objs, err := s.getManifestObjects(context.Background(), exporterCR(&nvidiav1.DCGMExporterSpec{}), catalog)
				require.NoError(t, err)
				return objs
			},
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			// On OpenShift the state renders its SCC with the operand ServiceAccounts.
			objs := tc.render(t, draSupportedOpenshiftCatalog())
			scc := findSCC(t, objs, tc.sccName)
			require.NotNil(t, scc, "expected SCC %s to be rendered on OpenShift", tc.sccName)
			require.Equal(t, tc.users, sccUsers(t, scc))

			// On vanilla Kubernetes no SCC is rendered.
			objs = tc.render(t, draSupportedCatalog())
			require.Nil(t, findSCC(t, objs, tc.sccName))
		})
	}
}
