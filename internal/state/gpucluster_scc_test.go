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

func TestDRADriverSCCAllowsMPSHostPID(t *testing.T) {
	s := newTestDRAState(t)
	cr := sampleGPUCluster()
	cr.Spec.DRADriver.FeatureGates["MPSSupport"] = true

	objs, err := s.getManifestObjects(context.Background(), cr, draSupportedOpenshiftCatalog())
	require.NoError(t, err)
	scc := findSCC(t, objs, "nvidia-dra-driver")
	require.NotNil(t, scc)

	// MPS daemon templates use SERVICE_ACCOUNT_NAME rather than inheriting
	// the kubelet plugin's ServiceAccount automatically.
	ds := findDaemonSet(t, objs)
	foundServiceAccountEnv := false
	for _, env := range containerByName(t, ds, "gpus").Env {
		if env.Name != "SERVICE_ACCOUNT_NAME" {
			continue
		}
		foundServiceAccountEnv = true
		require.Empty(t, env.Value)
		require.NotNil(t, env.ValueFrom)
		require.NotNil(t, env.ValueFrom.FieldRef)
		require.Equal(t, "spec.serviceAccountName", env.ValueFrom.FieldRef.FieldPath)
	}
	require.True(t, foundServiceAccountEnv)
	serviceAccount := ds.Spec.Template.Spec.ServiceAccountName
	require.NotEmpty(t, serviceAccount)
	require.Contains(t, sccUsers(t, scc), "system:serviceaccount:test-operator:"+serviceAccount)
	allowHostPID, found, err := unstructured.NestedBool(scc.Object, "allowHostPID")
	require.NoError(t, err)
	require.True(t, found)
	require.True(t, allowHostPID, "MPS control daemon pods require hostPID")
}

func TestComputeDomainDaemonAnyUIDBinding(t *testing.T) {
	for _, tc := range []struct {
		name           string
		openshift      bool
		computeDomains bool
	}{
		{name: "openshift-enabled", openshift: true, computeDomains: true},
		{name: "openshift-disabled", openshift: true},
		{name: "kubernetes-enabled", computeDomains: true},
		{name: "kubernetes-disabled"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s := newTestDRAState(t)
			cr := sampleGPUCluster()
			cr.Spec.DRADriver.ComputeDomains.Enabled = new(tc.computeDomains)
			catalog := draSupportedCatalog()
			if tc.openshift {
				catalog = draSupportedOpenshiftCatalog()
			}
			objs, err := s.getManifestObjects(context.Background(), cr, catalog)
			require.NoError(t, err)
			var binding *unstructured.Unstructured
			for _, obj := range objs {
				if obj.GetKind() == "ClusterRoleBinding" && obj.GetName() == "compute-domain-daemon-openshift-anyuid-role-binding" {
					binding = obj
				}
			}
			if tc.openshift && tc.computeDomains {
				require.NotNil(t, binding)
				require.Equal(t, map[string]any{
					"apiGroup": "rbac.authorization.k8s.io",
					"kind":     "ClusterRole",
					"name":     "system:openshift:scc:anyuid",
				}, binding.Object["roleRef"])
				require.Equal(t, []any{map[string]any{
					"kind":      "ServiceAccount",
					"name":      "compute-domain-daemon-service-account",
					"namespace": "test-operator",
				}}, binding.Object["subjects"])
			} else {
				require.Nil(t, binding)
			}
			if tc.openshift {
				scc := findSCC(t, objs, "nvidia-dra-driver")
				require.NotNil(t, scc)
				require.Contains(t, sccUsers(t, scc), "system:serviceaccount:test-operator:compute-domain-daemon-service-account")
				require.Contains(t, scc.Object, "priority")
				require.Nil(t, scc.Object["priority"])
			}
		})
	}
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
				cr.Spec.DCGM = &nvidiav1.DCGMSpec{Enabled: new(true)}
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
