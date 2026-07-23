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

package state

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/go-logr/logr"
	appsv1 "k8s.io/api/apps/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/log"

	"github.com/NVIDIA/gpu-operator/internal/consts"
	"github.com/NVIDIA/gpu-operator/internal/render"
	"github.com/NVIDIA/gpu-operator/internal/utils"
)

// a state skeleton intended to be embedded in structs implementing the State interface
// it provides many of the common constructs and functionality needed to implement a state.
type stateSkel struct {
	name        string
	description string

	namespace string
	client    client.Client
	scheme    *runtime.Scheme
	renderer  render.Renderer
}

// Name provides the State name
func (s *stateSkel) Name() string {
	return s.name
}

// Description provides the State description
func (s *stateSkel) Description() string {
	return s.description
}

// newStateSkel builds the common state skeleton, loading the manifests from
// manifestDir into the renderer.
func newStateSkel(
	k8sClient client.Client,
	namespace string,
	scheme *runtime.Scheme,
	manifestDir string,
	name string,
	description string) (stateSkel, error) {

	files, err := utils.GetFilesWithSuffix(manifestDir, render.ManifestFileSuffix...)
	if err != nil {
		return stateSkel{}, fmt.Errorf("failed to get files from manifest directory: %v", err)
	}

	return stateSkel{
		name:        name,
		description: description,
		client:      k8sClient,
		namespace:   namespace,
		scheme:      scheme,
		renderer:    render.NewRenderer(files),
	}, nil
}

// renderObjects renders the state's manifests with the given templating data.
func (s *stateSkel) renderObjects(ctx context.Context, data interface{}) ([]*unstructured.Unstructured, error) {
	logger := log.FromContext(ctx)
	logger.V(consts.LogLevelDebug).Info("Rendering objects", "State:", s.name, "data", data)

	objs, err := s.renderer.RenderObjects(&render.TemplatingData{Data: data})
	if err != nil {
		return nil, fmt.Errorf("failed to render kubernetes manifests: %w", err)
	}
	return objs, nil
}

// syncObjects creates or updates the rendered objects and returns the aggregated sync
// state. Owner references make every object (including cluster-scoped ones) garbage
// collected when the owning CR is deleted.
func (s *stateSkel) syncObjects(ctx context.Context, owner metav1.Object, objs []*unstructured.Unstructured) (SyncState, error) {
	err := s.createOrUpdateObjs(ctx, func(obj *unstructured.Unstructured) error {
		if err := controllerutil.SetControllerReference(owner, obj, s.scheme); err != nil {
			return fmt.Errorf("failed to set controller reference for object: %w", err)
		}
		return nil
	}, objs)
	if err != nil {
		return SyncStateNotReady, fmt.Errorf("failed to create/update objects: %w", err)
	}

	syncState, err := s.getSyncState(ctx, objs)
	if err != nil {
		return SyncStateNotReady, fmt.Errorf("failed to get sync state: %w", err)
	}
	return syncState, nil
}

