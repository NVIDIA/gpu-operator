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
	"strings"
	"time"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
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
	"sigs.k8s.io/controller-runtime/pkg/handler"
	"sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/predicate"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
	"sigs.k8s.io/controller-runtime/pkg/source"

	nvidiav1alpha1 "github.com/NVIDIA/gpu-operator/api/nvidia/v1alpha1"
	"github.com/NVIDIA/gpu-operator/controllers/clusterinfo"
	"github.com/NVIDIA/gpu-operator/internal/conditions"
	"github.com/NVIDIA/gpu-operator/internal/consts"
	"github.com/NVIDIA/gpu-operator/internal/state"
)

// gpuClusterFinalizer holds the GPUCluster until reconcileDelete has ordered teardown.
const gpuClusterFinalizer = "gpucluster.nvidia.com/dra-resourceclaim"

// draAdminNamespaceLabelKey is the label the kube-scheduler requires on a namespace
// before it allows adminAccess: true in ResourceClaim/ResourceClaimTemplate objects.
const draAdminNamespaceLabelKey = "resource.kubernetes.io/admin-access"

// GPUClusterReconciler reconciles a GPUCluster object
type GPUClusterReconciler struct {
	client.Client
	Scheme      *runtime.Scheme
	ClusterInfo clusterinfo.Interface
	Namespace   string

	stateManager     state.Manager
	conditionUpdater conditions.Updater
	recorder         events.EventRecorder

	// singleton is the GPUCluster that owns reconciliation; the first instance to
	// reconcile claims it (first-wins), mirroring ClusterPolicy.
	singleton *nvidiav1alpha1.GPUCluster
}

//+kubebuilder:rbac:groups=nvidia.com,resources=gpuclusters,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups=nvidia.com,resources=gpuclusters/status,verbs=get;update;patch
//+kubebuilder:rbac:groups=nvidia.com,resources=gpuclusters/finalizers,verbs=update
//+kubebuilder:rbac:groups=nvidia.com,resources=clusterpolicies,verbs=get;list;watch
//+kubebuilder:rbac:groups="",resources=namespaces,verbs=get;update;patch
//+kubebuilder:rbac:groups=events.k8s.io,resources=events,verbs=create;patch
//+kubebuilder:rbac:groups=resource.k8s.io,resources=resourceclaimtemplates,verbs=get;list;watch;create;update;delete

func (r *GPUClusterReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	logger := log.FromContext(ctx)
	logger.V(consts.LogLevelInfo).Info("Reconciling GPUCluster")

	instance := &nvidiav1alpha1.GPUCluster{}
	if err := r.Get(ctx, req.NamespacedName, instance); err != nil {
		if apierrors.IsNotFound(err) {
			// Deleted; owned objects are garbage-collected, so there is nothing to clean up.
			return ctrl.Result{}, nil
		}
		// instance was not populated by the failed Get, so there is no object to
		// update status on; just surface the error for requeue.
		logger.Error(err, "error getting GPUCluster object")
		return ctrl.Result{}, fmt.Errorf("error getting GPUCluster object: %w", err)
	}

	if !instance.DeletionTimestamp.IsZero() {
		return r.reconcileDelete(ctx, instance)
	}
	if !controllerutil.ContainsFinalizer(instance, gpuClusterFinalizer) {
		controllerutil.AddFinalizer(instance, gpuClusterFinalizer)
		if err := r.Update(ctx, instance); err != nil {
			return ctrl.Result{}, fmt.Errorf("error adding finalizer: %w", err)
		}
	}

	// GPUCluster (DRA stack) may coexist with a ClusterPolicy (device-plugin
	// stack): every operand DaemonSet of both stacks gates on the per-node
	// nvidia.com/gpu-operator.resource-allocation.mode label, so each node is served by exactly one stack.

	// Singleton, first-wins (mirroring ClusterPolicy): the first instance to reconcile
	// claims ownership; any other instance is marked Ignored and skipped. The owner is
	// held in memory, so the choice resets on operator restart.
	if r.singleton != nil && r.singleton.Name != instance.Name {
		logger.V(consts.LogLevelWarning).Info("Multiple GPUCluster instances found, ignoring this one",
			"name", instance.Name, "owner", r.singleton.Name)
		if err := r.updateCRStatus(ctx, instance, nvidiav1alpha1.Ignored); err != nil {
			return ctrl.Result{}, err
		}
		return ctrl.Result{}, nil
	}
	r.singleton = instance

	// The operand states render ResourceClaimTemplates with adminAccess: true, which the
	// kube-scheduler only admits from a labeled namespace; label it before syncing states.
	if err := r.ensureAdminAccessLabel(ctx); err != nil {
		return ctrl.Result{}, fmt.Errorf("failed to label namespace for admin access: %w", err)
	}

	infoCatalog := state.NewInfoCatalog()
	infoCatalog.Add(state.InfoTypeClusterInfo, r.ClusterInfo)

	managerStatus := r.stateManager.SyncState(ctx, instance, infoCatalog)

	if err := r.updateCRStatus(ctx, instance, nvidiav1alpha1.State(managerStatus.Status)); err != nil {
		return ctrl.Result{}, err
	}

	if managerStatus.Status != state.SyncStateReady {
		logger.Info("GPUCluster instance is not ready")
		for _, result := range managerStatus.StatesStatus {
			if result.Status != state.SyncStateReady && result.ErrInfo != nil {
				if condErr := r.conditionUpdater.SetConditionsError(ctx, instance, conditions.ReconcileFailed, fmt.Sprintf("Error syncing state %s: %v", result.StateName, result.ErrInfo)); condErr != nil {
					logger.Error(condErr, "failed to set condition")
				}
				return ctrl.Result{RequeueAfter: time.Second * 5}, nil
			}
		}
		// no state reported an error, so we are waiting on operand pods
		if condErr := r.conditionUpdater.SetConditionsError(ctx, instance, conditions.OperandNotReady, "Waiting for operand pods to be ready"); condErr != nil {
			logger.Error(condErr, "failed to set condition")
		}
		return ctrl.Result{RequeueAfter: time.Second * 5}, nil
	}

	if condErr := r.conditionUpdater.SetConditionsReady(ctx, instance, conditions.Reconciled, "All resources have been successfully reconciled"); condErr != nil {
		logger.Error(condErr, "failed to set condition")
		return ctrl.Result{}, condErr
	}
	// Resync periodically so out-of-band changes (a deleted DeviceClass/VAP, or a
	// newly-created ClusterPolicy) are detected and reconciled even while ready;
	// only DaemonSets are watched, and the ready path is otherwise event-driven.
	return ctrl.Result{RequeueAfter: time.Minute}, nil
}

