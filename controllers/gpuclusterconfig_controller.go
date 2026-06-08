/*
Copyright 2025.

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

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/util/workqueue"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	"sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/predicate"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
	"sigs.k8s.io/controller-runtime/pkg/source"

	gpuv1 "github.com/NVIDIA/gpu-operator/api/nvidia/v1"
	nvidiav1alpha1 "github.com/NVIDIA/gpu-operator/api/nvidia/v1alpha1"
	"github.com/NVIDIA/gpu-operator/controllers/clusterinfo"
	"github.com/NVIDIA/gpu-operator/internal/conditions"
	"github.com/NVIDIA/gpu-operator/internal/consts"
	"github.com/NVIDIA/gpu-operator/internal/state"
)

// GPUClusterConfigReconciler reconciles a GPUClusterConfig object
type GPUClusterConfigReconciler struct {
	client.Client
	Scheme      *runtime.Scheme
	ClusterInfo clusterinfo.Interface
	Namespace   string

	stateManager     state.Manager
	conditionUpdater conditions.Updater

	// singleton is the GPUClusterConfig that owns reconciliation; the first instance to
	// reconcile claims it (first-wins), mirroring ClusterPolicy.
	singleton *nvidiav1alpha1.GPUClusterConfig
}

//+kubebuilder:rbac:groups=nvidia.com,resources=gpuclusterconfigs,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups=nvidia.com,resources=gpuclusterconfigs/status,verbs=get;update;patch
//+kubebuilder:rbac:groups=nvidia.com,resources=gpuclusterconfigs/finalizers,verbs=update
//+kubebuilder:rbac:groups=nvidia.com,resources=clusterpolicies,verbs=get;list;watch

func (r *GPUClusterConfigReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	logger := log.FromContext(ctx)
	logger.V(consts.LogLevelInfo).Info("Reconciling GPUClusterConfig")

	instance := &nvidiav1alpha1.GPUClusterConfig{}
	if err := r.Get(ctx, req.NamespacedName, instance); err != nil {
		if apierrors.IsNotFound(err) {
			// Deleted; owned objects are garbage-collected, so there is nothing to clean up.
			return ctrl.Result{}, nil
		}
		// instance was not populated by the failed Get, so there is no object to
		// update status on; just surface the error for requeue.
		logger.Error(err, "error getting GPUClusterConfig object")
		return ctrl.Result{}, fmt.Errorf("error getting GPUClusterConfig object: %w", err)
	}

	// GPUClusterConfig (DRA path) is mutually exclusive with ClusterPolicy: if one
	// exists, yield to it rather than deploying the DRA stack alongside it.
	clusterPolicies := &gpuv1.ClusterPolicyList{}
	if err := r.List(ctx, clusterPolicies); err != nil {
		return ctrl.Result{}, fmt.Errorf("error listing ClusterPolicies: %w", err)
	}
	if len(clusterPolicies.Items) > 0 {
		logger.V(consts.LogLevelWarning).Info("ClusterPolicy present, skipping mutually exclusive GPUClusterConfig")
		if err := r.updateCrStatus(ctx, instance, nvidiav1alpha1.Disabled); err != nil {
			return ctrl.Result{}, err
		}
		msg := "GPUClusterConfig is mutually exclusive with ClusterPolicy; remove the ClusterPolicy or disable GPUClusterConfig"
		if condErr := r.conditionUpdater.SetConditionsError(ctx, instance, conditions.ReconcileFailed, msg); condErr != nil {
			logger.Error(condErr, "failed to set condition")
		}
		return ctrl.Result{}, nil
	}

	// Singleton, first-wins (mirroring ClusterPolicy): the first instance to reconcile
	// claims ownership; any other instance is marked Ignored and skipped. The owner is
	// held in memory, so the choice resets on operator restart.
	if r.singleton != nil && r.singleton.Name != instance.Name {
		logger.V(consts.LogLevelWarning).Info("Multiple GPUClusterConfig instances found, ignoring this one",
			"name", instance.Name, "owner", r.singleton.Name)
		if err := r.updateCrStatus(ctx, instance, nvidiav1alpha1.Ignored); err != nil {
			return ctrl.Result{}, err
		}
		return ctrl.Result{}, nil
	}
	r.singleton = instance

	infoCatalog := state.NewInfoCatalog()
	infoCatalog.Add(state.InfoTypeClusterInfo, r.ClusterInfo)

	managerStatus := r.stateManager.SyncState(ctx, instance, infoCatalog)

	if err := r.updateCrStatus(ctx, instance, nvidiav1alpha1.State(managerStatus.Status)); err != nil {
		return ctrl.Result{}, err
	}

	if managerStatus.Status != state.SyncStateReady {
		logger.Info("GPUClusterConfig instance is not ready")
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

// updateCrStatus writes desired to the CR's status, skipping the write when it is already current.
func (r *GPUClusterConfigReconciler) updateCrStatus(ctx context.Context, cr *nvidiav1alpha1.GPUClusterConfig, desired nvidiav1alpha1.State) error {
	reqLogger := log.FromContext(ctx)

	// Refetch to avoid a resourceVersion conflict.
	instance := &nvidiav1alpha1.GPUClusterConfig{}
	if err := r.Get(ctx, types.NamespacedName{Name: cr.Name}, instance); err != nil {
		reqLogger.Error(err, "Failed to get GPUClusterConfig instance for status update")
		return err
	}

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
	cr.Status.State = instance.Status.State
	cr.Status.Namespace = instance.Status.Namespace
	return nil
}

// enqueueAllGPUClusterConfigs enqueues every instance so each is reconciled when any
// instance or owned resource changes.
func (r *GPUClusterConfigReconciler) enqueueAllGPUClusterConfigs(ctx context.Context, _ *nvidiav1alpha1.GPUClusterConfig) []reconcile.Request {
	logger := log.FromContext(ctx)
	list := &nvidiav1alpha1.GPUClusterConfigList{}

	if err := r.List(ctx, list); err != nil {
		logger.Error(err, "Unable to list GPUClusterConfig resources")
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

func (r *GPUClusterConfigReconciler) SetupWithManager(ctx context.Context, mgr ctrl.Manager) error {
	// The state manager renders the DRA driver operand for the GPUClusterConfig.
	stateManager, err := state.NewManager(
		nvidiav1alpha1.GPUClusterConfigCRDName,
		r.Namespace,
		mgr.GetClient(),
		mgr.GetScheme())
	if err != nil {
		return fmt.Errorf("error creating state manager: %v", err)
	}
	r.stateManager = stateManager

	r.conditionUpdater = conditions.NewGPUClusterConfigUpdater(mgr.GetClient())

	c, err := controller.New("gpu-cluster-config-controller", mgr, controller.Options{
		Reconciler:              r,
		MaxConcurrentReconciles: 1,
		RateLimiter:             workqueue.NewTypedItemExponentialFailureRateLimiter[reconcile.Request](minDelayCR, maxDelayCR),
	})
	if err != nil {
		return err
	}

	err = c.Watch(source.Kind(
		mgr.GetCache(),
		&nvidiav1alpha1.GPUClusterConfig{},
		handler.TypedEnqueueRequestsFromMapFunc(r.enqueueAllGPUClusterConfigs),
		predicate.TypedGenerationChangedPredicate[*nvidiav1alpha1.GPUClusterConfig]{},
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
