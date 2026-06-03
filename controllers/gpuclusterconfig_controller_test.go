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
	"sort"
	"testing"

	"github.com/stretchr/testify/require"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	nvidiav1alpha1 "github.com/NVIDIA/gpu-operator/api/nvidia/v1alpha1"
	"github.com/NVIDIA/gpu-operator/internal/conditions"
	"github.com/NVIDIA/gpu-operator/internal/state"
)

// newGPUClusterConfigReconciler builds a reconciler over a fake client seeded with objs. The
// status subresource is registered so Status().Update persists.
func newGPUClusterConfigReconciler(t *testing.T, objs ...client.Object) (*GPUClusterConfigReconciler, client.Client) {
	t.Helper()

	scheme := runtime.NewScheme()
	require.NoError(t, nvidiav1alpha1.AddToScheme(scheme))

	c := fake.NewClientBuilder().
		WithScheme(scheme).
		WithObjects(objs...).
		WithStatusSubresource(&nvidiav1alpha1.GPUClusterConfig{}).
		Build()

	stateManager, err := state.NewManager(nvidiav1alpha1.GPUClusterConfigCRDName, "test-namespace", c, scheme)
	require.NoError(t, err)

	return &GPUClusterConfigReconciler{
		Client:           c,
		Scheme:           scheme,
		Namespace:        "test-namespace",
		stateManager:     stateManager,
		conditionUpdater: &FakeConditionUpdater{},
	}, c
}

func gccReconcile(t *testing.T, r *GPUClusterConfigReconciler, name string) {
	t.Helper()
	_, err := r.Reconcile(context.Background(), gccRequest(name))
	require.NoError(t, err)
}

func gccRequest(name string) ctrl.Request {
	return ctrl.Request{NamespacedName: types.NamespacedName{Name: name}}
}

func gccState(t *testing.T, c client.Client, name string) nvidiav1alpha1.State {
	t.Helper()
	instance := &nvidiav1alpha1.GPUClusterConfig{}
	require.NoError(t, c.Get(context.Background(), types.NamespacedName{Name: name}, instance))
	return instance.Status.State
}

// Empty state set, so SyncState reports ready.
func TestGPUClusterConfigReconcileReady(t *testing.T) {
	cfg := &nvidiav1alpha1.GPUClusterConfig{
		ObjectMeta: metav1.ObjectMeta{Name: "config"},
	}
	r, c := newGPUClusterConfigReconciler(t, cfg)

	gccReconcile(t, r, cfg.Name)

	instance := &nvidiav1alpha1.GPUClusterConfig{}
	require.NoError(t, c.Get(context.Background(), types.NamespacedName{Name: cfg.Name}, instance))
	require.Equal(t, nvidiav1alpha1.Ready, instance.Status.State)
	require.Equal(t, "test-namespace", instance.Status.Namespace)
}

func TestGPUClusterConfigReconcileNotFound(t *testing.T) {
	r, _ := newGPUClusterConfigReconciler(t)

	res, err := r.Reconcile(context.Background(), gccRequest("missing"))
	require.NoError(t, err)
	require.Zero(t, res)
}

// First-reconciled wins (mirroring ClusterPolicy): whichever instance reconciles first
// claims ownership, regardless of name or creationTimestamp.
func TestGPUClusterConfigSingleton(t *testing.T) {
	first := &nvidiav1alpha1.GPUClusterConfig{ObjectMeta: metav1.ObjectMeta{Name: "first"}}
	second := &nvidiav1alpha1.GPUClusterConfig{ObjectMeta: metav1.ObjectMeta{Name: "second"}}
	r, c := newGPUClusterConfigReconciler(t, first, second)

	gccReconcile(t, r, first.Name)
	require.Equal(t, nvidiav1alpha1.Ready, gccState(t, c, first.Name))

	gccReconcile(t, r, second.Name)
	require.Equal(t, nvidiav1alpha1.Ignored, gccState(t, c, second.Name))
}

// Matching ClusterPolicy, an ignored duplicate carries no status condition.
func TestGPUClusterConfigDuplicateNoCondition(t *testing.T) {
	owner := &nvidiav1alpha1.GPUClusterConfig{ObjectMeta: metav1.ObjectMeta{Name: "owner"}}
	duplicate := &nvidiav1alpha1.GPUClusterConfig{ObjectMeta: metav1.ObjectMeta{Name: "duplicate"}}
	r, c := newGPUClusterConfigReconciler(t, owner, duplicate)
	r.conditionUpdater = conditions.NewGPUClusterConfigUpdater(c)

	gccReconcile(t, r, owner.Name)     // owner reconciles first, claiming ownership
	gccReconcile(t, r, duplicate.Name) // duplicate is ignored

	instance := &nvidiav1alpha1.GPUClusterConfig{}
	require.NoError(t, c.Get(context.Background(), types.NamespacedName{Name: duplicate.Name}, instance))
	require.Equal(t, nvidiav1alpha1.Ignored, instance.Status.State)
	require.Nil(t, meta.FindStatusCondition(instance.Status.Conditions, conditions.Error))
	require.Nil(t, meta.FindStatusCondition(instance.Status.Conditions, conditions.Ready))
}

func TestEnqueueAllGPUClusterConfigs(t *testing.T) {
	r, _ := newGPUClusterConfigReconciler(t,
		&nvidiav1alpha1.GPUClusterConfig{ObjectMeta: metav1.ObjectMeta{Name: "config-a"}},
		&nvidiav1alpha1.GPUClusterConfig{ObjectMeta: metav1.ObjectMeta{Name: "config-b"}},
	)

	requests := r.enqueueAllGPUClusterConfigs(context.Background())

	require.Len(t, requests, 2)
	got := []string{requests[0].String(), requests[1].String()}
	sort.Strings(got)
	require.Equal(t, []string{"/config-a", "/config-b"}, got)
}
