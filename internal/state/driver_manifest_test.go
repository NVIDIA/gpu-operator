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

	"github.com/go-logr/logr/funcr"
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
	"k8s.io/apimachinery/pkg/util/validation"
	"k8s.io/utils/ptr"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/cache"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/client/interceptor"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/log"

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
	catalog.Add(InfoTypeHostRoot, "/host")
	catalog.Add(InfoTypeClusterInfo, fakeClusterInfo{})
	return catalog
}

func daemonSetsByName(t *testing.T, objs []*unstructured.Unstructured) map[string]*appsv1.DaemonSet {
	t.Helper()
	byName := map[string]*appsv1.DaemonSet{}
	for _, object := range objs {
		if object.GetKind() != "DaemonSet" {
			continue
		}
		daemonSet := &appsv1.DaemonSet{}
		require.NoError(t, runtime.DefaultUnstructuredConverter.FromUnstructured(object.Object, daemonSet))
		byName[daemonSet.Name] = daemonSet
	}
	return byName
}

// requireDS returns the single DaemonSet whose name has the given prefix, failing
// if zero or more than one match (names carry a nondeterministic hash suffix).
func requireDS(t *testing.T, byName map[string]*appsv1.DaemonSet, prefix string) *appsv1.DaemonSet {
	t.Helper()
	var found *appsv1.DaemonSet
	for name, ds := range byName {
		if strings.HasPrefix(name, prefix) {
			require.Nil(t, found, "multiple DaemonSets match prefix %q", prefix)
			found = ds
		}
	}
	require.NotNil(t, found, "no DaemonSet matched prefix %q", prefix)
	return found
}

// newGPUNodeOS builds a GPU node advertising a specific OS release/version.
func newGPUNodeOS(name, owner, osID, osVersion string) *corev1.Node {
	return &corev1.Node{ObjectMeta: metav1.ObjectMeta{
		Name: name,
		Labels: map[string]string{
			consts.GPUPresentLabel:        "true",
			consts.NVIDIADriverOwnerLabel: owner,
			nfdOSReleaseIDLabelKey:        osID,
			nfdOSVersionIDLabelKey:        osVersion,
		},
	}}
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

	// "nvidia-gpu-driver-" (18 chars) + the CR name, truncated to 253.
	expected := "nvidia-gpu-driver-" + strings.Repeat("a", 253-len("nvidia-gpu-driver-"))
	assert.Equal(t, expected, name)
	assert.Len(t, name, 253)
	// The truncated name must remain a valid Kubernetes object name.
	assert.Empty(t, validation.IsDNS1123Subdomain(name))
	// Deterministic for identical input.
	assert.Equal(t, name, getDriverName(cr, "ubuntu22.04"))
}

// --- startup probe defaults ----------------------------------------------------

func TestGetDefaultStartupProbe(t *testing.T) {
	testCases := []struct {
		name        string
		precompiled bool
		wantInitial int32
	}{
		{"standard driver uses the longer 60s delay", false, 60},
		{"precompiled driver uses the shorter 5s delay", true, 5},
	}
	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			spec := &nvidiav1alpha1.NVIDIADriverSpec{}
			if tc.precompiled {
				spec.UsePrecompiled = ptr.To(true)
			}
			probe := getDefaultStartupProbe(spec)
			require.NotNil(t, probe)
			assert.Equal(t, tc.wantInitial, probe.InitialDelaySeconds)
			// The remaining defaults are shared regardless of driver type.
			assert.Equal(t, int32(60), probe.TimeoutSeconds)
			assert.Equal(t, int32(10), probe.PeriodSeconds)
			assert.Equal(t, int32(1), probe.SuccessThreshold)
			assert.Equal(t, int32(120), probe.FailureThreshold)
		})
	}
}

func TestGetDriverSpecPreservesUserStartupProbe(t *testing.T) {
	cr := newDriverCR("driver-a")
	custom := &nvidiav1alpha1.ContainerProbeSpec{
		InitialDelaySeconds: 7, TimeoutSeconds: 3, PeriodSeconds: 2, SuccessThreshold: 1, FailureThreshold: 9,
	}
	cr.Spec.StartupProbe = custom

	spec, err := getDriverSpec(cr, nodePool{osTag: "ubuntu22.04"})
	require.NoError(t, err)
	// A user-provided probe must be preserved, not replaced by the defaults.
	assert.Equal(t, custom, spec.Spec.StartupProbe)
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
	sd := newTestStateDriver(t, cl, sch)

	catalog := NewInfoCatalog()
	catalog.Add(InfoTypeHostRoot, "/host")
	catalog.Add(InfoTypeClusterInfo, fakeClusterInfo{openshiftVersionErr: fmt.Errorf("boom")})

	_, err := sd.getManifestObjects(context.Background(), newDriverCR("driver-a"), catalog)
	require.ErrorContains(t, err, "failed to construct cluster runtime spec")
}

