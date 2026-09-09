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
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/client/interceptor"

	nvidiav1alpha1 "github.com/NVIDIA/gpu-operator/api/nvidia/v1alpha1"
	"github.com/NVIDIA/gpu-operator/internal/consts"
)

// setDriverOwnerReference sets a controller OwnerReference with the exact APIVersion and Kind
// that nvidiaDriverControllerIndex requires; anything else is not indexed.
func setDriverOwnerReference(daemonSet *appsv1.DaemonSet, ownerDriverName string) {
	daemonSet.OwnerReferences = []metav1.OwnerReference{{
		APIVersion: nvidiav1alpha1.SchemeGroupVersion.String(),
		Kind:       nvidiav1alpha1.NVIDIADriverCRDName,
		Name:       ownerDriverName,
		UID:        types.UID("uid-" + ownerDriverName),
		Controller: new(true),
	}}
}

func newDaemonSet(name, ownerDriverName string, desiredNumberScheduled, numberMisscheduled int32, nodeSelector map[string]string) *appsv1.DaemonSet {
	daemonSet := &appsv1.DaemonSet{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: "test-operator",
		},
		Spec: appsv1.DaemonSetSpec{
			Template: corev1.PodTemplateSpec{
				Spec: corev1.PodSpec{NodeSelector: nodeSelector},
			},
		},
		Status: appsv1.DaemonSetStatus{
			DesiredNumberScheduled: desiredNumberScheduled,
			NumberMisscheduled:     numberMisscheduled,
		},
	}
	if ownerDriverName != "" {
		setDriverOwnerReference(daemonSet, ownerDriverName)
	}
	return daemonSet
}

// nvidiaDriverControllerIndex reproduces the field index registered in
// controllers/nvidiadriver_controller.go, which cleanupStaleDriverDaemonsets lists against.
func nvidiaDriverControllerIndex(object client.Object) []string {
	controllerRef := metav1.GetControllerOf(object.(*appsv1.DaemonSet))
	if controllerRef == nil ||
		controllerRef.APIVersion != nvidiav1alpha1.SchemeGroupVersion.String() ||
		controllerRef.Kind != nvidiav1alpha1.NVIDIADriverCRDName {
		return nil
	}
	return []string{controllerRef.Name}
}

// newDaemonSetOwnerIndexClient registers the DaemonSet field index; without it the fake
// client rejects the MatchingFields list that cleanupStaleDriverDaemonsets issues.
func newDaemonSetOwnerIndexClient(scheme *runtime.Scheme, objects ...client.Object) client.Client {
	return fake.NewClientBuilder().
		WithScheme(scheme).
		WithObjects(objects...).
		WithIndex(&appsv1.DaemonSet{}, consts.NVIDIADriverControllerIndexKey, nvidiaDriverControllerIndex).
		Build()
}

