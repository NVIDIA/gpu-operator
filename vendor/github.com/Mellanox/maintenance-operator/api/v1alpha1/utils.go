/*
 Copyright 2024, NVIDIA CORPORATION & AFFILIATES

 Licensed under the Apache License, Version 2.0 (the "License");
 you may not use this file except in compliance with the License.
 You may obtain a copy of the License at

     http://www.apache.org/licenses/LICENSE-2.0

 Unless required by applicable law or agreed to in writing, software
 distributed under the License is distributed on an "AS IS" BASIS,
 WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
 See the License for the specific language governing permissions and
 limitations under the License.
*/

package v1alpha1

import (
	"fmt"
	"regexp"

	corev1 "k8s.io/api/core/v1"
)

// CanonicalString is a canonical string representation of NodeMaintenance
func (nm *NodeMaintenance) CanonicalString() string {
	return fmt.Sprintf("%s/%s:%s@%s", nm.Namespace, nm.Name, nm.Spec.RequestorID, nm.Spec.NodeName)
}

// Match matches PodEvictionFiterEntry on pod. returns true if Pod matches filter, false otherwise.
func (e *PodEvictionFiterEntry) Match(pod *corev1.Pod) bool {
	var match bool
	// match on ByResourceName regex
	re, err := regexp.Compile(*e.ByResourceNameRegex)
	if err != nil {
		return match
	}

OUTER:
	for _, c := range pod.Spec.Containers {
		for resourceName := range c.Resources.Requests {
			if re.MatchString(resourceName.String()) {
				match = true
				break OUTER
			}
		}
	}

	return match
}
