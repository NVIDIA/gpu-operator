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

	"github.com/go-logr/logr"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/client/interceptor"

	nvidiav1alpha1 "github.com/NVIDIA/gpu-operator/api/nvidia/v1alpha1"
	"github.com/NVIDIA/gpu-operator/internal/consts"
	"github.com/NVIDIA/gpu-operator/internal/utils"
)

func newDeploymentUnstructured(name, ns string) *unstructured.Unstructured {
	obj := &unstructured.Unstructured{}
	obj.SetGroupVersionKind(schema.GroupVersionKind{Group: "apps", Version: "v1", Kind: "Deployment"})
	obj.SetName(name)
	obj.SetNamespace(ns)
	return obj
}

// skelTestScheme registers only the types the stateSkel fake clients use.
func skelTestScheme(t *testing.T) *runtime.Scheme {
	t.Helper()
	s := runtime.NewScheme()
	require.NoError(t, corev1.AddToScheme(s))
	require.NoError(t, appsv1.AddToScheme(s))
	return s
}

func newTestSkel(t *testing.T, cl client.Client) *stateSkel {
	t.Helper()
	return &stateSkel{
		name:        "test-state",
		description: "test description",
		namespace:   "test-ns",
		client:      cl,
		scheme:      skelTestScheme(t),
	}
}

func newConfigMapUnstructured(name, ns string) *unstructured.Unstructured {
	obj := &unstructured.Unstructured{}
	obj.SetGroupVersionKind(schema.GroupVersionKind{Group: "", Version: "v1", Kind: "ConfigMap"})
	obj.SetName(name)
	obj.SetNamespace(ns)
	_ = unstructured.SetNestedStringMap(obj.Object, map[string]string{"key": "value"}, "data")
	return obj
}

func newServiceAccountUnstructured(name, ns string) *unstructured.Unstructured {
	obj := &unstructured.Unstructured{}
	obj.SetGroupVersionKind(schema.GroupVersionKind{Group: "", Version: "v1", Kind: "ServiceAccount"})
	obj.SetName(name)
	obj.SetNamespace(ns)
	return obj
}

func newDaemonSetUnstructured(name, ns string) *unstructured.Unstructured {
	obj := &unstructured.Unstructured{}
	obj.SetGroupVersionKind(schema.GroupVersionKind{Group: "apps", Version: "v1", Kind: "DaemonSet"})
	obj.SetName(name)
	obj.SetNamespace(ns)
	return obj
}

func setDaemonSetStatus(obj *unstructured.Unstructured, desired, available, updated int64) {
	_ = unstructured.SetNestedField(obj.Object, desired, "status", "desiredNumberScheduled")
	_ = unstructured.SetNestedField(obj.Object, available, "status", "numberAvailable")
	_ = unstructured.SetNestedField(obj.Object, updated, "status", "updatedNumberScheduled")
}

func TestSkelNameAndDescription(t *testing.T) {
	s := newTestSkel(t, fake.NewClientBuilder().WithScheme(skelTestScheme(t)).Build())
	assert.Equal(t, "test-state", s.Name())
	assert.Equal(t, "test description", s.Description())
}

func TestGetObj(t *testing.T) {
	existing := newConfigMapUnstructured("cm-a", "test-ns")
	cl := fake.NewClientBuilder().WithScheme(skelTestScheme(t)).WithObjects(existing).Build()
	s := newTestSkel(t, cl)

	// Object exists.
	got := newConfigMapUnstructured("cm-a", "test-ns")
	require.NoError(t, s.getObj(context.Background(), got))

	// Object does not exist -> IsNotFound error is returned.
	missing := newConfigMapUnstructured("cm-missing", "test-ns")
	err := s.getObj(context.Background(), missing)
	require.True(t, apierrors.IsNotFound(err))
}

func TestCreateObj(t *testing.T) {
	cl := fake.NewClientBuilder().WithScheme(skelTestScheme(t)).Build()
	s := newTestSkel(t, cl)

	obj := newConfigMapUnstructured("cm-new", "test-ns")
	require.NoError(t, s.createObj(context.Background(), obj))

	// Creating the same object again returns an AlreadyExists error.
	err := s.createObj(context.Background(), obj)
	require.True(t, apierrors.IsAlreadyExists(err))
}

