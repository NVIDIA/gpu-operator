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
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	gpuv1 "github.com/NVIDIA/gpu-operator/api/nvidia/v1"
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
	require.NoError(t, gpuv1.AddToScheme(scheme))
	require.NoError(t, corev1.AddToScheme(scheme))

	c := fake.NewClientBuilder().
		WithScheme(scheme).
		WithObjects(objs...).
		WithStatusSubresource(&nvidiav1alpha1.GPUClusterConfig{}).
		Build()

	return &GPUClusterConfigReconciler{
		Client:           c,
		Scheme:           scheme,
		Namespace:        "test-namespace",
		stateManager:     &fakeStateManager{results: state.Results{Status: state.SyncStateReady}},
		conditionUpdater: &FakeConditionUpdater{},
	}, c
}

// fakeStateManager returns canned SyncState results so the controller tests don't load
// real manifests. It records the last info catalog passed to SyncState so tests can
// assert on its entries. GetWatchSources is promoted from the embedded (nil) interface
// and is never called here — only SetupWithManager calls it, which these tests skip.
type fakeStateManager struct {
	state.Manager
	results     state.Results
	lastCatalog state.InfoCatalog
}

func (f *fakeStateManager) SyncState(_ context.Context, _ interface{}, catalog state.InfoCatalog) state.Results {
	f.lastCatalog = catalog
	return f.results
}

func gccReconcile(t *testing.T, r *GPUClusterConfigReconciler, name string) {
	t.Helper()
	_, err := r.Reconcile(t.Context(), gccRequest(name))
	require.NoError(t, err)
}

func gccRequest(name string) ctrl.Request {
	return ctrl.Request{NamespacedName: types.NamespacedName{Name: name}}
}

func gccState(t *testing.T, c client.Client, name string) nvidiav1alpha1.State {
	t.Helper()
	instance := &nvidiav1alpha1.GPUClusterConfig{}
	require.NoError(t, c.Get(t.Context(), types.NamespacedName{Name: name}, instance))
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
	require.NoError(t, c.Get(t.Context(), types.NamespacedName{Name: cfg.Name}, instance))
	require.Equal(t, nvidiav1alpha1.Ready, instance.Status.State)
	require.Equal(t, "test-namespace", instance.Status.Namespace)
}

func TestGPUClusterConfigReconcileNotFound(t *testing.T) {
	r, _ := newGPUClusterConfigReconciler(t)

	res, err := r.Reconcile(t.Context(), gccRequest("missing"))
	require.NoError(t, err)
	require.Zero(t, res)
}

// A ClusterPolicy in the cluster disables the GPUClusterConfig: the two paths are
// mutually exclusive, so the DRA stack is not deployed alongside ClusterPolicy.
func TestGPUClusterConfigDisabledByClusterPolicy(t *testing.T) {
	cfg := &nvidiav1alpha1.GPUClusterConfig{ObjectMeta: metav1.ObjectMeta{Name: "config"}}
	cp := &gpuv1.ClusterPolicy{ObjectMeta: metav1.ObjectMeta{Name: "cluster-policy"}}
	r, c := newGPUClusterConfigReconciler(t, cfg, cp)

	gccReconcile(t, r, cfg.Name)

	require.Equal(t, nvidiav1alpha1.Disabled, gccState(t, c, cfg.Name))
}

func testNode(name string, labels map[string]string) *corev1.Node {
	return &corev1.Node{ObjectMeta: metav1.ObjectMeta{Name: name, Labels: labels}}
}

func nodeLabels(t *testing.T, c client.Client, name string) map[string]string {
	t.Helper()
	node := &corev1.Node{}
	require.NoError(t, c.Get(t.Context(), types.NamespacedName{Name: name}, node))
	return node.GetLabels()
}

