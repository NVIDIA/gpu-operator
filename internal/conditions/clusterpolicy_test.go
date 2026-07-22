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

package conditions

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/kubernetes/scheme"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/client/interceptor"

	nvidiav1 "github.com/NVIDIA/gpu-operator/api/nvidia/v1"
)

// clusterPolicyScheme returns a scheme that knows about ClusterPolicy.
func clusterPolicyScheme(t *testing.T) {
	t.Helper()
	require.NoError(t, nvidiav1.AddToScheme(scheme.Scheme))
}

// newClusterPolicyClient builds a fake client seeded with the given objects and
// their status subresource enabled.
func newClusterPolicyClient(t *testing.T, objs ...client.Object) client.Client {
	t.Helper()
	clusterPolicyScheme(t)
	b := fake.NewClientBuilder().WithScheme(scheme.Scheme)
	if len(objs) > 0 {
		b = b.WithObjects(objs...).WithStatusSubresource(objs...)
	}
	return b.Build()
}

func newClusterPolicy(name string) *nvidiav1.ClusterPolicy {
	return &nvidiav1.ClusterPolicy{ObjectMeta: metav1.ObjectMeta{Name: name}}
}

func TestNewClusterPolicyUpdater(t *testing.T) {
	u := NewClusterPolicyUpdater(newClusterPolicyClient(t))
	assert.NotNil(t, u)
	assert.IsType(t, &clusterPolicyUpdater{}, u)
}

func TestClusterPolicyUpdater_SetConditionsReady(t *testing.T) {
	clusterPolicy := newClusterPolicy("cluster-policy")
	c := newClusterPolicyClient(t, clusterPolicy)
	u := NewClusterPolicyUpdater(c)

	err := u.SetConditionsReady(context.Background(), clusterPolicy, Reconciled, "all resources reconciled")
	require.NoError(t, err)

	got := &nvidiav1.ClusterPolicy{}
	require.NoError(t, c.Get(context.Background(), types.NamespacedName{Name: clusterPolicy.Name}, got))
	require.Len(t, got.Status.Conditions, 2)

	// Ready => True with the caller's reason/message.
	assert.Equal(t, Ready, got.Status.Conditions[0].Type)
	assert.Equal(t, metav1.ConditionTrue, got.Status.Conditions[0].Status)
	assert.Equal(t, Reconciled, got.Status.Conditions[0].Reason)
	assert.Equal(t, "all resources reconciled", got.Status.Conditions[0].Message)

	// Error => False with reason "Ready" and no message.
	assert.Equal(t, Error, got.Status.Conditions[1].Type)
	assert.Equal(t, metav1.ConditionFalse, got.Status.Conditions[1].Status)
	assert.Equal(t, Ready, got.Status.Conditions[1].Reason)
	assert.Empty(t, got.Status.Conditions[1].Message)
}

func TestClusterPolicyUpdater_SetConditionsError(t *testing.T) {
	clusterPolicy := newClusterPolicy("cluster-policy")
	c := newClusterPolicyClient(t, clusterPolicy)
	u := NewClusterPolicyUpdater(c)

	err := u.SetConditionsError(context.Background(), clusterPolicy, ReconcileFailed, "reconciliation failed")
	require.NoError(t, err)

	got := &nvidiav1.ClusterPolicy{}
	require.NoError(t, c.Get(context.Background(), types.NamespacedName{Name: clusterPolicy.Name}, got))
	require.Len(t, got.Status.Conditions, 2)

	// Ready => False with reason "Error" and no message.
	assert.Equal(t, Ready, got.Status.Conditions[0].Type)
	assert.Equal(t, metav1.ConditionFalse, got.Status.Conditions[0].Status)
	assert.Equal(t, Error, got.Status.Conditions[0].Reason)
	assert.Empty(t, got.Status.Conditions[0].Message)

	// Error => True with the caller's reason/message.
	assert.Equal(t, Error, got.Status.Conditions[1].Type)
	assert.Equal(t, metav1.ConditionTrue, got.Status.Conditions[1].Status)
	assert.Equal(t, ReconcileFailed, got.Status.Conditions[1].Reason)
	assert.Equal(t, "reconciliation failed", got.Status.Conditions[1].Message)
}

// TestClusterPolicyUpdater_ReadyThenError verifies transitioning from Ready to
// Error flips both conditions in place rather than accumulating duplicates.
func TestClusterPolicyUpdater_ReadyThenError(t *testing.T) {
	clusterPolicy := newClusterPolicy("cluster-policy")
	c := newClusterPolicyClient(t, clusterPolicy)
	u := NewClusterPolicyUpdater(c)
	ctx := context.Background()

	require.NoError(t, u.SetConditionsReady(ctx, clusterPolicy, Reconciled, "ok"))
	require.NoError(t, u.SetConditionsError(ctx, clusterPolicy, DriverNotReady, "driver down"))

	got := &nvidiav1.ClusterPolicy{}
	require.NoError(t, c.Get(ctx, types.NamespacedName{Name: clusterPolicy.Name}, got))
	require.Len(t, got.Status.Conditions, 2)

	assert.Equal(t, metav1.ConditionFalse, got.Status.Conditions[0].Status) // Ready
	assert.Equal(t, metav1.ConditionTrue, got.Status.Conditions[1].Status)  // Error
	assert.Equal(t, DriverNotReady, got.Status.Conditions[1].Reason)
	assert.Equal(t, "driver down", got.Status.Conditions[1].Message)
}

