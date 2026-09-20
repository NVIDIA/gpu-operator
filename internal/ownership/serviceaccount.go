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

// Package ownership holds the rules deciding whether an object belongs to one
// operand of the CR being reconciled.
//
// Both the ClusterPolicy and the GPUCluster controller set their own controller
// reference on every operand object they create, so ownership alone answers "does this
// CR own it", never "which operand created it". Where an object's name is
// user-configurable -- the DCGM Exporter ServiceAccount -- that distinction decides
// whether a configured name is a fresh account to create, an account to adopt, or a
// sibling operand's account that must be left alone. A Marker supplies the missing
// half.
package ownership

import (
	"context"
	"fmt"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
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

// Selector returns the marker as a list option, for finding every object the operand
// created regardless of the name a previous configuration gave it.
func (m Marker) Selector() client.MatchingLabels {
	return client.MatchingLabels{m.Key: m.Value}
}

// IsManaged reports whether obj is an object this operand created for owner: it carries
// the operand's marker and owner is its controller. Both halves are required -- the
// marker alone would match an object another CR created for the same operand, and the
// controller reference alone would match every other operand of this CR.
func IsManaged(obj metav1.Object, owner metav1.Object, m Marker) bool {
	return m.Matches(obj.GetLabels()) && metav1.IsControlledBy(obj, owner)
}

// ReleaseOwner drops owner's reference from obj, handing the object to whoever is meant
// to own it now. Returns whether anything changed.
func ReleaseOwner(obj metav1.Object, ownerUID types.UID) bool {
	refs := obj.GetOwnerReferences()
	kept := make([]metav1.OwnerReference, 0, len(refs))
	for _, ref := range refs {
		if ref.UID == ownerUID {
			continue
		}
		kept = append(kept, ref)
	}
	if len(kept) == len(refs) {
		return false
	}
	obj.SetOwnerReferences(kept)
	return true
}

// ConflictError reports a configured ServiceAccount name that names an object this
// operand does not manage -- an unrelated account, or one belonging to a sibling
// operand. Creating it is refused rather than silently adopting an identity the
// operand did not provision.
func ConflictError(ownerKind, name, namespace string) error {
	return fmt.Errorf(
		"ServiceAccount %q already exists in namespace %q and is not managed by the DCGM Exporter of this %s; "+
			"set dcgmExporter.serviceAccount.create to false to reference it",
		name, namespace, ownerKind)
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
