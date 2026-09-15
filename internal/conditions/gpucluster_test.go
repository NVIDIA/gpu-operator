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

func gpuClusterScheme(t *testing.T) *runtime.Scheme {
	t.Helper()
	scheme := runtime.NewScheme()
	require.NoError(t, nvidiav1alpha1.AddToScheme(scheme))
	return scheme
}

func newGPUClusterClient(t *testing.T, objects ...client.Object) client.Client {
	t.Helper()
	builder := fake.NewClientBuilder().WithScheme(gpuClusterScheme(t))
	if len(objects) > 0 {
		builder = builder.WithObjects(objects...).WithStatusSubresource(objects...)
	}
	return builder.Build()
}

func newGPUCluster(name string, state nvidiav1alpha1.State) *nvidiav1alpha1.GPUCluster {
	return &nvidiav1alpha1.GPUCluster{
		ObjectMeta: metav1.ObjectMeta{Name: name},
		Status:     nvidiav1alpha1.GPUClusterStatus{State: state},
	}
}

func getGPUCluster(t *testing.T, fakeClient client.Client, name string) *nvidiav1alpha1.GPUCluster {
	t.Helper()
	gpuCluster := &nvidiav1alpha1.GPUCluster{}
	require.NoError(t, fakeClient.Get(context.Background(), types.NamespacedName{Name: name}, gpuCluster))
	return gpuCluster
}

func assertGPUClusterConditions(t *testing.T, expectedConditions []metav1.Condition, gpuCluster *nvidiav1alpha1.GPUCluster) {
	t.Helper()
	diff := cmp.Diff(expectedConditions, gpuCluster.Status.Conditions,
		cmpopts.IgnoreFields(metav1.Condition{}, "LastTransitionTime", "ObservedGeneration"))
	assert.Empty(t, diff, "unexpected conditions (-expected +got):\n%s", diff)
}

func TestGPUClusterUpdater_New(t *testing.T) {
	updater := NewGPUClusterUpdater(newGPUClusterClient(t))
	assert.NotNil(t, updater)
	assert.IsType(t, &gpuClusterUpdater{}, updater)
}

func TestGPUClusterUpdater_WrongObjectType(t *testing.T) {
	updater := NewGPUClusterUpdater(newGPUClusterClient(t))
	ctx := context.Background()

	methods := []struct {
		name string
		call func(any) error
	}{
		{"SetConditionsReady", func(object any) error {
			return updater.SetConditionsReady(ctx, object, Reconciled, "m")
		}},
		{"SetConditionsError", func(object any) error {
			return updater.SetConditionsError(ctx, object, ReconcileFailed, "m")
		}},
	}
	wrongObjects := []struct {
		name   string
		object any
	}{
		{"string", "not-a-gpucluster"},
		{"untyped nil", nil},
		{"unrelated pointer", &metav1.ObjectMeta{}},
		{"gpucluster list", &nvidiav1alpha1.GPUClusterList{}},
		{"sibling cr", &nvidiav1alpha1.NVIDIADriver{}},
	}

	for _, method := range methods {
		for _, wrongObject := range wrongObjects {
			t.Run(method.name+"/"+wrongObject.name, func(t *testing.T) {
				err := method.call(wrongObject.object)
				require.Error(t, err)
				assert.ErrorContains(t, err, "provided object is not a *nvidiav1alpha1.GPUCluster")
			})
		}
	}
}

func TestGPUClusterUpdater_GetError(t *testing.T) {
	updater := NewGPUClusterUpdater(newGPUClusterClient(t))

	err := updater.SetConditionsReady(context.Background(), newGPUCluster("missing", ""), Reconciled, "m")
	require.Error(t, err)
	require.ErrorContains(t, err, "failed to get GPUCluster instance for status update")
	assert.True(t, apierrors.IsNotFound(err), "the NotFound cause must survive wrapping")
}

func TestGPUClusterUpdater_SetConditionsReady(t *testing.T) {
	gpuCluster := newGPUCluster("gpu-cluster", "")
	fakeClient := newGPUClusterClient(t, gpuCluster)
	updater := NewGPUClusterUpdater(fakeClient)

	require.NoError(t, updater.SetConditionsReady(context.Background(), gpuCluster, Reconciled, "all reconciled"))

	assertGPUClusterConditions(t, []metav1.Condition{
		{Type: Ready, Status: metav1.ConditionTrue, Reason: Reconciled, Message: "all reconciled"},
		{Type: Error, Status: metav1.ConditionFalse, Reason: Ready},
	}, getGPUCluster(t, fakeClient, gpuCluster.Name))
}

