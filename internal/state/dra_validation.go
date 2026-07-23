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

func NewStateDRAValidation(
	k8sClient client.Client,
	namespace string,
	scheme *runtime.Scheme,
	manifestDir string) (State, error) {

	skel, err := newStateSkel(k8sClient, namespace, scheme, manifestDir,
		"state-dra-validation", "NVIDIA DRA validator deployed in the cluster")
	if err != nil {
		return nil, err
	}
	return &configurableState{
		stateSkel: skel,
		// The validator claims a gpu.nvidia.com device, which the always-deployed gpus
		// capability of the DRA driver allocates.
		isEnabled: func(_ *nvidiav1alpha1.GPUCluster) bool {
			return true
		},
		// The validator runs the gpu-operator image (same as the DRA driver init container),
		// so it has no per-CR image overrides and relies solely on VALIDATOR_IMAGE.
		imageEnvName:    draValidatorImageEnvName,
		buildRenderData: buildValidatorRenderData,
	}, nil
}

func buildValidatorRenderData(_ context.Context, s *configurableState, cr *nvidiav1alpha1.GPUCluster, imagePath, apiVersion, openshiftVersion string) (interface{}, error) {
	// Reuse the DRA driver spec for the image pull settings.
	spec := cr.Spec.DRADriver
	daemonsets := cr.Spec.Daemonsets
	return &validatorRenderData{
		Validator:               &draDriverSpec{Spec: &spec, ImagePath: imagePath},
		Daemonsets:              &daemonsets,
		Namespace:               s.namespace,
		OpenshiftVersion:        openshiftVersion,
		ResourceClaimAPIVersion: apiVersion,
	}, nil
}
