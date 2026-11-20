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
	"maps"

	corev1 "k8s.io/api/core/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"

	gpuv1 "github.com/NVIDIA/gpu-operator/api/nvidia/v1"
	gpu "github.com/NVIDIA/gpu-operator/api/nvidia/v1alpha1"
	"github.com/NVIDIA/gpu-operator/internal/coexistence"
)

const devicePluginDeployLabelKey = "nvidia.com/gpu.deploy.device-plugin"

func annotationChanged(a, b client.Object, key string) bool {
	av, ap := a.GetAnnotations()[key]
	bv, bp := b.GetAnnotations()[key]
	return av != bv || ap != bp
}

func allocationEligibility(cp *gpuv1.ClusterPolicy, gc *gpu.GPUCluster) (bool, bool) {
	dp := cp != nil && cp.DeletionTimestamp.IsZero() && controllerutil.ContainsFinalizer(cp, nodeCleanupFinalizer)
	dra := gc != nil && gc.DeletionTimestamp.IsZero() && controllerutil.ContainsFinalizer(gc, nodeCleanupFinalizer)
	if cp != nil && gc != nil {
		if !cp.CreationTimestamp.Before(&gc.CreationTimestamp) {
			return false, false
		}
		// A deleting CP no longer gates surviving DRA activation. Its nodes remain
		// reserved separately until that node's cleanup has been observed.
		if cp.DeletionTimestamp.IsZero() {
			independentDriver := !cp.Spec.Driver.IsEnabled() || cp.Spec.Driver.UseNvidiaDriverCRDType()
			dra = dra && independentDriver && nodeSelectionInitializedTrue(cp)
		}
	}
	return dp, dra
}

func nodeSelection(node *corev1.Node, cp, gc bool, defaultStack string) (string, error) {
	mode, explicit := node.Labels[coexistence.ModeLabel]
	record, present := node.Annotations[coexistence.SelectionAnnotation]
	if present && record == "" && !explicit {
		return "", fmt.Errorf("empty allocator selection annotation")
	}
	return coexistence.Select(mode, explicit, record, cp, gc, defaultStack)
}

func (nlc *nodeLabelingController) nodeReserved(node *corev1.Node) bool {
	return (nlc.terminatingDP && hasStackMetadata(node, coexistence.DP)) ||
		(nlc.terminatingDRA && hasStackMetadata(node, coexistence.DRA))
}

func (nlc *nodeLabelingController) selectNode(ctx context.Context, node *corev1.Node) (bool, error) {
	// Reserve even when discovery or customer mode changes during teardown.
	if nlc.nodeReserved(node) {
		return false, nil
	}
	before := node.DeepCopy()
	labels := node.Labels
	if !hasCommonGPULabel(labels) {
		return removeAllGPUStateLabels(labels), nil
	}
	dp, dra := allocationEligibility(nlc.clusterPolicy, nlc.gpuCluster)
	cpPresent := nlc.clusterPolicy != nil && nlc.clusterPolicy.DeletionTimestamp.IsZero()
	gcPresent := nlc.gpuCluster != nil && nlc.gpuCluster.DeletionTimestamp.IsZero()
	selected, err := nodeSelection(node, cpPresent, gcPresent, nlc.defaultStack)
	// During an initial upgrade, running operands can predate the cleanup
	// finalizer. Do not evict them merely because the source controller has not
	// observed and protected that CR yet. Fresh nodes remain unrecorded.
	if err == nil {
		var selectedSource client.Object
		if selected == coexistence.DP && cpPresent {
			selectedSource = nlc.clusterPolicy
		}
		if selected == coexistence.DRA && gcPresent {
			selectedSource = nlc.gpuCluster
		}
		if selectedSource != nil && !controllerutil.ContainsFinalizer(selectedSource, nodeCleanupFinalizer) {
			return false, nil
		}
	}

	record := node.Annotations[coexistence.SelectionAnnotation]
	// A source that has disappeared cannot preserve an automatic choice forever.
	// Normal deletion clears it earlier, while the source finalizer is retained.
	if (record == coexistence.DP && !cpPresent) || (record == coexistence.DRA && !gcPresent) {
		delete(node.Annotations, coexistence.SelectionAnnotation)
	}
	eligible := (selected == coexistence.DP && dp) || (selected == coexistence.DRA && dra)
	switch {
	case err != nil || !eligible:
		nlc.withdrawPlacement(ctx, labels, node.Name)
		if err == nil && selected != "" {
			nlc.logger.Info("WaitingForStack", "node", node.Name, "stack", selected)
		}
	case selected == coexistence.DP && labels[draDriverDeployLabelKey] != "" && nlc.nodeHasDRAClaimPods(ctx, node.Name):
		// Drain claim consumers, retaining only the DRA plugin. Never enable DP in
		// the same patch that retains a DRA plugin for unprepare.
		nlc.withdrawPlacement(ctx, labels, node.Name)
	default:
		if node.Annotations == nil {
			node.Annotations = map[string]string{}
		}
		node.Annotations[coexistence.SelectionAnnotation] = selected
		saved := nlc.gpuCluster
		if selected == coexistence.DP {
			nlc.gpuCluster = nil
		}
		nlc.updateGPUStateLabels(ctx, labels, node.Name)
		nlc.gpuCluster = saved
	}
	return !maps.Equal(before.Labels, node.Labels) || !maps.Equal(before.Annotations, node.Annotations), err
}

