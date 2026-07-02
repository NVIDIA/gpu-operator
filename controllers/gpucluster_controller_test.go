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
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/tools/events"
	"k8s.io/utils/ptr"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	gpuv1 "github.com/NVIDIA/gpu-operator/api/nvidia/v1"
	nvidiav1alpha1 "github.com/NVIDIA/gpu-operator/api/nvidia/v1alpha1"
	"github.com/NVIDIA/gpu-operator/internal/conditions"
	"github.com/NVIDIA/gpu-operator/internal/state"
)

// newGPUClusterReconciler builds a reconciler over a fake client seeded with objs. The
// status subresource is registered so Status().Update persists.
func newGPUClusterReconciler(t *testing.T, objs ...client.Object) (*GPUClusterReconciler, client.Client) {
	t.Helper()

	scheme := runtime.NewScheme()
	require.NoError(t, nvidiav1alpha1.AddToScheme(scheme))
	require.NoError(t, gpuv1.AddToScheme(scheme))
	require.NoError(t, appsv1.AddToScheme(scheme))
	require.NoError(t, corev1.AddToScheme(scheme))

	// The reconciler labels the operator namespace for adminAccess, so it must exist.
	ns := &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: "test-namespace"}}
	c := fake.NewClientBuilder().
		WithScheme(scheme).
		WithObjects(append([]client.Object{ns}, objs...)...).
		WithStatusSubresource(&nvidiav1alpha1.GPUCluster{}).
		Build()

	return &GPUClusterReconciler{
		Client:           c,
		Scheme:           scheme,
		Namespace:        "test-namespace",
		stateManager:     &fakeStateManager{results: state.Results{Status: state.SyncStateReady}},
		conditionUpdater: &FakeConditionUpdater{},
		recorder:         events.NewFakeRecorder(100),
	}, c
}

// fakeStateManager returns canned SyncState results so the controller tests don't load
// real manifests. GetWatchSources is promoted from the embedded (nil) interface and is
// never called here — only SetupWithManager calls it, which these tests skip.
type fakeStateManager struct {
	state.Manager
	results state.Results
}

func (f *fakeStateManager) SyncState(_ context.Context, _ interface{}, _ state.InfoCatalog) state.Results {
	return f.results
}

func gccReconcile(t *testing.T, r *GPUClusterReconciler, name string) {
	t.Helper()
	_, err := r.Reconcile(t.Context(), gccRequest(name))
	require.NoError(t, err)
}

func gccRequest(name string) ctrl.Request {
	return ctrl.Request{NamespacedName: types.NamespacedName{Name: name}}
}

func gccState(t *testing.T, c client.Client, name string) nvidiav1alpha1.State {
	t.Helper()
	instance := &nvidiav1alpha1.GPUCluster{}
	require.NoError(t, c.Get(t.Context(), types.NamespacedName{Name: name}, instance))
	return instance.Status.State
}

// Empty state set, so SyncState reports ready.
func TestGPUClusterReconcileReady(t *testing.T) {
	cfg := &nvidiav1alpha1.GPUCluster{
		ObjectMeta: metav1.ObjectMeta{Name: "config"},
	}
	r, c := newGPUClusterReconciler(t, cfg)

	gccReconcile(t, r, cfg.Name)

	instance := &nvidiav1alpha1.GPUCluster{}
	require.NoError(t, c.Get(t.Context(), types.NamespacedName{Name: cfg.Name}, instance))
	require.Equal(t, nvidiav1alpha1.Ready, instance.Status.State)
	require.Equal(t, "test-namespace", instance.Status.Namespace)
}

func TestGPUClusterReconcileLabelsNamespaceForAdminAccess(t *testing.T) {
	cfg := &nvidiav1alpha1.GPUCluster{ObjectMeta: metav1.ObjectMeta{Name: "config"}}
	r, c := newGPUClusterReconciler(t, cfg)

	gccReconcile(t, r, cfg.Name)

	ns := &corev1.Namespace{}
	require.NoError(t, c.Get(t.Context(), types.NamespacedName{Name: "test-namespace"}, ns))
	require.Equal(t, "true", ns.Labels[draAdminNamespaceLabelKey])
}

