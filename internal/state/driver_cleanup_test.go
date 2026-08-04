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
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/client/interceptor"

	nvidiav1alpha1 "github.com/NVIDIA/gpu-operator/api/nvidia/v1alpha1"
	"github.com/NVIDIA/gpu-operator/internal/consts"
)

// ownedByDriver sets a controller OwnerReference to an NVIDIADriver named owner,
// matching what the controller sets and what the production field index reads.
func ownedByDriver(ds *appsv1.DaemonSet, owner string) {
	ds.OwnerReferences = []metav1.OwnerReference{{
		APIVersion: nvidiav1alpha1.SchemeGroupVersion.String(),
		Kind:       nvidiav1alpha1.NVIDIADriverCRDName,
		Name:       owner,
		UID:        types.UID("uid-" + owner),
		Controller: ptr.To(true),
	}}
}

func makeDaemonSet(name, owner string, desired, misscheduled int32, nodeSelector map[string]string) *appsv1.DaemonSet {
	ds := &appsv1.DaemonSet{
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
			DesiredNumberScheduled: desired,
			NumberMisscheduled:     misscheduled,
		},
	}
	if owner != "" {
		ownedByDriver(ds, owner)
	}
	return ds
}

// nvidiaDriverControllerIndex reproduces the production field index
// (controllers/nvidiadriver_controller.go): index by controlling NVIDIADriver name.
func nvidiaDriverControllerIndex(o client.Object) []string {
	owner := metav1.GetControllerOf(o.(*appsv1.DaemonSet))
	if owner == nil ||
		owner.APIVersion != nvidiav1alpha1.SchemeGroupVersion.String() ||
		owner.Kind != nvidiav1alpha1.NVIDIADriverCRDName {
		return nil
	}
	return []string{owner.Name}
}

// daemonSetOwnerIndexClient builds a fake client that indexes DaemonSets by their
// controlling NVIDIADriver, matching the field selector used by cleanupStaleDriverDaemonsets.
func daemonSetOwnerIndexClient(sch *runtime.Scheme, objs ...client.Object) client.Client {
	return fake.NewClientBuilder().
		WithScheme(sch).
		WithObjects(objs...).
		WithIndex(&appsv1.DaemonSet{}, consts.NVIDIADriverControllerIndexKey, nvidiaDriverControllerIndex).
		Build()
}

func TestCleanupStaleDriverDaemonsets(t *testing.T) {
	sch := driverTestScheme(t)

	matchingNode := &corev1.Node{ObjectMeta: metav1.ObjectMeta{
		Name:   "match-node",
		Labels: map[string]string{"pool": "gold"},
	}}

	// dsDesired: in desired list and active (Desired>0) -> kept.
	dsDesired := makeDaemonSet("ds-desired", "driver-a", 1, 0, nil)
	// dsStale: NOT in desired list -> deleted.
	dsStale := makeDaemonSet("ds-stale", "driver-a", 0, 0, nil)
	// dsInactive: in desired list, Desired=0, selector matches no nodes -> deleted.
	dsInactive := makeDaemonSet("ds-inactive", "driver-a", 0, 0, map[string]string{"pool": "silver"})
	// dsMisscheduled: in desired list, Desired=0 but Misscheduled>0 with no matching
	// nodes -> kept (NumberMisscheduled==0 is a deletion prerequisite).
	dsMisscheduled := makeDaemonSet("ds-misscheduled", "driver-a", 0, 1, map[string]string{"pool": "silver"})
	// dsInactiveButNodes: in desired list, Desired=0, but selector matches a node -> kept.
	dsInactiveButNodes := makeDaemonSet("ds-inactive-nodes", "driver-a", 0, 0, map[string]string{"pool": "gold"})
	// dsDriverB: owned by a different NVIDIADriver -> not indexed for driver-a -> kept.
	dsDriverB := makeDaemonSet("ds-driver-b", "driver-b", 0, 0, nil)
	// dsUnowned: no controller reference -> not indexed -> kept.
	dsUnowned := makeDaemonSet("ds-unowned", "", 0, 0, nil)
	// dsWrongKind: controlled by driver-a's name but a non-NVIDIADriver kind -> not indexed -> kept.
	dsWrongKind := makeDaemonSet("ds-wrong-kind", "", 0, 0, nil)
	dsWrongKind.OwnerReferences = []metav1.OwnerReference{{
		APIVersion: "apps/v1", Kind: "Deployment", Name: "driver-a", Controller: ptr.To(true),
	}}

	cl := daemonSetOwnerIndexClient(sch, matchingNode,
		dsDesired, dsStale, dsInactive, dsMisscheduled, dsInactiveButNodes, dsDriverB, dsUnowned, dsWrongKind)
	sd := newTestStateDriver(t, cl, sch)

	cr := &nvidiav1alpha1.NVIDIADriver{ObjectMeta: metav1.ObjectMeta{Name: "driver-a"}}

	desiredObjs := []*unstructured.Unstructured{
		newDaemonSetUnstructured("ds-desired", "test-operator"),
		newDaemonSetUnstructured("ds-inactive", "test-operator"),
		newDaemonSetUnstructured("ds-misscheduled", "test-operator"),
		newDaemonSetUnstructured("ds-inactive-nodes", "test-operator"),
	}

	require.NoError(t, sd.cleanupStaleDriverDaemonsets(context.Background(), cr, desiredObjs))

	assertExists := func(name string, shouldExist bool) {
		daemonSet := &appsv1.DaemonSet{}
		err := cl.Get(context.Background(), types.NamespacedName{Name: name, Namespace: "test-operator"}, daemonSet)
		if shouldExist {
			require.NoError(t, err, "expected %s to exist", name)
		} else {
			require.True(t, apierrors.IsNotFound(err), "expected %s to be deleted (NotFound), got: %v", name, err)
		}
	}

	assertExists("ds-desired", true)
	assertExists("ds-stale", false)
	assertExists("ds-inactive", false)
	assertExists("ds-misscheduled", true)
	assertExists("ds-inactive-nodes", true)
	// Isolation: cleanup for driver-a must never touch DaemonSets it does not own.
	assertExists("ds-driver-b", true)
	assertExists("ds-unowned", true)
	assertExists("ds-wrong-kind", true)
}

