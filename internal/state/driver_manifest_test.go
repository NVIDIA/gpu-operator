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

const (
	// Unlike its sibling driver-toolkit labels, this one is not exported by
	// internal/consts, so the manifest template's spelling is repeated here.
	dtkImageMissingLabel = "openshift.driver-toolkit.rhcos-image-missing"

	testOpenshiftVersion       = "4.13"
	driverManagerContainerName = "k8s-driver-manager"
)

func schemeWithoutNVIDIADriver(t *testing.T) *runtime.Scheme {
	t.Helper()
	scheme := runtime.NewScheme()
	require.NoError(t, corev1.AddToScheme(scheme))
	require.NoError(t, appsv1.AddToScheme(scheme))
	return scheme
}

func driverInfoCatalog(clusterInfo fakeClusterInfo) InfoCatalog {
	catalog := NewInfoCatalog()
	catalog.Add(InfoTypeHostRoot, "/host")
	catalog.Add(InfoTypeClusterInfo, clusterInfo)
	return catalog
}

// newDriverClientWithInterceptors mirrors newDriverFakeClient but lets a test
// inject client failures.
func newDriverClientWithInterceptors(scheme *runtime.Scheme, interceptorFuncs interceptor.Funcs, objects ...client.Object) client.Client {
	return fake.NewClientBuilder().
		WithScheme(scheme).
		WithObjects(objects...).
		WithIndex(&appsv1.DaemonSet{}, consts.NVIDIADriverControllerIndexKey, nvidiaDriverControllerIndex).
		WithInterceptorFuncs(interceptorFuncs).
		Build()
}

func failingListInterceptor[L client.ObjectList](listErr error) interceptor.Funcs {
	return interceptor.Funcs{
		List: func(ctx context.Context, wrappedClient client.WithWatch, list client.ObjectList, opts ...client.ListOption) error {
			if _, ok := list.(L); ok {
				return listErr
			}
			return wrappedClient.List(ctx, list, opts...)
		},
	}
}

func failingGetInterceptor[O client.Object](getErr error) interceptor.Funcs {
	return interceptor.Funcs{
		Get: func(ctx context.Context, wrappedClient client.WithWatch, key client.ObjectKey, object client.Object, opts ...client.GetOption) error {
			if _, ok := object.(O); ok {
				return getErr
			}
			return wrappedClient.Get(ctx, key, object, opts...)
		},
	}
}

func failingDeleteInterceptor(deleteErr error) interceptor.Funcs {
	return interceptor.Funcs{
		Delete: func(_ context.Context, _ client.WithWatch, _ client.Object, _ ...client.DeleteOption) error {
			return deleteErr
		},
	}
}

func renderDriverObjects(t *testing.T, scheme *runtime.Scheme, renderData *driverRenderData) []*unstructured.Unstructured {
	t.Helper()
	objects, err := newTestStateDriver(t, nil, scheme).renderManifestObjects(context.Background(), renderData)
	require.NoError(t, err)
	return objects
}

func newDaemonSetWithManagerImage(name, namespace, managerImage string) *appsv1.DaemonSet {
	return &appsv1.DaemonSet{
		ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: namespace},
		Spec: appsv1.DaemonSetSpec{
			Template: corev1.PodTemplateSpec{
				Spec: corev1.PodSpec{
					InitContainers: []corev1.Container{{Name: driverManagerContainerName, Image: managerImage}},
				},
			},
		},
	}
}

func daemonSetsByName(t *testing.T, objects []*unstructured.Unstructured) map[string]*appsv1.DaemonSet {
	t.Helper()
	byName := map[string]*appsv1.DaemonSet{}
	for _, object := range objects {
		if object.GetKind() != "DaemonSet" {
			continue
		}
		daemonSet := &appsv1.DaemonSet{}
		require.NoError(t, runtime.DefaultUnstructuredConverter.FromUnstructured(object.Object, daemonSet))
		byName[daemonSet.Name] = daemonSet
	}
	return byName
}

// requireDaemonSetWithPrefix matches on a prefix because rendered DaemonSet names
// carry a hash suffix derived from the CR UID.
func requireDaemonSetWithPrefix(t *testing.T, daemonSets map[string]*appsv1.DaemonSet, prefix string) *appsv1.DaemonSet {
	t.Helper()
	var matchingDaemonSet *appsv1.DaemonSet
	for name, daemonSet := range daemonSets {
		if strings.HasPrefix(name, prefix) {
			require.Nil(t, matchingDaemonSet, "multiple DaemonSets match prefix %q", prefix)
			matchingDaemonSet = daemonSet
		}
	}
	require.NotNil(t, matchingDaemonSet, "no DaemonSet matched prefix %q", prefix)
	return matchingDaemonSet
}

func newGPUNodeOS(name, ownerDriverName, osID, osVersion string) *corev1.Node {
	return &corev1.Node{ObjectMeta: metav1.ObjectMeta{
		Name: name,
		Labels: map[string]string{
			consts.GPUPresentLabel:        "true",
			consts.NVIDIADriverOwnerLabel: ownerDriverName,
			nfdOSReleaseIDLabelKey:        osID,
			nfdOSVersionIDLabelKey:        osVersion,
		},
	}}
}

