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
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	nvidiav1alpha1 "github.com/NVIDIA/gpu-operator/api/nvidia/v1alpha1"
	"github.com/NVIDIA/gpu-operator/controllers/clusterinfo"
	"github.com/NVIDIA/gpu-operator/internal/consts"
)

var _ clusterinfo.Interface = fakeClusterInfo{}

type fakeClusterInfo struct {
	containerRuntime    string
	containerRuntimeErr error
	openshiftVersion    string
	openshiftVersionErr error
	dtkImages           map[string]string
	proxySpec           *configv1.ProxySpec
	proxyErr            error
}

func (f fakeClusterInfo) GetContainerRuntime() (string, error) {
	return f.containerRuntime, f.containerRuntimeErr
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
	scheme := runtime.NewScheme()
	require.NoError(t, corev1.AddToScheme(scheme))
	require.NoError(t, appsv1.AddToScheme(scheme))
	require.NoError(t, rbacv1.AddToScheme(scheme))
	require.NoError(t, nvidiav1alpha1.AddToScheme(scheme))
	return scheme
}

func newTestStateDriver(t *testing.T, k8sClient client.Client, scheme *runtime.Scheme) *stateDriver {
	t.Helper()
	state, err := NewStateDriver(k8sClient, "test-operator", scheme, manifestDir)
	require.NoError(t, err)
	driverState, ok := state.(*stateDriver)
	require.True(t, ok)
	return driverState
}

func TestGetGDSSpec(t *testing.T) {
	ubuntu22NodePool := nodePool{osTag: "ubuntu22.04"}
	gdsEnabledSpec := func(image string) *nvidiav1alpha1.NVIDIADriverSpec {
		return &nvidiav1alpha1.NVIDIADriverSpec{
			GPUDirectStorage: &nvidiav1alpha1.GPUDirectStorageSpec{
				Enabled: new(true), Repository: "nvcr.io/nvidia/cloud-native", Image: image, Version: "2.16.1",
			},
		}
	}
	testCases := []struct {
		name              string
		nvidiaDriverSpec  *nvidiav1alpha1.NVIDIADriverSpec
		expectNilSpec     bool
		expectError       bool
		expectedImagePath string
	}{
		{name: "nil spec", nvidiaDriverSpec: nil, expectNilSpec: true},
		{name: "disabled", nvidiaDriverSpec: &nvidiav1alpha1.NVIDIADriverSpec{}, expectNilSpec: true},
		{name: "enabled resolves image path", nvidiaDriverSpec: gdsEnabledSpec("nvidia-fs"), expectedImagePath: "nvcr.io/nvidia/cloud-native/nvidia-fs:2.16.1-ubuntu22.04"},
		{name: "enabled with invalid image errors", nvidiaDriverSpec: gdsEnabledSpec("INVALID IMAGE"), expectError: true},
	}
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			gdsSpec, err := getGDSSpec(testCase.nvidiaDriverSpec, ubuntu22NodePool)
			if testCase.expectError {
				require.Error(t, err)
				return
			}
			require.NoError(t, err)
			if testCase.expectNilSpec {
				assert.Nil(t, gdsSpec)
				return
			}
			require.NotNil(t, gdsSpec)
			assert.Equal(t, testCase.expectedImagePath, gdsSpec.ImagePath)
		})
	}
}

func TestGetGDRCopySpec(t *testing.T) {
	ubuntu22NodePool := nodePool{osTag: "ubuntu22.04"}
	gdrCopyEnabledSpec := func(image string) *nvidiav1alpha1.NVIDIADriverSpec {
		return &nvidiav1alpha1.NVIDIADriverSpec{
			GDRCopy: &nvidiav1alpha1.GDRCopySpec{
				Enabled: new(true), Repository: "nvcr.io/nvidia/cloud-native", Image: image, Version: "v2.4.1",
			},
		}
	}
	testCases := []struct {
		name              string
		nvidiaDriverSpec  *nvidiav1alpha1.NVIDIADriverSpec
		expectNilSpec     bool
		expectError       bool
		expectedImagePath string
	}{
		{name: "nil spec", nvidiaDriverSpec: nil, expectNilSpec: true},
		{name: "disabled", nvidiaDriverSpec: &nvidiav1alpha1.NVIDIADriverSpec{}, expectNilSpec: true},
		{name: "enabled resolves image path", nvidiaDriverSpec: gdrCopyEnabledSpec("gdrdrv"), expectedImagePath: "nvcr.io/nvidia/cloud-native/gdrdrv:v2.4.1-ubuntu22.04"},
		{name: "enabled with invalid image errors", nvidiaDriverSpec: gdrCopyEnabledSpec("INVALID IMAGE"), expectError: true},
	}
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			gdrCopySpec, err := getGDRCopySpec(testCase.nvidiaDriverSpec, ubuntu22NodePool)
			if testCase.expectError {
				require.Error(t, err)
				return
			}
			require.NoError(t, err)
			if testCase.expectNilSpec {
				assert.Nil(t, gdrCopySpec)
				return
			}
			require.NotNil(t, gdrCopySpec)
			assert.Equal(t, testCase.expectedImagePath, gdrCopySpec.ImagePath)
		})
	}
}

