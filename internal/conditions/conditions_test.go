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

	"github.com/stretchr/testify/assert"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/kubernetes/scheme"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	nvidiav1alpha1 "github.com/NVIDIA/gpu-operator/api/nvidia/v1alpha1"
)

func TestConditionsUpdater_SetConditionsReady(t *testing.T) {
	driver := &nvidiav1alpha1.NVIDIADriver{ObjectMeta: metav1.ObjectMeta{Name: "gpu-driver"}}
	s := scheme.Scheme
	_ = nvidiav1alpha1.AddToScheme(s)
	c := fake.
		NewClientBuilder().
		WithScheme(s).
		WithObjects(driver).
		WithStatusSubresource(driver).
		Build()
	u := NewNvDriverUpdater(c)

	expectedReady := metav1.Condition{
		Type:    "Ready",
		Status:  metav1.ConditionTrue,
		Reason:  "Reconciled",
		Message: "All resources are successfully reconciled",
	}
	expectedError := metav1.Condition{
		Type:   "Error",
		Status: metav1.ConditionFalse,
		Reason: "Ready",
	}

	err := u.SetConditionsReady(context.Background(), driver, Reconciled, "All resources are successfully reconciled")
	assert.NoError(t, err)

	instance := &nvidiav1alpha1.NVIDIADriver{}
	err = c.Get(context.Background(), types.NamespacedName{Name: driver.Name}, instance)
	assert.NoError(t, err)

	assert.Len(t, instance.Status.Conditions, 2)

	assert.Equal(t, expectedReady.Type, instance.Status.Conditions[0].Type)
	assert.Equal(t, expectedReady.Status, instance.Status.Conditions[0].Status)
	assert.Equal(t, expectedReady.Reason, instance.Status.Conditions[0].Reason)
	assert.Equal(t, expectedReady.Message, instance.Status.Conditions[0].Message)

	assert.Equal(t, expectedError.Type, instance.Status.Conditions[1].Type)
	assert.Equal(t, expectedError.Status, instance.Status.Conditions[1].Status)
	assert.Equal(t, expectedError.Reason, instance.Status.Conditions[1].Reason)
}

func TestConditionsUpdater_SetConditionsErrored(t *testing.T) {
	driver := &nvidiav1alpha1.NVIDIADriver{ObjectMeta: metav1.ObjectMeta{Name: "gpu-driver"}}
	s := scheme.Scheme
	_ = nvidiav1alpha1.AddToScheme(s)
	c := fake.
		NewClientBuilder().
		WithScheme(s).
		WithObjects(driver).
		WithStatusSubresource(driver).
		Build()
	u := NewNvDriverUpdater(c)

	expectedReady := metav1.Condition{
		Type:   "Ready",
		Status: metav1.ConditionFalse,
		Reason: "Error",
	}
	expectedError := metav1.Condition{
		Type:    "Error",
		Status:  metav1.ConditionTrue,
		Reason:  ConflictingNodeSelector,
		Message: "Conflicting nodes found with given node selector label",
	}

	err := u.SetConditionsError(context.Background(), driver, ConflictingNodeSelector, "Conflicting nodes found with given node selector label")
	assert.NoError(t, err)

	instance := &nvidiav1alpha1.NVIDIADriver{}
	err = c.Get(context.Background(), types.NamespacedName{Name: driver.Name}, instance)
	assert.NoError(t, err)

	assert.Len(t, instance.Status.Conditions, 2)

	assert.Equal(t, expectedReady.Type, instance.Status.Conditions[0].Type)
	assert.Equal(t, expectedReady.Status, instance.Status.Conditions[0].Status)
	assert.Equal(t, expectedReady.Reason, instance.Status.Conditions[0].Reason)

	assert.Equal(t, expectedError.Type, instance.Status.Conditions[1].Type)
	assert.Equal(t, expectedError.Status, instance.Status.Conditions[1].Status)
	assert.Equal(t, expectedError.Reason, instance.Status.Conditions[1].Reason)
	assert.Equal(t, expectedError.Message, instance.Status.Conditions[1].Message)
}
