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

type gpuClusterConfigUpdater struct {
	client client.Client
}

func NewGPUClusterConfigUpdater(client client.Client) Updater {
	return &gpuClusterConfigUpdater{client: client}
}

func (u *gpuClusterConfigUpdater) SetConditionsReady(ctx context.Context, cr any, reason, message string) error {
	gpuClusterConfigCr, ok := cr.(*nvidiav1alpha1.GPUClusterConfig)
	if !ok {
		return fmt.Errorf("provided object is not a *nvidiav1alpha1.GPUClusterConfig")
	}
	return u.setConditions(ctx, gpuClusterConfigCr, Ready, reason, message)
}

func (u *gpuClusterConfigUpdater) SetConditionsError(ctx context.Context, cr any, reason, message string) error {
	gpuClusterConfigCr, ok := cr.(*nvidiav1alpha1.GPUClusterConfig)
	if !ok {
		return fmt.Errorf("provided object is not a *nvidiav1alpha1.GPUClusterConfig")
	}
	return u.setConditions(ctx, gpuClusterConfigCr, Error, reason, message)
}

func (u *gpuClusterConfigUpdater) updateConditions(ctx context.Context, cr *nvidiav1alpha1.GPUClusterConfig, statusType, reason, message string) error {
	// Refetch to avoid a resourceVersion conflict.
	instance := &nvidiav1alpha1.GPUClusterConfig{}
	if err := u.client.Get(ctx, types.NamespacedName{Name: cr.Name}, instance); err != nil {
		return fmt.Errorf("failed to get GPUClusterConfig instance for status update: %w", err)
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

		if instance.Status.State == "" {
			instance.Status.State = cr.Status.State
			if instance.Status.State == "" {
				instance.Status.State = nvidiav1alpha1.NotReady
			}
		}
	default:
		return fmt.Errorf("unknown status type provided: %s", statusType)
	}

	return u.client.Status().Update(ctx, instance)
}

// setConditions retries on conflict to absorb concurrent status writes.
func (u *gpuClusterConfigUpdater) setConditions(ctx context.Context, cr *nvidiav1alpha1.GPUClusterConfig, statusType, reason, message string) error {
	reqLogger := log.FromContext(ctx)

	err := retry.RetryOnConflict(retry.DefaultBackoff, func() error {
		return u.updateConditions(ctx, cr, statusType, reason, message)
	})

	if err != nil {
		reqLogger.Error(err, "Failed to update GPUClusterConfig status after retries", "name", cr.Name)
	}
	return err
}