func TestGetRuntimeSpec(t *testing.T) {
	nvidiaDriverSpec := &nvidiav1alpha1.NVIDIADriverSpec{}

	t.Run("non-openshift", func(t *testing.T) {
		clusterInfo := fakeClusterInfo{openshiftVersion: ""}
		runtimeSpec, err := getRuntimeSpec("test-ns", clusterInfo, nvidiaDriverSpec)
		require.NoError(t, err)
		assert.Equal(t, "test-ns", runtimeSpec.Namespace)
		assert.Empty(t, runtimeSpec.OpenshiftVersion)
		assert.False(t, runtimeSpec.OpenshiftDriverToolkitEnabled)
	})

	t.Run("openshift version error", func(t *testing.T) {
		clusterInfo := fakeClusterInfo{openshiftVersionErr: fmt.Errorf("boom")}
		_, err := getRuntimeSpec("test-ns", clusterInfo, nvidiaDriverSpec)
		require.ErrorContains(t, err, "failed to get openshift version")
	})

	t.Run("openshift with DTK enabled", func(t *testing.T) {
		clusterInfo := fakeClusterInfo{
			openshiftVersion: "4.13",
			dtkImages:        map[string]string{"413.92": "some-image"},
			proxySpec:        &configv1.ProxySpec{HTTPProxy: "http://proxy:8080"},
		}
		runtimeSpec, err := getRuntimeSpec("test-ns", clusterInfo, nvidiaDriverSpec)
		require.NoError(t, err)
		assert.Equal(t, "4.13", runtimeSpec.OpenshiftVersion)
		assert.True(t, runtimeSpec.OpenshiftDriverToolkitEnabled)
		require.NotNil(t, runtimeSpec.OpenshiftProxySpec)
		assert.Equal(t, "http://proxy:8080", runtimeSpec.OpenshiftProxySpec.HTTPProxy)
	})

	t.Run("openshift proxy error", func(t *testing.T) {
		clusterInfo := fakeClusterInfo{
			openshiftVersion: "4.13",
			proxyErr:         fmt.Errorf("proxy boom"),
		}
		_, err := getRuntimeSpec("test-ns", clusterInfo, nvidiaDriverSpec)
		require.ErrorContains(t, err, "failed to retrieve proxy settings")
	})

	t.Run("openshift with precompiled skips DTK", func(t *testing.T) {
		precompiledSpec := &nvidiav1alpha1.NVIDIADriverSpec{UsePrecompiled: new(true)}
		clusterInfo := fakeClusterInfo{
			openshiftVersion: "4.13",
			dtkImages:        map[string]string{"413.92": "some-image"},
		}
		runtimeSpec, err := getRuntimeSpec("test-ns", clusterInfo, precompiledSpec)
		require.NoError(t, err)
		assert.Equal(t, "4.13", runtimeSpec.OpenshiftVersion)
		assert.False(t, runtimeSpec.OpenshiftDriverToolkitEnabled)
	})
}

func TestRenderManifestObjects(t *testing.T) {
	driverState := newTestStateDriver(t, nil, driverTestScheme(t))

	objects, err := driverState.renderManifestObjects(context.Background(), getMinimalDriverRenderData())
	require.NoError(t, err)
	require.NotEmpty(t, objects)
}