func TestCheckDeleteSupported(t *testing.T) {
	s := newTestSkel(t, fake.NewClientBuilder().WithScheme(skelTestScheme(t)).Build())

	// Supported GVK (ConfigMap) - no panic, returns cleanly.
	s.checkDeleteSupported(context.Background(), newConfigMapUnstructured("cm", "test-ns"))

	// Unsupported GVK - exercises the warning branch.
	unsupported := &unstructured.Unstructured{}
	unsupported.SetGroupVersionKind(schema.GroupVersionKind{Group: "custom.io", Version: "v1", Kind: "Widget"})
	unsupported.SetName("w")
	s.checkDeleteSupported(context.Background(), unsupported)
}

func TestUpdateObj(t *testing.T) {
	existing := newConfigMapUnstructured("cm-a", "test-ns")
	cl := fake.NewClientBuilder().WithScheme(skelTestScheme(t)).WithObjects(existing).Build()
	s := newTestSkel(t, cl)

	// Fetch the current object to obtain a valid resourceVersion, then update it.
	current := newConfigMapUnstructured("cm-a", "test-ns")
	require.NoError(t, s.getObj(context.Background(), current))
	require.NoError(t, unstructured.SetNestedStringMap(current.Object, map[string]string{"key": "updated"}, "data"))
	require.NoError(t, s.updateObj(context.Background(), current))

	// Update error path via interceptor.
	errClient := fake.NewClientBuilder().WithScheme(skelTestScheme(t)).WithObjects(existing).
		WithInterceptorFuncs(interceptor.Funcs{
			Update: func(_ context.Context, _ client.WithWatch, _ client.Object, _ ...client.UpdateOption) error {
				return fmt.Errorf("injected update error")
			},
		}).Build()
	errSkel := newTestSkel(t, errClient)
	err := errSkel.updateObj(context.Background(), newConfigMapUnstructured("cm-a", "test-ns"))
	require.ErrorContains(t, err, "failed to update resource")
}

func TestAddStateSpecificLabels(t *testing.T) {
	s := newTestSkel(t, fake.NewClientBuilder().WithScheme(skelTestScheme(t)).Build())
	obj := newConfigMapUnstructured("cm", "test-ns")
	s.addStateSpecificLabels(obj)
	assert.Equal(t, "test-state", obj.GetLabels()[consts.StateLabel])
}

func TestMergeObjectsResourceVersion(t *testing.T) {
	s := newTestSkel(t, fake.NewClientBuilder().WithScheme(skelTestScheme(t)).Build())

	updated := newConfigMapUnstructured("cm", "test-ns")
	current := newConfigMapUnstructured("cm", "test-ns")
	current.SetResourceVersion("1234")

	require.NoError(t, s.mergeObjects(updated, current))
	assert.Equal(t, "1234", updated.GetResourceVersion())
}

func TestMergeServiceAccount(t *testing.T) {
	s := newTestSkel(t, fake.NewClientBuilder().WithScheme(skelTestScheme(t)).Build())

	updated := newServiceAccountUnstructured("sa", "test-ns")
	current := newServiceAccountUnstructured("sa", "test-ns")
	current.SetResourceVersion("42")
	require.NoError(t, unstructured.SetNestedSlice(current.Object,
		[]interface{}{map[string]interface{}{"name": "sa-token"}}, "secrets"))
	require.NoError(t, unstructured.SetNestedSlice(current.Object,
		[]interface{}{map[string]interface{}{"name": "pull-secret"}}, "imagePullSecrets"))

	require.NoError(t, s.mergeObjects(updated, current))

	secrets, ok, err := unstructured.NestedSlice(updated.Object, "secrets")
	require.NoError(t, err)
	require.True(t, ok)
	assert.Len(t, secrets, 1)

	pullSecrets, ok, err := unstructured.NestedSlice(updated.Object, "imagePullSecrets")
	require.NoError(t, err)
	require.True(t, ok)
	assert.Len(t, pullSecrets, 1)
}