func TestGPUClusterUpdater_SetConditionsError(t *testing.T) {
	gpuCluster := newGPUCluster("gpu-cluster", nvidiav1alpha1.Ready)
	fakeClient := newGPUClusterClient(t, gpuCluster)
	updater := NewGPUClusterUpdater(fakeClient)

	require.NoError(t, updater.SetConditionsError(context.Background(), gpuCluster, OperandNotReady, "waiting for operands"))

	assertGPUClusterConditions(t, []metav1.Condition{
		{Type: Ready, Status: metav1.ConditionFalse, Reason: Error},
		{Type: Error, Status: metav1.ConditionTrue, Reason: OperandNotReady, Message: "waiting for operands"},
	}, getGPUCluster(t, fakeClient, gpuCluster.Name))
}

func TestGPUClusterUpdater_ReadyThenError(t *testing.T) {
	gpuCluster := newGPUCluster("gpu-cluster", nvidiav1alpha1.Ready)
	fakeClient := newGPUClusterClient(t, gpuCluster)
	updater := NewGPUClusterUpdater(fakeClient)
	ctx := context.Background()

	require.NoError(t, updater.SetConditionsReady(ctx, gpuCluster, Reconciled, "all reconciled"))
	require.NoError(t, updater.SetConditionsError(ctx, gpuCluster, PrerequisiteNotMet, "nfd labels missing"))

	assertGPUClusterConditions(t, []metav1.Condition{
		{Type: Ready, Status: metav1.ConditionFalse, Reason: Error},
		{Type: Error, Status: metav1.ConditionTrue, Reason: PrerequisiteNotMet, Message: "nfd labels missing"},
	}, getGPUCluster(t, fakeClient, gpuCluster.Name))
}

func TestGPUClusterUpdater_ErrorThenReady(t *testing.T) {
	gpuCluster := newGPUCluster("gpu-cluster", nvidiav1alpha1.NotReady)
	fakeClient := newGPUClusterClient(t, gpuCluster)
	updater := NewGPUClusterUpdater(fakeClient)
	ctx := context.Background()

	require.NoError(t, updater.SetConditionsError(ctx, gpuCluster, OperandNotReady, "waiting for operands"))
	require.NoError(t, updater.SetConditionsReady(ctx, gpuCluster, Reconciled, "all reconciled"))

	assertGPUClusterConditions(t, []metav1.Condition{
		{Type: Ready, Status: metav1.ConditionTrue, Reason: Reconciled, Message: "all reconciled"},
		{Type: Error, Status: metav1.ConditionFalse, Reason: Ready},
	}, getGPUCluster(t, fakeClient, gpuCluster.Name))
}

func TestGPUClusterUpdater_SetConditionsErrorStateResolution(t *testing.T) {
	for _, testCase := range []struct {
		name          string
		storedState   nvidiav1alpha1.State
		callerState   nvidiav1alpha1.State
		expectedState nvidiav1alpha1.State
	}{
		{"stored state wins over caller", nvidiav1alpha1.Ready, nvidiav1alpha1.Disabled, nvidiav1alpha1.Ready},
		{"stored state wins over empty caller", nvidiav1alpha1.Ready, "", nvidiav1alpha1.Ready},
		{"empty stored state falls back to caller", "", nvidiav1alpha1.Disabled, nvidiav1alpha1.Disabled},
		{"both empty defaults to notReady", "", "", nvidiav1alpha1.NotReady},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			fakeClient := newGPUClusterClient(t, newGPUCluster("gpu-cluster", testCase.storedState))
			updater := NewGPUClusterUpdater(fakeClient)

			err := updater.SetConditionsError(context.Background(),
				newGPUCluster("gpu-cluster", testCase.callerState), ReconcileFailed, "boom")
			require.NoError(t, err)

			storedCluster := getGPUCluster(t, fakeClient, "gpu-cluster")
			assert.Equal(t, testCase.expectedState, storedCluster.Status.State)
			assertGPUClusterConditions(t, []metav1.Condition{
				{Type: Ready, Status: metav1.ConditionFalse, Reason: Error},
				{Type: Error, Status: metav1.ConditionTrue, Reason: ReconcileFailed, Message: "boom"},
			}, storedCluster)
		})
	}
}

