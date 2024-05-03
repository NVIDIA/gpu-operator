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

	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/log"

	nvidiav1alpha1 "github.com/NVIDIA/gpu-operator/api/nvidia/v1alpha1"
)

const (
	// ConflictingNodeSelector indicates that the nodeSelector of the NVIDIADriver instance
	// is leading to conflicting nodes with another instance.
	ConflictingNodeSelector = "ConflictingNodeSelector"
)

// Specific implementation of the Updater interface for one of our controllers
type nvDriverUpdater struct {
	client client.Client
}

// NewNvDriverUpdater returns an instance to update conditions for NVIDIADriver
func NewNvDriverUpdater(client client.Client) Updater {
	return &nvDriverUpdater{client: client}
}

func (u *nvDriverUpdater) SetConditionsReady(ctx context.Context, cr any, reason, message string) error {
	nvDriverCr, _ := cr.(*nvidiav1alpha1.NVIDIADriver)
	return u.setConditionsReady(ctx, nvDriverCr, reason, message)
}

func (u *nvDriverUpdater) SetConditionsError(ctx context.Context, cr any, reason, message string) error {
	nvDriverCr, _ := cr.(*nvidiav1alpha1.NVIDIADriver)
	return u.setConditionsError(ctx, nvDriverCr, reason, message)
}

func (u *nvDriverUpdater) setConditionsReady(ctx context.Context, cr *nvidiav1alpha1.NVIDIADriver, reason, message string) error {
	reqLogger := log.FromContext(ctx)
	// Fetch latest instance and update state to avoid version mismatch
	instance := &nvidiav1alpha1.NVIDIADriver{}
	err := u.client.Get(ctx, types.NamespacedName{Name: cr.Name}, instance)
	if err != nil {
		reqLogger.Error(err, "Failed to get NVIDIADriver instance for status update", "name", cr.Name)
		return err
	}

	meta.SetStatusCondition(&instance.Status.Conditions, metav1.Condition{
		Type:    Ready,
		Status:  metav1.ConditionTrue,
		Reason:  reason,
		Message: message,
	})

	meta.SetStatusCondition(&instance.Status.Conditions, metav1.Condition{
		Type:   Error,
		Status: metav1.ConditionFalse,
		Reason: Ready,
	})

	return u.client.Status().Update(ctx, instance)
}

func (u *nvDriverUpdater) setConditionsError(ctx context.Context, cr *nvidiav1alpha1.NVIDIADriver, reason, message string) error {
	reqLogger := log.FromContext(ctx)
	// Fetch latest instance and update state to avoid version mismatch
	instance := &nvidiav1alpha1.NVIDIADriver{}
	err := u.client.Get(ctx, types.NamespacedName{Name: cr.Name}, instance)
	if err != nil {
		reqLogger.Error(err, "Failed to get NVIDIADriver instance for status update", "name", cr.Name)
	}

	meta.SetStatusCondition(&instance.Status.Conditions, metav1.Condition{
		Type:   Ready,
		Status: metav1.ConditionFalse,
		Reason: Error,
	})

	meta.SetStatusCondition(&instance.Status.Conditions, metav1.Condition{
		Type:    Error,
		Status:  metav1.ConditionTrue,
		Reason:  reason,
		Message: message,
	})

	// Ensure status.state is not empty when updating the CR status.
	// The caller should set the state appropriately in the CR
	// depending on the error condition.
	instance.Status.State = cr.Status.State
	if instance.Status.State == "" {
		instance.Status.State = nvidiav1alpha1.NotReady
	}

	return u.client.Status().Update(ctx, instance)
}