func newRHCOSNode(name, ownerDriverName, rhcosVersion string) *corev1.Node {
	node := newGPUNodeOS(name, ownerDriverName, "rhcos", testOpenshiftVersion)
	node.Labels[nfdOSTreeVersionLabelKey] = rhcosVersion
	return node
}

func envValue(container corev1.Container, name string) (string, bool) {
	for _, env := range container.Env {
		if env.Name == name {
			return env.Value, true
		}
	}
	return "", false
}

func TestNewStateDriverBadManifestDir(t *testing.T) {
	_, err := NewStateDriver(nil, "", nil, "/nonexistent/manifest/dir")
	require.ErrorContains(t, err, "failed to get files from manifest directory")
}

func TestGetDriverNameTruncation(t *testing.T) {
	const namePrefix = "nvidia-gpu-driver-"

	// 253 is the maximum length of a Kubernetes object name, so this CR name is legal.
	driverCR := &nvidiav1alpha1.NVIDIADriver{
		ObjectMeta: metav1.ObjectMeta{Name: strings.Repeat("a", validation.DNS1123SubdomainMaxLength)},
		Spec:       nvidiav1alpha1.NVIDIADriverSpec{DriverType: nvidiav1alpha1.GPU},
	}

	ubuntuName := getDriverName(driverCR, "ubuntu22.04")
	rhcosName := getDriverName(driverCR, "rhcos4.13")

	// Truncation cuts inside the CR name, so the "-<osVersion>" suffix is dropped entirely and
	// a legal CR name collides across every OS. getDriverName names the ServiceAccount, Role,
	// ClusterRole and SCC, so node pools share those; the DaemonSet is unaffected because it is
	// named by getDriverAppName, which ends in a UID-derived hash.
	assert.Equal(t, ubuntuName, rhcosName)
	assert.Equal(t, namePrefix+strings.Repeat("a", validation.DNS1123SubdomainMaxLength-len(namePrefix)), ubuntuName)
	assert.Len(t, ubuntuName, validation.DNS1123SubdomainMaxLength)
	assert.Empty(t, validation.IsDNS1123Subdomain(ubuntuName))
}

func TestGetDefaultStartupProbe(t *testing.T) {
	testCases := []struct {
		name                        string
		precompiled                 bool
		expectedInitialDelaySeconds int32
	}{
		{"standard driver uses the longer 60s delay", false, 60},
		{"precompiled driver uses the shorter 5s delay", true, 5},
	}
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			nvidiaDriverSpec := &nvidiav1alpha1.NVIDIADriverSpec{}
			if testCase.precompiled {
				nvidiaDriverSpec.UsePrecompiled = new(true)
			}

			probe := getDefaultStartupProbe(nvidiaDriverSpec)

			require.NotNil(t, probe)
			assert.Equal(t, testCase.expectedInitialDelaySeconds, probe.InitialDelaySeconds)
			assert.Equal(t, int32(60), probe.TimeoutSeconds)
			assert.Equal(t, int32(10), probe.PeriodSeconds)
			assert.Equal(t, int32(1), probe.SuccessThreshold)
			assert.Equal(t, int32(120), probe.FailureThreshold)
		})
	}
}

func TestGetDriverSpecPreservesUserStartupProbe(t *testing.T) {
	driverCR := newDriverCR("driver-a")
	userProbe := &nvidiav1alpha1.ContainerProbeSpec{
		InitialDelaySeconds: 7, TimeoutSeconds: 3, PeriodSeconds: 2, SuccessThreshold: 1, FailureThreshold: 9,
	}
	driverCR.Spec.StartupProbe = userProbe

	constructedDriverSpec, err := getDriverSpec(driverCR, nodePool{osTag: "ubuntu22.04"})

	require.NoError(t, err)
	assert.Equal(t, userProbe, constructedDriverSpec.Spec.StartupProbe)
}

func TestGetDriverSpecManagerImageError(t *testing.T) {
	// An empty Manager spec only errors when the DRIVER_MANAGER_IMAGE fallback is
	// also unset.
	t.Setenv("DRIVER_MANAGER_IMAGE", "")
	driverCR := &nvidiav1alpha1.NVIDIADriver{
		ObjectMeta: metav1.ObjectMeta{Name: "driver-a"},
		Spec: nvidiav1alpha1.NVIDIADriverSpec{
			DriverType: nvidiav1alpha1.GPU,
			Repository: "nvcr.io/nvidia",
			Image:      "driver",
			Version:    "535.104.05",
			Manager:    nvidiav1alpha1.DriverManagerSpec{},
		},
	}

	_, err := getDriverSpec(driverCR, nodePool{osTag: "ubuntu22.04"})

	require.ErrorContains(t, err, "failed to construct image path for driver manager")
}

func TestGetObjectOfKindNotFound(t *testing.T) {
	_, err := getObjectOfKind([]*unstructured.Unstructured{}, "DaemonSet")
	require.ErrorContains(t, err, "did not find object of kind")
}

func TestGetDaemonsetFromObjectsErrors(t *testing.T) {
	t.Run("no DaemonSet among the objects", func(t *testing.T) {
		_, err := getDaemonsetFromObjects([]*unstructured.Unstructured{newConfigMapUnstructured("cm", "ns")})
		require.ErrorContains(t, err, "did not find object of kind")
	})

	t.Run("DaemonSet with an unconvertible spec", func(t *testing.T) {
		malformedDaemonSet := newDaemonSetUnstructured("ds-bad", "ns")
		malformedDaemonSet.Object["spec"] = "not-a-spec-object"

		_, err := getDaemonsetFromObjects([]*unstructured.Unstructured{malformedDaemonSet})
		require.ErrorContains(t, err, "error converting unstructured object to DaemonSet")
	})
}

