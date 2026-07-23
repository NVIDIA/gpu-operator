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
	"fmt"
	"strings"
	"testing"

	"github.com/google/go-cmp/cmp"
	"github.com/google/go-cmp/cmp/cmpopts"
	configv1 "github.com/openshift/api/config/v1"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/utils/ptr"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/cache"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/client/interceptor"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"

	gpuv1 "github.com/NVIDIA/gpu-operator/api/nvidia/v1"
	nvidiav1alpha1 "github.com/NVIDIA/gpu-operator/api/nvidia/v1alpha1"
	driverconfig "github.com/NVIDIA/gpu-operator/internal/config"
	"github.com/NVIDIA/gpu-operator/internal/consts"
	"github.com/NVIDIA/gpu-operator/internal/utils"
)

// coreAppsScheme returns a scheme with only core and apps types registered
// (no NVIDIADriver), used to trigger SetControllerReference errors.
func coreAppsScheme(t *testing.T) *runtime.Scheme {
	t.Helper()
	s := runtime.NewScheme()
	require.NoError(t, corev1.AddToScheme(s))
	require.NoError(t, appsv1.AddToScheme(s))
	return s
}

func fullCatalog() InfoCatalog {
	catalog := NewInfoCatalog()
	catalog.Add(InfoTypeClusterPolicyCR, gpuv1.ClusterPolicy{})
	catalog.Add(InfoTypeClusterInfo, fakeClusterInfo{})
	return catalog
}

// --- NewStateDriver error path -------------------------------------------------

func TestNewStateDriverBadManifestDir(t *testing.T) {
	_, err := NewStateDriver(nil, "", nil, "/nonexistent/manifest/dir")
	require.ErrorContains(t, err, "failed to get files from manifest directory")
}

// --- getDriverName truncation --------------------------------------------------

func TestGetDriverNameTruncation(t *testing.T) {
	cr := &nvidiav1alpha1.NVIDIADriver{
		ObjectMeta: metav1.ObjectMeta{Name: strings.Repeat("a", 300)},
		Spec:       nvidiav1alpha1.NVIDIADriverSpec{DriverType: nvidiav1alpha1.GPU},
	}
	name := getDriverName(cr, "ubuntu22.04")
	assert.Len(t, name, 253)
}

// --- getDriverSpec manager image error -----------------------------------------

func TestGetDriverSpecManagerImageError(t *testing.T) {
	// Ensure the fallback env var is not set so an empty Manager image errors.
	t.Setenv("DRIVER_MANAGER_IMAGE", "")
	cr := &nvidiav1alpha1.NVIDIADriver{
		ObjectMeta: metav1.ObjectMeta{Name: "driver-a"},
		Spec: nvidiav1alpha1.NVIDIADriverSpec{
			DriverType: nvidiav1alpha1.GPU,
			Repository: "nvcr.io/nvidia",
			Image:      "driver",
			Version:    "535.104.05",
			// Manager repository/image/version all empty -> image.ImagePath errors.
			Manager: nvidiav1alpha1.DriverManagerSpec{},
		},
	}
	_, err := getDriverSpec(cr, nodePool{osTag: "ubuntu22.04"})
	require.ErrorContains(t, err, "failed to construct image path for driver manager")
}

// --- getObjectOfKind / getDaemonsetFromObjects errors --------------------------

func TestGetObjectOfKindNotFound(t *testing.T) {
	_, err := getObjectOfKind([]*unstructured.Unstructured{}, "DaemonSet")
	require.ErrorContains(t, err, "did not find object of kind")
}

func TestGetDaemonsetFromObjectsErrors(t *testing.T) {
	// No DaemonSet present.
	_, err := getDaemonsetFromObjects([]*unstructured.Unstructured{newConfigMapUnstructured("cm", "ns")})
	require.ErrorContains(t, err, "did not find object of kind")

	// A DaemonSet-kinded object whose nested fields have the wrong type -> conversion error.
	bad := newDaemonSetUnstructured("ds-bad", "ns")
	bad.Object["spec"] = "not-a-spec-object"
	_, err = getDaemonsetFromObjects([]*unstructured.Unstructured{bad})
	require.ErrorContains(t, err, "error converting unstructured object to DaemonSet")
}

// --- renderManifestObjects error path ------------------------------------------

func TestRenderManifestObjectsError(t *testing.T) {
	state, err := NewStateDriver(nil, "", nil, manifestDir)
	require.NoError(t, err)
	sd := state.(*stateDriver)

	// Empty render data: templates dereference .Driver.Spec fields, which are nil,
	// causing template execution to fail.
	_, err = sd.renderManifestObjects(context.Background(), &driverRenderData{})
	require.Error(t, err)
}

// --- getManifestObjects error/branch coverage ----------------------------------

