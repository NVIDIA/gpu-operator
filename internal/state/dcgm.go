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

	nvidiav1alpha1 "github.com/NVIDIA/gpu-operator/api/nvidia/v1alpha1"
)

const (
	// dcgmImageEnvName is the fallback env var for the DCGM image when the CR does not
	// specify repository/image/version.
	dcgmImageEnvName = "DCGM_IMAGE"
)

func NewStateDCGM(
	k8sClient client.Client,
	namespace string,
	scheme *runtime.Scheme,
	manifestDir string) (State, error) {

	skel, err := newStateSkel(k8sClient, namespace, scheme, manifestDir,
		"state-dcgm", "NVIDIA DCGM hostengine deployed in the cluster")
	if err != nil {
		return nil, err
	}
	return &configurableState{
		stateSkel: skel,
		isEnabled: dcgmEnabled,
		imageOverride: func(cr *nvidiav1alpha1.GPUCluster) (string, string, string) {
			spec := cr.Spec.DCGM
			return spec.Repository, spec.Image, spec.Version
		},
		imageEnvName:    dcgmImageEnvName,
		buildRenderData: buildDCGMRenderData,
	}, nil
}

func buildDCGMRenderData(_ context.Context, s *configurableState, cr *nvidiav1alpha1.GPUCluster, imagePath, apiVersion, openshiftVersion string) (interface{}, error) {
	daemonsets := cr.Spec.Daemonsets
	return &dcgmRenderData{
		DCGM:                    &dcgmSpec{Spec: cr.Spec.DCGM, ImagePath: imagePath},
		Daemonsets:              &daemonsets,
		Namespace:               s.namespace,
		OpenshiftVersion:        openshiftVersion,
		ResourceClaimAPIVersion: apiVersion,
	}, nil
}