func TestCreateOrUpdateObjsCreatesNewObject(t *testing.T) {
	cl := fake.NewClientBuilder().WithScheme(skelTestScheme(t)).Build()
	s := newTestSkel(t, cl)

	obj := newDaemonSetUnstructured("ds-a", "test-ns")
	noop := func(_ *unstructured.Unstructured) error { return nil }

	require.NoError(t, s.createOrUpdateObjs(context.Background(), noop, []*unstructured.Unstructured{obj}))

	// The DaemonSet should now exist with a hash annotation and state label set.
	got := newDaemonSetUnstructured("ds-a", "test-ns")
	require.NoError(t, s.getObj(context.Background(), got))
	assert.NotEmpty(t, got.GetAnnotations()[consts.NvidiaAnnotationHashKey])
	assert.Equal(t, "test-state", got.GetLabels()[consts.StateLabel])
}

func TestCreateOrUpdateObjsUpdatesExistingObject(t *testing.T) {
	existing := newConfigMapUnstructured("cm-a", "test-ns")
	cl := fake.NewClientBuilder().WithScheme(skelTestScheme(t)).WithObjects(existing).Build()
	s := newTestSkel(t, cl)

	desired := newConfigMapUnstructured("cm-a", "test-ns")
	require.NoError(t, unstructured.SetNestedStringMap(desired.Object, map[string]string{"key": "new"}, "data"))
	noop := func(_ *unstructured.Unstructured) error { return nil }

	require.NoError(t, s.createOrUpdateObjs(context.Background(), noop, []*unstructured.Unstructured{desired}))

	got := newConfigMapUnstructured("cm-a", "test-ns")
	require.NoError(t, s.getObj(context.Background(), got))
	data, found, err := unstructured.NestedStringMap(got.Object, "data")
	require.NoError(t, err)
	require.True(t, found)
	assert.Equal(t, "new", data["key"])
}

func TestCreateOrUpdateObjsSkipsUnchangedDaemonSet(t *testing.T) {
	// Build the desired object exactly as createOrUpdateObjs would before hashing:
	// controller reference is a no-op here, state labels are applied, then the hash
	// is computed. Seed a current DaemonSet carrying that same hash so the update
	// is skipped.
	desired := newDaemonSetUnstructured("ds-a", "test-ns")
	s := newTestSkel(t, nil)
	s.addStateSpecificLabels(desired)
	hash := utils.GetObjectHash(desired)

	current := newDaemonSetUnstructured("ds-a", "test-ns")
	current.SetLabels(map[string]string{consts.StateLabel: "test-state"})
	current.SetAnnotations(map[string]string{consts.NvidiaAnnotationHashKey: hash})

	// Fail the sync if the client is asked to update anything: matching hashes must
	// short-circuit before updateObj is ever called.
	cl := fake.NewClientBuilder().WithScheme(skelTestScheme(t)).WithObjects(current).
		WithInterceptorFuncs(interceptor.Funcs{
			Update: func(_ context.Context, _ client.WithWatch, _ client.Object, _ ...client.UpdateOption) error {
				return fmt.Errorf("unexpected update: an unchanged object must not be updated")
			},
		}).Build()
	s.client = cl

	noop := func(_ *unstructured.Unstructured) error { return nil }
	require.NoError(t, s.createOrUpdateObjs(context.Background(), noop, []*unstructured.Unstructured{desired}))
}

func TestCreateOrUpdateObjsSetControllerReferenceError(t *testing.T) {
	cl := fake.NewClientBuilder().WithScheme(skelTestScheme(t)).Build()
	s := newTestSkel(t, cl)

	obj := newConfigMapUnstructured("cm-a", "test-ns")
	failRef := func(_ *unstructured.Unstructured) error { return fmt.Errorf("ref error") }

	err := s.createOrUpdateObjs(context.Background(), failRef, []*unstructured.Unstructured{obj})
	require.ErrorContains(t, err, "failed to set controller reference")
}