func TestGetManifestObjectsNodeListError(t *testing.T) {
	sch := driverTestScheme(t)
	errInjected := errors.New("injected node list error")
	cl := fake.NewClientBuilder().WithScheme(sch).
		WithIndex(&appsv1.DaemonSet{}, consts.NVIDIADriverControllerIndexKey, func(_ client.Object) []string { return nil }).
		WithInterceptorFuncs(interceptor.Funcs{
			List: func(ctx context.Context, cl client.WithWatch, list client.ObjectList, opts ...client.ListOption) error {
				if _, ok := list.(*corev1.NodeList); ok {
					return errInjected
				}
				return cl.List(ctx, list, opts...)
			},
		}).Build()
	sd := newTestStateDriver(t, cl, sch)

	_, err := sd.getManifestObjects(context.Background(), newDriverCR("driver-a"), fullCatalog())
	require.ErrorIs(t, err, errInjected)
	require.ErrorContains(t, err, "failed to get node pools")
}

func TestGetManifestObjectsHostRootWrongType(t *testing.T) {
	sch := driverTestScheme(t)
	cl := driverIndexBuilder(sch, newGPUNode("gpu-node", "driver-a"))
	sd := newTestStateDriver(t, cl, sch)

	catalog := NewInfoCatalog()
	catalog.Add(InfoTypeHostRoot, 123) // present but not a string
	catalog.Add(InfoTypeClusterInfo, fakeClusterInfo{})

	_, err := sd.getManifestObjects(context.Background(), newDriverCR("driver-a"), catalog)
	require.ErrorContains(t, err, "host root in info catalog has unexpected type")
}

func TestGetManifestObjectsDriverSpecError(t *testing.T) {
	sch := driverTestScheme(t)
	cl := driverIndexBuilder(sch, newGPUNode("gpu-node", "driver-a"))
	sd := newTestStateDriver(t, cl, sch)

	cr := newDriverCR("driver-a")
	cr.Spec.Image = "INVALID IMAGE" // breaks getDriverImagePath inside getDriverSpec

	_, err := sd.getManifestObjects(context.Background(), cr, fullCatalog())
	require.ErrorContains(t, err, "failed to construct driver spec")
}

func TestGetManifestObjectsGDSError(t *testing.T) {
	sch := driverTestScheme(t)
	cl := driverIndexBuilder(sch, newGPUNode("gpu-node", "driver-a"))
	sd := newTestStateDriver(t, cl, sch)

	cr := newDriverCR("driver-a")
	cr.Spec.GPUDirectStorage = &nvidiav1alpha1.GPUDirectStorageSpec{
		Enabled: ptr.To(true),
		Image:   "INVALID IMAGE",
	}

	_, err := sd.getManifestObjects(context.Background(), cr, fullCatalog())
	require.ErrorContains(t, err, "failed to construct GDS spec")
}

func TestGetManifestObjectsGDRCopyError(t *testing.T) {
	sch := driverTestScheme(t)
	cl := driverIndexBuilder(sch, newGPUNode("gpu-node", "driver-a"))
	sd := newTestStateDriver(t, cl, sch)

	cr := newDriverCR("driver-a")
	cr.Spec.GDRCopy = &nvidiav1alpha1.GDRCopySpec{
		Enabled: ptr.To(true),
		Image:   "INVALID IMAGE",
	}

	_, err := sd.getManifestObjects(context.Background(), cr, fullCatalog())
	require.ErrorContains(t, err, "failed to construct GDRCopy spec")
}