func TestCleanupStaleDriverDaemonsetsListError(t *testing.T) {
	sch := driverTestScheme(t)
	errInjected := errors.New("injected list error")
	cl := fake.NewClientBuilder().WithScheme(sch).
		WithIndex(&appsv1.DaemonSet{}, consts.NVIDIADriverControllerIndexKey, nvidiaDriverControllerIndex).
		WithInterceptorFuncs(interceptor.Funcs{
			List: func(ctx context.Context, cl client.WithWatch, list client.ObjectList, opts ...client.ListOption) error {
				if _, ok := list.(*appsv1.DaemonSetList); ok {
					return errInjected
				}
				return cl.List(ctx, list, opts...)
			},
		}).Build()
	sd := newTestStateDriver(t, cl, sch)

	cr := &nvidiav1alpha1.NVIDIADriver{ObjectMeta: metav1.ObjectMeta{Name: "driver-a"}}
	err := sd.cleanupStaleDriverDaemonsets(context.Background(), cr, nil)
	require.ErrorIs(t, err, errInjected)
	require.ErrorContains(t, err, "failed to list all NVIDIA driver DaemonSets")
}

// A DaemonSet vanishing between List and Delete returns NotFound, which cleanup
// treats as success — covered for both the stale and inactive delete paths.
func TestCleanupStaleDriverDaemonsetsNotFoundOnDeleteIgnored(t *testing.T) {
	sch := driverTestScheme(t)
	cr := &nvidiav1alpha1.NVIDIADriver{ObjectMeta: metav1.ObjectMeta{Name: "driver-a"}}

	// run confirms cleanup actually attempts the delete (exactly once, on the right
	// object) and treats the resulting NotFound as success.
	run := func(t *testing.T, ds *appsv1.DaemonSet, desired []*unstructured.Unstructured) {
		t.Helper()
		deleteCalls := 0
		cl := fake.NewClientBuilder().WithScheme(sch).WithObjects(ds).
			WithIndex(&appsv1.DaemonSet{}, consts.NVIDIADriverControllerIndexKey, nvidiaDriverControllerIndex).
			WithInterceptorFuncs(interceptor.Funcs{
				Delete: func(_ context.Context, _ client.WithWatch, obj client.Object, _ ...client.DeleteOption) error {
					deleteCalls++
					assert.Equal(t, ds.Name, obj.GetName())
					assert.Equal(t, "test-operator", obj.GetNamespace())
					return apierrors.NewNotFound(schema.GroupResource{Group: "apps", Resource: "daemonsets"}, obj.GetName())
				},
			}).Build()
		sd := newTestStateDriver(t, cl, sch)
		require.NoError(t, sd.cleanupStaleDriverDaemonsets(context.Background(), cr, desired))
		assert.Equal(t, 1, deleteCalls, "expected exactly one delete attempt")
	}

	t.Run("stale daemonset", func(t *testing.T) {
		// Empty desired list -> ds-stale is not desired -> stale delete path.
		run(t, makeDaemonSet("ds-stale", "driver-a", 0, 0, nil), nil)
	})

	t.Run("inactive daemonset", func(t *testing.T) {
		// ds-inactive is desired, Desired=0, selector matches no nodes -> inactive delete path.
		ds := makeDaemonSet("ds-inactive", "driver-a", 0, 0, map[string]string{"pool": "silver"})
		desired := []*unstructured.Unstructured{newDaemonSetUnstructured("ds-inactive", "test-operator")}
		run(t, ds, desired)
	})
}