func getSupportedGVKs() []schema.GroupVersionKind {
	return []schema.GroupVersionKind{
		{
			Group:   "",
			Kind:    "ServiceAccount",
			Version: "v1",
		},
		{
			Group:   "",
			Kind:    "ConfigMap",
			Version: "v1",
		},
		{
			Group:   "apps",
			Kind:    "DaemonSet",
			Version: "v1",
		},
		{
			Group:   "apps",
			Kind:    "Deployment",
			Version: "v1",
		},
		{
			Group:   "apiextensions.k8s.io",
			Kind:    "CustomResourceDefinition",
			Version: "v1",
		},
		{
			Group:   "rbac.authorization.k8s.io",
			Kind:    "ClusterRole",
			Version: "v1",
		},
		{
			Group:   "rbac.authorization.k8s.io",
			Kind:    "ClusterRoleBinding",
			Version: "v1",
		},
		{
			Group:   "rbac.authorization.k8s.io",
			Kind:    "Role",
			Version: "v1",
		},
		{
			Group:   "rbac.authorization.k8s.io",
			Kind:    "RoleBinding",
			Version: "v1",
		},
		{
			Group:   "k8s.cni.cncf.io",
			Kind:    "NetworkAttachmentDefinition",
			Version: "v1",
		},
		{
			Group:   "batch",
			Kind:    "CronJob",
			Version: "v1",
		},
		{
			Group:   "security.openshift.io",
			Kind:    "SecurityContextConstraints",
			Version: "v1",
		},
		{
			Group:   "",
			Kind:    "Pod",
			Version: "v1",
		},
		{
			Group:   "",
			Kind:    "Service",
			Version: "v1",
		},
		{
			Group:   "monitoring.coreos.com",
			Kind:    "ServiceMonitor",
			Version: "v1",
		},
		{
			Group:   "scheduling.k8s.io",
			Kind:    "PriorityClass",
			Version: "v1",
		},
		{
			Group:   "",
			Kind:    "Taint",
			Version: "v1",
		},
		{
			Group:   "policy",
			Kind:    "PodSecurityPolicy",
			Version: "v1beta1",
		},
		{
			Group:   "node.k8s.io",
			Kind:    "RuntimeClass",
			Version: "v1",
		},
		{
			Group:   "monitoring.coreos.com",
			Kind:    "PrometheusRule",
			Version: "v1",
		},
		// ResourceClaimTemplate is listed under every resource.k8s.io version because
		// which one a cluster serves depends on its Kubernetes version; unserved
		// versions are skipped via IsNoMatchError during deletion.
		{
			Group:   "resource.k8s.io",
			Kind:    "ResourceClaimTemplate",
			Version: "v1",
		},
		{
			Group:   "resource.k8s.io",
			Kind:    "ResourceClaimTemplate",
			Version: "v1beta2",
		},
		{
			Group:   "resource.k8s.io",
			Kind:    "ResourceClaimTemplate",
			Version: "v1beta1",
		},
	}
}

func (s *stateSkel) getObj(ctx context.Context, obj *unstructured.Unstructured) error {
	reqLogger := log.FromContext(ctx)
	reqLogger.V(consts.LogLevelInfo).Info("Get Object", "Namespace:", obj.GetNamespace(), "Name:", obj.GetName())

	err := s.client.Get(
		ctx, types.NamespacedName{Name: obj.GetName(), Namespace: obj.GetNamespace()}, obj)
	if apierrors.IsNotFound(err) {
		// does not exist (yet)
		reqLogger.V(consts.LogLevelInfo).Info("Object Does not Exists")
	}
	return err
}

func (s *stateSkel) createObj(ctx context.Context, obj *unstructured.Unstructured) error {
	reqLogger := log.FromContext(ctx)

	s.checkDeleteSupported(ctx, obj)
	reqLogger.V(consts.LogLevelInfo).Info("Creating Object", "Namespace:", obj.GetNamespace(), "Name:", obj.GetName())
	toCreate := obj.DeepCopy()
	if err := s.client.Create(ctx, toCreate); err != nil {
		if apierrors.IsAlreadyExists(err) {
			reqLogger.V(consts.LogLevelInfo).Info("Object Already Exists")
		}
		return err
	}
	reqLogger.V(consts.LogLevelInfo).Info("Object created successfully")
	return nil
}

func (s *stateSkel) checkDeleteSupported(ctx context.Context, obj *unstructured.Unstructured) {
	reqLogger := log.FromContext(ctx)

	for _, gvk := range getSupportedGVKs() {
		objGvk := obj.GroupVersionKind()
		if objGvk.Group == gvk.Group && objGvk.Version == gvk.Version && objGvk.Kind == gvk.Kind {
			return
		}
	}
	reqLogger.V(consts.LogLevelWarning).Info("Object will not be deleted if needed",
		"Namespace:", obj.GetNamespace(), "Name:", obj.GetName(), "GVK", obj.GroupVersionKind())
}