// TestClusterPolicyUpdater_WrongObjectType covers the type-assertion guard in
// both public methods for objects that are not *nvidiav1.ClusterPolicy.
func TestClusterPolicyUpdater_WrongObjectType(t *testing.T) {
	c := newClusterPolicyClient(t)
	u := NewClusterPolicyUpdater(c)
	ctx := context.Background()

	methods := []struct {
		name string
		call func(any) error
	}{
		{"SetConditionsReady", func(o any) error { return u.SetConditionsReady(ctx, o, Reconciled, "m") }},
		{"SetConditionsError", func(o any) error { return u.SetConditionsError(ctx, o, ReconcileFailed, "m") }},
	}
	wrongObjects := []struct {
		name string
		obj  any
	}{
		{"string", "not-a-cluster-policy"},
		{"untyped nil", nil},
		{"unrelated pointer", &metav1.ObjectMeta{}},
		{"clusterpolicy list", &nvidiav1.ClusterPolicyList{}},
	}

	for _, m := range methods {
		for _, w := range wrongObjects {
			t.Run(m.name+"/"+w.name, func(t *testing.T) {
				err := m.call(w.obj)
				require.Error(t, err)
				assert.ErrorContains(t, err, "provided object is not a *nvidiav1.ClusterPolicy")
			})
		}
	}
}

// TestClusterPolicyUpdater_GetError covers the failure to fetch the latest
// instance (object does not exist), which is not a conflict and so is returned
// without retry.
func TestClusterPolicyUpdater_GetError(t *testing.T) {
	c := newClusterPolicyClient(t) // no objects seeded
	u := NewClusterPolicyUpdater(c)

	clusterPolicy := newClusterPolicy("missing")
	err := u.SetConditionsReady(context.Background(), clusterPolicy, Reconciled, "m")
	require.Error(t, err)
	assert.ErrorContains(t, err, "failed to get ClusterPolicy instance for status update")
}

// TestClusterPolicyUpdater_UnknownStatusType covers the default branch of the
// status-type switch, reachable only through the unexported setConditions.
func TestClusterPolicyUpdater_UnknownStatusType(t *testing.T) {
	clusterPolicy := newClusterPolicy("cluster-policy")
	c := newClusterPolicyClient(t, clusterPolicy)
	u := &clusterPolicyUpdater{client: c}

	err := u.setConditions(context.Background(), clusterPolicy, "BogusStatus", "reason", "message")
	require.Error(t, err)
	assert.ErrorContains(t, err, "unknown status type provided: BogusStatus")
}

// TestClusterPolicyUpdater_RetryOnConflict verifies that a Conflict error on the
// status update is retried and ultimately succeeds.
func TestClusterPolicyUpdater_RetryOnConflict(t *testing.T) {
	clusterPolicy := newClusterPolicy("cluster-policy")
	clusterPolicyScheme(t)

	var updateCalls int
	c := fake.NewClientBuilder().
		WithScheme(scheme.Scheme).
		WithObjects(clusterPolicy).
		WithStatusSubresource(clusterPolicy).
		WithInterceptorFuncs(interceptor.Funcs{
			SubResourceUpdate: func(ctx context.Context, cl client.Client, subResourceName string, obj client.Object, opts ...client.SubResourceUpdateOption) error {
				updateCalls++
				if updateCalls == 1 {
					return apierrors.NewConflict(
						schema.GroupResource{Group: "nvidia.com", Resource: "clusterpolicies"},
						obj.GetName(),
						errors.New("the object has been modified"),
					)
				}
				return cl.SubResource(subResourceName).Update(ctx, obj, opts...)
			},
		}).
		Build()
	u := NewClusterPolicyUpdater(c)

	err := u.SetConditionsReady(context.Background(), clusterPolicy, Reconciled, "m")
	require.NoError(t, err)
	assert.Equal(t, 2, updateCalls, "expected exactly one retry after the conflict")

	got := &nvidiav1.ClusterPolicy{}
	require.NoError(t, c.Get(context.Background(), types.NamespacedName{Name: clusterPolicy.Name}, got))
	assert.Len(t, got.Status.Conditions, 2)
}

// TestClusterPolicyUpdater_UpdateError verifies that a non-conflict error from
// the status update is propagated to the caller.
func TestClusterPolicyUpdater_UpdateError(t *testing.T) {
	clusterPolicy := newClusterPolicy("cluster-policy")
	clusterPolicyScheme(t)

	c := fake.NewClientBuilder().
		WithScheme(scheme.Scheme).
		WithObjects(clusterPolicy).
		WithStatusSubresource(clusterPolicy).
		WithInterceptorFuncs(interceptor.Funcs{
			SubResourceUpdate: func(ctx context.Context, cl client.Client, subResourceName string, obj client.Object, opts ...client.SubResourceUpdateOption) error {
				return errors.New("status update boom")
			},
		}).
		Build()
	u := NewClusterPolicyUpdater(c)

	err := u.SetConditionsError(context.Background(), clusterPolicy, ReconcileFailed, "m")
	require.Error(t, err)
	assert.ErrorContains(t, err, "status update boom")
}
