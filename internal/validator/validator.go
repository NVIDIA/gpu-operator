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
	"fmt"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/labels"
	"sigs.k8s.io/controller-runtime/pkg/client"

	nvidiav1alpha1 "github.com/NVIDIA/gpu-operator/api/nvidia/v1alpha1"
)

// Validator provides interface to validate NVIDIADriver fields
type Validator interface {
	Validate(ctx context.Context, cr *nvidiav1alpha1.NVIDIADriver) error
}

// nodeSelectorValidator validates against the nodeSelector
type nodeSelectorValidator struct {
	client client.Client
}

// NewNodeSelectorValidator returns a new instance of nodeselector validator
func NewNodeSelectorValidator(c client.Client) Validator {
	return &nodeSelectorValidator{client: c}
}

// Check returns error when nodes matching with the selector labels of current instance of NVIDIADriver
// are conflicting with other instances of NVIDIADriver
func (nsv *nodeSelectorValidator) Validate(ctx context.Context, cr *nvidiav1alpha1.NVIDIADriver) error {
	drivers := &nvidiav1alpha1.NVIDIADriverList{}
	err := nsv.client.List(ctx, drivers)
	if err != nil {
		return err
	}

	crHasEmptyNodeSelector := false
	if cr.Spec.NodeSelector == nil || len(cr.Spec.NodeSelector) == 0 {
		crHasEmptyNodeSelector = true
	}

	names := map[string]struct{}{}
	for di, driver := range drivers.Items {
		if driver.Name == cr.Name {
			continue
		}

		if crHasEmptyNodeSelector {
			// If the CR we are validating has an empty node selector, it does not conflict
			// with any other CR unless there is another CR that also is configured with an
			// empty node selector. Only one NVIDIADriver instance can be configured with an
			// empty node selector at any point in time. We deem such an instance as the 'default'
			// instance, as it will get deployed on all GPU nodes. Other CRs, with non-empty
			// node selectors, can override the 'default' NVIDIADriver instance.
			if driver.Spec.NodeSelector == nil || len(driver.Spec.NodeSelector) == 0 {
				return fmt.Errorf("cannot have empty nodeSelector as another CR is configured with an empty nodeSelector: %s", driver.Name)
			}
			continue
		}

		nodeList, err := nsv.getNVIDIADriverSelectedNodes(ctx, &drivers.Items[di])
		if err != nil {
			return err
		}

		for ni := range nodeList.Items {
			if _, ok := names[nodeList.Items[ni].Name]; ok {
				return fmt.Errorf("conflicting NVIDIADriver NodeSelectors found for resource: %s, nodeSelector: %q", cr.Name, cr.Spec.NodeSelector)
			}

			names[nodeList.Items[ni].Name] = struct{}{}
		}

	}

	return nil
}

// getNVIDIADriverSelectedNodes returns selected nodes based on the nodeselector labels set for a given NVIDIADriver instance
func (nsv *nodeSelectorValidator) getNVIDIADriverSelectedNodes(ctx context.Context, cr *nvidiav1alpha1.NVIDIADriver) (*corev1.NodeList, error) {
	nodeList := &corev1.NodeList{}

	if cr.Spec.NodeSelector == nil {
		cr.Spec.NodeSelector = cr.GetNodeSelector()
	}

	selector := labels.Set(cr.Spec.NodeSelector).AsSelector()

	opts := []client.ListOption{
		client.MatchingLabelsSelector{Selector: selector},
	}
	err := nsv.client.List(ctx, nodeList, opts...)

	return nodeList, err
}
