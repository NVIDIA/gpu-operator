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
	"github.com/go-logr/logr"
	"github.com/prometheus/common/log"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"

	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller"
	"sigs.k8s.io/controller-runtime/pkg/event"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	"sigs.k8s.io/controller-runtime/pkg/manager"
	"sigs.k8s.io/controller-runtime/pkg/predicate"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
	"sigs.k8s.io/controller-runtime/pkg/source"
	"time"

	gpuv1 "github.com/NVIDIA/gpu-operator/api/v1"
)

// blank assignment to verify that ReconcileClusterPolicy implements reconcile.Reconciler
var _ reconcile.Reconciler = &ClusterPolicyReconciler{}
var clusterPolicyCtrl ClusterPolicyController

// ClusterPolicyReconciler reconciles a ClusterPolicy object
type ClusterPolicyReconciler struct {
	client.Client
	Log    logr.Logger
	Scheme *runtime.Scheme
}

// +kubebuilder:rbac:groups=nvidia.com,resources=*,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=config.openshift.io,resources=clusterversions;proxies,verbs=get;list;watch
// +kubebuilder:rbac:groups=security.openshift.io,resources=securitycontextconstraints,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=rbac.authorization.k8s.io,resources=clusterroles;clusterrolebindings;roles;rolebindings,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups="",resources=namespaces;serviceaccounts;pods;services;services/finalizers;endpoints,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups="",resources=persistentvolumeclaims;events;configmaps;secrets;nodes,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=apps,resources=deployments;daemonsets;replicasets;statefulsets,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=monitoring.coreos.com,resources=servicemonitors,verbs=get;list;watch;create
// +kubebuilder:rbac:groups=scheduling.k8s.io,resources=priorityclasses,verbs=get;list;watch;create
// +kubebuilder:rbac:groups=batch,resources=jobs,verbs=get;list;watch;create;update;patch
// +kubebuilder:rbac:groups=route.openshift.io,resources=routes,verbs=get;list;watch;create;update;patch

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
	_ = context.Background()
	_ = r.Log.WithValues("Reconciling ClusterPolicy", req.NamespacedName)

	// Fetch the ClusterPolicy instance
	instance := &gpuv1.ClusterPolicy{}
	err := r.Client.Get(context.TODO(), req.NamespacedName, instance)
	if err != nil {
		clusterPolicyCtrl.operatorMetrics.reconciliationStatus.Set(reconciliationStatusClusterPolicyUnavailable)
		if errors.IsNotFound(err) {
			// Request object not found, could have been deleted after reconcile request.
			// Owned objects are automatically garbage collected. For additional cleanup logic use finalizers.
			// Return and don't requeue
			return reconcile.Result{}, nil
		}
		// Error reading the object - requeue the request.
		return reconcile.Result{}, err
	}

	// TODO: Handle deletion of the main ClusterPolicy and cycle to the next one.
	// We already have a main Clusterpolicy
	if clusterPolicyCtrl.singleton != nil && clusterPolicyCtrl.singleton.ObjectMeta.Name != instance.ObjectMeta.Name {
		instance.SetState(gpuv1.Ignored)
		// do not change `clusterPolicyCtrl.operatorMetrics.reconciliationStatus` here,
		// spurious reconciliation
		return ctrl.Result{}, err
	}

	err = clusterPolicyCtrl.init(r, instance)
	if err != nil {
		r.Log.Error(err, "Failed to initialize ClusterPolicy controller")
		clusterPolicyCtrl.operatorMetrics.reconciliationStatus.Set(reconciliationStatusClusterPolicyUnavailable)
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
	for {
		status, statusError := clusterPolicyCtrl.step()
		if statusError != nil {
			clusterPolicyCtrl.operatorMetrics.reconciliationStatus.Set(reconciliationStatusNotReady)
			clusterPolicyCtrl.operatorMetrics.reconciliationFailed.Inc()
			return ctrl.Result{RequeueAfter: time.Second * 5}, statusError
		}

		if status == gpuv1.NotReady {
			// if CR was previously set to ready(prior reboot etc), reset it to current state
			if instance.Status.State == gpuv1.Ready {
				updateCRState(r, req.NamespacedName, gpuv1.NotReady)
			}
			// If the resource is not ready, log status and proceed with other components
			r.Log.Info("ClusterPolicy step wasn't ready", "State:", status)
			overallStatus = gpuv1.NotReady
		}

		if clusterPolicyCtrl.last() {
			break
		}
	}

	// if any state is not ready, requeue for reconfile after 5 seconds
	if overallStatus != gpuv1.Ready {
		clusterPolicyCtrl.operatorMetrics.reconciliationStatus.Set(reconciliationStatusNotReady)
		clusterPolicyCtrl.operatorMetrics.reconciliationFailed.Inc()
		return ctrl.Result{RequeueAfter: time.Second * 5}, nil
	}

	if !clusterPolicyCtrl.hasNFDLabels {
		// no NFD-labelled node in the cluster (required dependency),
		// watch periodically for the labels to appear
		var requeueAfter = time.Second * 45
		r.Log.Info("No NFD label found, polling for new nodes.",
			"requeueAfter", requeueAfter)

		// Update CR state as ready as all states are complete
		updateCRState(r, req.NamespacedName, gpuv1.NotReady)
		clusterPolicyCtrl.operatorMetrics.reconciliationStatus.Set(reconciliationStatusNotReady)

		return ctrl.Result{RequeueAfter: requeueAfter}, nil
	}

	// Update CR state as ready as all states are complete
	updateCRState(r, req.NamespacedName, gpuv1.Ready)
	clusterPolicyCtrl.operatorMetrics.reconciliationStatus.Set(reconciliationStatusSuccess)
	clusterPolicyCtrl.operatorMetrics.reconciliationLastSuccess.Set(float64(time.Now().Unix()))

	if !clusterPolicyCtrl.hasGPUNodes {
		r.Log.Info("No GPU node found, watching for new nodes to join the cluster.", "hasNFDLabels", clusterPolicyCtrl.hasNFDLabels)
	}

	return ctrl.Result{}, nil
}