func TestGetManifestObjectsRuntimeSpecError(t *testing.T) {
	sch := driverTestScheme(t)
	cl := driverIndexBuilder(sch)
	state, err := NewStateDriver(cl, "test-operator", sch, manifestDir)
	require.NoError(t, err)
	sd := state.(*stateDriver)

	catalog := NewInfoCatalog()
	catalog.Add(InfoTypeClusterPolicyCR, gpuv1.ClusterPolicy{})
	catalog.Add(InfoTypeClusterInfo, fakeClusterInfo{openshiftVersionErr: fmt.Errorf("boom")})

	_, err = sd.getManifestObjects(context.Background(), newDriverCR("driver-a"), catalog)
	require.ErrorContains(t, err, "failed to construct cluster runtime spec")
}

func TestGetManifestObjectsNodeListError(t *testing.T) {
	sch := driverTestScheme(t)
	cl := fake.NewClientBuilder().WithScheme(sch).
		WithIndex(&appsv1.DaemonSet{}, consts.NVIDIADriverControllerIndexKey, func(_ client.Object) []string { return nil }).
		WithInterceptorFuncs(interceptor.Funcs{
			List: func(_ context.Context, _ client.WithWatch, list client.ObjectList, _ ...client.ListOption) error {
				if _, ok := list.(*corev1.NodeList); ok {
					return fmt.Errorf("injected node list error")
				}
				return nil
			},
		}).Build()
	state, err := NewStateDriver(cl, "test-operator", sch, manifestDir)
	require.NoError(t, err)
	sd := state.(*stateDriver)

	_, err = sd.getManifestObjects(context.Background(), newDriverCR("driver-a"), fullCatalog())
	require.ErrorContains(t, err, "failed to get node pools")
}

func TestGetManifestObjectsDriverSpecError(t *testing.T) {
	sch := driverTestScheme(t)
	cl := driverIndexBuilder(sch, newGPUNode("gpu-node", "driver-a"))
	state, err := NewStateDriver(cl, "test-operator", sch, manifestDir)
	require.NoError(t, err)
	sd := state.(*stateDriver)

	cr := newDriverCR("driver-a")
	cr.Spec.Image = "INVALID IMAGE" // breaks getDriverImagePath inside getDriverSpec

	_, err = sd.getManifestObjects(context.Background(), cr, fullCatalog())
	require.ErrorContains(t, err, "failed to construct driver spec")
}

func TestGetManifestObjectsGDSError(t *testing.T) {
	sch := driverTestScheme(t)
	cl := driverIndexBuilder(sch, newGPUNode("gpu-node", "driver-a"))
	state, err := NewStateDriver(cl, "test-operator", sch, manifestDir)
	require.NoError(t, err)
	sd := state.(*stateDriver)

	cr := newDriverCR("driver-a")
	cr.Spec.GPUDirectStorage = &nvidiav1alpha1.GPUDirectStorageSpec{
		Enabled: ptr.To(true),
		Image:   "INVALID IMAGE",
	}

	_, err = sd.getManifestObjects(context.Background(), cr, fullCatalog())
	require.ErrorContains(t, err, "failed to construct GDS spec")
}

func TestGetManifestObjectsGDRCopyError(t *testing.T) {
	sch := driverTestScheme(t)
	cl := driverIndexBuilder(sch, newGPUNode("gpu-node", "driver-a"))
	state, err := NewStateDriver(cl, "test-operator", sch, manifestDir)
	require.NoError(t, err)
	sd := state.(*stateDriver)

	cr := newDriverCR("driver-a")
	cr.Spec.GDRCopy = &nvidiav1alpha1.GDRCopySpec{
		Enabled: ptr.To(true),
		Image:   "INVALID IMAGE",
	}

	_, err = sd.getManifestObjects(context.Background(), cr, fullCatalog())
	require.ErrorContains(t, err, "failed to construct GDRCopy spec")
}

func TestGetManifestObjectsPrecompiled(t *testing.T) {
	sch := driverTestScheme(t)
	node := newGPUNode("gpu-node", "driver-a")
	node.Labels[nfdKernelLabelKey] = "5.15.0-70-generic"
	cl := driverIndexBuilder(sch, node)
	state, err := NewStateDriver(cl, "test-operator", sch, manifestDir)
	require.NoError(t, err)
	sd := state.(*stateDriver)

	cr := newDriverCR("driver-a")
	cr.Spec.UsePrecompiled = ptr.To(true)

	objs, err := sd.getManifestObjects(context.Background(), cr, fullCatalog())
	require.NoError(t, err)
	require.NotEmpty(t, objs)
}

