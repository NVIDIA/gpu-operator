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
	rbacv1 "k8s.io/api/rbac/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/client/interceptor"

	"github.com/NVIDIA/gpu-operator/internal/consts"
)

// TestCreateOrUpdateObjsGetError covers the branch where an object already
// exists but the subsequent Get fails.
func TestCreateOrUpdateObjsGetError(t *testing.T) {
	existing := newConfigMapUnstructured("cm-a", "test-ns")
	cl := fake.NewClientBuilder().WithScheme(skelTestScheme(t)).WithObjects(existing).
		WithInterceptorFuncs(interceptor.Funcs{
			Get: func(_ context.Context, _ client.WithWatch, _ client.ObjectKey, _ client.Object, _ ...client.GetOption) error {
				return fmt.Errorf("injected get error")
			},
		}).Build()
	s := newTestSkel(t, cl)

	desired := newConfigMapUnstructured("cm-a", "test-ns")
	noop := func(_ *unstructured.Unstructured) error { return nil }
	err := s.createOrUpdateObjs(context.Background(), noop, []*unstructured.Unstructured{desired})
	require.ErrorContains(t, err, "injected get error")
}

// TestCreateOrUpdateObjsMergeError covers the mergeObjects error branch: an
// existing ServiceAccount whose "secrets" field is malformed makes
// mergeServiceAccount fail.
func TestCreateOrUpdateObjsMergeError(t *testing.T) {
	existing := newServiceAccountUnstructured("sa-a", "test-ns")
	cl := fake.NewClientBuilder().WithScheme(skelTestScheme(t)).WithObjects(existing).
		WithInterceptorFuncs(interceptor.Funcs{
			Get: func(_ context.Context, _ client.WithWatch, _ client.ObjectKey, obj client.Object, _ ...client.GetOption) error {
				// Return a ServiceAccount whose secrets field is not a slice.
				u, ok := obj.(*unstructured.Unstructured)
				if !ok {
					return fmt.Errorf("unexpected object type")
				}
				u.SetGroupVersionKind(schema.GroupVersionKind{Version: "v1", Kind: "ServiceAccount"})
				u.SetName("sa-a")
				u.SetNamespace("test-ns")
				u.Object["secrets"] = "not-a-slice"
				return nil
			},
		}).Build()
	s := newTestSkel(t, cl)

	desired := newServiceAccountUnstructured("sa-a", "test-ns")
	noop := func(_ *unstructured.Unstructured) error { return nil }
	err := s.createOrUpdateObjs(context.Background(), noop, []*unstructured.Unstructured{desired})
	require.Error(t, err)
}

// TestCreateOrUpdateObjsUpdateError covers the updateObj error branch during
// create-or-update of an existing object.
func TestCreateOrUpdateObjsUpdateError(t *testing.T) {
	existing := newConfigMapUnstructured("cm-a", "test-ns")
	cl := fake.NewClientBuilder().WithScheme(skelTestScheme(t)).WithObjects(existing).
		WithInterceptorFuncs(interceptor.Funcs{
			Update: func(_ context.Context, _ client.WithWatch, _ client.Object, _ ...client.UpdateOption) error {
				return fmt.Errorf("injected update error")
			},
		}).Build()
	s := newTestSkel(t, cl)

	desired := newConfigMapUnstructured("cm-a", "test-ns")
	require.NoError(t, unstructured.SetNestedStringMap(desired.Object, map[string]string{"key": "new"}, "data"))
	noop := func(_ *unstructured.Unstructured) error { return nil }
	err := s.createOrUpdateObjs(context.Background(), noop, []*unstructured.Unstructured{desired})
	require.ErrorContains(t, err, "failed to update resource")
}

// TestMergeServiceAccountErrors covers the NestedSlice error branches for both
// secrets and imagePullSecrets fields.
func TestMergeServiceAccountErrors(t *testing.T) {
	s := newTestSkel(t, fake.NewClientBuilder().WithScheme(skelTestScheme(t)).Build())

	t.Run("malformed secrets", func(t *testing.T) {
		updated := newServiceAccountUnstructured("sa", "test-ns")
		current := newServiceAccountUnstructured("sa", "test-ns")
		current.Object["secrets"] = "not-a-slice"
		err := s.mergeServiceAccount(updated, current)
		require.Error(t, err)
	})

	t.Run("malformed imagePullSecrets", func(t *testing.T) {
		updated := newServiceAccountUnstructured("sa", "test-ns")
		current := newServiceAccountUnstructured("sa", "test-ns")
		require.NoError(t, unstructured.SetNestedSlice(current.Object,
			[]interface{}{map[string]interface{}{"name": "s"}}, "secrets"))
		current.Object["imagePullSecrets"] = "not-a-slice"
		err := s.mergeServiceAccount(updated, current)
		require.Error(t, err)
	})
}

