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
	"context"
	"testing"

	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes/scheme"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	nvidiav1alpha1 "github.com/NVIDIA/gpu-operator/api/nvidia/v1alpha1"
	"github.com/NVIDIA/gpu-operator/internal/consts"
)

func TestGetOSTag(t *testing.T) {
	tests := []struct {
		description  string
		osRelease    string
		osVersion    string
		expected     string
		expectError  bool
		errorMessage string
	}{
		{
			description: "valid os release & version",
			osRelease:   "rhel",
			osVersion:   "9.4",
			expected:    "rhel9",
			expectError: false,
		},
		{
			description: "valid os release & version - rhel8",
			osRelease:   "rhel",
			osVersion:   "8.10",
			expected:    "rhel8",
			expectError: false,
		},
		{
			description: "valid os release & version - ubuntu",
			osRelease:   "ubuntu",
			osVersion:   "24.04",
			expected:    "ubuntu24.04",
			expectError: false,
		},
		{
			description: "rocky linux",
			osRelease:   "rocky",
			osVersion:   "9.4",
			expected:    "rocky9",
			expectError: false,
		},
		{
			description: "RHEL 10",
			osRelease:   "rhel",
			osVersion:   "10.1",
			expected:    "rhel10",
			expectError: false,
		},
		{
			description: "talos version with v prefix",
			osRelease:   "talos",
			osVersion:   "v1.12.6",
			expected:    "talosv1.12.6",
			expectError: false,
		},
		{
			description: "archlinux rolling version",
			osRelease:   "archlinux",
			osVersion:   "rolling",
			expected:    "archlinuxrolling",
			expectError: false,
		},
	}

	for _, test := range tests {
		t.Run(test.description, func(t *testing.T) {
			actual, err := getOSTag(test.osRelease, test.osVersion)
			if test.expectError {
				require.Error(t, err)
				require.Equal(t, test.errorMessage, err.Error())
			} else {
				require.NoError(t, err)
			}
			require.Equal(t, test.expected, actual)
		})
	}
}

func TestGetNodePoolsGroupsNodesByOSTag(t *testing.T) {
	require.NoError(t, corev1.AddToScheme(scheme.Scheme))

	k8sClient := fake.NewClientBuilder().
		WithScheme(scheme.Scheme).
		WithObjects(
			&corev1.Node{ObjectMeta: metav1.ObjectMeta{
				Name: "rhel-node",
				Labels: map[string]string{
					"pool":                        "gold",
					consts.GPUPresentLabel:        "true",
					consts.NVIDIADriverOwnerLabel: "driver-a",
					nfdOSReleaseIDLabelKey:        "rhel",
					nfdOSVersionIDLabelKey:        "9.4",
				},
			}},
			&corev1.Node{ObjectMeta: metav1.ObjectMeta{
				Name: "ubuntu-node",
				Labels: map[string]string{
					"pool":                        "gold",
					consts.GPUPresentLabel:        "true",
					consts.NVIDIADriverOwnerLabel: "driver-a",
					nfdOSReleaseIDLabelKey:        "ubuntu",
					nfdOSVersionIDLabelKey:        "22.04",
				},
			}},
			&corev1.Node{ObjectMeta: metav1.ObjectMeta{
				Name: "other-pool-node",
				Labels: map[string]string{
					"pool":                        "silver",
					consts.GPUPresentLabel:        "true",
					consts.NVIDIADriverOwnerLabel: "driver-a",
					nfdOSReleaseIDLabelKey:        "ubuntu",
					nfdOSVersionIDLabelKey:        "20.04",
				},
			}},
		).
		Build()
	driver := &nvidiav1alpha1.NVIDIADriver{
		ObjectMeta: metav1.ObjectMeta{Name: "driver-a"},
		Spec: nvidiav1alpha1.NVIDIADriverSpec{
			NodeSelector: map[string]string{"pool": "gold"},
		},
	}

	nodePools, err := getNodePools(context.Background(), k8sClient, driver, false)

	require.NoError(t, err)
	require.Len(t, nodePools, 2)

	poolsByName := nodePoolsByName(nodePools)
	require.Contains(t, poolsByName, "rhel9")
	require.Equal(t, "rhel", poolsByName["rhel9"].osRelease)
	require.Equal(t, "9.4", poolsByName["rhel9"].osVersion)
	require.Equal(t, "gold", poolsByName["rhel9"].nodeSelector["pool"])
	require.Equal(t, "driver-a", poolsByName["rhel9"].nodeSelector[consts.NVIDIADriverOwnerLabel])

	require.Contains(t, poolsByName, "ubuntu22.04")
	require.Equal(t, "ubuntu", poolsByName["ubuntu22.04"].osRelease)
	require.Equal(t, "22.04", poolsByName["ubuntu22.04"].osVersion)
}

