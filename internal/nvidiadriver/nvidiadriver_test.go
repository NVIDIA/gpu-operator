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

package nvidiadriver

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	nvidiav1alpha1 "github.com/NVIDIA/gpu-operator/api/nvidia/v1alpha1"
	"github.com/NVIDIA/gpu-operator/internal/consts"
)

func TestAssignNVIDIADriverOwnersGivesSpecificDriversPrecedence(t *testing.T) {
	scheme := runtime.NewScheme()
	require.NoError(t, nvidiav1alpha1.AddToScheme(scheme))
	require.NoError(t, corev1.AddToScheme(scheme))

	defaultDriver := &nvidiav1alpha1.NVIDIADriver{
		ObjectMeta: metav1.ObjectMeta{Name: consts.DefaultNVIDIADriverName},
		Spec:       nvidiav1alpha1.NVIDIADriverSpec{Default: true},
	}
	specificDriver := &nvidiav1alpha1.NVIDIADriver{
		ObjectMeta: metav1.ObjectMeta{Name: "h100-driver"},
		Spec: nvidiav1alpha1.NVIDIADriverSpec{
			NodeSelector: map[string]string{"nodepool": "h100"},
		},
	}
	defaultNode := &corev1.Node{ObjectMeta: metav1.ObjectMeta{
		Name:   "default-node",
		Labels: map[string]string{consts.GPUPresentLabel: "true"},
	}}
	specificNode := &corev1.Node{ObjectMeta: metav1.ObjectMeta{
		Name:   "specific-node",
		Labels: map[string]string{consts.GPUPresentLabel: "true", "nodepool": "h100"},
	}}

	k8sClient := fake.NewClientBuilder().WithScheme(scheme).WithObjects(defaultDriver, specificDriver, defaultNode, specificNode).Build()

	changed, err := AssignOwners(context.Background(), k8sClient)
	require.NoError(t, err)
	require.True(t, changed)

	require.NoError(t, k8sClient.Get(context.Background(), client.ObjectKey{Name: "default-node"}, defaultNode))
	require.NoError(t, k8sClient.Get(context.Background(), client.ObjectKey{Name: "specific-node"}, specificNode))
	require.Equal(t, consts.DefaultNVIDIADriverName, defaultNode.Labels[consts.NVIDIADriverOwnerLabel])
	require.Equal(t, "h100-driver", specificNode.Labels[consts.NVIDIADriverOwnerLabel])
}

func TestAssignNVIDIADriverOwnersAllowsMissingDefaultDriver(t *testing.T) {
	scheme := runtime.NewScheme()
	require.NoError(t, nvidiav1alpha1.AddToScheme(scheme))
	require.NoError(t, corev1.AddToScheme(scheme))

	specificDriver := &nvidiav1alpha1.NVIDIADriver{
		ObjectMeta: metav1.ObjectMeta{Name: "h100-driver"},
		Spec: nvidiav1alpha1.NVIDIADriverSpec{
			NodeSelector: map[string]string{"nodepool": "h100"},
		},
	}
	unmatchedNode := &corev1.Node{ObjectMeta: metav1.ObjectMeta{
		Name:   "unmatched-node",
		Labels: map[string]string{consts.GPUPresentLabel: "true", consts.NVIDIADriverOwnerLabel: consts.DefaultNVIDIADriverName},
	}}
	specificNode := &corev1.Node{ObjectMeta: metav1.ObjectMeta{
		Name:   "specific-node",
		Labels: map[string]string{consts.GPUPresentLabel: "true", "nodepool": "h100"},
	}}

	k8sClient := fake.NewClientBuilder().WithScheme(scheme).WithObjects(specificDriver, unmatchedNode, specificNode).Build()

	changed, err := AssignOwners(context.Background(), k8sClient)
	require.NoError(t, err)
	require.True(t, changed)

	require.NoError(t, k8sClient.Get(context.Background(), client.ObjectKey{Name: "unmatched-node"}, unmatchedNode))
	require.NoError(t, k8sClient.Get(context.Background(), client.ObjectKey{Name: "specific-node"}, specificNode))
	require.NotContains(t, unmatchedNode.Labels, consts.NVIDIADriverOwnerLabel)
	require.Equal(t, "h100-driver", specificNode.Labels[consts.NVIDIADriverOwnerLabel])
}

