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
	"encoding/json"
	"fmt"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/NVIDIA/gpu-operator/internal/certification"
	"github.com/NVIDIA/gpu-operator/internal/certification/k8s"
)

const (
	timeslicingConfigMapName    = "time-slicing-config-test"
	timeslicingConfigKey        = "any-gpu" // Generic key - works for any GPU type
	timeslicingDeployName       = "time-slicing-verification"
	timeslicingReplicas         = 5
	devicePluginConfigLabelPath = "/metadata/labels/nvidia.com~1device-plugin.config"
	gpuSharedResource           = "nvidia.com/gpu.shared"
)

type GPUSharing struct{}

func (t *GPUSharing) Name() string {
	return "clusterpolicy-gpu-sharing"
}

func (t *GPUSharing) Run(ctx context.Context, clients *k8s.Clients, cfg certification.Config) *certification.TestResult {
	result := certification.NewTestResult(t.Name())
	defer result.Finalize()

	timeout := cfg.ParseTimeout()

	// Step 1: Create time-slicing ConfigMap
	// The ConfigMap key name must match what we set in the node label
	configData := map[string]interface{}{
		"version": "v1",
		"sharing": map[string]interface{}{
			"timeSlicing": map[string]interface{}{
				"renameByDefault": true,
				"resources": []map[string]interface{}{
					{
						"name":     "nvidia.com/gpu",
						"replicas": timeslicingReplicas,
					},
				},
			},
		},
	}

	configJSON, err := json.Marshal(configData)
	if err != nil {
		result.Fail("failed to marshal time-slicing config: %v", err)
		return result
	}

	result.AddDetail("creating time-slicing ConfigMap")
	if err := clients.CreateOrUpdateConfigMap(ctx, cfg.Namespace, timeslicingConfigMapName, map[string]string{
		timeslicingConfigKey: string(configJSON),
	}); err != nil {
		result.Fail("failed to create ConfigMap: %v", err)
		return result
	}
	defer func() {
		_ = clients.DeleteConfigMap(ctx, cfg.Namespace, timeslicingConfigMapName)
	}()

	// Step 2: Patch ClusterPolicy to use the ConfigMap
	patchJSON := fmt.Sprintf(`[{"op": "replace", "path": "/spec/devicePlugin/config", "value": {"name": "%s"}}]`, timeslicingConfigMapName)

	result.AddDetail("patching ClusterPolicy with devicePlugin config")
	if err := clients.ModifyClusterPolicy(ctx, []byte(patchJSON)); err != nil {
		result.Fail("failed to patch ClusterPolicy: %v", err)
		return result
	}

	// Defer cleanup: reset devicePlugin config
	defer func() {
		resetPatch := `[{"op": "remove", "path": "/spec/devicePlugin/config/name"}]`
		_ = clients.ModifyClusterPolicy(ctx, []byte(resetPatch))
	}()

	// Step 3: Label GPU nodes to select the timeslicing config
	// The config-manager sidecar reads this label to select which config to use
	result.AddDetail("labeling GPU nodes to select timeslicing config")
	gpuNodes, err := clients.GetGPUNodes(ctx)
	if err != nil {
		result.Fail("failed to get GPU nodes: %v", err)
		return result
	}

	for _, nodeName := range gpuNodes {
		labelPatch := fmt.Sprintf(`[{"op": "add", "path": "%s", "value": "%s"}]`, devicePluginConfigLabelPath, timeslicingConfigKey)
		if _, err := clients.PatchNode(ctx, nodeName, []byte(labelPatch)); err != nil {
			result.Fail("failed to label node %s: %v", nodeName, err)
			return result
		}
		result.AddDetail("labeled node %s for timeslicing config", nodeName)
	}

	// Defer cleanup: remove node labels
	defer func() {
		for _, nodeName := range gpuNodes {
			removePatch := fmt.Sprintf(`[{"op": "remove", "path": "%s"}]`, devicePluginConfigLabelPath)
			_, _ = clients.PatchNode(ctx, nodeName, []byte(removePatch))
		}
	}()

	// Step 4: Wait for device-plugin pods to restart and be ready
	result.AddDetail("waiting for device-plugin pods to restart")
	if err := clients.WaitForPodsReady(ctx, cfg.Namespace, "app="+DevicePluginAppLabel, timeout); err != nil {
		result.Fail("device-plugin pods not ready: %v", err)
		return result
	}

	// Step 5: Verify GFD labels show replicas=5 and sharing-strategy=time-slicing
	result.AddDetail("verifying GFD labels for time-slicing")

	expectedReplicas := fmt.Sprintf("%d", timeslicingReplicas)
	err = clients.WaitForCondition(ctx, timeout, func(ctx context.Context) (bool, error) {
		nodes, err := clients.ListNodes(ctx, k8s.GPUNodeSelector)
		if err != nil {
			return false, nil
		}

		for _, node := range nodes.Items {
			replicas, hasReplicas := node.Labels["nvidia.com/gpu.replicas"]
			strategy, hasStrategy := node.Labels["nvidia.com/gpu.sharing-strategy"]

			if !hasReplicas || replicas != expectedReplicas || !hasStrategy || strategy != "time-slicing" {
				return false, nil
			}
		}
		return true, nil
	})
	if err != nil {
		result.Fail("GFD labels not updated for time-slicing: %v", err)
		return result
	}
	result.AddDetail("GFD labels verified: replicas=%d, sharing-strategy=time-slicing", timeslicingReplicas)

	// Step 6: Verify allocatable resources before deploying workload
	result.AddDetail("waiting for %s to be allocatable", gpuSharedResource)
	err = clients.WaitForCondition(ctx, timeout, func(ctx context.Context) (bool, error) {
		nodes, err := clients.ListNodes(ctx, k8s.GPUNodeSelector)
		if err != nil {
			return false, nil
		}
		for _, node := range nodes.Items {
			if qty, ok := node.Status.Allocatable[corev1.ResourceName(gpuSharedResource)]; ok && !qty.IsZero() {
				return true, nil
			}
		}
		return false, nil
	})
	if err != nil {
		result.Fail("%s not allocatable on any node: %v", gpuSharedResource, err)
		return result
	}
	result.AddDetail("%s is allocatable", gpuSharedResource)

	// Step 7: Deploy test workload with replicas matching timeslicing config
	result.AddDetail("deploying test workload with %d replicas", timeslicingReplicas)
	replicas := int32(timeslicingReplicas)
	deployment := &appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{
			Name:      timeslicingDeployName,
			Namespace: cfg.Namespace,
		},
		Spec: appsv1.DeploymentSpec{
			Replicas: &replicas,
			Selector: &metav1.LabelSelector{
				MatchLabels: map[string]string{"app": timeslicingDeployName},
			},
			Template: corev1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{
					Labels: map[string]string{"app": timeslicingDeployName},
				},
				Spec: corev1.PodSpec{
					Containers: []corev1.Container{
						{
							Name:    "cuda-test",
							Image:   cfg.TestWorkloadImage,
							Command: []string{"sleep", "infinity"},
							Resources: corev1.ResourceRequirements{
								Limits: corev1.ResourceList{
									corev1.ResourceName(gpuSharedResource): resource.MustParse("1"),
								},
							},
						},
					},
				},
			},
		},
	}

	if err := clients.CreateDeployment(ctx, cfg.Namespace, deployment); err != nil {
		result.Fail("failed to create test deployment: %v", err)
		return result
	}
	defer func() {
		_ = clients.DeleteDeployment(ctx, cfg.Namespace, timeslicingDeployName)
	}()

	// Step 8: Wait for deployment to be available
	result.AddDetail("waiting for test deployment to be available")
	if err := clients.WaitForDeploymentAvailable(ctx, cfg.Namespace, timeslicingDeployName, timeout); err != nil {
		result.Fail("test deployment not available: %v", err)
		return result
	}

	result.AddDetail("time-slicing test deployment with %d replicas is running", timeslicingReplicas)
	return result
}
