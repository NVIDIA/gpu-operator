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
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
	admissionv1 "k8s.io/api/admissionregistration/v1"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	resourcev1 "k8s.io/api/resource/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/interceptor"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"

	gpuv1 "github.com/NVIDIA/gpu-operator/api/nvidia/v1"
	nvidiav1alpha1 "github.com/NVIDIA/gpu-operator/api/nvidia/v1alpha1"
	"github.com/NVIDIA/gpu-operator/internal/consts"
	driverownership "github.com/NVIDIA/gpu-operator/internal/nvidiadriver"
)

func techPreviewMigrationDaemonSet(t *testing.T, scheme *runtime.Scheme, cr *nvidiav1alpha1.NVIDIADriver, node *corev1.Node) *appsv1.DaemonSet {
	t.Helper()
	assigned := node.DeepCopy()
	assigned.Labels[consts.NVIDIADriverOwnerLabel] = cr.Name
	state := newTestStateDriver(t, newDriverFakeClient(scheme, assigned), scheme)
	objects, err := state.getManifestObjects(t.Context(), cr, driverInfoCatalog(fakeClusterInfo{}))
	require.NoError(t, err)
	ds, err := getDaemonsetFromObjects(objects)
	require.NoError(t, err)
	ds.UID = "tech-preview-daemonset"
	require.NoError(t, controllerutil.SetControllerReference(cr, ds, scheme))
	delete(ds.Spec.Template.Spec.NodeSelector, consts.NVIDIADriverOwnerLabel)
	ds.Status.DesiredNumberScheduled = 2
	return ds
}

// Model the API server retaining a UID omitted by PUT; the fake client would
// otherwise clear it. A DELETE must fail the migration test rather than silently
// allowing the fake client to create another object with an empty UID.
func migrationClient(c client.Client) client.Client {
	return interceptor.NewClient(c.(client.WithWatch), interceptor.Funcs{
		Update: func(ctx context.Context, c client.WithWatch, obj client.Object, opts ...client.UpdateOption) error {
			if obj.GetObjectKind().GroupVersionKind().Kind == "DaemonSet" {
				current := &appsv1.DaemonSet{}
				if err := c.Get(ctx, client.ObjectKeyFromObject(obj), current); err != nil {
					return err
				}
				obj.SetUID(current.UID)
			}
			return c.Update(ctx, obj, opts...)
		},
		Delete: func(ctx context.Context, c client.WithWatch, obj client.Object, opts ...client.DeleteOption) error {
			switch obj.(type) {
			case *appsv1.DaemonSet, *corev1.Pod:
				return errors.New("unexpected deletion during ownership migration")
			}
			return c.Delete(ctx, obj, opts...)
		},
	})
}

func observeDriverOwner(t *testing.T, c client.Client, name, owner string) {
	t.Helper()
	node := &corev1.Node{}
	require.NoError(t, c.Get(t.Context(), client.ObjectKey{Name: name}, node))
	node.Labels[consts.NVIDIADriverOwnerLabel] = owner
	require.NoError(t, c.Update(t.Context(), node))
}