func TestGetManifestObjectsPrecompiled(t *testing.T) {
	sch := driverTestScheme(t)
	// A kernel that actually requires sanitization: the arch suffix is stripped
	// (and trailing dot trimmed) for the resource-metadata label, while the image
	// tag and node selector keep the raw kernel.
	const rawKernel = "5.14.0-427.el9.x86_64"
	const sanitizedKernel = "5.14.0-427.el9"
	node := newGPUNode("gpu-node", "driver-a")
	node.Labels[nfdKernelLabelKey] = rawKernel
	cl := driverIndexBuilder(sch, node)
	sd := newTestStateDriver(t, cl, sch)

	cr := newDriverCR("driver-a")
	cr.Spec.UsePrecompiled = ptr.To(true)

	objs, err := sd.getManifestObjects(context.Background(), cr, fullCatalog())
	require.NoError(t, err)

	ds, err := getDaemonsetFromObjects(objs)
	require.NoError(t, err)

	// The node selector and image tag pin the exact (raw) kernel...
	assert.Equal(t, rawKernel, ds.Spec.Template.Spec.NodeSelector[nfdKernelLabelKey])
	drv := containerByName(t, ds, "nvidia-driver-ctr")
	assert.Equal(t, "nvcr.io/nvidia/driver:535.104.05-"+rawKernel+"-ubuntu22.04", drv.Image)
	// ...while the precompiled labels mark the branch and carry the sanitized kernel.
	assert.Equal(t, "true", ds.Labels["nvidia.com/precompiled"])
	assert.Equal(t, sanitizedKernel, ds.Labels["nvidia.com/precompiled.kernel-version"])
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
	const dtkImage = "quay.io/openshift-release-dev/ocp-v4.0-art-dev@sha256:7fecaebc1d51b28bc3548171907e4d91823a031d7a6a694ab686999be2b4d867"
	cl := driverIndexBuilder(sch, node)
	sd := newTestStateDriver(t, cl, sch)

	catalog := NewInfoCatalog()
	catalog.Add(InfoTypeHostRoot, "/host")
	catalog.Add(InfoTypeClusterInfo, fakeClusterInfo{
		openshiftVersion: "4.13",
		dtkImages:        map[string]string{rhcosVersion: dtkImage},
	})

	cr := newDriverCR("driver-a")
	objs, err := sd.getManifestObjects(context.Background(), cr, catalog)
	require.NoError(t, err)

	ds, err := getDaemonsetFromObjects(objs)
	require.NoError(t, err)

	// DTK build path: the RHCOS OSTree selector pins the pool...
	assert.Equal(t, rhcosVersion, ds.Spec.Template.Spec.NodeSelector[nfdOSTreeVersionLabelKey])
	// ...the driver-toolkit container carries the resolved DTK image...
	dtk := containerByName(t, ds, "openshift-driver-toolkit-ctr")
	assert.Equal(t, dtkImage, dtk.Image)
	// ...and the DTK labels mark the build path (not the ordinary prebuilt path).
	assert.Equal(t, "true", ds.Labels[consts.OcpDriverToolkitIdentificationLabel])
	assert.Equal(t, rhcosVersion, ds.Labels[consts.OcpDriverToolkitVersionLabel])

	// With a matching DTK image, the missing-image fallback markers must be absent.
	assert.NotContains(t, ds.Labels, dtkImageMissingLabel)
	_, driverMissing := envValue(containerByName(t, ds, "nvidia-driver-ctr"), "RHCOS_IMAGE_MISSING")
	assert.False(t, driverMissing, "driver container must not carry RHCOS_IMAGE_MISSING when a DTK image is found")
	_, dtkMissing := envValue(dtk, "RHCOS_IMAGE_MISSING")
	assert.False(t, dtkMissing, "DTK container must not carry RHCOS_IMAGE_MISSING when a DTK image is found")
}

func envValue(container corev1.Container, name string) (string, bool) {
	for _, env := range container.Env {
		if env.Name == name {
			return env.Value, true
		}
	}
	return "", false
}

const dtkImageMissingLabel = "openshift.driver-toolkit.rhcos-image-missing"

