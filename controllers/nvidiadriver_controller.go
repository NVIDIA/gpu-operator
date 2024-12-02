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
	"maps"
	"time"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/util/workqueue"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller"
	"sigs.k8s.io/controller-runtime/pkg/event"
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
	"github.com/NVIDIA/gpu-operator/internal/validator"
)

// NVIDIADriverReconciler reconciles a NVIDIADriver object
type NVIDIADriverReconciler struct {
	client.Client
	Scheme      *runtime.Scheme
	ClusterInfo clusterinfo.Interface

	stateManager          state.Manager
	nodeSelectorValidator validator.Validator
	conditionUpdater      conditions.Updater
}

//+kubebuilder:rbac:groups=nvidia.com,resources=nvidiadrivers,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups=nvidia.com,resources=nvidiadrivers/status,verbs=get;update;patch
//+kubebuilder:rbac:groups=nvidia.com,resources=nvidiadrivers/finalizers,verbs=update

// Reconcile is part of the main kubernetes reconciliation loop which aims to
// move the current state of the cluster closer to the desired state.
// TODO(user): Modify the Reconcile function to compare the state specified by
// the NVIDIADriver object against the actual cluster state, and then
// perform operations to make the cluster state reflect the state specified by
// the user.
//
// For more details, check Reconcile and its Result here:
// - https://pkg.go.dev/sigs.k8s.io/controller-runtime@v0.8.3/pkg/reconcile
func (r *NVIDIADriverReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	logger := log.FromContext(ctx)
	logger.V(consts.LogLevelInfo).Info("Reconciling NVIDIADriver")

	// Get the NvidiaDriver instance from this request
	instance := &nvidiav1alpha1.NVIDIADriver{}
	var condErr error
	err := r.Client.Get(ctx, req.NamespacedName, instance)
	if err != nil {
		if apierrors.IsNotFound(err) {
			// Request object not found, could have been deleted after reconcile request.
			// Owned objects are automatically garbage collected. For additional cleanup logic use finalizers.
			// Return and don't requeue
			return reconcile.Result{}, nil
		}
		err = fmt.Errorf("Error getting NVIDIADriver object: %w", err)
		logger.V(consts.LogLevelError).Error(nil, err.Error())
		instance.Status.State = nvidiav1alpha1.NotReady
		condErr = r.conditionUpdater.SetConditionsError(ctx, instance, conditions.ReconcileFailed, err.Error())
		if condErr != nil {
			logger.V(consts.LogLevelDebug).Error(nil, condErr.Error())
		}
		// Error reading the object - requeue the request.
		return reconcile.Result{}, err
	}

	// Get the singleton NVIDIA ClusterPolicy object in the cluster.
	clusterPolicyList := &gpuv1.ClusterPolicyList{}
	err = r.Client.List(ctx, clusterPolicyList)
	if err != nil {
		err = fmt.Errorf("Error getting ClusterPolicy list: %v", err)
		logger.V(consts.LogLevelError).Error(nil, err.Error())
		instance.Status.State = nvidiav1alpha1.NotReady
		condErr = r.conditionUpdater.SetConditionsError(ctx, instance, conditions.ReconcileFailed, err.Error())
		if condErr != nil {
			logger.V(consts.LogLevelDebug).Error(nil, condErr.Error())
		}
		return reconcile.Result{}, fmt.Errorf("error getting ClusterPolicyList: %v", err)
	}

	if len(clusterPolicyList.Items) == 0 {
		err = fmt.Errorf("no ClusterPolicy object found in the cluster")
		logger.V(consts.LogLevelError).Error(nil, err.Error())
		instance.Status.State = nvidiav1alpha1.NotReady
		condErr = r.conditionUpdater.SetConditionsError(ctx, instance, conditions.ReconcileFailed, err.Error())
		if condErr != nil {
			logger.V(consts.LogLevelDebug).Error(nil, condErr.Error())
		}
		return reconcile.Result{}, err
	}
	clusterPolicyInstance := clusterPolicyList.Items[0]

	// Create a new InfoCatalog which is a generic interface for passing information to state managers
	infoCatalog := state.NewInfoCatalog()

	// Add an entry for ClusterInfo, which was collected before the NVIDIADriver controller was started
	infoCatalog.Add(state.InfoTypeClusterInfo, r.ClusterInfo)

	// Add an entry for Clusterpolicy, which is needed to deploy the driver daemonset
	infoCatalog.Add(state.InfoTypeClusterPolicyCR, clusterPolicyInstance)

	// Verify the nodeSelector configured for this NVIDIADriver instance does
	// not conflict with any other instances. This ensures only one driver
	// is deployed per GPU node.
	err = r.nodeSelectorValidator.Validate(ctx, instance)
	if err != nil {
		logger.V(consts.LogLevelError).Error(nil, err.Error())
		condErr = r.conditionUpdater.SetConditionsError(ctx, instance, conditions.ConflictingNodeSelector, err.Error())
		if condErr != nil {
			logger.V(consts.LogLevelDebug).Error(nil, condErr.Error())
		}
		return reconcile.Result{}, nil
	}

	if instance.Spec.UsePrecompiledDrivers() && (instance.Spec.IsGDSEnabled() || instance.Spec.IsGDRCopyEnabled()) {
		err = fmt.Errorf("GPUDirect Storage driver (nvidia-fs) and/or GDRCopy driver is not supported along with pre-compiled NVIDIA drivers")
		logger.V(consts.LogLevelError).Error(nil, err.Error())
		instance.Status.State = nvidiav1alpha1.NotReady
		condErr = r.conditionUpdater.SetConditionsError(ctx, instance, conditions.ReconcileFailed, err.Error())
		if condErr != nil {
			logger.V(consts.LogLevelDebug).Error(nil, condErr.Error())
		}
		return reconcile.Result{}, nil
	}

	if instance.Spec.IsGDSEnabled() && instance.Spec.IsOpenKernelModulesRequired() && !instance.Spec.IsOpenKernelModulesEnabled() {
		err = fmt.Errorf("GPUDirect Storage driver '%s' is only supported with NVIDIA OpenRM drivers. Please set 'useOpenKernelModules=true' to enable OpenRM mode", instance.Spec.GPUDirectStorage.Version)
		logger.V(consts.LogLevelError).Error(nil, err.Error())
		instance.Status.State = nvidiav1alpha1.NotReady
		condErr = r.conditionUpdater.SetConditionsError(ctx, instance, conditions.ReconcileFailed, err.Error())
		if condErr != nil {
			logger.V(consts.LogLevelDebug).Error(nil, condErr.Error())
		}
		return reconcile.Result{}, nil
	}

	// Sync state and update status
	managerStatus := r.stateManager.SyncState(ctx, instance, infoCatalog)

	// update CR status
	err = r.updateCrStatus(ctx, instance, managerStatus)
	if err != nil {
		return ctrl.Result{}, err
	}

	if managerStatus.Status != state.SyncStateReady {
		logger.Info("NVIDIADriver instance is not ready")
		var errorInfo error
		for _, result := range managerStatus.StatesStatus {
			if result.Status != state.SyncStateReady && result.ErrInfo != nil {
				errorInfo = result.ErrInfo
				condErr = r.conditionUpdater.SetConditionsError(ctx, instance, conditions.ReconcileFailed, fmt.Sprintf("Error syncing state %s: %v", result.StateName, errorInfo.Error()))
				if condErr != nil {
					logger.V(consts.LogLevelDebug).Error(nil, condErr.Error())
				}
				break
			}
		}
		// if no errors are reported from any state, then we would be waiting on driver daemonset pods
		if errorInfo == nil {
			condErr = r.conditionUpdater.SetConditionsError(ctx, instance, conditions.DriverNotReady, "Waiting for driver pod to be ready")
			if condErr != nil {
				logger.V(consts.LogLevelDebug).Error(nil, condErr.Error())
			}
		}
		return reconcile.Result{RequeueAfter: time.Second * 5}, nil
	}

	if condErr = r.conditionUpdater.SetConditionsReady(ctx, instance, "Reconciled", "All resources have been successfully reconciled"); condErr != nil {
		return ctrl.Result{}, condErr
	}
	return reconcile.Result{}, nil
}