func TestRenderManifestObjectsError(t *testing.T) {
	driverState := newTestStateDriver(t, nil, nil)

	// The templates dereference .Driver.Spec fields, which are nil for zero-valued
	// render data, so template execution fails.
	_, err := driverState.renderManifestObjects(context.Background(), &driverRenderData{})

	require.Error(t, err)
}

func TestGetManifestObjectsRuntimeSpecError(t *testing.T) {
	scheme := driverTestScheme(t)
	driverState := newTestStateDriver(t, newDriverFakeClient(scheme), scheme)
	errOpenshiftVersion := errors.New("injected openshift version error")
	catalog := driverInfoCatalog(fakeClusterInfo{openshiftVersionErr: errOpenshiftVersion})

	_, err := driverState.getManifestObjects(context.Background(), newDriverCR("driver-a"), catalog)

	require.ErrorContains(t, err, "failed to construct cluster runtime spec")
	require.ErrorContains(t, err, errOpenshiftVersion.Error())
}

func TestGetManifestObjectsNodeListError(t *testing.T) {
	scheme := driverTestScheme(t)
	errNodeList := errors.New("injected node list error")
	fakeClient := newDriverClientWithInterceptors(scheme, failingListInterceptor[*corev1.NodeList](errNodeList))
	driverState := newTestStateDriver(t, fakeClient, scheme)

	_, err := driverState.getManifestObjects(context.Background(), newDriverCR("driver-a"), driverInfoCatalog(fakeClusterInfo{}))

	require.ErrorIs(t, err, errNodeList)
	require.ErrorContains(t, err, "failed to get node pools")
}

func TestGetManifestObjectsHostRootWrongType(t *testing.T) {
	scheme := driverTestScheme(t)
	fakeClient := newDriverFakeClient(scheme, newGPUNode("gpu-node", "driver-a"))
	driverState := newTestStateDriver(t, fakeClient, scheme)

	catalog := NewInfoCatalog()
	catalog.Add(InfoTypeHostRoot, 123)
	catalog.Add(InfoTypeClusterInfo, fakeClusterInfo{})

	_, err := driverState.getManifestObjects(context.Background(), newDriverCR("driver-a"), catalog)

	require.ErrorContains(t, err, "host root in info catalog has unexpected type")
}

func TestGetManifestObjectsDriverSpecError(t *testing.T) {
	scheme := driverTestScheme(t)
	fakeClient := newDriverFakeClient(scheme, newGPUNode("gpu-node", "driver-a"))
	driverState := newTestStateDriver(t, fakeClient, scheme)

	driverCR := newDriverCR("driver-a")
	driverCR.Spec.Image = "INVALID IMAGE"

	_, err := driverState.getManifestObjects(context.Background(), driverCR, driverInfoCatalog(fakeClusterInfo{}))

	require.ErrorContains(t, err, "failed to construct driver spec")
}

func TestGetManifestObjectsGDSError(t *testing.T) {
	scheme := driverTestScheme(t)
	fakeClient := newDriverFakeClient(scheme, newGPUNode("gpu-node", "driver-a"))
	driverState := newTestStateDriver(t, fakeClient, scheme)

	driverCR := newDriverCR("driver-a")
	driverCR.Spec.GPUDirectStorage = &nvidiav1alpha1.GPUDirectStorageSpec{
		Enabled: new(true),
		Image:   "INVALID IMAGE",
	}

	_, err := driverState.getManifestObjects(context.Background(), driverCR, driverInfoCatalog(fakeClusterInfo{}))

	require.ErrorContains(t, err, "failed to construct GDS spec")
}

func TestGetManifestObjectsGDRCopyError(t *testing.T) {
	scheme := driverTestScheme(t)
	fakeClient := newDriverFakeClient(scheme, newGPUNode("gpu-node", "driver-a"))
	driverState := newTestStateDriver(t, fakeClient, scheme)

	driverCR := newDriverCR("driver-a")
	driverCR.Spec.GDRCopy = &nvidiav1alpha1.GDRCopySpec{
		Enabled: new(true),
		Image:   "INVALID IMAGE",
	}

	_, err := driverState.getManifestObjects(context.Background(), driverCR, driverInfoCatalog(fakeClusterInfo{}))

	require.ErrorContains(t, err, "failed to construct GDRCopy spec")
}

