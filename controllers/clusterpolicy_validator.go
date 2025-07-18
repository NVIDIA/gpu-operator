/*
 * Copyright (c) 2025, NVIDIA CORPORATION.  All rights reserved.
 *
 * Licensed under the Apache License, Version 2.0 (the "License");
 * you may not use this file except in compliance with the License.
 * You may obtain a copy of the License at
 *
 *     http://www.apache.org/licenses/LICENSE-2.0
 *
 * Unless required by applicable law or agreed to in writing, software
 * distributed under the License is distributed on an "AS IS" BASIS,
 * WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
 * See the License for the specific language governing permissions and
 * limitations under the License.
 */

package controllers

import (
	"fmt"

	gpuv1 "github.com/NVIDIA/gpu-operator/api/nvidia/v1"
)

func (n *ClusterPolicyController) validateClusterPolicy() error {
	err := validateDRA(n.singleton, n.draSupported)
	if err != nil {
		return fmt.Errorf("failed to validate DRA: %w", err)
	}
	return nil
}

func validateDRA(clusterpolicy *gpuv1.ClusterPolicy, draSupported bool) error {
	if !draSupported && clusterpolicy.Spec.DRADriver.IsEnabled() {
		return fmt.Errorf("the NVIDIA DRA driver for GPUs is enabled in ClusterPolicy but Dynamic Resource Allocation is not enabled in the Kubernetes cluster")
	}

	if clusterpolicy.Spec.DevicePlugin.IsEnabled() && clusterpolicy.Spec.DRADriver.IsGPUsEnabled() {
		return fmt.Errorf("the NVIDIA device plugin and the NVIDIA DRA driver for GPUs cannot both be enabled in ClusterPolicy")
	}

	return nil
}