func TestGetManifestObjectsOpenshiftDTK(t *testing.T) {
	sch := driverTestScheme(t)
	const rhcosVersion = "413.92.202304252344-0"
	node := &corev1.Node{ObjectMeta: metav1.ObjectMeta{
		Name: "rhcos-node",
		Labels: map[string]string{
			consts.GPUPresentLabel:        "true",
			consts.NVIDIADriverOwnerLabel: "driver-a",
			nfdOSReleaseIDLabelKey:        "rhcos",
			nfdOSVersionIDLabelKey:        "4.13",
			nfdOSTreeVersionLabelKey:      rhcosVersion,
		},
	}}
	cl := driverIndexBuilder(sch, node)
	state, err := NewStateDriver(cl, "test-operator", sch, manifestDir)
	require.NoError(t, err)
	sd := state.(*stateDriver)

	catalog := NewInfoCatalog()
	catalog.Add(InfoTypeClusterPolicyCR, gpuv1.ClusterPolicy{})
	catalog.Add(InfoTypeClusterInfo, fakeClusterInfo{
		openshiftVersion: "4.13",
		dtkImages: map[string]string{
			rhcosVersion: "quay.io/openshift-release-dev/ocp-v4.0-art-dev@sha256:7fecaebc1d51b28bc3548171907e4d91823a031d7a6a694ab686999be2b4d867",
		},
	})

	cr := newDriverCR("driver-a")
	objs, err := sd.getManifestObjects(context.Background(), cr, catalog)
	require.NoError(t, err)
	require.NotEmpty(t, objs)
}

func TestGetManifestObjectsAdditionalConfigsErrorIsLogged(t *testing.T) {
	// getDriverAdditionalConfigs failing only logs the error; manifest generation continues.
	sch := driverTestScheme(t)
	cl := driverIndexBuilder(sch, newGPUNode("gpu-node", "driver-a"))
	state, err := NewStateDriver(cl, "test-operator", sch, manifestDir)
	require.NoError(t, err)
	sd := state.(*stateDriver)

	cr := newDriverCR("driver-a")
	// Reference a ConfigMap that does not exist -> createConfigMapVolumeMounts errors.
	cr.Spec.RepoConfig = &nvidiav1alpha1.DriverRepoConfigSpec{Name: "missing-repo-config"}

	objs, err := sd.getManifestObjects(context.Background(), cr, fullCatalog())
	require.NoError(t, err)
	require.NotEmpty(t, objs)
}

func TestGetManifestObjectsHandleDefaultImagesError(t *testing.T) {
	sch := driverTestScheme(t)
	cl := fake.NewClientBuilder().WithScheme(sch).
		WithObjects(newGPUNode("gpu-node", "driver-a")).
		WithIndex(&appsv1.DaemonSet{}, consts.NVIDIADriverControllerIndexKey, func(_ client.Object) []string { return nil }).
		WithInterceptorFuncs(interceptor.Funcs{
			Get: func(_ context.Context, _ client.WithWatch, _ client.ObjectKey, obj client.Object, _ ...client.GetOption) error {
				if _, ok := obj.(*appsv1.DaemonSet); ok {
					return fmt.Errorf("injected daemonset get error")
				}
				return nil
			},
		}).Build()
	state, err := NewStateDriver(cl, "test-operator", sch, manifestDir)
	require.NoError(t, err)
	sd := state.(*stateDriver)

	cr := newDriverCR("driver-a")
	cr.Spec.Manager.Image = "" // triggers default-image handling that Gets the current DaemonSet

	_, err = sd.getManifestObjects(context.Background(), cr, fullCatalog())
	require.ErrorContains(t, err, "failed to get current driver DaemonSet")
}

// --- Sync error paths ----------------------------------------------------------

func TestSyncGetManifestObjectsError(t *testing.T) {
	state, err := NewStateDriver(nil, "test-operator", driverTestScheme(t), manifestDir)
	require.NoError(t, err)
	sd := state.(*stateDriver)

	// Empty catalog -> getManifestObjects fails.
	syncState, err := sd.Sync(context.Background(), newDriverCR("driver-a"), NewInfoCatalog())
	require.ErrorContains(t, err, "failed to create k8s objects from manifests")
	assert.Equal(t, SyncState(SyncStateNotReady), syncState)
}

func TestSyncCleanupError(t *testing.T) {
	sch := driverTestScheme(t)
	cl := fake.NewClientBuilder().WithScheme(sch).
		WithObjects(newGPUNode("gpu-node", "driver-a")).
		WithIndex(&appsv1.DaemonSet{}, consts.NVIDIADriverControllerIndexKey, func(_ client.Object) []string { return nil }).
		WithInterceptorFuncs(interceptor.Funcs{
			List: func(ctx context.Context, cl client.WithWatch, list client.ObjectList, opts ...client.ListOption) error {
				if _, ok := list.(*appsv1.DaemonSetList); ok {
					return fmt.Errorf("injected daemonset list error")
				}
				return cl.List(ctx, list, opts...)
			},
		}).Build()
	state, err := NewStateDriver(cl, "test-operator", sch, manifestDir)
	require.NoError(t, err)
	sd := state.(*stateDriver)

	syncState, err := sd.Sync(context.Background(), newDriverCR("driver-a"), fullCatalog())
	require.ErrorContains(t, err, "failed to cleanup stale driver DaemonSets")
	assert.Equal(t, SyncState(SyncStateNotReady), syncState)
}

