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
	"fmt"
	"slices"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	resourcev1 "k8s.io/api/resource/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/client/interceptor"

	gpuv1 "github.com/NVIDIA/gpu-operator/api/nvidia/v1"
	gpu "github.com/NVIDIA/gpu-operator/api/nvidia/v1alpha1"
	"github.com/NVIDIA/gpu-operator/internal/coexistence"
	"github.com/NVIDIA/gpu-operator/internal/consts"
)

func testSourceCP() *gpuv1.ClusterPolicy {
	cp := &gpuv1.ClusterPolicy{ObjectMeta: metav1.ObjectMeta{Name: "cp", UID: "cp-uid", CreationTimestamp: metav1.NewTime(time.Unix(1, 0)), Finalizers: []string{nodeCleanupFinalizer}}}
	cp.Spec.Driver.Enabled = new(false)
	cp.Status.Conditions = []metav1.Condition{{Type: nodeSelectionInitialized, Status: metav1.ConditionTrue, Reason: "AdoptionComplete"}}
	return cp
}
func testSourceGC() *gpu.GPUCluster {
	return &gpu.GPUCluster{ObjectMeta: metav1.ObjectMeta{Name: "gpu-cluster", UID: "gc-uid", CreationTimestamp: metav1.NewTime(time.Unix(2, 0)), Finalizers: []string{nodeCleanupFinalizer}}}
}
func lifecycleClient(t *testing.T, objects ...client.Object) client.Client {
	t.Helper()
	scheme := runtime.NewScheme()
	require.NoError(t, corev1.AddToScheme(scheme))
	require.NoError(t, appsv1.AddToScheme(scheme))
	require.NoError(t, resourcev1.AddToScheme(scheme))
	require.NoError(t, gpuv1.AddToScheme(scheme))
	require.NoError(t, gpu.AddToScheme(scheme))
	builder := fake.NewClientBuilder().WithScheme(scheme).WithObjects(objects...).WithStatusSubresource(&gpuv1.ClusterPolicy{}, &gpu.GPUCluster{})
	for _, obj := range []client.Object{&appsv1.DaemonSet{}, &appsv1.Deployment{}, &corev1.Pod{}} {
		builder.WithIndex(obj, sourceOwnerIndex, sourceOwnerKeys)
	}
	builder.WithIndex(&corev1.Pod{}, podNodeNameIndexKey, podNodeNameIndexer)
	return builder.Build()
}

func ownedMeta(name, kind string, owner client.Object) metav1.ObjectMeta {
	return metav1.ObjectMeta{Name: name, Namespace: "test", UID: types.UID(name + "-uid"), OwnerReferences: []metav1.OwnerReference{{APIVersion: "nvidia.com/v1alpha1", Kind: kind, Name: owner.GetName(), UID: owner.GetUID(), Controller: new(true), BlockOwnerDeletion: new(true)}}}
}
func deleteSource(t *testing.T, c client.Client, obj client.Object) {
	t.Helper()
	require.NoError(t, c.Delete(t.Context(), obj))
	require.NoError(t, c.Get(t.Context(), client.ObjectKeyFromObject(obj), obj))
}

