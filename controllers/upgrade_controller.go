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

	upgrade_v1alpha1 "github.com/NVIDIA/k8s-operator-libs/api/upgrade/v1alpha1"
	"github.com/NVIDIA/k8s-operator-libs/pkg/consts"
	"github.com/NVIDIA/k8s-operator-libs/pkg/upgrade"
	"github.com/go-logr/logr"
	appsv1 "k8s.io/api/apps/v1"
	"k8s.io/apimachinery/pkg/runtime"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"

	gpuv1 "github.com/NVIDIA/gpu-operator/api/nvidia/v1"
	nvidiav1alpha1 "github.com/NVIDIA/gpu-operator/api/nvidia/v1alpha1"
	gpuconsts "github.com/NVIDIA/gpu-operator/internal/consts"
)

// UpgradeReconciler reconciles Driver Daemon Sets for upgrade
type UpgradeReconciler struct {
	client.Client
	Log             logr.Logger
	Scheme          *runtime.Scheme
	StateManager    upgrade.ClusterUpgradeStateManager
	OperatorMetrics *OperatorMetrics
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
	// DriverAppComponentLabelValue indicates the label value of the NVIDIA driver component
	DriverAppComponentLabelValue = "nvidia-driver"
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
		r.OperatorMetrics.reconciliationStatus.Set(reconciliationStatusClusterPolicyUnavailable)
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
		r.OperatorMetrics.driverAutoUpgradeEnabled.Set(driverAutoUpgradeDisabled)
		return ctrl.Result{}, r.removeNodeUpgradeStateLabels(ctx)
	}

	// TODO: When integrating the NVIDIA DRA Driver for GPUs, decouple
	// the driver-upgrade controller from ClusterPolicy. If a ClusterPolicy
	// CR does not exist, take the NVIDIADriver code path.
	if clusterPolicy.Spec.Driver.UseNvidiaDriverCRDType() {
		return r.reconcileNVIDIADriverUpgrades(ctx, reqLogger)
	}

	return r.reconcileClusterPolicyDriverUpgrades(ctx, reqLogger, clusterPolicy)
}