func TestGetManifestObjectsPrecompiled(t *testing.T) {
	scheme := driverTestScheme(t)
	// The arch suffix (and any trailing dot) is stripped for the precompiled
	// labels, while the image tag and node selector keep the raw kernel.
	const rawKernel = "5.14.0-427.el9.x86_64"
	const sanitizedKernel = "5.14.0-427.el9"
	node := newGPUNode("gpu-node", "driver-a")
	node.Labels[nfdKernelLabelKey] = rawKernel
	driverState := newTestStateDriver(t, newDriverFakeClient(scheme, node), scheme)

	driverCR := newDriverCR("driver-a")
	driverCR.Spec.UsePrecompiled = new(true)

	objects, err := driverState.getManifestObjects(context.Background(), driverCR, driverInfoCatalog(fakeClusterInfo{}))
	require.NoError(t, err)

	daemonSet, err := getDaemonsetFromObjects(objects)
	require.NoError(t, err)

	assert.Equal(t, rawKernel, daemonSet.Spec.Template.Spec.NodeSelector[nfdKernelLabelKey])
	driverContainer := containerByName(t, daemonSet, "nvidia-driver-ctr")
	assert.Equal(t, "nvcr.io/nvidia/driver:535.104.05-"+rawKernel+"-ubuntu22.04", driverContainer.Image)
	assert.Equal(t, "true", daemonSet.Labels["nvidia.com/precompiled"])
	assert.Equal(t, sanitizedKernel, daemonSet.Labels["nvidia.com/precompiled.kernel-version"])
}

func TestGetManifestObjectsOpenshiftDTK(t *testing.T) {
	scheme := driverTestScheme(t)
	const rhcosVersion = "413.92.202304252344-0"
	const dtkImage = "quay.io/openshift-release-dev/ocp-v4.0-art-dev@sha256:7fecaebc1d51b28bc3548171907e4d91823a031d7a6a694ab686999be2b4d867"
	node := newRHCOSNode("rhcos-node", "driver-a", rhcosVersion)
	driverState := newTestStateDriver(t, newDriverFakeClient(scheme, node), scheme)

	catalog := driverInfoCatalog(fakeClusterInfo{
		openshiftVersion: testOpenshiftVersion,
		dtkImages:        map[string]string{rhcosVersion: dtkImage},
	})

	objects, err := driverState.getManifestObjects(context.Background(), newDriverCR("driver-a"), catalog)
	require.NoError(t, err)

	daemonSet, err := getDaemonsetFromObjects(objects)
	require.NoError(t, err)

	assert.Equal(t, rhcosVersion, daemonSet.Spec.Template.Spec.NodeSelector[nfdOSTreeVersionLabelKey])
	dtkContainer := containerByName(t, daemonSet, "openshift-driver-toolkit-ctr")
	assert.Equal(t, dtkImage, dtkContainer.Image)
	assert.Equal(t, "true", daemonSet.Labels[consts.OcpDriverToolkitIdentificationLabel])
	assert.Equal(t, rhcosVersion, daemonSet.Labels[consts.OcpDriverToolkitVersionLabel])

	assert.NotContains(t, daemonSet.Labels, dtkImageMissingLabel)
	_, driverHasImageMissingEnv := envValue(containerByName(t, daemonSet, "nvidia-driver-ctr"), "RHCOS_IMAGE_MISSING")
	assert.False(t, driverHasImageMissingEnv, "driver container must not carry RHCOS_IMAGE_MISSING when a DTK image is found")
	_, dtkHasImageMissingEnv := envValue(dtkContainer, "RHCOS_IMAGE_MISSING")
	assert.False(t, dtkHasImageMissingEnv, "DTK container must not carry RHCOS_IMAGE_MISSING when a DTK image is found")
}

func TestGetManifestObjectsOpenshiftDTKMissingImageForNodePool(t *testing.T) {
	// With no DTK image for the pool's RHCOS version the template falls back to the
	// driver image and flips the sidecar into self-build mode.
	scheme := driverTestScheme(t)
	const rhcosVersion = "413.92.202304252344-0"
	node := newRHCOSNode("rhcos-node", "driver-a", rhcosVersion)
	driverState := newTestStateDriver(t, newDriverFakeClient(scheme, node), scheme)

	catalog := driverInfoCatalog(fakeClusterInfo{
		openshiftVersion: testOpenshiftVersion,
		// Nonempty so DTK stays enabled, but with no entry for this node pool.
		dtkImages: map[string]string{"999.99.99-0": "quay.io/dtk@sha256:other"},
	})

	objects, err := driverState.getManifestObjects(context.Background(), newDriverCR("driver-a"), catalog)
	require.NoError(t, err)

	daemonSet, err := getDaemonsetFromObjects(objects)
	require.NoError(t, err)
	dtkContainer := containerByName(t, daemonSet, "openshift-driver-toolkit-ctr")
	driverContainer := containerByName(t, daemonSet, "nvidia-driver-ctr")
	assert.NotEmpty(t, dtkContainer.Image, "missing DTK image must not render an empty container image")
	assert.Equal(t, driverContainer.Image, dtkContainer.Image)

	assert.Equal(t, "true", daemonSet.Labels[dtkImageMissingLabel])
	assert.Equal(t, "true", daemonSet.Spec.Template.Labels[dtkImageMissingLabel])
	for _, container := range []corev1.Container{driverContainer, dtkContainer} {
		imageMissingEnv, ok := envValue(container, "RHCOS_IMAGE_MISSING")
		assert.True(t, ok, "%s missing RHCOS_IMAGE_MISSING env", container.Name)
		assert.Equal(t, "true", imageMissingEnv)
		rhcosVersionEnv, ok := envValue(container, "RHCOS_VERSION")
		assert.True(t, ok, "%s missing RHCOS_VERSION env", container.Name)
		assert.Equal(t, rhcosVersion, rhcosVersionEnv)
	}
}