func TestDeletionWaitsForOperandsAndMetadataObservation(t *testing.T) {
	cp := testSourceCP()
	node := selectedNode(coexistence.DP)
	node.Labels[devicePluginDeployLabelKey] = "paused-for-driver-upgrade"
	node.Labels[coexistence.ModeLabel] = coexistence.DRA
	node.Labels["nvidia.com/gpu.deploy.dcgm-exporter"] = "false"
	node.Labels[consts.NVIDIADriverOwnerLabel] = "driver"
	node.Labels[driverDeployLabelKey] = "false"
	node.Annotations[driverAutoUpgradeAnnotationKey] = "true"
	ds := &appsv1.DaemonSet{ObjectMeta: ownedMeta("device-plugin", "ClusterPolicy", cp)}
	ds.Finalizers = []string{"test/keep"}
	pod := &corev1.Pod{ObjectMeta: ownedMeta("dp-pod", "DaemonSet", ds)}
	c := lifecycleClient(t, cp, node, ds, pod)
	deleteSource(t, c, cp)
	result, err := teardownSourceOperands(t.Context(), c, "test", cp)
	require.NoError(t, err)
	require.Equal(t, lifecycleRetry, result.RequeueAfter)
	require.NoError(t, c.Get(t.Context(), client.ObjectKeyFromObject(cp), cp))
	require.False(t, operandsRemovedTrue(cp))
	require.Contains(t, meta.FindStatusCondition(cp.Status.Conditions, operandsRemoved).Message, "test/device-plugin")
	r := &NodeLabelingReconciler{Client: c}
	_, err = r.cleanupDeletedSources(t.Context())
	require.NoError(t, err)
	require.NoError(t, c.Get(t.Context(), client.ObjectKeyFromObject(node), node))
	require.Equal(t, coexistence.DP, node.Annotations[coexistence.SelectionAnnotation])
	require.Contains(t, node.Labels, devicePluginDeployLabelKey)
	// Even an explicit mode change must not bypass the reservation.
	nlc := &nodeLabelingController{client: c, clusterPolicy: cp, gpuCluster: testSourceGC()}
	changed, err := nlc.selectNode(t.Context(), node)
	require.NoError(t, err)
	require.False(t, changed)
	require.NoError(t, c.Get(t.Context(), client.ObjectKeyFromObject(ds), ds))
	require.NotNil(t, ds.DeletionTimestamp)
	// Fake clients have no GC: simulate observed foreground completion.
	require.NoError(t, c.Get(t.Context(), client.ObjectKeyFromObject(pod), pod))
	require.Nil(t, pod.DeletionTimestamp)
	require.NoError(t, c.Delete(t.Context(), pod))
	ds.Finalizers = nil
	require.NoError(t, c.Update(t.Context(), ds))
	result, err = teardownSourceOperands(t.Context(), c, "test", cp)
	require.NoError(t, err)
	require.Empty(t, result)
	require.NoError(t, c.Get(t.Context(), client.ObjectKeyFromObject(cp), cp))
	require.True(t, operandsRemovedTrue(cp))

	result, err = teardownSourceOperands(t.Context(), c, "test", cp)
	require.NoError(t, err)
	require.Empty(t, result)
	_, err = r.cleanupDeletedSources(t.Context())
	require.NoError(t, err)
	require.NoError(t, c.Get(t.Context(), client.ObjectKeyFromObject(cp), cp))
	require.Contains(t, cp.Finalizers, nodeCleanupFinalizer, "a successful patch is not observation")
	require.NoError(t, c.Get(t.Context(), client.ObjectKeyFromObject(node), node))
	require.NotContains(t, node.Annotations, coexistence.SelectionAnnotation)
	require.NotContains(t, node.Labels, devicePluginDeployLabelKey)
	require.Equal(t, coexistence.DRA, node.Labels[coexistence.ModeLabel])
	require.Equal(t, "false", node.Labels["nvidia.com/gpu.deploy.dcgm-exporter"])
	require.Equal(t, "false", node.Labels[driverDeployLabelKey])
	require.Equal(t, "true", node.Annotations[driverAutoUpgradeAnnotationKey])
	_, err = r.cleanupDeletedSources(t.Context())
	require.NoError(t, err)
	require.True(t, apierrors.IsNotFound(c.Get(t.Context(), client.ObjectKeyFromObject(cp), cp)))
}