func newGPUNode(name, ownerDriverName string) *corev1.Node {
	return newGPUNodeOS(name, ownerDriverName, "ubuntu", "22.04")
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

func newDriverFakeClient(scheme *runtime.Scheme, objects ...client.Object) client.Client {
	return fake.NewClientBuilder().
		WithScheme(scheme).
		WithObjects(objects...).
		WithIndex(&appsv1.DaemonSet{}, consts.NVIDIADriverControllerIndexKey, nvidiaDriverControllerIndex).
		Build()
}

func TestGetManifestObjectsMissingCatalogEntries(t *testing.T) {
	scheme := driverTestScheme(t)
	driverState := newTestStateDriver(t, newDriverFakeClient(scheme), scheme)
	driverCR := newDriverCR("driver-a")

	t.Run("host root missing", func(t *testing.T) {
		_, err := driverState.getManifestObjects(context.Background(), driverCR, NewInfoCatalog())
		require.ErrorContains(t, err, "failed to get host root from info catalog")
	})

	t.Run("cluster info missing", func(t *testing.T) {
		catalog := NewInfoCatalog()
		catalog.Add(InfoTypeHostRoot, "/host")
		_, err := driverState.getManifestObjects(context.Background(), driverCR, catalog)
		require.ErrorContains(t, err, "failed to get cluster info")
	})
}

func TestGetManifestObjectsNoNodes(t *testing.T) {
	scheme := driverTestScheme(t)
	fakeClient := newDriverFakeClient(scheme)
	driverState := newTestStateDriver(t, fakeClient, scheme)
	driverCR := newDriverCR("driver-a")

	catalog := NewInfoCatalog()
	catalog.Add(InfoTypeHostRoot, "/host")
	catalog.Add(InfoTypeClusterInfo, fakeClusterInfo{})

	objects, err := driverState.getManifestObjects(context.Background(), driverCR, catalog)
	require.NoError(t, err)
	assert.Empty(t, objects)
}

func TestGetManifestObjectsWithNode(t *testing.T) {
	scheme := driverTestScheme(t)
	fakeClient := newDriverFakeClient(scheme, newGPUNode("gpu-node", "driver-a"))
	driverState := newTestStateDriver(t, fakeClient, scheme)
	driverCR := newDriverCR("driver-a")

	catalog := NewInfoCatalog()
	catalog.Add(InfoTypeHostRoot, "/host")
	catalog.Add(InfoTypeClusterInfo, fakeClusterInfo{})

	objects, err := driverState.getManifestObjects(context.Background(), driverCR, catalog)
	require.NoError(t, err)
	require.NotEmpty(t, objects)

	_, err = getObjectOfKind(objects, "DaemonSet")
	require.NoError(t, err)
}

func TestSyncWrongCRType(t *testing.T) {
	driverState := newTestStateDriver(t, nil, driverTestScheme(t))

	syncState, err := driverState.Sync(context.Background(), "not-a-cr", NewInfoCatalog())
	require.Error(t, err)
	assert.Equal(t, SyncState(SyncStateError), syncState)
}

func TestSyncNoNodesReady(t *testing.T) {
	scheme := driverTestScheme(t)
	fakeClient := newDriverFakeClient(scheme)
	driverState := newTestStateDriver(t, fakeClient, scheme)
	driverCR := newDriverCR("driver-a")

	catalog := NewInfoCatalog()
	catalog.Add(InfoTypeHostRoot, "/host")
	catalog.Add(InfoTypeClusterInfo, fakeClusterInfo{})

	// Without GPU nodes there are no node pools, so nothing is rendered and
	// getSyncState reports Ready over the empty object list.
	syncState, err := driverState.Sync(context.Background(), driverCR, catalog)
	require.NoError(t, err)
	assert.Equal(t, SyncState(SyncStateReady), syncState)
}

func TestSyncCreatesObjects(t *testing.T) {
	scheme := driverTestScheme(t)
	fakeClient := newDriverFakeClient(scheme, newGPUNode("gpu-node", "driver-a"))
	driverState := newTestStateDriver(t, fakeClient, scheme)
	driverCR := newDriverCR("driver-a")

	catalog := NewInfoCatalog()
	catalog.Add(InfoTypeHostRoot, "/host")
	catalog.Add(InfoTypeClusterInfo, fakeClusterInfo{})

	// The returned SyncState is meaningless here: no DaemonSet controller runs behind the
	// fake client, so Generation, ObservedGeneration and DesiredNumberScheduled all stay
	// zero and isDaemonSetReady takes its "targets zero nodes" branch. Readiness is covered
	// with explicit statuses in TestIsDaemonSetReady.
	_, err := driverState.Sync(context.Background(), driverCR, catalog)
	require.NoError(t, err)

	daemonSets := &appsv1.DaemonSetList{}
	require.NoError(t, fakeClient.List(context.Background(), daemonSets))
	require.Len(t, daemonSets.Items, 1)
	syncedDaemonSet := daemonSets.Items[0]

	assert.True(t, strings.HasPrefix(syncedDaemonSet.Name, "nvidia-gpu-driver-ubuntu22.04-"),
		"unexpected DaemonSet name %q", syncedDaemonSet.Name)
	assert.Equal(t, "test-operator", syncedDaemonSet.Namespace)
	assert.Equal(t, "state-driver", syncedDaemonSet.Labels[consts.StateLabel])
	assert.NotEmpty(t, syncedDaemonSet.Annotations[consts.NvidiaAnnotationHashKey])

	require.Len(t, syncedDaemonSet.OwnerReferences, 1)
	ownerRef := syncedDaemonSet.OwnerReferences[0]
	assert.Equal(t, "NVIDIADriver", ownerRef.Kind)
	assert.Equal(t, driverCR.Name, ownerRef.Name)
	assert.Equal(t, driverCR.UID, ownerRef.UID)
	require.NotNil(t, ownerRef.Controller)
	assert.True(t, *ownerRef.Controller)
}

func TestGetDriverName(t *testing.T) {
	testCases := []struct {
		name               string
		driverType         nvidiav1alpha1.DriverType
		expectedDriverName string
	}{
		{name: "GPU", driverType: nvidiav1alpha1.GPU, expectedDriverName: "nvidia-gpu-driver-my-driver-ubuntu22.04"},
		{name: "vGPU host manager", driverType: nvidiav1alpha1.VGPUHostManager, expectedDriverName: "nvidia-vgpu-manager-my-driver-ubuntu22.04"},
	}
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			driverCR := &nvidiav1alpha1.NVIDIADriver{
				ObjectMeta: metav1.ObjectMeta{Name: "my-driver"},
				Spec:       nvidiav1alpha1.NVIDIADriverSpec{DriverType: testCase.driverType},
			}
			assert.Equal(t, testCase.expectedDriverName, getDriverName(driverCR, "ubuntu22.04"))
		})
	}
}

