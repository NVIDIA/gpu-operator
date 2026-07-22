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
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/client/interceptor"

	nvidiav1alpha1 "github.com/NVIDIA/gpu-operator/api/nvidia/v1alpha1"
	"github.com/NVIDIA/gpu-operator/internal/consts"
)

func makeDaemonSet(name, owner string, desired, misscheduled int32, nodeSelector map[string]string) *appsv1.DaemonSet {
	return &appsv1.DaemonSet{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: "test-operator",
			Labels:    map[string]string{"owner": owner},
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
}

// daemonSetOwnerIndex builds a fake client that indexes DaemonSets by the
// "owner" label, matching the field selector used by cleanupStaleDriverDaemonsets.
func daemonSetOwnerIndexClient(sch *runtime.Scheme, objs ...client.Object) client.Client {
	return fake.NewClientBuilder().
		WithScheme(sch).
		WithObjects(objs...).
		WithIndex(&appsv1.DaemonSet{}, consts.NVIDIADriverControllerIndexKey, func(o client.Object) []string {
			return []string{o.GetLabels()["owner"]}
		}).
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
	// dsInactiveButNodes: in desired list, Desired=0, but selector matches a node -> kept.
	dsInactiveButNodes := makeDaemonSet("ds-inactive-nodes", "driver-a", 0, 0, map[string]string{"pool": "gold"})

	cl := daemonSetOwnerIndexClient(sch, matchingNode, dsDesired, dsStale, dsInactive, dsInactiveButNodes)
	state, err := NewStateDriver(cl, "test-operator", sch, manifestDir)
	require.NoError(t, err)
	sd := state.(*stateDriver)

	cr := &nvidiav1alpha1.NVIDIADriver{ObjectMeta: metav1.ObjectMeta{Name: "driver-a"}}

	desiredObjs := []*unstructured.Unstructured{
		newDaemonSetUnstructured("ds-desired", "test-operator"),
		newDaemonSetUnstructured("ds-inactive", "test-operator"),
		newDaemonSetUnstructured("ds-inactive-nodes", "test-operator"),
	}

	require.NoError(t, sd.cleanupStaleDriverDaemonsets(context.Background(), cr, desiredObjs))

	assertExists := func(name string, shouldExist bool) {
		daemonSet := &appsv1.DaemonSet{}
		err := cl.Get(context.Background(), types.NamespacedName{Name: name, Namespace: "test-operator"}, daemonSet)
		if shouldExist {
			assert.NoError(t, err, "expected %s to exist", name)
		} else {
			assert.Error(t, err, "expected %s to be deleted", name)
		}
	}

	assertExists("ds-desired", true)
	assertExists("ds-stale", false)
	assertExists("ds-inactive", false)
	assertExists("ds-inactive-nodes", true)
}

func TestCleanupStaleDriverDaemonsetsListError(t *testing.T) {
	sch := driverTestScheme(t)
	cl := fake.NewClientBuilder().WithScheme(sch).
		WithIndex(&appsv1.DaemonSet{}, consts.NVIDIADriverControllerIndexKey, func(o client.Object) []string {
			return []string{o.GetLabels()["owner"]}
		}).
		WithInterceptorFuncs(interceptor.Funcs{
			List: func(_ context.Context, _ client.WithWatch, list client.ObjectList, _ ...client.ListOption) error {
				if _, ok := list.(*appsv1.DaemonSetList); ok {
					return fmt.Errorf("injected list error")
				}
				return nil
			},
		}).Build()
	state, err := NewStateDriver(cl, "test-operator", sch, manifestDir)
	require.NoError(t, err)
	sd := state.(*stateDriver)

	cr := &nvidiav1alpha1.NVIDIADriver{ObjectMeta: metav1.ObjectMeta{Name: "driver-a"}}
	err = sd.cleanupStaleDriverDaemonsets(context.Background(), cr, nil)
	require.ErrorContains(t, err, "failed to list all NVIDIA driver DaemonSets")
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

	names := map[string]bool{}
	for _, vm := range configs.VolumeMounts {
		names[vm.Name] = true
	}
	assert.True(t, names["cert-config"], "expected cert-config volume mount")
	assert.True(t, names["kernel-config"], "expected kernel-config volume mount")
	assert.True(t, names["topology-config"], "expected topology-config volume mount")
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

	state, err := NewStateDriver(nil, "test-operator", sch, manifestDir)
	require.NoError(t, err)
	sd := state.(*stateDriver)

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

// managerImageFromDaemonSet returns the image of the k8s-driver-manager init container.
func managerImageFromDaemonSet(daemonSet *appsv1.DaemonSet) string {
	for _, c := range daemonSet.Spec.Template.Spec.InitContainers {
		if c.Name == "k8s-driver-manager" {
			return c.Image
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

	var found bool
	for _, v := range configs.Volumes {
		if v.Name == "licensing-config" {
			require.NotNil(t, v.ConfigMap)
			assert.Equal(t, "lic-config", v.ConfigMap.Name)
			found = true
		}
	}
	assert.True(t, found)
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

	var found bool
	for _, v := range configs.Volumes {
		if v.Name == "licensing-config" {
			require.NotNil(t, v.Secret)
			assert.Equal(t, "lic-secret", v.Secret.SecretName)
			found = true
		}
	}
	assert.True(t, found)
}