func TestCleanupStaleDriverDaemonsets(t *testing.T) {
	scheme := driverTestScheme(t)

	goldPoolNode := &corev1.Node{ObjectMeta: metav1.ObjectMeta{
		Name:   "gold-pool-node",
		Labels: map[string]string{"pool": "gold"},
	}}

	deploymentOwnedDaemonSet := newDaemonSet("ds-wrong-kind", "", 0, 0, nil)
	deploymentOwnedDaemonSet.OwnerReferences = []metav1.OwnerReference{{
		APIVersion: "apps/v1", Kind: "Deployment", Name: "driver-a", Controller: new(true),
	}}

	testCases := []struct {
		name              string
		daemonSet         *appsv1.DaemonSet
		inDesiredManifest bool
		expectDeletion    bool
	}{
		{
			name:              "desired with scheduled pods survives",
			daemonSet:         newDaemonSet("ds-desired", "driver-a", 1, 0, nil),
			inDesiredManifest: true,
		},
		{
			name:           "absent from the desired manifest is deleted",
			daemonSet:      newDaemonSet("ds-stale", "driver-a", 0, 0, nil),
			expectDeletion: true,
		},
		{
			name:              "desired but scheduled nowhere and matching no node is deleted",
			daemonSet:         newDaemonSet("ds-inactive", "driver-a", 0, 0, map[string]string{"pool": "silver"}),
			inDesiredManifest: true,
			expectDeletion:    true,
		},
		{
			name:              "misscheduled pods keep an otherwise inactive DaemonSet",
			daemonSet:         newDaemonSet("ds-misscheduled", "driver-a", 0, 1, map[string]string{"pool": "silver"}),
			inDesiredManifest: true,
		},
		{
			name:              "a nodeSelector still matching a node keeps an inactive DaemonSet",
			daemonSet:         newDaemonSet("ds-inactive-nodes", "driver-a", 0, 0, map[string]string{"pool": "gold"}),
			inDesiredManifest: true,
		},
		{
			name:      "owned by a different NVIDIADriver is untouched",
			daemonSet: newDaemonSet("ds-driver-b", "driver-b", 0, 0, nil),
		},
		{
			name:      "without a controller reference is untouched",
			daemonSet: newDaemonSet("ds-unowned", "", 0, 0, nil),
		},
		{
			name:      "controlled by a non-NVIDIADriver kind is untouched",
			daemonSet: deploymentOwnedDaemonSet,
		},
	}

	existingObjects := []client.Object{goldPoolNode}
	var desiredObjects []*unstructured.Unstructured
	for _, testCase := range testCases {
		existingObjects = append(existingObjects, testCase.daemonSet)
		if testCase.inDesiredManifest {
			desiredObjects = append(desiredObjects, newDaemonSetUnstructured(testCase.daemonSet.Name, testCase.daemonSet.Namespace))
		}
	}

	fakeClient := newDaemonSetOwnerIndexClient(scheme, existingObjects...)
	driverState := newTestStateDriver(t, fakeClient, scheme)
	driverCR := &nvidiav1alpha1.NVIDIADriver{ObjectMeta: metav1.ObjectMeta{Name: "driver-a"}}

	require.NoError(t, driverState.cleanupStaleDriverDaemonsets(context.Background(), driverCR, desiredObjects))

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			err := fakeClient.Get(context.Background(), client.ObjectKeyFromObject(testCase.daemonSet), &appsv1.DaemonSet{})
			if testCase.expectDeletion {
				require.True(t, apierrors.IsNotFound(err), "expected NotFound, got: %v", err)
				return
			}
			require.NoError(t, err)
		})
	}
}

func TestCleanupStaleDriverDaemonsetsListError(t *testing.T) {
	scheme := driverTestScheme(t)
	errDaemonSetList := errors.New("injected list error")
	fakeClient := fake.NewClientBuilder().WithScheme(scheme).
		WithIndex(&appsv1.DaemonSet{}, consts.NVIDIADriverControllerIndexKey, nvidiaDriverControllerIndex).
		WithInterceptorFuncs(interceptor.Funcs{
			List: func(ctx context.Context, wrappedClient client.WithWatch, list client.ObjectList, opts ...client.ListOption) error {
				if _, ok := list.(*appsv1.DaemonSetList); ok {
					return errDaemonSetList
				}
				return wrappedClient.List(ctx, list, opts...)
			},
		}).Build()
	driverState := newTestStateDriver(t, fakeClient, scheme)

	driverCR := &nvidiav1alpha1.NVIDIADriver{ObjectMeta: metav1.ObjectMeta{Name: "driver-a"}}
	err := driverState.cleanupStaleDriverDaemonsets(context.Background(), driverCR, nil)
	require.ErrorIs(t, err, errDaemonSetList)
	require.ErrorContains(t, err, "failed to list all NVIDIA driver DaemonSets")
}

// A DaemonSet deleted between the List and the Delete surfaces as NotFound, which
// cleanupStaleDriverDaemonsets treats as success rather than an error.
func TestCleanupStaleDriverDaemonsetsNotFoundOnDeleteIgnored(t *testing.T) {
	scheme := driverTestScheme(t)
	driverCR := &nvidiav1alpha1.NVIDIADriver{ObjectMeta: metav1.ObjectMeta{Name: "driver-a"}}

	assertDeleteNotFoundIgnored := func(t *testing.T, daemonSet *appsv1.DaemonSet, desiredObjects []*unstructured.Unstructured) {
		t.Helper()
		deleteCalls := 0
		fakeClient := fake.NewClientBuilder().WithScheme(scheme).WithObjects(daemonSet).
			WithIndex(&appsv1.DaemonSet{}, consts.NVIDIADriverControllerIndexKey, nvidiaDriverControllerIndex).
			WithInterceptorFuncs(interceptor.Funcs{
				Delete: func(_ context.Context, _ client.WithWatch, object client.Object, _ ...client.DeleteOption) error {
					deleteCalls++
					assert.Equal(t, daemonSet.Name, object.GetName())
					assert.Equal(t, daemonSet.Namespace, object.GetNamespace())
					return apierrors.NewNotFound(schema.GroupResource{Group: "apps", Resource: "daemonsets"}, object.GetName())
				},
			}).Build()
		driverState := newTestStateDriver(t, fakeClient, scheme)
		require.NoError(t, driverState.cleanupStaleDriverDaemonsets(context.Background(), driverCR, desiredObjects))
		assert.Equal(t, 1, deleteCalls, "expected exactly one delete attempt")
	}

	t.Run("absent from the desired manifest", func(t *testing.T) {
		assertDeleteNotFoundIgnored(t, newDaemonSet("ds-stale", "driver-a", 0, 0, nil), nil)
	})

	t.Run("desired but scheduled nowhere and matching no node", func(t *testing.T) {
		daemonSet := newDaemonSet("ds-inactive", "driver-a", 0, 0, map[string]string{"pool": "silver"})
		desiredObjects := []*unstructured.Unstructured{newDaemonSetUnstructured(daemonSet.Name, daemonSet.Namespace)}
		assertDeleteNotFoundIgnored(t, daemonSet, desiredObjects)
	})
}

