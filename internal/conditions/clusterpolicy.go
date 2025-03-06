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
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/log"

	nvidiav1 "github.com/NVIDIA/gpu-operator/api/nvidia/v1"
)

// Specific implementation of the Updater interface for one of our controllers
type clusterPolicyUpdater struct {
	client client.Client
}

// NewClusterPolicyUpdater returns an instance to update conditions for ClusterPolicy
func NewClusterPolicyUpdater(client client.Client) Updater {
	return &clusterPolicyUpdater{client: client}
}

func (u *clusterPolicyUpdater) SetConditionsReady(ctx context.Context, cr any, reason, message string) error {
	clusterPolicyCr, ok := cr.(*nvidiav1.ClusterPolicy)
	if !ok {
		return fmt.Errorf("provided object is not a *nvidiav1.ClusterPolicy")
	}
	return u.setConditions(ctx, clusterPolicyCr, Ready, reason, message)
}

func (u *clusterPolicyUpdater) SetConditionsError(ctx context.Context, cr any, reason, message string) error {
	clusterPolicyCr, ok := cr.(*nvidiav1.ClusterPolicy)
	if !ok {
		return fmt.Errorf("provided object is not a *nvidiav1.ClusterPolicy")
	}
	return u.setConditions(ctx, clusterPolicyCr, Error, reason, message)
}

func (u *clusterPolicyUpdater) setConditions(ctx context.Context, cr *nvidiav1.ClusterPolicy, statusType, reason, message string) error {
	reqLogger := log.FromContext(ctx)
	// Fetch latest instance and update state to avoid version mismatch
	instance := &nvidiav1.ClusterPolicy{}
	err := u.client.Get(ctx, types.NamespacedName{Name: cr.Name}, instance)
	if err != nil {
		reqLogger.Error(err, "Failed to get ClusterPolicy instance for status update", "name", cr.Name)
		return err
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
	default:
		reqLogger.Error(nil, "Unknown status type provided", "statusType", statusType)
		return fmt.Errorf("unknown status type provided: %s", statusType)
	}

	return u.client.Status().Update(ctx, instance)
}
