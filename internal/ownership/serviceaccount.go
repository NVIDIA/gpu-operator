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

// Package ownership protects operator-managed objects during cleanup.
package ownership

import (
	"context"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// Marker is the label identifying the objects one operand created. Each controller
// carries its own: the label has to be one the operator already applied before this
// policy existed, so that objects created by an earlier release are still recognized
// after an upgrade.
type Marker struct {
	Key   string
	Value string
}

// Matches reports whether the labels carry this marker.
func (m Marker) Matches(labels map[string]string) bool {
	return labels[m.Key] == m.Value
}

// Apply stamps the marker onto an object the operand is about to create.
func (m Marker) Apply(obj metav1.Object) {
	labels := obj.GetLabels()
	if labels == nil {
		labels = map[string]string{}
	}
	labels[m.Key] = m.Value
	obj.SetLabels(labels)
}

// IsManaged reports whether obj is an object this operand created for owner: it carries
// the operand's marker and owner is its controller. Both halves are required -- the
// marker alone would match an object another CR created for the same operand, and the
// controller reference alone would match every other operand of this CR.
func IsManaged(obj metav1.Object, owner metav1.Object, m Marker) bool {
	return m.Matches(obj.GetLabels()) && metav1.IsControlledBy(obj, owner)
}

// DeleteObserved deletes only the object version whose ownership was checked. UID
// protects a replacement with the same name; resourceVersion protects a concurrent
// ownership hand-off on the same object. Return conflicts to schedule a retry: a
// metadata update can conflict even when the object is still ours to delete.
func DeleteObserved(ctx context.Context, c client.Client, obj client.Object) error {
	uid, version := obj.GetUID(), obj.GetResourceVersion()
	err := c.Delete(ctx, obj, client.Preconditions{UID: &uid, ResourceVersion: &version})
	if apierrors.IsNotFound(err) {
		return nil
	}
	return err
}
