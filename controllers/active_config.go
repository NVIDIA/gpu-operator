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
	"context"
	"fmt"

	"sigs.k8s.io/controller-runtime/pkg/client"

	gpuv1 "github.com/NVIDIA/gpu-operator/api/nvidia/v1"
	nvidiav1alpha1 "github.com/NVIDIA/gpu-operator/api/nvidia/v1alpha1"
	"github.com/NVIDIA/gpu-operator/internal/consts"
)

// TODO: with multiple CRs of a kind, the tie-breaker is list order, which is not
// guaranteed. Resolve the singleton the reconcilers actually selected instead.
func resolveActiveConfig(ctx context.Context, c client.Client) (*gpuv1.ClusterPolicy, *nvidiav1alpha1.GPUCluster, error) {
	var clusterPolicy *gpuv1.ClusterPolicy
	clusterPolicies := &gpuv1.ClusterPolicyList{}
	if err := c.List(ctx, clusterPolicies); err != nil {
		return nil, nil, fmt.Errorf("failed to list ClusterPolicy: %w", err)
	}
	if len(clusterPolicies.Items) > 0 {
		clusterPolicy = &clusterPolicies.Items[0]
	}

	var gpuCluster *nvidiav1alpha1.GPUCluster
	gpuClusters := &nvidiav1alpha1.GPUClusterList{}
	if err := c.List(ctx, gpuClusters); err != nil {
		return nil, nil, fmt.Errorf("failed to list GPUCluster: %w", err)
	}
	if len(gpuClusters.Items) > 0 {
		gpuCluster = &gpuClusters.Items[0]
	}

	return clusterPolicy, gpuCluster, nil
}

// resolveDefaultMode returns the nvidia.com/gpu-operator.resource-allocation.mode value for a GPU node that
// does not have one yet. When exactly one configuration CR exists its stack wins;
// envDefaultMode (the validated DEFAULT_GPU_ALLOCATION_MODE operator environment variable)
// is consulted only when both CRs exist, defaulting to device-plugin when unset. Nodes
// already labeled are never touched, so changing DEFAULT_GPU_ALLOCATION_MODE only affects
// nodes labeled afterward.
func resolveDefaultMode(clusterPolicyExists, gpuClusterExists bool, envDefaultMode consts.GPUAllocationMode) consts.GPUAllocationMode {
	switch {
	case clusterPolicyExists && gpuClusterExists:
		if envDefaultMode == consts.GPUAllocationModeDRA {
			return consts.GPUAllocationModeDRA
		}
		return consts.GPUAllocationModeDevicePlugin
	case gpuClusterExists:
		return consts.GPUAllocationModeDRA
	case clusterPolicyExists:
		return consts.GPUAllocationModeDevicePlugin
	default:
		return ""
	}
}
