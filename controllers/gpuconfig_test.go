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

package controllers

import (
	"testing"

	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	ctrl "sigs.k8s.io/controller-runtime"

	gpuv1 "github.com/NVIDIA/gpu-operator/api/nvidia/v1"
	"github.com/NVIDIA/gpu-operator/internal/gpuconfig/decision"
	"github.com/NVIDIA/gpu-operator/internal/gpuconfig/gate"
	"github.com/NVIDIA/gpu-operator/internal/gpuconfig/publish"
)

var gpuConfigValidator = gpuv1.ValidatorSpec{
	Repository:      "nvcr.io/nvidia",
	Image:           "gpu-operator",
	Version:         "v1.0.0",
	ImagePullPolicy: "Always",
}

const gpuConfigValidatorImage = "nvcr.io/nvidia/gpu-operator:v1.0.0"

func gpuConfigManagerDaemonset() Daemonset {
	return NewDaemonset().
		WithInitContainer(corev1.Container{Name: "driver-validation"}).
		WithContainer(corev1.Container{
			Name: "nvidia-gpu-config-manager",
			Env: []corev1.EnvVar{
				{Name: "NODE_NAME", ValueFrom: &corev1.EnvVarSource{FieldRef: &corev1.ObjectFieldSelector{FieldPath: "spec.nodeName"}}},
				{Name: GPUConfigClusterDocumentEnvName, Value: "FILLED BY THE OPERATOR"},
			},
			Resources: corev1.ResourceRequirements{Requests: corev1.ResourceList{
				corev1.ResourceCPU: resource.MustParse("100m"),
			}},
		})
}

func envValue(ctr corev1.Container, name string) string {
	for _, e := range ctr.Env {
		if e.Name == name {
			return e.Value
		}
	}
	return ""
}

func TestTransformGPUConfigManager(t *testing.T) {
	limits := corev1.ResourceList{corev1.ResourceMemory: resource.MustParse("1Gi")}
	manifestRequests := corev1.ResourceList{corev1.ResourceCPU: resource.MustParse("100m")}
	tests := map[string]struct {
		manager         gpuv1.GPUConfigManagerSpec
		wantEnv         map[string]string
		wantEnvLen      int
		wantResources   corev1.ResourceRequirements
		wantHostNetwork bool
	}{
		"defaults keep the manifest's requests": {
			manager:       gpuv1.GPUConfigManagerSpec{Enabled: new(true)},
			wantEnv:       map[string]string{GPUConfigClusterDocumentEnvName: "gpu-config"},
			wantEnvLen:    2,
			wantResources: corev1.ResourceRequirements{Requests: manifestRequests},
		},
		"manifest valueFrom replaced": {
			manager: gpuv1.GPUConfigManagerSpec{
				Enabled: new(true),
				Env:     []gpuv1.EnvVar{{Name: "NODE_NAME", Value: "node-x"}},
			},
			wantEnv:       map[string]string{GPUConfigClusterDocumentEnvName: "gpu-config", "NODE_NAME": "node-x"},
			wantEnvLen:    2,
			wantResources: corev1.ResourceRequirements{Requests: manifestRequests},
		},
		"all fields set": {
			manager: gpuv1.GPUConfigManagerSpec{
				Enabled:     new(true),
				Config:      &gpuv1.GPUConfigManagerConfigSpec{Name: "gpu-config-v2"},
				Env:         []gpuv1.EnvVar{{Name: "POOL_LABEL_KEY", Value: "karpenter.sh/nodepool"}},
				Resources:   &gpuv1.ResourceRequirements{Limits: limits},
				HostNetwork: new(true),
			},
			wantEnv: map[string]string{
				GPUConfigClusterDocumentEnvName: "gpu-config-v2",
				"POOL_LABEL_KEY":                "karpenter.sh/nodepool",
			},
			wantEnvLen:      3,
			wantResources:   corev1.ResourceRequirements{Limits: limits},
			wantHostNetwork: true,
		},
	}
	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			ds := gpuConfigManagerDaemonset()
			spec := &gpuv1.ClusterPolicySpec{Validator: gpuConfigValidator, GPUConfigManager: tc.manager}
			require.NoError(t, TransformGPUConfigManager(ds.DaemonSet, spec, ClusterPolicyController{logger: ctrl.Log.WithName("test")}))

			pod := ds.Spec.Template.Spec
			main := pod.Containers[0]
			require.Equal(t, gpuConfigValidatorImage, main.Image)
			require.Equal(t, corev1.PullAlways, main.ImagePullPolicy)
			require.Len(t, main.Env, tc.wantEnvLen)
			for k, v := range tc.wantEnv {
				require.Equal(t, v, envValue(main, k), k)
			}
			for _, e := range main.Env {
				require.False(t, e.Value != "" && e.ValueFrom != nil, "%s has both value and valueFrom", e.Name)
			}
			require.Equal(t, tc.wantResources, main.Resources)
			require.Equal(t, tc.wantHostNetwork, pod.HostNetwork)

			initCtr := pod.InitContainers[0]
			require.Equal(t, gpuConfigValidatorImage, initCtr.Image)
			require.Equal(t, int64(0), *initCtr.SecurityContext.RunAsUser)
		})
	}
}