func (s *stateSkel) updateObj(ctx context.Context, obj *unstructured.Unstructured) error {
	reqLogger := log.FromContext(ctx)
	reqLogger.V(consts.LogLevelInfo).Info("Updating Object", "Namespace:", obj.GetNamespace(), "Name:", obj.GetName())

	// Note: Some objects may require update of the resource version
	// TODO: using Patch preserves runtime attributes. In the future consider using patch if relevant
	desired := obj.DeepCopy()
	if err := s.client.Update(ctx, desired); err != nil {
		return fmt.Errorf("failed to update resource: %w", err)
	}
	reqLogger.V(consts.LogLevelInfo).Info("Object updated successfully")
	return nil
}

func (s *stateSkel) createOrUpdateObjs(
	ctx context.Context,
	setControllerReference func(obj *unstructured.Unstructured) error,
	objs []*unstructured.Unstructured) error {
	reqLogger := log.FromContext(ctx)
	for _, desiredObj := range objs {
		reqLogger.V(consts.LogLevelInfo).Info("Handling manifest object", "Kind:", desiredObj.GetKind(),
			"Name", desiredObj.GetName())
		// Set controller reference for object to allow cleanup on CR deletion
		if err := setControllerReference(desiredObj); err != nil {
			return fmt.Errorf("failed to set controller reference for object: %w", err)
		}

		s.addStateSpecificLabels(desiredObj)

		var desiredObjectHash string
		if desiredObj.GetKind() == "DaemonSet" {
			desiredObjectHash = utils.GetObjectHash(desiredObj)
			annotations := desiredObj.GetAnnotations()
			if annotations == nil {
				annotations = make(map[string]string)
			}
			annotations[consts.NvidiaAnnotationHashKey] = desiredObjectHash
			desiredObj.SetAnnotations(annotations)
		}

		err := s.createObj(ctx, desiredObj)
		if err == nil {
			// object created successfully
			continue
		}
		if !apierrors.IsAlreadyExists(err) {
			// Some error occurred
			return err
		}

		currentObj := desiredObj.DeepCopy()
		if err := s.getObj(ctx, currentObj); err != nil {
			// Some error occurred
			return err
		}

		if desiredObj.GetKind() == "DaemonSet" {
			if currentObjHash, ok := currentObj.GetAnnotations()[consts.NvidiaAnnotationHashKey]; ok {
				if desiredObjectHash == currentObjHash {
					reqLogger.V(consts.LogLevelDebug).Info("Object is unchanged, so skipping update",
						"Kind", desiredObj.GetKind(), "Name", desiredObj.GetName())
					continue
				}
			}
		}

		if err := s.mergeObjects(desiredObj, currentObj); err != nil {
			return err
		}

		// Object found, Update it
		if err := s.updateObj(ctx, desiredObj); err != nil {
			return err
		}
	}
	return nil
}

func (s *stateSkel) addStateSpecificLabels(obj *unstructured.Unstructured) {
	labels := obj.GetLabels()
	if labels == nil {
		labels = make(map[string]string)
	}
	labels[consts.StateLabel] = s.name
	obj.SetLabels(labels)
}

func (s *stateSkel) handleStateObjectsDeletion(ctx context.Context) (SyncState, error) {
	reqLogger := log.FromContext(ctx)
	reqLogger.V(consts.LogLevelInfo).Info(
		"State spec in CR is nil, deleting existing objects if needed", "State:", s.name)
	found, err := s.deleteStateRelatedObjects(ctx)
	if err != nil {
		return SyncStateError, fmt.Errorf("failed to delete k8s objects: %w", err)
	}
	if found {
		reqLogger.V(consts.LogLevelInfo).Info("State deleting objects in progress", "State:", s.name)
		return SyncStateNotReady, nil
	}
	return SyncStateIgnore, nil
}