func TestTeardownOwnershipAndDRAOrdering(t *testing.T) {
	gc := testSourceGC()
	deleteTime := metav1.Now()
	gc.DeletionTimestamp = &deleteTime
	consumer := &appsv1.DaemonSet{ObjectMeta: ownedMeta("consumer", "GPUCluster", gc)}
	consumer.Finalizers = []string{"test/keep"}
	// Pods from an older revision may have claims even when the current template has none.
	claimPod := &corev1.Pod{ObjectMeta: ownedMeta("claim-pod", "DaemonSet", consumer)}
	claimPod.Spec.ResourceClaims = []corev1.PodResourceClaim{{Name: "gpu"}}
	plugin := &appsv1.DaemonSet{ObjectMeta: ownedMeta("nvidia-dra-driver-kubelet-plugin", "GPUCluster", gc)}
	deployment := &appsv1.Deployment{ObjectMeta: ownedMeta("nvidia-dra-driver-controller", "GPUCluster", gc)}
	deployment.Finalizers = []string{"test/foreground"}
	rs := &appsv1.ReplicaSet{ObjectMeta: ownedMeta("controller-rs", "Deployment", deployment)}
	pod := &corev1.Pod{ObjectMeta: ownedMeta("controller-pod", "ReplicaSet", rs)}
	independent := &appsv1.DaemonSet{ObjectMeta: metav1.ObjectMeta{Name: "driver", Namespace: "test", UID: "driver-uid", OwnerReferences: []metav1.OwnerReference{{Kind: "NVIDIADriver", Name: "driver", UID: "independent-uid"}}}}
	passive := &corev1.ConfigMap{ObjectMeta: ownedMeta("config", "GPUCluster", gc)}
	base := lifecycleClient(t, gc, consumer, claimPod, plugin, deployment, rs, pod, independent, passive)
	c := interceptor.NewClient(base.(client.WithWatch), interceptor.Funcs{
		List: func(ctx context.Context, c client.WithWatch, list client.ObjectList, opts ...client.ListOption) error {
			_, podList := list.(*corev1.PodList)
			require.False(t, podList, "GPUCluster teardown relies on foreground workload deletion")
			return c.List(ctx, list, opts...)
		},
		Delete: func(ctx context.Context, c client.WithWatch, obj client.Object, opts ...client.DeleteOption) error {
			options := (&client.DeleteOptions{}).ApplyOptions(opts)
			require.Equal(t, new(metav1.DeletePropagationForeground), options.PropagationPolicy)
			require.NotNil(t, options.Preconditions)
			require.Equal(t, new(obj.GetUID()), options.Preconditions.UID)
			require.Contains(t, []string{consumer.Name, plugin.Name, deployment.Name}, obj.GetName())
			return c.Delete(ctx, obj, opts...)
		},
	})
	_, err := teardownSourceOperands(t.Context(), c, "test", gc)
	require.NoError(t, err)
	for _, obj := range []client.Object{plugin, deployment, rs, pod, independent, passive} {
		require.NoError(t, c.Get(t.Context(), client.ObjectKeyFromObject(obj), obj))
		require.Nil(t, obj.GetDeletionTimestamp())
	}
	// Simulate foreground deletion waiting for the old claim-consuming Pod.
	_, err = teardownSourceOperands(t.Context(), c, "test", gc)
	require.NoError(t, err)
	require.False(t, operandsRemovedTrue(gc))
	require.NoError(t, c.Get(t.Context(), client.ObjectKeyFromObject(plugin), plugin))
	require.NoError(t, base.Delete(t.Context(), claimPod))
	require.NoError(t, c.Get(t.Context(), client.ObjectKeyFromObject(consumer), consumer))
	consumer.Finalizers = nil
	require.NoError(t, c.Update(t.Context(), consumer))
	require.NoError(t, c.Get(t.Context(), client.ObjectKeyFromObject(gc), gc))
	_, err = teardownSourceOperands(t.Context(), c, "test", gc)
	require.NoError(t, err)
	require.False(t, operandsRemovedTrue(gc), "Delete success is not completion")
	require.True(t, apierrors.IsNotFound(c.Get(t.Context(), client.ObjectKeyFromObject(plugin), plugin)))
	require.NoError(t, c.Get(t.Context(), client.ObjectKeyFromObject(deployment), deployment))
	require.NotNil(t, deployment.DeletionTimestamp)
	_, err = teardownSourceOperands(t.Context(), c, "test", gc)
	require.NoError(t, err)
	require.False(t, operandsRemovedTrue(gc))
	// Simulate the garbage collector completing the Deployment -> ReplicaSet -> Pod cascade.
	for _, obj := range []client.Object{pod, rs} {
		require.NoError(t, base.Delete(t.Context(), obj))
	}
	deployment.Finalizers = nil
	require.NoError(t, base.Update(t.Context(), deployment))
	_, err = teardownSourceOperands(t.Context(), c, "test", gc)
	require.NoError(t, err)
	require.True(t, operandsRemovedTrue(gc))
	require.NoError(t, c.Get(t.Context(), client.ObjectKeyFromObject(independent), independent))
	require.NoError(t, c.Get(t.Context(), client.ObjectKeyFromObject(passive), passive))
}