func TestGetManifestObjectsOpenshiftDTKMissingImageForNodePool(t *testing.T) {
	// When the DTK image map has no entry for a pool's RHCOS version, the template
	// falls back to the driver image (not an empty image), so the DaemonSet is valid.
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
	sd := newTestStateDriver(t, driverIndexBuilder(sch, node), sch)

	catalog := NewInfoCatalog()
	catalog.Add(InfoTypeHostRoot, "/host")
	catalog.Add(InfoTypeClusterInfo, fakeClusterInfo{
		openshiftVersion: "4.13",
		// Nonempty (so DTK is enabled) but with no entry for this node pool's RHCOS version.
		dtkImages: map[string]string{"999.99.99-0": "quay.io/dtk@sha256:other"},
	})

	objs, err := sd.getManifestObjects(context.Background(), newDriverCR("driver-a"), catalog)
	require.NoError(t, err)

	ds, err := getDaemonsetFromObjects(objs)
	require.NoError(t, err)
	dtk := containerByName(t, ds, "openshift-driver-toolkit-ctr")
	driver := containerByName(t, ds, "nvidia-driver-ctr")
	assert.NotEmpty(t, dtk.Image, "missing DTK image must not render an empty container image")
	assert.Equal(t, driver.Image, dtk.Image, "driver-toolkit falls back to the driver image when no DTK image matches the pool")

	// The fallback also flips the marker label and the RHCOS env in both containers,
	// which drive the DTK sidecar to self-build the driver.
	assert.Equal(t, "true", ds.Labels[dtkImageMissingLabel])
	assert.Equal(t, "true", ds.Spec.Template.Labels[dtkImageMissingLabel])
	for _, container := range []corev1.Container{driver, dtk} {
		missing, ok := envValue(container, "RHCOS_IMAGE_MISSING")
		assert.True(t, ok, "%s missing RHCOS_IMAGE_MISSING env", container.Name)
		assert.Equal(t, "true", missing)
		version, ok := envValue(container, "RHCOS_VERSION")
		assert.True(t, ok, "%s missing RHCOS_VERSION env", container.Name)
		assert.Equal(t, rhcosVersion, version)
	}
}

func TestGetManifestObjectsMultipleNodePools(t *testing.T) {
	sch := driverTestScheme(t)

	t.Run("distinct OS versions render one DaemonSet each", func(t *testing.T) {
		cl := driverIndexBuilder(sch,
			newGPUNode("u22", "driver-a"),                      // ubuntu 22.04
			newGPUNodeOS("u20", "driver-a", "ubuntu", "20.04"), // ubuntu 20.04
		)
		sd := newTestStateDriver(t, cl, sch)

		objs, err := sd.getManifestObjects(context.Background(), newDriverCR("driver-a"), fullCatalog())
		require.NoError(t, err)

		// Compare as a set: the render loop appends pools in nondeterministic order.
		byName := daemonSetsByName(t, objs)
		require.Len(t, byName, 2)
		u22 := requireDS(t, byName, "nvidia-gpu-driver-ubuntu22.04-")
		u20 := requireDS(t, byName, "nvidia-gpu-driver-ubuntu20.04-")

		assert.Equal(t, "22.04", u22.Spec.Template.Spec.NodeSelector[nfdOSVersionIDLabelKey])
		assert.Equal(t, "20.04", u20.Spec.Template.Spec.NodeSelector[nfdOSVersionIDLabelKey])
		assert.Equal(t, "nvcr.io/nvidia/driver:535.104.05-ubuntu22.04", containerByName(t, u22, "nvidia-driver-ctr").Image)
		assert.Equal(t, "nvcr.io/nvidia/driver:535.104.05-ubuntu20.04", containerByName(t, u20, "nvidia-driver-ctr").Image)
	})

	t.Run("two nodes in the same pool render a single DaemonSet", func(t *testing.T) {
		cl := driverIndexBuilder(sch,
			newGPUNode("u22-a", "driver-a"),
			newGPUNode("u22-b", "driver-a"),
		)
		sd := newTestStateDriver(t, cl, sch)

		objs, err := sd.getManifestObjects(context.Background(), newDriverCR("driver-a"), fullCatalog())
		require.NoError(t, err)
		assert.Len(t, daemonSetsByName(t, objs), 1)
	})
}