func TestGPUClusterReconcileNotFound(t *testing.T) {
	r, _ := newGPUClusterReconciler(t)

	res, err := r.Reconcile(t.Context(), gccRequest("missing"))
	require.NoError(t, err)
	require.Zero(t, res)
}

func TestGPUClusterReconcileAddsFinalizer(t *testing.T) {
	cfg := &nvidiav1alpha1.GPUCluster{ObjectMeta: metav1.ObjectMeta{Name: "config"}}
	r, c := newGPUClusterReconciler(t, cfg)

	gccReconcile(t, r, cfg.Name)

	instance := &nvidiav1alpha1.GPUCluster{}
	require.NoError(t, c.Get(t.Context(), types.NamespacedName{Name: cfg.Name}, instance))
	require.Contains(t, instance.Finalizers, gpuClusterFinalizer)
}

// gccOwnedDaemonSet returns a DaemonSet controlled by cr, optionally consuming a
// ResourceClaim. Extra finalizers keep it around after Delete so tests can observe the
// draining phase (the fake client has no garbage collector to hold it like foreground
// deletion would).
func gccOwnedDaemonSet(name string, cr *nvidiav1alpha1.GPUCluster, claims bool, finalizers ...string) *appsv1.DaemonSet {
	ds := &appsv1.DaemonSet{
		ObjectMeta: metav1.ObjectMeta{
			Name:       name,
			Namespace:  "test-namespace",
			Finalizers: finalizers,
			OwnerReferences: []metav1.OwnerReference{{
				APIVersion: nvidiav1alpha1.SchemeGroupVersion.String(),
				Kind:       "GPUCluster",
				Name:       cr.Name,
				UID:        cr.UID,
				Controller: ptr.To(true),
			}},
		},
	}
	if claims {
		ds.Spec.Template.Spec.ResourceClaims = []corev1.PodResourceClaim{{Name: "gpu"}}
	}
	return ds
}

// Deleting the GPUCluster first drains ResourceClaim-consuming DaemonSets (the pods
// that need the DRA kubelet plugin to unprepare their claims) while holding the CR via
// finalizer; only once they are gone is the finalizer removed, releasing everything
// else (including the kubelet plugin) to garbage collection.
func TestGPUClusterTeardownDrainsClaimConsumersFirst(t *testing.T) {
	cfg := &nvidiav1alpha1.GPUCluster{ObjectMeta: metav1.ObjectMeta{
		Name:       "config",
		UID:        "gpucluster-uid",
		Finalizers: []string{gpuClusterFinalizer},
	}}
	consumer := gccOwnedDaemonSet("nvidia-dra-validator", cfg, true, "test.nvidia.com/hold")
	plugin := gccOwnedDaemonSet("nvidia-dra-driver-kubelet-plugin", cfg, false)
	r, c := newGPUClusterReconciler(t, cfg, consumer, plugin)

	require.NoError(t, c.Delete(t.Context(), cfg))

	// First pass: the claim consumer is deleted and still draining, so the CR is held.
	res, err := r.Reconcile(t.Context(), gccRequest(cfg.Name))
	require.NoError(t, err)
	require.NotZero(t, res.RequeueAfter)

	ds := &appsv1.DaemonSet{}
	require.NoError(t, c.Get(t.Context(), types.NamespacedName{Name: consumer.Name, Namespace: "test-namespace"}, ds))
	require.False(t, ds.DeletionTimestamp.IsZero())
	require.NoError(t, c.Get(t.Context(), types.NamespacedName{Name: plugin.Name, Namespace: "test-namespace"}, ds))
	require.True(t, ds.DeletionTimestamp.IsZero(), "kubelet plugin must not be deleted while consumers drain")
	instance := &nvidiav1alpha1.GPUCluster{}
	require.NoError(t, c.Get(t.Context(), types.NamespacedName{Name: cfg.Name}, instance))
	require.Contains(t, instance.Finalizers, gpuClusterFinalizer)

	// Consumer finishes draining (drop the finalizer holding it).
	require.NoError(t, c.Get(t.Context(), types.NamespacedName{Name: consumer.Name, Namespace: "test-namespace"}, ds))
	ds.Finalizers = nil
	require.NoError(t, c.Update(t.Context(), ds))

	// Second pass: finalizer removed, CR released to garbage collection.
	res, err = r.Reconcile(t.Context(), gccRequest(cfg.Name))
	require.NoError(t, err)
	require.Zero(t, res)
	err = c.Get(t.Context(), types.NamespacedName{Name: cfg.Name}, instance)
	require.True(t, apierrors.IsNotFound(err))
	require.NoError(t, c.Get(t.Context(), types.NamespacedName{Name: plugin.Name, Namespace: "test-namespace"}, ds))
}

