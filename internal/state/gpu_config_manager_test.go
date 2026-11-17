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
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	nvidiav1 "github.com/NVIDIA/gpu-operator/api/nvidia/v1"
	nvidiav1alpha1 "github.com/NVIDIA/gpu-operator/api/nvidia/v1alpha1"
	"github.com/NVIDIA/gpu-operator/internal/gpuconfig/decision"
)

const gpuConfigManagerManifestDir = "../../manifests/state-gpu-config-manager"

func newTestGPUConfigManagerState(t *testing.T) *configurableState {
	t.Helper()
	t.Setenv("VALIDATOR_IMAGE", "nvcr.io/nvidia/gpu-operator-validator:test")
	c := fake.NewClientBuilder().Build()
	s, err := NewStateGPUConfigManager(c, "test-operator", runtime.NewScheme(), gpuConfigManagerManifestDir)
	require.NoError(t, err)
	return s.(*configurableState)
}

func gpuConfigManagerCR(spec *nvidiav1.GPUConfigManagerSpec) *nvidiav1alpha1.GPUCluster {
	cr := sampleGPUCluster()
	cr.Spec.GPUConfigManager = spec
	return cr
}

func TestGPUConfigManagerDisabled(t *testing.T) {
	cases := map[string]*nvidiav1.GPUConfigManagerSpec{
		"unset":    nil,
		"nil":      {},
		"disabled": {Enabled: new(false)},
	}
	for name, spec := range cases {
		t.Run(name, func(t *testing.T) {
			s := newTestGPUConfigManagerState(t)
			objs, err := s.getManifestObjects(context.Background(), gpuConfigManagerCR(spec), draSupportedCatalog())
			require.NoError(t, err)
			require.Empty(t, objs)
		})
	}
}

func TestGPUConfigManagerRender(t *testing.T) {
	s := newTestGPUConfigManagerState(t)
	objs, err := s.getManifestObjects(context.Background(),
		gpuConfigManagerCR(&nvidiav1.GPUConfigManagerSpec{Enabled: new(true)}), draSupportedCatalog())
	require.NoError(t, err)
	require.Equal(t, map[string]int{
		"ServiceAccount": 1, "Role": 1, "RoleBinding": 1, "ClusterRole": 1, "ClusterRoleBinding": 1, "DaemonSet": 1,
	}, kindCounts(objs))

	ds := findDaemonSet(t, objs)
	// The kubelet plugin's gpu-config init container looks up this DaemonSet by name.
	require.Equal(t, decision.ApplierDaemonSet(decision.DRAStack), ds.Name)
	require.Equal(t, decision.ApplierComponent, ds.Spec.Template.Labels["app.kubernetes.io/component"])
	require.Equal(t, map[string]string{"nvidia.com/gpu.deploy.gpu-config-manager": "true"}, ds.Spec.Template.Spec.NodeSelector)

	tolerations := ds.Spec.Template.Spec.Tolerations
	require.Contains(t, tolerations, corev1.Toleration{Key: "nvidia.com/gpu", Operator: corev1.TolerationOpExists, Effect: corev1.TaintEffectNoSchedule})
	for _, tol := range sampleGPUCluster().Spec.Daemonsets.Tolerations {
		require.Contains(t, tolerations, tol)
	}

	ctr := containerByName(t, ds, "nvidia-gpu-config-manager")
	require.Equal(t, "nvcr.io/nvidia/gpu-operator-validator:test", ctr.Image)
	v, _ := envValue(ctr, "GPU_CONFIG_STACK")
	require.Equal(t, string(decision.DRAStack), v)
	v, _ = envValue(ctr, gpuConfigClusterDocumentEnvName)
	require.Equal(t, "gpu-config", v)
	require.Equal(t, resource.MustParse("100m"), ctr.Resources.Requests[corev1.ResourceCPU])

	require.Len(t, ds.Spec.Template.Spec.InitContainers, 1)
	initCtr := ds.Spec.Template.Spec.InitContainers[0]
	require.Equal(t, "driver-validation", initCtr.Name)
	v, _ = envValue(initCtr, "DRIVER_VALIDATION_SKIP_GPU_INIT")
	require.Equal(t, "true", v)
}

func TestGPUConfigManagerEnv(t *testing.T) {
	tests := map[string]struct {
		env     []nvidiav1.EnvVar
		wantEnv map[string]string
	}{
		"cluster document replaced": {
			env: []nvidiav1.EnvVar{
				{Name: "POOL_LABEL_KEY", Value: "karpenter.sh/nodepool"},
				{Name: gpuConfigClusterDocumentEnvName, Value: "override"},
			},
			wantEnv: map[string]string{gpuConfigClusterDocumentEnvName: "override", "POOL_LABEL_KEY": "karpenter.sh/nodepool"},
		},
		"manifest entries replaced": {
			env: []nvidiav1.EnvVar{
				{Name: "NODE_NAME", Value: "node-x"},
				{Name: "OPERATOR_NAMESPACE", Value: "other-ns"},
				{Name: "GPU_CONFIG_STACK", Value: "device-plugin"},
			},
			wantEnv: map[string]string{"NODE_NAME": "node-x", "OPERATOR_NAMESPACE": "other-ns", "GPU_CONFIG_STACK": "device-plugin"},
		},
	}
	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			s := newTestGPUConfigManagerState(t)
			spec := &nvidiav1.GPUConfigManagerSpec{
				Enabled: new(true),
				Config:  &nvidiav1.GPUConfigManagerConfigSpec{Name: "my-doc"},
				Env:     tc.env,
			}
			objs, err := s.getManifestObjects(context.Background(), gpuConfigManagerCR(spec), draSupportedCatalog())
			require.NoError(t, err)
			ctr := containerByName(t, findDaemonSet(t, objs), "nvidia-gpu-config-manager")

			for k, want := range tc.wantEnv {
				v, _ := envValue(ctr, k)
				require.Equal(t, want, v, k)
			}
			seen := map[string]bool{}
			for _, e := range ctr.Env {
				require.False(t, seen[e.Name], "duplicate env %s", e.Name)
				seen[e.Name] = true
				require.False(t, e.Value != "" && e.ValueFrom != nil, "%s has both value and valueFrom", e.Name)
			}
			for _, k := range []string{"NODE_NAME", "OPERATOR_NAMESPACE", "GPU_CONFIG_STACK", gpuConfigClusterDocumentEnvName} {
				require.True(t, seen[k], k)
			}
		})
	}
}