func TestDirectPodDeletionBatches(t *testing.T) {
	cp := testSourceCP()
	otherOwner := testSourceCP()
	otherOwner.UID = "another-cp-uid"
	other := &corev1.Pod{ObjectMeta: ownedMeta("other", "ClusterPolicy", otherOwner)}
	completed := &corev1.Pod{ObjectMeta: ownedMeta("completed", "ClusterPolicy", cp), Status: corev1.PodStatus{Phase: corev1.PodSucceeded}}
	failed := &corev1.Pod{ObjectMeta: ownedMeta("failed", "ClusterPolicy", cp), Status: corev1.PodStatus{Phase: corev1.PodFailed}}
	unknown := &corev1.Pod{ObjectMeta: ownedMeta("unknown", "ClusterPolicy", cp), Status: corev1.PodStatus{Phase: corev1.PodUnknown}}
	stuck := []*corev1.Pod{failed, unknown}
	objects := []client.Object{cp, other, completed}
	for _, pod := range stuck {
		pod.Finalizers = []string{"test/hold"}
		objects = append(objects, pod)
	}
	for i := range directPodBatchSize + 2 {
		objects = append(objects, &corev1.Pod{ObjectMeta: ownedMeta(fmt.Sprintf("validator-%03d", i), "ClusterPolicy", cp)})
	}
	base := lifecycleClient(t, objects...)
	deleteSource(t, base, cp)
	for _, pod := range stuck {
		require.NoError(t, base.Delete(t.Context(), pod))
	}
	deletedPods := 0
	c := interceptor.NewClient(base.(client.WithWatch), interceptor.Funcs{
		List: func(ctx context.Context, c client.WithWatch, list client.ObjectList, opts ...client.ListOption) error {
			if err := c.List(ctx, list, opts...); err != nil {
				return err
			}
			if pods, ok := list.(*corev1.PodList); ok {
				o := (&client.ListOptions{}).ApplyOptions(opts)
				key, indexed := o.FieldSelector.RequiresExactMatch(sourceOwnerIndex)
				require.True(t, indexed)
				if key == string(cp.UID) {
					require.EqualValues(t, directPodBatchSize, o.Limit)
				} else {
					require.Equal(t, string(cp.UID)+"/terminating", key)
					require.EqualValues(t, maxReportedWorkloads+1, o.Limit)
				}
				// The real cache honors Limit. The fake client ignores it.
				if len(pods.Items) > int(o.Limit) {
					pods.Items = pods.Items[:o.Limit]
				}
			}
			return nil
		},
		Delete: func(ctx context.Context, c client.WithWatch, obj client.Object, opts ...client.DeleteOption) error {
			if _, pod := obj.(*corev1.Pod); pod {
				require.True(t, metav1.IsControlledBy(obj, cp))
				require.Nil(t, obj.GetDeletionTimestamp())
				deletedPods++
			}
			return c.Delete(ctx, obj, opts...)
		},
	})
	for _, wantDeleted := range []int{directPodBatchSize, directPodBatchSize + 2, directPodBatchSize + 2} {
		_, err := teardownSourceOperands(t.Context(), c, "test", cp)
		require.NoError(t, err)
		require.Equal(t, wantDeleted, deletedPods)
		require.False(t, operandsRemovedTrue(cp))
	}
	condition := meta.FindStatusCondition(cp.Status.Conditions, operandsRemoved)
	require.NotNil(t, condition)
	for _, pod := range stuck {
		require.Contains(t, condition.Message, pod.Name)
		require.NoError(t, c.Get(t.Context(), client.ObjectKeyFromObject(pod), pod))
		pod.Finalizers = nil
		require.NoError(t, c.Update(t.Context(), pod))
	}

	_, err := teardownSourceOperands(t.Context(), c, "test", cp)
	require.NoError(t, err)
	require.True(t, operandsRemovedTrue(cp))
	require.NoError(t, c.Get(t.Context(), client.ObjectKeyFromObject(completed), completed))
	require.NoError(t, c.Get(t.Context(), client.ObjectKeyFromObject(other), other))
}

func TestCleanupRetriesPreserveSurvivingSelections(t *testing.T) {
	for name, tc := range map[string]struct {
		defaultStack  string
		wantSelection string
	}{
		"unset default selects surviving DRA":    {wantSelection: coexistence.DRA},
		"device plugin default waits for source": {defaultStack: coexistence.DP},
		"DRA default selects surviving DRA":      {defaultStack: coexistence.DRA, wantSelection: coexistence.DRA},
	} {
		t.Run(name, func(t *testing.T) {
			cp := testSourceCP()
			gc := testSourceGC()
			node := selectedNode(coexistence.DP)
			node.Labels[devicePluginDeployLabelKey] = ""
			c := lifecycleClient(t, cp, gc, node)
			deleteSource(t, c, cp)
			require.NoError(t, setOperandsRemoved(t.Context(), c, cp, true, "TeardownComplete", "done"))
			r := &NodeLabelingReconciler{Client: c}
			_, err := r.cleanupDeletedSources(t.Context())
			require.NoError(t, err)
			require.NoError(t, c.Get(t.Context(), client.ObjectKeyFromObject(node), node))
			// Surviving DRA may activate before the terminating CP disappears.
			nlc := &nodeLabelingController{client: c, clusterPolicy: cp, gpuCluster: gc, defaultStack: tc.defaultStack}
			_, err = nlc.selectNode(t.Context(), node)
			require.NoError(t, err)
			require.Equal(t, tc.wantSelection, node.Annotations[coexistence.SelectionAnnotation])
			require.NoError(t, c.Update(t.Context(), node))
			_, err = r.cleanupDeletedSources(t.Context())
			require.NoError(t, err)
			require.NoError(t, c.Get(t.Context(), client.ObjectKeyFromObject(node), node))
			require.Equal(t, tc.wantSelection, node.Annotations[coexistence.SelectionAnnotation])
			require.True(t, apierrors.IsNotFound(c.Get(t.Context(), client.ObjectKeyFromObject(cp), cp)))
		})
	}
}