func TestAssignNVIDIADriverOwnersIgnoresDeletingDrivers(t *testing.T) {
	scheme := runtime.NewScheme()
	require.NoError(t, nvidiav1alpha1.AddToScheme(scheme))
	require.NoError(t, corev1.AddToScheme(scheme))

	deleteTime := metav1.Now()
	deletingDriver := &nvidiav1alpha1.NVIDIADriver{
		ObjectMeta: metav1.ObjectMeta{
			Name:              "demo-gold",
			DeletionTimestamp: &deleteTime,
			Finalizers:        []string{"test-finalizer"},
		},
		Spec: nvidiav1alpha1.NVIDIADriverSpec{
			NodeSelector: map[string]string{"nodepool": "gold"},
		},
	}
	node := &corev1.Node{ObjectMeta: metav1.ObjectMeta{
		Name: "gpu-node",
		Labels: map[string]string{
			consts.GPUPresentLabel:        "true",
			consts.NVIDIADriverOwnerLabel: "demo-gold",
			"nodepool":                    "gold",
		},
	}}

	k8sClient := fake.NewClientBuilder().WithScheme(scheme).WithObjects(deletingDriver, node).Build()

	changed, err := AssignOwners(context.Background(), k8sClient)
	require.NoError(t, err)
	require.True(t, changed)

	require.NoError(t, k8sClient.Get(context.Background(), client.ObjectKey{Name: "gpu-node"}, node))
	require.NotContains(t, node.Labels, consts.NVIDIADriverOwnerLabel)
}

func TestAssignNVIDIADriverOwnersUsesDefaultDriverWithArbitraryName(t *testing.T) {
	scheme := runtime.NewScheme()
	require.NoError(t, nvidiav1alpha1.AddToScheme(scheme))
	require.NoError(t, corev1.AddToScheme(scheme))

	defaultDriver := &nvidiav1alpha1.NVIDIADriver{
		ObjectMeta: metav1.ObjectMeta{Name: "fallback-driver"},
		Spec:       nvidiav1alpha1.NVIDIADriverSpec{Default: true},
	}
	node := &corev1.Node{ObjectMeta: metav1.ObjectMeta{
		Name:   "gpu-node",
		Labels: map[string]string{consts.GPUPresentLabel: "true"},
	}}

	k8sClient := fake.NewClientBuilder().WithScheme(scheme).WithObjects(defaultDriver, node).Build()

	changed, err := AssignOwners(context.Background(), k8sClient)
	require.NoError(t, err)
	require.True(t, changed)

	require.NoError(t, k8sClient.Get(context.Background(), client.ObjectKey{Name: "gpu-node"}, node))
	require.Equal(t, "fallback-driver", node.Labels[consts.NVIDIADriverOwnerLabel])
}

