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

	configv1 "github.com/openshift/api/config/v1"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	rbacv1 "k8s.io/api/rbac/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	apitypes "k8s.io/apimachinery/pkg/types"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	nvidiav1alpha1 "github.com/NVIDIA/gpu-operator/api/nvidia/v1alpha1"
	"github.com/NVIDIA/gpu-operator/internal/consts"
)

// fakeClusterInfo is a configurable clusterinfo.Interface implementation.
type fakeClusterInfo struct {
	runtime             string
	runtimeErr          error
	openshiftVersion    string
	openshiftVersionErr error
	dtkImages           map[string]string
	proxySpec           *configv1.ProxySpec
	proxyErr            error
}

func (f fakeClusterInfo) GetContainerRuntime() (string, error) {
	return f.runtime, f.runtimeErr
}

func (f fakeClusterInfo) GetOpenshiftVersion() (string, error) {
	return f.openshiftVersion, f.openshiftVersionErr
}

func (f fakeClusterInfo) GetOpenshiftDriverToolkitImages() map[string]string {
	return f.dtkImages
}

func (f fakeClusterInfo) GetOpenshiftProxySpec() (*configv1.ProxySpec, error) {
	return f.proxySpec, f.proxyErr
}

func (f fakeClusterInfo) GetDRAResourceGVR() (schema.GroupVersionResource, bool, error) {
	return schema.GroupVersionResource{}, false, nil
}

func driverTestScheme(t *testing.T) *runtime.Scheme {
	t.Helper()
	s := runtime.NewScheme()
	require.NoError(t, corev1.AddToScheme(s))
	require.NoError(t, appsv1.AddToScheme(s))
	require.NoError(t, rbacv1.AddToScheme(s))
	require.NoError(t, nvidiav1alpha1.AddToScheme(s))
	return s
}

// newTestStateDriver builds a *stateDriver, failing the test if construction or
// the type assertion fails instead of panicking on a discarded error.
func newTestStateDriver(t *testing.T, cl client.Client, sch *runtime.Scheme) *stateDriver {
	t.Helper()
	state, err := NewStateDriver(cl, "test-operator", sch, manifestDir)
	require.NoError(t, err)
	sd, ok := state.(*stateDriver)
	require.True(t, ok)
	return sd
}

func TestGetGDSSpec(t *testing.T) {
	pool := nodePool{osTag: "ubuntu22.04"}
	enabledSpec := func(image string) *nvidiav1alpha1.NVIDIADriverSpec {
		return &nvidiav1alpha1.NVIDIADriverSpec{
			GPUDirectStorage: &nvidiav1alpha1.GPUDirectStorageSpec{
				Enabled: ptr.To(true), Repository: "nvcr.io/nvidia/cloud-native", Image: image, Version: "2.16.1",
			},
		}
	}
	testCases := []struct {
		name      string
		spec      *nvidiav1alpha1.NVIDIADriverSpec
		wantNil   bool
		wantErr   bool
		wantImage string
	}{
		{name: "nil spec", spec: nil, wantNil: true},
		{name: "disabled", spec: &nvidiav1alpha1.NVIDIADriverSpec{}, wantNil: true},
		{name: "enabled resolves image path", spec: enabledSpec("nvidia-fs"), wantImage: "nvcr.io/nvidia/cloud-native/nvidia-fs:2.16.1-ubuntu22.04"},
		{name: "enabled with invalid image errors", spec: enabledSpec("INVALID IMAGE"), wantErr: true},
	}
	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			gds, err := getGDSSpec(tc.spec, pool)
			if tc.wantErr {
				require.Error(t, err)
				return
			}
			require.NoError(t, err)
			if tc.wantNil {
				assert.Nil(t, gds)
				return
			}
			require.NotNil(t, gds)
			assert.Equal(t, tc.wantImage, gds.ImagePath)
		})
	}
}