func TestCleanupFailureAndRestart(t *testing.T) {
	cp := testSourceCP()
	node := selectedNode(coexistence.DP)
	// Cleanup includes nodes that no longer have discovery labels.
	node.Labels = map[string]string{devicePluginDeployLabelKey: "true"}
	base := lifecycleClient(t, cp, node)
	deleteSource(t, base, cp)
	require.NoError(t, setOperandsRemoved(t.Context(), base, cp, true, "TeardownComplete", "done"))
	failing := interceptor.NewClient(base.(client.WithWatch), interceptor.Funcs{Patch: func(ctx context.Context, c client.WithWatch, obj client.Object, p client.Patch, opts ...client.PatchOption) error {
		if _, ok := obj.(*corev1.Node); ok {
			return fmt.Errorf("node write unavailable")
		}
		return c.Patch(ctx, obj, p, opts...)
	}})
	r := &NodeLabelingReconciler{Client: failing}
	_, err := r.cleanupDeletedSources(t.Context())
	require.ErrorContains(t, err, fmt.Sprintf("clean stack labels on node %q", node.Name))
	require.ErrorContains(t, err, "node write unavailable")
	require.NoError(t, base.Get(t.Context(), client.ObjectKeyFromObject(cp), cp))
	require.Contains(t, cp.Finalizers, nodeCleanupFinalizer)
	// A replacement controller resumes only the unfinished metadata phase.
	r = &NodeLabelingReconciler{Client: base}
	_, err = r.cleanupDeletedSources(t.Context())
	require.NoError(t, err)
	require.NoError(t, base.Get(t.Context(), client.ObjectKeyFromObject(node), node))
	require.Empty(t, node.Annotations)
	require.Empty(t, node.Labels)
	_, err = r.cleanupDeletedSources(t.Context())
	require.NoError(t, err)
	require.True(t, apierrors.IsNotFound(base.Get(t.Context(), client.ObjectKeyFromObject(cp), cp)))
}

func TestSourceReconcileDeletionPrecedesPrerequisites(t *testing.T) {
	cp := testSourceCP()
	gc := testSourceGC()
	cp.CreationTimestamp = gc.CreationTimestamp
	c := lifecycleClient(t, cp, gc)
	deleteSource(t, c, cp)
	r := &ClusterPolicyReconciler{Client: c, Namespace: "test"}
	_, err := r.Reconcile(t.Context(), ctrl.Request{NamespacedName: client.ObjectKeyFromObject(cp)})
	require.NoError(t, err)
	require.NoError(t, c.Get(t.Context(), client.ObjectKeyFromObject(cp), cp))
	require.True(t, operandsRemovedTrue(cp))
}

func TestGPUClusterContinuesAfterOldFinalizerRemoved(t *testing.T) {
	gc := testSourceGC()
	plugin := &appsv1.DaemonSet{ObjectMeta: ownedMeta("plugin", "GPUCluster", gc)}
	c := lifecycleClient(t, gc, plugin)
	deleteSource(t, c, gc)
	r := &GPUClusterReconciler{Client: c, Namespace: "test"}
	_, err := r.Reconcile(t.Context(), ctrl.Request{NamespacedName: client.ObjectKeyFromObject(gc)})
	require.NoError(t, err)
	require.True(t, apierrors.IsNotFound(c.Get(t.Context(), client.ObjectKeyFromObject(plugin), plugin)))
	require.NoError(t, c.Get(t.Context(), client.ObjectKeyFromObject(gc), gc))
	require.False(t, operandsRemovedTrue(gc))
	_, err = r.Reconcile(t.Context(), ctrl.Request{NamespacedName: client.ObjectKeyFromObject(gc)})
	require.NoError(t, err)
	require.NoError(t, c.Get(t.Context(), client.ObjectKeyFromObject(gc), gc))
	require.True(t, operandsRemovedTrue(gc))
}

func TestAdoptionConditionIsObservedAndLatched(t *testing.T) {
	cp := testSourceCP()
	cp.Status.Conditions = nil
	c := lifecycleClient(t, cp)
	ready, err := observeSourceAdoption(t.Context(), c, cp, "")
	require.NoError(t, err)
	require.False(t, ready)
	actual := &gpuv1.ClusterPolicy{}
	require.NoError(t, c.Get(t.Context(), client.ObjectKeyFromObject(cp), actual))
	require.True(t, nodeSelectionInitializedTrue(actual))
	ready, err = observeSourceAdoption(t.Context(), c, actual, coexistence.DRA)
	require.NoError(t, err)
	require.True(t, ready)
}

func TestGPUClusterAdoptionInitializesRequiredState(t *testing.T) {
	for name, tc := range map[string]struct {
		initial   gpu.State
		wantState gpu.State
	}{
		"initializes missing state": {wantState: gpu.NotReady},
		"preserves ready state":     {initial: gpu.Ready, wantState: gpu.Ready},
	} {
		t.Run(name, func(t *testing.T) {
			gc := testSourceGC()
			gc.Status.State = tc.initial
			c := lifecycleClient(t, gc)
			observed, err := observeSourceAdoption(t.Context(), c, gc, "")
			require.NoError(t, err)
			require.False(t, observed)
			require.NoError(t, c.Get(t.Context(), client.ObjectKeyFromObject(gc), gc))
			require.True(t, nodeSelectionInitializedTrue(gc))
			require.Equal(t, tc.wantState, gc.Status.State)
		})
	}
}

