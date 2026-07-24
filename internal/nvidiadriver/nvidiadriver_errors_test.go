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
	"errors"
	"testing"

	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/client/interceptor"

	nvidiav1alpha1 "github.com/NVIDIA/gpu-operator/api/nvidia/v1alpha1"
	"github.com/NVIDIA/gpu-operator/internal/consts"
)

func assignOwnersScheme(t *testing.T) *runtime.Scheme {
	t.Helper()
	s := runtime.NewScheme()
	require.NoError(t, nvidiav1alpha1.AddToScheme(s))
	require.NoError(t, corev1.AddToScheme(s))
	return s
}

func gpuNode(name string, extraLabels map[string]string) *corev1.Node {
	labels := map[string]string{consts.GPUPresentLabel: "true"}
	for k, v := range extraLabels {
		labels[k] = v
	}
	return &corev1.Node{ObjectMeta: metav1.ObjectMeta{Name: name, Labels: labels}}
}

// TestAssignOwnersReturnsErrorWhenDriverListFails covers the failure to list
// NVIDIADriver CRs.
func TestAssignOwnersReturnsErrorWhenDriverListFails(t *testing.T) {
	c := fake.NewClientBuilder().
		WithScheme(assignOwnersScheme(t)).
		WithInterceptorFuncs(interceptor.Funcs{
			List: func(ctx context.Context, cl client.WithWatch, list client.ObjectList, opts ...client.ListOption) error {
				if _, ok := list.(*nvidiav1alpha1.NVIDIADriverList); ok {
					return errors.New("driver list boom")
				}
				return cl.List(ctx, list, opts...)
			},
		}).
		Build()

	changed, err := AssignOwners(context.Background(), c)
	require.Error(t, err)
	require.False(t, changed)
	require.Contains(t, err.Error(), "failed to list NVIDIADriver CRs")
	require.Contains(t, err.Error(), "driver list boom")
}

// TestAssignOwnersReturnsErrorWhenNodeListFails covers the failure to list GPU
// nodes, which happens after the driver list succeeds.
func TestAssignOwnersReturnsErrorWhenNodeListFails(t *testing.T) {
	defaultDriver := &nvidiav1alpha1.NVIDIADriver{
		ObjectMeta: metav1.ObjectMeta{Name: consts.DefaultNVIDIADriverName},
		Spec:       nvidiav1alpha1.NVIDIADriverSpec{Default: true},
	}

	c := fake.NewClientBuilder().
		WithScheme(assignOwnersScheme(t)).
		WithObjects(defaultDriver).
		WithInterceptorFuncs(interceptor.Funcs{
			List: func(ctx context.Context, cl client.WithWatch, list client.ObjectList, opts ...client.ListOption) error {
				if _, ok := list.(*corev1.NodeList); ok {
					return errors.New("node list boom")
				}
				return cl.List(ctx, list, opts...)
			},
		}).
		Build()

	changed, err := AssignOwners(context.Background(), c)
	require.Error(t, err)
	require.False(t, changed)
	require.Contains(t, err.Error(), "failed to list GPU nodes")
	require.Contains(t, err.Error(), "node list boom")
}

// TestAssignOwnersReturnsErrorWhenOwnerLabelUpdateFails covers the patch-failure
// path when an owner label is being added/updated (desiredOwner != "").
func TestAssignOwnersReturnsErrorWhenOwnerLabelUpdateFails(t *testing.T) {
	defaultDriver := &nvidiav1alpha1.NVIDIADriver{
		ObjectMeta: metav1.ObjectMeta{Name: consts.DefaultNVIDIADriverName},
		Spec:       nvidiav1alpha1.NVIDIADriverSpec{Default: true},
	}
	node := gpuNode("gpu-node", nil) // no owner label yet -> needs the label added

	c := fake.NewClientBuilder().
		WithScheme(assignOwnersScheme(t)).
		WithObjects(defaultDriver, node).
		WithInterceptorFuncs(interceptor.Funcs{
			Patch: func(ctx context.Context, cl client.WithWatch, obj client.Object, patch client.Patch, opts ...client.PatchOption) error {
				return errors.New("patch boom")
			},
		}).
		Build()

	changed, err := AssignOwners(context.Background(), c)
	require.Error(t, err)
	require.False(t, changed)
	require.Contains(t, err.Error(), "failed to update NVIDIADriver owner label for node \"gpu-node\"")
	require.Contains(t, err.Error(), "patch boom")
}

// TestAssignOwnersReturnsErrorWhenOwnerLabelRemovalFails covers the patch-failure
// path when a stale owner label is being removed (desiredOwner == ""). With no
// drivers present, a node carrying an owner label must have it cleared.
func TestAssignOwnersReturnsErrorWhenOwnerLabelRemovalFails(t *testing.T) {
	node := gpuNode("gpu-node", map[string]string{consts.NVIDIADriverOwnerLabel: "stale-driver"})

	c := fake.NewClientBuilder().
		WithScheme(assignOwnersScheme(t)).
		WithObjects(node).
		WithInterceptorFuncs(interceptor.Funcs{
			Patch: func(ctx context.Context, cl client.WithWatch, obj client.Object, patch client.Patch, opts ...client.PatchOption) error {
				return errors.New("patch boom")
			},
		}).
		Build()

	changed, err := AssignOwners(context.Background(), c)
	require.Error(t, err)
	require.False(t, changed)
	require.Contains(t, err.Error(), "failed to remove NVIDIADriver owner label for node \"gpu-node\"")
	require.Contains(t, err.Error(), "patch boom")
}