func TestSyncCreateOrUpdateError(t *testing.T) {
	// Scheme without NVIDIADriver registered -> SetControllerReference fails inside
	// createOrUpdateObjs.
	sch := coreAppsScheme(t)
	cl := fake.NewClientBuilder().WithScheme(sch).
		WithObjects(newGPUNode("gpu-node", "driver-a")).
		WithIndex(&appsv1.DaemonSet{}, consts.NVIDIADriverControllerIndexKey, func(_ client.Object) []string { return nil }).
		Build()
	state, err := NewStateDriver(cl, "test-operator", sch, manifestDir)
	require.NoError(t, err)
	sd := state.(*stateDriver)

	syncState, err := sd.Sync(context.Background(), newDriverCR("driver-a"), fullCatalog())
	require.ErrorContains(t, err, "failed to create/update objects")
	assert.Equal(t, SyncState(SyncStateNotReady), syncState)
}

func TestSyncGetSyncStateError(t *testing.T) {
	sch := driverTestScheme(t)
	cl := fake.NewClientBuilder().WithScheme(sch).
		WithObjects(newGPUNode("gpu-node", "driver-a")).
		WithIndex(&appsv1.DaemonSet{}, consts.NVIDIADriverControllerIndexKey, func(_ client.Object) []string { return nil }).
		WithInterceptorFuncs(interceptor.Funcs{
			Get: func(_ context.Context, _ client.WithWatch, _ client.ObjectKey, obj client.Object, _ ...client.GetOption) error {
				// Fail only the readiness Gets (unstructured) performed by getSyncState.
				if _, ok := obj.(*unstructured.Unstructured); ok {
					return fmt.Errorf("injected get error")
				}
				return nil
			},
		}).Build()
	state, err := NewStateDriver(cl, "test-operator", sch, manifestDir)
	require.NoError(t, err)
	sd := state.(*stateDriver)

	syncState, err := sd.Sync(context.Background(), newDriverCR("driver-a"), fullCatalog())
	require.ErrorContains(t, err, "failed to get sync state")
	assert.Equal(t, SyncState(SyncStateNotReady), syncState)
}

// --- cleanupStaleDriverDaemonsets delete/list error paths ----------------------

func TestCleanupStaleDeleteErrors(t *testing.T) {
	sch := driverTestScheme(t)

	t.Run("stale daemonset delete error", func(t *testing.T) {
		dsStale := makeDaemonSet("ds-stale", "driver-a", 0, 0, nil)
		cl := fake.NewClientBuilder().WithScheme(sch).WithObjects(dsStale).
			WithIndex(&appsv1.DaemonSet{}, consts.NVIDIADriverControllerIndexKey, func(o client.Object) []string {
				return []string{o.GetLabels()["owner"]}
			}).
			WithInterceptorFuncs(interceptor.Funcs{
				Delete: func(_ context.Context, _ client.WithWatch, _ client.Object, _ ...client.DeleteOption) error {
					return fmt.Errorf("injected delete error")
				},
			}).Build()
		state, _ := NewStateDriver(cl, "test-operator", sch, manifestDir)
		sd := state.(*stateDriver)
		cr := &nvidiav1alpha1.NVIDIADriver{ObjectMeta: metav1.ObjectMeta{Name: "driver-a"}}
		// desiredObjs empty -> dsStale is not desired -> deleted -> delete error.
		err := sd.cleanupStaleDriverDaemonsets(context.Background(), cr, nil)
		require.ErrorContains(t, err, "error deleting DaemonSet")
	})

	t.Run("node list error", func(t *testing.T) {
		dsInactive := makeDaemonSet("ds-inactive", "driver-a", 0, 0, map[string]string{"pool": "gold"})
		cl := fake.NewClientBuilder().WithScheme(sch).WithObjects(dsInactive).
			WithIndex(&appsv1.DaemonSet{}, consts.NVIDIADriverControllerIndexKey, func(o client.Object) []string {
				return []string{o.GetLabels()["owner"]}
			}).
			WithInterceptorFuncs(interceptor.Funcs{
				List: func(ctx context.Context, cl client.WithWatch, list client.ObjectList, opts ...client.ListOption) error {
					if _, ok := list.(*corev1.NodeList); ok {
						return fmt.Errorf("injected node list error")
					}
					return cl.List(ctx, list, opts...)
				},
			}).Build()
		state, _ := NewStateDriver(cl, "test-operator", sch, manifestDir)
		sd := state.(*stateDriver)
		cr := &nvidiav1alpha1.NVIDIADriver{ObjectMeta: metav1.ObjectMeta{Name: "driver-a"}}
		desired := []*unstructured.Unstructured{newDaemonSetUnstructured("ds-inactive", "test-operator")}
		err := sd.cleanupStaleDriverDaemonsets(context.Background(), cr, desired)
		require.ErrorContains(t, err, "failed to list nodes")
	})

	t.Run("inactive daemonset delete error", func(t *testing.T) {
		dsInactive := makeDaemonSet("ds-inactive", "driver-a", 0, 0, map[string]string{"pool": "silver"})
		cl := fake.NewClientBuilder().WithScheme(sch).WithObjects(dsInactive).
			WithIndex(&appsv1.DaemonSet{}, consts.NVIDIADriverControllerIndexKey, func(o client.Object) []string {
				return []string{o.GetLabels()["owner"]}
			}).
			WithInterceptorFuncs(interceptor.Funcs{
				Delete: func(_ context.Context, _ client.WithWatch, _ client.Object, _ ...client.DeleteOption) error {
					return fmt.Errorf("injected delete error")
				},
			}).Build()
		state, _ := NewStateDriver(cl, "test-operator", sch, manifestDir)
		sd := state.(*stateDriver)
		cr := &nvidiav1alpha1.NVIDIADriver{ObjectMeta: metav1.ObjectMeta{Name: "driver-a"}}
		desired := []*unstructured.Unstructured{newDaemonSetUnstructured("ds-inactive", "test-operator")}
		err := sd.cleanupStaleDriverDaemonsets(context.Background(), cr, desired)
		require.ErrorContains(t, err, "error deleting DaemonSet")
	})
}