func TestGetGDRCopySpec(t *testing.T) {
	pool := nodePool{osTag: "ubuntu22.04"}
	enabledSpec := func(image string) *nvidiav1alpha1.NVIDIADriverSpec {
		return &nvidiav1alpha1.NVIDIADriverSpec{
			GDRCopy: &nvidiav1alpha1.GDRCopySpec{
				Enabled: ptr.To(true), Repository: "nvcr.io/nvidia/cloud-native", Image: image, Version: "v2.4.1",
			},
		}
	}
	testCases := []struct {
		name      string
		spec      *nvidiav1alpha1.NVIDIADriverSpec
		wantNil   bool
		wantErr   bool
		wantImage string
	}{
		{name: "nil spec", spec: nil, wantNil: true},
		{name: "disabled", spec: &nvidiav1alpha1.NVIDIADriverSpec{}, wantNil: true},
		{name: "enabled resolves image path", spec: enabledSpec("gdrdrv"), wantImage: "nvcr.io/nvidia/cloud-native/gdrdrv:v2.4.1-ubuntu22.04"},
		{name: "enabled with invalid image errors", spec: enabledSpec("INVALID IMAGE"), wantErr: true},
	}
	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			gdr, err := getGDRCopySpec(tc.spec, pool)
			if tc.wantErr {
				require.Error(t, err)
				return
			}
			require.NoError(t, err)
			if tc.wantNil {
				assert.Nil(t, gdr)
				return
			}
			require.NotNil(t, gdr)
			assert.Equal(t, tc.wantImage, gdr.ImagePath)
		})
	}
}

func TestGetRuntimeSpec(t *testing.T) {
	spec := &nvidiav1alpha1.NVIDIADriverSpec{}

	t.Run("non-openshift", func(t *testing.T) {
		info := fakeClusterInfo{openshiftVersion: ""}
		rs, err := getRuntimeSpec("test-ns", info, spec)
		require.NoError(t, err)
		assert.Equal(t, "test-ns", rs.Namespace)
		assert.Empty(t, rs.OpenshiftVersion)
		assert.False(t, rs.OpenshiftDriverToolkitEnabled)
	})

	t.Run("openshift version error", func(t *testing.T) {
		info := fakeClusterInfo{openshiftVersionErr: fmt.Errorf("boom")}
		_, err := getRuntimeSpec("test-ns", info, spec)
		require.ErrorContains(t, err, "failed to get openshift version")
	})

	t.Run("openshift with DTK enabled", func(t *testing.T) {
		info := fakeClusterInfo{
			openshiftVersion: "4.13",
			dtkImages:        map[string]string{"413.92": "some-image"},
			proxySpec:        &configv1.ProxySpec{HTTPProxy: "http://proxy:8080"},
		}
		rs, err := getRuntimeSpec("test-ns", info, spec)
		require.NoError(t, err)
		assert.Equal(t, "4.13", rs.OpenshiftVersion)
		assert.True(t, rs.OpenshiftDriverToolkitEnabled)
		require.NotNil(t, rs.OpenshiftProxySpec)
		assert.Equal(t, "http://proxy:8080", rs.OpenshiftProxySpec.HTTPProxy)
	})

	t.Run("openshift proxy error", func(t *testing.T) {
		info := fakeClusterInfo{
			openshiftVersion: "4.13",
			proxyErr:         fmt.Errorf("proxy boom"),
		}
		_, err := getRuntimeSpec("test-ns", info, spec)
		require.ErrorContains(t, err, "failed to retrieve proxy settings")
	})

	t.Run("openshift with precompiled skips DTK", func(t *testing.T) {
		precompiledSpec := &nvidiav1alpha1.NVIDIADriverSpec{UsePrecompiled: ptr.To(true)}
		info := fakeClusterInfo{
			openshiftVersion: "4.13",
			dtkImages:        map[string]string{"413.92": "some-image"},
		}
		rs, err := getRuntimeSpec("test-ns", info, precompiledSpec)
		require.NoError(t, err)
		assert.Equal(t, "4.13", rs.OpenshiftVersion)
		assert.False(t, rs.OpenshiftDriverToolkitEnabled)
	})
}

func TestRenderManifestObjects(t *testing.T) {
	state, err := NewStateDriver(nil, "", nil, manifestDir)
	require.NoError(t, err)
	sd := state.(*stateDriver)

	objs, err := sd.renderManifestObjects(context.Background(), getMinimalDriverRenderData())
	require.NoError(t, err)
	require.NotEmpty(t, objs)
}

func newGPUNode(name, owner string) *corev1.Node {
	return newGPUNodeOS(name, owner, "ubuntu", "22.04")
}

func newDriverCR(name string) *nvidiav1alpha1.NVIDIADriver {
	return &nvidiav1alpha1.NVIDIADriver{
		ObjectMeta: metav1.ObjectMeta{
			Name: name,
			UID:  apitypes.UID("test-uid-" + name),
		},
		Spec: nvidiav1alpha1.NVIDIADriverSpec{
			DriverType: nvidiav1alpha1.GPU,
			Repository: "nvcr.io/nvidia",
			Image:      "driver",
			Version:    "535.104.05",
			Manager: nvidiav1alpha1.DriverManagerSpec{
				Repository: "nvcr.io/nvidia/cloud-native",
				Image:      "k8s-driver-manager",
				Version:    "v0.6.2",
			},
		},
	}
}