func TestModeCannotBypassTerminatingSource(t *testing.T) {
	for stack, tc := range map[string]struct {
		requestedMode string
		deployLabel   string
	}{
		coexistence.DP:  {coexistence.DRA, devicePluginDeployLabelKey},
		coexistence.DRA: {coexistence.DP, draDriverDeployLabelKey},
	} {
		t.Run(stack, func(t *testing.T) {
			node := selectedNode(stack)
			node.Labels[coexistence.ModeLabel] = tc.requestedMode
			node.Labels[commonGPULabelKey] = "false"
			node.Labels[tc.deployLabel] = "true"
			deletedAt := metav1.Now()
			nlc := &nodeLabelingController{}
			if stack == coexistence.DP {
				nlc.clusterPolicy = testSourceCP()
				nlc.clusterPolicy.DeletionTimestamp = &deletedAt
			} else {
				nlc.gpuCluster = testSourceGC()
				nlc.gpuCluster.DeletionTimestamp = &deletedAt
			}
			changed, err := nlc.selectNode(t.Context(), node)
			require.NoError(t, err)
			require.False(t, changed)
			require.Equal(t, stack, node.Annotations[coexistence.SelectionAnnotation])
			require.Equal(t, "true", node.Labels[tc.deployLabel])
		})
	}
}

func TestCleanupSameNameNewUIDIsNotOwned(t *testing.T) {
	gc := testSourceGC()
	old := gc.DeepCopy()
	old.UID = "old-uid"
	ds := &appsv1.DaemonSet{ObjectMeta: ownedMeta("old-plugin", "GPUCluster", old)}
	c := lifecycleClient(t, gc, ds)
	workloads, _, err := ownedWorkloads(t.Context(), c, "test", gc)
	require.NoError(t, err)
	require.Empty(t, workloads)
}

func TestCleanupPatchConflictPreservesConcurrentMode(t *testing.T) {
	cp := testSourceCP()
	node := selectedNode(coexistence.DP)
	base := lifecycleClient(t, cp, node)
	deleteSource(t, base, cp)
	require.NoError(t, setOperandsRemoved(t.Context(), base, cp, true, "TeardownComplete", "done"))
	racing := interceptor.NewClient(base.(client.WithWatch), interceptor.Funcs{Patch: func(ctx context.Context, c client.WithWatch, obj client.Object, p client.Patch, opts ...client.PatchOption) error {
		if _, ok := obj.(*corev1.Node); ok {
			latest := &corev1.Node{}
			require.NoError(t, c.Get(ctx, client.ObjectKeyFromObject(obj), latest))
			latest.Labels[coexistence.ModeLabel] = coexistence.DRA
			require.NoError(t, c.Update(ctx, latest))
		}
		return c.Patch(ctx, obj, p, opts...)
	}})
	r := &NodeLabelingReconciler{Client: racing}
	pending, err := r.cleanupDeletedSources(t.Context())
	require.NoError(t, err)
	require.True(t, pending)
	require.NoError(t, base.Get(t.Context(), client.ObjectKeyFromObject(cp), cp))
	require.Contains(t, cp.Finalizers, nodeCleanupFinalizer)
	r.Client = base
	_, err = r.cleanupDeletedSources(t.Context())
	require.NoError(t, err)
	require.NoError(t, base.Get(t.Context(), client.ObjectKeyFromObject(node), node))
	require.Equal(t, coexistence.DRA, node.Labels[coexistence.ModeLabel])
	require.NotContains(t, node.Annotations, coexistence.SelectionAnnotation)
}

func TestSimultaneousSourceDeletionConverges(t *testing.T) {
	cp := testSourceCP()
	gc := testSourceGC()
	node := selectedNode(coexistence.DRA)
	node.Labels[devicePluginDeployLabelKey] = "true"
	node.Labels[draDriverDeployLabelKey] = "false"
	c := lifecycleClient(t, cp, gc, node)
	deleteSource(t, c, cp)
	deleteSource(t, c, gc)
	require.NoError(t, setOperandsRemoved(t.Context(), c, cp, true, "TeardownComplete", "done"))
	require.NoError(t, setOperandsRemoved(t.Context(), c, gc, true, "TeardownComplete", "done"))
	r := &NodeLabelingReconciler{Client: c}
	for range 4 {
		_, err := r.cleanupDeletedSources(t.Context())
		require.NoError(t, err)
	}
	require.True(t, apierrors.IsNotFound(c.Get(t.Context(), client.ObjectKeyFromObject(cp), cp)))
	require.True(t, apierrors.IsNotFound(c.Get(t.Context(), client.ObjectKeyFromObject(gc), gc)))
	require.NoError(t, c.Get(t.Context(), client.ObjectKeyFromObject(node), node))
	require.NotContains(t, node.Labels, devicePluginDeployLabelKey)
	require.Equal(t, "false", node.Labels[draDriverDeployLabelKey])
	require.NotContains(t, node.Annotations, coexistence.SelectionAnnotation)
}

