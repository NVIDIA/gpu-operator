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

	"github.com/google/go-cmp/cmp"
	"github.com/google/go-cmp/cmp/cmpopts"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/client/interceptor"

	nvidiav1alpha1 "github.com/NVIDIA/gpu-operator/api/nvidia/v1alpha1"
)

func nvDriverScheme(t *testing.T) *runtime.Scheme {
	t.Helper()
	s := runtime.NewScheme()
	require.NoError(t, nvidiav1alpha1.AddToScheme(s))
	return s
}

func newNvDriverClient(t *testing.T, objs ...client.Object) client.Client {
	t.Helper()
	b := fake.NewClientBuilder().WithScheme(nvDriverScheme(t))
	if len(objs) > 0 {
		b = b.WithObjects(objs...).WithStatusSubresource(objs...)
	}
	return b.Build()
}

func newNvDriver(name string) *nvidiav1alpha1.NVIDIADriver {
	return &nvidiav1alpha1.NVIDIADriver{ObjectMeta: metav1.ObjectMeta{Name: name}}
}

func TestNvDriverUpdater_New(t *testing.T) {
	u := NewNvDriverUpdater(newNvDriverClient(t))
	assert.NotNil(t, u)
	assert.IsType(t, &nvDriverUpdater{}, u)
}

func TestNvDriverUpdater_WrongObjectType(t *testing.T) {
	c := newNvDriverClient(t)
	u := NewNvDriverUpdater(c)
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
		{"string", "not-a-driver"},
		{"untyped nil", nil},
		{"unrelated pointer", &metav1.ObjectMeta{}},
		{"driver list", &nvidiav1alpha1.NVIDIADriverList{}},
	}

	for _, m := range methods {
		for _, w := range wrongObjects {
			t.Run(m.name+"/"+w.name, func(t *testing.T) {
				err := m.call(w.obj)
				require.Error(t, err)
				assert.ErrorContains(t, err, "provided object is not a *nvidiav1alpha1.NVIDIADriver")
			})
		}
	}
}

func TestNvDriverUpdater_GetError(t *testing.T) {
	c := newNvDriverClient(t)
	u := NewNvDriverUpdater(c)

	err := u.SetConditionsReady(context.Background(), newNvDriver("missing"), Reconciled, "m")
	require.Error(t, err)
	assert.ErrorContains(t, err, "failed to get NVIDIADriver instance for status update")
}

// The default branch is reachable only through the unexported setConditions.
func TestNvDriverUpdater_UnknownStatusType(t *testing.T) {
	drv := newNvDriver("gpu-driver")
	c := newNvDriverClient(t, drv)
	u := &nvDriverUpdater{client: c}

	err := u.setConditions(context.Background(), drv, "BogusStatus", "reason", "message")
	require.Error(t, err)
	assert.ErrorContains(t, err, "unknown status type provided: BogusStatus")
}

func TestNvDriverUpdater_SetConditionsErrorDefaultsState(t *testing.T) {
	drv := newNvDriver("gpu-driver")
	c := newNvDriverClient(t, drv)
	u := NewNvDriverUpdater(c)

	err := u.SetConditionsError(context.Background(), drv, ConflictingNodeSelector, "conflicting nodes")
	require.NoError(t, err)

	got := &nvidiav1alpha1.NVIDIADriver{}
	require.NoError(t, c.Get(context.Background(), types.NamespacedName{Name: drv.Name}, got))

	assert.Equal(t, nvidiav1alpha1.NotReady, got.Status.State)

	want := []metav1.Condition{
		{Type: Ready, Status: metav1.ConditionFalse, Reason: Error},
		{Type: Error, Status: metav1.ConditionTrue, Reason: ConflictingNodeSelector, Message: "conflicting nodes"},
	}
	diff := cmp.Diff(want, got.Status.Conditions,
		cmpopts.IgnoreFields(metav1.Condition{}, "LastTransitionTime", "ObservedGeneration"))
	assert.Empty(t, diff, "unexpected conditions (-want +got):\n%s", diff)
}

func TestNvDriverUpdater_SetConditionsErrorPreservesState(t *testing.T) {
	drv := newNvDriver("gpu-driver")
	drv.Status.State = nvidiav1alpha1.Ready
	c := newNvDriverClient(t, drv)
	u := NewNvDriverUpdater(c)

	err := u.SetConditionsError(context.Background(), drv, ConflictingNodeSelector, "conflicting nodes")
	require.NoError(t, err)

	got := &nvidiav1alpha1.NVIDIADriver{}
	require.NoError(t, c.Get(context.Background(), types.NamespacedName{Name: drv.Name}, got))
	assert.Equal(t, nvidiav1alpha1.Ready, got.Status.State)
}

func TestNvDriverUpdater_RetryOnConflict(t *testing.T) {
	drv := newNvDriver("gpu-driver")

	var updateCalls int
	c := fake.NewClientBuilder().
		WithScheme(nvDriverScheme(t)).
		WithObjects(drv).
		WithStatusSubresource(drv).
		WithInterceptorFuncs(interceptor.Funcs{
			SubResourceUpdate: func(ctx context.Context, cl client.Client, subResourceName string, obj client.Object, opts ...client.SubResourceUpdateOption) error {
				updateCalls++
				if updateCalls == 1 {
					return apierrors.NewConflict(
						schema.GroupResource{Group: "nvidia.com", Resource: "nvidiadrivers"},
						obj.GetName(),
						errors.New("the object has been modified"),
					)
				}
				return cl.SubResource(subResourceName).Update(ctx, obj, opts...)
			},
		}).
		Build()
	u := NewNvDriverUpdater(c)

	err := u.SetConditionsReady(context.Background(), drv, Reconciled, "m")
	require.NoError(t, err)
	assert.Equal(t, 2, updateCalls, "expected exactly one retry after the conflict")

	got := &nvidiav1alpha1.NVIDIADriver{}
	require.NoError(t, c.Get(context.Background(), types.NamespacedName{Name: drv.Name}, got))
	assert.Len(t, got.Status.Conditions, 2)
}

func TestNvDriverUpdater_UpdateError(t *testing.T) {
	drv := newNvDriver("gpu-driver")

	c := fake.NewClientBuilder().
		WithScheme(nvDriverScheme(t)).
		WithObjects(drv).
		WithStatusSubresource(drv).
		WithInterceptorFuncs(interceptor.Funcs{
			SubResourceUpdate: func(ctx context.Context, cl client.Client, subResourceName string, obj client.Object, opts ...client.SubResourceUpdateOption) error {
				return errors.New("status update boom")
			},
		}).
		Build()
	u := NewNvDriverUpdater(c)

	err := u.SetConditionsReady(context.Background(), drv, Reconciled, "m")
	require.Error(t, err)
	assert.ErrorContains(t, err, "status update boom")
}
