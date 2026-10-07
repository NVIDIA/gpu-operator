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

package clusterpolicy

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"time"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/NVIDIA/gpu-operator/internal/certification"
	"github.com/NVIDIA/gpu-operator/internal/certification/k8s"
)

type migStrategy string

const (
	migStrategySingle migStrategy = "single"
	migStrategyMixed  migStrategy = "mixed"

	migWorkloadPodName = "mig-workload-test"
)

func runMIGTest(
	ctx context.Context,
	clients *k8s.Clients,
	result *certification.TestResult,
	cfg certification.Config,
	strategy migStrategy,
) {
	timeout := cfg.ParseTimeout()

	// Pre-check: requires MIG-capable GPU
	migNodes, err := clients.ListNodes(ctx, MIGCapableLabel+"=true")
	if err != nil || len(migNodes.Items) == 0 {
		result.Skip("no MIG-capable GPUs detected (requires Ampere+ architecture)")
		return
	}
	result.AddDetail("found %d MIG-capable node(s)", len(migNodes.Items))

	// Pre-check for single strategy: skip on heterogeneous GPU nodes. GFD's
	// gpu.count label reflects GPUs of the labeled product type while node
	// capacity includes all physical GPUs. A mismatch means the node has
	// mixed GPU types and single strategy (which requires all GPUs in MIG
	// mode) cannot work. Only compare when MIG is disabled (config absent
	// or "all-disabled"), because active MIG inflates capacity with slices.
	if strategy == migStrategySingle {
		for _, node := range migNodes.Items {
			migConfig := node.Labels["nvidia.com/mig.config"]
			if migConfig != "" && migConfig != "all-disabled" {
				continue
			}
			gpuCountStr, ok := node.Labels["nvidia.com/gpu.count"]
			if !ok {
				result.Skip("node %s missing nvidia.com/gpu.count label; cannot verify GPU homogeneity for MIG single strategy", node.Name)
				return
			}
			gpuCount, _ := strconv.ParseInt(gpuCountStr, 10, 64)
			totalGPUs := node.Status.Capacity[corev1.ResourceName("nvidia.com/gpu")]
			if gpuCount > 0 && totalGPUs.Value() > gpuCount {
				result.Skip("node %s has %d total GPUs but %d of the labeled type; MIG single strategy requires all GPUs to be MIG-capable", node.Name, totalGPUs.Value(), gpuCount)
				return
			}
		}
	}

	// Step 1: Patch ClusterPolicy with mig.strategy
	result.AddDetail("patching ClusterPolicy with mig.strategy=%s", strategy)
	patchJSON := fmt.Sprintf(`[{"op": "replace", "path": "/spec/mig/strategy", "value": "%s"}]`, strategy)
	if err := clients.ModifyClusterPolicy(ctx, []byte(patchJSON)); err != nil {
		result.Fail("failed to patch ClusterPolicy: %v", err)
		return
	}

	// Step 2: Verify mig-manager daemonset is running
	result.AddDetail("waiting for mig-manager daemonset to be ready")
	if err := clients.WaitForPodReady(ctx, cfg.Namespace, MIGManagerAppLabel, timeout); err != nil {
		result.Fail("mig-manager pods not ready: %v", err)
		return
	}
	result.AddDetail("mig-manager daemonset is ready")

	// Step 3: Set mig.config label on MIG-capable nodes.
	// Mixed uses "all-balanced" (has device-filters, works on all GPUs).
	// Single selects a per-node profile from the auto-generated ConfigMap, picking
	// the first all-1g.* profile with more than one instance. Each node gets its
	// own profile to handle heterogeneous GPU types across nodes.
	migConfig := DefaultMIGConfigMixed
	for _, node := range migNodes.Items {
		nodeConfig := migConfig
		if strategy == migStrategySingle {
			nodeConfig = DefaultMIGConfigSingle // fallback
			if profile, err := clients.SelectSingleMIGProfileFromNode(ctx, cfg.Namespace, node.Name); err == nil {
				nodeConfig = profile
				result.AddDetail("node %s: auto-detected single profile %s from per-node configmap", node.Name, profile)
			} else {
				result.AddDetail("node %s: per-node mig-config not available: %v, falling back to static configmap", node.Name, err)
				pciDeviceID, err := clients.GetGPUPCIDeviceID(ctx, cfg.Namespace, node.Name)
				if err != nil {
					result.AddDetail("node %s: could not query MIG-capable GPU device ID: %v, using default %s", node.Name, err, nodeConfig)
				} else {
					result.AddDetail("node %s: detected MIG-capable GPU with PCI device ID: %s", node.Name, pciDeviceID)
					profile, err := clients.SelectSingleMIGProfile(ctx, cfg.Namespace, pciDeviceID)
					if err != nil {
						result.AddDetail("node %s: could not auto-detect single profile: %v, using default %s", node.Name, err, nodeConfig)
					} else {
						nodeConfig = profile
					}
				}
			}
		}
		labelPatch := `[{"op": "add", "path": "` + MIGConfigLabelPath + `", "value": "` + nodeConfig + `"}]`
		if _, err := clients.PatchNode(ctx, node.Name, []byte(labelPatch)); err != nil {
			result.Fail("failed to label node %s: %v", node.Name, err)
			return
		}
		result.AddDetail("labeled node %s with mig.config=%s", node.Name, nodeConfig)
	}

	// Defer cleanup: set mig.config to all-disabled and wait for completion
	defer func() {
		for _, node := range migNodes.Items {
			disablePatch := `[{"op": "replace", "path": "` + MIGConfigLabelPath + `", "value": "all-disabled"}]`
			_, _ = clients.PatchNode(ctx, node.Name, []byte(disablePatch))
		}
		// Wait for MIG disable to complete so the next test starts with a clean state
		_ = clients.WaitForCondition(ctx, timeout, func(ctx context.Context) (bool, error) {
			nodes, err := clients.ListNodes(ctx, MIGCapableLabel+"=true")
			if err != nil || len(nodes.Items) == 0 {
				return false, nil
			}
			for _, node := range nodes.Items {
				state := node.Labels["nvidia.com/mig.config.state"]
				if state != "success" {
					return false, nil
				}
				config := node.Labels["nvidia.com/mig.config"]
				if config != "all-disabled" {
					return false, nil
				}
			}
			return true, nil
		})
		// Wait for operands to stabilize after MIG reconfig. The operator replaces
		// pods after MIG mode changes; without this, the next TestSet could see
		// stale "Ready" pods that are about to be terminated.
		for _, operand := range []string{DevicePluginAppLabel, GFDAppLabel, ValidatorAppLabel} {
			_ = clients.WaitForPodReady(ctx, cfg.Namespace, operand, timeout)
		}
	}()

	// Step 4: Wait for MIG config state to leave "success" first (avoid stale state).
	// Use a short timeout: if the mig-manager doesn't react within 2m the config
	// may already be applied from a previous run.
	result.AddDetail("waiting for MIG reconfiguration to begin")
	transitionErr := clients.WaitForCondition(ctx, 2*time.Minute, func(ctx context.Context) (bool, error) {
		nodes, err := clients.ListNodes(ctx, MIGCapableLabel+"=true")
		if err != nil {
			return false, nil
		}
		for _, node := range nodes.Items {
			state := node.Labels["nvidia.com/mig.config.state"]
			if state != "success" {
				return true, nil // State changed, MIG manager is processing
			}
		}
		return false, nil
	})
	if transitionErr != nil {
		result.AddDetail("MIG config may already be applied, skipping transition wait")
	}

	// Now wait for MIG configuration to complete and resources to appear.
	// Detect permanent failure early so we don't burn the full timeout.
	err = clients.WaitForCondition(ctx, timeout, func(ctx context.Context) (bool, error) {
		nodes, err := clients.ListNodes(ctx, MIGCapableLabel+"=true")
		if err != nil || len(nodes.Items) == 0 {
			return false, nil
		}

		for _, node := range nodes.Items {
			configState := node.Labels["nvidia.com/mig.config.state"]
			if configState == "failed" {
				return false, fmt.Errorf("MIG configuration failed on node %s", node.Name)
			}
			if configState != "success" {
				return false, nil
			}

			if !verifyMIGResources(node, strategy) {
				return false, nil
			}
		}
		return true, nil
	})
	if err != nil {
		result.Fail("MIG configuration did not complete: %v", err)
		return
	}

	// Log allocatable MIG resources
	logMIGResources(ctx, clients, result, strategy)

	result.AddDetail("MIG %s strategy configuration successful", strategy)

	// Step 5: Deploy workload to verify MIG GPU access.
	// Re-fetch nodes to get current allocatable resources after MIG reconfiguration.
	migNodes, err = clients.ListNodes(ctx, MIGCapableLabel+"=true")
	if err != nil {
		result.Fail("failed to list MIG nodes: %v", err)
		return
	}
	var resourceName string
	switch strategy {
	case migStrategySingle:
		resourceName = "nvidia.com/gpu"
	case migStrategyMixed:
		for _, node := range migNodes.Items {
			for resName, qty := range node.Status.Allocatable {
				if strings.HasPrefix(string(resName), "nvidia.com/mig-") && qty.Value() > 0 {
					resourceName = string(resName)
					break
				}
			}
			if resourceName != "" {
				break
			}
		}
	}
	if resourceName == "" {
		result.Fail("no MIG GPU resource found for workload")
		return
	}

	result.AddDetail("deploying MIG workload pod requesting %s", resourceName)
	_ = clients.DeletePod(ctx, cfg.Namespace, migWorkloadPodName)

	pod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      migWorkloadPodName,
			Namespace: cfg.Namespace,
		},
		Spec: corev1.PodSpec{
			RestartPolicy: corev1.RestartPolicyNever,
			Containers: []corev1.Container{{
				Name:  "cuda-test",
				Image: cfg.TestWorkloadImage,
				Resources: corev1.ResourceRequirements{
					Limits: corev1.ResourceList{
						corev1.ResourceName(resourceName): resource.MustParse("1"),
					},
				},
			}},
		},
	}

	if err := clients.CreatePod(ctx, cfg.Namespace, pod); err != nil {
		result.Fail("failed to create MIG workload pod: %v", err)
		return
	}
	defer func() { _ = clients.DeletePod(ctx, cfg.Namespace, migWorkloadPodName) }()

	succeeded, err := clients.WaitForPodComplete(ctx, cfg.Namespace, migWorkloadPodName, timeout)
	if err != nil {
		result.Fail("MIG workload pod did not complete: %v", err)
		return
	}
	if !succeeded {
		result.Fail("MIG workload pod failed")
		return
	}
	result.AddDetail("MIG workload completed successfully with resource %s", resourceName)
}

