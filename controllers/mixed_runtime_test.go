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
	"testing"

	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/log"

	gpuv1 "github.com/NVIDIA/gpu-operator/api/nvidia/v1"
)

func TestGetNodeRuntimeMap(t *testing.T) {
	tests := []struct {
		name           string
		nodes          []corev1.Node
		openshift      string
		expectedResult map[string]gpuv1.Runtime
		expectError    bool
	}{
		{
			name: "mixed runtimes - containerd and cri-o",
			nodes: []corev1.Node{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name: "node1",
						Labels: map[string]string{
							"feature.node.kubernetes.io/pci-10de.present": "true",
							"nvidia.com/gpu.present":                      "true",
						},
					},
					Status: corev1.NodeStatus{
						NodeInfo: corev1.NodeSystemInfo{
							ContainerRuntimeVersion: "containerd://1.6.0",
						},
					},
				},
				{
					ObjectMeta: metav1.ObjectMeta{
						Name: "node2",
						Labels: map[string]string{
							"feature.node.kubernetes.io/pci-10de.present": "true",
							"nvidia.com/gpu.present":                      "true",
						},
					},
					Status: corev1.NodeStatus{
						NodeInfo: corev1.NodeSystemInfo{
							ContainerRuntimeVersion: "cri-o://1.24.0",
						},
					},
				},
				{
					ObjectMeta: metav1.ObjectMeta{
						Name: "node3",
						Labels: map[string]string{
							"feature.node.kubernetes.io/pci-10de.present": "true",
							"nvidia.com/gpu.present":                      "true",
						},
					},
					Status: corev1.NodeStatus{
						NodeInfo: corev1.NodeSystemInfo{
							ContainerRuntimeVersion: "containerd://1.7.0",
						},
					},
				},
			},
			openshift: "",
			expectedResult: map[string]gpuv1.Runtime{
				"node1": gpuv1.Containerd,
				"node2": gpuv1.CRIO,
				"node3": gpuv1.Containerd,
			},
			expectError: false,
		},
		{
			name: "openshift cluster - all nodes should be cri-o",
			nodes: []corev1.Node{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name: "node1",
						Labels: map[string]string{
							"feature.node.kubernetes.io/pci-10de.present": "true",
							"nvidia.com/gpu.present":                      "true",
						},
					},
					Status: corev1.NodeStatus{
						NodeInfo: corev1.NodeSystemInfo{
							ContainerRuntimeVersion: "containerd://1.6.0", // Should be ignored
						},
					},
				},
				{
					ObjectMeta: metav1.ObjectMeta{
						Name: "node2",
						Labels: map[string]string{
							"feature.node.kubernetes.io/pci-10de.present": "true",
							"nvidia.com/gpu.present":                      "true",
						},
					},
					Status: corev1.NodeStatus{
						NodeInfo: corev1.NodeSystemInfo{
							ContainerRuntimeVersion: "cri-o://1.24.0",
						},
					},
				},
			},
			openshift: "4.12.0",
			expectedResult: map[string]gpuv1.Runtime{
				"node1": gpuv1.CRIO,
				"node2": gpuv1.CRIO,
			},
			expectError: false,
		},
		{
			name: "unknown runtime falls back to containerd",
			nodes: []corev1.Node{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name: "node1",
						Labels: map[string]string{
							"feature.node.kubernetes.io/pci-10de.present": "true",
							"nvidia.com/gpu.present":                      "true",
						},
					},
					Status: corev1.NodeStatus{
						NodeInfo: corev1.NodeSystemInfo{
							ContainerRuntimeVersion: "unknown://1.0.0",
						},
					},
				},
				{
					ObjectMeta: metav1.ObjectMeta{
						Name: "node2",
						Labels: map[string]string{
							"feature.node.kubernetes.io/pci-10de.present": "true",
							"nvidia.com/gpu.present":                      "true",
						},
					},
					Status: corev1.NodeStatus{
						NodeInfo: corev1.NodeSystemInfo{
							ContainerRuntimeVersion: "containerd://1.6.0",
						},
					},
				},
			},
			openshift: "",
			expectedResult: map[string]gpuv1.Runtime{
				"node1": gpuv1.Containerd, // Falls back to containerd
				"node2": gpuv1.Containerd,
			},
			expectError: false,
		},
		{
			name: "docker runtime support",
			nodes: []corev1.Node{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name: "node1",
						Labels: map[string]string{
							"feature.node.kubernetes.io/pci-10de.present": "true",
							"nvidia.com/gpu.present":                      "true",
						},
					},
					Status: corev1.NodeStatus{
						NodeInfo: corev1.NodeSystemInfo{
							ContainerRuntimeVersion: "docker://20.10.0",
						},
					},
				},
			},
			openshift: "",
			expectedResult: map[string]gpuv1.Runtime{
				"node1": gpuv1.Docker,
			},
			expectError: false,
		},
		{
			name:           "no GPU nodes",
			nodes:          []corev1.Node{},
			openshift:      "",
			expectedResult: map[string]gpuv1.Runtime{},
			expectError:    false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Create fake client with test nodes
			scheme := runtime.NewScheme()
			_ = corev1.AddToScheme(scheme)

			objs := make([]runtime.Object, len(tt.nodes))
			for i := range tt.nodes {
				objs[i] = &tt.nodes[i]
			}

			client := fake.NewClientBuilder().WithScheme(scheme).WithRuntimeObjects(objs...).Build()

			// Create controller with test data
			controller := &ClusterPolicyController{
				client:    client,
				logger:    log.Log.WithName("test"),
				ctx:       context.Background(),
				openshift: tt.openshift,
			}

			// Call the method under test
			result, err := controller.getNodeRuntimeMap()

			// Verify results
			if tt.expectError {
				require.Error(t, err)
			} else {
				require.NoError(t, err)
				require.Equal(t, tt.expectedResult, result)
			}
		})
	}
}