// TestIsDaemonSetReadyErrors covers the JSON marshal and unmarshal error paths.
func TestIsDaemonSetReadyErrors(t *testing.T) {
	s := newTestSkel(t, fake.NewClientBuilder().WithScheme(skelTestScheme(t)).Build())

	t.Run("marshal error", func(t *testing.T) {
		daemonSet := newDaemonSetUnstructured("ds", "test-ns")
		// A channel value cannot be marshalled to JSON.
		daemonSet.Object["bad"] = make(chan int)
		_, err := s.isDaemonSetReady(daemonSet, logr.Discard())
		require.ErrorContains(t, err, "failed to marshall unstructured daemonset object")
	})

	t.Run("unmarshal error", func(t *testing.T) {
		daemonSet := newDaemonSetUnstructured("ds", "test-ns")
		// status must be an object; a string marshals fine but fails to unmarshal
		// into the typed DaemonSet.Status struct.
		daemonSet.Object["status"] = "not-an-object"
		_, err := s.isDaemonSetReady(daemonSet, logr.Discard())
		require.ErrorContains(t, err, "failed to unmarshall to daemonset object")
	})
}

// TestGetObjNotFoundHelper verifies IsNotFound classification on a missing object.
func TestGetObjNotFound(t *testing.T) {
	cl := fake.NewClientBuilder().WithScheme(skelTestScheme(t)).Build()
	s := newTestSkel(t, cl)
	missing := newConfigMapUnstructured("missing", "test-ns")
	err := s.getObj(context.Background(), missing)
	require.True(t, apierrors.IsNotFound(err))
}

// TestCreateObjAlreadyExists verifies the AlreadyExists branch in createObj.
func TestCreateObjAlreadyExists(t *testing.T) {
	obj := newConfigMapUnstructured("cm", "test-ns")
	cl := fake.NewClientBuilder().WithScheme(skelTestScheme(t)).WithObjects(obj).Build()
	s := newTestSkel(t, cl)

	err := s.createObj(context.Background(), newConfigMapUnstructured("cm", "test-ns"))
	require.True(t, apierrors.IsAlreadyExists(err))
}

// --- readiness predicates ------------------------------------------------------

// Characterizes a known gap: the nonzero-desired path of isDaemonSetReady does
// not re-check ObservedGeneration, so stale status from a prior generation is
// reported ready (unlike Kubernetes rollout-status).
func TestIsDaemonSetReadyStaleGeneration(t *testing.T) {
	ds := &appsv1.DaemonSet{
		ObjectMeta: metav1.ObjectMeta{Generation: 2},
		Status: appsv1.DaemonSetStatus{
			ObservedGeneration:     1,
			DesiredNumberScheduled: 2,
			CurrentNumberScheduled: 2,
			NumberAvailable:        2,
			UpdatedNumberScheduled: 2,
		},
	}
	s := &stateSkel{}
	ready, err := s.isDaemonSetReady(toUnstructuredDaemonSet(t, ds), logr.Discard())
	require.NoError(t, err)
	assert.True(t, ready, "known gap: stale-generation status is currently treated as ready")
}

func toUnstructuredDeployment(t *testing.T, dep *appsv1.Deployment) *unstructured.Unstructured {
	t.Helper()
	obj, err := runtime.DefaultUnstructuredConverter.ToUnstructured(dep)
	require.NoError(t, err)
	return &unstructured.Unstructured{Object: obj}
}

