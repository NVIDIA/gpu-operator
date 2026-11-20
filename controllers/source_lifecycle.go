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
	"github.com/NVIDIA/gpu-operator/internal/coexistence"
)

const (
	nodeCleanupFinalizer     = "nvidia.com/gpu-operator-node-cleanup"
	operandsRemoved          = "OperandsRemoved"
	nodeSelectionInitialized = "NodeSelectionInitialized"
	lifecycleRetry           = 5 * time.Second
	sourceOwnerIndex         = "gpu-operator.source-owner"
	directPodBatchSize       = 64
	maxReportedWorkloads     = 5
)

func sourceStack(obj client.Object) string {
	if _, ok := obj.(*gpuv1.ClusterPolicy); ok {
		return coexistence.DP
	}
	return coexistence.DRA
}
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
func operandsRemovedTrue(obj client.Object) bool {
	cs := sourceConditions(obj)
	return cs != nil && meta.IsStatusConditionTrue(*cs, operandsRemoved)
}

func nodeSelectionInitializedTrue(obj client.Object) bool {
	cs := sourceConditions(obj)
	return cs != nil && meta.IsStatusConditionTrue(*cs, nodeSelectionInitialized)
}

func setOperandsRemoved(ctx context.Context, c client.Client, obj client.Object, done bool, reason, message string) error {
	return setSourceCondition(ctx, c, obj, operandsRemoved, done, reason, message)
}

func setSourceCondition(ctx context.Context, c client.Client, obj client.Object, condition string, done bool, reason, message string) error {
	original := obj.DeepCopyObject().(client.Object)
	// Successful adoption preserves readiness. The schema requires a state on the first write.
	notReady := condition == operandsRemoved || !done
	switch o := obj.(type) {
	case *gpuv1.ClusterPolicy:
		if notReady || o.Status.State == "" {
			o.Status.State = gpuv1.NotReady
		}
	case *gpu.GPUCluster:
		if notReady || o.Status.State == "" {
			o.Status.State = gpu.NotReady
		}
	}
	status := metav1.ConditionFalse
	if done {
		status = metav1.ConditionTrue
	}
	cs := sourceConditions(obj)
	meta.SetStatusCondition(cs, metav1.Condition{Type: condition, Status: status, Reason: reason, Message: message, ObservedGeneration: obj.GetGeneration()})
	if notReady {
		meta.SetStatusCondition(cs, metav1.Condition{Type: "Ready", Status: metav1.ConditionFalse, Reason: reason, Message: message, ObservedGeneration: obj.GetGeneration()})
	}
	if reflect.DeepEqual(original, obj) {
		return nil
	}
	return c.Status().Patch(ctx, obj, client.MergeFromWithOptions(original, client.MergeFromWithOptimisticLock{}))
}

