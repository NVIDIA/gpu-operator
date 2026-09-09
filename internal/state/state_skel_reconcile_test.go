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

func noopSetControllerReference(_ *unstructured.Unstructured) error { return nil }

func TestCreateOrUpdateObjsGetError(t *testing.T) {
	existingConfigMap := newConfigMapUnstructured("cm-a", "test-ns")
	fakeClient := fake.NewClientBuilder().WithScheme(skelTestScheme(t)).WithObjects(existingConfigMap).
		WithInterceptorFuncs(interceptor.Funcs{
			Get: func(_ context.Context, _ client.WithWatch, _ client.ObjectKey, _ client.Object, _ ...client.GetOption) error {
				return fmt.Errorf("injected get error")
			},
		}).Build()
	skel := newTestSkel(t, fakeClient)

	desiredConfigMap := newConfigMapUnstructured("cm-a", "test-ns")
	err := skel.createOrUpdateObjs(context.Background(), noopSetControllerReference,
		[]*unstructured.Unstructured{desiredConfigMap})
	require.ErrorContains(t, err, "injected get error")
}

func TestCreateOrUpdateObjsMergeError(t *testing.T) {
	existingServiceAccount := newServiceAccountUnstructured("sa-a", "test-ns")
	fakeClient := fake.NewClientBuilder().WithScheme(skelTestScheme(t)).WithObjects(existingServiceAccount).
		WithInterceptorFuncs(interceptor.Funcs{
			Get: func(_ context.Context, _ client.WithWatch, _ client.ObjectKey, object client.Object, _ ...client.GetOption) error {
				serviceAccount, ok := object.(*unstructured.Unstructured)
				if !ok {
					return fmt.Errorf("unexpected object type %T", object)
				}
				serviceAccount.SetGroupVersionKind(schema.GroupVersionKind{Version: "v1", Kind: "ServiceAccount"})
				serviceAccount.SetName("sa-a")
				serviceAccount.SetNamespace("test-ns")
				serviceAccount.Object["secrets"] = "not-a-slice"
				return nil
			},
		}).Build()
	skel := newTestSkel(t, fakeClient)

	desiredServiceAccount := newServiceAccountUnstructured("sa-a", "test-ns")
	err := skel.createOrUpdateObjs(context.Background(), noopSetControllerReference,
		[]*unstructured.Unstructured{desiredServiceAccount})
	require.ErrorContains(t, err, ".secrets accessor error")
}

func TestCreateOrUpdateObjsUpdateError(t *testing.T) {
	existingConfigMap := newConfigMapUnstructured("cm-a", "test-ns")
	fakeClient := fake.NewClientBuilder().WithScheme(skelTestScheme(t)).WithObjects(existingConfigMap).
		WithInterceptorFuncs(interceptor.Funcs{
			Update: func(_ context.Context, _ client.WithWatch, _ client.Object, _ ...client.UpdateOption) error {
				return fmt.Errorf("injected update error")
			},
		}).Build()
	skel := newTestSkel(t, fakeClient)

	desiredConfigMap := newConfigMapUnstructured("cm-a", "test-ns")
	err := skel.createOrUpdateObjs(context.Background(), noopSetControllerReference,
		[]*unstructured.Unstructured{desiredConfigMap})
	require.ErrorContains(t, err, "failed to update resource")
	require.ErrorContains(t, err, "injected update error")
}

func TestMergeServiceAccountErrors(t *testing.T) {
	skel := newTestSkel(t, fake.NewClientBuilder().WithScheme(skelTestScheme(t)).Build())

	t.Run("malformed secrets", func(t *testing.T) {
		updatedServiceAccount := newServiceAccountUnstructured("sa", "test-ns")
		currentServiceAccount := newServiceAccountUnstructured("sa", "test-ns")
		currentServiceAccount.Object["secrets"] = "not-a-slice"
		err := skel.mergeServiceAccount(updatedServiceAccount, currentServiceAccount)
		require.ErrorContains(t, err, ".secrets accessor error")
	})

	t.Run("malformed imagePullSecrets", func(t *testing.T) {
		updatedServiceAccount := newServiceAccountUnstructured("sa", "test-ns")
		currentServiceAccount := newServiceAccountUnstructured("sa", "test-ns")
		require.NoError(t, unstructured.SetNestedSlice(currentServiceAccount.Object,
			[]any{map[string]any{"name": "s"}}, "secrets"))
		currentServiceAccount.Object["imagePullSecrets"] = "not-a-slice"
		err := skel.mergeServiceAccount(updatedServiceAccount, currentServiceAccount)
		require.ErrorContains(t, err, ".imagePullSecrets accessor error")
	})
}