func TestCreateOrUpdateObjsCreateError(t *testing.T) {
	cl := fake.NewClientBuilder().WithScheme(skelTestScheme(t)).
		WithInterceptorFuncs(interceptor.Funcs{
			Create: func(_ context.Context, _ client.WithWatch, _ client.Object, _ ...client.CreateOption) error {
				return fmt.Errorf("injected create error")
			},
		}).Build()
	s := newTestSkel(t, cl)

	obj := newConfigMapUnstructured("cm-a", "test-ns")
	noop := func(_ *unstructured.Unstructured) error { return nil }
	err := s.createOrUpdateObjs(context.Background(), noop, []*unstructured.Unstructured{obj})
	require.ErrorContains(t, err, "injected create error")
}

func TestGetSyncState(t *testing.T) {
	t.Run("all objects ready", func(t *testing.T) {
		cm := newConfigMapUnstructured("cm-a", "test-ns")
		daemonSet := newDaemonSetUnstructured("ds-a", "test-ns")
		setDaemonSetStatus(daemonSet, 2, 2, 2)
		cl := fake.NewClientBuilder().WithScheme(skelTestScheme(t)).WithObjects(cm, daemonSet).Build()
		s := newTestSkel(t, cl)

		state, err := s.getSyncState(context.Background(),
			[]*unstructured.Unstructured{newConfigMapUnstructured("cm-a", "test-ns"), newDaemonSetUnstructured("ds-a", "test-ns")})
		require.NoError(t, err)
		assert.Equal(t, SyncState(SyncStateReady), state)
	})

	t.Run("object not found is not ready", func(t *testing.T) {
		cl := fake.NewClientBuilder().WithScheme(skelTestScheme(t)).Build()
		s := newTestSkel(t, cl)
		state, err := s.getSyncState(context.Background(),
			[]*unstructured.Unstructured{newConfigMapUnstructured("cm-missing", "test-ns")})
		require.NoError(t, err)
		assert.Equal(t, SyncState(SyncStateNotReady), state)
	})

	t.Run("daemonset not ready", func(t *testing.T) {
		daemonSet := newDaemonSetUnstructured("ds-a", "test-ns")
		setDaemonSetStatus(daemonSet, 3, 1, 1)
		cl := fake.NewClientBuilder().WithScheme(skelTestScheme(t)).WithObjects(daemonSet).Build()
		s := newTestSkel(t, cl)
		state, err := s.getSyncState(context.Background(),
			[]*unstructured.Unstructured{newDaemonSetUnstructured("ds-a", "test-ns")})
		require.NoError(t, err)
		assert.Equal(t, SyncState(SyncStateNotReady), state)
	})

	t.Run("get error propagates", func(t *testing.T) {
		cl := fake.NewClientBuilder().WithScheme(skelTestScheme(t)).
			WithInterceptorFuncs(interceptor.Funcs{
				Get: func(_ context.Context, _ client.WithWatch, _ client.ObjectKey, _ client.Object, _ ...client.GetOption) error {
					return fmt.Errorf("injected get error")
				},
			}).Build()
		s := newTestSkel(t, cl)
		state, err := s.getSyncState(context.Background(),
			[]*unstructured.Unstructured{newConfigMapUnstructured("cm-a", "test-ns")})
		require.Error(t, err)
		assert.Equal(t, SyncState(SyncStateNotReady), state)
	})

	t.Run("deployment ready", func(t *testing.T) {
		dep := &appsv1.Deployment{
			ObjectMeta: metav1.ObjectMeta{Name: "dep-a", Namespace: "test-ns"},
			Spec:       appsv1.DeploymentSpec{Replicas: ptr.To[int32](1)},
			Status:     appsv1.DeploymentStatus{Replicas: 1, UpdatedReplicas: 1, AvailableReplicas: 1},
		}
		cl := fake.NewClientBuilder().WithScheme(skelTestScheme(t)).WithObjects(dep).Build()
		s := newTestSkel(t, cl)
		state, err := s.getSyncState(context.Background(),
			[]*unstructured.Unstructured{newDeploymentUnstructured("dep-a", "test-ns")})
		require.NoError(t, err)
		assert.Equal(t, SyncState(SyncStateReady), state)
	})

	t.Run("deployment not ready", func(t *testing.T) {
		dep := &appsv1.Deployment{
			ObjectMeta: metav1.ObjectMeta{Name: "dep-a", Namespace: "test-ns"},
			Spec:       appsv1.DeploymentSpec{Replicas: ptr.To[int32](2)},
			Status:     appsv1.DeploymentStatus{Replicas: 1, UpdatedReplicas: 1, AvailableReplicas: 1},
		}
		cl := fake.NewClientBuilder().WithScheme(skelTestScheme(t)).WithObjects(dep).Build()
		s := newTestSkel(t, cl)
		state, err := s.getSyncState(context.Background(),
			[]*unstructured.Unstructured{newDeploymentUnstructured("dep-a", "test-ns")})
		require.NoError(t, err)
		assert.Equal(t, SyncState(SyncStateNotReady), state)
	})
}

