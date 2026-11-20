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
	"maps"
	"slices"
	"strings"
	"time"

	"github.com/NVIDIA/k8s-operator-libs/pkg/upgrade"
	"github.com/go-logr/logr"
	corev1 "k8s.io/api/core/v1"
	resourcev1 "k8s.io/api/resource/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/tools/events"
	"k8s.io/client-go/util/workqueue"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/event"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	"sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/predicate"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
	"sigs.k8s.io/controller-runtime/pkg/source"

	gpuv1 "github.com/NVIDIA/gpu-operator/api/nvidia/v1"
	nvidiav1alpha1 "github.com/NVIDIA/gpu-operator/api/nvidia/v1alpha1"
	"github.com/NVIDIA/gpu-operator/internal/coexistence"
	"github.com/NVIDIA/gpu-operator/internal/consts"
	nvidiadriverutil "github.com/NVIDIA/gpu-operator/internal/nvidiadriver"
)

const nodeLabelingControllerSingletonName = "cluster"

// podNodeNameIndexKey indexes pods by their assigned node and DRA plugin pods
// by their DaemonSet target before the scheduler binds them.
const podNodeNameIndexKey = "gpu-operator.nodeName"

// NodeLabelingReconciler applies GPU-Operator related labels and annotations to Kubernetes nodes.
// All node label write operations for the GPU Operator are centralized here.
type NodeLabelingReconciler struct {
	client.Client
	Scheme       *runtime.Scheme
	Namespace    string
	Log          logr.Logger
	DefaultStack string

	recorder events.EventRecorder
}

// nodeLabelingController holds per-reconcile state so that helper methods don't need to
// re-receive that state as arguments. Both sources can be present; selectNode resolves
// each node's allocator before applying the corresponding deployment labels.
type nodeLabelingController struct {
	client        client.Client
	namespace     string
	clusterPolicy *gpuv1.ClusterPolicy
	gpuCluster    *nvidiav1alpha1.GPUCluster
	logger        logr.Logger
	recorder      events.EventRecorder
	defaultStack  string

	// Requeue while claim cleanup or plugin removal blocks allocator placement.
	draPluginRemovalDeferred bool
}

// gpuNodeLabelsUpdateResult reports total node patches and the subset where GPU
// discovery state changed. The discovery state is stored in nvidia.com/gpu.present,
// which AssignOwners uses to find GPU nodes, so dependent operations are deferred
// until the informer cache observes those node updates.
type gpuNodeLabelsUpdateResult struct {
	totalPatchedNodeCount             int
	gpuDiscoveryStateChangedNodeCount int
}

// nodeLabelUpdateReasons captures why a node update event should trigger node-label reconciliation.
type nodeLabelUpdateReasons struct {
	gpuCommonLabelMissing                bool
	gpuCommonLabelOutdated               bool
	gpuCommonLabelChanged                bool
	commonOperandsLabelChanged           bool
	gpuWorkloadConfigChanged             bool
	migCapableLabelChanged               bool
	osTreeLabelChanged                   bool
	nvidiaDriverOwnerLabelChange         bool
	nvidiaDriverNodeSelectorLabelChanged bool
}

// needsUpdate reports whether any tracked node-label change requires reconciliation.
func (r nodeLabelUpdateReasons) needsUpdate() bool {
	return r.gpuCommonLabelMissing ||
		r.gpuCommonLabelOutdated ||
		r.gpuCommonLabelChanged ||
		r.commonOperandsLabelChanged ||
		r.gpuWorkloadConfigChanged ||
		r.migCapableLabelChanged ||
		r.osTreeLabelChanged ||
		r.nvidiaDriverOwnerLabelChange ||
		r.nvidiaDriverNodeSelectorLabelChanged
}

// getNodeLabelUpdateReasons collects changes that can affect GPU labels or driver pool membership.
func (r *NodeLabelingReconciler) getNodeLabelUpdateReasons(ctx context.Context, nodeName string, oldLabels, newLabels map[string]string) (nodeLabelUpdateReasons, error) {
	reasons := nodeLabelUpdateReasons{
		gpuCommonLabelMissing:        hasGPULabels(newLabels) && !hasCommonGPULabel(newLabels),
		gpuCommonLabelOutdated:       !hasGPULabels(newLabels) && hasCommonGPULabel(newLabels),
		gpuCommonLabelChanged:        oldLabels[commonGPULabelKey] != newLabels[commonGPULabelKey],
		commonOperandsLabelChanged:   hasOperandsDisabled(oldLabels) != hasOperandsDisabled(newLabels),
		gpuWorkloadConfigChanged:     oldLabels[gpuWorkloadConfigLabelKey] != newLabels[gpuWorkloadConfigLabelKey],
		migCapableLabelChanged:       hasMIGCapableGPU(oldLabels) != hasMIGCapableGPU(newLabels),
		osTreeLabelChanged:           oldLabels[nfdOSTreeVersionLabelKey] != newLabels[nfdOSTreeVersionLabelKey],
		nvidiaDriverOwnerLabelChange: oldLabels[consts.NVIDIADriverOwnerLabel] != newLabels[consts.NVIDIADriverOwnerLabel],
	}

	if reasons.needsUpdate() || maps.Equal(oldLabels, newLabels) {
		return reasons, nil
	}

	// A node can enter another driver's pool even when its current owner is
	// the default driver or it has no owner, so inspect every live selector.
	drivers := &nvidiav1alpha1.NVIDIADriverList{}
	if err := r.List(ctx, drivers); err != nil {
		r.Log.Error(err, "failed to list NVIDIADrivers for node label update", "node", nodeName)
		return reasons, err
	}
	for _, driver := range drivers.Items {
		if driver.HasDeletionTimestamp() {
			continue
		}
		for key := range driver.Spec.NodeSelector {
			oldValue, oldPresent := oldLabels[key]
			newValue, newPresent := newLabels[key]
			if oldValue != newValue || oldPresent != newPresent {
				reasons.nvidiaDriverNodeSelectorLabelChanged = true
				return reasons, nil
			}
		}
	}

	return reasons, nil
}

