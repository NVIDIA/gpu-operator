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

package controllers

import (
	"context"
	"fmt"
	"reflect"
	"slices"
	"strings"
	"time"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"

	gpuv1 "github.com/NVIDIA/gpu-operator/api/nvidia/v1"
	gpu "github.com/NVIDIA/gpu-operator/api/nvidia/v1alpha1"
	"github.com/NVIDIA/gpu-operator/internal/consts"
)

const (
	nodeCleanupFinalizer = "nvidia.com/gpu-operator-node-cleanup"
	operandsRemoved      = "OperandsRemoved"
	lifecycleRetry       = 5 * time.Second
	sourceOwnerIndex     = "gpu-operator.source-owner"
	directPodBatchSize   = 64
	maxReportedWorkloads = 5
)

func sourceConditions(obj client.Object) *[]metav1.Condition {
	switch obj := obj.(type) {
	case *gpuv1.ClusterPolicy:
		return &obj.Status.Conditions
	case *gpu.GPUCluster:
		return &obj.Status.Conditions
	default:
		return nil
	}
}
func sourceConditionTrue(obj client.Object, name string) bool {
	cs := sourceConditions(obj)
	return cs != nil && meta.IsStatusConditionTrue(*cs, name)
}
func sourceConditionsChanged(a, b client.Object) bool {
	return sourceConditionTrue(a, operandsRemoved) != sourceConditionTrue(b, operandsRemoved)
}

// Source controllers alone call this helper; the node-label controller only reads status.
func setSourceCondition(ctx context.Context, c client.Client, obj client.Object, name string, value bool, reason, message string) error {
	original := obj.DeepCopyObject().(client.Object)
	// Teardown can be the first status write; the GPUCluster schema requires state.
	switch o := obj.(type) {
	case *gpuv1.ClusterPolicy:
		if o.Status.State == "" {
			o.Status.State = gpuv1.NotReady
		}
	case *gpu.GPUCluster:
		if o.Status.State == "" {
			o.Status.State = gpu.NotReady
		}
	}
	status := metav1.ConditionFalse
	if value {
		status = metav1.ConditionTrue
	}
	meta.SetStatusCondition(sourceConditions(obj), metav1.Condition{Type: name, Status: status, Reason: reason, Message: message, ObservedGeneration: obj.GetGeneration()})
	if !value || name == operandsRemoved {
		meta.SetStatusCondition(sourceConditions(obj), metav1.Condition{Type: "Ready", Status: metav1.ConditionFalse, Reason: reason, Message: message, ObservedGeneration: obj.GetGeneration()})
		switch o := obj.(type) {
		case *gpuv1.ClusterPolicy:
			o.Status.State = gpuv1.NotReady
		case *gpu.GPUCluster:
			o.Status.State = gpu.NotReady
		}
	}
	if reflect.DeepEqual(original, obj) {
		return nil
	}
	return c.Status().Patch(ctx, obj, client.MergeFromWithOptions(original, client.MergeFromWithOptimisticLock{}))
}

// Index only operands controlled directly by a source. Kubernetes garbage collection
// follows the remaining controller references (for example Deployment -> ReplicaSet -> Pod).
func sourceOwnerKeys(obj client.Object) []string {
	ref := metav1.GetControllerOfNoCopy(obj)
	if ref == nil || (ref.Kind != "ClusterPolicy" && ref.Kind != "GPUCluster") ||
		schema.FromAPIVersionAndKind(ref.APIVersion, ref.Kind).Group != gpu.SchemeGroupVersion.Group {
		return nil
	}
	key := string(ref.UID)
	if _, pod := obj.(*corev1.Pod); pod && !obj.GetDeletionTimestamp().IsZero() {
		// A slow terminating Pod must not occupy a slot in every deletion batch.
		key += "/terminating"
	}
	return []string{key}
}

func registerWorkloadIndexes(ctx context.Context, mgr ctrl.Manager) error {
	for _, obj := range []client.Object{&appsv1.DaemonSet{}, &appsv1.Deployment{}, &corev1.Pod{}} {
		if err := mgr.GetFieldIndexer().IndexField(ctx, obj, sourceOwnerIndex, sourceOwnerKeys); err != nil {
			return err
		}
	}
	return nil
}