func (s *stateSkel) deleteStateRelatedObjects(ctx context.Context) (bool, error) {
	stateLabel := map[string]string{
		consts.StateLabel: s.name,
	}
	found := false
	for _, gvk := range getSupportedGVKs() {
		// Scope the query for namespaced kinds to the operator namespace (where all
		// operand objects live) so only namespaced list permission is needed; list
		// cluster-scoped kinds cluster-wide. RESTMapping also reports unserved kinds.
		mapping, err := s.client.RESTMapper().RESTMapping(gvk.GroupKind(), gvk.Version)
		if meta.IsNoMatchError(err) {
			continue
		}
		if err != nil {
			return false, err
		}
		l := &unstructured.UnstructuredList{}
		l.SetGroupVersionKind(gvk)
		listOpts := []client.ListOption{client.MatchingLabels(stateLabel)}
		if mapping.Scope.Name() == meta.RESTScopeNameNamespace {
			listOpts = append(listOpts, client.InNamespace(s.namespace))
		}
		if err := s.client.List(ctx, l, listOpts...); err != nil {
			// The operator cannot have created objects of a kind it is not allowed to
			// list, so a forbidden error means there is nothing of this kind to clean up.
			if apierrors.IsForbidden(err) {
				continue
			}
			return false, err
		}
		if len(l.Items) > 0 {
			found = true
		}
		for _, obj := range l.Items {
			if obj.GetDeletionTimestamp() == nil {
				err := s.client.Delete(ctx, &obj)
				if err != nil && !apierrors.IsNotFound(err) {
					return true, err
				}
			}
		}
	}
	return found, nil
}

func (s *stateSkel) mergeObjects(updated, current *unstructured.Unstructured) error {
	// Set resource version
	// ResourceVersion must be passed unmodified back to the server.
	// ResourceVersion helps the kubernetes API server to implement optimistic concurrency for PUT operations
	// when two PUT requests are specifying the resourceVersion, one of the PUTs will fail.
	updated.SetResourceVersion(current.GetResourceVersion())

	gvk := updated.GroupVersionKind()
	if gvk.Group == "" && gvk.Kind == "ServiceAccount" {
		return s.mergeServiceAccount(updated, current)
	}
	return nil
}

// For Service Account, keep secrets if exists
func (s *stateSkel) mergeServiceAccount(updated, current *unstructured.Unstructured) error {
	curSecrets, ok, err := unstructured.NestedSlice(current.Object, "secrets")
	if err != nil {
		return err
	}
	if ok {
		if err := unstructured.SetNestedField(updated.Object, curSecrets, "secrets"); err != nil {
			return err
		}
	}

	curImagePullSecrets, ok, err := unstructured.NestedSlice(current.Object, "imagePullSecrets")
	if err != nil {
		return err
	}
	if ok {
		if err := unstructured.SetNestedField(updated.Object, curImagePullSecrets, "imagePullSecrets"); err != nil {
			return err
		}
	}
	return nil
}

// Iterate over objects and check for their readiness
func (s *stateSkel) getSyncState(ctx context.Context, objs []*unstructured.Unstructured) (SyncState, error) {
	reqLogger := log.FromContext(ctx)
	reqLogger.V(consts.LogLevelInfo).Info("Checking related object states")

	for _, obj := range objs {
		reqLogger.V(consts.LogLevelInfo).Info("Checking object", "Kind:", obj.GetKind(), "Name", obj.GetName())
		// Check if object exists
		found := obj.DeepCopy()
		err := s.getObj(ctx, found)
		if err != nil {
			if apierrors.IsNotFound(err) {
				// does not exist (yet)
				reqLogger.V(consts.LogLevelInfo).Info("Object is not ready", "Kind:", obj.GetKind(), "Name", obj.GetName())
				return SyncStateNotReady, nil
			}
			// other error
			return SyncStateNotReady, fmt.Errorf("failed to get object: %w", err)
		}

		// Object exists, check for Kind specific readiness
		if found.GetKind() == "DaemonSet" {
			if ready, err := s.isDaemonSetReady(found, reqLogger); err != nil || !ready {
				reqLogger.V(consts.LogLevelInfo).Info("Object is not ready", "Kind:", obj.GetKind(), "Name", obj.GetName())
				return SyncStateNotReady, err
			}
		}
		if found.GetKind() == "Deployment" {
			if ready, err := s.isDeploymentReady(found, reqLogger); err != nil || !ready {
				reqLogger.V(consts.LogLevelInfo).Info("Object is not ready", "Kind:", obj.GetKind(), "Name", obj.GetName())
				return SyncStateNotReady, err
			}
		}
		reqLogger.V(consts.LogLevelInfo).Info("Object is ready", "Kind:", obj.GetKind(), "Name", obj.GetName())
	}
	return SyncStateReady, nil
}