// nodeUpdateRequiresReconcile reports whether a node label change requires reconciliation.
// Driver lookup failures also trigger reconciliation so the update is not lost.
func (r *NodeLabelingReconciler) nodeUpdateRequiresReconcile(ctx context.Context, oldNode, newNode *corev1.Node) bool {
	newLabels := newNode.GetLabels()
	oldLabels := oldNode.GetLabels()
	nodeName := newNode.GetName()

	reasons, err := r.getNodeLabelUpdateReasons(ctx, nodeName, oldLabels, newLabels)
	if err != nil {
		// Enqueue reconciliation so a transient cache error cannot drop the update.
		return true
	}

	needsUpdate := reasons.needsUpdate() || annotationChanged(oldNode, newNode, coexistence.SelectionAnnotation)
	for _, labels := range []map[string]string{oldLabels, newLabels} {
		for key := range labels {
			if strings.HasPrefix(key, "nvidia.com/gpu.deploy.") {
				old, oldOK := oldLabels[key]
				new, newOK := newLabels[key]
				if old != new || oldOK != newOK {
					needsUpdate = true
				}
			}
		}
	}
	if needsUpdate {
		r.Log.Info("Node needs an update",
			"name", nodeName,
			"gpuCommonLabelMissing", reasons.gpuCommonLabelMissing,
			"gpuCommonLabelOutdated", reasons.gpuCommonLabelOutdated,
			"gpuCommonLabelChanged", reasons.gpuCommonLabelChanged,
			"commonOperandsLabelChanged", reasons.commonOperandsLabelChanged,
			"gpuWorkloadConfigLabelChanged", reasons.gpuWorkloadConfigChanged,
			"migCapableLabelChanged", reasons.migCapableLabelChanged,
			"osTreeLabelChanged", reasons.osTreeLabelChanged,
			"nvidiaDriverOwnerLabelChanged", reasons.nvidiaDriverOwnerLabelChange,
			"nvidiaDriverNodeSelectorLabelChanged", reasons.nvidiaDriverNodeSelectorLabelChanged,
		)
	}
	return needsUpdate
}

// +kubebuilder:rbac:groups="",resources=nodes,verbs=get;list;watch;update;patch

// Reconcile applies GPU-Operator related labels and annotations to all cluster nodes.
func (r *NodeLabelingReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	logger := log.FromContext(ctx)
	logger.Info("Reconciling node labels")

	clusterPolicy, gpuCluster, err := resolveActiveConfig(ctx, r.Client)
	if err != nil {
		return reconcile.Result{}, err
	}
	cleanupPending, cleanupErr := r.cleanupDeletedSources(ctx)
	if cleanupErr != nil {
		// A failed cleanup reserves only the affected nodes; it must not freeze
		// ordinary assignment to the surviving stack on other nodes.
		logger.Error(cleanupErr, "Source node cleanup will be retried")
	}
	if clusterPolicy == nil && gpuCluster == nil {
		logger.Info("No eligible ClusterPolicy or GPUCluster exists, skipping node labeling")
		if cleanupPending {
			return reconcile.Result{RequeueAfter: lifecycleRetry}, cleanupErr
		}
		return reconcile.Result{}, cleanupErr
	}

	usesNvidiaDriverCRD := gpuCluster != nil ||
		(clusterPolicy != nil && clusterPolicy.Spec.Driver.UseNvidiaDriverCRDType())
	nlc := &nodeLabelingController{
		client:        r.Client,
		namespace:     r.Namespace,
		clusterPolicy: clusterPolicy,
		gpuCluster:    gpuCluster,
		logger:        logger,
		recorder:      r.recorder,
		defaultStack:  r.DefaultStack,
	}

	gpuLabelUpdateResult, err := nlc.labelGPUNodes(ctx)
	if err != nil {
		if gpuLabelUpdateResult.totalPatchedNodeCount > 0 {
			logger.Error(err, "GPU node label update failed after partially updating nodes",
				"totalPatchedNodeCount", gpuLabelUpdateResult.totalPatchedNodeCount,
				"gpuDiscoveryStateChangedNodeCount", gpuLabelUpdateResult.gpuDiscoveryStateChangedNodeCount,
			)
		}
		return reconcile.Result{}, err
	}
	if gpuLabelUpdateResult.gpuDiscoveryStateChangedNodeCount > 0 {
		logger.V(consts.LogLevelDebug).Info("GPU discovery state used by owner assignment updated; dependent node label operations will run after the node update event",
			"totalPatchedNodeCount", gpuLabelUpdateResult.totalPatchedNodeCount,
			"gpuDiscoveryStateChangedNodeCount", gpuLabelUpdateResult.gpuDiscoveryStateChangedNodeCount,
		)
		return reconcile.Result{}, cleanupErr
	}

	// Route each GPU node to its NVIDIADriver CR. Skipping this leaves the NVIDIADriver controller owning no nodes, and it
	// then removes the driver DaemonSet.
	if usesNvidiaDriverCRD {
		if _, err := nvidiadriverutil.AssignOwners(ctx, r.Client); err != nil {
			return reconcile.Result{}, fmt.Errorf("failed to assign NVIDIADriver owners to nodes: %w", err)
		}
		if err := nlc.labelNodesWithOrphanedDriverPods(ctx); err != nil {
			return reconcile.Result{}, fmt.Errorf("failed to label nodes with orphaned NVIDIA driver pods: %w", err)
		}
	}

	// The k8s-driver-manager init container consumes this annotation on either stack.
	if err := nlc.applyDriverAutoUpgradeAnnotation(ctx); err != nil {
		return reconcile.Result{}, err
	}

	if cleanupErr != nil {
		return reconcile.Result{}, cleanupErr
	}
	if nlc.draPluginRemovalDeferred || cleanupPending {
		// Claim cleanup or plugin termination can finish without a node update.
		return reconcile.Result{RequeueAfter: 30 * time.Second}, nil
	}
	return reconcile.Result{}, nil
}