func TestTechPreviewDriverOwnershipMigration(t *testing.T) {
	for name, tc := range map[string]struct {
		driverType                                         nvidiav1alpha1.DriverType
		precompiled, updateDriver, defaultDriver, inactive bool
	}{
		"GPU tainted inactive pool": {driverType: nvidiav1alpha1.GPU, inactive: true},
		"GPU pinned images":         {driverType: nvidiav1alpha1.GPU},
		"GPU driver update":         {driverType: nvidiav1alpha1.GPU, updateDriver: true},
		"GPU default fallback":      {driverType: nvidiav1alpha1.GPU, defaultDriver: true},
		"GPU precompiled":           {driverType: nvidiav1alpha1.GPU, precompiled: true},
		"vGPU":                      {driverType: nvidiav1alpha1.VGPU},
		"vGPU host manager":         {driverType: nvidiav1alpha1.VGPUHostManager},
	} {
		for _, autoUpgrade := range []bool{false, true} {
			t.Run(fmt.Sprintf("%s/autoUpgrade=%t", name, autoUpgrade), func(t *testing.T) {
				scheme := driverTestScheme(t)
				cr := newDriverCR("default")
				cr.Spec.DriverType = tc.driverType
				cr.Spec.UsePrecompiled = new(tc.precompiled)
				cr.Spec.Default = tc.defaultDriver
				cr.Spec.UpgradePolicy = &nvidiav1alpha1.DriverUpgradePolicySpec{AutoUpgrade: autoUpgrade, MaxParallelUpgrades: 1}
				first := newGPUNode("first", "")
				first.Labels[nfdKernelLabelKey] = "6.8.0-99-generic"
				deployLabel := "nvidia.com/gpu.deploy.driver"
				if tc.driverType == nvidiav1alpha1.VGPUHostManager {
					deployLabel = "nvidia.com/gpu.deploy.vgpu-manager"
				}
				first.Labels[deployLabel] = "true"
				if tc.inactive {
					first.Spec.Taints = []corev1.Taint{{Key: "example.com/untolerated", Effect: corev1.TaintEffectNoSchedule}}
				}

				second := first.DeepCopy()
				second.Name = "second"
				ds := techPreviewMigrationDaemonSet(t, scheme, cr, first)
				if tc.inactive {
					ds.Status.DesiredNumberScheduled = 0
				}
				oldTemplate := ds.Spec.Template.DeepCopy()
				if tc.updateDriver {
					cr.Spec.Version = "535.161.07"
				}
				pod := &corev1.Pod{ObjectMeta: metav1.ObjectMeta{Name: "existing-driver", Namespace: ds.Namespace, UID: "existing-pod", OwnerReferences: []metav1.OwnerReference{{APIVersion: "apps/v1", Kind: "DaemonSet", UID: ds.UID, Controller: new(true)}}}, Spec: corev1.PodSpec{NodeName: second.Name}}
				cached := migrationClient(newDriverFakeClient(scheme, cr, first, second, ds, pod))
				authoritative := newDriverFakeClient(scheme, cr, first, second)
				state := newTestStateDriver(t, cached, scheme)
				checkPending := func() {
					status, err := state.Sync(t.Context(), cr, driverInfoCatalog(fakeClusterInfo{}))
					require.ErrorContains(t, err, "waiting for initial driver ownership assignment")
					require.NotContains(t, err.Error(), "failed to check")
					require.Equal(t, SyncState(SyncStateNotReady), status)
					actual := &appsv1.DaemonSet{}
					require.NoError(t, cached.Get(t.Context(), client.ObjectKeyFromObject(ds), actual))
					require.Equal(t, ds.UID, actual.UID)
					require.Equal(t, *oldTemplate, actual.Spec.Template)
				}
				checkPending()
				// Successful assignment writes do not help until the informer observes them.
				_, err := driverownership.AssignOwners(t.Context(), authoritative)
				require.NoError(t, err)
				checkPending()
				observeDriverOwner(t, cached, first.Name, cr.Name)
				checkPending()
				observeDriverOwner(t, cached, second.Name, cr.Name)
				_, err = state.Sync(t.Context(), cr, driverInfoCatalog(fakeClusterInfo{}))
				require.NoError(t, err)
				actual := &appsv1.DaemonSet{}
				require.NoError(t, cached.Get(t.Context(), client.ObjectKeyFromObject(ds), actual))
				require.Equal(t, ds.UID, actual.UID)
				require.Equal(t, cr.Name, actual.Spec.Template.Spec.NodeSelector[consts.NVIDIADriverOwnerLabel])
				require.Equal(t, appsv1.OnDeleteDaemonSetStrategyType, actual.Spec.UpdateStrategy.Type)
				if tc.updateDriver {
					require.True(t, strings.Contains(actual.Spec.Template.Spec.Containers[0].Image, cr.Spec.Version))
				}
				currentPod := &corev1.Pod{}
				require.NoError(t, cached.Get(t.Context(), client.ObjectKeyFromObject(pod), currentPod))
				require.Equal(t, pod.UID, currentPod.UID)
				actualCR := &nvidiav1alpha1.NVIDIADriver{}
				require.NoError(t, cached.Get(t.Context(), client.ObjectKeyFromObject(cr), actualCR))
				require.Equal(t, cr.UID, actualCR.UID)
				// Once migrated, a later unassigned node must not block ordinary updates.
				late := newGPUNode("late-node", "")
				late.Labels[nfdKernelLabelKey] = first.Labels[nfdKernelLabelKey]
				require.NoError(t, cached.Create(t.Context(), late))
				actualCR.Spec.Version = "535.183.01"
				require.NoError(t, cached.Update(t.Context(), actualCR))
				_, err = state.Sync(t.Context(), actualCR, driverInfoCatalog(fakeClusterInfo{}))
				require.NoError(t, err)
				require.NoError(t, cached.Get(t.Context(), client.ObjectKeyFromObject(ds), actual))
				require.Equal(t, ds.UID, actual.UID)
				observeDriverOwner(t, cached, late.Name, cr.Name)
				_, err = state.Sync(t.Context(), actualCR, driverInfoCatalog(fakeClusterInfo{}))
				require.NoError(t, err)
				require.NoError(t, cached.Get(t.Context(), client.ObjectKeyFromObject(ds), actual))
				require.Equal(t, ds.UID, actual.UID)

			})
		}
	}
}