func TestGetDriverAdditionalConfigsCertAndKernelAndTopology(t *testing.T) {
	certConfigMap := &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{Name: "cert-config", Namespace: "test-ns"},
		Data:       map[string]string{"ca.crt": "cert-data"},
	}
	kernelModuleConfigMap := &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{Name: "kernel-config", Namespace: "test-ns"},
		Data:       map[string]string{"module.conf": "options nvidia"},
	}

	fakeClient := fake.NewClientBuilder().WithScheme(driverTestScheme(t)).WithObjects(certConfigMap, kernelModuleConfigMap).Build()
	driverState := &stateDriver{stateSkel: stateSkel{client: fakeClient, namespace: "test-ns"}}

	driverCR := &nvidiav1alpha1.NVIDIADriver{
		Spec: nvidiav1alpha1.NVIDIADriverSpec{
			CertConfig:         &nvidiav1alpha1.DriverCertConfigSpec{Name: "cert-config"},
			KernelModuleConfig: &nvidiav1alpha1.KernelModuleConfigSpec{Name: "kernel-config"},
			VirtualTopologyConfig: &nvidiav1alpha1.VirtualTopologyConfigSpec{
				Name: "topology-config",
			},
		},
	}

	configs, err := driverState.getDriverAdditionalConfigs(
		context.Background(),
		driverCR,
		fakeClusterInfo{containerRuntime: consts.Containerd},
		nodePool{osRelease: "rhel", osVersion: "9.4"},
	)
	require.NoError(t, err)

	mountsByName := map[string]corev1.VolumeMount{}
	for _, mount := range configs.VolumeMounts {
		mountsByName[mount.Name] = mount
	}
	volumesByName := map[string]corev1.Volume{}
	for _, volume := range configs.Volumes {
		volumesByName[volume.Name] = volume
	}

	certConfigDir, err := getCertConfigPath("rhel")
	require.NoError(t, err)

	assertConfigMapMount := func(name, mountDir, file string) {
		t.Helper()
		mount, ok := mountsByName[name]
		require.True(t, ok, "%s mount missing", name)
		assert.Equal(t, filepath.Join(mountDir, file), mount.MountPath)
		assert.Equal(t, file, mount.SubPath)
		assert.True(t, mount.ReadOnly)

		volume, ok := volumesByName[name]
		require.True(t, ok, "%s volume missing", name)
		require.NotNil(t, volume.ConfigMap)
		assert.Equal(t, name, volume.ConfigMap.Name)
		assert.Contains(t, volume.ConfigMap.Items, corev1.KeyToPath{Key: file, Path: file})
	}

	assertConfigMapMount("cert-config", certConfigDir, "ca.crt")
	assertConfigMapMount("kernel-config", "/drivers", "module.conf")

	topologyMount, ok := mountsByName["topology-config"]
	require.True(t, ok, "topology-config mount missing")
	assert.Equal(t, consts.VGPUTopologyConfigMountPath, topologyMount.MountPath)
	assert.Equal(t, consts.VGPUTopologyConfigFileName, topologyMount.SubPath)
	assert.True(t, topologyMount.ReadOnly)
	topologyVolume, ok := volumesByName["topology-config"]
	require.True(t, ok, "topology-config volume missing")
	require.NotNil(t, topologyVolume.ConfigMap)
	assert.Equal(t, "topology-config", topologyVolume.ConfigMap.Name)
}