func TestAssignNVIDIADriverOwnersReturnsFalseWhenOwnersAreCurrent(t *testing.T) {
	scheme := runtime.NewScheme()
	require.NoError(t, nvidiav1alpha1.AddToScheme(scheme))
	require.NoError(t, corev1.AddToScheme(scheme))

	defaultDriver := &nvidiav1alpha1.NVIDIADriver{
		ObjectMeta: metav1.ObjectMeta{Name: consts.DefaultNVIDIADriverName},
		Spec:       nvidiav1alpha1.NVIDIADriverSpec{Default: true},
	}
	specificDriver := &nvidiav1alpha1.NVIDIADriver{
		ObjectMeta: metav1.ObjectMeta{Name: "h100-driver"},
		Spec: nvidiav1alpha1.NVIDIADriverSpec{
			NodeSelector: map[string]string{"nodepool": "h100"},
		},
	}
	defaultNode := &corev1.Node{ObjectMeta: metav1.ObjectMeta{
		Name: "default-node",
		Labels: map[string]string{
			consts.GPUPresentLabel:        "true",
			consts.NVIDIADriverOwnerLabel: consts.DefaultNVIDIADriverName,
		},
	}}
	specificNode := &corev1.Node{ObjectMeta: metav1.ObjectMeta{
		Name: "specific-node",
		Labels: map[string]string{
			consts.GPUPresentLabel:        "true",
			consts.NVIDIADriverOwnerLabel: "h100-driver",
			"nodepool":                    "h100",
		},
	}}

	k8sClient := fake.NewClientBuilder().WithScheme(scheme).WithObjects(defaultDriver, specificDriver, defaultNode, specificNode).Build()

	changed, err := AssignOwners(context.Background(), k8sClient)
	require.NoError(t, err)
	require.False(t, changed)
}

func TestAssignNVIDIADriverOwnersErrorsOnMultipleDefaultDrivers(t *testing.T) {
	scheme := runtime.NewScheme()
	require.NoError(t, nvidiav1alpha1.AddToScheme(scheme))
	require.NoError(t, corev1.AddToScheme(scheme))

	defaultDriverA := &nvidiav1alpha1.NVIDIADriver{
		ObjectMeta: metav1.ObjectMeta{Name: "fallback-a"},
		Spec:       nvidiav1alpha1.NVIDIADriverSpec{Default: true},
	}
	defaultDriverB := &nvidiav1alpha1.NVIDIADriver{
		ObjectMeta: metav1.ObjectMeta{Name: "fallback-b"},
		Spec:       nvidiav1alpha1.NVIDIADriverSpec{Default: true},
	}
	node := &corev1.Node{ObjectMeta: metav1.ObjectMeta{
		Name:   "gpu-node",
		Labels: map[string]string{consts.GPUPresentLabel: "true"},
	}}

	k8sClient := fake.NewClientBuilder().WithScheme(scheme).WithObjects(defaultDriverA, defaultDriverB, node).Build()

	changed, err := AssignOwners(context.Background(), k8sClient)
	require.Error(t, err)
	require.False(t, changed)
	require.Contains(t, err.Error(), "multiple default NVIDIADrivers found")

	require.NoError(t, k8sClient.Get(context.Background(), client.ObjectKey{Name: "gpu-node"}, node))
	require.NotContains(t, node.Labels, consts.NVIDIADriverOwnerLabel)
}

func TestAssignNVIDIADriverOwnersRejectsReservedOwnerLabelSelector(t *testing.T) {
	scheme := runtime.NewScheme()
	require.NoError(t, nvidiav1alpha1.AddToScheme(scheme))
	require.NoError(t, corev1.AddToScheme(scheme))

	driver := &nvidiav1alpha1.NVIDIADriver{
		ObjectMeta: metav1.ObjectMeta{Name: "bad-driver"},
		Spec: nvidiav1alpha1.NVIDIADriverSpec{
			NodeSelector: map[string]string{consts.NVIDIADriverOwnerLabel: "other-driver"},
		},
	}
	node := &corev1.Node{ObjectMeta: metav1.ObjectMeta{
		Name: "gpu-node",
		Labels: map[string]string{
			consts.GPUPresentLabel:        "true",
			consts.NVIDIADriverOwnerLabel: "existing-driver",
		},
	}}

	k8sClient := fake.NewClientBuilder().WithScheme(scheme).WithObjects(driver, node).Build()

	changed, err := AssignOwners(context.Background(), k8sClient)
	require.Error(t, err)
	require.False(t, changed)
	require.Contains(t, err.Error(), "reserved label")
	require.Contains(t, err.Error(), consts.NVIDIADriverOwnerLabel)

	require.NoError(t, k8sClient.Get(context.Background(), client.ObjectKey{Name: "gpu-node"}, node))
	require.Equal(t, "existing-driver", node.Labels[consts.NVIDIADriverOwnerLabel])
}