// --- handleDefaultImagesInObjects additional branches --------------------------

func TestHandleDefaultImagesNoDaemonSet(t *testing.T) {
	sch := driverTestScheme(t)
	cl := driverIndexBuilder(sch)
	state, _ := NewStateDriver(cl, "test-operator", sch, manifestDir)
	sd := state.(*stateDriver)

	cr := newDriverCR("driver-a")
	cr.Spec.Manager.Image = ""
	renderData := getMinimalDriverRenderData()

	// objs without any DaemonSet -> getDaemonsetFromObjects fails.
	objs := []*unstructured.Unstructured{newConfigMapUnstructured("cm", "test-operator")}
	_, err := sd.handleDefaultImagesInObjects(context.Background(), objs, cr, *renderData)
	require.ErrorContains(t, err, "error getting DaemonSet from unstructured objects")
}

func TestHandleDefaultImagesCurrentImageMatches(t *testing.T) {
	sch := driverTestScheme(t)

	state, _ := NewStateDriver(nil, "test-operator", sch, manifestDir)
	sd := state.(*stateDriver)
	renderData := getMinimalDriverRenderData()
	renderData.Runtime.Namespace = "test-operator"
	desiredObjs, err := sd.renderManifestObjects(context.Background(), renderData)
	require.NoError(t, err)
	desiredDs, err := getDaemonsetFromObjects(desiredObjs)
	require.NoError(t, err)

	// Current DaemonSet already runs the same k8s-driver-manager image.
	currentDs := &appsv1.DaemonSet{
		ObjectMeta: metav1.ObjectMeta{Name: desiredDs.Name, Namespace: desiredDs.Namespace},
		Spec: appsv1.DaemonSetSpec{
			Template: corev1.PodTemplateSpec{
				Spec: corev1.PodSpec{
					InitContainers: []corev1.Container{
						{Name: "k8s-driver-manager", Image: renderData.Driver.ManagerImagePath},
					},
				},
			},
		},
	}
	cl := fake.NewClientBuilder().WithScheme(sch).WithObjects(currentDs).Build()
	sd.client = cl

	cr := newDriverCR("driver-a")
	cr.Spec.Manager.Image = ""

	got, err := sd.handleDefaultImagesInObjects(context.Background(), desiredObjs, cr, *renderData)
	require.NoError(t, err)
	assert.Equal(t, desiredObjs, got)
}

func TestHandleDefaultImagesCurrentGetError(t *testing.T) {
	sch := driverTestScheme(t)
	cl := fake.NewClientBuilder().WithScheme(sch).
		WithInterceptorFuncs(interceptor.Funcs{
			Get: func(_ context.Context, _ client.WithWatch, _ client.ObjectKey, _ client.Object, _ ...client.GetOption) error {
				return fmt.Errorf("injected get error")
			},
		}).Build()
	state, _ := NewStateDriver(cl, "test-operator", sch, manifestDir)
	sd := state.(*stateDriver)

	cr := newDriverCR("driver-a")
	cr.Spec.Manager.Image = ""
	renderData := getMinimalDriverRenderData()
	objs := []*unstructured.Unstructured{newDaemonSetUnstructured("ds-a", "test-operator")}

	_, err := sd.handleDefaultImagesInObjects(context.Background(), objs, cr, *renderData)
	require.ErrorContains(t, err, "failed to get current driver DaemonSet")
}