func TestIsDaemonSetReadyErrors(t *testing.T) {
	skel := newTestSkel(t, fake.NewClientBuilder().WithScheme(skelTestScheme(t)).Build())

	t.Run("marshal error", func(t *testing.T) {
		daemonSet := newDaemonSetUnstructured("ds", "test-ns")
		daemonSet.Object["bad"] = make(chan int)
		_, err := skel.isDaemonSetReady(daemonSet, logr.Discard())
		require.ErrorContains(t, err, "failed to marshall unstructured daemonset object")
	})

	t.Run("unmarshal error", func(t *testing.T) {
		daemonSet := newDaemonSetUnstructured("ds", "test-ns")
		// A string marshals fine and only fails on the way into the typed Status struct,
		// which is what separates this path from the marshal error above.
		daemonSet.Object["status"] = "not-an-object"
		_, err := skel.isDaemonSetReady(daemonSet, logr.Discard())
		require.ErrorContains(t, err, "failed to unmarshall to daemonset object")
	})
}

// Characterizes a known gap: the nonzero-desired branch of isDaemonSetReady does not
// re-check ObservedGeneration, so status from a prior generation is reported ready.
func TestIsDaemonSetReadyStaleGeneration(t *testing.T) {
	staleDaemonSet := &appsv1.DaemonSet{
		ObjectMeta: metav1.ObjectMeta{Generation: 2},
		Status: appsv1.DaemonSetStatus{
			ObservedGeneration:     1,
			DesiredNumberScheduled: 2,
			CurrentNumberScheduled: 2,
			NumberAvailable:        2,
			UpdatedNumberScheduled: 2,
		},
	}
	skel := &stateSkel{}
	ready, err := skel.isDaemonSetReady(toUnstructuredDaemonSet(t, staleDaemonSet), logr.Discard())
	require.NoError(t, err)
	require.True(t, ready, "known gap: stale-generation status is currently treated as ready")
}

func toUnstructuredDeployment(t *testing.T, deployment *appsv1.Deployment) *unstructured.Unstructured {
	t.Helper()
	object, err := runtime.DefaultUnstructuredConverter.ToUnstructured(deployment)
	require.NoError(t, err)
	return &unstructured.Unstructured{Object: object}
}

func TestIsDeploymentReady(t *testing.T) {
	testCases := []struct {
		name          string
		deployment    *appsv1.Deployment
		expectedReady bool
	}{
		{
			name: "all counts match and generation observed",
			deployment: &appsv1.Deployment{
				ObjectMeta: metav1.ObjectMeta{Generation: 1},
				Spec:       appsv1.DeploymentSpec{Replicas: ptr.To[int32](1)},
				Status:     appsv1.DeploymentStatus{ObservedGeneration: 1, Replicas: 1, UpdatedReplicas: 1, AvailableReplicas: 1},
			},
			expectedReady: true,
		},
		{
			name: "nil replicas defaults to one",
			deployment: &appsv1.Deployment{
				Status: appsv1.DeploymentStatus{Replicas: 1, UpdatedReplicas: 1, AvailableReplicas: 1},
			},
			expectedReady: true,
		},
		{
			name: "zero desired replicas",
			deployment: &appsv1.Deployment{
				Spec: appsv1.DeploymentSpec{Replicas: ptr.To[int32](0)},
			},
			expectedReady: true,
		},
		{
			name: "generation not yet observed",
			deployment: &appsv1.Deployment{
				ObjectMeta: metav1.ObjectMeta{Generation: 2},
				Spec:       appsv1.DeploymentSpec{Replicas: ptr.To[int32](1)},
				Status:     appsv1.DeploymentStatus{ObservedGeneration: 1, UpdatedReplicas: 1, AvailableReplicas: 1},
			},
			expectedReady: false,
		},
		{
			name: "updated replicas below desired",
			deployment: &appsv1.Deployment{
				Spec:   appsv1.DeploymentSpec{Replicas: ptr.To[int32](2)},
				Status: appsv1.DeploymentStatus{UpdatedReplicas: 1, AvailableReplicas: 2},
			},
			expectedReady: false,
		},
		{
			name: "available replicas below desired",
			deployment: &appsv1.Deployment{
				Spec:   appsv1.DeploymentSpec{Replicas: ptr.To[int32](2)},
				Status: appsv1.DeploymentStatus{UpdatedReplicas: 2, AvailableReplicas: 1},
			},
			expectedReady: false,
		},
		{
			// Known gap: isDeploymentReady ignores Status.Replicas, so a superseded
			// replica that is still terminating does not hold the rollout back.
			name: "old replica still terminating is currently ready",
			deployment: &appsv1.Deployment{
				Spec:   appsv1.DeploymentSpec{Replicas: ptr.To[int32](1)},
				Status: appsv1.DeploymentStatus{Replicas: 2, UpdatedReplicas: 1, AvailableReplicas: 1},
			},
			expectedReady: true,
		},
	}

	skel := &stateSkel{}
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			ready, err := skel.isDeploymentReady(toUnstructuredDeployment(t, testCase.deployment), logr.Discard())
			require.NoError(t, err)
			assert.Equal(t, testCase.expectedReady, ready)
		})
	}
}