func TestMigrationCheckPrecedesManifestRendering(t *testing.T) {
	scheme := driverTestScheme(t)
	cr := newDriverCR("driver-a")
	node := newGPUNode("node", "")
	ds := techPreviewMigrationDaemonSet(t, scheme, cr, node)
	c := newDriverFakeClient(scheme, cr, node, ds)
	nodeLists := 0
	c = interceptor.NewClient(c.(client.WithWatch), interceptor.Funcs{List: func(ctx context.Context, c client.WithWatch, list client.ObjectList, opts ...client.ListOption) error {
		if err := c.List(ctx, list, opts...); err != nil {
			return err
		}
		if _, ok := list.(*corev1.NodeList); ok {
			nodeLists++
			if nodeLists == 1 {
				// Advance the cache immediately after returning its old node snapshot.
				current := &corev1.Node{}
				if err := c.Get(ctx, client.ObjectKeyFromObject(node), current); err != nil {
					return err
				}
				current.Labels[consts.NVIDIADriverOwnerLabel] = cr.Name
				return c.Update(ctx, current)
			}
		}
		return nil
	}})
	c = migrationClient(c)
	state := newTestStateDriver(t, c, scheme)
	_, err := state.Sync(t.Context(), cr, driverInfoCatalog(fakeClusterInfo{}))
	require.ErrorContains(t, err, "waiting for initial driver ownership assignment")
	require.Equal(t, 1, nodeLists, "rendering must not run with an older snapshot")
	_, err = state.Sync(t.Context(), cr, driverInfoCatalog(fakeClusterInfo{}))
	require.NoError(t, err)
	actual := &appsv1.DaemonSet{}
	require.NoError(t, c.Get(t.Context(), client.ObjectKeyFromObject(ds), actual))
	require.Equal(t, ds.UID, actual.UID)
}

func TestMigrationAllowsExistingRemovalPaths(t *testing.T) {
	for name, modify := range map[string]func(*nvidiav1alpha1.NVIDIADriver, *corev1.Node){
		"selector no longer matches": func(cr *nvidiav1alpha1.NVIDIADriver, _ *corev1.Node) {
			cr.Spec.NodeSelector = map[string]string{"pool": "other"}
		},
		"selector excludes GPU nodes": func(cr *nvidiav1alpha1.NVIDIADriver, _ *corev1.Node) {
			cr.Spec.NodeSelector = map[string]string{consts.GPUPresentLabel: "false"}
		},

		"GPU eligibility removed": func(_ *nvidiav1alpha1.NVIDIADriver, n *corev1.Node) { n.Labels[consts.GPUPresentLabel] = "false" },
		"node removed":            func(_ *nvidiav1alpha1.NVIDIADriver, n *corev1.Node) { n.Name = "" },
		"assigned to another driver": func(_ *nvidiav1alpha1.NVIDIADriver, n *corev1.Node) {
			n.Labels[consts.NVIDIADriverOwnerLabel] = "other"
		},
	} {
		t.Run(name, func(t *testing.T) {
			scheme := driverTestScheme(t)
			cr := newDriverCR("driver-a")
			node := newGPUNode("node", "")
			ds := techPreviewMigrationDaemonSet(t, scheme, cr, node)
			modify(cr, node)
			objects := []client.Object{cr, ds}
			if node.Name != "" {
				objects = append(objects, node)
			}
			c := newDriverFakeClient(scheme, objects...)
			state := newTestStateDriver(t, c, scheme)
			_, err := state.Sync(t.Context(), cr, driverInfoCatalog(fakeClusterInfo{}))
			require.NoError(t, err)
			require.True(t, apierrors.IsNotFound(c.Get(t.Context(), client.ObjectKeyFromObject(ds), &appsv1.DaemonSet{})))
		})
	}
}