func TestHandleDefaultImagesReRenderError(t *testing.T) {
	sch := driverTestScheme(t)
	state, _ := NewStateDriver(nil, "test-operator", sch, manifestDir)
	sd := state.(*stateDriver)

	// Seed a current DaemonSet whose manager image differs from the render data's.
	dsName := "nvidia-gpu-driver-ubuntu22.04"
	currentDs := &appsv1.DaemonSet{
		ObjectMeta: metav1.ObjectMeta{Name: dsName, Namespace: "test-operator"},
		Spec: appsv1.DaemonSetSpec{
			Template: corev1.PodTemplateSpec{
				Spec: corev1.PodSpec{
					InitContainers: []corev1.Container{{Name: "k8s-driver-manager", Image: "old-manager:1.0"}},
				},
			},
		},
	}
	cl := fake.NewClientBuilder().WithScheme(sch).WithObjects(currentDs).Build()
	sd.client = cl

	cr := newDriverCR("driver-a")
	cr.Spec.Manager.Image = ""

	// desiredObjs contains a valid DaemonSet (name/namespace match the seeded one),
	// but the render data passed for re-render has a nil Driver.Spec, so the second
	// render fails.
	desiredObjs := []*unstructured.Unstructured{newDaemonSetUnstructured(dsName, "test-operator")}
	renderData := &driverRenderData{
		Driver:  &driverSpec{ManagerImagePath: "new-manager:2.0", Spec: nil},
		Runtime: &driverRuntimeSpec{Namespace: "test-operator"},
	}

	_, err := sd.handleDefaultImagesInObjects(context.Background(), desiredObjs, cr, *renderData)
	require.ErrorContains(t, err, "failed to render kubernetes manifests")
}

func TestHandleDefaultImagesReRenderSetRefError(t *testing.T) {
	// Scheme without NVIDIADriver -> SetControllerReference on re-rendered DaemonSet fails.
	sch := coreAppsScheme(t)
	renderState, _ := NewStateDriver(nil, "test-operator", driverTestScheme(t), manifestDir)
	renderSd := renderState.(*stateDriver)
	renderData := getMinimalDriverRenderData()
	renderData.Runtime.Namespace = "test-operator"
	desiredObjs, err := renderSd.renderManifestObjects(context.Background(), renderData)
	require.NoError(t, err)
	desiredDs, err := getDaemonsetFromObjects(desiredObjs)
	require.NoError(t, err)

	currentDs := &appsv1.DaemonSet{
		ObjectMeta: metav1.ObjectMeta{Name: desiredDs.Name, Namespace: desiredDs.Namespace},
		Spec: appsv1.DaemonSetSpec{
			Template: corev1.PodTemplateSpec{
				Spec: corev1.PodSpec{
					InitContainers: []corev1.Container{{Name: "k8s-driver-manager", Image: "old-manager:1.0"}},
				},
			},
		},
	}
	cl := fake.NewClientBuilder().WithScheme(sch).WithObjects(currentDs).Build()

	state, _ := NewStateDriver(cl, "test-operator", sch, manifestDir)
	sd := state.(*stateDriver)

	cr := newDriverCR("driver-a")
	cr.Spec.Manager.Image = ""

	_, err = sd.handleDefaultImagesInObjects(context.Background(), desiredObjs, cr, *renderData)
	require.ErrorContains(t, err, "failed to set controller reference")
}