func TestIsDeploymentReady(t *testing.T) {
	testCases := []struct {
		name     string
		dep      *appsv1.Deployment
		expected bool
	}{
		{
			name: "all counts match and generation observed",
			dep: &appsv1.Deployment{
				ObjectMeta: metav1.ObjectMeta{Generation: 1},
				Spec:       appsv1.DeploymentSpec{Replicas: ptr.To[int32](1)},
				Status:     appsv1.DeploymentStatus{ObservedGeneration: 1, Replicas: 1, UpdatedReplicas: 1, AvailableReplicas: 1},
			},
			expected: true,
		},
		{
			name: "nil replicas defaults to one",
			dep: &appsv1.Deployment{
				Status: appsv1.DeploymentStatus{Replicas: 1, UpdatedReplicas: 1, AvailableReplicas: 1},
			},
			expected: true,
		},
		{
			name: "zero desired replicas",
			dep: &appsv1.Deployment{
				Spec: appsv1.DeploymentSpec{Replicas: ptr.To[int32](0)},
			},
			expected: true,
		},
		{
			name: "generation not yet observed",
			dep: &appsv1.Deployment{
				ObjectMeta: metav1.ObjectMeta{Generation: 2},
				Spec:       appsv1.DeploymentSpec{Replicas: ptr.To[int32](1)},
				Status:     appsv1.DeploymentStatus{ObservedGeneration: 1, UpdatedReplicas: 1, AvailableReplicas: 1},
			},
			expected: false,
		},
		{
			name: "updated replicas below desired",
			dep: &appsv1.Deployment{
				Spec:   appsv1.DeploymentSpec{Replicas: ptr.To[int32](2)},
				Status: appsv1.DeploymentStatus{UpdatedReplicas: 1, AvailableReplicas: 2},
			},
			expected: false,
		},
		{
			name: "available replicas below desired",
			dep: &appsv1.Deployment{
				Spec:   appsv1.DeploymentSpec{Replicas: ptr.To[int32](2)},
				Status: appsv1.DeploymentStatus{UpdatedReplicas: 2, AvailableReplicas: 1},
			},
			expected: false,
		},
		{
			// Known gap: isDeploymentReady ignores Status.Replicas, so a rollout with
			// an old replica still terminating (Replicas=2, desired=1) reports ready.
			name: "old replica still terminating is currently ready",
			dep: &appsv1.Deployment{
				Spec:   appsv1.DeploymentSpec{Replicas: ptr.To[int32](1)},
				Status: appsv1.DeploymentStatus{Replicas: 2, UpdatedReplicas: 1, AvailableReplicas: 1},
			},
			expected: true,
		},
	}

	s := &stateSkel{}
	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			ready, err := s.isDeploymentReady(toUnstructuredDeployment(t, tc.dep), logr.Discard())
			require.NoError(t, err)
			assert.Equal(t, tc.expected, ready)
		})
	}
}

func TestIsDeploymentReadyErrors(t *testing.T) {
	s := &stateSkel{}

	t.Run("marshal error", func(t *testing.T) {
		dep := toUnstructuredDeployment(t, &appsv1.Deployment{})
		dep.Object["bad"] = make(chan int)
		_, err := s.isDeploymentReady(dep, logr.Discard())
		require.ErrorContains(t, err, "failed to marshall unstructured deployment object")
	})

	t.Run("unmarshal error", func(t *testing.T) {
		dep := toUnstructuredDeployment(t, &appsv1.Deployment{})
		dep.Object["status"] = "not-an-object"
		_, err := s.isDeploymentReady(dep, logr.Discard())
		require.ErrorContains(t, err, "failed to unmarshall to deployment object")
	})
}

// --- generic state-object deletion ---------------------------------------------

// newDeletionSkel builds a stateSkel whose REST mapper knows a namespaced kind
// (DaemonSet) and a cluster-scoped kind (ClusterRole); other GVKs are NoMatch-skipped.
func newDeletionSkel(t *testing.T, objs ...client.Object) (*stateSkel, client.Client) {
	t.Helper()
	return newDeletionSkelWithInterceptor(t, interceptor.Funcs{}, objs...)
}