func driverIndexBuilder(sch *runtime.Scheme, objs ...client.Object) client.Client {
	return fake.NewClientBuilder().
		WithScheme(sch).
		WithObjects(objs...).
		WithIndex(&appsv1.DaemonSet{}, consts.NVIDIADriverControllerIndexKey, func(_ client.Object) []string {
			return nil
		}).
		Build()
}

func TestGetManifestObjectsMissingCatalogEntries(t *testing.T) {
	sch := driverTestScheme(t)
	cl := driverIndexBuilder(sch)
	sd := newTestStateDriver(t, cl, sch)
	cr := newDriverCR("driver-a")

	// Missing host root.
	_, err := sd.getManifestObjects(context.Background(), cr, NewInfoCatalog())
	require.ErrorContains(t, err, "failed to get host root from info catalog")

	// Host root present, but ClusterInfo missing.
	catalog := NewInfoCatalog()
	catalog.Add(InfoTypeHostRoot, "/host")
	_, err = sd.getManifestObjects(context.Background(), cr, catalog)
	require.ErrorContains(t, err, "failed to get cluster info")
}

func TestGetManifestObjectsNoNodes(t *testing.T) {
	sch := driverTestScheme(t)
	cl := driverIndexBuilder(sch)
	sd := newTestStateDriver(t, cl, sch)
	cr := newDriverCR("driver-a")

	catalog := NewInfoCatalog()
	catalog.Add(InfoTypeHostRoot, "/host")
	catalog.Add(InfoTypeClusterInfo, fakeClusterInfo{})

	objs, err := sd.getManifestObjects(context.Background(), cr, catalog)
	require.NoError(t, err)
	assert.Empty(t, objs)
}

func TestGetManifestObjectsWithNode(t *testing.T) {
	sch := driverTestScheme(t)
	cl := driverIndexBuilder(sch, newGPUNode("gpu-node", "driver-a"))
	sd := newTestStateDriver(t, cl, sch)
	cr := newDriverCR("driver-a")

	catalog := NewInfoCatalog()
	catalog.Add(InfoTypeHostRoot, "/host")
	catalog.Add(InfoTypeClusterInfo, fakeClusterInfo{})

	objs, err := sd.getManifestObjects(context.Background(), cr, catalog)
	require.NoError(t, err)
	require.NotEmpty(t, objs)

	// A DaemonSet should be among the rendered objects.
	_, err = getObjectOfKind(objs, "DaemonSet")
	require.NoError(t, err)
}

func TestSyncWrongCRType(t *testing.T) {
	state, err := NewStateDriver(nil, "", nil, manifestDir)
	require.NoError(t, err)
	sd := state.(*stateDriver)

	syncState, err := sd.Sync(context.Background(), "not-a-cr", NewInfoCatalog())
	require.Error(t, err)
	assert.Equal(t, SyncState(SyncStateError), syncState)
}

func TestSyncNoNodesReady(t *testing.T) {
	sch := driverTestScheme(t)
	cl := driverIndexBuilder(sch)
	sd := newTestStateDriver(t, cl, sch)
	cr := newDriverCR("driver-a")

	catalog := NewInfoCatalog()
	catalog.Add(InfoTypeHostRoot, "/host")
	catalog.Add(InfoTypeClusterInfo, fakeClusterInfo{})

	// No nodes -> no objects -> sync reports ready.
	syncState, err := sd.Sync(context.Background(), cr, catalog)
	require.NoError(t, err)
	assert.Equal(t, SyncState(SyncStateReady), syncState)
}