func TestIsDeploymentReadyErrors(t *testing.T) {
	skel := &stateSkel{}

	t.Run("marshal error", func(t *testing.T) {
		deployment := toUnstructuredDeployment(t, &appsv1.Deployment{})
		deployment.Object["bad"] = make(chan int)
		_, err := skel.isDeploymentReady(deployment, logr.Discard())
		require.ErrorContains(t, err, "failed to marshall unstructured deployment object")
	})

	t.Run("unmarshal error", func(t *testing.T) {
		deployment := toUnstructuredDeployment(t, &appsv1.Deployment{})
		deployment.Object["status"] = "not-an-object"
		_, err := skel.isDeploymentReady(deployment, logr.Discard())
		require.ErrorContains(t, err, "failed to unmarshall to deployment object")
	})
}

func newDeletionSkel(t *testing.T, objects ...client.Object) (*stateSkel, client.Client) {
	t.Helper()
	return newDeletionSkelWithInterceptor(t, interceptor.Funcs{}, objects...)
}

// The REST mapper registers only DaemonSet and ClusterRole, so every other kind in
// getSupportedGVKs is skipped as a NoMatch and cannot interfere with the assertions.
func newDeletionSkelWithInterceptor(t *testing.T, interceptorFuncs interceptor.Funcs, objects ...client.Object) (*stateSkel, client.Client) {
	t.Helper()
	scheme := runtime.NewScheme()
	require.NoError(t, corev1.AddToScheme(scheme))
	require.NoError(t, appsv1.AddToScheme(scheme))
	require.NoError(t, rbacv1.AddToScheme(scheme))

	mapper := meta.NewDefaultRESTMapper(nil)
	mapper.Add(schema.GroupVersionKind{Group: "apps", Version: "v1", Kind: "DaemonSet"}, meta.RESTScopeNamespace)
	mapper.Add(schema.GroupVersionKind{Group: "rbac.authorization.k8s.io", Version: "v1", Kind: "ClusterRole"}, meta.RESTScopeRoot)

	fakeClient := fake.NewClientBuilder().WithScheme(scheme).WithRESTMapper(mapper).
		WithObjects(objects...).WithInterceptorFuncs(interceptorFuncs).Build()
	skel := &stateSkel{name: "test-state", namespace: "test-ns", client: fakeClient, scheme: scheme}
	return skel, fakeClient
}

func stateLabeledDaemonSet(name, namespace string) *appsv1.DaemonSet {
	return &appsv1.DaemonSet{ObjectMeta: metav1.ObjectMeta{
		Name: name, Namespace: namespace, Labels: map[string]string{consts.StateLabel: "test-state"},
	}}
}

func stateLabeledClusterRole(name string) *rbacv1.ClusterRole {
	return &rbacv1.ClusterRole{ObjectMeta: metav1.ObjectMeta{
		Name: name, Labels: map[string]string{consts.StateLabel: "test-state"},
	}}
}