func TestLabelNodeWithRuntime(t *testing.T) {
	tests := []struct {
		name           string
		node           corev1.Node
		openshift      string
		expectedLabels map[string]string
	}{
		{
			name: "label GPU node with containerd runtime",
			node: corev1.Node{
				ObjectMeta: metav1.ObjectMeta{
					Name: "node1",
					Labels: map[string]string{
						"feature.node.kubernetes.io/pci-10de.present": "true",
					},
				},
				Status: corev1.NodeStatus{
					NodeInfo: corev1.NodeSystemInfo{
						ContainerRuntimeVersion: "containerd://1.6.0",
					},
				},
			},
			openshift: "",
			expectedLabels: map[string]string{
				"nvidia.com/gpu.runtime.containerd": "true",
			},
		},
		{
			name: "label GPU node with cri-o runtime",
			node: corev1.Node{
				ObjectMeta: metav1.ObjectMeta{
					Name: "node2",
					Labels: map[string]string{
						"feature.node.kubernetes.io/pci-10de.present": "true",
					},
				},
				Status: corev1.NodeStatus{
					NodeInfo: corev1.NodeSystemInfo{
						ContainerRuntimeVersion: "cri-o://1.24.0",
					},
				},
			},
			openshift: "",
			expectedLabels: map[string]string{
				"nvidia.com/gpu.runtime.crio": "true",
			},
		},
		{
			name: "remove old runtime labels when runtime changes",
			node: corev1.Node{
				ObjectMeta: metav1.ObjectMeta{
					Name: "node1",
					Labels: map[string]string{
						"feature.node.kubernetes.io/pci-10de.present": "true",
						"nvidia.com/gpu.runtime.crio":                  "true", // Old label
					},
				},
				Status: corev1.NodeStatus{
					NodeInfo: corev1.NodeSystemInfo{
						ContainerRuntimeVersion: "containerd://1.6.0", // New runtime
					},
				},
			},
			openshift: "",
			expectedLabels: map[string]string{
				"nvidia.com/gpu.runtime.containerd": "true",
				// crio label should be removed
			},
		},
		{
			name: "openshift cluster forces crio runtime",
			node: corev1.Node{
				ObjectMeta: metav1.ObjectMeta{
					Name: "openshift-node",
					Labels: map[string]string{
						"feature.node.kubernetes.io/pci-10de.present": "true",
					},
				},
				Status: corev1.NodeStatus{
					NodeInfo: corev1.NodeSystemInfo{
						ContainerRuntimeVersion: "containerd://1.6.0", // Should be ignored
					},
				},
			},
			openshift: "4.12.0",
			expectedLabels: map[string]string{
				"nvidia.com/gpu.runtime.crio": "true",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Create fake client with test node
			scheme := runtime.NewScheme()
			_ = corev1.AddToScheme(scheme)

			client := fake.NewClientBuilder().WithScheme(scheme).WithRuntimeObjects(&tt.node).Build()

			// Call the function under test
			err := labelNodeWithRuntime(&tt.node, client, tt.openshift, log.Log.WithName("test"))
			require.NoError(t, err)

			// Verify labels were applied correctly
			node := &corev1.Node{}
			err = client.Get(context.Background(), types.NamespacedName{Name: tt.node.Name}, node)
			require.NoError(t, err)

			// Check expected labels are present
			for key, value := range tt.expectedLabels {
				require.Equal(t, value, node.Labels[key], "Expected label %s=%s on node %s", key, value, tt.node.Name)
			}

			// Check that other runtime labels are removed
			for _, runtime := range []gpuv1.Runtime{gpuv1.Docker, gpuv1.CRIO, gpuv1.Containerd} {
				runtimeLabel := "nvidia.com/gpu.runtime." + runtime.String()
				if _, shouldExist := tt.expectedLabels[runtimeLabel]; !shouldExist {
					require.NotContains(t, node.Labels, runtimeLabel, "Runtime label %s should not exist on node %s", runtimeLabel, tt.node.Name)
				}
			}
		})
	}
}

