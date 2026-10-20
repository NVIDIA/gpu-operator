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

package baseline

import (
	"context"

	corev1 "k8s.io/api/core/v1"

	"github.com/NVIDIA/gpu-operator/internal/certification"
	"github.com/NVIDIA/gpu-operator/internal/certification/k8s"
)

const gpuResourceName = corev1.ResourceName("nvidia.com/gpu")

type Allocatable struct{}

func (a *Allocatable) Name() string {
	return "gpu-allocatable"
}

func (a *Allocatable) Run(ctx context.Context, clients *k8s.Clients, cfg certification.Config) *certification.TestResult {
	result := certification.NewTestResult(a.Name())
	defer result.Finalize()

	gpuNodes, err := clients.GetGPUNodes(ctx)
	if err != nil {
		result.Fail("failed to get GPU nodes: %v", err)
		return result
	}

	if len(gpuNodes) == 0 {
		result.Fail("no GPU nodes found")
		return result
	}

	result.AddDetail("found %d GPU node(s)", len(gpuNodes))

	for _, nodeName := range gpuNodes {
		node, err := clients.GetNode(ctx, nodeName)
		if err != nil {
			result.Fail("failed to get node %s: %v", nodeName, err)
			return result
		}

		gpuQuantity, exists := node.Status.Allocatable[gpuResourceName]
		if !exists {
			result.Fail("node %s: %s not found in allocatable resources", nodeName, gpuResourceName)
			return result
		}

		gpuCount := gpuQuantity.Value()
		if gpuCount <= 0 {
			result.Fail("node %s: %s allocatable is %d (expected > 0)", nodeName, gpuResourceName, gpuCount)
			return result
		}

		result.AddDetail("node %s: %s=%d allocatable", nodeName, gpuResourceName, gpuCount)
	}

	return result
}