func (nlc *nodeLabelingController) withdrawPlacement(ctx context.Context, labels map[string]string, nodeName string) {
	driver, driverOK := labels[driverDeployLabelKey]
	vgpu, vgpuOK := labels[vgpuManagerDeployLabelKey]
	plugin, pluginOK := labels[draDriverDeployLabelKey]
	removeAllGPUStateLabels(labels)
	if driverOK {
		labels[driverDeployLabelKey] = driver
	}
	if vgpuOK {
		labels[vgpuManagerDeployLabelKey] = vgpu
	}
	if pluginOK && plugin != "" && nlc.nodeHasDRAClaimPods(ctx, nodeName) {
		labels[draDriverDeployLabelKey] = plugin
		nlc.draPluginRemovalDeferred = true
	}
}

// sourceAdoptionPending ignores intentional waits and component pause values.
// It requires observing the record and compatible scheduling metadata, not ready Pods.
func sourceAdoptionPending(ctx context.Context, c client.Client, source client.Object) (bool, error) {
	cp, gc, err := resolveActiveConfig(ctx, c)
	if err != nil {
		return false, err
	}
	defaultStack, err := coexistence.DefaultStack()
	if err != nil {
		return false, err
	}
	dp, dra := allocationEligibility(cp, gc)
	nodes := &corev1.NodeList{}
	if err := c.List(ctx, nodes); err != nil {
		return false, err
	}
	target := sourceStack(source)
	nlc := &nodeLabelingController{terminatingDP: cp != nil && !cp.DeletionTimestamp.IsZero(), terminatingDRA: gc != nil && !gc.DeletionTimestamp.IsZero()}
	for i := range nodes.Items {
		node := &nodes.Items[i]
		// A source's DaemonSets can also match stale labels on nodes selected
		// for another stack (or no longer discovered). Check those before
		// excluding nodes that do not need adoption by this source.
		mode, explicit := node.Labels[coexistence.ModeLabel]
		modeAllowsTarget := !explicit || mode == target
		if !hasGPULabels(node.Labels) || nlc.nodeReserved(node) {
			if modeAllowsTarget && hasEnabledStackLabel(node, target) {
				return true, nil
			}
			continue
		}
		selected, err := nodeSelection(node, cp != nil && cp.DeletionTimestamp.IsZero(), gc != nil && gc.DeletionTimestamp.IsZero(), defaultStack)
		if err != nil {
			return true, fmt.Errorf("node %s: %w", node.Name, err)
		}
		if selected != target || (selected == coexistence.DP && !dp) || (selected == coexistence.DRA && !dra) {
			if modeAllowsTarget && hasEnabledStackLabel(node, target) {
				return true, nil
			}
			continue
		}
		if node.Annotations[coexistence.SelectionAnnotation] != selected || !hasCommonGPULabel(node.Labels) {
			return true, nil
		}
		other := coexistence.DP
		if selected == coexistence.DP {
			other = coexistence.DRA
		}
		for key := range stackExclusiveKeys(other) {
			if _, exists := node.Labels[key]; exists {
				return true, nil
			}
		}
		// Component=false and CP operands=false are valid initialized states.
		if selected == coexistence.DP && hasOperandsDisabled(node.Labels) {
			continue
		}
		keys := gpuClusterStateLabels
		if selected == coexistence.DP {
			config, _ := getWorkloadConfig(node.Labels, cp.Spec.SandboxWorkloads.IsEnabled())
			keys = getEffectiveStateLabels(config, cp.Spec.SandboxWorkloads.Mode)
		}
		for key := range keys {
			if node.Labels[key] == "" {
				return true, nil
			}
		}
	}
	return false, nil
}

func hasEnabledStackLabel(node *corev1.Node, stack string) bool {
	for key := range stackExclusiveKeys(stack) {
		if node.Labels[key] == "true" {
			return true
		}
	}
	return false
}
