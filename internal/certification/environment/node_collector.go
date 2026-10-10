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

package environment

import (
	"context"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"time"

	corev1 "k8s.io/api/core/v1"

	"github.com/NVIDIA/gpu-operator/internal/certification/k8s"
)

type nodeCollector struct{}

func (c *nodeCollector) Name() string { return "nodes" }

func (c *nodeCollector) Collect(ctx context.Context, clients *k8s.Clients, namespace string, s *EnvironmentSnapshot) error {
	nodes, err := clients.ListNodes(ctx, "")
	if err != nil {
		return err
	}

	for _, node := range nodes.Items {
		info := c.extractNodeInfo(&node)

		// Collect recent events for this node
		events, err := clients.ListNodeEvents(ctx, node.Name)
		if err == nil {
			for _, event := range events.Items {
				info.Events = append(info.Events, EventInfo{
					Type:     event.Type,
					Reason:   event.Reason,
					Message:  event.Message,
					Count:    event.Count,
					LastSeen: event.LastTimestamp.Format("2006-01-02T15:04:05Z"),
				})
			}
		}

		s.Nodes = append(s.Nodes, info)
	}

	return nil
}

func (c *nodeCollector) extractNodeInfo(node *corev1.Node) NodeInfo {
	labels := node.Labels
	info := NodeInfo{
		Name:   node.Name,
		Status: nodeStatus(node),
		Roles:  nodeRoles(node),
		Age:    formatAge(time.Since(node.CreationTimestamp.Time)),

		// System info
		KubeletVersion:   node.Status.NodeInfo.KubeletVersion,
		ContainerRuntime: node.Status.NodeInfo.ContainerRuntimeVersion,
		OSImage:          node.Status.NodeInfo.OSImage,
		KernelVersion:    node.Status.NodeInfo.KernelVersion,
		Architecture:     node.Status.NodeInfo.Architecture,
		OperatingSystem:  node.Status.NodeInfo.OperatingSystem,

		// Addresses
		InternalIP: getNodeAddress(node, corev1.NodeInternalIP),
		ExternalIP: getNodeAddress(node, corev1.NodeExternalIP),
		Hostname:   getNodeAddress(node, corev1.NodeHostName),

		// GPU hardware
		GPUProduct:             labels["nvidia.com/gpu.product"],
		GPUCount:               labelInt(labels, "nvidia.com/gpu.count"),
		GPUMemoryMB:            labelInt(labels, "nvidia.com/gpu.memory"),
		GPUFamily:              labels["nvidia.com/gpu.family"],
		GPUMachine:             labels["nvidia.com/gpu.machine"],
		ComputeCapabilityMajor: labelInt(labels, "nvidia.com/gpu.compute.major"),
		ComputeCapabilityMinor: labelInt(labels, "nvidia.com/gpu.compute.minor"),

		// Driver and CUDA versions
		DriverVersion: labels["nvidia.com/cuda.driver-version.full"],
		CUDAVersion:   labels["nvidia.com/cuda.runtime-version.full"],

		// MIG
		MIGCapable:  labelBool(labels, "nvidia.com/mig.capable"),
		MIGStrategy: labels["nvidia.com/mig.strategy"],

		// MPS
		MPSCapable: labelBool(labels, "nvidia.com/mps.capable"),

		// Sharing
		SharingStrategy: labels["nvidia.com/gpu.sharing-strategy"],
		GPUReplicas:     labelInt(labels, "nvidia.com/gpu.replicas"),

		// vGPU
		VGPUPresent: labelBool(labels, "nvidia.com/vgpu.present"),

		// GFD timestamp
		GFDTimestamp: labels["nvidia.com/gfd.timestamp"],
	}

	// Resources
	if gpuQty, ok := node.Status.Allocatable["nvidia.com/gpu"]; ok {
		info.AllocatableGPUs = int(gpuQty.Value())
	}
	if gpuQty, ok := node.Status.Capacity["nvidia.com/gpu"]; ok {
		info.CapacityGPUs = int(gpuQty.Value())
	}

	// Taints
	for _, taint := range node.Spec.Taints {
		info.Taints = append(info.Taints, TaintInfo{
			Key:    taint.Key,
			Value:  taint.Value,
			Effect: string(taint.Effect),
		})
		if taint.Key == "nvidia.com/gpu" {
			info.HasGPUTaint = true
		}
	}

	// Conditions
	info.Conditions = make(map[string]string)
	for _, cond := range node.Status.Conditions {
		info.Conditions[string(cond.Type)] = string(cond.Status)
	}

	// Deploy states
	info.DeployStates = make(map[string]string)
	for k, v := range labels {
		if key, ok := strings.CutPrefix(k, "nvidia.com/gpu.deploy."); ok {
			info.DeployStates[key] = v
		}
	}

	return info
}

// labelInt parses an integer from a label value.
func labelInt(labels map[string]string, key string) int {
	if v, ok := labels[key]; ok {
		if i, err := strconv.Atoi(v); err == nil {
			return i
		}
	}
	return 0
}

// labelBool parses a boolean from a label value.
func labelBool(labels map[string]string, key string) bool {
	if v, ok := labels[key]; ok {
		return v == "true"
	}
	return false
}

// getNodeAddress extracts an address of the given type from the node.
func getNodeAddress(node *corev1.Node, addrType corev1.NodeAddressType) string {
	for _, addr := range node.Status.Addresses {
		if addr.Type == addrType {
			return addr.Address
		}
	}
	return ""
}

// nodeStatus returns "Ready" or "NotReady" based on the node's Ready condition.
func nodeStatus(node *corev1.Node) string {
	for _, cond := range node.Status.Conditions {
		if cond.Type == corev1.NodeReady {
			if cond.Status == corev1.ConditionTrue {
				return "Ready"
			}
			return "NotReady"
		}
	}
	return "Unknown"
}

// nodeRoles extracts roles from node-role.kubernetes.io/* labels.
func nodeRoles(node *corev1.Node) string {
	var roles []string
	for k := range node.Labels {
		if role, ok := strings.CutPrefix(k, "node-role.kubernetes.io/"); ok {
			if role != "" {
				roles = append(roles, role)
			}
		}
	}
	sort.Strings(roles)
	if len(roles) == 0 {
		return "<none>"
	}
	return strings.Join(roles, ",")
}

// formatAge formats a duration into a human-readable age string.
func formatAge(d time.Duration) string {
	switch {
	case d >= 24*time.Hour:
		return fmt.Sprintf("%dd", int(d.Hours()/24))
	case d >= time.Hour:
		return fmt.Sprintf("%dh", int(d.Hours()))
	default:
		return fmt.Sprintf("%dm", int(d.Minutes()))
	}
}