func TestGetManifestObjectsMultipleNodePools(t *testing.T) {
	scheme := driverTestScheme(t)

	t.Run("distinct OS versions render one DaemonSet each", func(t *testing.T) {
		fakeClient := newDriverFakeClient(scheme,
			newGPUNode("u22", "driver-a"),
			newGPUNodeOS("u20", "driver-a", "ubuntu", "20.04"),
		)
		driverState := newTestStateDriver(t, fakeClient, scheme)

		objects, err := driverState.getManifestObjects(context.Background(), newDriverCR("driver-a"), driverInfoCatalog(fakeClusterInfo{}))
		require.NoError(t, err)

		// Compare as a set: the render loop appends pools in nondeterministic order.
		renderedDaemonSets := daemonSetsByName(t, objects)
		require.Len(t, renderedDaemonSets, 2)
		ubuntu22DaemonSet := requireDaemonSetWithPrefix(t, renderedDaemonSets, "nvidia-gpu-driver-ubuntu22.04-")
		ubuntu20DaemonSet := requireDaemonSetWithPrefix(t, renderedDaemonSets, "nvidia-gpu-driver-ubuntu20.04-")

		assert.Equal(t, "22.04", ubuntu22DaemonSet.Spec.Template.Spec.NodeSelector[nfdOSVersionIDLabelKey])
		assert.Equal(t, "20.04", ubuntu20DaemonSet.Spec.Template.Spec.NodeSelector[nfdOSVersionIDLabelKey])
		assert.Equal(t, "nvcr.io/nvidia/driver:535.104.05-ubuntu22.04", containerByName(t, ubuntu22DaemonSet, "nvidia-driver-ctr").Image)
		assert.Equal(t, "nvcr.io/nvidia/driver:535.104.05-ubuntu20.04", containerByName(t, ubuntu20DaemonSet, "nvidia-driver-ctr").Image)
	})

	t.Run("two nodes in the same pool render a single DaemonSet", func(t *testing.T) {
		fakeClient := newDriverFakeClient(scheme,
			newGPUNode("u22-a", "driver-a"),
			newGPUNode("u22-b", "driver-a"),
		)
		driverState := newTestStateDriver(t, fakeClient, scheme)

		objects, err := driverState.getManifestObjects(context.Background(), newDriverCR("driver-a"), driverInfoCatalog(fakeClusterInfo{}))
		require.NoError(t, err)
		assert.Len(t, daemonSetsByName(t, objects), 1)
	})
}

func TestGetManifestObjectsAdditionalConfigsErrorIsLogged(t *testing.T) {
	scheme := driverTestScheme(t)
	fakeClient := newDriverFakeClient(scheme, newGPUNode("gpu-node", "driver-a"))
	driverState := newTestStateDriver(t, fakeClient, scheme)

	driverCR := newDriverCR("driver-a")
	driverCR.Spec.RepoConfig = &nvidiav1alpha1.DriverRepoConfigSpec{Name: "missing-repo-config"}

	var logs strings.Builder
	logger := funcr.New(func(_, args string) { logs.WriteString(args + "\n") }, funcr.Options{})
	ctx := log.IntoContext(context.Background(), logger)

	objects, err := driverState.getManifestObjects(ctx, driverCR, driverInfoCatalog(fakeClusterInfo{}))
	require.NoError(t, err)
	require.NotEmpty(t, objects)
	assert.Contains(t, logs.String(), "error rendering addition driver volume")
	assert.Contains(t, logs.String(), "missing-repo-config")

	// The driver is still deployed, silently without the repo config the user asked for.
	daemonSet, err := getDaemonsetFromObjects(objects)
	require.NoError(t, err)
	for _, volume := range daemonSet.Spec.Template.Spec.Volumes {
		assert.NotEqual(t, "missing-repo-config", volume.Name, "unresolved repo config must not be mounted")
	}
}

func TestGetManifestObjectsHandleDefaultImagesError(t *testing.T) {
	scheme := driverTestScheme(t)
	errDaemonSetGet := errors.New("injected daemonset get error")
	fakeClient := newDriverClientWithInterceptors(scheme,
		failingGetInterceptor[*appsv1.DaemonSet](errDaemonSetGet),
		newGPUNode("gpu-node", "driver-a"))
	driverState := newTestStateDriver(t, fakeClient, scheme)

	// Clearing the Manager fields makes rendering fall back to DRIVER_MANAGER_IMAGE,
	// which is the only path on which handleDefaultImagesInObjects Gets the
	// current DaemonSet.
	driverCR := newDriverCR("driver-a")
	driverCR.Spec.Manager.Repository = ""
	driverCR.Spec.Manager.Image = ""
	driverCR.Spec.Manager.Version = ""
	t.Setenv("DRIVER_MANAGER_IMAGE", "nvcr.io/nvidia/cloud-native/k8s-driver-manager:v0.6.2")

	_, err := driverState.getManifestObjects(context.Background(), driverCR, driverInfoCatalog(fakeClusterInfo{}))

	require.ErrorIs(t, err, errDaemonSetGet)
	require.ErrorContains(t, err, "failed to get current driver DaemonSet")
}

func TestSyncGetManifestObjectsError(t *testing.T) {
	driverState := newTestStateDriver(t, nil, driverTestScheme(t))

	syncState, err := driverState.Sync(context.Background(), newDriverCR("driver-a"), NewInfoCatalog())

	require.ErrorContains(t, err, "failed to create k8s objects from manifests")
	assert.Equal(t, SyncState(SyncStateNotReady), syncState)
}