func (r *NodeLabelingReconciler) cleanupDeletedSources(ctx context.Context) (bool, error) {
	cps := &gpuv1.ClusterPolicyList{}
	gcs := &nvidiav1alpha1.GPUClusterList{}
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
			continue
		}
		if !controllerutil.ContainsFinalizer(obj, nodeCleanupFinalizer) {
			continue
		}
		deleting = append(deleting, obj)
		// ClusterPolicy can still own a legacy driver during teardown.
		if _, isClusterPolicy := obj.(*gpuv1.ClusterPolicy); isClusterPolicy && !operandsRemovedTrue(obj) {
			keepDriver = true
		}
	}
	if len(deleting) == 0 {
		return false, nil
	}
	if !keepDriver {
		drivers := &nvidiav1alpha1.NVIDIADriverList{}
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
		if !operandsRemovedTrue(obj) {
			continue
		}
		needsAnotherPass := false
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
				if apierrors.IsConflict(err) {
					needsAnotherPass = true
					continue
				}
				return true, fmt.Errorf("clean stack labels on node %q: %w", node.Name, err)
			}
			needsAnotherPass = true
		}
		if needsAnotherPass {
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

// labelGPUNodes reconciles GPU-related labels and reports which node labels were patched.
func (nlc *nodeLabelingController) labelGPUNodes(ctx context.Context) (gpuNodeLabelsUpdateResult, error) {
	result := gpuNodeLabelsUpdateResult{}
	nodeList := &corev1.NodeList{}
	if err := nlc.client.List(ctx, nodeList); err != nil {
		return result, fmt.Errorf("unable to list nodes: %w", err)
	}

	for _, node := range nodeList.Items {
		original := node.DeepCopy()
		labels := node.GetLabels()
		if labels == nil {
			labels = map[string]string{}
			node.SetLabels(labels)
		}
		gpuDiscoveryStateChanged := false

		if nlc.reconcileCommonGPULabel(labels, node.Name) {
			node.SetLabels(labels)
			gpuDiscoveryStateChanged = true
		}

		stateLabelsModified, selectionErr := nlc.selectNode(ctx, &node)
		if selectionErr != nil {
			nlc.logger.Error(selectionErr, "Invalid allocator selection", "node", node.Name)
			if nlc.recorder != nil {
				nlc.recorder.Eventf(&node, nil, corev1.EventTypeWarning, "InvalidAllocatorSelection", "SelectAllocator", "%v", selectionErr)
			}
		}

		modified := gpuDiscoveryStateChanged || stateLabelsModified
		if modified {
			if err := nlc.client.Patch(ctx, &node, client.MergeFromWithOptions(original, client.MergeFromWithOptimisticLock{})); err != nil {
				return result, fmt.Errorf("unable to label node %s: %w", node.Name, err)
			}
			result.totalPatchedNodeCount++
			if gpuDiscoveryStateChanged {
				result.gpuDiscoveryStateChangedNodeCount++
			}
		}
	}
	return result, nil
}

// reconcileCommonGPULabel keeps nvidia.com/gpu.present in sync with NFD GPU PCI labels.
// Returns true if labels were modified.
func (nlc *nodeLabelingController) reconcileCommonGPULabel(labels map[string]string, nodeName string) bool {
	if !hasCommonGPULabel(labels) && hasGPULabels(labels) {
		nlc.logger.Info("Node has GPU(s), setting common GPU label", "NodeName", nodeName)
		labels[commonGPULabelKey] = commonGPULabelValue
		return true
	}
	if hasCommonGPULabel(labels) && !hasGPULabels(labels) {
		nlc.logger.Info("Node no longer has GPUs, clearing GPU labels", "NodeName", nodeName)
		labels[commonGPULabelKey] = "false"
		return true
	}
	return false
}

// updateGPUStateLabels syncs deploy labels for the selected stack and sets the
// MIG config label when applicable. A node without the common GPU label loses
// its deploy labels. The return value reports whether any labels changed.
func (nlc *nodeLabelingController) updateGPUStateLabels(ctx context.Context, labels map[string]string, nodeName, stack string) bool {
	if !hasCommonGPULabel(labels) {
		return removeAllGPUStateLabels(labels)
	}

	if stack == coexistence.DRA {
		// Sweep only the device-plugin stack's exclusive keys so k8s-driver-manager
		// pause state on the DRA stack's own keys survives.
		sweptPreviousStack := nlc.removeLabelsFromNode(labels, devicePluginOnlyStateLabelKeys(), nodeName)
		appliedStackLabels := updateGPUClusterStateLabels(labels)
		return sweptPreviousStack || appliedStackLabels
	}

	cp := nlc.clusterPolicy
	var sandbox gpuv1.SandboxWorkloadsSpec
	if cp != nil {
		sandbox = cp.Spec.SandboxWorkloads
	}

	config, err := getWorkloadConfig(labels, sandbox)
	if err != nil {
		nlc.logger.Info("WARNING: failed to get GPU workload config for node; using default",
			"NodeName", nodeName, "Error", err, "defaultGPUWorkloadConfig", config)
	}
	gpuWorkloadConfig := &gpuWorkloadConfiguration{
		config:      config,
		sandboxMode: sandbox.Mode,
		node:        nodeName,
		log:         nlc.logger,
	}
	// Preserve the plugin-removal deferral while claim consumers unprepare.
	draPluginLabel, draPluginWasSet := labels[draDriverDeployLabelKey]
	modified := gpuWorkloadConfig.updateGPUStateLabels(labels)
	if draPluginWasSet {
		if _, stillSet := labels[draDriverDeployLabelKey]; !stillSet && nlc.nodeHasDRAClaimPods(ctx, nodeName) {
			labels[draDriverDeployLabelKey] = draPluginLabel
			nlc.draPluginRemovalDeferred = true
			nlc.logger.Info("Deferring DRA kubelet-plugin removal until pods with GPU claims are gone",
				"NodeName", nodeName, "Label", draDriverDeployLabelKey)
		}
	}

	if cp != nil && cp.Spec.MIGManager.IsEnabled() && hasMIGCapableGPU(labels) && !hasMIGConfigLabel(labels) {
		migConfigDefault := ""
		if cp.Spec.MIGManager.Config != nil {
			migConfigDefault = cp.Spec.MIGManager.Config.Default
		}
		if migConfigDefault == migConfigDisabledValue {
			nlc.logger.Info("Setting MIG config label", "NodeName", nodeName,
				"Label", migConfigLabelKey, "Value", migConfigDisabledValue)
			labels[migConfigLabelKey] = migConfigDisabledValue
			modified = true
		}
	}
	return modified
}

// updateGPUClusterStateLabels is the GPUCluster analogue of the ClusterPolicy
// gpuWorkloadConfiguration state-label logic: it sets the DRA operand deploy labels on a GPU
// node (removal once the GPUs are gone is handled by removeAllGPUStateLabels). Like the
// ClusterPolicy path it honors an existing non-empty value so the k8s-driver-manager can
// pause an operand by flipping its label to drain it off a node during a driver reload.
// A present-but-empty value is treated as absent: no legitimate state is "", but
// k8s-driver-manager stamps "" when pausing an operand that was never deployed on the node.
// Returns true if modified.
func updateGPUClusterStateLabels(labels map[string]string) bool {
	modified := false
	for key, value := range gpuClusterStateLabels {
		if v, ok := labels[key]; !ok || v == "" {
			labels[key] = value
			modified = true
		}
	}
	return modified
}

// NVIDIAGPUDRADriverName is the DRA driver that allocates NVIDIA GPUs. Pods holding
// ResourceClaims allocated by it consume GPUs exactly like pods requesting
// device-plugin resources.
const NVIDIAGPUDRADriverName = "gpu.nvidia.com"

const nvidiaComputeDomainDRADriverName = "compute-domain.nvidia.com"

// PodHasNVIDIAGPUClaim reports whether any of the pod's ResourceClaims is allocated by
// the NVIDIA GPU DRA driver. A claim that exists but cannot be read counts as a GPU
// claim: callers use this to decide whether a pod still depends on the DRA
// kubelet-plugin, and overcounting is recoverable while undercounting wedges teardown.
//
// includeAdminAccess selects between the two meanings of "holds a GPU claim":
// admin-access allocations grant monitoring/validation pods (the GPUCluster operands)
// a management view of the devices without consuming them, so they neither block a
// driver reload nor warrant eviction (pass false); unpreparing them still requires
// the DRA kubelet-plugin, so teardown ordering must count them (pass true).
func PodHasNVIDIAGPUClaim(ctx context.Context, c client.Reader, pod *corev1.Pod, includeAdminAccess bool) bool {
	return podHasDRAClaim(ctx, c, pod, includeAdminAccess, NVIDIAGPUDRADriverName)
}

func podHasDRAClaim(ctx context.Context, c client.Reader, pod *corev1.Pod, includeAdminAccess bool, drivers ...string) bool {
	for _, podClaim := range pod.Spec.ResourceClaims {
		claimName := podClaim.ResourceClaimName
		if claimName == nil {
			// Claim generated from a ResourceClaimTemplate; the actual name is in status.
			for i := range pod.Status.ResourceClaimStatuses {
				status := &pod.Status.ResourceClaimStatuses[i]
				if status.Name == podClaim.Name && status.ResourceClaimName != nil {
					claimName = status.ResourceClaimName
					break
				}
			}
		}
		if claimName == nil {
			// No claim object exists yet, so nothing is allocated for this entry.
			continue
		}

		claim := &resourcev1.ResourceClaim{}
		if err := c.Get(ctx, types.NamespacedName{Namespace: pod.Namespace, Name: *claimName}, claim); err != nil {
			ctrl.Log.Error(err, "failed to resolve pod ResourceClaim, treating pod as a GPU pod",
				"pod", pod.Namespace+"/"+pod.Name, "claim", *claimName)
			return true
		}
		if claim.Status.Allocation == nil {
			continue
		}
		for _, result := range claim.Status.Allocation.Devices.Results {
			if !includeAdminAccess && result.AdminAccess != nil && *result.AdminAccess {
				continue
			}
			if slices.Contains(drivers, result.Driver) {
				return true
			}
		}
	}
	return false
}

// nodeHasDRAClaimPods reports whether pods on the node still need the NVIDIA
// kubelet plugin to unprepare GPU or compute-domain claims. Terminating pods and
// admin-access claims count. Pods that reached a terminal phase and are outside
// deletion have already unprepared their claims.
func (nlc *nodeLabelingController) nodeHasDRAClaimPods(ctx context.Context, nodeName string) bool {
	podList := &corev1.PodList{}
	if err := nlc.client.List(ctx, podList, client.MatchingFields{podNodeNameIndexKey: nodeName}); err != nil {
		nlc.logger.Error(err, "failed to list pods; assuming the node still has GPU claim pods", "NodeName", nodeName)
		return true
	}
	for i := range podList.Items {
		pod := &podList.Items[i]
		terminal := pod.Status.Phase == corev1.PodSucceeded || pod.Status.Phase == corev1.PodFailed
		if terminal && pod.DeletionTimestamp == nil {
			continue
		}
		if podHasDRAClaim(ctx, nlc.client, pod, true, NVIDIAGPUDRADriverName, nvidiaComputeDomainDRADriverName) {
			return true
		}
	}
	return false
}

func (nlc *nodeLabelingController) draPlacementPending(ctx context.Context, node *corev1.Node) bool {
	pluginLabel := node.Labels[draDriverDeployLabelKey]
	if pluginLabel == "true" || (pluginLabel != "" && nlc.nodeHasDRAClaimPods(ctx, node.Name)) {
		return true
	}

	podList := &corev1.PodList{}
	if err := nlc.client.List(ctx, podList, client.InNamespace(nlc.namespace), client.MatchingFields{podNodeNameIndexKey: node.Name}); err != nil {
		nlc.logger.Error(err, "failed to check whether the DRA plugin has exited", "NodeName", node.Name)
		return true
	}
	for i := range podList.Items {
		if isDRAKubeletPluginPod(&podList.Items[i]) {
			return true
		}
	}
	return false
}

func isDRAKubeletPluginPod(pod *corev1.Pod) bool {
	owner := metav1.GetControllerOfNoCopy(pod)
	return owner != nil && owner.APIVersion == "apps/v1" && owner.Kind == "DaemonSet" && owner.Name == draKubeletPluginDaemonSetName
}

func podNodeNameIndexer(obj client.Object) []string {
	pod := obj.(*corev1.Pod)
	if pod.Spec.NodeName != "" {
		return []string{pod.Spec.NodeName}
	}
	if !isDRAKubeletPluginPod(pod) || pod.Spec.Affinity == nil || pod.Spec.Affinity.NodeAffinity == nil {
		return nil
	}
	target := pod.Spec.Affinity.NodeAffinity.RequiredDuringSchedulingIgnoredDuringExecution
	if target == nil {
		return nil
	}
	// The DaemonSet controller pins new, unbound pods to their target via metadata.name.
	for _, term := range target.NodeSelectorTerms {
		for _, field := range term.MatchFields {
			if field.Key == "metadata.name" && field.Operator == corev1.NodeSelectorOpIn {
				return field.Values
			}
		}
	}
	return nil
}

// removeLabelsFromNode deletes the given label keys from the node's labels map,
// value-blind; keys outside deleteKeys are never touched. Returns true if labels
// were modified.
func (nlc *nodeLabelingController) removeLabelsFromNode(labels map[string]string, deleteKeys map[string]bool, nodeName string) bool {
	modified := false
	for key := range deleteKeys {
		if _, ok := labels[key]; !ok {
			continue
		}
		nlc.logger.Info("Deleting node label", "NodeName", nodeName, "Label", key)
		delete(labels, key)
		modified = true
	}
	return modified
}

func (nlc *nodeLabelingController) setDriverAutoUpgradeAnnotation(ctx context.Context, node *corev1.Node, autoUpgradeEnabled bool) error {
	annotationValue, annotationExists := node.Annotations[driverAutoUpgradeAnnotationKey]
	updateRequired := false
	if autoUpgradeEnabled {
		updateRequired = !annotationExists || annotationValue != "true"
	} else {
		updateRequired = annotationExists
	}
	if !updateRequired {
		return nil
	}

	original := node.DeepCopy()
	if node.Annotations == nil {
		node.Annotations = map[string]string{}
	}
	if autoUpgradeEnabled {
		node.Annotations[driverAutoUpgradeAnnotationKey] = "true"
	} else {
		delete(node.Annotations, driverAutoUpgradeAnnotationKey)
	}
	if err := nlc.client.Patch(ctx, node, client.MergeFrom(original)); err != nil {
		nlc.logger.Error(err, "Failed to patch driver auto-upgrade annotation",
			"node", node.Name, "enabled", autoUpgradeEnabled)
		return err
	}

	return nil
}

// applyDriverAutoUpgradeAnnotation sets or clears the driver auto-upgrade annotation on GPU nodes.
func (nlc *nodeLabelingController) applyDriverAutoUpgradeAnnotation(ctx context.Context) error {
	cp := nlc.clusterPolicy

	// Without a ClusterPolicy, operator-managed drivers must be NVIDIADriver-managed.
	upgradePolicyFromNVIDIADriverCRs := cp == nil ||
		(cp.Spec.Driver.UseNvidiaDriverCRDType() && !cp.Spec.SandboxWorkloads.IsEnabled())
	if upgradePolicyFromNVIDIADriverCRs {
		return nlc.applyDriverAutoUpgradeAnnotationForNVD(ctx)
	}

	autoUpgradeEnabled := cp.Spec.Driver.IsEnabled() &&
		cp.Spec.Driver.IsAutoUpgradeEnabled() &&
		!cp.Spec.SandboxWorkloads.IsEnabled()

	nodeList := &corev1.NodeList{}
	if err := nlc.client.List(ctx, nodeList, client.MatchingLabels{consts.GPUPresentLabel: "true"}); err != nil {
		return fmt.Errorf("unable to list nodes: %w", err)
	}

	for _, node := range nodeList.Items {
		err := nlc.setDriverAutoUpgradeAnnotation(ctx, &node, autoUpgradeEnabled)
		if err != nil {
			return fmt.Errorf("failed to set driver auto-upgrade annotation on node %q: %w", node.Name, err)
		}
	}
	return nil
}

func (nlc *nodeLabelingController) applyDriverAutoUpgradeAnnotationForNVD(ctx context.Context) error {
	nvidiaDriverList := &nvidiav1alpha1.NVIDIADriverList{}
	if err := nlc.client.List(ctx, nvidiaDriverList); err != nil {
		return fmt.Errorf("failed to list NVIDIADriver instances: %w", err)
	}

	for _, nvd := range nvidiaDriverList.Items {
		nodeList := &corev1.NodeList{}
		if err := nlc.client.List(ctx, nodeList, client.MatchingLabels{consts.NVIDIADriverOwnerLabel: nvd.Name}); err != nil {
			nlc.logger.Error(err, "Failed to list nodes for NVIDIADriver", "name", nvd.Name)
			return err
		}
		autoUpgradeEnabled := nvd.Spec.GetUpgradePolicyWithDefaults().AutoUpgrade
		for _, node := range nodeList.Items {
			err := nlc.setDriverAutoUpgradeAnnotation(ctx, &node, autoUpgradeEnabled)
			if err != nil {
				return fmt.Errorf("failed to set driver auto-upgrade annotation on node %q: %w", node.Name, err)
			}
		}
	}

	return nil
}

// labelNodesWithOrphanedDriverPods marks nodes that still have unowned (orphaned) ClusterPolicy
// driver pods so the upgrade controller can replace them in the normal upgrade flow.
func (nlc *nodeLabelingController) labelNodesWithOrphanedDriverPods(ctx context.Context) error {
	nvidiaDrivers := &nvidiav1alpha1.NVIDIADriverList{}
	if err := nlc.client.List(ctx, nvidiaDrivers); err != nil {
		return fmt.Errorf("failed to list NVIDIADriver CRs: %w", err)
	}
	if len(nvidiaDrivers.Items) == 0 {
		return nil
	}

	pods := &corev1.PodList{}
	if err := nlc.client.List(ctx, pods,
		client.InNamespace(nlc.namespace),
		client.MatchingLabels{AppComponentLabelKey: DriverAppComponentLabelValue},
	); err != nil {
		return fmt.Errorf("failed to list NVIDIA driver pods: %w", err)
	}

	for _, pod := range pods.Items {
		if len(pod.OwnerReferences) > 0 || pod.Status.Phase != corev1.PodRunning || pod.Spec.NodeName == "" {
			continue
		}

		node := &corev1.Node{}
		if err := nlc.client.Get(ctx, types.NamespacedName{Name: pod.Spec.NodeName}, node); err != nil {
			nlc.logger.Error(err, "failed to get node for orphaned driver pod", "pod", pod.Name, "node", pod.Spec.NodeName)
			continue
		}
		if !nodeOwnedByNVIDIADriver(node, nvidiaDrivers.Items) {
			continue
		}

		upgradeStateLabel := upgrade.GetUpgradeStateLabelKey()
		upgradeState := upgrade.UpgradeStateUnknown
		if node.Labels != nil {
			upgradeState = node.Labels[upgradeStateLabel]
		}
		if !isDriverUpgradeRequestAllowed(upgradeState) {
			continue
		}

		original := node.DeepCopy()
		if node.Labels == nil {
			node.Labels = map[string]string{}
		}
		node.Labels[upgradeStateLabel] = upgrade.UpgradeStateUpgradeRequired
		if err := nlc.client.Patch(ctx, node, client.MergeFrom(original)); err != nil {
			return fmt.Errorf("failed to label node %q for orphaned driver pod %q: %w", node.Name, pod.Name, err)
		}
	}
	return nil
}

// nodeOwnedByNVIDIADriver returns true when the node has an owner label matching a live NVIDIADriver.
func nodeOwnedByNVIDIADriver(node *corev1.Node, nvidiaDrivers []nvidiav1alpha1.NVIDIADriver) bool {
	if node.Labels == nil || node.Labels[consts.NVIDIADriverOwnerLabel] == "" {
		return false
	}
	for _, nvidiaDriver := range nvidiaDrivers {
		if nvidiaDriver.HasDeletionTimestamp() {
			continue
		}
		if node.Labels[consts.NVIDIADriverOwnerLabel] == nvidiaDriver.Name {
			return true
		}
	}
	return false
}

// isDriverUpgradeRequestAllowed returns true when migration can request a driver upgrade
// without overwriting an active or failed upgrade state.
func isDriverUpgradeRequestAllowed(upgradeState string) bool {
	return upgradeState == upgrade.UpgradeStateUnknown || upgradeState == upgrade.UpgradeStateDone
}

// singletonCRPredicate returns a predicate that triggers reconciliation on create, delete,
// and spec-changing update events for singleton CRs (ClusterPolicy, GPUCluster).
// Create events are passed explicitly so that a CR created after the operator starts
// immediately causes GPU nodes to be labeled without waiting for a subsequent update.
func singletonCRPredicate[T interface {
	client.Object
	GetGeneration() int64
}]() predicate.TypedFuncs[T] {
	return predicate.TypedFuncs[T]{
		CreateFunc: func(e event.TypedCreateEvent[T]) bool {
			return true
		},
		UpdateFunc: func(e event.TypedUpdateEvent[T]) bool {
			return e.ObjectNew.GetGeneration() != e.ObjectOld.GetGeneration()
		},
		DeleteFunc: func(e event.TypedDeleteEvent[T]) bool {
			return true
		},
	}
}

func nodeLabelingSourcePredicate[T client.Object]() predicate.TypedFuncs[T] {
	p := singletonCRPredicate[T]()
	update := p.Update
	p.UpdateFunc = func(e event.TypedUpdateEvent[T]) bool {
		// Adoption can run before the source controller adds its finalizer.
		addedCleanupFinalizer := !controllerutil.ContainsFinalizer(e.ObjectOld, nodeCleanupFinalizer) &&
			controllerutil.ContainsFinalizer(e.ObjectNew, nodeCleanupFinalizer)
		return update(e) || (!operandsRemovedTrue(e.ObjectOld) && operandsRemovedTrue(e.ObjectNew)) || addedCleanupFinalizer
	}
	return p
}

func sourceAdoptionPredicate[T client.Object]() predicate.TypedFuncs[T] {
	return predicate.TypedFuncs[T]{
		UpdateFunc: func(e event.TypedUpdateEvent[T]) bool {
			return !nodeSelectionInitializedTrue(e.ObjectOld) && nodeSelectionInitializedTrue(e.ObjectNew)
		},
	}
}

// SetupWithManager registers the NodeLabelingReconciler with the controller-runtime manager.
func (r *NodeLabelingReconciler) SetupWithManager(ctx context.Context, mgr ctrl.Manager) error {
	r.recorder = mgr.GetEventRecorder("nvidia-gpu-operator")
	if err := registerWorkloadIndexes(ctx, mgr); err != nil {
		return err
	}
	mapToSingleton := func(_ context.Context, _ client.Object) []reconcile.Request {
		return []reconcile.Request{{NamespacedName: types.NamespacedName{Name: nodeLabelingControllerSingletonName}}}
	}

	if err := mgr.GetFieldIndexer().IndexField(ctx, &corev1.Pod{}, podNodeNameIndexKey, podNodeNameIndexer); err != nil {
		return fmt.Errorf("failed to add pod node-name index: %w", err)
	}

	c, err := controller.New("node-labeling-controller", mgr, controller.Options{
		Reconciler:              newLoggingReconciler(r),
		MaxConcurrentReconciles: 1,
		RateLimiter:             workqueue.NewTypedItemExponentialFailureRateLimiter[reconcile.Request](minDelayCR, maxDelayCR),
	})
	if err != nil {
		return fmt.Errorf("error creating node-labeling controller: %w", err)
	}

	clusterPolicyMapFn := func(ctx context.Context, cp *gpuv1.ClusterPolicy) []reconcile.Request {
		return mapToSingleton(ctx, cp)
	}
	if err := c.Watch(source.Kind(
		mgr.GetCache(),
		&gpuv1.ClusterPolicy{},
		handler.TypedEnqueueRequestsFromMapFunc(clusterPolicyMapFn),
		predicate.Or(nodeLabelingSourcePredicate[*gpuv1.ClusterPolicy](), sourceAdoptionPredicate[*gpuv1.ClusterPolicy]()),
	)); err != nil {
		return fmt.Errorf("error watching ClusterPolicy: %w", err)
	}

	// Watch GPUCluster so GPU nodes are (re)labeled for the DRA stack as the CR is
	// created or removed, mirroring the ClusterPolicy watch above.
	gpuClusterMapFn := func(ctx context.Context, gc *nvidiav1alpha1.GPUCluster) []reconcile.Request {
		return mapToSingleton(ctx, gc)
	}
	if err := c.Watch(source.Kind(
		mgr.GetCache(),
		&nvidiav1alpha1.GPUCluster{},
		handler.TypedEnqueueRequestsFromMapFunc(gpuClusterMapFn),
		predicate.Or(nodeLabelingSourcePredicate[*nvidiav1alpha1.GPUCluster](), sourceAdoptionPredicate[*nvidiav1alpha1.GPUCluster]()),
	)); err != nil {
		return fmt.Errorf("error watching GPUCluster: %w", err)
	}

	// Watch NVIDIADriver including delete events so owner labels are cleaned up promptly.
	nvidiaDriverMapFn := func(ctx context.Context, nd *nvidiav1alpha1.NVIDIADriver) []reconcile.Request {
		return mapToSingleton(ctx, nd)
	}
	if err := c.Watch(source.Kind(
		mgr.GetCache(),
		&nvidiav1alpha1.NVIDIADriver{},
		handler.TypedEnqueueRequestsFromMapFunc(nvidiaDriverMapFn),
		predicate.TypedGenerationChangedPredicate[*nvidiav1alpha1.NVIDIADriver]{},
	)); err != nil {
		return fmt.Errorf("error watching NVIDIADriver: %w", err)
	}

	nodePredicate := predicate.TypedFuncs[*corev1.Node]{
		CreateFunc: func(e event.TypedCreateEvent[*corev1.Node]) bool {
			labels := e.Object.GetLabels()
			return hasGPULabels(labels)
		},
		UpdateFunc: func(e event.TypedUpdateEvent[*corev1.Node]) bool {
			return r.nodeUpdateRequiresReconcile(ctx, e.ObjectOld, e.ObjectNew)
		},
		DeleteFunc: func(e event.TypedDeleteEvent[*corev1.Node]) bool {
			return true
		},
	}
	nodeMapFn := func(ctx context.Context, n *corev1.Node) []reconcile.Request {
		return mapToSingleton(ctx, n)
	}
	if err := c.Watch(source.Kind(
		mgr.GetCache(),
		&corev1.Node{},
		handler.TypedEnqueueRequestsFromMapFunc(nodeMapFn),
		nodePredicate,
	)); err != nil {
		return fmt.Errorf("error watching Nodes: %w", err)
	}

	// Check for orphaned driver pods when they start and retry handoffs when
	// DRA plugin pods are deleted.
	podPredicate := predicate.TypedFuncs[*corev1.Pod]{
		CreateFunc: func(e event.TypedCreateEvent[*corev1.Pod]) bool {
			return e.Object.GetLabels()[AppComponentLabelKey] == DriverAppComponentLabelValue
		},
		UpdateFunc: func(e event.TypedUpdateEvent[*corev1.Pod]) bool {
			if e.ObjectNew.GetLabels()[AppComponentLabelKey] != DriverAppComponentLabelValue {
				return false
			}
			return e.ObjectOld.Status.Phase != corev1.PodRunning &&
				e.ObjectNew.Status.Phase == corev1.PodRunning
		},
		DeleteFunc: func(e event.TypedDeleteEvent[*corev1.Pod]) bool {
			return e.Object.Namespace == r.Namespace && isDRAKubeletPluginPod(e.Object)
		},
	}
	podMapFn := func(ctx context.Context, p *corev1.Pod) []reconcile.Request {
		return mapToSingleton(ctx, p)
	}
	if err := c.Watch(source.Kind(
		mgr.GetCache(),
		&corev1.Pod{},
		handler.TypedEnqueueRequestsFromMapFunc(podMapFn),
		podPredicate,
	)); err != nil {
		return fmt.Errorf("error watching Pods: %w", err)
	}

	return nil
}