func TestMigrationNodeListFailurePreservesDaemonSet(t *testing.T) {
	scheme := driverTestScheme(t)
	cr := newDriverCR("driver-a")
	node := newGPUNode("node", "")
	ds := techPreviewMigrationDaemonSet(t, scheme, cr, node)
	listErr := errors.New("node cache read failed")
	c := newDriverClientWithInterceptors(scheme, failingListInterceptor[*corev1.NodeList](listErr), cr, node, ds)
	state := newTestStateDriver(t, c, scheme)
	status, err := state.Sync(t.Context(), cr, driverInfoCatalog(fakeClusterInfo{}))
	require.ErrorIs(t, err, listErr)
	require.ErrorContains(t, err, "failed to check driver node ownership")
	require.Equal(t, SyncState(SyncStateNotReady), status)
	actual := &appsv1.DaemonSet{}
	require.NoError(t, c.Get(t.Context(), client.ObjectKeyFromObject(ds), actual))
	require.Equal(t, ds.Spec.Template, actual.Spec.Template)
}

func TestMigrationCheckStopsAfterOwnerSelectorIsAdded(t *testing.T) {
	scheme := driverTestScheme(t)
	cr := newDriverCR("driver-a")
	techPreview := techPreviewMigrationDaemonSet(t, scheme, cr, newGPUNode("node-0", cr.Name))
	objects := []client.Object{cr, techPreview}
	for i := range 5000 {
		node := newGPUNode(fmt.Sprintf("node-%d", i), cr.Name)
		if i == 4999 {
			delete(node.Labels, consts.NVIDIADriverOwnerLabel)
		}
		objects = append(objects, node)
	}
	nodeLists := 0
	c := newDriverClientWithInterceptors(scheme, interceptor.Funcs{List: func(ctx context.Context, c client.WithWatch, list client.ObjectList, opts ...client.ListOption) error {
		if _, ok := list.(*corev1.NodeList); ok {
			options := &client.ListOptions{}
			options.ApplyOptions(opts)
			// Pool discovery already filters by owner; count only the migration lookup.
			if !strings.Contains(options.LabelSelector.String(), consts.NVIDIADriverOwnerLabel) {
				nodeLists++
			}
		}
		return c.List(ctx, list, opts...)
	}}, objects...)
	state := newTestStateDriver(t, c, scheme)
	for range 3 {
		_, err := state.Sync(t.Context(), cr, driverInfoCatalog(fakeClusterInfo{}))
		require.ErrorContains(t, err, "waiting for initial driver ownership assignment")
	}
	require.Equal(t, 3, nodeLists, "one cached node list per migration check")
	current := &appsv1.DaemonSet{}
	require.NoError(t, c.Get(t.Context(), client.ObjectKeyFromObject(techPreview), current))
	current.Spec.Template.Spec.NodeSelector = map[string]string{consts.NVIDIADriverOwnerLabel: cr.Name}
	require.NoError(t, c.Update(t.Context(), current))
	for range 3 {
		_, err := state.Sync(t.Context(), cr, driverInfoCatalog(fakeClusterInfo{}))
		require.NoError(t, err)
	}
	require.Equal(t, 3, nodeLists, "future reconciles must not check migration nodes")
}

func TestMigrationIgnoresDaemonSetsOutsideDriverOwnership(t *testing.T) {
	for name, ref := range map[string]*metav1.OwnerReference{
		"ClusterPolicy":      {APIVersion: gpuv1.SchemeGroupVersion.String(), Kind: "ClusterPolicy", Name: "default", UID: "policy", Controller: new(true)},
		"GPUCluster DRA":     {APIVersion: nvidiav1alpha1.SchemeGroupVersion.String(), Kind: "GPUCluster", Name: "default", UID: "cluster", Controller: new(true)},
		"other NVIDIADriver": {APIVersion: nvidiav1alpha1.SchemeGroupVersion.String(), Kind: "NVIDIADriver", Name: "another", UID: "other", Controller: new(true)},
		"unmanaged":          nil,
	} {
		t.Run(name, func(t *testing.T) {
			scheme := driverTestScheme(t)
			cr := newDriverCR("default")
			ds := newDaemonSet("existing-driver", "", 1, 0, nil)
			ds.UID = "unaffected"
			if ref != nil {
				ds.OwnerReferences = []metav1.OwnerReference{*ref}
			}
			c := newDriverFakeClient(scheme, cr, ds, newGPUNode("unassigned", ""))
			state := newTestStateDriver(t, c, scheme)
			_, err := state.Sync(t.Context(), cr, driverInfoCatalog(fakeClusterInfo{}))
			require.NoError(t, err)
			actual := &appsv1.DaemonSet{}
			require.NoError(t, c.Get(t.Context(), client.ObjectKeyFromObject(ds), actual))
			require.Equal(t, ds, actual)
		})
	}
}

