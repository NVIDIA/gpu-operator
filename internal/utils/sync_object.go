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

package utils

import (
	"context"
	"fmt"

	corev1 "k8s.io/api/core/v1"
	rbacv1 "k8s.io/api/rbac/v1"
	"k8s.io/apimachinery/pkg/api/equality"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/apiutil"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/log"
)

// SyncOptions changes how SyncObject writes kinds that need special handling.
type SyncOptions struct {
	// CreateOnly keeps an existing object unchanged.
	CreateOnly bool
	// Use PreserveLiveFields to copy fields from the live object that the API server sets
	// and that an update must not clear, such as the ClusterIP of a Service.
	// Runs only when an update is to be made.
	PreserveLiveFields func(desired, live client.Object)
}

// SyncObject makes the cluster match desired. If shouldExist is false, SyncObject deletes
// the object. Otherwise, it adds a controlling owner reference from desired to owner, so
// that deleting owner deletes desired. Then it creates the object, or updates it unless
// ContentEqual reports no change or opts say to keep it. live must be an empty object of
// the same type as desired.
func SyncObject(ctx context.Context, c client.Client, owner, desired, live client.Object, shouldExist bool, opts SyncOptions) error {
	logger := log.FromContext(ctx).WithValues("Kind", kindOf(c, desired), "Name", desired.GetName(), "Namespace", desired.GetNamespace())
	key := client.ObjectKeyFromObject(desired)

	if !shouldExist {
		if err := c.Get(ctx, key, live); err != nil {
			if apierrors.IsNotFound(err) {
				return nil
			}
			return err
		}
		logger.Info("Deleting")
		if err := c.Delete(ctx, live); err != nil && !apierrors.IsNotFound(err) {
			logger.Info("Couldn't delete", "Error", err)
			return err
		}
		return nil
	}

	if err := controllerutil.SetControllerReference(owner, desired, c.Scheme()); err != nil {
		return err
	}

	// Read from cache
	err := c.Get(ctx, key, live)
	if apierrors.IsNotFound(err) {
		logger.Info("Not found, creating")
		if err := c.Create(ctx, desired); err != nil {
			logger.Info("Couldn't create", "Error", err)
			return err
		}
		return nil
	}
	if err != nil {
		return err
	}

	if opts.CreateOnly {
		return nil
	}

	if ContentEqual(c.Scheme(), desired, live) {
		logger.Info("Identical, skipping update")
		return nil
	}

	if opts.PreserveLiveFields != nil {
		opts.PreserveLiveFields(desired, live)
	}
	desired.SetResourceVersion(live.GetResourceVersion())

	logger.Info("Different, updating")
	if err := c.Update(ctx, desired); err != nil {
		logger.Info("Couldn't update", "Error", err)
		return err
	}
	return nil
}

func kindOf(c client.Client, obj client.Object) string {
	gvk, err := c.GroupVersionKindFor(obj)
	if err != nil {
		return fmt.Sprintf("%T", obj)
	}
	return gvk.Kind
}

// PreserveServiceClusterIP copies the ClusterIP that the API server assigned to a Service,
// because an update must not change it.
func PreserveServiceClusterIP(desired, live client.Object) {
	desired.(*corev1.Service).Spec.ClusterIP = live.(*corev1.Service).Spec.ClusterIP
}

// contentEqualKinds lists the kinds that ContentEqual compares. It holds only kinds whose
// content the API server stores exactly as sent, so a live object matches the desired
// object when nothing has changed.
var contentEqualKinds = map[schema.GroupKind]bool{
	{Group: rbacv1.GroupName, Kind: "Role"}:               true,
	{Group: rbacv1.GroupName, Kind: "ClusterRole"}:        true,
	{Group: rbacv1.GroupName, Kind: "RoleBinding"}:        true,
	{Group: rbacv1.GroupName, Kind: "ClusterRoleBinding"}: true,
	{Group: corev1.GroupName, Kind: "ConfigMap"}:          true,
	// ServiceMonitor and PrometheusRule are left out because their CRDs come from the
	// Prometheus installation, so live objects can have fields and defaults that the
	// vendored types do not.
	// SecurityContextConstraints is left out because OpenShift fills in fields that the
	// manifests leave empty, so a live SCC would never match its manifest.
}

// ContentEqual reports whether live has the labels, annotations, owner references, and
// content of desired. desired and live can be typed or unstructured. For a kind that is not
// in contentEqualKinds, ContentEqual returns false, because the API server adds defaults that
// desired does not have.
func ContentEqual(scheme *runtime.Scheme, desired, live client.Object) bool {
	gvk, err := apiutil.GVKForObject(desired, scheme)
	if err != nil || !contentEqualKinds[gvk.GroupKind()] {
		return false
	}
	if !equality.Semantic.DeepEqual(desired.GetLabels(), live.GetLabels()) ||
		!equality.Semantic.DeepEqual(desired.GetAnnotations(), live.GetAnnotations()) ||
		!equality.Semantic.DeepEqual(desired.GetOwnerReferences(), live.GetOwnerReferences()) {
		return false
	}
	desiredContent, err := storedContent(scheme, gvk, desired)
	if err != nil {
		return false
	}
	liveContent, err := storedContent(scheme, gvk, live)
	if err != nil {
		return false
	}
	return equality.Semantic.DeepEqual(desiredContent, liveContent)
}

// storedContent returns the fields of obj other than its type, metadata, and status, in the
// form that the API server stores. An unstructured object is converted to its typed struct, so
// that an empty field and a missing field become the same value. The conversion back to a map
// then drops the empty fields that the JSON encoding omits, and returns a new map that can be
// changed without changing obj.
func storedContent(scheme *runtime.Scheme, gvk schema.GroupVersionKind, obj client.Object) (map[string]any, error) {
	var typed any = obj
	if u, ok := obj.(*unstructured.Unstructured); ok {
		newObj, err := scheme.New(gvk)
		if err != nil {
			return nil, err
		}
		if err := runtime.DefaultUnstructuredConverter.FromUnstructured(u.Object, newObj); err != nil {
			return nil, err
		}
		typed = newObj
	}
	content, err := runtime.DefaultUnstructuredConverter.ToUnstructured(typed)
	if err != nil {
		return nil, err
	}
	for _, field := range []string{"apiVersion", "kind", "metadata", "status"} {
		delete(content, field)
	}
	return content, nil
}