// isDaemonSetReady checks if daemonset is ready
func (s *stateSkel) isDaemonSetReady(uds *unstructured.Unstructured, reqLogger logr.Logger) (bool, error) {
	buf, err := uds.MarshalJSON()
	if err != nil {
		return false, fmt.Errorf("failed to marshall unstructured daemonset object: %w", err)
	}

	ds := &appsv1.DaemonSet{}
	if err = json.Unmarshal(buf, ds); err != nil {
		return false, fmt.Errorf("failed to unmarshall to daemonset object: %w", err)
	}

	reqLogger.V(consts.LogLevelDebug).Info(
		"Check daemonset state",
		"DesiredNodes:", ds.Status.DesiredNumberScheduled,
		"CurrentNodes:", ds.Status.CurrentNumberScheduled,
		"PodsAvailable:", ds.Status.NumberAvailable,
		"PodsUnavailable:", ds.Status.NumberUnavailable,
		"UpdatedPodsScheduled", ds.Status.UpdatedNumberScheduled,
		"PodsReady:", ds.Status.NumberReady,
		"Conditions:", ds.Status.Conditions)
	// A DaemonSet targeting zero nodes is ready: every operand DaemonSet gates on the
	// nvidia.com/gpu-operator.resource-allocation.mode node label, so zero desired pods is a legitimate
	// steady state (e.g. all GPU nodes serving the other stack). ObservedGeneration
	// distinguishes this from a DaemonSet the controller has not processed yet, where the
	// zero counts are merely uninitialized status.
	if ds.Status.ObservedGeneration >= ds.Generation &&
		ds.Status.DesiredNumberScheduled == 0 && ds.Status.NumberMisscheduled == 0 &&
		ds.Status.CurrentNumberScheduled == 0 {
		return true, nil
	}
	if ds.Status.DesiredNumberScheduled != 0 && ds.Status.DesiredNumberScheduled == ds.Status.NumberAvailable &&
		ds.Status.UpdatedNumberScheduled == ds.Status.NumberAvailable {
		return true, nil
	}
	return false, nil
}

// isDeploymentReady checks if a deployment is ready
func (s *stateSkel) isDeploymentReady(ud *unstructured.Unstructured, reqLogger logr.Logger) (bool, error) {
	buf, err := ud.MarshalJSON()
	if err != nil {
		return false, fmt.Errorf("failed to marshall unstructured deployment object: %w", err)
	}

	dep := &appsv1.Deployment{}
	if err = json.Unmarshal(buf, dep); err != nil {
		return false, fmt.Errorf("failed to unmarshall to deployment object: %w", err)
	}

	desired := int32(1)
	if dep.Spec.Replicas != nil {
		desired = *dep.Spec.Replicas
	}

	reqLogger.V(consts.LogLevelDebug).Info(
		"Check deployment state",
		"DesiredReplicas:", desired,
		"UpdatedReplicas:", dep.Status.UpdatedReplicas,
		"AvailableReplicas:", dep.Status.AvailableReplicas,
		"ObservedGeneration:", dep.Status.ObservedGeneration)

	if dep.Status.ObservedGeneration >= dep.Generation &&
		dep.Status.UpdatedReplicas == desired &&
		dep.Status.AvailableReplicas == desired {
		return true, nil
	}
	return false, nil
}