func (r *NVIDIADriverReconciler) updateCrStatus(
	ctx context.Context, cr *nvidiav1alpha1.NVIDIADriver, status state.Results) error {
	reqLogger := log.FromContext(ctx)

	// Fetch latest instance and update state to avoid version mismatch
	instance := &nvidiav1alpha1.NVIDIADriver{}
	err := r.Client.Get(ctx, types.NamespacedName{Name: cr.Name}, instance)
	if err != nil {
		reqLogger.Error(err, "Failed to get NVIDIADriver instance for status update")
		return err
	}

	// Update global State
	if instance.Status.State == nvidiav1alpha1.State(status.Status) {
		return nil
	}
	instance.Status.State = nvidiav1alpha1.State(status.Status)

	// send status update request to k8s API
	reqLogger.V(consts.LogLevelInfo).Info("Updating CR Status", "Status", instance.Status)
	err = r.Status().Update(ctx, instance)
	if err != nil {
		reqLogger.V(consts.LogLevelError).Error(err, "Failed to update CR status")
		return err
	}
	return nil
}

// SetupWithManager sets up the controller with the Manager.
func (r *NVIDIADriverReconciler) SetupWithManager(ctx context.Context, mgr ctrl.Manager) error {
	// Create state manager
	stateManager, err := state.NewManager(
		nvidiav1alpha1.NVIDIADriverCRDName,
		mgr.GetClient(),
		mgr.GetScheme())
	if err != nil {
		return fmt.Errorf("error creating state manager: %v", err)
	}
	r.stateManager = stateManager

	// initialize validators
	r.nodeSelectorValidator = validator.NewNodeSelectorValidator(r.Client)

	// initialize condition updater
	r.conditionUpdater = conditions.NewNvDriverUpdater(mgr.GetClient())

	// Create a new NVIDIADriver controller
	c, err := controller.New("nvidia-driver-controller", mgr, controller.Options{
		Reconciler:              r,
		MaxConcurrentReconciles: 1,
		RateLimiter:             workqueue.NewItemExponentialFailureRateLimiter(minDelayCR, maxDelayCR),
	})
	if err != nil {
		return err
	}

	// Watch for changes to the primary resource NVIDIaDriver
	err = c.Watch(source.Kind(mgr.GetCache(), &nvidiav1alpha1.NVIDIADriver{}), &handler.EnqueueRequestForObject{}, predicate.GenerationChangedPredicate{})
	if err != nil {
		return err
	}

	// Watch for changes to ClusterPolicy. Whenever an event is generated for ClusterPolicy, enqueue
	// a reconcile request for all NVIDIADriver instances.
	mapFn := func(ctx context.Context, a client.Object) []reconcile.Request {
		logger := log.FromContext(ctx)
		opts := []client.ListOption{}
		list := &nvidiav1alpha1.NVIDIADriverList{}

		err := mgr.GetClient().List(ctx, list, opts...)
		if err != nil {
			logger.Error(err, "Unable to list NVIDIADriver resources")
			return []reconcile.Request{}
		}

		reconcileRequests := []reconcile.Request{}
		for _, nvidiaDriver := range list.Items {
			reconcileRequests = append(reconcileRequests,
				reconcile.Request{
					NamespacedName: types.NamespacedName{
						Name:      nvidiaDriver.ObjectMeta.GetName(),
						Namespace: nvidiaDriver.ObjectMeta.GetNamespace(),
					},
				})
		}

		return reconcileRequests
	}

	err = c.Watch(
		source.Kind(mgr.GetCache(), &gpuv1.ClusterPolicy{}),
		handler.EnqueueRequestsFromMapFunc(mapFn),
		predicate.GenerationChangedPredicate{},
	)
	if err != nil {
		return err
	}

	nodePredicate := predicate.Funcs{
		CreateFunc: func(e event.CreateEvent) bool {
			labels := e.Object.GetLabels()
			return hasGPULabels(labels)
		},
		UpdateFunc: func(e event.UpdateEvent) bool {
			logger := log.FromContext(ctx)
			newLabels := e.ObjectNew.GetLabels()
			oldLabels := e.ObjectOld.GetLabels()
			nodeName := e.ObjectNew.GetName()

			needsUpdate := hasGPULabels(newLabels) && !maps.Equal(newLabels, oldLabels)

			if needsUpdate {
				logger.Info("Node labels have been changed",
					"name", nodeName,
				)
			}
			return needsUpdate
		},
		DeleteFunc: func(e event.DeleteEvent) bool {
			labels := e.Object.GetLabels()
			return hasGPULabels(labels)
		},
	}

	// Watch for changes to node labels
	err = c.Watch(
		source.Kind(mgr.GetCache(), &corev1.Node{}),
		handler.EnqueueRequestsFromMapFunc(mapFn),
		nodePredicate,
	)
	if err != nil {
		return err
	}

	// Watch for changes to secondary resources which each state manager manages
	watchSources := stateManager.GetWatchSources(mgr)
	nvDriverPredicate, err := predicate.LabelSelectorPredicate(metav1.LabelSelector{MatchLabels: map[string]string{AppComponentLabelKey: AppComponentLabelValue}})
	if err != nil {
		return fmt.Errorf("failed to create labelSelector predicate: %w", err)
	}
	for _, watchSource := range watchSources {
		err = c.Watch(
			watchSource,
			handler.EnqueueRequestForOwner(
				mgr.GetScheme(),
				mgr.GetRESTMapper(),
				&nvidiav1alpha1.NVIDIADriver{},
				handler.OnlyControllerOwner(),
			),
			nvDriverPredicate,
		)
		if err != nil {
			return fmt.Errorf("error setting up Watch for source type %v: %w", watchSource, err)
		}
	}

	// Add an index key which allows our reconciler to quickly look up DaemonSets owned by an NVIDIADriver instance
	if err := mgr.GetFieldIndexer().IndexField(ctx, &appsv1.DaemonSet{}, consts.NVIDIADriverControllerIndexKey, func(rawObj client.Object) []string {
		ds := rawObj.(*appsv1.DaemonSet)
		owner := metav1.GetControllerOf(ds)
		if owner == nil {
			return nil
		}
		if owner.APIVersion != nvidiav1alpha1.SchemeGroupVersion.String() || owner.Kind != nvidiav1alpha1.NVIDIADriverCRDName {
			return nil
		}
		return []string{owner.Name}
	}); err != nil {
		return fmt.Errorf("failed to add index key: %w", err)
	}

	return nil
}