func ownedWorkloads(ctx context.Context, c client.Client, namespace string, owner client.Object) ([]client.Object, bool, error) {
	key := string(owner.GetUID())
	owned := client.MatchingFields{sourceOwnerIndex: key}
	var result []client.Object
	for _, list := range []client.ObjectList{&appsv1.DaemonSetList{}, &appsv1.DeploymentList{}} {
		if err := c.List(ctx, list, client.InNamespace(namespace), owned); err != nil {
			return nil, false, err
		}
		if err := meta.EachListItem(list, func(item runtime.Object) error {
			result = append(result, item.(client.Object))
			return nil
		}); err != nil {
			return nil, false, err
		}
	}

	// Validator Pods can be owned directly by the CR and cannot be collected
	// through their creator's DaemonSet. Wait for them before releasing the source.
	pods := &corev1.PodList{}
	podLimit := directPodBatchSize
	if err := c.List(ctx, pods, client.InNamespace(namespace), owned, client.Limit(int64(podLimit))); err != nil {
		return nil, false, err
	}
	if len(pods.Items) == 0 {
		podLimit = maxReportedWorkloads + 1
		terminating := client.MatchingFields{sourceOwnerIndex: key + "/terminating"}
		if err := c.List(ctx, pods, client.InNamespace(namespace), terminating, client.Limit(int64(podLimit))); err != nil {
			return nil, false, err
		}
	}
	for i := range pods.Items {
		result = append(result, &pods.Items[i])
	}
	return result, len(pods.Items) >= podLimit, nil
}

// Keep the providers until every other directly owned workload is gone. A Pod
// from an older rollout can still consume claims after its template changes.
func isDRAProvider(obj client.Object) bool {
	switch obj.(type) {
	case *appsv1.DaemonSet:
		return obj.GetName() == "nvidia-dra-driver-kubelet-plugin"
	case *appsv1.Deployment:
		return obj.GetName() == "nvidia-dra-driver-controller"
	}
	return false
}

func teardownSourceOperands(ctx context.Context, c client.Client, namespace string, obj client.Object) (ctrl.Result, error) {
	if !controllerutil.ContainsFinalizer(obj, nodeCleanupFinalizer) {
		return ctrl.Result{}, nil
	}
	if sourceConditionTrue(obj, operandsRemoved) {
		return ctrl.Result{RequeueAfter: lifecycleRetry}, nil
	}
	workloads, podListAtLimit, err := ownedWorkloads(ctx, c, namespace, obj)
	if err != nil {
		_ = setSourceCondition(ctx, c, obj, operandsRemoved, false, "OperandTeardownPending", err.Error())
		return ctrl.Result{}, err
	}
	if len(workloads) == 0 {
		if err := setSourceCondition(ctx, c, obj, operandsRemoved, true, "TeardownComplete", "Observed all source-owned operand workloads and Pods removed"); err != nil {
			return ctrl.Result{}, err
		}
		return ctrl.Result{RequeueAfter: lifecycleRetry}, nil
	}
	targets := workloads
	if _, ok := obj.(*gpu.GPUCluster); ok {
		var dependents []client.Object
		for _, workload := range workloads {
			if !isDRAProvider(workload) {
				dependents = append(dependents, workload)
			}
		}
		if len(dependents) > 0 {
			targets = dependents
		}
	}
	message := teardownBlockerMessage(targets, podListAtLimit)
	if err := setSourceCondition(ctx, c, obj, operandsRemoved, false, "OperandTeardownPending", message); err != nil {
		return ctrl.Result{}, err
	}
	for _, target := range targets {
		if !target.GetDeletionTimestamp().IsZero() {
			continue
		}
		uid := target.GetUID()
		if err := c.Delete(ctx, target, client.PropagationPolicy(metav1.DeletePropagationForeground), client.Preconditions{UID: &uid}); err != nil && !apierrors.IsNotFound(err) {
			_ = setSourceCondition(ctx, c, obj, operandsRemoved, false, "OperandTeardownPending", err.Error())
			return ctrl.Result{}, fmt.Errorf("delete %T %s: %w", target, target.GetName(), err)
		}
	}
	return ctrl.Result{RequeueAfter: lifecycleRetry}, nil
}