func TestSyncCleanupError(t *testing.T) {
	scheme := driverTestScheme(t)
	errDaemonSetList := errors.New("injected daemonset list error")
	fakeClient := newDriverClientWithInterceptors(scheme,
		failingListInterceptor[*appsv1.DaemonSetList](errDaemonSetList),
		newGPUNode("gpu-node", "driver-a"))
	driverState := newTestStateDriver(t, fakeClient, scheme)

	syncState, err := driverState.Sync(context.Background(), newDriverCR("driver-a"), driverInfoCatalog(fakeClusterInfo{}))

	require.ErrorIs(t, err, errDaemonSetList)
	require.ErrorContains(t, err, "failed to cleanup stale driver DaemonSets")
	assert.Equal(t, SyncState(SyncStateNotReady), syncState)
}

func TestSyncCreateOrUpdateError(t *testing.T) {
	// Without NVIDIADriver in the scheme, SetControllerReference fails inside
	// createOrUpdateObjs.
	scheme := schemeWithoutNVIDIADriver(t)
	fakeClient := newDriverFakeClient(scheme, newGPUNode("gpu-node", "driver-a"))
	driverState := newTestStateDriver(t, fakeClient, scheme)

	syncState, err := driverState.Sync(context.Background(), newDriverCR("driver-a"), driverInfoCatalog(fakeClusterInfo{}))

	require.ErrorContains(t, err, "failed to create/update objects")
	assert.Equal(t, SyncState(SyncStateNotReady), syncState)
}

func TestSyncGetSyncStateError(t *testing.T) {
	scheme := driverTestScheme(t)
	// getSyncState reads back the applied objects as unstructured, so failing only
	// those Gets leaves the earlier Sync steps working.
	fakeClient := newDriverClientWithInterceptors(scheme,
		failingGetInterceptor[*unstructured.Unstructured](errors.New("injected get error")),
		newGPUNode("gpu-node", "driver-a"))
	driverState := newTestStateDriver(t, fakeClient, scheme)

	syncState, err := driverState.Sync(context.Background(), newDriverCR("driver-a"), driverInfoCatalog(fakeClusterInfo{}))

	require.ErrorContains(t, err, "failed to get sync state")
	assert.Equal(t, SyncState(SyncStateNotReady), syncState)
}

func TestCleanupStaleDeleteErrors(t *testing.T) {
	scheme := driverTestScheme(t)
	driverCR := &nvidiav1alpha1.NVIDIADriver{ObjectMeta: metav1.ObjectMeta{Name: "driver-a"}}

	t.Run("stale daemonset delete error", func(t *testing.T) {
		staleDaemonSet := newDaemonSet("ds-stale", "driver-a", 0, 0, nil)
		errDelete := errors.New("injected delete error")
		fakeClient := fake.NewClientBuilder().WithScheme(scheme).WithObjects(staleDaemonSet).
			WithIndex(&appsv1.DaemonSet{}, consts.NVIDIADriverControllerIndexKey, nvidiaDriverControllerIndex).
			WithInterceptorFuncs(failingDeleteInterceptor(errDelete)).Build()
		driverState := newTestStateDriver(t, fakeClient, scheme)

		// No desired objects, so the DaemonSet is stale and deletion is attempted.
		err := driverState.cleanupStaleDriverDaemonsets(context.Background(), driverCR, nil)

		require.ErrorIs(t, err, errDelete)
		require.ErrorContains(t, err, "error deleting DaemonSet")
	})

	t.Run("node list error", func(t *testing.T) {
		inactiveDaemonSet := newDaemonSet("ds-inactive", "driver-a", 0, 0, map[string]string{"pool": "gold"})
		errNodeList := errors.New("injected node list error")
		fakeClient := fake.NewClientBuilder().WithScheme(scheme).WithObjects(inactiveDaemonSet).
			WithIndex(&appsv1.DaemonSet{}, consts.NVIDIADriverControllerIndexKey, nvidiaDriverControllerIndex).
			WithInterceptorFuncs(failingListInterceptor[*corev1.NodeList](errNodeList)).Build()
		driverState := newTestStateDriver(t, fakeClient, scheme)
		desiredObjects := []*unstructured.Unstructured{newDaemonSetUnstructured("ds-inactive", "test-operator")}

		err := driverState.cleanupStaleDriverDaemonsets(context.Background(), driverCR, desiredObjects)

		require.ErrorIs(t, err, errNodeList)
		require.ErrorContains(t, err, "failed to list nodes")
	})

	t.Run("inactive daemonset delete error", func(t *testing.T) {
		inactiveDaemonSet := newDaemonSet("ds-inactive", "driver-a", 0, 0, map[string]string{"pool": "silver"})
		errDelete := errors.New("injected delete error")
		fakeClient := fake.NewClientBuilder().WithScheme(scheme).WithObjects(inactiveDaemonSet).
			WithIndex(&appsv1.DaemonSet{}, consts.NVIDIADriverControllerIndexKey, nvidiaDriverControllerIndex).
			WithInterceptorFuncs(failingDeleteInterceptor(errDelete)).Build()
		driverState := newTestStateDriver(t, fakeClient, scheme)
		desiredObjects := []*unstructured.Unstructured{newDaemonSetUnstructured("ds-inactive", "test-operator")}

		err := driverState.cleanupStaleDriverDaemonsets(context.Background(), driverCR, desiredObjects)

		require.ErrorIs(t, err, errDelete)
		require.ErrorContains(t, err, "error deleting DaemonSet")
	})
}

