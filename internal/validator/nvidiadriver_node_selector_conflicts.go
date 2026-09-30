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

package validator

import (
	"context"
	"errors"
	"fmt"
	"sort"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/labels"
	"sigs.k8s.io/controller-runtime/pkg/client"

	nvidiav1alpha1 "github.com/NVIDIA/gpu-operator/api/nvidia/v1alpha1"
	"github.com/NVIDIA/gpu-operator/internal/consts"
)

// ErrConflictingNVIDIADriverNodeSelectors indicates that two non-default drivers target or could target the same GPU node.
var ErrConflictingNVIDIADriverNodeSelectors = errors.New("conflicting NVIDIADriver node selectors")

// ValidatePotentialNVIDIADriverNodeSelectorConflicts rejects selectors that could
// select the same GPU node, including nodes that do not exist yet.
func ValidatePotentialNVIDIADriverNodeSelectorConflicts(ctx context.Context, reader client.Reader, incoming *nvidiav1alpha1.NVIDIADriver) error {
	potentialConflicts, err := findPotentialNVIDIADriverNodeSelectorConflicts(ctx, reader, incoming)
	if err != nil {
		return err
	}
	if len(potentialConflicts) > 0 {
		return fmt.Errorf("%w: %q and %q could select the same GPU node", ErrConflictingNVIDIADriverNodeSelectors, incoming.Name, potentialConflicts[0].Name)
	}
	return nil
}

// ValidateNVIDIADriverNodeSelectorConflictsOnExistingNodes rejects selectors
// only when a GPU node currently matches both drivers.
func ValidateNVIDIADriverNodeSelectorConflictsOnExistingNodes(ctx context.Context, reader client.Reader, incoming *nvidiav1alpha1.NVIDIADriver) error {
	potentialConflicts, err := findPotentialNVIDIADriverNodeSelectorConflicts(ctx, reader, incoming)
	if err != nil {
		return err
	}
	if len(potentialConflicts) == 0 {
		return nil
	}

	nodes := &corev1.NodeList{}
	if err := reader.List(ctx, nodes, client.MatchingLabels{consts.GPUPresentLabel: "true"}); err != nil {
		return fmt.Errorf("list GPU nodes: %w", err)
	}

	incomingSelector := labels.SelectorFromSet(incoming.GetNodeSelector())
	for i := range potentialConflicts {
		existing := &potentialConflicts[i]
		existingSelector := labels.SelectorFromSet(existing.GetNodeSelector())
		for _, node := range nodes.Items {
			nodeLabels := labels.Set(node.Labels)
			if incomingSelector.Matches(nodeLabels) && existingSelector.Matches(nodeLabels) {
				return fmt.Errorf("%w: %q and %q both select GPU node %q", ErrConflictingNVIDIADriverNodeSelectors, incoming.Name, existing.Name, node.Name)
			}
		}
	}
	return nil
}

// findPotentialNVIDIADriverNodeSelectorConflicts returns live non-default drivers
// whose selectors could select the same GPU node as the incoming driver.
func findPotentialNVIDIADriverNodeSelectorConflicts(ctx context.Context, reader client.Reader, incoming *nvidiav1alpha1.NVIDIADriver) ([]nvidiav1alpha1.NVIDIADriver, error) {
	if incoming == nil {
		return nil, errors.New("incoming NVIDIADriver is nil")
	}
	if err := incoming.ValidateNodeSelector(); err != nil {
		return nil, err
	}

	drivers := &nvidiav1alpha1.NVIDIADriverList{}
	if err := reader.List(ctx, drivers); err != nil {
		return nil, fmt.Errorf("list NVIDIADrivers: %w", err)
	}

	var potentialConflicts []nvidiav1alpha1.NVIDIADriver
	incomingSelector := incoming.GetNodeSelector()
	for i := range drivers.Items {
		existing := &drivers.Items[i]
		if existing.Name == incoming.Name || existing.HasDeletionTimestamp() {
			continue
		}
		if err := existing.ValidateNodeSelector(); err != nil {
			return nil, err
		}
		if incoming.IsDefault() && existing.IsDefault() {
			names := []string{incoming.Name, existing.Name}
			sort.Strings(names)
			return nil, fmt.Errorf("%w: %v", ErrMultipleDefaultNVIDIADrivers, names)
		}
		// The default driver is a fallback for GPU nodes selected by no non-default driver.
		if incoming.IsDefault() || existing.IsDefault() {
			continue
		}
		if nodeSelectorsCanSelectSameGPUNode(incomingSelector, existing.GetNodeSelector()) {
			potentialConflicts = append(potentialConflicts, *existing)
		}
	}
	return potentialConflicts, nil
}

// nodeSelectorsCanSelectSameGPUNode checks whether any GPU node can satisfy both sets of equality requirements.
func nodeSelectorsCanSelectSameGPUNode(a, b map[string]string) bool {
	// Driver ownership is only assigned on GPU nodes, even when a selector omits this label.
	for _, selector := range []map[string]string{a, b} {
		if value, ok := selector[consts.GPUPresentLabel]; ok && value != "true" {
			return false
		}
	}
	for key, value := range a {
		if other, ok := b[key]; ok && value != other {
			return false
		}
	}
	return true
}