func TestAssignNVIDIADriverOwnersRejectsDefaultDriverNodeSelector(t *testing.T) {
	scheme := runtime.NewScheme()
	require.NoError(t, nvidiav1alpha1.AddToScheme(scheme))
	require.NoError(t, corev1.AddToScheme(scheme))

	defaultDriver := &nvidiav1alpha1.NVIDIADriver{
		ObjectMeta: metav1.ObjectMeta{Name: consts.DefaultNVIDIADriverName},
		Spec: nvidiav1alpha1.NVIDIADriverSpec{
			Default:      true,
			NodeSelector: map[string]string{"driver-default": "true"},
		},
	}
	specificDriver := &nvidiav1alpha1.NVIDIADriver{
		ObjectMeta: metav1.ObjectMeta{Name: "h100-driver"},
		Spec: nvidiav1alpha1.NVIDIADriverSpec{
			NodeSelector: map[string]string{"nodepool": "h100"},
		},
	}
	defaultNode := &corev1.Node{ObjectMeta: metav1.ObjectMeta{
		Name:   "default-node",
		Labels: map[string]string{consts.GPUPresentLabel: "true", "driver-default": "true"},
	}}
	unmatchedNode := &corev1.Node{ObjectMeta: metav1.ObjectMeta{
		Name:   "unmatched-node",
		Labels: map[string]string{consts.GPUPresentLabel: "true", consts.NVIDIADriverOwnerLabel: consts.DefaultNVIDIADriverName},
	}}
	specificNode := &corev1.Node{ObjectMeta: metav1.ObjectMeta{
		Name:   "specific-node",
		Labels: map[string]string{consts.GPUPresentLabel: "true", "driver-default": "true", "nodepool": "h100"},
	}}

	k8sClient := fake.NewClientBuilder().WithScheme(scheme).WithObjects(defaultDriver, specificDriver, defaultNode, unmatchedNode, specificNode).Build()

	changed, err := AssignOwners(context.Background(), k8sClient)
	require.Error(t, err)
	require.False(t, changed)
	require.Contains(t, err.Error(), "default NVIDIADriver")
	require.Contains(t, err.Error(), "cannot use nodeSelector")

	require.NoError(t, k8sClient.Get(context.Background(), client.ObjectKey{Name: "default-node"}, defaultNode))
	require.NoError(t, k8sClient.Get(context.Background(), client.ObjectKey{Name: "unmatched-node"}, unmatchedNode))
	require.NoError(t, k8sClient.Get(context.Background(), client.ObjectKey{Name: "specific-node"}, specificNode))
	require.NotContains(t, defaultNode.Labels, consts.NVIDIADriverOwnerLabel)
	require.Equal(t, consts.DefaultNVIDIADriverName, unmatchedNode.Labels[consts.NVIDIADriverOwnerLabel])
	require.NotContains(t, specificNode.Labels, consts.NVIDIADriverOwnerLabel)
}