func TestGetDriverAdditionalConfigsCertAndKernelAndTopology(t *testing.T) {
	certCM := &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{Name: "cert-config", Namespace: "test-ns"},
		Data:       map[string]string{"ca.crt": "cert-data"},
	}
	kernelCM := &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{Name: "kernel-config", Namespace: "test-ns"},
		Data:       map[string]string{"module.conf": "options nvidia"},
	}

	cl := fake.NewClientBuilder().WithScheme(clientScheme(t)).WithObjects(certCM, kernelCM).Build()
	sd := &stateDriver{stateSkel: stateSkel{client: cl, namespace: "test-ns"}}

	cr := &nvidiav1alpha1.NVIDIADriver{
		Spec: nvidiav1alpha1.NVIDIADriverSpec{
			CertConfig:         &nvidiav1alpha1.DriverCertConfigSpec{Name: "cert-config"},
			KernelModuleConfig: &nvidiav1alpha1.KernelModuleConfigSpec{Name: "kernel-config"},
			VirtualTopologyConfig: &nvidiav1alpha1.VirtualTopologyConfigSpec{
				Name: "topology-config",
			},
		},
	}

	configs, err := sd.getDriverAdditionalConfigs(
		context.Background(),
		cr,
		fakeClusterInfo{runtime: consts.Containerd},
		nodePool{osRelease: "rhel", osVersion: "9.4"},
	)
	require.NoError(t, err)

	mounts := map[string]corev1.VolumeMount{}
	for _, mount := range configs.VolumeMounts {
		mounts[mount.Name] = mount
	}
	volumes := map[string]corev1.Volume{}
	for _, volume := range configs.Volumes {
		volumes[volume.Name] = volume
	}

	certDir, err := getCertConfigPath("rhel")
	require.NoError(t, err)

	// Each ConfigMap-backed config must produce a read-only per-file mount at the
	// expected path plus a matching ConfigMap volume with a Key->Path item.
	assertConfigMapMount := func(name, mountDir, file string) {
		t.Helper()
		m, ok := mounts[name]
		require.True(t, ok, "%s mount missing", name)
		assert.Equal(t, filepath.Join(mountDir, file), m.MountPath)
		assert.Equal(t, file, m.SubPath)
		assert.True(t, m.ReadOnly)

		v, ok := volumes[name]
		require.True(t, ok, "%s volume missing", name)
		require.NotNil(t, v.ConfigMap)
		assert.Equal(t, name, v.ConfigMap.Name)
		assert.Contains(t, v.ConfigMap.Items, corev1.KeyToPath{Key: file, Path: file})
	}

	assertConfigMapMount("cert-config", certDir, "ca.crt")
	assertConfigMapMount("kernel-config", "/drivers", "module.conf")

	// Topology mounts a single well-known file, read-only, from its ConfigMap.
	topo, ok := mounts["topology-config"]
	require.True(t, ok, "topology-config mount missing")
	assert.Equal(t, consts.VGPUTopologyConfigMountPath, topo.MountPath)
	assert.Equal(t, consts.VGPUTopologyConfigFileName, topo.SubPath)
	assert.True(t, topo.ReadOnly)
	topoVol, ok := volumes["topology-config"]
	require.True(t, ok, "topology-config volume missing")
	require.NotNil(t, topoVol.ConfigMap)
	assert.Equal(t, "topology-config", topoVol.ConfigMap.Name)
}