func verifyMIGResources(node corev1.Node, strategy migStrategy) bool {
	switch strategy {
	case migStrategySingle:
		gpuQty, hasGPU := node.Status.Allocatable[corev1.ResourceName("nvidia.com/gpu")]
		// MIG single exposes all slices as nvidia.com/gpu. With a 1g profile
		// on most GPUs this means 4-7 slices, so check for > 1.
		return hasGPU && gpuQty.Value() > 1
	case migStrategyMixed:
		// Mixed strategy must expose multiple distinct nvidia.com/mig-* resource
		// types with non-zero quantities (e.g. mig-1g.10gb: 2, mig-2g.20gb: 1).
		// Zero-valued entries may linger from previous configurations.
		var migTypes int
		for resName, qty := range node.Status.Allocatable {
			if strings.HasPrefix(string(resName), "nvidia.com/mig-") && !qty.IsZero() {
				migTypes++
			}
		}
		return migTypes > 1
	}
	return false
}

func logMIGResources(ctx context.Context, clients *k8s.Clients, result *certification.TestResult, strategy migStrategy) {
	prefix := "nvidia.com/gpu"
	if strategy == migStrategyMixed {
		prefix = "nvidia.com/mig-"
	}

	nodes, err := clients.ListNodes(ctx, MIGCapableLabel+"=true")
	if err != nil {
		return
	}
	for _, node := range nodes.Items {
		for resName, qty := range node.Status.Allocatable {
			if strings.HasPrefix(string(resName), prefix) {
				result.AddDetail("node %s: %s=%s", node.Name, resName, qty.String())
			}
		}
	}
}
