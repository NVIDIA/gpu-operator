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

	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"

	nvidiav1 "github.com/NVIDIA/gpu-operator/api/nvidia/v1"
	nvidiav1alpha1 "github.com/NVIDIA/gpu-operator/api/nvidia/v1alpha1"
)

// gpuConfigClusterDocumentEnvName passes the name of the ConfigMap with the GPU
// settings for all nodes to the GPU configuration manager.
const gpuConfigClusterDocumentEnvName = "GPU_CONFIG_CLUSTER_DOCUMENT"

func NewStateGPUConfigManager(
	k8sClient client.Client,
	namespace string,
	scheme *runtime.Scheme,
	manifestDir string) (State, error) {

	skel, err := newStateSkel(k8sClient, namespace, scheme, manifestDir,
		"state-gpu-config-manager", "NVIDIA GPU configuration manager deployed in the cluster")
	if err != nil {
		return nil, err
	}
	return &configurableState{
		stateSkel: skel,
		isEnabled: gpuConfigManagerEnabled,
		// The GPU configuration manager ships in the gpu-operator image, as the validator does.
		imageEnvName:    draValidatorImageEnvName,
		buildRenderData: buildGPUConfigManagerRenderData,
	}, nil
}

func buildGPUConfigManagerRenderData(_ context.Context, s *configurableState, cr *nvidiav1alpha1.GPUCluster, imagePath, _, openshiftVersion string) (any, error) {
	spec := cr.Spec.GPUConfigManager
	hostPaths := cr.Spec.HostPaths
	daemonsets := cr.Spec.Daemonsets
	return &gpuConfigManagerRenderData{
		GPUConfigManager: &gpuConfigManagerSpec{
			Spec:      spec,
			ImagePath: imagePath,
			Env:       gpuConfigManagerEnv(spec),
		},
		// The gpu-operator image's pull settings come from the DRA driver spec,
		// as for the DRA validator.
		PullSpec:         &cr.Spec.DRADriver,
		HostPaths:        &hostPaths,
		Daemonsets:       &daemonsets,
		Namespace:        s.namespace,
		OpenshiftVersion: openshiftVersion,
	}, nil
}

// gpuConfigManagerEnv returns the GPU configuration manager's configurable
// environment: the cluster-wide ConfigMap name, then the CR's variables, which
// replace one of the same name.
func gpuConfigManagerEnv(spec *nvidiav1.GPUConfigManagerSpec) []nvidiav1.EnvVar {
	env := []nvidiav1.EnvVar{{Name: gpuConfigClusterDocumentEnvName, Value: spec.ClusterDocumentName()}}
	return dedupeEnv(append(env, spec.Env...))
}

// dedupeEnv keeps one entry per name, at the first position with the last value,
// which matches setting each entry in turn as the ClusterPolicy side does.
func dedupeEnv(env []nvidiav1.EnvVar) []nvidiav1.EnvVar {
	var out []nvidiav1.EnvVar
	index := map[string]int{}
	for _, e := range env {
		if i, ok := index[e.Name]; ok {
			out[i].Value = e.Value
			continue
		}
		index[e.Name] = len(out)
		out = append(out, e)
	}
	return out
}
