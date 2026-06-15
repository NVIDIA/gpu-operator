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

	"github.com/NVIDIA/k8s-operator-libs/pkg/upgrade"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/log"

	nvidiav1alpha1 "github.com/NVIDIA/gpu-operator/api/nvidia/v1alpha1"
	"github.com/NVIDIA/gpu-operator/internal/consts"
)

// labelNodesWithOrphanedDriverPods marks NVIDIADriver-owned nodes that still have orphaned
// ClusterPolicy driver pods so the driver upgrade controller can replace those pods in
// the normal controlled upgrade flow.
func (r *NVIDIADriverReconciler) labelNodesWithOrphanedDriverPods(ctx context.Context) error {
	nvidiaDrivers := &nvidiav1alpha1.NVIDIADriverList{}
	if err := r.List(ctx, nvidiaDrivers); err != nil {
		return fmt.Errorf("failed to list NVIDIADriver CRs: %w", err)
	}
	if len(nvidiaDrivers.Items) == 0 {
		return nil
	}

	pods := &corev1.PodList{}
	if err := r.List(ctx, pods,
		client.InNamespace(r.Namespace),
		client.MatchingLabels{AppComponentLabelKey: DriverAppComponentLabelValue},
	); err != nil {
		return fmt.Errorf("failed to list NVIDIA driver pods: %w", err)
	}

	for i := range pods.Items {
		pod := &pods.Items[i]
		if len(pod.OwnerReferences) > 0 || pod.Status.Phase != corev1.PodRunning || pod.Spec.NodeName == "" {
			continue
		}

		node := &corev1.Node{}
		if err := r.Get(ctx, types.NamespacedName{Name: pod.Spec.NodeName}, node); err != nil {
			log.FromContext(ctx).Error(err, "failed to get node for orphaned NVIDIA driver pod", "pod", pod.Name, "node", pod.Spec.NodeName)
			continue
		}
		if !nodeOwnedByNVIDIADriver(node, nvidiaDrivers.Items) {
			continue
		}

		upgradeStateLabel := upgrade.GetUpgradeStateLabelKey()
		upgradeState := upgrade.UpgradeStateUnknown
		if node.Labels != nil {
			upgradeState = node.Labels[upgradeStateLabel]
		}
		if !isDriverUpgradeRequestAllowed(upgradeState) {
			continue
		}
		originalNode := node.DeepCopy()
		if node.Labels == nil {
			node.Labels = map[string]string{}
		}
		node.Labels[upgradeStateLabel] = upgrade.UpgradeStateUpgradeRequired
		if err := r.Patch(ctx, node, client.MergeFrom(originalNode)); err != nil {
			log.FromContext(ctx).Error(err, "failed to label node with orphaned NVIDIA driver pod", "pod", pod.Name, "node", node.Name)
		}
	}

	return nil
}

// isDriverUpgradeRequestAllowed returns true when migration can request a
// driver upgrade without rewinding an active or failed upgrade state.
func isDriverUpgradeRequestAllowed(upgradeState string) bool {
	return upgradeState == upgrade.UpgradeStateUnknown || upgradeState == upgrade.UpgradeStateDone
}

// nodeOwnedByNVIDIADriver returns true when the node has an owner label for a live NVIDIADriver.
func nodeOwnedByNVIDIADriver(node *corev1.Node, nvidiaDrivers []nvidiav1alpha1.NVIDIADriver) bool {
	if node.Labels == nil || node.Labels[consts.NVIDIADriverOwnerLabel] == "" {
		return false
	}

	for _, nvidiaDriver := range nvidiaDrivers {
		if !nvidiaDriver.GetDeletionTimestamp().IsZero() {
			continue
		}
		if node.Labels[consts.NVIDIADriverOwnerLabel] == nvidiaDriver.Name {
			return true
		}
	}
	return false
}
