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
	"strings"

	corev1 "k8s.io/api/core/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"

	nvidiav1alpha1 "github.com/NVIDIA/gpu-operator/api/nvidia/v1alpha1"
	"github.com/NVIDIA/gpu-operator/internal/consts"
)

// ValidateNodeSelector rejects selectors that use operator-managed routing labels
// or scope the default fallback driver.
func ValidateNodeSelector(driver *nvidiav1alpha1.NVIDIADriver) error {
	if driver == nil || driver.Spec.NodeSelector == nil {
		return nil
	}
	if driver.IsDefault() && len(driver.Spec.NodeSelector) > 0 {
		return fmt.Errorf("default NVIDIADriver %q cannot use nodeSelector", driver.Name)
	}
	if _, ok := driver.Spec.NodeSelector[consts.NVIDIADriverOwnerLabel]; ok {
		return fmt.Errorf("NVIDIADriver %q nodeSelector cannot use reserved label %q", driver.Name, consts.NVIDIADriverOwnerLabel)
	}
	return nil
}

// NodeMatchesSelector returns true when all selector labels are present on the node.
func NodeMatchesSelector(node *corev1.Node, selector map[string]string) bool {
	for key, value := range selector {
		if node.Labels[key] != value {
			return false
		}
	}
	return true
}

// AssignOwners labels GPU nodes with the NVIDIADriver that should manage their driver pods.
// Non-default NVIDIADrivers take precedence over the default fallback, and conflicts fail closed before
// node owner labels are changed. It returns true when any node label was changed.
func AssignOwners(ctx context.Context, c client.Client) (bool, error) {
	drivers := &nvidiav1alpha1.NVIDIADriverList{}
	if err := c.List(ctx, drivers); err != nil {
		return false, fmt.Errorf("failed to list NVIDIADriver CRs: %w", err)
	}

	var defaultDriver *nvidiav1alpha1.NVIDIADriver
	defaultDriverNames := []string{}
	nonDefaultDrivers := make([]nvidiav1alpha1.NVIDIADriver, 0, len(drivers.Items))
	for i := range drivers.Items {
		if !drivers.Items[i].GetDeletionTimestamp().IsZero() {
			continue
		}
		if err := ValidateNodeSelector(&drivers.Items[i]); err != nil {
			return false, err
		}
		if drivers.Items[i].IsDefault() {
			defaultDriverNames = append(defaultDriverNames, drivers.Items[i].Name)
			defaultDriver = &drivers.Items[i]
			continue
		}
		nonDefaultDrivers = append(nonDefaultDrivers, drivers.Items[i])
	}
	if len(defaultDriverNames) > 1 {
		return false, fmt.Errorf("multiple default NVIDIADrivers found: %s", strings.Join(defaultDriverNames, ", "))
	}
	nodes := &corev1.NodeList{}
	if err := c.List(ctx, nodes, client.MatchingLabels{consts.GPUPresentLabel: "true"}); err != nil {
		return false, fmt.Errorf("failed to list GPU nodes: %w", err)
	}

	for i := range nodes.Items {
		matchingDrivers := []string{}
		for _, driver := range nonDefaultDrivers {
			if NodeMatchesSelector(&nodes.Items[i], driver.GetNodeSelector()) {
				matchingDrivers = append(matchingDrivers, driver.Name)
			}
		}
		if len(matchingDrivers) > 1 {
			return false, fmt.Errorf("multiple NVIDIADrivers match the same node %s: %v", nodes.Items[i].Name, matchingDrivers)
		}
	}

	changed := false
	for i := range nodes.Items {
		node := &nodes.Items[i]
		originalNode := node.DeepCopy()
		owner := ""
		for _, driver := range nonDefaultDrivers {
			if NodeMatchesSelector(node, driver.GetNodeSelector()) {
				owner = driver.Name
			}
		}
		if owner == "" && defaultDriver != nil && NodeMatchesSelector(node, defaultDriver.GetNodeSelector()) {
			owner = defaultDriver.Name
		}
		if owner == "" {
			if node.Labels == nil {
				continue
			}
			if _, ok := node.Labels[consts.NVIDIADriverOwnerLabel]; !ok {
				continue
			}
			delete(node.Labels, consts.NVIDIADriverOwnerLabel)
			if err := c.Patch(ctx, node, client.MergeFrom(originalNode)); err != nil {
				return false, fmt.Errorf("failed to remove NVIDIADriver owner label for node %s: %w", node.Name, err)
			}
			changed = true
			continue
		}
		if node.Labels != nil && node.Labels[consts.NVIDIADriverOwnerLabel] == owner {
			continue
		}
		if node.Labels == nil {
			node.Labels = map[string]string{}
		}
		node.Labels[consts.NVIDIADriverOwnerLabel] = owner
		if err := c.Patch(ctx, node, client.MergeFrom(originalNode)); err != nil {
			return false, fmt.Errorf("failed to update NVIDIADriver owner label for node %s: %w", node.Name, err)
		}
		changed = true
	}

	return changed, nil
}