func TestTransformGPUConfigManagerMissingContainer(t *testing.T) {
	ds := NewDaemonset().WithContainer(corev1.Container{Name: "other"})
	err := TransformGPUConfigManager(ds.DaemonSet, &gpuv1.ClusterPolicySpec{Validator: gpuConfigValidator}, ClusterPolicyController{})
	require.Error(t, err)
}

func TestTransformGPUConfigGate(t *testing.T) {
	tests := map[string]struct {
		manager gpuv1.GPUConfigManagerSpec
		wantEnv map[string]string
		wantLen int
	}{
		"manager unset": {wantLen: 3},
		"manager disabled, env still applied": {
			manager: gpuv1.GPUConfigManagerSpec{
				Enabled: new(false),
				Env:     []gpuv1.EnvVar{{Name: "GPU_CONFIG_NO_MODULE_TIMEOUT", Value: "10m"}},
			},
			wantEnv: map[string]string{"GPU_CONFIG_NO_MODULE_TIMEOUT": "10m"},
			wantLen: 4,
		},
		"manager enabled, manifest value replaced": {
			manager: gpuv1.GPUConfigManagerSpec{
				Enabled: new(true),
				Env: []gpuv1.EnvVar{
					{Name: "GPU_CONFIG_NO_MODULE_TIMEOUT", Value: "1m"},
					{Name: "WITH_WAIT", Value: "false"},
				},
			},
			wantEnv: map[string]string{"GPU_CONFIG_NO_MODULE_TIMEOUT": "1m", "WITH_WAIT": "false", "COMPONENT": "gpu-config"},
			wantLen: 4,
		},
		"manifest valueFrom replaced": {
			manager: gpuv1.GPUConfigManagerSpec{Env: []gpuv1.EnvVar{{Name: "NODE_NAME", Value: "node-x"}}},
			wantEnv: map[string]string{"NODE_NAME": "node-x"},
			wantLen: 3,
		},
	}
	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			ds := NewDaemonset().
				WithInitContainer(corev1.Container{Name: "driver-validation"}).
				WithInitContainer(corev1.Container{Name: "gpu-config", Env: []corev1.EnvVar{
					{Name: "COMPONENT", Value: "gpu-config"},
					{Name: "WITH_WAIT", Value: "true"},
					{Name: "NODE_NAME", ValueFrom: &corev1.EnvVarSource{FieldRef: &corev1.ObjectFieldSelector{FieldPath: "spec.nodeName"}}},
				}}).
				WithInitContainer(corev1.Container{Name: "toolkit-validation"})
			spec := &gpuv1.ClusterPolicySpec{Validator: gpuConfigValidator, GPUConfigManager: tc.manager}
			require.NoError(t, transformGPUConfigGate(spec, &ds.Spec.Template.Spec))

			inits := ds.Spec.Template.Spec.InitContainers
			gateCtr := inits[1]
			require.Equal(t, gpuConfigValidatorImage, gateCtr.Image)
			require.Equal(t, corev1.PullAlways, gateCtr.ImagePullPolicy)
			require.Equal(t, int64(0), *gateCtr.SecurityContext.RunAsUser)
			require.Len(t, gateCtr.Env, tc.wantLen)
			for k, v := range tc.wantEnv {
				require.Equal(t, v, envValue(gateCtr, k), k)
			}
			for _, e := range gateCtr.Env {
				require.False(t, e.Value != "" && e.ValueFrom != nil, "%s has both value and valueFrom", e.Name)
			}
			require.Empty(t, inits[0].Env, "only the gpu-config init container gets the env")
			require.Empty(t, inits[2].Env, "only the gpu-config init container gets the env")
		})
	}

	without := NewDaemonset().WithInitContainer(corev1.Container{Name: "driver-validation"})
	require.NoError(t, transformGPUConfigGate(&gpuv1.ClusterPolicySpec{Validator: gpuConfigValidator}, &without.Spec.Template.Spec))
}

func TestGPUConfigManagerStateEnabled(t *testing.T) {
	for _, enabled := range []bool{true, false} {
		n := ClusterPolicyController{singleton: &gpuv1.ClusterPolicy{Spec: gpuv1.ClusterPolicySpec{
			GPUConfigManager: gpuv1.GPUConfigManagerSpec{Enabled: new(enabled)},
		}}}
		require.Equal(t, enabled, n.isStateEnabled("state-gpu-config-manager"))
	}
	n := ClusterPolicyController{singleton: &gpuv1.ClusterPolicy{}}
	require.False(t, n.isStateEnabled("state-gpu-config-manager"), "disabled by default")
}

// internal/gpuconfig imports nothing from the rest of this repo, so it keeps
// its own copies of the names the operator renders; this pins them.
func TestGPUConfigNamesMatchOperator(t *testing.T) {
	require.Equal(t, "nvidia-gpu-config-manager", decision.ApplierDaemonSet(decision.ContainerStack))
	require.Equal(t, "nvidia-operator-validator", decision.ValidatorDaemonSet)
	require.Equal(t, "gpu-config", decision.GateContainer)
	require.Equal(t, gpuConfigManagerDeployLabelKey, gate.DeployLabel)
	require.Equal(t, "true", gpuStateLabels[gpuWorkloadConfigContainer][gate.DeployLabel])
	require.Equal(t, "nvidia.com/gpu-config.sync", publish.SyncLabel)
}