// ensureAdminAccessLabel patches the operator namespace with the label required by the
// kube-scheduler to allow adminAccess: true in ResourceClaim/ResourceClaimTemplate
// objects. The label is deliberately never removed: it is namespace-level configuration
// that other adminAccess consumers in the namespace may rely on.
func (r *GPUClusterReconciler) ensureAdminAccessLabel(ctx context.Context) error {
	ns := &corev1.Namespace{}
	if err := r.Get(ctx, client.ObjectKey{Name: r.Namespace}, ns); err != nil {
		return fmt.Errorf("could not get namespace %s: %w", r.Namespace, err)
	}
	if ns.Labels[draAdminNamespaceLabelKey] == "true" {
		return nil
	}
	patch := client.MergeFrom(ns.DeepCopy())
	if ns.Labels == nil {
		ns.Labels = make(map[string]string)
	}
	ns.Labels[draAdminNamespaceLabelKey] = "true"
	return r.Patch(ctx, ns, patch)
}

// reconcileDelete drains ResourceClaim-consuming DaemonSets before releasing the CR:
// garbage collection would otherwise delete the DRA kubelet plugin while their pods
// still need it to unprepare claims, leaving them stuck in Terminating. Foreground
// propagation keeps each DaemonSet present until its pods are gone, i.e. unprepared.
func (r *GPUClusterReconciler) reconcileDelete(ctx context.Context, instance *nvidiav1alpha1.GPUCluster) (ctrl.Result, error) {
	logger := log.FromContext(ctx)

	if !controllerutil.ContainsFinalizer(instance, gpuClusterFinalizer) {
		return ctrl.Result{}, nil
	}

	dsList := &appsv1.DaemonSetList{}
	if err := r.List(ctx, dsList, client.InNamespace(r.Namespace)); err != nil {
		return ctrl.Result{}, fmt.Errorf("error listing DaemonSets: %w", err)
	}
	var draining []string
	for i := range dsList.Items {
		ds := &dsList.Items[i]
		if !metav1.IsControlledBy(ds, instance) || len(ds.Spec.Template.Spec.ResourceClaims) == 0 {
			continue
		}
		draining = append(draining, ds.Name)
		if ds.DeletionTimestamp.IsZero() {
			logger.V(consts.LogLevelInfo).Info("Draining ResourceClaim-consuming DaemonSet before teardown", "DaemonSet", ds.Name)
			if err := r.Delete(ctx, ds, client.PropagationPolicy(metav1.DeletePropagationForeground)); err != nil && !apierrors.IsNotFound(err) {
				return ctrl.Result{}, fmt.Errorf("error deleting DaemonSet %s: %w", ds.Name, err)
			}
		}
	}
	if len(draining) > 0 {
		r.recorder.Eventf(instance, nil, corev1.EventTypeNormal, "DrainingClaimConsumers", "Delete",
			"Waiting for ResourceClaim-consuming DaemonSet(s) to terminate: %s", strings.Join(draining, ", "))
		return ctrl.Result{RequeueAfter: time.Second * 5}, nil
	}

	controllerutil.RemoveFinalizer(instance, gpuClusterFinalizer)
	if err := r.Update(ctx, instance); err != nil {
		return ctrl.Result{}, fmt.Errorf("error removing finalizer: %w", err)
	}
	return ctrl.Result{}, nil
}