func newDeletionSkelWithInterceptor(t *testing.T, funcs interceptor.Funcs, objs ...client.Object) (*stateSkel, client.Client) {
	t.Helper()
	sch := runtime.NewScheme()
	require.NoError(t, corev1.AddToScheme(sch))
	require.NoError(t, appsv1.AddToScheme(sch))
	require.NoError(t, rbacv1.AddToScheme(sch))

	mapper := meta.NewDefaultRESTMapper(nil)
	mapper.Add(schema.GroupVersionKind{Group: "apps", Version: "v1", Kind: "DaemonSet"}, meta.RESTScopeNamespace)
	mapper.Add(schema.GroupVersionKind{Group: "rbac.authorization.k8s.io", Version: "v1", Kind: "ClusterRole"}, meta.RESTScopeRoot)

	cl := fake.NewClientBuilder().WithScheme(sch).WithRESTMapper(mapper).
		WithObjects(objs...).WithInterceptorFuncs(funcs).Build()
	s := &stateSkel{name: "test-state", namespace: "test-ns", client: cl, scheme: sch}
	return s, cl
}

func labeledDaemonSet(name, ns string) *appsv1.DaemonSet {
	return &appsv1.DaemonSet{ObjectMeta: metav1.ObjectMeta{
		Name: name, Namespace: ns, Labels: map[string]string{consts.StateLabel: "test-state"},
	}}
}

func labeledClusterRole(name string) *rbacv1.ClusterRole {
	return &rbacv1.ClusterRole{ObjectMeta: metav1.ObjectMeta{
		Name: name, Labels: map[string]string{consts.StateLabel: "test-state"},
	}}
}

func TestDeleteStateRelatedObjectsScoping(t *testing.T) {
	s, cl := newDeletionSkel(t,
		labeledDaemonSet("ds-here", "test-ns"),
		labeledDaemonSet("ds-other", "other-ns"),
		labeledClusterRole("cr-1"),
	)

	found, err := s.deleteStateRelatedObjects(context.Background())
	require.NoError(t, err)
	assert.True(t, found)

	// Namespaced kinds are listed only in the operator namespace, so the
	// same-labeled DaemonSet in another namespace survives.
	assert.True(t, apierrors.IsNotFound(cl.Get(context.Background(),
		client.ObjectKey{Name: "ds-here", Namespace: "test-ns"}, &appsv1.DaemonSet{})))
	assert.NoError(t, cl.Get(context.Background(),
		client.ObjectKey{Name: "ds-other", Namespace: "other-ns"}, &appsv1.DaemonSet{}))
	// Cluster-scoped kinds are listed cluster-wide and cleaned up.
	assert.True(t, apierrors.IsNotFound(cl.Get(context.Background(),
		client.ObjectKey{Name: "cr-1"}, &rbacv1.ClusterRole{})))
}

func TestHandleStateObjectsDeletion(t *testing.T) {
	t.Run("objects present reports not ready while deleting", func(t *testing.T) {
		s, _ := newDeletionSkel(t, labeledDaemonSet("ds-here", "test-ns"))
		st, err := s.handleStateObjectsDeletion(context.Background())
		require.NoError(t, err)
		assert.Equal(t, SyncState(SyncStateNotReady), st)
	})
	t.Run("nothing to delete reports ignore", func(t *testing.T) {
		s, _ := newDeletionSkel(t)
		st, err := s.handleStateObjectsDeletion(context.Background())
		require.NoError(t, err)
		assert.Equal(t, SyncState(SyncStateIgnore), st)
	})
	t.Run("deletion error surfaces as sync error", func(t *testing.T) {
		s, _ := newDeletionSkelWithInterceptor(t, interceptor.Funcs{
			Delete: func(_ context.Context, _ client.WithWatch, _ client.Object, _ ...client.DeleteOption) error {
				return fmt.Errorf("boom delete")
			},
		}, labeledDaemonSet("ds-here", "test-ns"))
		st, err := s.handleStateObjectsDeletion(context.Background())
		require.ErrorContains(t, err, "failed to delete k8s objects")
		assert.Equal(t, SyncState(SyncStateError), st)
	})
}

// Characterizes a known gap: a Forbidden List is treated as "nothing to clean
// up", so cleanup reports Ignore and leaves the object behind.
func TestDeleteStateRelatedObjectsForbiddenListSkipped(t *testing.T) {
	s, cl := newDeletionSkelWithInterceptor(t, interceptor.Funcs{
		List: func(_ context.Context, _ client.WithWatch, _ client.ObjectList, _ ...client.ListOption) error {
			return apierrors.NewForbidden(schema.GroupResource{Resource: "daemonsets"}, "", fmt.Errorf("nope"))
		},
	}, labeledDaemonSet("ds-here", "test-ns"))

	st, err := s.handleStateObjectsDeletion(context.Background())
	require.NoError(t, err)
	assert.Equal(t, SyncState(SyncStateIgnore), st)
	// The object the operator could not list is left in place.
	assert.NoError(t, cl.Get(context.Background(),
		client.ObjectKey{Name: "ds-here", Namespace: "test-ns"}, &appsv1.DaemonSet{}))
}

