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
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/client/interceptor"

	"github.com/NVIDIA/gpu-operator/internal/consts"
	"github.com/NVIDIA/gpu-operator/internal/utils"
)

// skelTestScheme returns a fresh scheme registering exactly the types the
// stateSkel fake clients exercise (ConfigMaps, ServiceAccounts, DaemonSets),
// keeping each test hermetic instead of relying on the global scheme.
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
	require.Error(t, err)
}

func TestCreateObj(t *testing.T) {
	cl := fake.NewClientBuilder().WithScheme(skelTestScheme(t)).Build()
	s := newTestSkel(t, cl)

	obj := newConfigMapUnstructured("cm-new", "test-ns")
	require.NoError(t, s.createObj(context.Background(), obj))

	// Creating the same object again returns an AlreadyExists error.
	err := s.createObj(context.Background(), obj)
	require.Error(t, err)
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
	data, _, _ := unstructured.NestedStringMap(got.Object, "data")
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

	cl := fake.NewClientBuilder().WithScheme(skelTestScheme(t)).WithObjects(current).Build()
	s.client = cl

	// Record the resourceVersion the stored object has before the sync.
	before := newDaemonSetUnstructured("ds-a", "test-ns")
	require.NoError(t, s.getObj(context.Background(), before))

	noop := func(_ *unstructured.Unstructured) error { return nil }
	require.NoError(t, s.createOrUpdateObjs(context.Background(), noop, []*unstructured.Unstructured{desired}))

	// The hashes matched, so the update must have been skipped. If the skip logic were
	// removed, updateObj would run and the fake client would bump the resourceVersion.
	after := newDaemonSetUnstructured("ds-a", "test-ns")
	require.NoError(t, s.getObj(context.Background(), after))
	assert.Equal(t, before.GetResourceVersion(), after.GetResourceVersion())
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
}

func TestIsDaemonSetReady(t *testing.T) {
	s := newTestSkel(t, fake.NewClientBuilder().WithScheme(skelTestScheme(t)).Build())

	ready := newDaemonSetUnstructured("ds-ready", "test-ns")
	setDaemonSetStatus(ready, 2, 2, 2)
	got, err := s.isDaemonSetReady(ready, logr.Discard())
	require.NoError(t, err)
	assert.True(t, got)

	notReady := newDaemonSetUnstructured("ds-notready", "test-ns")
	setDaemonSetStatus(notReady, 0, 0, 0)
	got, err = s.isDaemonSetReady(notReady, logr.Discard())
	require.NoError(t, err)
	assert.False(t, got)
}

func TestGetSupportedGVKs(t *testing.T) {
	gvks := getSupportedGVKs()
	assert.NotEmpty(t, gvks)
	found := false
	for _, gvk := range gvks {
		if gvk.Kind == "DaemonSet" && gvk.Group == "apps" {
			found = true
		}
	}
	assert.True(t, found)
}