func TestGetDriverAdditionalConfigsSLESSubscription(t *testing.T) {
	cl := fake.NewClientBuilder().WithScheme(clientScheme(t)).Build()
	sd := &stateDriver{stateSkel: stateSkel{client: cl, namespace: "test-ns"}}

	cr := &nvidiav1alpha1.NVIDIADriver{}

	configs, err := sd.getDriverAdditionalConfigs(
		context.Background(),
		cr,
		fakeClusterInfo{runtime: consts.Containerd},
		nodePool{osRelease: "sles", osVersion: "15.5"},
	)
	require.NoError(t, err)
	assert.True(t, hasSubscriptionVolumeMount(configs.VolumeMounts), "expected SLES subscription mounts")
}

func TestGetDriverAdditionalConfigsUnsupportedCertOS(t *testing.T) {
	cl := fake.NewClientBuilder().WithScheme(clientScheme(t)).Build()
	sd := &stateDriver{stateSkel: stateSkel{client: cl, namespace: "test-ns"}}

	cr := &nvidiav1alpha1.NVIDIADriver{
		Spec: nvidiav1alpha1.NVIDIADriverSpec{
			CertConfig: &nvidiav1alpha1.DriverCertConfigSpec{Name: "cert-config"},
		},
	}

	_, err := sd.getDriverAdditionalConfigs(
		context.Background(),
		cr,
		fakeClusterInfo{runtime: consts.Containerd},
		nodePool{osRelease: "unsupported-os", osVersion: "1.0"},
	)
	require.ErrorContains(t, err, "not supported")
}