func TestDeleteStateRelatedObjectsNotFoundOnDeleteIgnored(t *testing.T) {
	deleteCalls := 0
	s, _ := newDeletionSkelWithInterceptor(t, interceptor.Funcs{
		Delete: func(_ context.Context, _ client.WithWatch, obj client.Object, _ ...client.DeleteOption) error {
			deleteCalls++
			assert.Equal(t, "ds-here", obj.GetName())
			assert.Equal(t, "test-ns", obj.GetNamespace())
			return apierrors.NewNotFound(schema.GroupResource{Resource: "daemonsets"}, obj.GetName())
		},
	}, labeledDaemonSet("ds-here", "test-ns"))

	found, err := s.deleteStateRelatedObjects(context.Background())
	require.NoError(t, err) // NotFound on delete is ignored
	assert.True(t, found)
	assert.Equal(t, 1, deleteCalls, "expected exactly one delete attempt")
}

func TestDeleteStateRelatedObjectsListErrorPropagates(t *testing.T) {
	s, _ := newDeletionSkelWithInterceptor(t, interceptor.Funcs{
		List: func(_ context.Context, _ client.WithWatch, _ client.ObjectList, _ ...client.ListOption) error {
			return fmt.Errorf("boom list")
		},
	}, labeledDaemonSet("ds-here", "test-ns"))

	_, err := s.deleteStateRelatedObjects(context.Background())
	require.ErrorContains(t, err, "boom list")
}

func TestDeleteStateRelatedObjectsDeleteErrorPropagates(t *testing.T) {
	s, _ := newDeletionSkelWithInterceptor(t, interceptor.Funcs{
		Delete: func(_ context.Context, _ client.WithWatch, _ client.Object, _ ...client.DeleteOption) error {
			return fmt.Errorf("boom delete")
		},
	}, labeledDaemonSet("ds-here", "test-ns"))

	_, err := s.deleteStateRelatedObjects(context.Background())
	require.ErrorContains(t, err, "boom delete")
}

func TestDeleteStateRelatedObjectsSkipsAlreadyDeleting(t *testing.T) {
	ds := labeledDaemonSet("ds-deleting", "test-ns")
	now := metav1.Now()
	ds.DeletionTimestamp = &now
	ds.Finalizers = []string{"nvidia.com/keep"}

	deletes := 0
	s, _ := newDeletionSkelWithInterceptor(t, interceptor.Funcs{
		Delete: func(ctx context.Context, cl client.WithWatch, obj client.Object, opts ...client.DeleteOption) error {
			deletes++
			return cl.Delete(ctx, obj, opts...)
		},
	}, ds)

	found, err := s.deleteStateRelatedObjects(context.Background())
	require.NoError(t, err)
	assert.True(t, found, "an object still present counts as found")
	assert.Zero(t, deletes, "an object already being deleted must not be deleted again")
}

// erroringRESTMapper returns a non-NoMatch error from RESTMapping (the only
// method deleteStateRelatedObjects calls; the embedded interface stays nil).
type erroringRESTMapper struct{ meta.RESTMapper }

func (erroringRESTMapper) RESTMapping(schema.GroupKind, ...string) (*meta.RESTMapping, error) {
	return nil, fmt.Errorf("boom mapping error")
}

func TestDeleteStateRelatedObjectsMappingErrorPropagates(t *testing.T) {
	sch := runtime.NewScheme()
	require.NoError(t, appsv1.AddToScheme(sch))
	cl := fake.NewClientBuilder().WithScheme(sch).WithRESTMapper(erroringRESTMapper{}).Build()
	s := &stateSkel{name: "test-state", namespace: "test-ns", client: cl, scheme: sch}

	// A RESTMapping error other than NoMatch must propagate rather than be skipped.
	_, err := s.deleteStateRelatedObjects(context.Background())
	require.ErrorContains(t, err, "boom mapping error")
}