func TestGetDriverSpecErrors(t *testing.T) {
	t.Run("nil CR", func(t *testing.T) {
		_, err := getDriverSpec(nil, nodePool{})
		require.ErrorContains(t, err, "no NVIDIADriver CR provided")
	})

	t.Run("invalid driver image reference", func(t *testing.T) {
		driverCR := &nvidiav1alpha1.NVIDIADriver{
			ObjectMeta: metav1.ObjectMeta{Name: "driver-a"},
			Spec: nvidiav1alpha1.NVIDIADriverSpec{
				DriverType: nvidiav1alpha1.GPU,
				Repository: "nvcr.io/nvidia",
				Image:      "INVALID IMAGE",
				Version:    "535.104.05",
			},
		}
		_, err := getDriverSpec(driverCR, nodePool{osTag: "ubuntu22.04"})
		require.Error(t, err)
	})
}

func TestHandleDefaultImagesInObjectsManagerImageSet(t *testing.T) {
	scheme := driverTestScheme(t)
	driverState := newTestStateDriver(t, newDriverFakeClient(scheme), scheme)

	driverCR := newDriverCR("driver-a")
	require.NotEmpty(t, driverCR.Spec.Manager.Image)
	renderData := getMinimalDriverRenderData()
	objects := []*unstructured.Unstructured{newDaemonSetUnstructured("ds-a", "test-operator")}

	objectsWithDefaultImages, err := driverState.handleDefaultImagesInObjects(context.Background(), objects, driverCR, *renderData)
	require.NoError(t, err)
	assert.Equal(t, objects, objectsWithDefaultImages)
}

func TestHandleDefaultImagesInObjectsCurrentDaemonSetNotFound(t *testing.T) {
	scheme := driverTestScheme(t)
	fakeClient := newDriverFakeClient(scheme)
	driverState := newTestStateDriver(t, fakeClient, scheme)

	driverCR := newDriverCR("driver-a")
	driverCR.Spec.Manager.Image = ""
	renderData := getMinimalDriverRenderData()
	objects := []*unstructured.Unstructured{newDaemonSetUnstructured("nvidia-gpu-driver-test", "test-operator")}

	objectsWithDefaultImages, err := driverState.handleDefaultImagesInObjects(context.Background(), objects, driverCR, *renderData)
	require.NoError(t, err)
	assert.Equal(t, objects, objectsWithDefaultImages)
}