func TestGetDriverAdditionalConfigsSLESSubscription(t *testing.T) {
	fakeClient := fake.NewClientBuilder().WithScheme(driverTestScheme(t)).Build()
	driverState := &stateDriver{stateSkel: stateSkel{client: fakeClient, namespace: "test-ns"}}

	driverCR := &nvidiav1alpha1.NVIDIADriver{}

	configs, err := driverState.getDriverAdditionalConfigs(
		context.Background(),
		driverCR,
		fakeClusterInfo{containerRuntime: consts.Containerd},
		nodePool{osRelease: "sles", osVersion: "15.5"},
	)
	require.NoError(t, err)
	assert.True(t, hasSubscriptionVolumeMount(configs.VolumeMounts), "expected SLES subscription mounts")
}

func TestGetDriverAdditionalConfigsUnsupportedCertOS(t *testing.T) {
	fakeClient := fake.NewClientBuilder().WithScheme(driverTestScheme(t)).Build()
	driverState := &stateDriver{stateSkel: stateSkel{client: fakeClient, namespace: "test-ns"}}

	driverCR := &nvidiav1alpha1.NVIDIADriver{
		Spec: nvidiav1alpha1.NVIDIADriverSpec{
			CertConfig: &nvidiav1alpha1.DriverCertConfigSpec{Name: "cert-config"},
		},
	}

	_, err := driverState.getDriverAdditionalConfigs(
		context.Background(),
		driverCR,
		fakeClusterInfo{containerRuntime: consts.Containerd},
		nodePool{osRelease: "unsupported-os", osVersion: "1.0"},
	)
	require.ErrorContains(t, err, "not supported")
}

func TestHandleDefaultImagesInObjectsReRender(t *testing.T) {
	scheme := driverTestScheme(t)

	renderData := getMinimalDriverRenderData()

	renderingDriverState := newTestStateDriver(t, nil, scheme)
	desiredObjects, err := renderingDriverState.renderManifestObjects(context.Background(), renderData)
	require.NoError(t, err)

	desiredDaemonSet, err := getDaemonsetFromObjects(desiredObjects)
	require.NoError(t, err)

	expectedManagerImage := managerImageFromDaemonSet(desiredDaemonSet)
	require.NotEmpty(t, expectedManagerImage)
	require.NotEqual(t, "old-manager-image:1.0", expectedManagerImage)

	// The differing manager image forces the re-render, and the stale hash annotation
	// makes the re-rendered hash differ, which the production code reads as a spec change.
	currentDaemonSet := &appsv1.DaemonSet{
		ObjectMeta: metav1.ObjectMeta{
			Name:        desiredDaemonSet.Name,
			Namespace:   desiredDaemonSet.Namespace,
			Annotations: map[string]string{consts.NvidiaAnnotationHashKey: "stale-hash"},
		},
		Spec: appsv1.DaemonSetSpec{
			Template: corev1.PodTemplateSpec{
				Spec: corev1.PodSpec{
					InitContainers: []corev1.Container{
						{Name: "k8s-driver-manager", Image: "old-manager-image:1.0"},
					},
				},
			},
		},
	}

	fakeClient := fake.NewClientBuilder().WithScheme(scheme).WithObjects(currentDaemonSet).Build()
	driverState := newTestStateDriver(t, fakeClient, scheme)

	driverCR := newDriverCR("driver-a")
	// An empty Manager.Image is how the production code detects that the default
	// image came from the DRIVER_MANAGER_IMAGE env var.
	driverCR.Spec.Manager.Image = ""

	objectsWithDefaultImages, err := driverState.handleDefaultImagesInObjects(context.Background(), desiredObjects, driverCR, *renderData)
	require.NoError(t, err)
	require.NotEmpty(t, objectsWithDefaultImages)

	// On a spec change the desired objects win, so the manager image must not be
	// downgraded to the one the current DaemonSet still runs.
	gotDaemonSet, err := getDaemonsetFromObjects(objectsWithDefaultImages)
	require.NoError(t, err)
	assert.Equal(t, expectedManagerImage, managerImageFromDaemonSet(gotDaemonSet))
}

func managerImageFromDaemonSet(daemonSet *appsv1.DaemonSet) string {
	for _, container := range daemonSet.Spec.Template.Spec.InitContainers {
		if container.Name == "k8s-driver-manager" {
			return container.Image
		}
	}
	return ""
}