func TestTeardownAPIFailureKeepsReservation(t *testing.T) {
	cp := testSourceCP()
	ds := &appsv1.DaemonSet{ObjectMeta: ownedMeta("dp", "ClusterPolicy", cp)}
	base := lifecycleClient(t, cp, ds)
	deleteSource(t, base, cp)
	failing := interceptor.NewClient(base.(client.WithWatch), interceptor.Funcs{Delete: func(context.Context, client.WithWatch, client.Object, ...client.DeleteOption) error {
		return fmt.Errorf("delete denied")
	}})
	_, err := teardownSourceOperands(t.Context(), failing, "test", cp)
	require.ErrorContains(t, err, "delete denied")
	require.NoError(t, base.Get(t.Context(), client.ObjectKeyFromObject(cp), cp))
	require.False(t, operandsRemovedTrue(cp))
	require.Contains(t, cp.Finalizers, nodeCleanupFinalizer)
}

func TestSourceDeletionDoesNotStopIndependentDriverAssignment(t *testing.T) {
	cp := testSourceCP()
	cp.Spec.Driver.UseNvidiaDriverCRD = new(true)
	driver := &gpu.NVIDIADriver{ObjectMeta: metav1.ObjectMeta{Name: "independent-driver"}}
	node := &corev1.Node{ObjectMeta: metav1.ObjectMeta{Name: "node", Labels: map[string]string{
		commonGPULabelKey: "true",
		"feature.node.kubernetes.io/pci-10de.present": "true",
		"nvidia.com/gpu.deploy.device-plugin":         "true",
	}}}
	c := lifecycleClient(t, cp, driver, node)
	deleteSource(t, c, cp)
	r := &NodeLabelingReconciler{Client: c}
	result, err := r.Reconcile(t.Context(), ctrl.Request{})
	require.NoError(t, err)
	require.Equal(t, lifecycleRetry, result.RequeueAfter)
	require.NoError(t, c.Get(t.Context(), client.ObjectKeyFromObject(node), node))
	require.Equal(t, driver.Name, node.Labels[consts.NVIDIADriverOwnerLabel])
	require.Equal(t, "true", node.Labels["nvidia.com/gpu.deploy.device-plugin"])
	require.NotContains(t, node.Labels, "nvidia.com/gpu.deploy.dcgm-exporter")
}

func TestTeardownBlockerMessageStable(t *testing.T) {
	gc := testSourceGC()
	daemonSet := &appsv1.DaemonSet{ObjectMeta: ownedMeta("operand", "GPUCluster", gc)}
	targets := []client.Object{daemonSet}
	for i := range maxReportedWorkloads + 2 {
		targets = append(targets, &corev1.Pod{ObjectMeta: ownedMeta(fmt.Sprintf("validator-%d", i), "GPUCluster", gc)})
	}
	want := teardownBlockerMessage(targets, false)
	require.Contains(t, want, "test/validator-0")
	require.NotContains(t, want, "test/validator-6")
	slices.Reverse(targets)
	require.Equal(t, want, teardownBlockerMessage(targets, false))

	// The cache may return different validator Pods when a list reaches its limit.
	first := []client.Object{daemonSet, targets[0]}
	second := []client.Object{targets[1], daemonSet}
	require.Equal(t, teardownBlockerMessage(first, true), teardownBlockerMessage(second, true))
	require.Contains(t, teardownBlockerMessage(first, true), "at least 6 validator Pods remain")
}

func TestNodeCleanupContinuesAfterConflict(t *testing.T) {
	cp := testSourceCP()
	const label = "nvidia.com/gpu.deploy.device-plugin"
	nodeA := &corev1.Node{ObjectMeta: metav1.ObjectMeta{Name: "a", Labels: map[string]string{label: "true"}}}
	nodeB := &corev1.Node{ObjectMeta: metav1.ObjectMeta{Name: "b", Labels: map[string]string{label: "true"}}}
	base := lifecycleClient(t, cp, nodeA, nodeB)
	deleteSource(t, base, cp)
	require.NoError(t, setOperandsRemoved(t.Context(), base, cp, true, "TeardownComplete", "done"))
	var attempted []string
	r := &NodeLabelingReconciler{Client: interceptor.NewClient(base.(client.WithWatch), interceptor.Funcs{
		Patch: func(ctx context.Context, c client.WithWatch, obj client.Object, patch client.Patch, opts ...client.PatchOption) error {
			if node, ok := obj.(*corev1.Node); ok {
				attempted = append(attempted, node.Name)
				if len(attempted) == 1 {
					latest := &corev1.Node{}
					require.NoError(t, c.Get(ctx, client.ObjectKeyFromObject(node), latest))
					latest.Labels[label] = "false"
					require.NoError(t, c.Update(ctx, latest))
					err := c.Patch(ctx, node, patch, opts...)
					require.True(t, apierrors.IsConflict(err))
					return err
				}
			}
			return c.Patch(ctx, obj, patch, opts...)
		},
	})}

	pending, err := r.cleanupDeletedSources(t.Context())
	require.NoError(t, err)
	require.True(t, pending)
	require.ElementsMatch(t, []string{nodeA.Name, nodeB.Name}, attempted)
	require.NoError(t, base.Get(t.Context(), client.ObjectKeyFromObject(cp), cp))
	require.Contains(t, cp.Finalizers, nodeCleanupFinalizer)

	node := &corev1.Node{}
	require.NoError(t, base.Get(t.Context(), client.ObjectKey{Name: attempted[0]}, node))
	require.Equal(t, "false", node.Labels[label])
	require.NoError(t, base.Get(t.Context(), client.ObjectKey{Name: attempted[1]}, node))
	require.NotContains(t, node.Labels, label)

	_, err = r.cleanupDeletedSources(t.Context())
	require.NoError(t, err)
	require.True(t, apierrors.IsNotFound(base.Get(t.Context(), client.ObjectKeyFromObject(cp), cp)))
}