func TestSyncObjects(t *testing.T) {
	owner := &nvidiav1alpha1.NVIDIADriver{ObjectMeta: metav1.ObjectMeta{Name: "driver-a", UID: "uid-a"}}

	t.Run("creates objects with owner reference", func(t *testing.T) {
		// driverTestScheme registers NVIDIADriver so SetControllerReference resolves the owner.
		sch := driverTestScheme(t)
		cl := fake.NewClientBuilder().WithScheme(sch).Build()
		s := &stateSkel{name: "state-driver", namespace: "test-ns", client: cl, scheme: sch}

		_, err := s.syncObjects(context.Background(), owner,
			[]*unstructured.Unstructured{newDaemonSetUnstructured("ds-a", "test-ns")})
		require.NoError(t, err)

		dsList := &appsv1.DaemonSetList{}
		require.NoError(t, cl.List(context.Background(), dsList))
		require.Len(t, dsList.Items, 1)
		require.Len(t, dsList.Items[0].OwnerReferences, 1)
		assert.Equal(t, "driver-a", dsList.Items[0].OwnerReferences[0].Name)
	})

	t.Run("set controller reference error", func(t *testing.T) {
		// skelTestScheme omits NVIDIADriver, so SetControllerReference on the owner fails.
		sch := skelTestScheme(t)
		cl := fake.NewClientBuilder().WithScheme(sch).Build()
		s := &stateSkel{name: "state-driver", namespace: "test-ns", client: cl, scheme: sch}

		_, err := s.syncObjects(context.Background(), owner,
			[]*unstructured.Unstructured{newDaemonSetUnstructured("ds-a", "test-ns")})
		require.ErrorContains(t, err, "failed to create/update objects")
	})
}

func toUnstructuredDaemonSet(t *testing.T, ds *appsv1.DaemonSet) *unstructured.Unstructured {
	t.Helper()
	obj, err := runtime.DefaultUnstructuredConverter.ToUnstructured(ds)
	require.NoError(t, err)
	return &unstructured.Unstructured{Object: obj}
}