// A ClusterPolicy in the cluster does not disable the GPUCluster: the two stacks
// coexist, with per-node ownership decided by the nvidia.com/gpu-operator.resource-allocation.mode label.
func TestGPUClusterCoexistsWithClusterPolicy(t *testing.T) {
	cfg := &nvidiav1alpha1.GPUCluster{ObjectMeta: metav1.ObjectMeta{Name: "config"}}
	cp := &gpuv1.ClusterPolicy{ObjectMeta: metav1.ObjectMeta{Name: "cluster-policy"}}
	r, c := newGPUClusterReconciler(t, cfg, cp)

	gccReconcile(t, r, cfg.Name)

	require.Equal(t, nvidiav1alpha1.Ready, gccState(t, c, cfg.Name))
}

// First-reconciled wins (mirroring ClusterPolicy): whichever instance reconciles first
// claims ownership, regardless of name or creationTimestamp.
func TestGPUClusterSingleton(t *testing.T) {
	first := &nvidiav1alpha1.GPUCluster{ObjectMeta: metav1.ObjectMeta{Name: "first"}}
	second := &nvidiav1alpha1.GPUCluster{ObjectMeta: metav1.ObjectMeta{Name: "second"}}
	r, c := newGPUClusterReconciler(t, first, second)

	gccReconcile(t, r, first.Name)
	require.Equal(t, nvidiav1alpha1.Ready, gccState(t, c, first.Name))

	gccReconcile(t, r, second.Name)
	require.Equal(t, nvidiav1alpha1.Ignored, gccState(t, c, second.Name))
}

// Matching ClusterPolicy, an ignored duplicate carries no status condition.
func TestGPUClusterDuplicateNoCondition(t *testing.T) {
	owner := &nvidiav1alpha1.GPUCluster{ObjectMeta: metav1.ObjectMeta{Name: "owner"}}
	duplicate := &nvidiav1alpha1.GPUCluster{ObjectMeta: metav1.ObjectMeta{Name: "duplicate"}}
	r, c := newGPUClusterReconciler(t, owner, duplicate)
	r.conditionUpdater = conditions.NewGPUClusterUpdater(c)

	gccReconcile(t, r, owner.Name)     // owner reconciles first, claiming ownership
	gccReconcile(t, r, duplicate.Name) // duplicate is ignored

	instance := &nvidiav1alpha1.GPUCluster{}
	require.NoError(t, c.Get(t.Context(), types.NamespacedName{Name: duplicate.Name}, instance))
	require.Equal(t, nvidiav1alpha1.Ignored, instance.Status.State)
	require.Nil(t, meta.FindStatusCondition(instance.Status.Conditions, conditions.Error))
	require.Nil(t, meta.FindStatusCondition(instance.Status.Conditions, conditions.Ready))
}

func TestEnqueueAllGPUClusters(t *testing.T) {
	r, _ := newGPUClusterReconciler(t,
		&nvidiav1alpha1.GPUCluster{ObjectMeta: metav1.ObjectMeta{Name: "config-a"}},
		&nvidiav1alpha1.GPUCluster{ObjectMeta: metav1.ObjectMeta{Name: "config-b"}},
	)

	requests := r.enqueueAllGPUClusters(t.Context(), nil)

	require.Len(t, requests, 2)
	got := []string{requests[0].String(), requests[1].String()}
	sort.Strings(got)
	require.Equal(t, []string{"/config-a", "/config-b"}, got)
}