func TestHandleDefaultImagesInObjectsReRender(t *testing.T) {
	sch := driverTestScheme(t)

	sd := newTestStateDriver(t, nil, sch)

	renderData := getMinimalDriverRenderData()
	renderData.Runtime.Namespace = "test-operator"

	desiredObjs, err := sd.renderManifestObjects(context.Background(), renderData)
	require.NoError(t, err)

	desiredDs, err := getDaemonsetFromObjects(desiredObjs)
	require.NoError(t, err)

	// Capture the manager image baked into the freshly-rendered (desired) DaemonSet.
	// The "spec changed" branch must return these desired objects unchanged.
	expectedManagerImage := managerImageFromDaemonSet(desiredDs)
	require.NotEmpty(t, expectedManagerImage)
	require.NotEqual(t, "old-manager-image:1.0", expectedManagerImage)

	// Seed a current DaemonSet with a *different* k8s-driver-manager image and a
	// stale hash annotation so the re-render path executes and detects a change.
	currentDs := &appsv1.DaemonSet{
		ObjectMeta: metav1.ObjectMeta{
			Name:        desiredDs.Name,
			Namespace:   desiredDs.Namespace,
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

	cl := fake.NewClientBuilder().WithScheme(sch).WithObjects(currentDs).Build()
	sd.client = cl

	cr := newDriverCR("driver-a")
	cr.Spec.Manager.Image = "" // force env-var / default-image handling

	got, err := sd.handleDefaultImagesInObjects(context.Background(), desiredObjs, cr, *renderData)
	require.NoError(t, err)
	require.NotEmpty(t, got)

	// The driver spec effectively changed (stale hash != freshly computed hash), so the
	// function must keep the desired objects, i.e. the NEW manager image, and must NOT
	// downgrade to the current DaemonSet's "old-manager-image:1.0".
	gotDs, err := getDaemonsetFromObjects(got)
	require.NoError(t, err)
	assert.Equal(t, expectedManagerImage, managerImageFromDaemonSet(gotDs))
	assert.NotEqual(t, "old-manager-image:1.0", managerImageFromDaemonSet(gotDs))
}

func managerImageFromDaemonSet(daemonSet *appsv1.DaemonSet) string {
	for _, container := range daemonSet.Spec.Template.Spec.InitContainers {
		if container.Name == "k8s-driver-manager" {
			return container.Image
		}
	}
	return ""
}

// clientScheme returns a scheme with core types registered for volume-config tests.
func clientScheme(t *testing.T) *runtime.Scheme {
	t.Helper()
	s := runtime.NewScheme()
	require.NoError(t, corev1.AddToScheme(s))
	return s
}

func TestGetDriverAdditionalConfigsRepoConfigUnsupportedOS(t *testing.T) {
	cl := fake.NewClientBuilder().WithScheme(clientScheme(t)).Build()
	sd := &stateDriver{stateSkel: stateSkel{client: cl, namespace: "test-ns"}}
	cr := &nvidiav1alpha1.NVIDIADriver{
		Spec: nvidiav1alpha1.NVIDIADriverSpec{
			RepoConfig: &nvidiav1alpha1.DriverRepoConfigSpec{Name: "repo-config"},
		},
	}
	_, err := sd.getDriverAdditionalConfigs(context.Background(), cr,
		fakeClusterInfo{runtime: consts.Containerd},
		nodePool{osRelease: "unsupported-os", osVersion: "1.0"})
	require.ErrorContains(t, err, "custom repo config")
}

func TestGetDriverAdditionalConfigsRepoConfigMissingConfigMap(t *testing.T) {
	cl := fake.NewClientBuilder().WithScheme(clientScheme(t)).Build()
	sd := &stateDriver{stateSkel: stateSkel{client: cl, namespace: "test-ns"}}
	cr := &nvidiav1alpha1.NVIDIADriver{
		Spec: nvidiav1alpha1.NVIDIADriverSpec{
			RepoConfig: &nvidiav1alpha1.DriverRepoConfigSpec{Name: "missing-repo"},
		},
	}
	_, err := sd.getDriverAdditionalConfigs(context.Background(), cr,
		fakeClusterInfo{runtime: consts.Containerd},
		nodePool{osRelease: "ubuntu", osVersion: "22.04"})
	require.ErrorContains(t, err, "custom repo config")
}

func TestGetDriverAdditionalConfigsCertConfigMissingConfigMap(t *testing.T) {
	cl := fake.NewClientBuilder().WithScheme(clientScheme(t)).Build()
	sd := &stateDriver{stateSkel: stateSkel{client: cl, namespace: "test-ns"}}
	cr := &nvidiav1alpha1.NVIDIADriver{
		Spec: nvidiav1alpha1.NVIDIADriverSpec{
			CertConfig: &nvidiav1alpha1.DriverCertConfigSpec{Name: "missing-cert"},
		},
	}
	_, err := sd.getDriverAdditionalConfigs(context.Background(), cr,
		fakeClusterInfo{runtime: consts.Containerd},
		nodePool{osRelease: "ubuntu", osVersion: "22.04"})
	require.ErrorContains(t, err, "custom certs")
}

func TestGetDriverAdditionalConfigsKernelModuleMissingConfigMap(t *testing.T) {
	cl := fake.NewClientBuilder().WithScheme(clientScheme(t)).Build()
	sd := &stateDriver{stateSkel: stateSkel{client: cl, namespace: "test-ns"}}
	cr := &nvidiav1alpha1.NVIDIADriver{
		Spec: nvidiav1alpha1.NVIDIADriverSpec{
			KernelModuleConfig: &nvidiav1alpha1.KernelModuleConfigSpec{Name: "missing-kmod"},
		},
	}
	_, err := sd.getDriverAdditionalConfigs(context.Background(), cr,
		fakeClusterInfo{runtime: consts.Containerd},
		nodePool{osRelease: "ubuntu", osVersion: "22.04"})
	require.ErrorContains(t, err, "kernel module configuration")
}

func TestGetDriverAdditionalConfigsRuntimeError(t *testing.T) {
	cl := fake.NewClientBuilder().WithScheme(clientScheme(t)).Build()
	sd := &stateDriver{stateSkel: stateSkel{client: cl, namespace: "test-ns"}}
	cr := &nvidiav1alpha1.NVIDIADriver{}
	_, err := sd.getDriverAdditionalConfigs(context.Background(), cr,
		fakeClusterInfo{runtimeErr: fmt.Errorf("runtime boom")},
		nodePool{osRelease: "ubuntu", osVersion: "22.04"})
	require.ErrorContains(t, err, "retrieve container runtime")
}

func TestGetDriverAdditionalConfigsOpenshiftVersionError(t *testing.T) {
	cl := fake.NewClientBuilder().WithScheme(clientScheme(t)).Build()
	sd := &stateDriver{stateSkel: stateSkel{client: cl, namespace: "test-ns"}}
	cr := &nvidiav1alpha1.NVIDIADriver{}
	_, err := sd.getDriverAdditionalConfigs(context.Background(), cr,
		fakeClusterInfo{runtime: consts.Containerd, openshiftVersionErr: fmt.Errorf("ocp boom")},
		nodePool{osRelease: "ubuntu", osVersion: "22.04"})
	require.ErrorContains(t, err, "introspecting cluster")
}

func volumeByName(t *testing.T, volumes []corev1.Volume, name string) corev1.Volume {
	t.Helper()
	for _, volume := range volumes {
		if volume.Name == name {
			return volume
		}
	}
	t.Fatalf("volume %q not found", name)
	return corev1.Volume{}
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
	cl := fake.NewClientBuilder().WithScheme(clientScheme(t)).Build()
	sd := &stateDriver{stateSkel: stateSkel{client: cl, namespace: "test-ns"}}
	cr := &nvidiav1alpha1.NVIDIADriver{
		Spec: nvidiav1alpha1.NVIDIADriverSpec{
			// Name set, no SecretName, NLSEnabled defaults to true.
			LicensingConfig: &nvidiav1alpha1.DriverLicensingConfigSpec{Name: "lic-config"},
		},
	}
	configs, err := sd.getDriverAdditionalConfigs(context.Background(), cr,
		fakeClusterInfo{runtime: consts.Containerd},
		nodePool{osRelease: "ubuntu", osVersion: "22.04"})
	require.NoError(t, err)

	vol := volumeByName(t, configs.Volumes, "licensing-config")
	require.NotNil(t, vol.ConfigMap, "NLS-enabled licensing should use a ConfigMap volume")
	assert.Equal(t, "lic-config", vol.ConfigMap.Name)
	assert.Nil(t, vol.Secret)

	mounts := licensingMountsByPath(configs.VolumeMounts)
	// gridd.conf is always mounted read-only at the licensing path...
	gridd, ok := mounts[consts.VGPULicensingConfigMountPath]
	require.True(t, ok, "gridd.conf licensing mount missing")
	assert.Equal(t, consts.VGPULicensingFileName, gridd.SubPath)
	assert.True(t, gridd.ReadOnly)
	// ...and with NLS enabled the client token is mounted too.
	token, ok := mounts[consts.NLSClientTokenMountPath]
	require.True(t, ok, "NLS client-token mount missing when NLS is enabled")
	assert.Equal(t, consts.NLSClientTokenFileName, token.SubPath)
	assert.True(t, token.ReadOnly)
}

func TestGetDriverAdditionalConfigsLicensingSecretNoNLS(t *testing.T) {
	cl := fake.NewClientBuilder().WithScheme(clientScheme(t)).Build()
	sd := &stateDriver{stateSkel: stateSkel{client: cl, namespace: "test-ns"}}
	cr := &nvidiav1alpha1.NVIDIADriver{
		Spec: nvidiav1alpha1.NVIDIADriverSpec{
			LicensingConfig: &nvidiav1alpha1.DriverLicensingConfigSpec{
				SecretName: "lic-secret",
				NLSEnabled: ptr.To(false),
			},
		},
	}
	configs, err := sd.getDriverAdditionalConfigs(context.Background(), cr,
		fakeClusterInfo{runtime: consts.Containerd},
		nodePool{osRelease: "ubuntu", osVersion: "22.04"})
	require.NoError(t, err)

	vol := volumeByName(t, configs.Volumes, "licensing-config")
	require.NotNil(t, vol.Secret, "licensing should use a Secret volume")
	assert.Equal(t, "lic-secret", vol.Secret.SecretName)
	assert.Nil(t, vol.ConfigMap)

	mounts := licensingMountsByPath(configs.VolumeMounts)
	gridd, ok := mounts[consts.VGPULicensingConfigMountPath]
	require.True(t, ok, "gridd.conf licensing mount missing")
	assert.Equal(t, consts.VGPULicensingFileName, gridd.SubPath)
	assert.True(t, gridd.ReadOnly)
	// NLS disabled -> no client-token mount.
	_, hasToken := mounts[consts.NLSClientTokenMountPath]
	assert.False(t, hasToken, "NLS token must not be mounted when NLS is disabled")
}
