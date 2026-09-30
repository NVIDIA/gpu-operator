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
	"testing"

	"github.com/stretchr/testify/require"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"

	nvidiav1 "github.com/NVIDIA/gpu-operator/api/nvidia/v1"
	nvidiav1alpha1 "github.com/NVIDIA/gpu-operator/api/nvidia/v1alpha1"
)

func TestConditionWritersSetObservedGeneration(t *testing.T) {
	const generation int64 = 7

	t.Run("ClusterPolicy", func(t *testing.T) {
		object := newClusterPolicy("cluster-policy")
		object.Generation = generation
		client := newClusterPolicyClient(t, object)
		require.NoError(t, NewClusterPolicyUpdater(client).SetConditionsReady(context.Background(), object, Reconciled, "ready"))

		got := &nvidiav1.ClusterPolicy{}
		require.NoError(t, client.Get(context.Background(), objectKey(object.Name), got))
		assertObservedGenerations(t, got.Status.Conditions, generation)

		errorObject := newClusterPolicy("cluster-policy-error")
		errorObject.Generation = generation
		errorClient := newClusterPolicyClient(t, errorObject)
		require.NoError(t, NewClusterPolicyUpdater(errorClient).SetConditionsError(context.Background(), errorObject, Reconciled, "error"))

		errorGot := &nvidiav1.ClusterPolicy{}
		require.NoError(t, errorClient.Get(context.Background(), objectKey(errorObject.Name), errorGot))
		assertObservedGenerations(t, errorGot.Status.Conditions, generation)
	})

	t.Run("NVIDIADriver", func(t *testing.T) {
		object := newNvDriver("gpu-driver")
		object.Generation = generation
		client := newNvDriverClient(t, object)
		require.NoError(t, NewNvDriverUpdater(client).SetConditionsReady(context.Background(), object, Reconciled, "ready"))

		got := &nvidiav1alpha1.NVIDIADriver{}
		require.NoError(t, client.Get(context.Background(), objectKey(object.Name), got))
		assertObservedGenerations(t, got.Status.Conditions, generation)

		errorObject := newNvDriver("gpu-driver-error")
		errorObject.Generation = generation
		errorClient := newNvDriverClient(t, errorObject)
		require.NoError(t, NewNvDriverUpdater(errorClient).SetConditionsError(context.Background(), errorObject, Reconciled, "error"))

		errorGot := &nvidiav1alpha1.NVIDIADriver{}
		require.NoError(t, errorClient.Get(context.Background(), objectKey(errorObject.Name), errorGot))
		assertObservedGenerations(t, errorGot.Status.Conditions, generation)
	})

	t.Run("GPUCluster", func(t *testing.T) {
		object := newGPUCluster("gpu-cluster", "")
		object.Generation = generation
		client := newGPUClusterClient(t, object)
		require.NoError(t, NewGPUClusterUpdater(client).SetConditionsReady(context.Background(), object, Reconciled, "ready"))

		got := getGPUCluster(t, client, object.Name)
		assertObservedGenerations(t, got.Status.Conditions, generation)

		errorObject := newGPUCluster("gpu-cluster-error", "")
		errorObject.Generation = generation
		errorClient := newGPUClusterClient(t, errorObject)
		require.NoError(t, NewGPUClusterUpdater(errorClient).SetConditionsError(context.Background(), errorObject, Reconciled, "error"))

		errorGot := getGPUCluster(t, errorClient, errorObject.Name)
		assertObservedGenerations(t, errorGot.Status.Conditions, generation)
	})
}

func objectKey(name string) types.NamespacedName {
	return types.NamespacedName{Name: name}
}

func assertObservedGenerations(t *testing.T, conditions []metav1.Condition, generation int64) {
	t.Helper()
	require.Len(t, conditions, 2)
	for _, condition := range conditions {
		require.Equal(t, generation, condition.ObservedGeneration)
	}
}