func TestAssignNVIDIADriverOwnersDoesNotFallbackToDefaultOnUserDriverConflict(t *testing.T) {
	scheme := runtime.NewScheme()
	require.NoError(t, nvidiav1alpha1.AddToScheme(scheme))
	require.NoError(t, corev1.AddToScheme(scheme))

	defaultDriver := &nvidiav1alpha1.NVIDIADriver{
		ObjectMeta: metav1.ObjectMeta{Name: consts.DefaultNVIDIADriverName},
		Spec:       nvidiav1alpha1.NVIDIADriverSpec{Default: true},
	}
	driverA := &nvidiav1alpha1.NVIDIADriver{
		ObjectMeta: metav1.ObjectMeta{Name: "driver-a"},
		Spec: nvidiav1alpha1.NVIDIADriverSpec{
			NodeSelector: map[string]string{"nodepool": "shared"},
		},
	}
	driverB := &nvidiav1alpha1.NVIDIADriver{
		ObjectMeta: metav1.ObjectMeta{Name: "driver-b"},
		Spec: nvidiav1alpha1.NVIDIADriverSpec{
			NodeSelector: map[string]string{"nodepool": "shared"},
		},
	}
	conflictedNode := &corev1.Node{ObjectMeta: metav1.ObjectMeta{
		Name: "conflicted-node",
		Labels: map[string]string{
			consts.GPUPresentLabel:        "true",
			consts.NVIDIADriverOwnerLabel: "driver-a",
			"nodepool":                    "shared",
		},
	}}

	k8sClient := fake.NewClientBuilder().WithScheme(scheme).WithObjects(defaultDriver, driverA, driverB, conflictedNode).Build()

	changed, err := AssignOwners(context.Background(), k8sClient)
	require.Error(t, err)
	require.False(t, changed)
	require.Contains(t, err.Error(), "multiple NVIDIADrivers match the same node")

	require.NoError(t, k8sClient.Get(context.Background(), client.ObjectKey{Name: "conflicted-node"}, conflictedNode))
	require.Equal(t, "driver-a", conflictedNode.Labels[consts.NVIDIADriverOwnerLabel])
}

func TestAssignNVIDIADriverOwnersDoesNotChangeOwnersWhenAnyUserDriverConflicts(t *testing.T) {
	scheme := runtime.NewScheme()
	require.NoError(t, nvidiav1alpha1.AddToScheme(scheme))
	require.NoError(t, corev1.AddToScheme(scheme))

	defaultDriver := &nvidiav1alpha1.NVIDIADriver{
		ObjectMeta: metav1.ObjectMeta{Name: consts.DefaultNVIDIADriverName},
		Spec:       nvidiav1alpha1.NVIDIADriverSpec{Default: true},
	}
	goldDriver := &nvidiav1alpha1.NVIDIADriver{
		ObjectMeta: metav1.ObjectMeta{Name: "demo-gold"},
		Spec: nvidiav1alpha1.NVIDIADriverSpec{
			NodeSelector: map[string]string{"region": "us-east-1"},
		},
	}
	silverDriver := &nvidiav1alpha1.NVIDIADriver{
		ObjectMeta: metav1.ObjectMeta{Name: "demo-silver"},
	}
	goldNode := &corev1.Node{ObjectMeta: metav1.ObjectMeta{
		Name: "gold-node",
		Labels: map[string]string{
			consts.GPUPresentLabel:        "true",
			consts.NVIDIADriverOwnerLabel: "demo-gold",
			"region":                      "us-east-1",
		},
	}}
	defaultNode := &corev1.Node{ObjectMeta: metav1.ObjectMeta{
		Name: "default-node",
		Labels: map[string]string{
			consts.GPUPresentLabel:        "true",
			consts.NVIDIADriverOwnerLabel: consts.DefaultNVIDIADriverName,
			"region":                      "us-east-2",
		},
	}}

	k8sClient := fake.NewClientBuilder().WithScheme(scheme).WithObjects(defaultDriver, goldDriver, silverDriver, goldNode, defaultNode).Build()

	changed, err := AssignOwners(context.Background(), k8sClient)
	require.Error(t, err)
	require.False(t, changed)
	require.Contains(t, err.Error(), "multiple NVIDIADrivers match the same node")

	require.NoError(t, k8sClient.Get(context.Background(), client.ObjectKey{Name: "gold-node"}, goldNode))
	require.NoError(t, k8sClient.Get(context.Background(), client.ObjectKey{Name: "default-node"}, defaultNode))
	require.Equal(t, "demo-gold", goldNode.Labels[consts.NVIDIADriverOwnerLabel])
	require.Equal(t, consts.DefaultNVIDIADriverName, defaultNode.Labels[consts.NVIDIADriverOwnerLabel])
}