// updateCRStatus persists the given state (and the operator namespace) to the GPUCluster's
// .status subresource. It refetches the CR first to avoid resourceVersion conflicts and skips
// the API write when the status is already current. The desired status is mirrored onto cr
// up front so it is set on every non-error path.
func (r *GPUClusterReconciler) updateCRStatus(ctx context.Context, cr *nvidiav1alpha1.GPUCluster, desired nvidiav1alpha1.State) error {
	reqLogger := log.FromContext(ctx)

	// Refetch to avoid a resourceVersion conflict.
	instance := &nvidiav1alpha1.GPUCluster{}
	if err := r.Get(ctx, types.NamespacedName{Name: cr.Name}, instance); err != nil {
		reqLogger.Error(err, "Failed to get GPUCluster instance for status update")
		return err
	}
	cr.Status.State = desired
	cr.Status.Namespace = r.Namespace

	if instance.Status.State == desired && instance.Status.Namespace == r.Namespace {
		return nil
	}
	instance.Status.State = desired
	instance.Status.Namespace = r.Namespace

	reqLogger.V(consts.LogLevelInfo).Info("Updating CR Status", "Status", instance.Status)
	if err := r.Status().Update(ctx, instance); err != nil {
		reqLogger.Error(err, "Failed to update CR status")
		return err
	}
	return nil
}

// enqueueAllGPUClusters enqueues every instance so each is reconciled when any
// instance or owned resource changes.
func (r *GPUClusterReconciler) enqueueAllGPUClusters(ctx context.Context, _ *nvidiav1alpha1.GPUCluster) []reconcile.Request {
	logger := log.FromContext(ctx)
	list := &nvidiav1alpha1.GPUClusterList{}

	if err := r.List(ctx, list); err != nil {
		logger.Error(err, "Unable to list GPUCluster resources")
		return []reconcile.Request{}
	}

	reconcileRequests := make([]reconcile.Request, 0, len(list.Items))
	for _, config := range list.Items {
		reconcileRequests = append(reconcileRequests,
			reconcile.Request{
				NamespacedName: types.NamespacedName{
					Name: config.GetName(),
				},
			})
	}

	return reconcileRequests
}

func (r *GPUClusterReconciler) SetupWithManager(ctx context.Context, mgr ctrl.Manager) error {
	// The state manager renders the DRA driver operand for the GPUCluster.
	stateManager, err := state.NewManager(
		nvidiav1alpha1.GPUClusterCRDName,
		r.Namespace,
		mgr.GetClient(),
		mgr.GetScheme())
	if err != nil {
		return fmt.Errorf("error creating state manager: %w", err)
	}
	r.stateManager = stateManager

	r.conditionUpdater = conditions.NewGPUClusterUpdater(mgr.GetClient())
	r.recorder = mgr.GetEventRecorder("nvidia-gpu-operator")

	c, err := controller.New("gpu-cluster-controller", mgr, controller.Options{
		Reconciler:              r,
		MaxConcurrentReconciles: 1,
		RateLimiter:             workqueue.NewTypedItemExponentialFailureRateLimiter[reconcile.Request](minDelayCR, maxDelayCR),
	})
	if err != nil {
		return err
	}

	err = c.Watch(source.Kind(
		mgr.GetCache(),
		&nvidiav1alpha1.GPUCluster{},
		handler.TypedEnqueueRequestsFromMapFunc(r.enqueueAllGPUClusters),
		predicate.TypedGenerationChangedPredicate[*nvidiav1alpha1.GPUCluster]{},
	),
	)
	if err != nil {
		return err
	}

	// Watch the secondary resources each state manager owns.
	watchSources := stateManager.GetWatchSources(mgr)
	for _, watchSource := range watchSources {
		err = c.Watch(
			watchSource,
		)
		if err != nil {
			return fmt.Errorf("error setting up Watch for source type %v: %w", watchSource, err)
		}
	}

	return nil
}