func TestDriverLabelCleanupWaitsForClusterPolicyOperands(t *testing.T) {
	cp := testSourceCP()
	cp.Spec.Driver.Enabled = new(true)
	gc := testSourceGC()
	node := &corev1.Node{ObjectMeta: metav1.ObjectMeta{Name: "node", Labels: map[string]string{
		driverDeployLabelKey:          "true",
		vgpuManagerDeployLabelKey:     "false",
		draDriverDeployLabelKey:       "true",
		consts.NVIDIADriverOwnerLabel: "stale-owner",
	}}}
	c := lifecycleClient(t, cp, gc, node)
	deleteSource(t, c, cp)
	deleteSource(t, c, gc)
	require.NoError(t, setOperandsRemoved(t.Context(), c, gc, true, "TeardownComplete", "done"))
	r := &NodeLabelingReconciler{Client: c}
	for range 2 {
		_, err := r.cleanupDeletedSources(t.Context())
		require.NoError(t, err)
	}
	require.True(t, apierrors.IsNotFound(c.Get(t.Context(), client.ObjectKeyFromObject(gc), gc)))
	require.NoError(t, c.Get(t.Context(), client.ObjectKeyFromObject(node), node))
	require.NotContains(t, node.Labels, draDriverDeployLabelKey)
	require.Equal(t, "true", node.Labels[driverDeployLabelKey])

	require.NoError(t, setOperandsRemoved(t.Context(), c, cp, true, "TeardownComplete", "done"))
	for range 2 {
		_, err := r.cleanupDeletedSources(t.Context())
		require.NoError(t, err)
	}
	require.True(t, apierrors.IsNotFound(c.Get(t.Context(), client.ObjectKeyFromObject(cp), cp)))
	require.NoError(t, c.Get(t.Context(), client.ObjectKeyFromObject(node), node))
	require.NotContains(t, node.Labels, driverDeployLabelKey)
	require.Equal(t, "false", node.Labels[vgpuManagerDeployLabelKey])
	require.Equal(t, "stale-owner", node.Labels[consts.NVIDIADriverOwnerLabel])
}

func TestDriverLabelCleanupChecksDriverCRs(t *testing.T) {
	for _, keepDriver := range []bool{false, true} {
		t.Run(fmt.Sprintf("driver CR exists=%t", keepDriver), func(t *testing.T) {
			cp := testSourceCP()
			cp.Spec.Driver.UseNvidiaDriverCRD = new(true)
			node := &corev1.Node{ObjectMeta: metav1.ObjectMeta{Name: "node", Labels: map[string]string{
				driverDeployLabelKey:          "true",
				vgpuManagerDeployLabelKey:     "true",
				consts.NVIDIADriverOwnerLabel: "driver",
			}}}
			objects := []client.Object{cp, node}
			if keepDriver {
				objects = append(objects, &gpu.NVIDIADriver{ObjectMeta: metav1.ObjectMeta{Name: "driver"}})
			}
			c := lifecycleClient(t, objects...)
			deleteSource(t, c, cp)
			require.NoError(t, setOperandsRemoved(t.Context(), c, cp, true, "TeardownComplete", "done"))
			r := &NodeLabelingReconciler{Client: c}
			for range 2 {
				_, err := r.cleanupDeletedSources(t.Context())
				require.NoError(t, err)
			}
			require.True(t, apierrors.IsNotFound(c.Get(t.Context(), client.ObjectKeyFromObject(cp), cp)))
			require.NoError(t, c.Get(t.Context(), client.ObjectKeyFromObject(node), node))
			_, hasDriverLabel := node.Labels[driverDeployLabelKey]
			_, hasVGPUManagerLabel := node.Labels[vgpuManagerDeployLabelKey]
			require.Equal(t, keepDriver, hasDriverLabel)
			require.Equal(t, keepDriver, hasVGPUManagerLabel)
		})
	}
}