func TestGetDriverAdditionalConfigsRepoConfigUnsupportedOS(t *testing.T) {
	fakeClient := fake.NewClientBuilder().WithScheme(driverTestScheme(t)).Build()
	driverState := &stateDriver{stateSkel: stateSkel{client: fakeClient, namespace: "test-ns"}}
	driverCR := &nvidiav1alpha1.NVIDIADriver{
		Spec: nvidiav1alpha1.NVIDIADriverSpec{
			RepoConfig: &nvidiav1alpha1.DriverRepoConfigSpec{Name: "repo-config"},
		},
	}
	_, err := driverState.getDriverAdditionalConfigs(context.Background(), driverCR,
		fakeClusterInfo{containerRuntime: consts.Containerd},
		nodePool{osRelease: "unsupported-os", osVersion: "1.0"})
	require.ErrorContains(t, err, "custom repo config")
}

func TestGetDriverAdditionalConfigsRepoConfigMissingConfigMap(t *testing.T) {
	fakeClient := fake.NewClientBuilder().WithScheme(driverTestScheme(t)).Build()
	driverState := &stateDriver{stateSkel: stateSkel{client: fakeClient, namespace: "test-ns"}}
	driverCR := &nvidiav1alpha1.NVIDIADriver{
		Spec: nvidiav1alpha1.NVIDIADriverSpec{
			RepoConfig: &nvidiav1alpha1.DriverRepoConfigSpec{Name: "missing-repo"},
		},
	}
	_, err := driverState.getDriverAdditionalConfigs(context.Background(), driverCR,
		fakeClusterInfo{containerRuntime: consts.Containerd},
		nodePool{osRelease: "ubuntu", osVersion: "22.04"})
	require.ErrorContains(t, err, "custom repo config")
}

func TestGetDriverAdditionalConfigsCertConfigMissingConfigMap(t *testing.T) {
	fakeClient := fake.NewClientBuilder().WithScheme(driverTestScheme(t)).Build()
	driverState := &stateDriver{stateSkel: stateSkel{client: fakeClient, namespace: "test-ns"}}
	driverCR := &nvidiav1alpha1.NVIDIADriver{
		Spec: nvidiav1alpha1.NVIDIADriverSpec{
			CertConfig: &nvidiav1alpha1.DriverCertConfigSpec{Name: "missing-cert"},
		},
	}
	_, err := driverState.getDriverAdditionalConfigs(context.Background(), driverCR,
		fakeClusterInfo{containerRuntime: consts.Containerd},
		nodePool{osRelease: "ubuntu", osVersion: "22.04"})
	require.ErrorContains(t, err, "custom certs")
}

func TestGetDriverAdditionalConfigsKernelModuleMissingConfigMap(t *testing.T) {
	fakeClient := fake.NewClientBuilder().WithScheme(driverTestScheme(t)).Build()
	driverState := &stateDriver{stateSkel: stateSkel{client: fakeClient, namespace: "test-ns"}}
	driverCR := &nvidiav1alpha1.NVIDIADriver{
		Spec: nvidiav1alpha1.NVIDIADriverSpec{
			KernelModuleConfig: &nvidiav1alpha1.KernelModuleConfigSpec{Name: "missing-kmod"},
		},
	}
	_, err := driverState.getDriverAdditionalConfigs(context.Background(), driverCR,
		fakeClusterInfo{containerRuntime: consts.Containerd},
		nodePool{osRelease: "ubuntu", osVersion: "22.04"})
	require.ErrorContains(t, err, "kernel module configuration")
}

func TestGetDriverAdditionalConfigsRuntimeError(t *testing.T) {
	fakeClient := fake.NewClientBuilder().WithScheme(driverTestScheme(t)).Build()
	driverState := &stateDriver{stateSkel: stateSkel{client: fakeClient, namespace: "test-ns"}}
	driverCR := &nvidiav1alpha1.NVIDIADriver{}
	_, err := driverState.getDriverAdditionalConfigs(context.Background(), driverCR,
		fakeClusterInfo{containerRuntimeErr: fmt.Errorf("runtime boom")},
		nodePool{osRelease: "ubuntu", osVersion: "22.04"})
	require.ErrorContains(t, err, "retrieve container runtime")
}

func TestGetDriverAdditionalConfigsOpenshiftVersionError(t *testing.T) {
	fakeClient := fake.NewClientBuilder().WithScheme(driverTestScheme(t)).Build()
	driverState := &stateDriver{stateSkel: stateSkel{client: fakeClient, namespace: "test-ns"}}
	driverCR := &nvidiav1alpha1.NVIDIADriver{}
	_, err := driverState.getDriverAdditionalConfigs(context.Background(), driverCR,
		fakeClusterInfo{containerRuntime: consts.Containerd, openshiftVersionErr: fmt.Errorf("ocp boom")},
		nodePool{osRelease: "ubuntu", osVersion: "22.04"})
	require.ErrorContains(t, err, "introspecting cluster")
}

