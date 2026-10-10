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

package state

import (
	"fmt"

	appsv1 "k8s.io/api/apps/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/util/sets"
)

func applyGPUClusterDaemonSetLabels(objs []*unstructured.Unstructured, commonLabels map[string]string) error {
	if len(commonLabels) == 0 {
		return nil
	}
	for _, obj := range objs {
		if obj.GetAPIVersion() != appsv1.SchemeGroupVersion.String() || obj.GetKind() != "DaemonSet" {
			continue
		}

		selectorMap, _, err := unstructured.NestedMap(obj.Object, "spec", "selector")
		if err != nil {
			return fmt.Errorf("failed to read selector for DaemonSet %s: %w", obj.GetName(), err)
		}
		var selector metav1.LabelSelector
		if err := runtime.DefaultUnstructuredConverter.FromUnstructured(selectorMap, &selector); err != nil {
			return fmt.Errorf("failed to decode selector for DaemonSet %s: %w", obj.GetName(), err)
		}

		// Derive reserved keys from the immutable selector instead of maintaining a
		// second per-operand list. Expression keys must also preserve their presence
		// or absence. Keep the existing app and part-of exclusions even when they
		// are not part of this operand's selector (e.g. the validator's app label).
		reserved := sets.New("app", "app.kubernetes.io/part-of")
		for key := range selector.MatchLabels {
			reserved.Insert(key)
		}
		for _, requirement := range selector.MatchExpressions {
			reserved.Insert(requirement.Key)
		}

		podLabels, _, err := unstructured.NestedStringMap(obj.Object, "spec", "template", "metadata", "labels")
		if err != nil {
			return fmt.Errorf("failed to read pod labels for DaemonSet %s: %w", obj.GetName(), err)
		}
		if podLabels == nil {
			podLabels = make(map[string]string)
		}
		// Preserve the DCGM template exclusions while still allowing other
		// operands to customize their component label.
		switch podLabels[AppComponentLabelKey] {
		case "nvidia-dcgm", "nvidia-dcgm-exporter":
			reserved.Insert(AppComponentLabelKey)
		}
		for key, value := range commonLabels {
			if !reserved.Has(key) {
				podLabels[key] = value
			}
		}
		if err := unstructured.SetNestedStringMap(obj.Object, podLabels, "spec", "template", "metadata", "labels"); err != nil {
			return fmt.Errorf("failed to set pod labels for DaemonSet %s: %w", obj.GetName(), err)
		}
	}
	return nil
}