func TestGetManifestObjectsAdditionalConfigsErrorIsLogged(t *testing.T) {
	sch := driverTestScheme(t)
	cl := driverIndexBuilder(sch, newGPUNode("gpu-node", "driver-a"))
	sd := newTestStateDriver(t, cl, sch)

	cr := newDriverCR("driver-a")
	// Reference a ConfigMap that does not exist -> getDriverAdditionalConfigs errors.
	cr.Spec.RepoConfig = &nvidiav1alpha1.DriverRepoConfigSpec{Name: "missing-repo-config"}

	// Capture the logger to assert the failure is logged and non-fatal, not swallowed.
	var logs strings.Builder
	logger := funcr.New(func(_, args string) { logs.WriteString(args + "\n") }, funcr.Options{})
	ctx := log.IntoContext(context.Background(), logger)

	objs, err := sd.getManifestObjects(ctx, cr, fullCatalog())
	require.NoError(t, err)
	require.NotEmpty(t, objs)
	// The log must name the specific ConfigMap that failed to load, not just a generic message.
	assert.Contains(t, logs.String(), "error rendering addition driver volume")
	assert.Contains(t, logs.String(), "missing-repo-config")

	// Since the requested config could not be resolved, its volume must not be
	// mounted; the driver is deployed without the configuration the user asked for.
	ds, err := getDaemonsetFromObjects(objs)
	require.NoError(t, err)
	for _, volume := range ds.Spec.Template.Spec.Volumes {
		assert.NotEqual(t, "missing-repo-config", volume.Name, "unresolved repo config must not be mounted")
	}
}

func TestGetManifestObjectsHandleDefaultImagesError(t *testing.T) {
	sch := driverTestScheme(t)
	cl := fake.NewClientBuilder().WithScheme(sch).
		WithObjects(newGPUNode("gpu-node", "driver-a")).
		WithIndex(&appsv1.DaemonSet{}, consts.NVIDIADriverControllerIndexKey, func(_ client.Object) []string { return nil }).
		WithInterceptorFuncs(interceptor.Funcs{
			Get: func(ctx context.Context, cl client.WithWatch, key client.ObjectKey, obj client.Object, opts ...client.GetOption) error {
				if _, ok := obj.(*appsv1.DaemonSet); ok {
					return fmt.Errorf("injected daemonset get error")
				}
				return cl.Get(ctx, key, obj, opts...)
			},
		}).Build()
	sd := newTestStateDriver(t, cl, sch)

	cr := newDriverCR("driver-a")
	// The default (env-var) driver-manager image lets rendering succeed, so
	// handleDefaultImages then Gets the current DaemonSet to check for an update.
	cr.Spec.Manager.Repository = ""
	cr.Spec.Manager.Image = ""
	cr.Spec.Manager.Version = ""
	t.Setenv("DRIVER_MANAGER_IMAGE", "nvcr.io/nvidia/cloud-native/k8s-driver-manager:v0.6.2")

	_, err := sd.getManifestObjects(context.Background(), cr, fullCatalog())
	require.ErrorContains(t, err, "failed to get current driver DaemonSet")
}

// --- Sync error paths ----------------------------------------------------------

func TestSyncGetManifestObjectsError(t *testing.T) {
	sd := newTestStateDriver(t, nil, driverTestScheme(t))

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
	sd := newTestStateDriver(t, cl, sch)

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
	sd := newTestStateDriver(t, cl, sch)

	syncState, err := sd.Sync(context.Background(), newDriverCR("driver-a"), fullCatalog())
	require.ErrorContains(t, err, "failed to create/update objects")
	assert.Equal(t, SyncState(SyncStateNotReady), syncState)
}

