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

package coexistence

import (
	"fmt"
	"os"

	corev1 "k8s.io/api/core/v1"
)

const ModeLabel = "nvidia.com/gpu.deploy.mode"
const DefaultStackEnv = "DEFAULT_GPU_ALLOCATION_STACK"
const SelectionAnnotation = "nvidia.com/gpu-operator.selected-allocator"
const DP = "device-plugin"
const DRA = "dra"

// DefaultStack reads and validates the operator's configured default allocator.
func DefaultStack() (string, error) {
	value, set := os.LookupEnv(DefaultStackEnv)
	if !set {
		return "", nil
	}
	if value != DP && value != DRA {
		return "", fmt.Errorf("%s must be %q or %q, got %q", DefaultStackEnv, DP, DRA, value)
	}
	return value, nil
}

// Select chooses intent. Source eligibility and deletion reservations are checked by
// the caller before recording or enabling placement. The default is validated at startup.
func Select(mode string, explicit bool, recorded string, cp, gc bool, defaultStack string) (string, error) {
	if explicit {
		if mode != DP && mode != DRA {
			return "", fmt.Errorf("invalid allocator mode %q", mode)
		}
		return mode, nil
	}
	if recorded != "" && recorded != DP && recorded != DRA {
		return "", fmt.Errorf("invalid allocator selection %q", recorded)
	}
	if (recorded == DP && cp) || (recorded == DRA && gc) {
		return recorded, nil
	}
	if defaultStack != "" {
		return defaultStack, nil
	}
	if cp {
		return DP, nil
	}
	if gc {
		return DRA, nil
	}
	return "", nil
}

// ConstrainMode intersects every existing affinity alternative with customer intent.
func ConstrainMode(spec *corev1.PodSpec, mode string) {
	if spec.Affinity == nil {
		spec.Affinity = &corev1.Affinity{}
	}
	if spec.Affinity.NodeAffinity == nil {
		spec.Affinity.NodeAffinity = &corev1.NodeAffinity{}
	}
	affinity := spec.Affinity.NodeAffinity
	terms := []corev1.NodeSelectorTerm{{}}
	if affinity.RequiredDuringSchedulingIgnoredDuringExecution != nil {
		terms = affinity.RequiredDuringSchedulingIgnoredDuringExecution.NodeSelectorTerms
	}
	var result []corev1.NodeSelectorTerm
	for _, term := range terms {
		if affinity.RequiredDuringSchedulingIgnoredDuringExecution != nil && len(term.MatchExpressions) == 0 && len(term.MatchFields) == 0 {
			continue
		}
		for _, requirement := range []corev1.NodeSelectorRequirement{
			{Key: ModeLabel, Operator: corev1.NodeSelectorOpDoesNotExist},
			{Key: ModeLabel, Operator: corev1.NodeSelectorOpIn, Values: []string{mode}},
		} {
			copy := term.DeepCopy()
			copy.MatchExpressions = append(copy.MatchExpressions, requirement)
			result = append(result, *copy)
		}
	}
	affinity.RequiredDuringSchedulingIgnoredDuringExecution = &corev1.NodeSelector{NodeSelectorTerms: result}
}
