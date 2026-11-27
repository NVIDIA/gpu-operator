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

package k8s

import (
	"testing"

	"github.com/stretchr/testify/require"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"

	nvidiav1alpha1 "github.com/NVIDIA/gpu-operator/api/nvidia/v1alpha1"
	"github.com/NVIDIA/gpu-operator/internal/consts"
)

// The GDRCopy test picks its eligible nodes, and the node for its workload pod,
// from the nodes whose driver pods the NVIDIADriver manages. Selecting on the
// nodeSelector alone matches nodes without a GPU, every node when the
// nodeSelector is empty, and GPU nodes the operator assigned to another
// NVIDIADriver.
func TestDriverNodeSelector(t *testing.T) {
	gpuNode := func(owner string) map[string]string {
		return map[string]string{
			consts.GPUPresentLabel:        "true",
			consts.NVIDIADriverOwnerLabel: owner,
			"pool":                        "a100",
		}
	}

	for name, tc := range map[string]struct {
		nodeSelector map[string]string
		nodeLabels   map[string]string
		want         bool
	}{
		"GPU node owned by the NVIDIADriver": {
			nodeLabels: gpuNode("default"),
			want:       true,
		},
		"GPU node owned by the NVIDIADriver and in its nodeSelector": {
			nodeSelector: map[string]string{"pool": "a100"},
			nodeLabels:   gpuNode("default"),
			want:         true,
		},
		"node without a GPU": {
			nodeLabels: map[string]string{"kubernetes.io/os": "linux"},
			want:       false,
		},
		"node without a GPU in the nodeSelector": {
			nodeSelector: map[string]string{"pool": "a100"},
			nodeLabels:   map[string]string{"pool": "a100"},
			want:         false,
		},
		"GPU node owned by another NVIDIADriver": {
			nodeLabels: gpuNode("precompiled"),
			want:       false,
		},
	} {
		t.Run(name, func(t *testing.T) {
			driver := nvidiav1alpha1.NVIDIADriver{
				ObjectMeta: metav1.ObjectMeta{Name: "default"},
				Spec:       nvidiav1alpha1.NVIDIADriverSpec{NodeSelector: tc.nodeSelector},
			}
			selector, err := labels.Parse(driverNodeSelector(driver))
			require.NoError(t, err)
			require.Equal(t, tc.want, selector.Matches(labels.Set(tc.nodeLabels)))
		})
	}
}