func TestGPUClusterUpdater_SetConditionsReadyLeavesStateAlone(t *testing.T) {
	for _, testCase := range []struct {
		name        string
		storedState nvidiav1alpha1.State
	}{
		{"empty stored state stays empty", ""},
		{"existing stored state is preserved", nvidiav1alpha1.Ready},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			fakeClient := newGPUClusterClient(t, newGPUCluster("gpu-cluster", testCase.storedState))
			updater := NewGPUClusterUpdater(fakeClient)

			err := updater.SetConditionsReady(context.Background(),
				newGPUCluster("gpu-cluster", nvidiav1alpha1.Disabled), Reconciled, "m")
			require.NoError(t, err)

			assert.Equal(t, testCase.storedState, getGPUCluster(t, fakeClient, "gpu-cluster").Status.State)
		})
	}
}

// The default branch is reachable only through the unexported setConditions.
func TestGPUClusterUpdater_UnknownStatusType(t *testing.T) {
	gpuCluster := newGPUCluster("gpu-cluster", "")
	updater := &gpuClusterUpdater{client: newGPUClusterClient(t, gpuCluster)}

	err := updater.setConditions(context.Background(), gpuCluster, "BogusStatus", "reason", "message")
	require.Error(t, err)
	assert.ErrorContains(t, err, "unknown status type provided: BogusStatus")
}

func TestGPUClusterUpdater_RetryOnConflict(t *testing.T) {
	gpuCluster := newGPUCluster("gpu-cluster", "")

	var getCalls, updateCalls int
	fakeClient := fake.NewClientBuilder().
		WithScheme(gpuClusterScheme(t)).
		WithObjects(gpuCluster).
		WithStatusSubresource(gpuCluster).
		WithInterceptorFuncs(interceptor.Funcs{
			Get: func(ctx context.Context, wrappedClient client.WithWatch, key client.ObjectKey, object client.Object, opts ...client.GetOption) error {
				getCalls++
				return wrappedClient.Get(ctx, key, object, opts...)
			},
			SubResourceUpdate: func(ctx context.Context, wrappedClient client.Client, subResourceName string, object client.Object, opts ...client.SubResourceUpdateOption) error {
				updateCalls++
				if updateCalls == 1 {
					return apierrors.NewConflict(
						schema.GroupResource{Group: "nvidia.com", Resource: "gpuclusters"},
						object.GetName(),
						errors.New("the object has been modified"),
					)
				}
				return wrappedClient.SubResource(subResourceName).Update(ctx, object, opts...)
			},
		}).
		Build()
	updater := NewGPUClusterUpdater(fakeClient)

	err := updater.SetConditionsReady(context.Background(), gpuCluster, Reconciled, "m")
	require.NoError(t, err)
	assert.Equal(t, 2, updateCalls, "expected exactly one retry after the conflict")
	assert.Equal(t, 2, getCalls, "every attempt must refetch the instance")

	assertGPUClusterConditions(t, []metav1.Condition{
		{Type: Ready, Status: metav1.ConditionTrue, Reason: Reconciled, Message: "m"},
		{Type: Error, Status: metav1.ConditionFalse, Reason: Ready},
	}, getGPUCluster(t, fakeClient, gpuCluster.Name))
}

func TestGPUClusterUpdater_UpdateError(t *testing.T) {
	gpuCluster := newGPUCluster("gpu-cluster", "")

	var updateCalls int
	fakeClient := fake.NewClientBuilder().
		WithScheme(gpuClusterScheme(t)).
		WithObjects(gpuCluster).
		WithStatusSubresource(gpuCluster).
		WithInterceptorFuncs(interceptor.Funcs{
			SubResourceUpdate: func(_ context.Context, _ client.Client, _ string, _ client.Object, _ ...client.SubResourceUpdateOption) error {
				updateCalls++
				return errors.New("status update boom")
			},
		}).
		Build()
	updater := NewGPUClusterUpdater(fakeClient)

	err := updater.SetConditionsError(context.Background(), gpuCluster, ReconcileFailed, "m")
	require.Error(t, err)
	require.ErrorContains(t, err, "status update boom")
	assert.Equal(t, 1, updateCalls, "a non-conflict error must not be retried")
}
