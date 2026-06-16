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

package nvidiadriver

import (
	"context"
	"fmt"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/labels"
	"sigs.k8s.io/controller-runtime/pkg/client"

	nvidiav1alpha1 "github.com/NVIDIA/gpu-operator/api/nvidia/v1alpha1"
	"github.com/NVIDIA/gpu-operator/internal/consts"
)

// nodeMatchesSelector reports whether nodeLabels satisfy the NVIDIADriver nodeSelector.
func nodeMatchesSelector(nodeLabels map[string]string, selector map[string]string) bool {
	return labels.SelectorFromSet(selector).Matches(labels.Set(nodeLabels))
}

// AssignOwners labels GPU nodes with the NVIDIADriver that should manage their driver pods.
// Non-default NVIDIADrivers take precedence over the default fallback, and conflicts fail closed before
// node owner labels are changed. On success, it returns true when any node owner label was changed.
func AssignOwners(ctx context.Context, c client.Client) (bool, error) {
	drivers := &nvidiav1alpha1.NVIDIADriverList{}
	if err := c.List(ctx, drivers); err != nil {
		return false, fmt.Errorf("failed to list NVIDIADriver CRs: %w", err)
	}

	defaultDrivers, nonDefaultDrivers, err := classifyDrivers(drivers.Items)
	if err != nil {
		return false, err
	}
	if len(defaultDrivers) > 1 {
		return false, fmt.Errorf("multiple default NVIDIADrivers found: %v", driverNames(defaultDrivers))
	}
	defaultOwner := ""
	if len(defaultDrivers) == 1 {
		defaultOwner = defaultDrivers[0].Name
	}

	nodes := &corev1.NodeList{}
	if err := c.List(ctx, nodes, client.MatchingLabels{consts.GPUPresentLabel: "true"}); err != nil {
		return false, fmt.Errorf("failed to list GPU nodes: %w", err)
	}

	desiredOwnersByNode := map[string]string{}
	for _, node := range nodes.Items {
		desiredOwner, err := desiredOwnerForNode(&node, nonDefaultDrivers, defaultOwner)
		if err != nil {
			return false, err
		}
		desiredOwnersByNode[node.Name] = desiredOwner
	}

	changed := false
	for _, nodeItem := range nodes.Items {
		node := nodeItem.DeepCopy()
		desiredOwner := desiredOwnersByNode[node.Name]

		if !ownerLabelNeedsUpdate(node.Labels, desiredOwner) {
			continue
		}

		originalNode := node.DeepCopy()
		if desiredOwner == "" {
			delete(node.Labels, consts.NVIDIADriverOwnerLabel)
		} else {
			if node.Labels == nil {
				node.Labels = map[string]string{}
			}
			node.Labels[consts.NVIDIADriverOwnerLabel] = desiredOwner
		}

		if err := c.Patch(ctx, node, client.MergeFrom(originalNode)); err != nil {
			if desiredOwner == "" {
				return false, fmt.Errorf("failed to remove NVIDIADriver owner label for node %q: %w", node.Name, err)
			}
			return false, fmt.Errorf("failed to update NVIDIADriver owner label for node %q: %w", node.Name, err)
		}
		changed = true
	}

	return changed, nil
}

// classifyDrivers filters out deleting drivers and splits live drivers by default fallback status.
func classifyDrivers(drivers []nvidiav1alpha1.NVIDIADriver) ([]nvidiav1alpha1.NVIDIADriver, []nvidiav1alpha1.NVIDIADriver, error) {
	defaultDrivers := []nvidiav1alpha1.NVIDIADriver{}
	nonDefaultDrivers := make([]nvidiav1alpha1.NVIDIADriver, 0, len(drivers))
	for _, driver := range drivers {
		if driver.HasDeletionTimestamp() {
			continue
		}
		if err := driver.ValidateNodeSelector(); err != nil {
			return nil, nil, err
		}
		if driver.IsDefault() {
			defaultDrivers = append(defaultDrivers, driver)
			continue
		}
		nonDefaultDrivers = append(nonDefaultDrivers, driver)
	}
	return defaultDrivers, nonDefaultDrivers, nil
}

// driverNames returns the names of the provided drivers in their current order.
func driverNames(drivers []nvidiav1alpha1.NVIDIADriver) []string {
	names := make([]string, len(drivers))
	for i, driver := range drivers {
		names[i] = driver.Name
	}
	return names
}

// desiredOwnerForNode returns the non-default driver that should own the node, or the fallback owner when none match.
func desiredOwnerForNode(
	node *corev1.Node,
	nonDefaultDrivers []nvidiav1alpha1.NVIDIADriver,
	defaultOwner string,
) (string, error) {
	matchingDrivers := []string{}
	for _, driver := range nonDefaultDrivers {
		if nodeMatchesSelector(node.Labels, driver.GetNodeSelector()) {
			matchingDrivers = append(matchingDrivers, driver.Name)
		}
	}
	if len(matchingDrivers) > 1 {
		return "", fmt.Errorf("multiple NVIDIADrivers match the same node %s: %v", node.Name, matchingDrivers)
	}
	if len(matchingDrivers) == 1 {
		return matchingDrivers[0], nil
	}
	return defaultOwner, nil
}

// ownerLabelNeedsUpdate reports whether the node owner label differs from the desired owner.
func ownerLabelNeedsUpdate(nodeLabels map[string]string, desiredOwner string) bool {
	currentOwner, hasOwnerLabel := nodeLabels[consts.NVIDIADriverOwnerLabel]
	if desiredOwner == "" {
		return hasOwnerLabel
	}
	return !hasOwnerLabel || (currentOwner != desiredOwner)
}