func TestHandleDefaultImagesNoDaemonSet(t *testing.T) {
	scheme := driverTestScheme(t)
	driverState := newTestStateDriver(t, newDriverFakeClient(scheme), scheme)

	driverCR := newDriverCR("driver-a")
	driverCR.Spec.Manager.Image = ""
	renderData := getMinimalDriverRenderData()
	objects := []*unstructured.Unstructured{newConfigMapUnstructured("cm", "test-operator")}

	_, err := driverState.handleDefaultImagesInObjects(context.Background(), objects, driverCR, *renderData)

	require.ErrorContains(t, err, "error getting DaemonSet from unstructured objects")
}

func TestHandleDefaultImagesCurrentImageMatches(t *testing.T) {
	scheme := driverTestScheme(t)
	renderData := getMinimalDriverRenderData()
	desiredObjects := renderDriverObjects(t, scheme, renderData)
	desiredDaemonSet, err := getDaemonsetFromObjects(desiredObjects)
	require.NoError(t, err)

	currentDaemonSet := newDaemonSetWithManagerImage(desiredDaemonSet.Name, desiredDaemonSet.Namespace, renderData.Driver.ManagerImagePath)
	driverState := newTestStateDriver(t, newDriverFakeClient(scheme, currentDaemonSet), scheme)

	driverCR := newDriverCR("driver-a")
	driverCR.Spec.Manager.Image = ""

	objectsWithDefaultImages, err := driverState.handleDefaultImagesInObjects(context.Background(), desiredObjects, driverCR, *renderData)

	require.NoError(t, err)
	assert.Equal(t, desiredObjects, objectsWithDefaultImages)
}

func TestHandleDefaultImagesCurrentGetError(t *testing.T) {
	scheme := driverTestScheme(t)
	errGet := errors.New("injected get error")
	fakeClient := newDriverClientWithInterceptors(scheme, failingGetInterceptor[client.Object](errGet))
	driverState := newTestStateDriver(t, fakeClient, scheme)

	driverCR := newDriverCR("driver-a")
	driverCR.Spec.Manager.Image = ""
	renderData := getMinimalDriverRenderData()
	objects := []*unstructured.Unstructured{newDaemonSetUnstructured("ds-a", "test-operator")}

	_, err := driverState.handleDefaultImagesInObjects(context.Background(), objects, driverCR, *renderData)

	require.ErrorIs(t, err, errGet)
	require.ErrorContains(t, err, "failed to get current driver DaemonSet")
}

func TestHandleDefaultImagesReRenderError(t *testing.T) {
	scheme := driverTestScheme(t)
	const daemonSetName = "nvidia-gpu-driver-ubuntu22.04"
	currentDaemonSet := newDaemonSetWithManagerImage(daemonSetName, "test-operator", "old-manager:1.0")
	driverState := newTestStateDriver(t, newDriverFakeClient(scheme, currentDaemonSet), scheme)

	driverCR := newDriverCR("driver-a")
	driverCR.Spec.Manager.Image = ""

	// The DaemonSet name and namespace match the seeded one so the current image is
	// found, but Driver.Spec is nil, so the re-render with that image fails.
	desiredObjects := []*unstructured.Unstructured{newDaemonSetUnstructured(daemonSetName, "test-operator")}
	renderData := &driverRenderData{
		Driver:  &driverSpec{ManagerImagePath: "new-manager:2.0", Spec: nil},
		Runtime: &driverRuntimeSpec{Namespace: "test-operator"},
	}

	_, err := driverState.handleDefaultImagesInObjects(context.Background(), desiredObjects, driverCR, *renderData)

	require.ErrorContains(t, err, "failed to render kubernetes manifests")
}

func TestHandleDefaultImagesReRenderSetRefError(t *testing.T) {
	scheme := schemeWithoutNVIDIADriver(t)
	renderData := getMinimalDriverRenderData()
	desiredObjects := renderDriverObjects(t, scheme, renderData)
	desiredDaemonSet, err := getDaemonsetFromObjects(desiredObjects)
	require.NoError(t, err)

	currentDaemonSet := newDaemonSetWithManagerImage(desiredDaemonSet.Name, desiredDaemonSet.Namespace, "old-manager:1.0")
	fakeClient := fake.NewClientBuilder().WithScheme(scheme).WithObjects(currentDaemonSet).Build()
	driverState := newTestStateDriver(t, fakeClient, scheme)

	driverCR := newDriverCR("driver-a")
	driverCR.Spec.Manager.Image = ""

	_, err = driverState.handleDefaultImagesInObjects(context.Background(), desiredObjects, driverCR, *renderData)

	require.ErrorContains(t, err, "failed to set controller reference")
}

