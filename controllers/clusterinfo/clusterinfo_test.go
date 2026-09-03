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

package clusterinfo

import (
	"testing"

	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"

	"github.com/NVIDIA/gpu-operator/internal/consts"
)

func TestGetRuntimeString(t *testing.T) {
	tests := []struct {
		description string
		runtimeVer  string
		expected    string
		expectError bool
	}{
		{
			description: "docker runtime",
			runtimeVer:  "docker://20.10.7",
			expected:    consts.Docker,
		},
		{
			description: "containerd runtime",
			runtimeVer:  "containerd://1.6.8",
			expected:    consts.Containerd,
		},
		{
			description: "cri-o runtime",
			runtimeVer:  "cri-o://1.24.1",
			expected:    consts.CRIO,
		},
		{
			description: "unrecognized runtime returns error",
			runtimeVer:  "unknown-runtime://1.0.0",
			expectError: true,
		},
		{
			description: "empty runtime version returns error",
			runtimeVer:  "",
			expectError: true,
		},
	}

	for _, tc := range tests {
		t.Run(tc.description, func(t *testing.T) {
			node := corev1.Node{
				Status: corev1.NodeStatus{
					NodeInfo: corev1.NodeSystemInfo{
						ContainerRuntimeVersion: tc.runtimeVer,
					},
				},
			}

			result, err := getRuntimeString(node)

			if tc.expectError {
				require.Error(t, err)
				return
			}
			require.NoError(t, err)
			require.Equal(t, tc.expected, result)
		})
	}
}