func teardownBlockerMessage(targets []client.Object, podListAtLimit bool) string {
	blockers := make([]string, 0, len(targets))
	for _, target := range targets {
		blocker := fmt.Sprintf("%T %s/%s", target, target.GetNamespace(), target.GetName())
		if pod, ok := target.(*corev1.Pod); ok {
			if podListAtLimit {
				// A limited cache list can return a different subset each time.
				continue
			}
			if pod.Spec.NodeName != "" {
				blocker += " on " + pod.Spec.NodeName
			}
		}
		blockers = append(blockers, blocker)
	}
	slices.Sort(blockers)
	shown := min(len(blockers), maxReportedWorkloads)
	message := "Waiting for source-owned workloads to be removed"
	if shown > 0 {
		message += ": " + strings.Join(blockers[:shown], ", ")
	}
	if remaining := len(blockers) - shown; remaining > 0 {
		message += fmt.Sprintf(" (and %d more)", remaining)
	}
	if podListAtLimit {
		message += fmt.Sprintf("; at least %d validator Pods remain", maxReportedWorkloads+1)
	}
	return message
}

func stackExclusiveKeys(source client.Object) map[string]bool {
	if _, ok := source.(*gpuv1.ClusterPolicy); ok {
		return devicePluginOnlyStateLabelKeys()
	}
	keys := map[string]bool{}
	for key := range gpuClusterStateLabels {
		if key != driverDeployLabelKey {
			keys[key] = true
		}
	}
	return keys
}

// Preserve per-node opt-outs when the same stack is installed again. Discovery
// and independent driver metadata belong to their respective controllers.
func cleanSourceNode(node *corev1.Node, source client.Object, keepDriver bool) bool {
	changed := false
	for key := range stackExclusiveKeys(source) {
		if value, ok := node.Labels[key]; ok && value != "false" {
			delete(node.Labels, key)
			changed = true
		}
	}
	if !keepDriver && node.Labels[consts.NVIDIADriverOwnerLabel] == "" {
		for _, key := range []string{driverDeployLabelKey, vgpuManagerDeployLabelKey} {
			if value, ok := node.Labels[key]; ok && value != "false" {
				delete(node.Labels, key)
				changed = true
			}
		}
	}
	return changed
}

func (r *NodeLabelingReconciler) cleanupDeletedSources(ctx context.Context) (bool, error) {
	cps := &gpuv1.ClusterPolicyList{}
	gcs := &gpu.GPUClusterList{}
	if err := r.List(ctx, cps); err != nil {
		return false, err
	}
	if err := r.List(ctx, gcs); err != nil {
		return false, err
	}
	sources := []client.Object{}
	for i := range cps.Items {
		sources = append(sources, &cps.Items[i])
	}
	for i := range gcs.Items {
		sources = append(sources, &gcs.Items[i])
	}
	var deleting []client.Object
	keepDriver := false
	for _, obj := range sources {
		if obj.GetDeletionTimestamp().IsZero() {
			keepDriver = true
		}
		if cp, ok := obj.(*gpuv1.ClusterPolicy); !ok || cp.Spec.Driver.UseNvidiaDriverCRDType() {
			keepDriver = true
		}
		if !obj.GetDeletionTimestamp().IsZero() && controllerutil.ContainsFinalizer(obj, nodeCleanupFinalizer) {
			deleting = append(deleting, obj)
		}
	}
	if len(deleting) == 0 {
		return false, nil
	}
	if !keepDriver {
		drivers := &gpu.NVIDIADriverList{}
		if err := r.List(ctx, drivers); err != nil {
			return true, err
		}
		keepDriver = len(drivers.Items) > 0
	}
	nodes := &corev1.NodeList{}
	if err := r.List(ctx, nodes); err != nil {
		return true, err
	}
	for _, obj := range deleting {
		if !sourceConditionTrue(obj, operandsRemoved) {
			continue
		}
		patched := false
		for i := range nodes.Items {
			// Do not mutate the shared snapshot: a later source's patch must also be
			// conditional on the resourceVersion actually observed in the cache.
			node := nodes.Items[i].DeepCopy()
			original := node.DeepCopy()
			if !cleanSourceNode(node, obj, keepDriver) {
				continue
			}
			if err := r.Patch(ctx, node, client.MergeFromWithOptions(original, client.MergeFromWithOptimisticLock{})); err != nil {
				if apierrors.IsNotFound(err) {
					continue
				}
				return true, err
			}
			patched = true
		}
		if patched {
			continue
		}
		original := obj.DeepCopyObject().(client.Object)
		controllerutil.RemoveFinalizer(obj, nodeCleanupFinalizer)
		if err := r.Patch(ctx, obj, client.MergeFromWithOptions(original, client.MergeFromWithOptimisticLock{})); err != nil && !apierrors.IsNotFound(err) {
			return true, err
		}
	}
	return true, nil
}