// Nodes NFD discovered GPUs on get the common GPU label and the driver deploy
// label (the driver DaemonSet's nodeSelector requires it); nodes that lost their
// GPUs get the common label reset to "false" and the driver label removed; other
// nodes are untouched.
func TestGPUClusterConfigLabelsGPUNodes(t *testing.T) {
	cfg := &nvidiav1alpha1.GPUClusterConfig{ObjectMeta: metav1.ObjectMeta{Name: "config"}}
	gpuNode := testNode("gpu-node", map[string]string{
		"feature.node.kubernetes.io/pci-10de.present": "true",
	})
	plainNode := testNode("plain-node", map[string]string{
		"kubernetes.io/hostname": "plain-node",
	})
	removedGPUNode := testNode("removed-gpu-node", map[string]string{
		commonGPULabelKey:    commonGPULabelValue,
		driverDeployLabelKey: "true",
	})
	r, c := newGPUClusterConfigReconciler(t, cfg, gpuNode, plainNode, removedGPUNode)

	gccReconcile(t, r, cfg.Name)

	require.Equal(t, commonGPULabelValue, nodeLabels(t, c, gpuNode.Name)[commonGPULabelKey])
	require.Equal(t, "true", nodeLabels(t, c, gpuNode.Name)[driverDeployLabelKey])
	require.NotContains(t, nodeLabels(t, c, plainNode.Name), commonGPULabelKey)
	require.Equal(t, "false", nodeLabels(t, c, removedGPUNode.Name)[commonGPULabelKey])
	require.NotContains(t, nodeLabels(t, c, removedGPUNode.Name), driverDeployLabelKey)
}

// A GPU node that already has gpu.present but is missing the driver deploy label
// (e.g. it was removed out of band) is converged back.
func TestGPUClusterConfigRestoresDriverDeployLabel(t *testing.T) {
	cfg := &nvidiav1alpha1.GPUClusterConfig{ObjectMeta: metav1.ObjectMeta{Name: "config"}}
	gpuNode := testNode("gpu-node", map[string]string{
		"feature.node.kubernetes.io/pci-10de.present": "true",
		commonGPULabelKey: commonGPULabelValue,
	})
	r, c := newGPUClusterConfigReconciler(t, cfg, gpuNode)

	gccReconcile(t, r, cfg.Name)

	require.Equal(t, "true", nodeLabels(t, c, gpuNode.Name)[driverDeployLabelKey])
}

// The mutually-exclusive ClusterPolicy path returns before node labeling, so the
// GPUClusterConfig controller never touches nodes on clusters ClusterPolicy owns.
func TestGPUClusterConfigNoNodeLabelingWhenClusterPolicyPresent(t *testing.T) {
	cfg := &nvidiav1alpha1.GPUClusterConfig{ObjectMeta: metav1.ObjectMeta{Name: "config"}}
	cp := &gpuv1.ClusterPolicy{ObjectMeta: metav1.ObjectMeta{Name: "cluster-policy"}}
	gpuNode := testNode("gpu-node", map[string]string{
		"feature.node.kubernetes.io/pci-10de.present": "true",
	})
	r, c := newGPUClusterConfigReconciler(t, cfg, cp, gpuNode)

	gccReconcile(t, r, cfg.Name)

	require.Equal(t, nvidiav1alpha1.Disabled, gccState(t, c, cfg.Name))
	require.NotContains(t, nodeLabels(t, c, gpuNode.Name), commonGPULabelKey)
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
	require.NoError(t, c.Get(t.Context(), types.NamespacedName{Name: duplicate.Name}, instance))
	require.Equal(t, nvidiav1alpha1.Ignored, instance.Status.State)
	require.Nil(t, meta.FindStatusCondition(instance.Status.Conditions, conditions.Error))
	require.Nil(t, meta.FindStatusCondition(instance.Status.Conditions, conditions.Ready))
}

func TestEnqueueAllGPUClusterConfigs(t *testing.T) {
	r, _ := newGPUClusterConfigReconciler(t,
		&nvidiav1alpha1.GPUClusterConfig{ObjectMeta: metav1.ObjectMeta{Name: "config-a"}},
		&nvidiav1alpha1.GPUClusterConfig{ObjectMeta: metav1.ObjectMeta{Name: "config-b"}},
	)

	requests := r.enqueueAllGPUClusterConfigs(t.Context(), nil)

	require.Len(t, requests, 2)
	got := []string{requests[0].String(), requests[1].String()}
	sort.Strings(got)
	require.Equal(t, []string{"/config-a", "/config-b"}, got)
}
