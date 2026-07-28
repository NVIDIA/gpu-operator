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

// getSingletonClusterPolicy returns the ClusterPolicy treated as the cluster-wide
// singleton: oldest creationTimestamp first, lowest name as the tie-breaker. This
// selection is a heuristic: ideally the operator would hold a reference to the
// singleton ClusterPolicy globally throughout its lifetime, and until it does,
// picking the oldest is the next-best approach. Unlike a first-reconciled-wins claim
// held in controller memory, the result is stable across operator restarts and
// derivable by every controller. Returns nil for an empty list.
// GPUCluster needs no selection: its CRD pins metadata.name, enforcing the singleton
// at admission.
func getSingletonClusterPolicy(items []gpuv1.ClusterPolicy) *gpuv1.ClusterPolicy {
	var active *gpuv1.ClusterPolicy
	for i := range items {
		candidate := &items[i]
		if active == nil {
			active = candidate
			continue
		}
		candidateCreated, activeCreated := candidate.CreationTimestamp, active.CreationTimestamp
		if candidateCreated.Before(&activeCreated) ||
			(candidateCreated.Equal(&activeCreated) && candidate.Name < active.Name) {
			active = candidate
		}
	}
	return active
}

// resolveActiveConfig returns the active ClusterPolicy, selected with the same
// getSingletonClusterPolicy rule the ClusterPolicy controller uses, and the GPUCluster.
func resolveActiveConfig(ctx context.Context, c client.Reader) (*gpuv1.ClusterPolicy, *nvidiav1alpha1.GPUCluster, error) {
	clusterPolicies := &gpuv1.ClusterPolicyList{}
	if err := c.List(ctx, clusterPolicies); err != nil {
		return nil, nil, fmt.Errorf("failed to list ClusterPolicy: %w", err)
	}

	var gpuCluster *nvidiav1alpha1.GPUCluster
	gpuClusters := &nvidiav1alpha1.GPUClusterList{}
	if err := c.List(ctx, gpuClusters); err != nil {
		return nil, nil, fmt.Errorf("failed to list GPUCluster: %w", err)
	}
	if len(gpuClusters.Items) > 0 {
		gpuCluster = &gpuClusters.Items[0]
	}

	return getSingletonClusterPolicy(clusterPolicies.Items), gpuCluster, nil
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