func TestDeleteStateRelatedObjectsScoping(t *testing.T) {
	otherStateDaemonSet := &appsv1.DaemonSet{ObjectMeta: metav1.ObjectMeta{
		Name: "ds-other-state", Namespace: "test-ns",
		Labels: map[string]string{consts.StateLabel: "another-state"},
	}}
	skel, fakeClient := newDeletionSkel(t,
		stateLabeledDaemonSet("ds-here", "test-ns"),
		stateLabeledDaemonSet("ds-other-ns", "other-ns"),
		otherStateDaemonSet,
		stateLabeledClusterRole("cr-1"),
	)

	ctx := context.Background()
	foundObjects, err := skel.deleteStateRelatedObjects(ctx)
	require.NoError(t, err)
	assert.True(t, foundObjects)

	assert.True(t, apierrors.IsNotFound(fakeClient.Get(ctx,
		client.ObjectKey{Name: "ds-here", Namespace: "test-ns"}, &appsv1.DaemonSet{})),
		"a labeled DaemonSet in the operator namespace must be deleted")
	assert.NoError(t, fakeClient.Get(ctx,
		client.ObjectKey{Name: "ds-other-ns", Namespace: "other-ns"}, &appsv1.DaemonSet{}),
		"namespaced kinds are listed only in the operator namespace, so another namespace is untouched")
	assert.NoError(t, fakeClient.Get(ctx,
		client.ObjectKey{Name: "ds-other-state", Namespace: "test-ns"}, &appsv1.DaemonSet{}),
		"a DaemonSet labeled for a different state must survive")
	assert.True(t, apierrors.IsNotFound(fakeClient.Get(ctx,
		client.ObjectKey{Name: "cr-1"}, &rbacv1.ClusterRole{})),
		"cluster-scoped kinds are listed cluster-wide and cleaned up")
}

func TestHandleStateObjectsDeletion(t *testing.T) {
	t.Run("objects present reports not ready while deleting", func(t *testing.T) {
		skel, _ := newDeletionSkel(t, stateLabeledDaemonSet("ds-here", "test-ns"))
		syncState, err := skel.handleStateObjectsDeletion(context.Background())
		require.NoError(t, err)
		assert.Equal(t, SyncState(SyncStateNotReady), syncState)
	})
	t.Run("nothing to delete reports ignore", func(t *testing.T) {
		skel, _ := newDeletionSkel(t)
		syncState, err := skel.handleStateObjectsDeletion(context.Background())
		require.NoError(t, err)
		assert.Equal(t, SyncState(SyncStateIgnore), syncState)
	})
	t.Run("deletion error surfaces as sync error", func(t *testing.T) {
		skel, _ := newDeletionSkelWithInterceptor(t, interceptor.Funcs{
			Delete: func(_ context.Context, _ client.WithWatch, _ client.Object, _ ...client.DeleteOption) error {
				return fmt.Errorf("injected delete error")
			},
		}, stateLabeledDaemonSet("ds-here", "test-ns"))
		syncState, err := skel.handleStateObjectsDeletion(context.Background())
		require.ErrorContains(t, err, "failed to delete k8s objects")
		assert.Equal(t, SyncState(SyncStateError), syncState)
	})
}

// deleteStateRelatedObjects treats a Forbidden List as "nothing to clean up" by design,
// on the invariant that the operator cannot have created objects of a kind it cannot list.
// The residual risk this pins is list permission revoked after creation, where cleanup
// reports Ignore and leaves the operand behind.
func TestDeleteStateRelatedObjectsForbiddenListSkipped(t *testing.T) {
	skel, fakeClient := newDeletionSkelWithInterceptor(t, interceptor.Funcs{
		List: func(_ context.Context, _ client.WithWatch, _ client.ObjectList, _ ...client.ListOption) error {
			return apierrors.NewForbidden(schema.GroupResource{Resource: "daemonsets"}, "", fmt.Errorf("nope"))
		},
	}, stateLabeledDaemonSet("ds-here", "test-ns"))

	ctx := context.Background()
	syncState, err := skel.handleStateObjectsDeletion(ctx)
	require.NoError(t, err)
	assert.Equal(t, SyncState(SyncStateIgnore), syncState)
	assert.NoError(t, fakeClient.Get(ctx,
		client.ObjectKey{Name: "ds-here", Namespace: "test-ns"}, &appsv1.DaemonSet{}),
		"the object the operator could not list is left in place")
}

