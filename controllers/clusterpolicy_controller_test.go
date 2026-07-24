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

	"github.com/NVIDIA/k8s-operator-libs/pkg/upgrade"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	gpuconsts "github.com/NVIDIA/gpu-operator/internal/consts"
)

func TestIsIncompleteDriverUpgradeState(t *testing.T) {
	tests := []struct {
		name     string
		state    string
		expected bool
	}{
		{
			name:     "unknown state is inactive",
			state:    upgrade.UpgradeStateUnknown,
			expected: false,
		},
		{
			name:     "upgrade required is pending and in progress",
			state:    upgrade.UpgradeStateUpgradeRequired,
			expected: true,
		},
		{
			name:     "done is inactive",
			state:    upgrade.UpgradeStateDone,
			expected: false,
		},
		{
			name:     "failed is incomplete",
			state:    upgrade.UpgradeStateFailed,
			expected: true,
		},
		{
			name:     "pod restart required is active",
			state:    upgrade.UpgradeStatePodRestartRequired,
			expected: true,
		},
		{
			name:     "uncordon required is active",
			state:    upgrade.UpgradeStateUncordonRequired,
			expected: true,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			require.Equal(t, tc.expected, isIncompleteDriverUpgradeState(tc.state))
		})
	}
}

func TestClusterPolicyNotReadyMessage(t *testing.T) {
	tests := []struct {
		name              string
		statesNotReady    []string
		upgradeInProgress bool
		expected          string
	}{
		{
			name:              "driver upgrade only",
			upgradeInProgress: true,
			expected:          "ClusterPolicy is not ready; NVIDIADriver upgrade has not completed; one or more NVIDIADriver-owned Nodes are marked pending, in-progress, or failed",
		},
		{
			name:              "not ready states and driver upgrade",
			statesNotReady:    []string{"state-container-toolkit", "state-device-plugin"},
			upgradeInProgress: true,
			expected:          "ClusterPolicy is not ready; states not ready: [state-container-toolkit state-device-plugin]; NVIDIADriver upgrade has not completed; one or more NVIDIADriver-owned Nodes are marked pending, in-progress, or failed",
		},
		{
			name:           "not ready states only",
			statesNotReady: []string{"state-container-toolkit"},
			expected:       "ClusterPolicy is not ready; states not ready: [state-container-toolkit]",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			require.Equal(t, tc.expected, clusterPolicyNotReadyMessage(tc.statesNotReady, tc.upgradeInProgress))
		})
	}
}

func TestNVIDIADriverUpgradeIncomplete(t *testing.T) {
	upgradeStateLabel := upgrade.GetUpgradeStateLabelKey()

	tests := []struct {
		name     string
		nodes    []client.Object
		expected bool
	}{
		{
			name: "active upgrade state on NVIDIADriver-owned node",
			nodes: []client.Object{
				nodeWithLabels("gpu-node", map[string]string{
					gpuconsts.NVIDIADriverOwnerLabel: "default",
					upgradeStateLabel:                upgrade.UpgradeStatePodRestartRequired,
				}),
			},
			expected: true,
		},
		{
			name: "pending upgrade keeps rollout in progress after another node completes",
			nodes: []client.Object{
				nodeWithLabels("upgraded-gpu-node", map[string]string{
					gpuconsts.NVIDIADriverOwnerLabel: "default",
					upgradeStateLabel:                upgrade.UpgradeStateDone,
				}),
				nodeWithLabels("pending-gpu-node", map[string]string{
					gpuconsts.NVIDIADriverOwnerLabel: "default",
					upgradeStateLabel:                upgrade.UpgradeStateUpgradeRequired,
				}),
			},
			expected: true,
		},
		{
			name: "active upgrade state on unowned node is ignored",
			nodes: []client.Object{
				nodeWithLabels("gpu-node", map[string]string{
					upgradeStateLabel: upgrade.UpgradeStatePodRestartRequired,
				}),
			},
			expected: false,
		},
		{
			name: "failed upgrade state keeps rollout incomplete",
			nodes: []client.Object{
				nodeWithLabels("gpu-node", map[string]string{
					gpuconsts.NVIDIADriverOwnerLabel: "default",
					upgradeStateLabel:                upgrade.UpgradeStateFailed,
				}),
			},
			expected: true,
		},
		{
			name: "completed upgrade state is not treated as in progress",
			nodes: []client.Object{
				nodeWithLabels("gpu-node", map[string]string{
					gpuconsts.NVIDIADriverOwnerLabel: "default",
					upgradeStateLabel:                upgrade.UpgradeStateDone,
				}),
			},
			expected: false,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			scheme := runtime.NewScheme()
			require.NoError(t, corev1.AddToScheme(scheme))
			reconciler := &ClusterPolicyReconciler{
				Client: fake.NewClientBuilder().WithScheme(scheme).WithObjects(tc.nodes...).Build(),
			}

			actual, err := reconciler.nvidiaDriverUpgradeIncomplete(context.Background())
			require.NoError(t, err)
			require.Equal(t, tc.expected, actual)
		})
	}
}

func nodeWithLabels(name string, labels map[string]string) *corev1.Node {
	return &corev1.Node{
		ObjectMeta: metav1.ObjectMeta{
			Name:   name,
			Labels: labels,
		},
	}
}
