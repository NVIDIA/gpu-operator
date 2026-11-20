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

// selectNode reconciles the node's allocator annotation and placement labels.
func (nlc *nodeLabelingController) selectNode(ctx context.Context, node *corev1.Node) (bool, error) {
	// Reserve even when discovery or customer mode changes during teardown.
	if nodeReservedForTeardown(node, nlc.clusterPolicy, nlc.gpuCluster) {
		return false, nil
	}
	if !hasCommonGPULabel(node.Labels) {
		return removeAllGPUStateLabels(node.Labels), nil
	}

	cpPresent := nlc.clusterPolicy != nil && nlc.clusterPolicy.DeletionTimestamp.IsZero()
	gcPresent := nlc.gpuCluster != nil && nlc.gpuCluster.DeletionTimestamp.IsZero()
	stack, selectionErr := nodeSelection(node, cpPresent, gcPresent, nlc.defaultStack)
	if selectionErr == nil && nlc.waitingForSourceFinalizer(stack, cpPresent, gcPresent) {
		return false, nil
	}

	previousLabels := maps.Clone(node.Labels)
	previousAnnotations := maps.Clone(node.Annotations)

	// A disappeared source cannot keep its automatic selection on this node.
	record := node.Annotations[coexistence.SelectionAnnotation]
	if (record == coexistence.DP && !cpPresent) || (record == coexistence.DRA && !gcPresent) {
		delete(node.Annotations, coexistence.SelectionAnnotation)
	}

	if selectionErr != nil {
		nlc.withdrawPlacement(ctx, node.Labels, node.Name)
	} else {
		nlc.reconcileAllocatorPlacement(ctx, node, stack)
	}

	changed := !maps.Equal(previousLabels, node.Labels) || !maps.Equal(previousAnnotations, node.Annotations)
	return changed, selectionErr
}

// waitingForSourceFinalizer protects operands that predate the finalizer on upgrade.
func (nlc *nodeLabelingController) waitingForSourceFinalizer(stack string, cpPresent, gcPresent bool) bool {
	switch stack {
	case coexistence.DP:
		return cpPresent && !controllerutil.ContainsFinalizer(nlc.clusterPolicy, nodeCleanupFinalizer)
	case coexistence.DRA:
		return gcPresent && !controllerutil.ContainsFinalizer(nlc.gpuCluster, nodeCleanupFinalizer)
	default:
		return false
	}
}

func (nlc *nodeLabelingController) reconcileAllocatorPlacement(ctx context.Context, node *corev1.Node, stack string) {
	labels := node.Labels
	dpEligible, draEligible := allocationEligibility(nlc.clusterPolicy, nlc.gpuCluster)
	eligible := (stack == coexistence.DP && dpEligible) || (stack == coexistence.DRA && draEligible)
	if !eligible {
		nlc.withdrawPlacement(ctx, labels, node.Name)
		if stack != "" {
			nlc.logger.Info("WaitingForStack", "node", node.Name, "stack", stack)
		}
		return
	}

	// Withdraw DRA placement in a separate Node update before enabling the
	// device plugin. A DaemonSet pod can outlive its scheduling label.
	if stack == coexistence.DP && nlc.draPlacementPending(ctx, node) {
		nlc.withdrawPlacement(ctx, labels, node.Name)
		nlc.draPluginRemovalDeferred = true
		return
	}

	if node.Annotations == nil {
		node.Annotations = map[string]string{}
	}
	node.Annotations[coexistence.SelectionAnnotation] = stack
	nlc.updateGPUStateLabels(ctx, labels, node.Name, stack)
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
func sourceAdoptionPending(ctx context.Context, c client.Client, source client.Object, defaultStack string) (bool, error) {
	cp, gc, err := resolveActiveConfig(ctx, c)
	if err != nil {
		return false, err
	}
	dp, dra := allocationEligibility(cp, gc)
	nodes := &corev1.NodeList{}
	if err := c.List(ctx, nodes); err != nil {
		return false, err
	}
	target := sourceStack(source)
	for i := range nodes.Items {
		node := &nodes.Items[i]
		// A source's DaemonSets can also match stale labels on nodes selected
		// for another stack (or no longer discovered). Check those before
		// excluding nodes that do not need adoption by this source.
		if !hasGPULabels(node.Labels) || nodeReservedForTeardown(node, cp, gc) {
			if hasSchedulableStackLabel(node, target) {
				return true, nil
			}
			continue
		}
		selected, selectionErr := nodeSelection(node, cp != nil && cp.DeletionTimestamp.IsZero(), gc != nil && gc.DeletionTimestamp.IsZero(), defaultStack)
		// Invalid metadata excludes the node once labels that can still schedule
		// this stack have been withdrawn.
		if selectionErr != nil || selected != target || (selected == coexistence.DP && !dp) || (selected == coexistence.DRA && !dra) {
			if hasSchedulableStackLabel(node, target) {
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
			sandbox := cp.Spec.SandboxWorkloads
			config, _ := getWorkloadConfig(node.Labels, sandbox)
			keys = getEffectiveStateLabels(config, sandbox.Mode)
		}
		for key := range keys {
			if node.Labels[key] == "" {
				return true, nil
			}
		}
	}
	return false, nil
}

func hasSchedulableStackLabel(node *corev1.Node, stack string) bool {
	if mode, explicit := node.Labels[coexistence.ModeLabel]; explicit && mode != stack {
		// The DRA plugin follows its deploy label so it can finish unprepare.
		return stack == coexistence.DRA && node.Labels[draDriverDeployLabelKey] == "true"
	}
	for key := range stackExclusiveKeys(stack) {
		if node.Labels[key] == "true" {
			return true
		}
	}
	return false
}