func TestHandleDefaultImagesUnchangedSpecKeepsCurrentImage(t *testing.T) {
	scheme := driverTestScheme(t)
	driverCR := newDriverCR("driver-a")
	driverCR.Spec.Manager.Image = ""
	const currentManagerImage = "custom-manager:1.0"

	// Replicate the production hashing steps so the current DaemonSet carries the
	// hash handleDefaultImagesInObjects recomputes, making newHash == currentHash.
	hashDriverState := newTestStateDriver(t, nil, scheme)
	hashRenderData := getMinimalDriverRenderData()
	hashRenderData.Driver.ManagerImagePath = currentManagerImage
	hashObjects, err := hashDriverState.renderManifestObjects(context.Background(), hashRenderData)
	require.NoError(t, err)
	hashDaemonSetObject, err := getObjectOfKind(hashObjects, "DaemonSet")
	require.NoError(t, err)
	require.NoError(t, controllerutil.SetControllerReference(driverCR, hashDaemonSetObject, scheme))
	hashDriverState.addStateSpecificLabels(hashDaemonSetObject)
	unchangedSpecHash := utils.GetObjectHash(hashDaemonSetObject)

	// The desired objects use the default manager image path, which differs from
	// currentManagerImage and so triggers the re-render.
	renderData := getMinimalDriverRenderData()
	desiredObjects := renderDriverObjects(t, scheme, renderData)
	desiredDaemonSet, err := getDaemonsetFromObjects(desiredObjects)
	require.NoError(t, err)

	currentDaemonSet := newDaemonSetWithManagerImage(desiredDaemonSet.Name, desiredDaemonSet.Namespace, currentManagerImage)
	currentDaemonSet.Annotations = map[string]string{consts.NvidiaAnnotationHashKey: unchangedSpecHash}
	driverState := newTestStateDriver(t, newDriverFakeClient(scheme, currentDaemonSet), scheme)

	objectsWithDefaultImages, err := driverState.handleDefaultImagesInObjects(context.Background(), desiredObjects, driverCR, *renderData)

	require.NoError(t, err)
	gotDaemonSet, err := getDaemonsetFromObjects(objectsWithDefaultImages)
	require.NoError(t, err)
	assert.Equal(t, currentManagerImage, managerImageFromDaemonSet(gotDaemonSet))
}

func TestBuildDriverInstallConfigAllFields(t *testing.T) {
	renderData := &driverRenderData{
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
			Enabled:      new(true),
			UseHostMOFED: new(true),
		},
		GDS: &gdsDriverSpec{
			ImagePath: "nvcr.io/nvidia/cloud-native/nvidia-fs:2.16.1",
			Spec:      &nvidiav1alpha1.GPUDirectStorageSpec{Enabled: new(true), Env: []nvidiav1alpha1.EnvVar{{Name: "G", Value: "1"}}},
		},
		GDRCopy: &gdrcopyDriverSpec{
			ImagePath: "nvcr.io/nvidia/cloud-native/gdrdrv:v2.4.1",
			Spec:      &nvidiav1alpha1.GDRCopySpec{Enabled: new(true), Env: []nvidiav1alpha1.EnvVar{{Name: "H", Value: "1"}}},
		},
		Runtime: &driverRuntimeSpec{
			Namespace:                     "test-operator",
			OpenshiftVersion:              testOpenshiftVersion,
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

	installConfig := buildDriverInstallConfig(renderData)
	require.NotNil(t, installConfig)

	expectedInstallConfig := driverconfig.DriverInstallState{
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
		OpenshiftVersion:       testOpenshiftVersion,
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

	diff := cmp.Diff(expectedInstallConfig, *installConfig, cmpopts.EquateEmpty())
	assert.Empty(t, diff, "unexpected driver install config (-expected +got):\n%s", diff)
}

func TestGetNodePoolsListError(t *testing.T) {
	scheme := driverTestScheme(t)
	errNodeList := errors.New("injected node list error")
	fakeClient := newDriverClientWithInterceptors(scheme, failingListInterceptor[client.ObjectList](errNodeList))

	driverCR := &nvidiav1alpha1.NVIDIADriver{ObjectMeta: metav1.ObjectMeta{Name: "driver-a"}}
	_, err := getNodePools(context.Background(), fakeClient, driverCR, false)

	require.ErrorIs(t, err, errNodeList)
}

// fakeManager implements only the ctrl.Manager methods GetWatchSources calls;
// the embedded nil interface panics on anything else.
type fakeManager struct {
	ctrl.Manager
	scheme *runtime.Scheme
	mapper meta.RESTMapper
}

func (f *fakeManager) GetCache() cache.Cache          { return nil }
func (f *fakeManager) GetScheme() *runtime.Scheme     { return f.scheme }
func (f *fakeManager) GetRESTMapper() meta.RESTMapper { return f.mapper }

func TestDriverGetWatchSources(t *testing.T) {
	scheme := driverTestScheme(t)

	mapper := meta.NewDefaultRESTMapper([]schema.GroupVersion{
		{Group: "nvidia.com", Version: "v1alpha1"},
	})
	mapper.Add(schema.GroupVersionKind{Group: "nvidia.com", Version: "v1alpha1", Kind: "NVIDIADriver"}, meta.RESTScopeRoot)

	driverState := newTestStateDriver(t, nil, scheme)

	sources := driverState.GetWatchSources(&fakeManager{scheme: scheme, mapper: mapper})

	require.Len(t, sources, 1)
	require.Contains(t, sources, "DaemonSet")
	assert.NotNil(t, sources["DaemonSet"])
}