func TestHandleDefaultImagesUnchangedSpecKeepsCurrentImage(t *testing.T) {
	sch := driverTestScheme(t)
	cr := newDriverCR("driver-a")
	const currentImage = "custom-manager:1.0"

	// Replicate the production hashing steps to derive the hash the current
	// DaemonSet must carry so that newHash == currentHash.
	hashState, _ := NewStateDriver(nil, "test-operator", sch, manifestDir)
	hashSd := hashState.(*stateDriver)
	hashData := getMinimalDriverRenderData()
	hashData.Runtime.Namespace = "test-operator"
	hashData.Driver.ManagerImagePath = currentImage
	hashObjs, err := hashSd.renderManifestObjects(context.Background(), hashData)
	require.NoError(t, err)
	hashObj, err := getObjectOfKind(hashObjs, "DaemonSet")
	require.NoError(t, err)
	require.NoError(t, controllerutil.SetControllerReference(cr, hashObj, sch))
	hashSd.addStateSpecificLabels(hashObj)
	expectedHash := utils.GetObjectHash(hashObj)

	// desiredObjs is rendered with the default manager image path (differs from currentImage).
	renderState, _ := NewStateDriver(nil, "test-operator", sch, manifestDir)
	renderSd := renderState.(*stateDriver)
	renderData := getMinimalDriverRenderData()
	renderData.Runtime.Namespace = "test-operator"
	desiredObjs, err := renderSd.renderManifestObjects(context.Background(), renderData)
	require.NoError(t, err)
	desiredDs, err := getDaemonsetFromObjects(desiredObjs)
	require.NoError(t, err)

	currentDs := &appsv1.DaemonSet{
		ObjectMeta: metav1.ObjectMeta{
			Name:        desiredDs.Name,
			Namespace:   desiredDs.Namespace,
			Annotations: map[string]string{consts.NvidiaAnnotationHashKey: expectedHash},
		},
		Spec: appsv1.DaemonSetSpec{
			Template: corev1.PodTemplateSpec{
				Spec: corev1.PodSpec{
					InitContainers: []corev1.Container{{Name: "k8s-driver-manager", Image: currentImage}},
				},
			},
		},
	}
	cl := fake.NewClientBuilder().WithScheme(sch).WithObjects(currentDs).Build()

	state, _ := NewStateDriver(cl, "test-operator", sch, manifestDir)
	sd := state.(*stateDriver)

	cr.Spec.Manager.Image = ""
	got, err := sd.handleDefaultImagesInObjects(context.Background(), desiredObjs, cr, *renderData)
	require.NoError(t, err)
	// The returned objects use the current (unchanged) manager image.
	gotDs, err := getDaemonsetFromObjects(got)
	require.NoError(t, err)
	var managerImage string
	for _, c := range gotDs.Spec.Template.Spec.InitContainers {
		if c.Name == "k8s-driver-manager" {
			managerImage = c.Image
		}
	}
	assert.Equal(t, currentImage, managerImage)
}

// --- buildDriverInstallConfig full field coverage ------------------------------

func TestBuildDriverInstallConfigAllFields(t *testing.T) {
	data := &driverRenderData{
		Driver: &driverSpec{
			ImagePath:        "nvcr.io/nvidia/driver:535-ubuntu22.04",
			ManagerImagePath: "nvcr.io/nvidia/cloud-native/k8s-driver-manager:v0.6.2",
			Spec: &nvidiav1alpha1.NVIDIADriverSpec{
				DriverType:            nvidiav1alpha1.GPU,
				KernelModuleType:      "open",
				Args:                  []string{"--foo"},
				SecretEnv:             "secret-env",
				Env:                   []nvidiav1alpha1.EnvVar{{Name: "A", Value: "1"}},
				Manager:               nvidiav1alpha1.DriverManagerSpec{Env: []nvidiav1alpha1.EnvVar{{Name: "B", Value: "2"}}},
				LicensingConfig:       &nvidiav1alpha1.DriverLicensingConfigSpec{SecretName: "lic-secret"},
				VirtualTopologyConfig: &nvidiav1alpha1.VirtualTopologyConfigSpec{Name: "topo"},
				KernelModuleConfig:    &nvidiav1alpha1.KernelModuleConfigSpec{Name: "kmod"},
				RepoConfig:            &nvidiav1alpha1.DriverRepoConfigSpec{Name: "repo"},
				CertConfig:            &nvidiav1alpha1.DriverCertConfigSpec{Name: "cert"},
			},
		},
		GPUDirectRDMA: &nvidiav1alpha1.GPUDirectRDMASpec{
			Enabled:      ptr.To(true),
			UseHostMOFED: ptr.To(true),
		},
		GDS: &gdsDriverSpec{
			ImagePath: "nvcr.io/nvidia/cloud-native/nvidia-fs:2.16.1",
			Spec:      &nvidiav1alpha1.GPUDirectStorageSpec{Enabled: ptr.To(true), Env: []nvidiav1alpha1.EnvVar{{Name: "G", Value: "1"}}},
		},
		GDRCopy: &gdrcopyDriverSpec{
			ImagePath: "nvcr.io/nvidia/cloud-native/gdrdrv:v2.4.1",
			Spec:      &nvidiav1alpha1.GDRCopySpec{Enabled: ptr.To(true), Env: []nvidiav1alpha1.EnvVar{{Name: "H", Value: "1"}}},
		},
		Runtime: &driverRuntimeSpec{
			Namespace:                     "test-operator",
			OpenshiftVersion:              "4.13",
			OpenshiftDriverToolkitEnabled: true,
			OpenshiftProxySpec: &configv1.ProxySpec{
				HTTPProxy:  "http://proxy:8080",
				HTTPSProxy: "https://proxy:8443",
				NoProxy:    "localhost",
				TrustedCA:  configv1.ConfigMapNameReference{Name: "trusted-ca"},
			},
		},
		Openshift: &openshiftSpec{
			ToolkitImage: "quay.io/toolkit:latest",
			RHCOSVersion: "413.92",
		},
		Precompiled: &precompiledSpec{
			KernelVersion: "5.15.0-70-generic",
		},
		AdditionalConfigs: &additionalConfigs{
			VolumeMounts: []corev1.VolumeMount{{Name: "vm", MountPath: "/x"}},
			Volumes:      []corev1.Volume{{Name: "vm"}},
		},
		HostRoot: "/host",
	}

	config := buildDriverInstallConfig(data)
	require.NotNil(t, config)

	// Compare the entire mapped install config in one shot so every field
	// buildDriverInstallConfig populates is covered by the assertion.
	want := driverconfig.DriverInstallState{
		DriverImage:            "nvcr.io/nvidia/driver:535-ubuntu22.04",
		DriverManagerImage:     "nvcr.io/nvidia/cloud-native/k8s-driver-manager:v0.6.2",
		PeermemImage:           "nvcr.io/nvidia/driver:535-ubuntu22.04",
		GDSImage:               "nvcr.io/nvidia/cloud-native/nvidia-fs:2.16.1",
		GDRCopyImage:           "nvcr.io/nvidia/cloud-native/gdrdrv:v2.4.1",
		DTKImage:               "quay.io/toolkit:latest",
		DriverType:             "gpu",
		KernelModuleType:       "open",
		DriverArgs:             []string{"--foo"},
		DriverEnv:              []driverconfig.EnvVar{{Name: "A", Value: "1"}},
		ManagerEnv:             []driverconfig.EnvVar{{Name: "B", Value: "2"}},
		GDSEnv:                 []driverconfig.EnvVar{{Name: "G", Value: "1"}},
		GDRCopyEnv:             []driverconfig.EnvVar{{Name: "H", Value: "1"}},
		SecretEnvSource:        "secret-env",
		GPUDirectRDMAEnabled:   true,
		UseHostMOFED:           true,
		GDSEnabled:             true,
		GDRCopyEnabled:         true,
		LicensingConfigName:    "lic-secret",
		VirtualTopologyConfig:  "topo",
		KernelModuleConfig:     "kmod",
		RepoConfig:             "repo",
		CertConfig:             "cert",
		UsePrecompiled:         true,
		KernelVersion:          "5.15.0-70-generic",
		OpenshiftVersion:       "4.13",
		DTKEnabled:             true,
		RHCOSVersion:           "413.92",
		HTTPProxy:              "http://proxy:8080",
		HTTPSProxy:             "https://proxy:8443",
		NoProxy:                "localhost",
		TrustedCAConfigMapName: "trusted-ca",
		AdditionalVolumes:      []driverconfig.VolumeConfig{{Name: "vm"}},
		AdditionalVolumeMounts: []driverconfig.VolumeMountConfig{{Name: "vm", MountPath: "/x"}},
		HostRoot:               "/host",
	}

	diff := cmp.Diff(want, *config, cmpopts.EquateEmpty())
	assert.Empty(t, diff, "unexpected driver install config (-want +got):\n%s", diff)
}