// reconcileClusterPolicyDriverUpgrades handles driver upgrade reconciliation when the
// ClusterPolicy CR is used for driver management.
func (r *UpgradeReconciler) reconcileClusterPolicyDriverUpgrades(ctx context.Context, reqLogger logr.Logger, clusterPolicy *gpuv1.ClusterPolicy) (ctrl.Result, error) {
	if clusterPolicy.Spec.Driver.UpgradePolicy == nil ||
		!clusterPolicy.Spec.Driver.UpgradePolicy.AutoUpgrade {
		reqLogger.V(consts.LogLevelInfo).Info("Advanced driver upgrade policy is disabled, cleaning up upgrade state and skipping reconciliation")
		r.OperatorMetrics.driverAutoUpgradeEnabled.Set(driverAutoUpgradeDisabled)
		return ctrl.Result{}, r.removeNodeUpgradeStateLabels(ctx)
	}
	r.OperatorMetrics.driverAutoUpgradeEnabled.Set(driverAutoUpgradeEnabled)

	var driverLabel map[string]string

	// initialize with common app=nvidia-driver-daemonset label
	driverLabelKey := DriverLabelKey
	driverLabelValue := DriverLabelValue

	if clusterPolicyCtrl.openshift != "" && clusterPolicyCtrl.ocpDriverToolkit.enabled {
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

	if err := r.clearStaleUpgradeLabels(ctx, state, driverLabel, clusterPolicyCtrl.operatorNamespace); err != nil {
		r.Log.Error(err, "Failed to clear stale upgrade labels")
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
	if clusterPolicy.Spec.Driver.UpgradePolicy.DrainSpec == nil {
		clusterPolicy.Spec.Driver.UpgradePolicy.DrainSpec = &upgrade_v1alpha1.DrainSpec{}
	}
	if clusterPolicy.Spec.Driver.UpgradePolicy.DrainSpec.PodSelector == "" {
		clusterPolicy.Spec.Driver.UpgradePolicy.DrainSpec.PodSelector = UpgradeSkipDrainLabelSelector
	} else {
		clusterPolicy.Spec.Driver.UpgradePolicy.DrainSpec.PodSelector =
			fmt.Sprintf("%s,%s", clusterPolicy.Spec.Driver.UpgradePolicy.DrainSpec.PodSelector, UpgradeSkipDrainLabelSelector)
	}

	// log metrics with the current state
	r.OperatorMetrics.upgradesInProgress.Set(float64(r.StateManager.GetUpgradesInProgress(state)))
	r.OperatorMetrics.upgradesDone.Set(float64(r.StateManager.GetUpgradesDone(state)))
	r.OperatorMetrics.upgradesAvailable.Set(float64(r.StateManager.GetUpgradesAvailable(state, clusterPolicy.Spec.Driver.UpgradePolicy.MaxParallelUpgrades, maxUnavailable)))
	r.OperatorMetrics.upgradesFailed.Set(float64(r.StateManager.GetUpgradesFailed(state)))
	r.OperatorMetrics.upgradesPending.Set(float64(r.StateManager.GetUpgradesPending(state)))

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

// reconcileNVIDIADriverUpgrades handles driver upgrade reconciliation when the NVIDIADriver CRD
// is used for driver management. Each NVIDIADriver instance may have its own upgrade policy.
func (r *UpgradeReconciler) reconcileNVIDIADriverUpgrades(ctx context.Context, reqLogger logr.Logger) (ctrl.Result, error) {
	var (
		upgradesInProgress, upgradesDone, upgradesAvailable, upgradesFailed, upgradesPending int
	)

	nvidiaDriverList := &nvidiav1alpha1.NVIDIADriverList{}
	if err := r.List(ctx, nvidiaDriverList); err != nil {
		return ctrl.Result{}, err
	}

	// Check if all NVIDIADriver instances have disabled automatic upgrades
	noAutoUpgradesEnabled := true
	for _, nvd := range nvidiaDriverList.Items {
		upgradePolicy := nvd.Spec.GetUpgradePolicyWithDefaults()
		if upgradePolicy.AutoUpgrade {
			noAutoUpgradesEnabled = false
			break
		}
	}

	if noAutoUpgradesEnabled {
		reqLogger.V(consts.LogLevelInfo).Info("No NVIDIADriver instance has upgrade policy enabled, cleaning up upgrade state and skipping reconciliation")
		r.OperatorMetrics.driverAutoUpgradeEnabled.Set(driverAutoUpgradeDisabled)
		return ctrl.Result{}, r.removeNodeUpgradeStateLabels(ctx)
	}

	r.OperatorMetrics.driverAutoUpgradeEnabled.Set(driverAutoUpgradeEnabled)

	// Build a cluster-wide upgrade state using only the component label so that ALL
	// driver pods are captured, including orphaned pods (e.g. pods left over from a
	// ClusterPolicy-managed DaemonSet).
	// TODO: decouple the operatorNamespace field from the ClusterPolicyController object
	clusterState, err := r.StateManager.BuildState(ctx, clusterPolicyCtrl.operatorNamespace, map[string]string{AppComponentLabelKey: DriverAppComponentLabelValue})
	if err != nil {
		r.Log.Error(err, "Failed to build cluster upgrade state")
		return ctrl.Result{}, err
	}

	if err := r.clearStaleUpgradeLabels(ctx, clusterState, map[string]string{AppComponentLabelKey: DriverAppComponentLabelValue}, clusterPolicyCtrl.operatorNamespace); err != nil {
		r.Log.Error(err, "Failed to clear stale upgrade labels")
	}

	// Partition the cluster upgrade state into per-NVIDIADriver buckets by reading the
	// nvidia.com/gpu-operator.driver.owner label from each node.
	statesByNVD := make(map[string]*upgrade.ClusterUpgradeState)
	for stateKey, nodeStates := range clusterState.NodeStates {
		for _, nodeState := range nodeStates {
			ownerName := nodeState.Node.Labels[gpuconsts.NVIDIADriverOwnerLabel]
			if ownerName == "" {
				reqLogger.V(consts.LogLevelInfo).Info("Node does not have nvidia.com/gpu-operator.driver.owner label, skipping ...", "NodeName", nodeState.Node.Name)
				continue
			}
			if statesByNVD[ownerName] == nil {
				s := upgrade.NewClusterUpgradeState()
				statesByNVD[ownerName] = &s
			}
			statesByNVD[ownerName].NodeStates[stateKey] = append(statesByNVD[ownerName].NodeStates[stateKey], nodeState)
		}
	}

	// Apply the upgrade policy for each NVIDIADriver instance using its partitioned cluster upgrade state
	for _, nvd := range nvidiaDriverList.Items {
		upgradePolicy := nvd.Spec.GetUpgradePolicyWithDefaults()
		if !upgradePolicy.AutoUpgrade {
			reqLogger.V(consts.LogLevelInfo).Info("Auto upgrade is disabled for NVIDIADriver, cleaning up upgrade state for nodes it manages",
				"name", nvd.Name)
			if err := r.removeNodeUpgradeStateLabelsForNVD(ctx, nvd.Name); err != nil {
				r.Log.Error(err, "Failed to remove upgrade state labels for NVIDIADriver", "name", nvd.Name)
				return ctrl.Result{}, err
			}
			continue
		}

		state, ok := statesByNVD[nvd.Name]
		if !ok {
			continue
		}

		reqLogger.V(consts.LogLevelDebug).Info("Current cluster upgrade state for NVIDIADriver",
			"name", nvd.Name, "state", state)

		totalNodes := r.StateManager.GetTotalManagedNodes(state)
		maxUnavailable, err := intstr.GetScaledValueFromIntOrPercent(upgradePolicy.MaxUnavailable, totalNodes, true)
		if err != nil {
			r.Log.Error(err, "Failed to compute maxUnavailable for NVIDIADriver", "name", nvd.Name)
			return ctrl.Result{}, err
		}

		upgradesInProgress += r.StateManager.GetUpgradesInProgress(state)
		upgradesDone += r.StateManager.GetUpgradesDone(state)
		upgradesAvailable += r.StateManager.GetUpgradesAvailable(state, upgradePolicy.MaxParallelUpgrades, maxUnavailable)
		upgradesFailed += r.StateManager.GetUpgradesFailed(state)
		upgradesPending += r.StateManager.GetUpgradesPending(state)

		// We want to skip the operator itself during the drain because the upgrade process might hang
		// if the operator is evicted and can't be rescheduled to any other node, e.g. in a single-node cluster.
		// It's safe to do because the goal of the node draining during the upgrade is to
		// evict pods that might use driver and operator doesn't use in its own pod.
		if upgradePolicy.DrainSpec.PodSelector == "" {
			upgradePolicy.DrainSpec.PodSelector = UpgradeSkipDrainLabelSelector
		} else {
			upgradePolicy.DrainSpec.PodSelector = fmt.Sprintf("%s,%s", upgradePolicy.DrainSpec.PodSelector, UpgradeSkipDrainLabelSelector)
		}

		reqLogger.Info("Applying upgrade policy for NVIDIADriver", "name", nvd.Name)
		if err := r.StateManager.ApplyState(ctx, state, upgradePolicy); err != nil {
			r.Log.Error(err, "Failed to apply cluster upgrade state for NVIDIADriver", "name", nvd.Name)
			return ctrl.Result{}, err
		}
	}

	// Capture aggregate metrics from all NVIDIADriver CRs processed. This should provide
	// a cluster-wide view of driver daemonset upgrades.
	r.OperatorMetrics.upgradesInProgress.Set(float64(upgradesInProgress))
	r.OperatorMetrics.upgradesDone.Set(float64(upgradesDone))
	r.OperatorMetrics.upgradesAvailable.Set(float64(upgradesAvailable))
	r.OperatorMetrics.upgradesFailed.Set(float64(upgradesFailed))
	r.OperatorMetrics.upgradesPending.Set(float64(upgradesPending))

	// In some cases if node state changes fail to apply, upgrade process
	// might become stuck until the new reconcile loop is scheduled.
	// Since node/ds/clusterpolicy updates from outside of the upgrade flow
	// are not guaranteed, for safety reconcile loop should be requeued every few minutes.
	return ctrl.Result{Requeue: true, RequeueAfter: plannedRequeueInterval}, nil
}

// clearStaleUpgradeLabels removes upgrade-state labels from nodes no longer selected by a driver DaemonSet.
func (r *UpgradeReconciler) clearStaleUpgradeLabels(ctx context.Context, state *upgrade.ClusterUpgradeState, driverLabel map[string]string, namespace string) error {
	upgradeStateLabel := upgrade.GetUpgradeStateLabelKey()
	managedNodes := make(map[string]bool)
	for _, nodeStates := range state.NodeStates {
		for _, nodeState := range nodeStates {
			if nodeState.Node != nil {
				managedNodes[nodeState.Node.Name] = true
			}
		}
	}

	nodeList := &corev1.NodeList{}
	if err := r.List(ctx, nodeList, client.HasLabels{upgradeStateLabel}); err != nil {
		return fmt.Errorf("list nodes with upgrade labels: %w", err)
	}

	driverDaemonSets := &appsv1.DaemonSetList{}
	if err := r.List(ctx, driverDaemonSets, client.InNamespace(namespace), client.MatchingLabels(driverLabel)); err != nil {
		return fmt.Errorf("list driver DaemonSets: %w", err)
	}

	for index := range nodeList.Items {
		node := &nodeList.Items[index]
		if managedNodes[node.Name] || selectedByDriverDaemonSet(node, driverDaemonSets.Items) {
			continue
		}

		patch := client.MergeFrom(node.DeepCopy())
		delete(node.Labels, upgradeStateLabel)
		if err := r.Patch(ctx, node, patch); err != nil {
			r.Log.Error(err, "Failed to clear stale upgrade label from node", "node", node.Name)
		}
	}

	return nil
}

func selectedByDriverDaemonSet(node *corev1.Node, driverDaemonSets []appsv1.DaemonSet) bool {
	for _, daemonSet := range driverDaemonSets {
		selector := labels.SelectorFromSet(daemonSet.Spec.Template.Spec.NodeSelector)
		if selector.Matches(labels.Set(node.Labels)) {
			return true
		}
	}
	return false
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

	for _, node := range nodeList.Items {
		if _, present := node.Labels[upgradeStateLabel]; !present {
			continue
		}
		patch := client.MergeFrom(node.DeepCopy())
		delete(node.Labels, upgradeStateLabel)
		err = r.Patch(ctx, &node, patch)
		if err != nil {
			r.Log.Error(err, "Failed to remove upgrade state label from node", "node", node)
			return err
		}

	}
	return nil
}

// removeNodeUpgradeStateLabelsForNVD removes the upgrade-state label from all nodes owned by
// the given NVIDIADriver CR. It is used for cleanup when autoUpgrade is disabled for that CR.
func (r *UpgradeReconciler) removeNodeUpgradeStateLabelsForNVD(ctx context.Context, nvdName string) error {
	r.Log.Info("Resetting node upgrade labels for NVIDIADriver", "name", nvdName)

	nodeList := &corev1.NodeList{}
	if err := r.List(ctx, nodeList, client.MatchingLabels{gpuconsts.NVIDIADriverOwnerLabel: nvdName}); err != nil {
		r.Log.Error(err, "Failed to list nodes for NVIDIADriver", "name", nvdName)
		return err
	}

	upgradeStateLabel := upgrade.GetUpgradeStateLabelKey()

	for _, node := range nodeList.Items {
		if _, present := node.Labels[upgradeStateLabel]; !present {
			continue
		}
		patch := client.MergeFrom(node.DeepCopy())
		delete(node.Labels, upgradeStateLabel)
		if err := r.Patch(ctx, &node, patch); err != nil {
			r.Log.Error(err, "Failed to remove upgrade state label from node", "node", node.Name)
			return err
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
		ls := metav1.LabelSelector{MatchLabels: map[string]string{AppComponentLabelKey: DriverAppComponentLabelValue}}
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