func volumeByName(t *testing.T, volumes []corev1.Volume, name string) corev1.Volume {
	t.Helper()
	volume := findVolumeByName(volumes, name)
	require.NotNil(t, volume, "volume %q not found", name)
	return *volume
}

func licensingMountsByPath(mounts []corev1.VolumeMount) map[string]corev1.VolumeMount {
	byPath := map[string]corev1.VolumeMount{}
	for _, mount := range mounts {
		if mount.Name == "licensing-config" {
			byPath[mount.MountPath] = mount
		}
	}
	return byPath
}

func TestGetDriverAdditionalConfigsLicensingConfigMap(t *testing.T) {
	fakeClient := fake.NewClientBuilder().WithScheme(driverTestScheme(t)).Build()
	driverState := &stateDriver{stateSkel: stateSkel{client: fakeClient, namespace: "test-ns"}}
	driverCR := &nvidiav1alpha1.NVIDIADriver{
		Spec: nvidiav1alpha1.NVIDIADriverSpec{
			// A nil NLSEnabled means NLS is enabled.
			LicensingConfig: &nvidiav1alpha1.DriverLicensingConfigSpec{Name: "lic-config"},
		},
	}
	configs, err := driverState.getDriverAdditionalConfigs(context.Background(), driverCR,
		fakeClusterInfo{containerRuntime: consts.Containerd},
		nodePool{osRelease: "ubuntu", osVersion: "22.04"})
	require.NoError(t, err)

	licensingVolume := volumeByName(t, configs.Volumes, "licensing-config")
	require.NotNil(t, licensingVolume.ConfigMap, "NLS-enabled licensing should use a ConfigMap volume")
	assert.Equal(t, "lic-config", licensingVolume.ConfigMap.Name)
	assert.Nil(t, licensingVolume.Secret)

	mountsByPath := licensingMountsByPath(configs.VolumeMounts)
	griddMount, ok := mountsByPath[consts.VGPULicensingConfigMountPath]
	require.True(t, ok, "gridd.conf licensing mount missing")
	assert.Equal(t, consts.VGPULicensingFileName, griddMount.SubPath)
	assert.True(t, griddMount.ReadOnly)
	tokenMount, ok := mountsByPath[consts.NLSClientTokenMountPath]
	require.True(t, ok, "NLS client-token mount missing when NLS is enabled")
	assert.Equal(t, consts.NLSClientTokenFileName, tokenMount.SubPath)
	assert.True(t, tokenMount.ReadOnly)
}

func TestGetDriverAdditionalConfigsLicensingSecretNoNLS(t *testing.T) {
	fakeClient := fake.NewClientBuilder().WithScheme(driverTestScheme(t)).Build()
	driverState := &stateDriver{stateSkel: stateSkel{client: fakeClient, namespace: "test-ns"}}
	driverCR := &nvidiav1alpha1.NVIDIADriver{
		Spec: nvidiav1alpha1.NVIDIADriverSpec{
			LicensingConfig: &nvidiav1alpha1.DriverLicensingConfigSpec{
				SecretName: "lic-secret",
				NLSEnabled: new(false),
			},
		},
	}
	configs, err := driverState.getDriverAdditionalConfigs(context.Background(), driverCR,
		fakeClusterInfo{containerRuntime: consts.Containerd},
		nodePool{osRelease: "ubuntu", osVersion: "22.04"})
	require.NoError(t, err)

	licensingVolume := volumeByName(t, configs.Volumes, "licensing-config")
	require.NotNil(t, licensingVolume.Secret, "licensing should use a Secret volume")
	assert.Equal(t, "lic-secret", licensingVolume.Secret.SecretName)
	assert.Nil(t, licensingVolume.ConfigMap)

	mountsByPath := licensingMountsByPath(configs.VolumeMounts)
	griddMount, ok := mountsByPath[consts.VGPULicensingConfigMountPath]
	require.True(t, ok, "gridd.conf licensing mount missing")
	assert.Equal(t, consts.VGPULicensingFileName, griddMount.SubPath)
	assert.True(t, griddMount.ReadOnly)
	_, hasToken := mountsByPath[consts.NLSClientTokenMountPath]
	assert.False(t, hasToken, "NLS token must not be mounted when NLS is disabled")
}