func TestDeleteStateRelatedObjectsNotFoundOnDeleteIgnored(t *testing.T) {
	deleteCalls := 0
	skel, _ := newDeletionSkelWithInterceptor(t, interceptor.Funcs{
		Delete: func(_ context.Context, _ client.WithWatch, object client.Object, _ ...client.DeleteOption) error {
			deleteCalls++
			assert.Equal(t, "ds-here", object.GetName())
			assert.Equal(t, "test-ns", object.GetNamespace())
			return apierrors.NewNotFound(schema.GroupResource{Resource: "daemonsets"}, object.GetName())
		},
	}, stateLabeledDaemonSet("ds-here", "test-ns"))

	foundObjects, err := skel.deleteStateRelatedObjects(context.Background())
	require.NoError(t, err)
	assert.True(t, foundObjects)
	assert.Equal(t, 1, deleteCalls, "expected exactly one delete attempt")
}

func TestDeleteStateRelatedObjectsListErrorPropagates(t *testing.T) {
	skel, _ := newDeletionSkelWithInterceptor(t, interceptor.Funcs{
		List: func(_ context.Context, _ client.WithWatch, _ client.ObjectList, _ ...client.ListOption) error {
			return fmt.Errorf("injected list error")
		},
	}, stateLabeledDaemonSet("ds-here", "test-ns"))

	_, err := skel.deleteStateRelatedObjects(context.Background())
	require.ErrorContains(t, err, "injected list error")
}

func TestDeleteStateRelatedObjectsDeleteErrorPropagates(t *testing.T) {
	skel, _ := newDeletionSkelWithInterceptor(t, interceptor.Funcs{
		Delete: func(_ context.Context, _ client.WithWatch, _ client.Object, _ ...client.DeleteOption) error {
			return fmt.Errorf("injected delete error")
		},
	}, stateLabeledDaemonSet("ds-here", "test-ns"))

	foundObjects, err := skel.deleteStateRelatedObjects(context.Background())
	require.ErrorContains(t, err, "injected delete error")
	assert.True(t, foundObjects, "a delete failure still reports the objects it found")
}

func TestDeleteStateRelatedObjectsSkipsAlreadyDeleting(t *testing.T) {
	deletingDaemonSet := stateLabeledDaemonSet("ds-deleting", "test-ns")
	now := metav1.Now()
	deletingDaemonSet.DeletionTimestamp = &now
	deletingDaemonSet.Finalizers = []string{"nvidia.com/keep"}

	deleteCalls := 0
	skel, _ := newDeletionSkelWithInterceptor(t, interceptor.Funcs{
		Delete: func(ctx context.Context, wrappedClient client.WithWatch, object client.Object, opts ...client.DeleteOption) error {
			deleteCalls++
			return wrappedClient.Delete(ctx, object, opts...)
		},
	}, deletingDaemonSet)

	foundObjects, err := skel.deleteStateRelatedObjects(context.Background())
	require.NoError(t, err)
	assert.True(t, foundObjects, "an object still present counts as found")
	assert.Zero(t, deleteCalls, "an object already being deleted must not be deleted again")
}

// RESTMapping is the only method deleteStateRelatedObjects calls, so the embedded
// interface can stay nil; any other call would panic and expose the assumption.
type erroringRESTMapper struct{ meta.RESTMapper }

func (erroringRESTMapper) RESTMapping(schema.GroupKind, ...string) (*meta.RESTMapping, error) {
	return nil, fmt.Errorf("injected mapping error")
}

func TestDeleteStateRelatedObjectsMappingErrorPropagates(t *testing.T) {
	scheme := runtime.NewScheme()
	require.NoError(t, appsv1.AddToScheme(scheme))
	fakeClient := fake.NewClientBuilder().WithScheme(scheme).WithRESTMapper(erroringRESTMapper{}).Build()
	skel := &stateSkel{name: "test-state", namespace: "test-ns", client: fakeClient, scheme: scheme}

	_, err := skel.deleteStateRelatedObjects(context.Background())
	require.ErrorContains(t, err, "injected mapping error")
}