func TestSyncCreatesObjects(t *testing.T) {
	sch := driverTestScheme(t)
	cl := driverIndexBuilder(sch, newGPUNode("gpu-node", "driver-a"))
	sd := newTestStateDriver(t, cl, sch)
	cr := newDriverCR("driver-a")

	catalog := NewInfoCatalog()
	catalog.Add(InfoTypeHostRoot, "/host")
	catalog.Add(InfoTypeClusterInfo, fakeClusterInfo{})

	// Sync creates the driver objects without error. Readiness is intentionally
	// not asserted here: the fake client runs no DaemonSet controller, so the
	// reported state would only reflect a zero-valued status. Readiness is
	// covered directly with explicit statuses in TestIsDaemonSetReady.
	_, err := sd.Sync(context.Background(), cr, catalog)
	require.NoError(t, err)

	// Verify the DaemonSet was created and carries the render + apply metadata:
	// correct name/namespace, controller owner reference, state label, and hash.
	dsList := &appsv1.DaemonSetList{}
	require.NoError(t, cl.List(context.Background(), dsList))
	require.Len(t, dsList.Items, 1)
	ds := dsList.Items[0]

	assert.True(t, strings.HasPrefix(ds.Name, "nvidia-gpu-driver-ubuntu22.04-"), "unexpected DaemonSet name %q", ds.Name)
	assert.Equal(t, "test-operator", ds.Namespace)
	assert.Equal(t, "state-driver", ds.Labels[consts.StateLabel])
	assert.NotEmpty(t, ds.Annotations[consts.NvidiaAnnotationHashKey])

	require.Len(t, ds.OwnerReferences, 1)
	owner := ds.OwnerReferences[0]
	assert.Equal(t, "NVIDIADriver", owner.Kind)
	assert.Equal(t, cr.Name, owner.Name)
	assert.Equal(t, cr.UID, owner.UID)
	require.NotNil(t, owner.Controller)
	assert.True(t, *owner.Controller)
}

func TestGetDriverName(t *testing.T) {
	testCases := []struct {
		name       string
		driverType nvidiav1alpha1.DriverType
		want       string
	}{
		{"GPU", nvidiav1alpha1.GPU, "nvidia-gpu-driver-my-driver-ubuntu22.04"},
		{"vGPU host manager", nvidiav1alpha1.VGPUHostManager, "nvidia-vgpu-manager-my-driver-ubuntu22.04"},
	}
	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			cr := &nvidiav1alpha1.NVIDIADriver{
				ObjectMeta: metav1.ObjectMeta{Name: "my-driver"},
				Spec:       nvidiav1alpha1.NVIDIADriverSpec{DriverType: tc.driverType},
			}
			assert.Equal(t, tc.want, getDriverName(cr, "ubuntu22.04"))
		})
	}
}

func TestGetDriverSpecErrors(t *testing.T) {
	// nil CR -> error.
	_, err := getDriverSpec(nil, nodePool{})
	require.ErrorContains(t, err, "no NVIDIADriver CR provided")

	// Invalid driver image reference -> error.
	badImageCR := &nvidiav1alpha1.NVIDIADriver{
		ObjectMeta: metav1.ObjectMeta{Name: "driver-a"},
		Spec: nvidiav1alpha1.NVIDIADriverSpec{
			DriverType: nvidiav1alpha1.GPU,
			Repository: "nvcr.io/nvidia",
			Image:      "INVALID IMAGE",
			Version:    "535.104.05",
		},
	}
	_, err = getDriverSpec(badImageCR, nodePool{osTag: "ubuntu22.04"})
	require.Error(t, err)
}

func TestHandleDefaultImagesInObjectsManagerImageSet(t *testing.T) {
	sch := driverTestScheme(t)
	cl := driverIndexBuilder(sch)
	sd := newTestStateDriver(t, cl, sch)

	// Manager.Image is set, so the default-image handling returns the objects unchanged.
	cr := newDriverCR("driver-a")
	renderData := getMinimalDriverRenderData()
	objs := []*unstructured.Unstructured{newDaemonSetUnstructured("ds-a", "test-operator")}

	got, err := sd.handleDefaultImagesInObjects(context.Background(), objs, cr, *renderData)
	require.NoError(t, err)
	assert.Equal(t, objs, got)
}

func TestHandleDefaultImagesInObjectsCurrentDaemonSetNotFound(t *testing.T) {
	sch := driverTestScheme(t)
	cl := driverIndexBuilder(sch)
	sd := newTestStateDriver(t, cl, sch)

	// Manager.Image empty -> env var path; current DaemonSet does not exist -> returns objs unchanged.
	cr := newDriverCR("driver-a")
	cr.Spec.Manager.Image = ""
	renderData := getMinimalDriverRenderData()

	daemonSet := newDaemonSetUnstructured("nvidia-gpu-driver-test", "test-operator")
	objs := []*unstructured.Unstructured{daemonSet}

	got, err := sd.handleDefaultImagesInObjects(context.Background(), objs, cr, *renderData)
	require.NoError(t, err)
	assert.Equal(t, objs, got)
}
