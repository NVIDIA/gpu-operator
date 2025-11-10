/*
Copyright 2021.

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

	"github.com/go-logr/logr"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/util/workqueue"

	"time"

	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller"
	"sigs.k8s.io/controller-runtime/pkg/event"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	"sigs.k8s.io/controller-runtime/pkg/predicate"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
	"sigs.k8s.io/controller-runtime/pkg/source"

	gpuv1 "github.com/NVIDIA/gpu-operator/api/nvidia/v1"
	"github.com/NVIDIA/gpu-operator/internal/conditions"
)

const (
	minDelayCR                      = 100 * time.Millisecond
	maxDelayCR                      = 3 * time.Second
	clusterPolicyControllerIndexKey = "metadata.nvidia.clusterpolicy.controller"
)

// blank assignment to verify that ReconcileClusterPolicy implements reconcile.Reconciler
var _ reconcile.Reconciler = &ClusterPolicyReconciler{}
var clusterPolicyCtrl ClusterPolicyController

// ClusterPolicyReconciler reconciles a ClusterPolicy object
type ClusterPolicyReconciler struct {
	client.Client
	Log              logr.Logger
	Scheme           *runtime.Scheme
	Namespace        string
	conditionUpdater conditions.Updater
}

// +kubebuilder:rbac:groups=nvidia.com,resources=*,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=config.openshift.io,resources=clusterversions;proxies,verbs=get;list;watch
// +kubebuilder:rbac:groups=security.openshift.io,resources=securitycontextconstraints,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=security.openshift.io,resources=securitycontextconstraints,verbs=use,resourceNames=privileged
// +kubebuilder:rbac:groups=rbac.authorization.k8s.io,resources=clusterroles;clusterrolebindings;roles;rolebindings,verbs=*
// +kubebuilder:rbac:groups="",resources=namespaces;serviceaccounts;pods;pods/eviction;services;services/finalizers;endpoints,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups="",resources=persistentvolumeclaims;events;configmaps;secrets;nodes,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=apps,resources=deployments;daemonsets;replicasets;statefulsets,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=apps,resources=controllerrevisions,verbs=get;list;watch
// +kubebuilder:rbac:groups=monitoring.coreos.com,resources=servicemonitors;prometheusrules,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=scheduling.k8s.io,resources=priorityclasses,verbs=get;list;watch;create
// +kubebuilder:rbac:groups=batch,resources=jobs,verbs=get;list;watch;create;update;patch
// +kubebuilder:rbac:groups=route.openshift.io,resources=routes,verbs=get;list;watch;create;update;patch
// +kubebuilder:rbac:groups=image.openshift.io,resources=imagestreams,verbs=get;list;watch
// +kubebuilder:rbac:groups=node.k8s.io,resources=runtimeclasses,verbs=get;list;create;update;watch;delete
// +kubebuilder:rbac:groups=apiextensions.k8s.io,resources=customresourcedefinitions,verbs=get;list;watch

// Reconcile is part of the main kubernetes reconciliation loop which aims to
// move the current state of the cluster closer to the desired state.
// TODO(user): Modify the Reconcile function to compare the state specified by
// the ClusterPolicy object against the actual cluster state, and then
// perform operations to make the cluster state reflect the state specified by
// the user.
//
// For more details, check Reconcile and its Result here:
// - https://pkg.go.dev/sigs.k8s.io/controller-runtime@v0.7.0/pkg/reconcile
func (r *ClusterPolicyReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	_ = r.Log.WithValues("Reconciling ClusterPolicy", req.NamespacedName)

	// Fetch the ClusterPolicy instance
	instance := &gpuv1.ClusterPolicy{}
	if err := r.Get(ctx, req.NamespacedName, instance); err != nil {
		err = fmt.Errorf("failed to get ClusterPolicy object: %w", err)
		r.Log.Error(err, "unable to fetch ClusterPolicy")
		clusterPolicyCtrl.operatorMetrics.reconciliationStatus.Set(reconciliationStatusClusterPolicyUnavailable)
		if apierrors.IsNotFound(err) {
			// Request object not found, could have been deleted after reconcile request.
			// Owned objects are automatically garbage collected. For additional cleanup logic use finalizers.
			// Return and don't requeue
			return reconcile.Result{}, nil
		}
		// Error reading the object - requeue the request.
		if condErr := r.conditionUpdater.SetConditionsError(ctx, instance, conditions.ReconcileFailed, err.Error()); condErr != nil {
			r.Log.Error(condErr, "failed to set condition")
		}
		return reconcile.Result{}, err
	}

	// TODO: Handle deletion of the main ClusterPolicy and cycle to the next one.
	// We already have a main Clusterpolicy
	if clusterPolicyCtrl.singleton != nil && clusterPolicyCtrl.singleton.Name != instance.Name {
		instance.SetStatus(gpuv1.Ignored, clusterPolicyCtrl.operatorNamespace)
		// do not change `clusterPolicyCtrl.operatorMetrics.reconciliationStatus` here,
		// spurious reconciliation
		return ctrl.Result{}, nil
	}

	if err := clusterPolicyCtrl.init(ctx, r, instance); err != nil {
		r.Log.Error(err, "unable to initialize ClusterPolicy controller")
		if condErr := r.conditionUpdater.SetConditionsError(ctx, instance, conditions.ReconcileFailed, err.Error()); condErr != nil {
			r.Log.Error(condErr, "failed to set condition")
		}
		if clusterPolicyCtrl.operatorMetrics != nil {
			clusterPolicyCtrl.operatorMetrics.reconciliationStatus.Set(reconciliationStatusClusterPolicyUnavailable)
		}
		return ctrl.Result{}, err
	}

	if !clusterPolicyCtrl.hasNFDLabels {
		r.Log.Info("WARNING: NFD labels missing in the cluster, GPU nodes cannot be discovered.")
		clusterPolicyCtrl.operatorMetrics.reconciliationHasNFDLabels.Set(0)
	} else {
		clusterPolicyCtrl.operatorMetrics.reconciliationHasNFDLabels.Set(1)
	}
	if !clusterPolicyCtrl.hasGPUNodes {
		r.Log.Info("No GPU node can be found in the cluster.")
	}

	clusterPolicyCtrl.operatorMetrics.reconciliationTotal.Inc()
	overallStatus := gpuv1.Ready
	statesNotReady := []string{}
	for {
		status, statusError := clusterPolicyCtrl.step()
		if statusError != nil {
			clusterPolicyCtrl.operatorMetrics.reconciliationStatus.Set(reconciliationStatusNotReady)
			clusterPolicyCtrl.operatorMetrics.reconciliationFailed.Inc()
			updateCRState(ctx, r, req.NamespacedName, gpuv1.NotReady)
			if condErr := r.conditionUpdater.SetConditionsError(ctx, instance, conditions.ReconcileFailed, fmt.Sprintf("Failed to reconcile %s: %s", clusterPolicyCtrl.stateNames[clusterPolicyCtrl.idx], statusError.Error())); condErr != nil {
				r.Log.Error(condErr, "failed to set condition")
			}
			return ctrl.Result{}, statusError
		}

		if status == gpuv1.NotReady {
			overallStatus = gpuv1.NotReady
			statesNotReady = append(statesNotReady, clusterPolicyCtrl.stateNames[clusterPolicyCtrl.idx-1])
		}
		r.Log.Info("ClusterPolicy step completed",
			"state:", clusterPolicyCtrl.stateNames[clusterPolicyCtrl.idx-1],
			"status", status)

		if clusterPolicyCtrl.last() {
			break
		}
	}

	// if any state is not ready, requeue for reconcile after 5 seconds
	if overallStatus != gpuv1.Ready {
		clusterPolicyCtrl.operatorMetrics.reconciliationStatus.Set(reconciliationStatusNotReady)
		clusterPolicyCtrl.operatorMetrics.reconciliationFailed.Inc()

		err := fmt.Errorf("ClusterPolicy is not ready, states not ready: %v", statesNotReady)
		r.Log.Error(err, "ClusterPolicy not yet ready")
		updateCRState(ctx, r, req.NamespacedName, gpuv1.NotReady)
		if condErr := r.conditionUpdater.SetConditionsError(ctx, instance, conditions.OperandNotReady, err.Error()); condErr != nil {
			r.Log.Error(condErr, "failed to set condition")
		}
		return ctrl.Result{RequeueAfter: time.Second * 5}, nil
	}

	if !clusterPolicyCtrl.hasNFDLabels {
		// no NFD-labelled node in the cluster (required dependency),
		// watch periodically for the labels to appear
		var requeueAfter = time.Second * 45
		r.Log.Info("No NFD label found, polling for new nodes.",
			"requeueAfter", requeueAfter)

		// Update CR state as ready as all states are complete
		updateCRState(ctx, r, req.NamespacedName, gpuv1.Ready)
		if condErr := r.conditionUpdater.SetConditionsReady(ctx, instance, conditions.NFDLabelsMissing, "No NFD labels found"); condErr != nil {
			r.Log.Error(condErr, "failed to set condition")
		}

		clusterPolicyCtrl.operatorMetrics.reconciliationStatus.Set(reconciliationStatusSuccess)

		return ctrl.Result{RequeueAfter: requeueAfter}, nil
	}

	// Update CR state as ready as all states are complete
	updateCRState(ctx, r, req.NamespacedName, gpuv1.Ready)
	clusterPolicyCtrl.operatorMetrics.reconciliationStatus.Set(reconciliationStatusSuccess)
	clusterPolicyCtrl.operatorMetrics.reconciliationLastSuccess.Set(float64(time.Now().Unix()))

	var infoStr string
	if !clusterPolicyCtrl.hasGPUNodes {
		infoStr = "No GPU node found, watching for new nodes to join the cluster."
		r.Log.Info(infoStr, "hasNFDLabels", clusterPolicyCtrl.hasNFDLabels)
		if condErr := r.conditionUpdater.SetConditionsReady(ctx, instance, conditions.NoGPUNodes, infoStr); condErr != nil {
			r.Log.Error(condErr, "failed to set condition")
			return ctrl.Result{}, condErr
		}
	} else {
		infoStr = "ClusterPolicy is ready as all resources have been successfully reconciled"
		r.Log.Info(infoStr)
		if condErr := r.conditionUpdater.SetConditionsReady(ctx, instance, conditions.Reconciled, infoStr); condErr != nil {
			r.Log.Error(condErr, "failed to set condition")
			return ctrl.Result{}, condErr
		}
	}
	return ctrl.Result{}, nil
}

func updateCRState(ctx context.Context, r *ClusterPolicyReconciler, namespacedName types.NamespacedName, state gpuv1.State) {
	// Fetch latest instance and update state to avoid version mismatch
	instance := &gpuv1.ClusterPolicy{}
	if err := r.Get(ctx, namespacedName, instance); err != nil {
		r.Log.Error(err, "Failed to get ClusterPolicy instance for status update")
	}
	if instance.Status.State == state {
		// state is unchanged
		return
	}
	// Update the CR state
	instance.SetStatus(state, clusterPolicyCtrl.operatorNamespace)
	if err := r.Client.Status().Update(ctx, instance); err != nil {
		r.Log.Error(err, "Failed to update ClusterPolicy status")
	}
}

func addWatchNewGPUNode(r *ClusterPolicyReconciler, c controller.Controller, mgr ctrl.Manager) error {
	// Define a mapping from the Node object in the event to one or more
	// ClusterPolicy objects to Reconcile
	mapFn := func(ctx context.Context, n *corev1.Node) []reconcile.Request {
		// find all the ClusterPolicy to trigger their reconciliation
		opts := []client.ListOption{} // Namespace = "" to list across all namespaces.
		list := &gpuv1.ClusterPolicyList{}

		err := r.List(ctx, list, opts...)
		if err != nil {
			r.Log.Error(err, "Unable to list ClusterPolicies")
			return []reconcile.Request{}
		}

		cpToRec := []reconcile.Request{}

		for _, cp := range list.Items {
			cpToRec = append(cpToRec, reconcile.Request{NamespacedName: types.NamespacedName{
				Name:      cp.GetName(),
				Namespace: cp.GetNamespace(),
			}})
		}
		r.Log.Info("Reconciliate ClusterPolicies after node label update", "nb", len(cpToRec))

		return cpToRec
	}

	p := predicate.TypedFuncs[*corev1.Node]{
		CreateFunc: func(e event.TypedCreateEvent[*corev1.Node]) bool {
			labels := e.Object.GetLabels()

			return hasGPULabels(labels)
		},
		UpdateFunc: func(e event.TypedUpdateEvent[*corev1.Node]) bool {
			newLabels := e.ObjectNew.GetLabels()
			oldLabels := e.ObjectOld.GetLabels()
			nodeName := e.ObjectNew.GetName()

			gpuCommonLabelMissing := hasGPULabels(newLabels) && !hasCommonGPULabel(newLabels)
			gpuCommonLabelOutdated := !hasGPULabels(newLabels) && hasCommonGPULabel(newLabels)
			migManagerLabelMissing := hasMIGCapableGPU(newLabels) && !hasMIGManagerLabel(newLabels)
			commonOperandsLabelChanged := hasOperandsDisabled(oldLabels) != hasOperandsDisabled(newLabels)

			oldGPUWorkloadConfig, _ := getWorkloadConfig(oldLabels, true)
			newGPUWorkloadConfig, _ := getWorkloadConfig(newLabels, true)
			gpuWorkloadConfigLabelChanged := oldGPUWorkloadConfig != newGPUWorkloadConfig

			oldOSTreeLabel := oldLabels[nfdOSTreeVersionLabelKey]
			newOSTreeLabel := newLabels[nfdOSTreeVersionLabelKey]
			osTreeLabelChanged := oldOSTreeLabel != newOSTreeLabel

			needsUpdate := gpuCommonLabelMissing ||
				gpuCommonLabelOutdated ||
				migManagerLabelMissing ||
				commonOperandsLabelChanged ||
				gpuWorkloadConfigLabelChanged ||
				osTreeLabelChanged

			if needsUpdate {
				r.Log.Info("Node needs an update",
					"name", nodeName,
					"gpuCommonLabelMissing", gpuCommonLabelMissing,
					"gpuCommonLabelOutdated", gpuCommonLabelOutdated,
					"migManagerLabelMissing", migManagerLabelMissing,
					"commonOperandsLabelChanged", commonOperandsLabelChanged,
					"gpuWorkloadConfigLabelChanged", gpuWorkloadConfigLabelChanged,
					"osTreeLabelChanged", osTreeLabelChanged,
				)
			}
			return needsUpdate
		},
		DeleteFunc: func(e event.TypedDeleteEvent[*corev1.Node]) bool {
			// if an RHCOS GPU node is deleted, trigger a
			// reconciliation to ensure that there is no dangling
			// OpenShift Driver-Toolkit (RHCOS version-specific)
			// DaemonSet.
			// NB: we cannot know here if the DriverToolkit is
			// enabled.

			labels := e.Object.GetLabels()

			_, hasOSTreeLabel := labels[nfdOSTreeVersionLabelKey]

			return hasGPULabels(labels) && hasOSTreeLabel
		},
	}

	err := c.Watch(
		source.Kind(mgr.GetCache(),
			&corev1.Node{},
			handler.TypedEnqueueRequestsFromMapFunc[*corev1.Node](mapFn),
			p,
		),
	)

	return err
}

// SetupWithManager sets up the controller with the Manager.
func (r *ClusterPolicyReconciler) SetupWithManager(ctx context.Context, mgr ctrl.Manager) error {
	// Create a new controller
	c, err := controller.New("clusterpolicy-controller", mgr, controller.Options{Reconciler: r, MaxConcurrentReconciles: 1,
		RateLimiter: workqueue.NewTypedItemExponentialFailureRateLimiter[reconcile.Request](minDelayCR, maxDelayCR)})
	if err != nil {
		return err
	}

	// initialize condition updater
	r.conditionUpdater = conditions.NewClusterPolicyUpdater(mgr.GetClient())

	// Watch for changes to primary resource ClusterPolicy
	err = c.Watch(source.Kind(
		mgr.GetCache(),
		&gpuv1.ClusterPolicy{},
		&handler.TypedEnqueueRequestForObject[*gpuv1.ClusterPolicy]{},
		predicate.TypedGenerationChangedPredicate[*gpuv1.ClusterPolicy]{},
	),
	)
	if err != nil {
		return err
	}

	// Watch for changes to Node labels and requeue the owner ClusterPolicy
	err = addWatchNewGPUNode(r, c, mgr)
	if err != nil {
		return err
	}

	// TODO(user): Modify this to be the types you create that are owned by the primary resource
	// Watch for changes to secondary resource Daemonsets and requeue the owner ClusterPolicy
	err = c.Watch(
		source.Kind(mgr.GetCache(),
			&appsv1.DaemonSet{},
			handler.TypedEnqueueRequestForOwner[*appsv1.DaemonSet](mgr.GetScheme(), mgr.GetRESTMapper(), &gpuv1.ClusterPolicy{},
				handler.OnlyControllerOwner()),
		),
	)
	if err != nil {
		return err
	}

	// Add an index key which allows our reconciler to quickly look up DaemonSets owned by it.
	//
	// (cdesiniotis) Ideally we could duplicate this index for all the k8s objects
	// that ClusterPolicy manages, that way, we could easily restrict the ClusterPolicy
	// controller to only update / delete objects it owns. Unfortunately, the
	// underlying implementation of the index does not support generic container types
	// (i.e. unstructured.Unstructured{}). For additional details, see the comment in
	// the last link of the below call stack:
	// IndexField(): https://github.com/kubernetes-sigs/controller-runtime/blob/main/pkg/cache/informer_cache.go#L204
	//   GetInformer(): https://github.com/kubernetes-sigs/controller-runtime/blob/main/pkg/cache/informer_cache.go#L168
	//     GVKForObject(): https://github.com/kubernetes-sigs/controller-runtime/blob/main/pkg/client/apiutil/apimachinery.go#L113
	if err := mgr.GetFieldIndexer().IndexField(ctx, &appsv1.DaemonSet{}, clusterPolicyControllerIndexKey, func(rawObj client.Object) []string {
		ds := rawObj.(*appsv1.DaemonSet)
		owner := metav1.GetControllerOf(ds)
		if owner == nil {
			return nil
		}
		if owner.APIVersion != gpuv1.SchemeGroupVersion.String() || owner.Kind != "ClusterPolicy" {
			return nil
		}
		return []string{owner.Name}
	}); err != nil {
		return fmt.Errorf("failed to add index key: %w", err)
	}

	return nil
}
