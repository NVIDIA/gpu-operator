/*
Copyright 2022 NVIDIA CORPORATION & AFFILIATES

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package controllers

import (
	"context"
	"fmt"
	"time"

	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/intstr"
	"k8s.io/client-go/util/workqueue"
	"sigs.k8s.io/controller-runtime/pkg/controller"
	"sigs.k8s.io/controller-runtime/pkg/event"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	"sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/predicate"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
	"sigs.k8s.io/controller-runtime/pkg/source"

	"github.com/NVIDIA/k8s-operator-libs/pkg/consts"
	"github.com/NVIDIA/k8s-operator-libs/pkg/upgrade"
	"github.com/go-logr/logr"
	appsv1 "k8s.io/api/apps/v1"
	"k8s.io/apimachinery/pkg/runtime"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"

	gpuv1 "github.com/NVIDIA/gpu-operator/api/nvidia/v1"
	nvidiav1alpha1 "github.com/NVIDIA/gpu-operator/api/nvidia/v1alpha1"
)

// UpgradeReconciler reconciles Driver Daemon Sets for upgrade
type UpgradeReconciler struct {
	client.Client
	Log          logr.Logger
	Scheme       *runtime.Scheme
	StateManager upgrade.ClusterUpgradeStateManager
}

const (
	plannedRequeueInterval = time.Minute * 2
	// DriverLabelKey indicates pod label key of the driver
	DriverLabelKey = "app"
	// DriverLabelValue indicates pod label value of the driver
	DriverLabelValue = "nvidia-driver-daemonset"
	// UpgradeSkipDrainLabelSelector indicates the pod selector label to skip with drain
	UpgradeSkipDrainLabelSelector = "nvidia.com/gpu-driver-upgrade-drain.skip!=true"
	// AppComponentLabelKey indicates the label key of the component
	AppComponentLabelKey = "app.kubernetes.io/component"
	// AppComponentLabelValue indicates the label values of the nvidia-gpu-driver component
	AppComponentLabelValue = "nvidia-driver"
)

//nolint
// +kubebuilder:rbac:groups=mellanox.com,resources=*,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups="",resources=nodes,verbs=get;list;watch;update;patch
// +kubebuilder:rbac:groups="",resources=pods,verbs=list
// +kubebuilder:rbac:groups=apps,resources=deployments;daemonsets;replicasets;statefulsets,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=apps,resources=deployments/finalizers,verbs=update

// Reconcile is part of the main kubernetes reconciliation loop which aims to
// move the current state of the cluster closer to the desired state.
func (r *UpgradeReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	reqLogger := r.Log.WithValues("upgrade", req.NamespacedName)
	reqLogger.V(consts.LogLevelInfo).Info("Reconciling Upgrade")

	// Fetch the ClusterPolicy instance
	clusterPolicy := &gpuv1.ClusterPolicy{}
	err := r.Get(ctx, req.NamespacedName, clusterPolicy)
	if err != nil {
		reqLogger.Error(err, "Error getting ClusterPolicy object")
		if clusterPolicyCtrl.operatorMetrics != nil {
			clusterPolicyCtrl.operatorMetrics.reconciliationStatus.Set(reconciliationStatusClusterPolicyUnavailable)
		}
		if apierrors.IsNotFound(err) {
			// Request object not found, could have been deleted after reconcile request.
			// Owned objects are automatically garbage collected. For additional cleanup logic use finalizers.
			// Return and don't requeue
			return reconcile.Result{}, nil
		}
		// Error reading the object - requeue the request.
		return reconcile.Result{}, err
	}

	if clusterPolicy.Spec.SandboxWorkloads.IsEnabled() {
		reqLogger.V(consts.LogLevelInfo).Info("Advanced driver upgrade policy is not supported when 'sandboxWorkloads.enabled=true'" +
			"in ClusterPolicy, cleaning up upgrade state and skipping reconciliation")
		// disable driver upgrade metrics
		if clusterPolicyCtrl.operatorMetrics != nil {
			clusterPolicyCtrl.operatorMetrics.driverAutoUpgradeEnabled.Set(driverAutoUpgradeDisabled)
		}
		return ctrl.Result{}, r.removeNodeUpgradeStateLabels(ctx)
	}

	if clusterPolicy.Spec.Driver.UpgradePolicy == nil ||
		!clusterPolicy.Spec.Driver.UpgradePolicy.AutoUpgrade {
		reqLogger.V(consts.LogLevelInfo).Info("Advanced driver upgrade policy is disabled, cleaning up upgrade state and skipping reconciliation")
		// disable driver upgrade metrics
		if clusterPolicyCtrl.operatorMetrics != nil {
			clusterPolicyCtrl.operatorMetrics.driverAutoUpgradeEnabled.Set(driverAutoUpgradeDisabled)
		}
		return ctrl.Result{}, r.removeNodeUpgradeStateLabels(ctx)
	}
	// enable driver upgrade metrics
	if clusterPolicyCtrl.operatorMetrics != nil {
		clusterPolicyCtrl.operatorMetrics.driverAutoUpgradeEnabled.Set(driverAutoUpgradeEnabled)
	}

	var driverLabel map[string]string

	// initialize with common app=nvidia-driver-daemonset label
	driverLabelKey := DriverLabelKey
	driverLabelValue := DriverLabelValue

	if clusterPolicy.Spec.Driver.UseNvidiaDriverCRDType() {
		// app component label is added for all new driver daemonsets deployed by NVIDIADriver controller
		driverLabelKey = AppComponentLabelKey
		driverLabelValue = AppComponentLabelValue
	} else if clusterPolicyCtrl.openshift != "" && clusterPolicyCtrl.ocpDriverToolkit.enabled {
		// For OCP, when DTK is enabled app=nvidia-driver-daemonset label is not constant and changes
		// based on rhcos version. Hence use DTK label instead
		driverLabelKey = ocpDriverToolkitIdentificationLabel
		driverLabelValue = ocpDriverToolkitIdentificationValue
	}

	driverLabel = map[string]string{driverLabelKey: driverLabelValue}
	reqLogger.Info("Using label selector", "key", driverLabelKey, "value", driverLabelValue)

	state, err := r.StateManager.BuildState(ctx, clusterPolicyCtrl.operatorNamespace,
		driverLabel)
	if err != nil {
		r.Log.Error(err, "Failed to build cluster upgrade state")
		return ctrl.Result{}, err
	}

	// Clear stale upgrade labels from nodes that no longer have driver pods
	// Use the built state to avoid duplicate API calls and to skip nodes actively being upgraded
	// This is best-effort cleanup that runs every reconciliation
	if err := r.clearStaleUpgradeLabels(ctx, state, driverLabel, clusterPolicyCtrl.operatorNamespace); err != nil {
		// Log the error but continue with the upgrade process, as this is a best-effort cleanup and should not block upgrades
		r.Log.V(consts.LogLevelWarning).Info("Failed to clear stale upgrade labels", "error", err)
	}

	reqLogger.Info("Propagate state to state manager")
	reqLogger.V(consts.LogLevelDebug).Info("Current cluster upgrade state", "state", state)

	totalNodes := r.StateManager.GetTotalManagedNodes(state)
	maxUnavailable := totalNodes
	if clusterPolicy.Spec.Driver.UpgradePolicy != nil && clusterPolicy.Spec.Driver.UpgradePolicy.MaxUnavailable != nil {
		maxUnavailable, err = intstr.GetScaledValueFromIntOrPercent(clusterPolicy.Spec.Driver.UpgradePolicy.MaxUnavailable, totalNodes, true)
		if err != nil {
			r.Log.Error(err, "Failed to compute maxUnavailable from the current total nodes")
			return ctrl.Result{}, err
		}
	}

	// We want to skip operator itself during the drain because the upgrade process might hang
	// if the operator is evicted and can't be rescheduled to any other node, e.g. in a single-node cluster.
	// It's safe to do because the goal of the node draining during the upgrade is to
	// evict pods that might use driver and operator doesn't use in its own pod.
	if clusterPolicy.Spec.Driver.UpgradePolicy.DrainSpec.PodSelector == "" {
		clusterPolicy.Spec.Driver.UpgradePolicy.DrainSpec.PodSelector = UpgradeSkipDrainLabelSelector
	} else {
		clusterPolicy.Spec.Driver.UpgradePolicy.DrainSpec.PodSelector =
			fmt.Sprintf("%s,%s", clusterPolicy.Spec.Driver.UpgradePolicy.DrainSpec.PodSelector, UpgradeSkipDrainLabelSelector)
	}

	// log metrics with the current state
	if clusterPolicyCtrl.operatorMetrics != nil {
		clusterPolicyCtrl.operatorMetrics.upgradesInProgress.Set(float64(r.StateManager.GetUpgradesInProgress(state)))
		clusterPolicyCtrl.operatorMetrics.upgradesDone.Set(float64(r.StateManager.GetUpgradesDone(state)))
		clusterPolicyCtrl.operatorMetrics.upgradesAvailable.Set(float64(r.StateManager.GetUpgradesAvailable(state, clusterPolicy.Spec.Driver.UpgradePolicy.MaxParallelUpgrades, maxUnavailable)))
		clusterPolicyCtrl.operatorMetrics.upgradesFailed.Set(float64(r.StateManager.GetUpgradesFailed(state)))
		clusterPolicyCtrl.operatorMetrics.upgradesPending.Set(float64(r.StateManager.GetUpgradesPending(state)))
	}

	err = r.StateManager.ApplyState(ctx, state, clusterPolicy.Spec.Driver.UpgradePolicy)
	if err != nil {
		r.Log.Error(err, "Failed to apply cluster upgrade state")
		return ctrl.Result{}, err
	}

	// In some cases if node state changes fail to apply, upgrade process
	// might become stuck until the new reconcile loop is scheduled.
	// Since node/ds/clusterpolicy updates from outside of the upgrade flow
	// are not guaranteed, for safety reconcile loop should be requeued every few minutes.
	return ctrl.Result{Requeue: true, RequeueAfter: plannedRequeueInterval}, nil
}

// clearStaleUpgradeLabels removes upgrade labels from nodes where driver pods are no longer scheduled.
// This handles the case where a nodeSelector change causes pods to be terminated from certain nodes,
// but the upgrade labels remain. It skips nodes that are actively being managed by the upgrade process.
func (r *UpgradeReconciler) clearStaleUpgradeLabels(ctx context.Context, state *upgrade.ClusterUpgradeState, driverLabel map[string]string, namespace string) error {
	upgradeStateLabel := upgrade.GetUpgradeStateLabelKey()

	// Build a set of nodes being actively managed by the upgrade process
	// Managed nodes are those in ClusterUpgradeState.NodeStates, which are populated by BuildState()
	// and represent nodes that currently have driver pods associated with them
	managedNodes := make(map[string]bool)
	for _, nodeStates := range state.NodeStates {
		for _, nodeState := range nodeStates {
			if nodeState.Node != nil {
				managedNodes[nodeState.Node.Name] = true
			}
		}
	}

	// List only nodes that have the upgrade label
	nodeList := &corev1.NodeList{}
	if err := r.List(ctx, nodeList, client.HasLabels{upgradeStateLabel}); err != nil {
		return fmt.Errorf("failed to list nodes with upgrade labels: %w", err)
	}

	if len(nodeList.Items) == 0 {
		return nil
	}

	// Filter out nodes being actively managed by upgrade process (those with driver pods in state.NodeStates)
	// This ensures we only clean up labels from nodes that truly have stale labels
	var nodesToCheck []corev1.Node
	for _, node := range nodeList.Items {
		if !managedNodes[node.Name] {
			nodesToCheck = append(nodesToCheck, node)
		}
	}

	if len(nodesToCheck) == 0 {
		return nil
	}

	// List driver DaemonSets to check which nodes should have pods
	// This protects against removing labels during pod recreation (e.g., during rolling updates)
	dsList := &appsv1.DaemonSetList{}
	if err := r.List(ctx, dsList, client.InNamespace(namespace), client.MatchingLabels(driverLabel)); err != nil {
		return fmt.Errorf("failed to list driver DaemonSets: %w", err)
	}

	// Build a set of nodes that DaemonSets would schedule to
	// This protects nodes during pod recreation (e.g., rolling updates where pod is temporarily missing)
	nodesSelected := make(map[string]bool)
	for _, ds := range dsList.Items {
		// DaemonSet nodeSelector is a map[string]string in spec.template.spec.nodeSelector
		nodeSelector := ds.Spec.Template.Spec.NodeSelector
		selector := labels.SelectorFromSet(nodeSelector)

		for _, node := range nodesToCheck {
			// Check if this DaemonSet would schedule to this node
			if selector.Matches(labels.Set(node.Labels)) {
				nodesSelected[node.Name] = true
			}
		}
	}

	// Clear upgrade label from nodes that aren't targeted by any DaemonSet
	// nodesToCheck excludes nodes actively managed by the upgrade state machine (managedNodes filter)
	// These managed nodes are in ClusterUpgradeState.NodeStates and have driver pods associated with them
	// The nodesSelected check protects against removing labels during pod recreation
	// (e.g., rolling updates where old pod is deleted but new pod not yet scheduled)
	//
	// Note: There is a small race condition window where a DaemonSet's nodeSelector is updated
	// after we list DaemonSets but before we patch the node. This is acceptable because:
	// 1. This is best-effort cleanup that runs every 2 minutes
	// 2. The primary use case (nodeSelector narrowing to exclude nodes) is correctly handled
	// 3. False negatives (not removing when we should) are safer than false positives
	for i := range nodesToCheck {
		node := &nodesToCheck[i]

		// Only remove label from node if no DaemonSet intends to schedule a pod there
		if _, exists := nodesSelected[node.Name]; !exists {
			r.Log.Info("Clearing stale upgrade label from node", "node", node.Name)

			nodeCopy := node.DeepCopy()
			delete(nodeCopy.Labels, upgradeStateLabel)
			if err := r.Patch(ctx, nodeCopy, client.MergeFrom(node)); err != nil {
				r.Log.Error(err, "Failed to clear upgrade label from node", "node", node.Name)
				// Continue with other nodes even if one fails
				continue
			}
		}
	}

	return nil
}

// removeNodeUpgradeStateLabels loops over nodes in the cluster and removes "nvidia.com/gpu-driver-upgrade-state"
// It is used for cleanup when autoUpgrade feature gets disabled
func (r *UpgradeReconciler) removeNodeUpgradeStateLabels(ctx context.Context) error {
	r.Log.Info("Resetting node upgrade labels from all nodes")

	nodeList := &corev1.NodeList{}
	err := r.List(ctx, nodeList)
	if err != nil {
		r.Log.Error(err, "Failed to get node list to reset upgrade labels")
		return err
	}

	upgradeStateLabel := upgrade.GetUpgradeStateLabelKey()

	for i := range nodeList.Items {
		node := &nodeList.Items[i]
		_, present := node.Labels[upgradeStateLabel]
		if present {
			delete(node.Labels, upgradeStateLabel)
			err = r.Update(ctx, node)
			if err != nil {
				r.Log.Error(err, "Failed to reset upgrade state label from node", "node", node)
				return err
			}
		}
	}
	return nil
}

// SetupWithManager sets up the controller with the Manager.
//
//nolint:dupl
func (r *UpgradeReconciler) SetupWithManager(ctx context.Context, mgr ctrl.Manager) error {
	// Create a new controller
	c, err := controller.New("upgrade-controller", mgr, controller.Options{Reconciler: r, MaxConcurrentReconciles: 1,
		RateLimiter: workqueue.NewTypedItemExponentialFailureRateLimiter[reconcile.Request](minDelayCR, maxDelayCR)})
	if err != nil {
		return err
	}

	// Watch for changes to primary resource ClusterPolicy
	err = c.Watch(source.Kind(
		mgr.GetCache(),
		&gpuv1.ClusterPolicy{},
		&handler.TypedEnqueueRequestForObject[*gpuv1.ClusterPolicy]{},
		predicate.TypedGenerationChangedPredicate[*gpuv1.ClusterPolicy]{}),
	)
	if err != nil {
		return err
	}

	// Define a mapping from the Node object in the event to one or more
	// ClusterPolicy objects to Reconcile
	nodeMapFn := func(ctx context.Context, o *corev1.Node) []reconcile.Request {
		return getClusterPoliciesToReconcile(ctx, mgr.GetClient())
	}

	// Only watch for changes to the upgrade state label
	upgradeStateLabelPredicate := predicate.TypedFuncs[*corev1.Node]{
		UpdateFunc: func(e event.TypedUpdateEvent[*corev1.Node]) bool {
			label := upgrade.GetUpgradeStateLabelKey()
			return e.ObjectOld.Labels[label] != e.ObjectNew.Labels[label]
		},
	}

	err = c.Watch(
		source.Kind(
			mgr.GetCache(),
			&corev1.Node{},
			handler.TypedEnqueueRequestsFromMapFunc[*corev1.Node](nodeMapFn),
			upgradeStateLabelPredicate,
		),
	)
	if err != nil {
		return err
	}

	// Define a mapping between the DaemonSet object in the event
	// to one or more ClusterPolicy instances to reconcile.
	//
	// For events generated by DaemonSets, ensure the object is
	// owned by either ClusterPolicy or NVIDIADriver.
	dsMapFn := func(ctx context.Context, a *appsv1.DaemonSet) []reconcile.Request {
		ownerRefs := a.GetOwnerReferences()

		ownedByNVIDIA := false
		for _, owner := range ownerRefs {
			if (owner.APIVersion == gpuv1.SchemeGroupVersion.String() && owner.Kind == "ClusterPolicy") ||
				(owner.APIVersion == nvidiav1alpha1.SchemeGroupVersion.String() && owner.Kind == "NVIDIADriver") {
				ownedByNVIDIA = true
				break
			}
		}

		if !ownedByNVIDIA {
			return nil
		}

		return getClusterPoliciesToReconcile(ctx, mgr.GetClient())
	}

	// Watch for changes to NVIDIA driver daemonsets and enqueue ClusterPolicy
	// TODO: use one common label to identify all NVIDIA driver DaemonSets
	appLabelSelector := predicate.NewTypedPredicateFuncs(func(ds *appsv1.DaemonSet) bool {
		ls := metav1.LabelSelector{MatchLabels: map[string]string{DriverLabelKey: DriverLabelValue}}
		selector, _ := metav1.LabelSelectorAsSelector(&ls)
		return selector.Matches(labels.Set(ds.GetLabels()))
	})

	dtkLabelSelector := predicate.NewTypedPredicateFuncs(func(ds *appsv1.DaemonSet) bool {
		ls := metav1.LabelSelector{MatchLabels: map[string]string{ocpDriverToolkitIdentificationLabel: ocpDriverToolkitIdentificationValue}}
		selector, _ := metav1.LabelSelectorAsSelector(&ls)
		return selector.Matches(labels.Set(ds.GetLabels()))
	})

	componentLabelSelector := predicate.NewTypedPredicateFuncs(func(ds *appsv1.DaemonSet) bool {
		ls := metav1.LabelSelector{MatchLabels: map[string]string{AppComponentLabelKey: AppComponentLabelValue}}
		selector, _ := metav1.LabelSelectorAsSelector(&ls)
		return selector.Matches(labels.Set(ds.GetLabels()))
	})

	err = c.Watch(
		source.Kind(
			mgr.GetCache(),
			&appsv1.DaemonSet{},
			handler.TypedEnqueueRequestsFromMapFunc[*appsv1.DaemonSet](dsMapFn),
			predicate.And[*appsv1.DaemonSet](
				predicate.TypedGenerationChangedPredicate[*appsv1.DaemonSet]{},
				predicate.Or[*appsv1.DaemonSet](appLabelSelector, dtkLabelSelector, componentLabelSelector),
			),
		))
	if err != nil {
		return err
	}

	return nil
}

func getClusterPoliciesToReconcile(ctx context.Context, k8sClient client.Client) []reconcile.Request {
	logger := log.FromContext(ctx)
	opts := []client.ListOption{}
	list := &gpuv1.ClusterPolicyList{}

	err := k8sClient.List(ctx, list, opts...)
	if err != nil {
		logger.Error(err, "Unable to list ClusterPolicies")
		return []reconcile.Request{}
	}

	cpToRec := []reconcile.Request{}

	for _, cp := range list.Items {
		cpToRec = append(cpToRec, reconcile.Request{NamespacedName: types.NamespacedName{
			Name:      cp.GetName(),
			Namespace: cp.GetNamespace(),
		}})
	}

	return cpToRec
}