func TestFreshNVIDIADriverInstallDoesNotWaitForMigration(t *testing.T) {
	scheme := driverTestScheme(t)
	cr := newDriverCR("driver-a")
	c := newDriverFakeClient(scheme, cr, newGPUNode("node", ""))
	state := newTestStateDriver(t, c, scheme)
	_, err := state.Sync(t.Context(), cr, driverInfoCatalog(fakeClusterInfo{}))
	require.NoError(t, err)
	sets := &appsv1.DaemonSetList{}
	require.NoError(t, c.List(t.Context(), sets))
	require.Empty(t, sets.Items)
	_, err = driverownership.AssignOwners(t.Context(), c)
	require.NoError(t, err)
	_, err = state.Sync(t.Context(), cr, driverInfoCatalog(fakeClusterInfo{}))
	require.NoError(t, err)
	require.NoError(t, c.List(t.Context(), sets))
	require.Len(t, sets.Items, 1)
	require.Equal(t, cr.Name, sets.Items[0].Spec.Template.Spec.NodeSelector[consts.NVIDIADriverOwnerLabel])
}

func TestDRADriverSyncDoesNotRequireNVIDIADriverOwnership(t *testing.T) {
	scheme := driverTestScheme(t)
	require.NoError(t, admissionv1.AddToScheme(scheme))
	require.NoError(t, resourcev1.AddToScheme(scheme))
	cluster := sampleGPUCluster()
	cluster.UID = "cluster-uid"
	client := newDriverClientWithInterceptors(scheme, interceptor.Funcs{List: func(ctx context.Context, c client.WithWatch, list client.ObjectList, opts ...client.ListOption) error {
		switch list.(type) {
		case *corev1.NodeList, *nvidiav1alpha1.NVIDIADriverList:
			return errors.New("DRA plugin must not require driver ownership")
		}
		return c.List(ctx, list, opts...)
	}}, cluster, newGPUNode("node", ""))
	state := newTestDRAState(t)
	state.client = client
	state.scheme = scheme
	_, err := state.Sync(t.Context(), cluster, draSupportedCatalog())
	require.NoError(t, err)
	sets := &appsv1.DaemonSetList{}
	require.NoError(t, client.List(t.Context(), sets))
	require.NotEmpty(t, sets.Items)
	for _, ds := range sets.Items {
		require.Equal(t, "GPUCluster", metav1.GetControllerOf(&ds).Kind)
		require.NotContains(t, ds.Spec.Template.Spec.NodeSelector, consts.NVIDIADriverOwnerLabel)
	}
}

func TestMigrationIgnoresUnselectedUnassignedNodes(t *testing.T) {
	scheme := driverTestScheme(t)
	cr := newDriverCR("driver-a")
	cr.Spec.NodeSelector = map[string]string{"pool": "selected"}
	selected := newGPUNode("selected", cr.Name)
	selected.Labels["pool"] = "selected"
	other := newGPUNode("other", "")
	other.Labels["pool"] = "other"
	ds := techPreviewMigrationDaemonSet(t, scheme, cr, selected)
	c := migrationClient(newDriverFakeClient(scheme, cr, selected, other, ds))
	state := newTestStateDriver(t, c, scheme)
	_, err := state.Sync(t.Context(), cr, driverInfoCatalog(fakeClusterInfo{}))
	require.NoError(t, err)
	actual := &appsv1.DaemonSet{}
	require.NoError(t, c.Get(t.Context(), client.ObjectKeyFromObject(ds), actual))
	require.Equal(t, ds.UID, actual.UID)
	require.Equal(t, cr.Name, actual.Spec.Template.Spec.NodeSelector[consts.NVIDIADriverOwnerLabel])
}
