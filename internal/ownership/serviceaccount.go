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
	"sigs.k8s.io/controller-runtime/pkg/client"
)

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
