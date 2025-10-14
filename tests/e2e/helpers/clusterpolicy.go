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

package helpers

import (
	"context"
	"time"

	nvidiav1 "github.com/NVIDIA/gpu-operator/api/nvidia/v1"
	gpuclientset "github.com/NVIDIA/gpu-operator/api/versioned"
	"github.com/NVIDIA/gpu-operator/internal/conditions"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/client-go/util/retry"
	"k8s.io/utils/ptr"
)

type ClusterPolicyClient struct {
	client gpuclientset.Interface
}

func NewClusterPolicyClient(client gpuclientset.Interface) *ClusterPolicyClient {
	return &ClusterPolicyClient{
		client: client,
	}
}

func (h *ClusterPolicyClient) Get(ctx context.Context, name string) (*nvidiav1.ClusterPolicy, error) {
	return h.client.NvidiaV1().ClusterPolicies().Get(ctx, name, metav1.GetOptions{})
}

func (h *ClusterPolicyClient) Update(ctx context.Context, cp *nvidiav1.ClusterPolicy) (*nvidiav1.ClusterPolicy, error) {
	return h.client.NvidiaV1().ClusterPolicies().Update(ctx, cp, metav1.UpdateOptions{})
}

// modify applies a mutation function to a ClusterPolicy and persists the changes.
// It uses RetryOnConflict to handle concurrent modifications by the operator controller.
func (h *ClusterPolicyClient) modify(ctx context.Context, name string, mutate func(*nvidiav1.ClusterPolicy)) error {
	return retry.RetryOnConflict(retry.DefaultBackoff, func() error {
		clusterPolicy, err := h.Get(ctx, name)
		if err != nil {
			return err
		}

		mutate(clusterPolicy)

		_, err = h.Update(ctx, clusterPolicy)
		return err
	})
}

func (h *ClusterPolicyClient) UpdateDriverVersion(ctx context.Context, name, version string) error {
	return h.modify(ctx, name, func(clusterPolicy *nvidiav1.ClusterPolicy) {
		clusterPolicy.Spec.Driver.Version = version
	})
}

func (h *ClusterPolicyClient) EnableDCGM(ctx context.Context, name string) error {
	return h.modify(ctx, name, func(clusterPolicy *nvidiav1.ClusterPolicy) {
		clusterPolicy.Spec.DCGM.Enabled = ptr.To(true)
	})
}

func (h *ClusterPolicyClient) DisableDCGM(ctx context.Context, name string) error {
	return h.modify(ctx, name, func(clusterPolicy *nvidiav1.ClusterPolicy) {
		clusterPolicy.Spec.DCGM.Enabled = ptr.To(false)
	})
}

func (h *ClusterPolicyClient) EnableDCGMExporter(ctx context.Context, name string) error {
	return h.modify(ctx, name, func(clusterPolicy *nvidiav1.ClusterPolicy) {
		clusterPolicy.Spec.DCGMExporter.Enabled = ptr.To(true)
	})
}

func (h *ClusterPolicyClient) DisableDCGMExporter(ctx context.Context, name string) error {
	return h.modify(ctx, name, func(clusterPolicy *nvidiav1.ClusterPolicy) {
		clusterPolicy.Spec.DCGMExporter.Enabled = ptr.To(false)
	})
}

func (h *ClusterPolicyClient) EnableGFD(ctx context.Context, name string) error {
	return h.modify(ctx, name, func(clusterPolicy *nvidiav1.ClusterPolicy) {
		clusterPolicy.Spec.GPUFeatureDiscovery.Enabled = ptr.To(true)
	})
}

func (h *ClusterPolicyClient) DisableGFD(ctx context.Context, name string) error {
	return h.modify(ctx, name, func(clusterPolicy *nvidiav1.ClusterPolicy) {
		clusterPolicy.Spec.GPUFeatureDiscovery.Enabled = ptr.To(false)
	})
}

func (h *ClusterPolicyClient) SetMIGStrategy(ctx context.Context, name, strategy string) error {
	return h.modify(ctx, name, func(clusterPolicy *nvidiav1.ClusterPolicy) {
		clusterPolicy.Spec.MIG.Strategy = nvidiav1.MIGStrategy(strategy)
	})
}

func (h *ClusterPolicyClient) WaitForReady(ctx context.Context, name string, timeout time.Duration) error {
	return wait.PollUntilContextTimeout(ctx, defaultPollingInterval, timeout, true, func(ctx context.Context) (bool, error) {
		clusterPolicy, err := h.Get(ctx, name)
		if err != nil {
			return false, err
		}

		for _, condition := range clusterPolicy.Status.Conditions {
			if condition.Type == conditions.Ready && condition.Status == metav1.ConditionTrue {
				return true, nil
			}
		}

		return false, nil
	})
}