func TestGetNodePoolsSkipsNodesMissingNFDOSLabels(t *testing.T) {
	require.NoError(t, corev1.AddToScheme(scheme.Scheme))

	k8sClient := fake.NewClientBuilder().
		WithScheme(scheme.Scheme).
		WithObjects(
			&corev1.Node{ObjectMeta: metav1.ObjectMeta{
				Name: "missing-os-release",
				Labels: map[string]string{
					consts.GPUPresentLabel:        "true",
					consts.NVIDIADriverOwnerLabel: "driver-a",
					nfdOSVersionIDLabelKey:        "9.4",
				},
			}},
			&corev1.Node{ObjectMeta: metav1.ObjectMeta{
				Name: "missing-os-version",
				Labels: map[string]string{
					consts.GPUPresentLabel:        "true",
					consts.NVIDIADriverOwnerLabel: "driver-a",
					nfdOSReleaseIDLabelKey:        "rhel",
				},
			}},
		).
		Build()
	driver := &nvidiav1alpha1.NVIDIADriver{
		ObjectMeta: metav1.ObjectMeta{Name: "driver-a"},
	}

	nodePools, err := getNodePools(context.Background(), k8sClient, driver, false)

	require.NoError(t, err)
	require.Empty(t, nodePools)
}

func TestGetNodePoolsPartitionsPrecompiledNodesByKernel(t *testing.T) {
	require.NoError(t, corev1.AddToScheme(scheme.Scheme))

	k8sClient := fake.NewClientBuilder().
		WithScheme(scheme.Scheme).
		WithObjects(
			&corev1.Node{ObjectMeta: metav1.ObjectMeta{
				Name: "kernel-node",
				Labels: map[string]string{
					consts.GPUPresentLabel:        "true",
					consts.NVIDIADriverOwnerLabel: "driver-a",
					nfdOSReleaseIDLabelKey:        "ubuntu",
					nfdOSVersionIDLabelKey:        "22.04",
					nfdKernelLabelKey:             "5.15.0-70-generic_x86_64",
				},
			}},
			&corev1.Node{ObjectMeta: metav1.ObjectMeta{
				Name: "missing-kernel-node",
				Labels: map[string]string{
					consts.GPUPresentLabel:        "true",
					consts.NVIDIADriverOwnerLabel: "driver-a",
					nfdOSReleaseIDLabelKey:        "ubuntu",
					nfdOSVersionIDLabelKey:        "22.04",
				},
			}},
		).
		Build()
	driver := &nvidiav1alpha1.NVIDIADriver{
		ObjectMeta: metav1.ObjectMeta{Name: "driver-a"},
		Spec: nvidiav1alpha1.NVIDIADriverSpec{
			UsePrecompiled: ptr.To(true),
		},
	}

	nodePools, err := getNodePools(context.Background(), k8sClient, driver, false)

	require.NoError(t, err)
	require.Len(t, nodePools, 1)
	require.Equal(t, "ubuntu22.04-5.15.0-70-generic", nodePools[0].name)
	require.Equal(t, "5.15.0-70-generic_x86_64", nodePools[0].kernel)
	require.Equal(t, "5.15.0-70-generic_x86_64", nodePools[0].nodeSelector[nfdKernelLabelKey])
}

func TestGetNodePoolsPartitionsOpenShiftNodesByRHCOSVersion(t *testing.T) {
	require.NoError(t, corev1.AddToScheme(scheme.Scheme))

	k8sClient := fake.NewClientBuilder().
		WithScheme(scheme.Scheme).
		WithObjects(
			&corev1.Node{ObjectMeta: metav1.ObjectMeta{
				Name: "rhcos-node",
				Labels: map[string]string{
					consts.GPUPresentLabel:        "true",
					consts.NVIDIADriverOwnerLabel: "driver-a",
					nfdOSReleaseIDLabelKey:        "rhcos",
					nfdOSVersionIDLabelKey:        "4.14",
					nfdOSTreeVersionLabelKey:      "414.92.202309282257",
				},
			}},
			&corev1.Node{ObjectMeta: metav1.ObjectMeta{
				Name: "missing-rhcos-node",
				Labels: map[string]string{
					consts.GPUPresentLabel:        "true",
					consts.NVIDIADriverOwnerLabel: "driver-a",
					nfdOSReleaseIDLabelKey:        "rhcos",
					nfdOSVersionIDLabelKey:        "4.14",
				},
			}},
		).
		Build()
	driver := &nvidiav1alpha1.NVIDIADriver{
		ObjectMeta: metav1.ObjectMeta{Name: "driver-a"},
	}

	nodePools, err := getNodePools(context.Background(), k8sClient, driver, true)

	require.NoError(t, err)
	require.Len(t, nodePools, 1)
	require.Equal(t, "414.92.202309282257", nodePools[0].name)
	require.Equal(t, "414.92.202309282257", nodePools[0].rhcosVersion)
	require.Equal(t, "414.92.202309282257", nodePools[0].nodeSelector[nfdOSTreeVersionLabelKey])
}

func nodePoolsByName(nodePools []nodePool) map[string]nodePool {
	poolsByName := make(map[string]nodePool, len(nodePools))
	for _, pool := range nodePools {
		poolsByName[pool.name] = pool
	}
	return poolsByName
}
