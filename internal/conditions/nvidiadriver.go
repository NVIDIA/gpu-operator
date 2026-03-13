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
	"fmt"

	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/util/retry"
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
	nvDriverCr, ok := cr.(*nvidiav1alpha1.NVIDIADriver)
	if !ok {
		return fmt.Errorf("provided object is not a *nvidiav1alpha1.NVIDIADriver")
	}
	return u.setConditions(ctx, nvDriverCr, Ready, reason, message)
}

func (u *nvDriverUpdater) SetConditionsError(ctx context.Context, cr any, reason, message string) error {
	nvDriverCr, ok := cr.(*nvidiav1alpha1.NVIDIADriver)
	if !ok {
		return fmt.Errorf("provided object is not a *nvidiav1alpha1.NVIDIADriver")
	}
	return u.setConditions(ctx, nvDriverCr, Error, reason, message)
}

// updateConditions updates the conditions of the NVIDIADriver CR
func (u *nvDriverUpdater) updateConditions(ctx context.Context, cr *nvidiav1alpha1.NVIDIADriver, statusType, reason, message string) error {
	// Fetch latest instance and update state to avoid version mismatch
	instance := &nvidiav1alpha1.NVIDIADriver{}
	if err := u.client.Get(ctx, types.NamespacedName{Name: cr.Name}, instance); err != nil {
		return fmt.Errorf("failed to get NVIDIADriver instance for status update: %w", err)
	}

	switch statusType {
	case Ready:
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
	case Error:
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
	default:
		return fmt.Errorf("unknown status type provided: %s", statusType)
	}

	return u.client.Status().Update(ctx, instance)
}

// setConditions updates the conditions of the NVIDIADriver CR
// with retry on conflict to handle version mismatches
func (u *nvDriverUpdater) setConditions(ctx context.Context, cr *nvidiav1alpha1.NVIDIADriver, statusType, reason, message string) error {
	reqLogger := log.FromContext(ctx)

	err := retry.RetryOnConflict(retry.DefaultBackoff, func() error {
		return u.updateConditions(ctx, cr, statusType, reason, message)
	})

	if err != nil {
		reqLogger.Error(err, "Failed to update NVIDIADriver status after retries", "name", cr.Name)
	}
	return err
}