func TestIsDaemonSetReady(t *testing.T) {
	testCases := []struct {
		name     string
		ds       *appsv1.DaemonSet
		expected bool
	}{
		{
			// A mode-gated DaemonSet on a cluster where no node selects this stack.
			name: "zero desired pods after the DaemonSet controller processed the object",
			ds: &appsv1.DaemonSet{
				ObjectMeta: metav1.ObjectMeta{Generation: 1},
				Status:     appsv1.DaemonSetStatus{ObservedGeneration: 1},
			},
			expected: true,
		},
		{
			name: "zero desired pods but the DaemonSet controller has not processed the object yet",
			ds: &appsv1.DaemonSet{
				ObjectMeta: metav1.ObjectMeta{Generation: 1},
				Status:     appsv1.DaemonSetStatus{ObservedGeneration: 0},
			},
			expected: false,
		},
		{
			name: "zero desired pods with a misscheduled pod still running",
			ds: &appsv1.DaemonSet{
				ObjectMeta: metav1.ObjectMeta{Generation: 1},
				Status: appsv1.DaemonSetStatus{
					ObservedGeneration: 1,
					NumberMisscheduled: 1,
				},
			},
			expected: false,
		},
		{
			name: "zero desired pods with a pod still scheduled (draining)",
			ds: &appsv1.DaemonSet{
				ObjectMeta: metav1.ObjectMeta{Generation: 1},
				Status: appsv1.DaemonSetStatus{
					ObservedGeneration:     1,
					CurrentNumberScheduled: 1,
				},
			},
			expected: false,
		},
		{
			name: "all desired pods available and updated",
			ds: &appsv1.DaemonSet{
				ObjectMeta: metav1.ObjectMeta{Generation: 2},
				Status: appsv1.DaemonSetStatus{
					ObservedGeneration:     2,
					DesiredNumberScheduled: 2,
					CurrentNumberScheduled: 2,
					NumberAvailable:        2,
					UpdatedNumberScheduled: 2,
				},
			},
			expected: true,
		},
		{
			name: "desired pods not yet available",
			ds: &appsv1.DaemonSet{
				ObjectMeta: metav1.ObjectMeta{Generation: 1},
				Status: appsv1.DaemonSetStatus{
					ObservedGeneration:     1,
					DesiredNumberScheduled: 1,
					CurrentNumberScheduled: 1,
					NumberAvailable:        0,
				},
			},
			expected: false,
		},
	}

	s := &stateSkel{}
	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			ready, err := s.isDaemonSetReady(toUnstructuredDaemonSet(t, tc.ds), logr.Discard())
			require.NoError(t, err)
			require.Equal(t, tc.expected, ready)
		})
	}
}

func TestGetSupportedGVKs(t *testing.T) {
	// Generic cleanup deletes every kind in this list, so its exact contents
	// matter. Compare the full set (ElementsMatch also flags duplicates and is
	// order-independent) rather than probing for a single kind.
	expected := []schema.GroupVersionKind{
		{Group: "", Version: "v1", Kind: "ServiceAccount"},
		{Group: "", Version: "v1", Kind: "ConfigMap"},
		{Group: "apps", Version: "v1", Kind: "DaemonSet"},
		{Group: "apps", Version: "v1", Kind: "Deployment"},
		{Group: "apiextensions.k8s.io", Version: "v1", Kind: "CustomResourceDefinition"},
		{Group: "rbac.authorization.k8s.io", Version: "v1", Kind: "ClusterRole"},
		{Group: "rbac.authorization.k8s.io", Version: "v1", Kind: "ClusterRoleBinding"},
		{Group: "rbac.authorization.k8s.io", Version: "v1", Kind: "Role"},
		{Group: "rbac.authorization.k8s.io", Version: "v1", Kind: "RoleBinding"},
		{Group: "k8s.cni.cncf.io", Version: "v1", Kind: "NetworkAttachmentDefinition"},
		{Group: "batch", Version: "v1", Kind: "CronJob"},
		{Group: "security.openshift.io", Version: "v1", Kind: "SecurityContextConstraints"},
		{Group: "", Version: "v1", Kind: "Pod"},
		{Group: "", Version: "v1", Kind: "Service"},
		{Group: "monitoring.coreos.com", Version: "v1", Kind: "ServiceMonitor"},
		{Group: "scheduling.k8s.io", Version: "v1", Kind: "PriorityClass"},
		{Group: "", Version: "v1", Kind: "Taint"},
		{Group: "policy", Version: "v1beta1", Kind: "PodSecurityPolicy"},
		{Group: "node.k8s.io", Version: "v1", Kind: "RuntimeClass"},
		{Group: "monitoring.coreos.com", Version: "v1", Kind: "PrometheusRule"},
		{Group: "resource.k8s.io", Version: "v1", Kind: "ResourceClaimTemplate"},
		{Group: "resource.k8s.io", Version: "v1beta2", Kind: "ResourceClaimTemplate"},
		{Group: "resource.k8s.io", Version: "v1beta1", Kind: "ResourceClaimTemplate"},
	}
	assert.ElementsMatch(t, expected, getSupportedGVKs())
}