func TestSyncGetSyncStateError(t *testing.T) {
	sch := driverTestScheme(t)
	errInjected := errors.New("injected get error")
	cl := fake.NewClientBuilder().WithScheme(sch).
		WithObjects(newGPUNode("gpu-node", "driver-a")).
		WithIndex(&appsv1.DaemonSet{}, consts.NVIDIADriverControllerIndexKey, func(_ client.Object) []string { return nil }).
		WithInterceptorFuncs(interceptor.Funcs{
			Get: func(ctx context.Context, cl client.WithWatch, key client.ObjectKey, obj client.Object, opts ...client.GetOption) error {
				// Fail only the readiness Gets (unstructured) performed by getSyncState.
				if _, ok := obj.(*unstructured.Unstructured); ok {
					return errInjected
				}
				return cl.Get(ctx, key, obj, opts...)
			},
		}).Build()
	sd := newTestStateDriver(t, cl, sch)

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
			WithIndex(&appsv1.DaemonSet{}, consts.NVIDIADriverControllerIndexKey, nvidiaDriverControllerIndex).
			WithInterceptorFuncs(interceptor.Funcs{
				Delete: func(_ context.Context, _ client.WithWatch, _ client.Object, _ ...client.DeleteOption) error {
					return fmt.Errorf("injected delete error")
				},
			}).Build()
		sd := newTestStateDriver(t, cl, sch)
		cr := &nvidiav1alpha1.NVIDIADriver{ObjectMeta: metav1.ObjectMeta{Name: "driver-a"}}
		// desiredObjs empty -> dsStale is not desired -> deleted -> delete error.
		err := sd.cleanupStaleDriverDaemonsets(context.Background(), cr, nil)
		require.ErrorContains(t, err, "error deleting DaemonSet")
	})

	t.Run("node list error", func(t *testing.T) {
		dsInactive := makeDaemonSet("ds-inactive", "driver-a", 0, 0, map[string]string{"pool": "gold"})
		cl := fake.NewClientBuilder().WithScheme(sch).WithObjects(dsInactive).
			WithIndex(&appsv1.DaemonSet{}, consts.NVIDIADriverControllerIndexKey, nvidiaDriverControllerIndex).
			WithInterceptorFuncs(interceptor.Funcs{
				List: func(ctx context.Context, cl client.WithWatch, list client.ObjectList, opts ...client.ListOption) error {
					if _, ok := list.(*corev1.NodeList); ok {
						return fmt.Errorf("injected node list error")
					}
					return cl.List(ctx, list, opts...)
				},
			}).Build()
		sd := newTestStateDriver(t, cl, sch)
		cr := &nvidiav1alpha1.NVIDIADriver{ObjectMeta: metav1.ObjectMeta{Name: "driver-a"}}
		desired := []*unstructured.Unstructured{newDaemonSetUnstructured("ds-inactive", "test-operator")}
		err := sd.cleanupStaleDriverDaemonsets(context.Background(), cr, desired)
		require.ErrorContains(t, err, "failed to list nodes")
	})

	t.Run("inactive daemonset delete error", func(t *testing.T) {
		dsInactive := makeDaemonSet("ds-inactive", "driver-a", 0, 0, map[string]string{"pool": "silver"})
		cl := fake.NewClientBuilder().WithScheme(sch).WithObjects(dsInactive).
			WithIndex(&appsv1.DaemonSet{}, consts.NVIDIADriverControllerIndexKey, nvidiaDriverControllerIndex).
			WithInterceptorFuncs(interceptor.Funcs{
				Delete: func(_ context.Context, _ client.WithWatch, _ client.Object, _ ...client.DeleteOption) error {
					return fmt.Errorf("injected delete error")
				},
			}).Build()
		sd := newTestStateDriver(t, cl, sch)
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
	sd := newTestStateDriver(t, cl, sch)

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

	sd := newTestStateDriver(t, nil, sch)
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
	sd := newTestStateDriver(t, cl, sch)

	cr := newDriverCR("driver-a")
	cr.Spec.Manager.Image = ""
	renderData := getMinimalDriverRenderData()
	objs := []*unstructured.Unstructured{newDaemonSetUnstructured("ds-a", "test-operator")}

	_, err := sd.handleDefaultImagesInObjects(context.Background(), objs, cr, *renderData)
	require.ErrorContains(t, err, "failed to get current driver DaemonSet")
}

func TestHandleDefaultImagesReRenderError(t *testing.T) {
	sch := driverTestScheme(t)
	sd := newTestStateDriver(t, nil, sch)

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
	renderSd := newTestStateDriver(t, nil, driverTestScheme(t))
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

	sd := newTestStateDriver(t, cl, sch)

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
	hashSd := newTestStateDriver(t, nil, sch)
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
	renderSd := newTestStateDriver(t, nil, sch)
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

	sd := newTestStateDriver(t, cl, sch)

	cr.Spec.Manager.Image = ""
	got, err := sd.handleDefaultImagesInObjects(context.Background(), desiredObjs, cr, *renderData)
	require.NoError(t, err)
	// The returned objects use the current (unchanged) manager image.
	gotDs, err := getDaemonsetFromObjects(got)
	require.NoError(t, err)
	assert.Equal(t, currentImage, managerImageFromDaemonSet(gotDs))
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

	sd := newTestStateDriver(t, nil, sch)

	mgr := &fakeManager{scheme: sch, mapper: mapper}
	sources := sd.GetWatchSources(mgr)
	require.Contains(t, sources, "DaemonSet")
	assert.NotNil(t, sources["DaemonSet"])
}