func updateCRState(r *ClusterPolicyReconciler, namespacedName types.NamespacedName, state gpuv1.State) error {
	// Fetch latest instance and update state to avoid version mismatch
	instance := &gpuv1.ClusterPolicy{}
	err := r.Client.Get(context.TODO(), namespacedName, instance)
	if err != nil {
		r.Log.Error(err, "Failed to get ClusterPolicy instance for status update")
		return err
	}
	// Update the CR state
	instance.SetState(state)
	err = r.Client.Status().Update(context.TODO(), instance)
	if err != nil {
		r.Log.Error(err, "Failed to update ClusterPolicy status")
		return err
	}
	return nil
}

func addWatchNewGPUNode(r *ClusterPolicyReconciler, c controller.Controller, mgr manager.Manager) error {
	// Define a mapping from the Node object in the event to one or more
	// ClusterPolicy objects to Reconcile
	mapFn := func(a client.Object) []reconcile.Request {
		// find all the ClusterPolicy to trigger their reconciliation
		opts := []client.ListOption{} // Namespace = "" to list across all namespaces.
		list := &gpuv1.ClusterPolicyList{}

		err := mgr.GetClient().List(context.TODO(), list, opts...)
		if err != nil {
			r.Log.Error(err, "Unable to list ClusterPolicies")
			return []reconcile.Request{}
		}

		cpToRec := []reconcile.Request{}

		for _, cp := range list.Items {
			cpToRec = append(cpToRec, reconcile.Request{NamespacedName: types.NamespacedName{
				Name:      cp.ObjectMeta.GetName(),
				Namespace: cp.ObjectMeta.GetNamespace(),
			}})
		}
		r.Log.Info("Reconciliate ClusterPolicies after node label update", "nb", len(cpToRec))

		return cpToRec
	}

	p := predicate.Funcs{
		CreateFunc: func(e event.CreateEvent) bool {
			labels := e.Object.GetLabels()

			gpuCommonLabelMissing := hasGPULabels(labels) && !hasCommonGPULabel(labels)
			if gpuCommonLabelMissing {
				log.Info("New node needs an update, GPU common label missing.",
					"name", e.Object.GetName())
			}
			return gpuCommonLabelMissing
		},
		UpdateFunc: func(e event.UpdateEvent) bool {
			newLabels := e.ObjectNew.GetLabels()

			gpuCommonLabelMissing := hasGPULabels(newLabels) && !hasCommonGPULabel(newLabels)
			gpuCommonLabelOutdated := !hasGPULabels(newLabels) && hasCommonGPULabel(newLabels)
			migManagerLabelMissing := hasMIGCapableGPU(newLabels) && !hasMIGManagerLabel(newLabels)
			needsUpdate := gpuCommonLabelMissing || gpuCommonLabelOutdated || migManagerLabelMissing
			if needsUpdate {
				log.Info("Node needs an update",
					"name", e.ObjectNew.GetName(),
					"gpuCommonLabelMissing", gpuCommonLabelMissing,
					"gpuCommonLabelOutdated", gpuCommonLabelOutdated,
					"migManagerLabelMissing", migManagerLabelMissing)
			}
			return needsUpdate
		},
	}

	err := c.Watch(
		&source.Kind{Type: &corev1.Node{}},
		handler.EnqueueRequestsFromMapFunc(mapFn),
		p)

	return err
}

// SetupWithManager sets up the controller with the Manager.
func (r *ClusterPolicyReconciler) SetupWithManager(mgr ctrl.Manager) error {
	// Create a new controller
	c, err := controller.New("clusterpolicy-controller", mgr, controller.Options{Reconciler: r})
	if err != nil {
		return err
	}

	// Watch for changes to primary resource ClusterPolicy
	err = c.Watch(&source.Kind{Type: &gpuv1.ClusterPolicy{}}, &handler.EnqueueRequestForObject{})
	if err != nil {
		return err
	}

	// Watch for changes to Node labels and requeue the owner ClusterPolicy
	err = addWatchNewGPUNode(r, c, mgr)
	if err != nil {
		return err
	}

	// TODO(user): Modify this to be the types you create that are owned by the primary resource
	// Watch for changes to secondary resource Pods and requeue the owner ClusterPolicy
	err = c.Watch(&source.Kind{Type: &corev1.Pod{}}, &handler.EnqueueRequestForOwner{
		IsController: true,
		OwnerType:    &gpuv1.ClusterPolicy{},
	})
	if err != nil {
		return err
	}

	return nil
}