func observeSourceAdoption(ctx context.Context, c client.Client, obj client.Object, defaultStack string) (bool, error) {
	if nodeSelectionInitializedTrue(obj) {
		return true, nil
	}
	pending, err := sourceAdoptionPending(ctx, c, obj, defaultStack)
	reason, message := "AdoptionComplete", "Observed selection records and compatible placement on eligible discovered nodes"
	if pending || err != nil {
		reason, message = "NodeAdoptionPending", "Waiting to observe selection records and compatible placement"
	}
	if err != nil {
		message = err.Error()
	}
	if statusErr := setSourceCondition(ctx, c, obj, nodeSelectionInitialized, !pending && err == nil, reason, message); statusErr != nil {
		return false, statusErr
	}
	// Even a successful status patch is observed before activation in another pass.
	return false, err
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
	if pod, ok := obj.(*corev1.Pod); ok {
		// Only ClusterPolicy creates direct validator Pods. A successful validator
		// can remain for garbage collection after its owner is deleted.
		if ref.Kind != "ClusterPolicy" || pod.Status.Phase == corev1.PodSucceeded {
			return nil
		}
		if !pod.GetDeletionTimestamp().IsZero() {
			// A slow terminating Pod must not occupy a slot in every deletion batch.
			key += "/terminating"
		}
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

	if _, isClusterPolicy := owner.(*gpuv1.ClusterPolicy); !isClusterPolicy {
		return result, false, nil
	}

	// ClusterPolicy validators can be owned directly by the CR. Wait for Pods
	// that have not succeeded before releasing the source.
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

const draKubeletPluginDaemonSetName = "nvidia-dra-driver-kubelet-plugin"

// Keep the providers until every other directly owned workload is gone. A Pod
// from an older rollout can still consume claims after its template changes.
func isDRAProvider(obj client.Object) bool {
	switch obj.(type) {
	case *appsv1.DaemonSet:
		return obj.GetName() == draKubeletPluginDaemonSetName
	case *appsv1.Deployment:
		return obj.GetName() == "nvidia-dra-driver-controller"
	}
	return false
}

func teardownSourceOperands(ctx context.Context, c client.Client, namespace string, obj client.Object) (ctrl.Result, error) {
	if !controllerutil.ContainsFinalizer(obj, nodeCleanupFinalizer) || operandsRemovedTrue(obj) {
		return ctrl.Result{}, nil
	}
	workloads, podListAtLimit, err := ownedWorkloads(ctx, c, namespace, obj)
	if err != nil {
		_ = setOperandsRemoved(ctx, c, obj, false, "OperandTeardownPending", err.Error())
		return ctrl.Result{}, err
	}
	if len(workloads) == 0 {
		// The condition wakes the node-labeling controller to finish cleanup.
		return ctrl.Result{}, setOperandsRemoved(ctx, c, obj, true, "TeardownComplete", "Source-owned workload controllers removed. Succeeded ClusterPolicy validator Pods may remain for garbage collection.")
	}
	targets := workloads
	if sourceStack(obj) == coexistence.DRA {
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
	if err := setOperandsRemoved(ctx, c, obj, false, "OperandTeardownPending", message); err != nil {
		return ctrl.Result{}, err
	}
	for _, target := range targets {
		if !target.GetDeletionTimestamp().IsZero() {
			continue
		}
		uid := target.GetUID()
		if err := c.Delete(ctx, target, client.PropagationPolicy(metav1.DeletePropagationForeground), client.Preconditions{UID: &uid}); err != nil && !apierrors.IsNotFound(err) {
			_ = setOperandsRemoved(ctx, c, obj, false, "OperandTeardownPending", err.Error())
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

func stackExclusiveKeys(stack string) map[string]bool {
	if stack == coexistence.DP {
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

func nodeReservedForTeardown(node *corev1.Node, cp *gpuv1.ClusterPolicy, gc *gpu.GPUCluster) bool {
	return (cp != nil && !cp.DeletionTimestamp.IsZero() && hasStackMetadata(node, coexistence.DP)) ||
		(gc != nil && !gc.DeletionTimestamp.IsZero() && hasStackMetadata(node, coexistence.DRA))
}

func hasStackMetadata(node *corev1.Node, stack string) bool {
	if node.Annotations[coexistence.SelectionAnnotation] == stack {
		return true
	}
	for key := range stackExclusiveKeys(stack) {
		if value, ok := node.Labels[key]; ok && value != "false" {
			return true
		}
	}
	return false
}

// Preserve per-node opt-outs when the same stack is installed again. Discovery
// and independent driver metadata belong to their respective controllers.
func cleanSourceNode(node *corev1.Node, source client.Object, keepDriver bool) bool {
	changed := false
	for key := range stackExclusiveKeys(sourceStack(source)) {
		if value, ok := node.Labels[key]; ok && value != "false" {
			delete(node.Labels, key)
			changed = true
		}
	}
	if node.Annotations[coexistence.SelectionAnnotation] == sourceStack(source) {
		delete(node.Annotations, coexistence.SelectionAnnotation)
		changed = true
	}
	if !keepDriver {
		for _, key := range []string{driverDeployLabelKey, vgpuManagerDeployLabelKey} {
			if value, ok := node.Labels[key]; ok && value != "false" {
				delete(node.Labels, key)
				changed = true
			}
		}
	}
	return changed
}