// --- GetWatchSources (driver.go) -----------------------------------------------

// fakeManager implements just enough of ctrl.Manager for GetWatchSources.
type fakeManager struct {
	ctrl.Manager
	cache  cache.Cache
	scheme *runtime.Scheme
	mapper meta.RESTMapper
}

func (f *fakeManager) GetCache() cache.Cache          { return f.cache }
func (f *fakeManager) GetScheme() *runtime.Scheme     { return f.scheme }
func (f *fakeManager) GetRESTMapper() meta.RESTMapper { return f.mapper }

// --- getNodePools list error ---------------------------------------------------

func TestGetNodePoolsListError(t *testing.T) {
	sch := driverTestScheme(t)
	cl := fake.NewClientBuilder().WithScheme(sch).
		WithInterceptorFuncs(interceptor.Funcs{
			List: func(_ context.Context, _ client.WithWatch, _ client.ObjectList, _ ...client.ListOption) error {
				return fmt.Errorf("injected node list error")
			},
		}).Build()

	cr := &nvidiav1alpha1.NVIDIADriver{ObjectMeta: metav1.ObjectMeta{Name: "driver-a"}}
	_, err := getNodePools(context.Background(), cl, cr, false)
	require.ErrorContains(t, err, "injected node list error")
}

func TestDriverGetWatchSources(t *testing.T) {
	sch := driverTestScheme(t)

	mapper := meta.NewDefaultRESTMapper([]schema.GroupVersion{
		{Group: "nvidia.com", Version: "v1alpha1"},
	})
	mapper.Add(schema.GroupVersionKind{Group: "nvidia.com", Version: "v1alpha1", Kind: "NVIDIADriver"}, meta.RESTScopeRoot)

	state, err := NewStateDriver(nil, "test-operator", sch, manifestDir)
	require.NoError(t, err)
	sd := state.(*stateDriver)

	mgr := &fakeManager{scheme: sch, mapper: mapper}
	sources := sd.GetWatchSources(mgr)
	require.Contains(t, sources, "DaemonSet")
	assert.NotNil(t, sources["DaemonSet"])
}
