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
	"errors"
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
	Namespace   string

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
	if err := r.Get(ctx, req.NamespacedName, instance); err != nil {
		if apierrors.IsNotFound(err) {
			// Request object not found, could have been deleted after reconcile request.
			return reconcile.Result{}, nil
		}
		wrappedErr := fmt.Errorf("error getting NVIDIADriver object: %w", err)
		logger.Error(err, "error getting NVIDIADriver object")
		instance.Status.State = nvidiav1alpha1.NotReady
		if condErr := r.conditionUpdater.SetConditionsError(ctx, instance, conditions.ReconcileFailed, wrappedErr.Error()); condErr != nil {
			logger.Error(condErr, "failed to set condition")
		}
		// Error reading the object - requeue the request.
		return reconcile.Result{}, wrappedErr
	}
	if instance.HasDeletionTimestamp() {
		return reconcile.Result{}, nil
	}

	// Get the singleton NVIDIA ClusterPolicy object in the cluster.
	clusterPolicyList := &gpuv1.ClusterPolicyList{}
	if err := r.List(ctx, clusterPolicyList); err != nil {
		wrappedErr := fmt.Errorf("error getting ClusterPolicy list: %w", err)
		logger.Error(err, "error getting ClusterPolicy list")
		instance.Status.State = nvidiav1alpha1.NotReady
		if condErr := r.conditionUpdater.SetConditionsError(ctx, instance, conditions.ReconcileFailed, err.Error()); condErr != nil {
			logger.Error(condErr, "failed to set condition")
		}
		return reconcile.Result{}, wrappedErr
	}

	if len(clusterPolicyList.Items) == 0 {
		err := fmt.Errorf("no ClusterPolicy object found in the cluster")
		logger.Error(err, "failed to get ClusterPolicy object")
		instance.Status.State = nvidiav1alpha1.NotReady
		if condErr := r.conditionUpdater.SetConditionsError(ctx, instance, conditions.ReconcileFailed, err.Error()); condErr != nil {
			logger.Error(condErr, "failed to set condition")
		}
		return reconcile.Result{}, err
	}
	clusterPolicyInstance := clusterPolicyList.Items[0]

	// Ensure that ClusterPolicy is configured to use NVIDIADriver CRD
	if !clusterPolicyInstance.Spec.Driver.UseNvidiaDriverCRDType() {
		msg := "useNvidiaDriverCRD is not enabled in ClusterPolicy"
		logger.V(consts.LogLevelWarning).Info("NVIDIADriver reconciliation skipped", "reason", msg)
		instance.Status.State = nvidiav1alpha1.Disabled
		if condErr := r.conditionUpdater.SetConditionsError(ctx, instance, conditions.Reconciled, msg); condErr != nil {
			logger.Error(condErr, "failed to set condition")
		}
		return reconcile.Result{}, nil
	}

	// Create a new InfoCatalog which is a generic interface for passing information to state managers
	infoCatalog := state.NewInfoCatalog()

	// Add an entry for ClusterInfo, which was collected before the NVIDIADriver controller was started
	infoCatalog.Add(state.InfoTypeClusterInfo, r.ClusterInfo)

	// Add an entry for Clusterpolicy, which is needed to deploy the driver daemonset
	infoCatalog.Add(state.InfoTypeClusterPolicyCR, clusterPolicyInstance)

	// Verify the nodeSelector configured for this NVIDIADriver instance does
	// not conflict with any other instances. This ensures only one driver
	// is deployed per GPU node.
	if err := r.nodeSelectorValidator.Validate(ctx, instance); err != nil {
		logger.Error(err, "nodeSelector validation failed")
		instance.Status.State = nvidiav1alpha1.NotReady
		if condErr := r.conditionUpdater.SetConditionsError(ctx, instance, conditions.ConflictingNodeSelector, err.Error()); condErr != nil {
			logger.Error(condErr, "failed to set condition")
		}
		return reconcile.Result{}, nil
	}

	if instance.Spec.UsePrecompiledDrivers() && (instance.Spec.IsGDSEnabled() || instance.Spec.IsGDRCopyEnabled()) {
		err := errors.New("GPUDirect Storage driver (nvidia-fs) and/or GDRCopy driver is not supported along with pre-compiled NVIDIA drivers")
		logger.Error(err, "unsupported driver combination detected")
		instance.Status.State = nvidiav1alpha1.NotReady
		if condErr := r.conditionUpdater.SetConditionsError(ctx, instance, conditions.ReconcileFailed, err.Error()); condErr != nil {
			logger.Error(condErr, "failed to set condition")
		}
		return reconcile.Result{}, nil
	}

	if instance.Spec.IsGDSEnabled() && instance.Spec.IsOpenKernelModulesRequired() && !instance.Spec.IsOpenKernelModulesEnabled() {
		err := fmt.Errorf("GPUDirect Storage driver '%s' is only supported with NVIDIA OpenRM drivers. Please set 'useOpenKernelModules=true' to enable OpenRM mode", instance.Spec.GPUDirectStorage.Version)
		logger.Error(err, "unsupported driver combination detected")
		instance.Status.State = nvidiav1alpha1.NotReady
		if condErr := r.conditionUpdater.SetConditionsError(ctx, instance, conditions.ReconcileFailed, err.Error()); condErr != nil {
			logger.Error(condErr, "failed to set condition")
		}
		return reconcile.Result{}, nil
	}

	// ensure that the specified K8s secret actually exists in the operator namespace
	secretName := instance.Spec.SecretEnv
	if len(secretName) > 0 {
		key := client.ObjectKey{Namespace: r.Namespace, Name: secretName}
		if err := r.Get(ctx, key, &corev1.Secret{}); err != nil {
			logger.Error(err, "failed to get secret")
			if condErr := r.conditionUpdater.SetConditionsError(ctx, instance, conditions.ReconcileFailed, err.Error()); condErr != nil {
				logger.Error(condErr, "failed to set condition")
			}
			return reconcile.Result{}, nil
		}
	}

	// Sync state and update status
	managerStatus := r.stateManager.SyncState(ctx, instance, infoCatalog)

	// update CR status
	if err := r.updateCrStatus(ctx, instance, managerStatus); err != nil {
		return ctrl.Result{}, err
	}

	if managerStatus.Status != state.SyncStateReady {
		logger.Info("NVIDIADriver instance is not ready")
		var errorInfo error
		for _, result := range managerStatus.StatesStatus {
			if result.Status != state.SyncStateReady && result.ErrInfo != nil {
				errorInfo = result.ErrInfo
				if condErr := r.conditionUpdater.SetConditionsError(ctx, instance, conditions.ReconcileFailed, fmt.Sprintf("Error syncing state %s: %v", result.StateName, errorInfo.Error())); condErr != nil {
					logger.Error(condErr, "failed to set condition")
				}
				break
			}
		}
		// if no errors are reported from any state, then we would be waiting on driver daemonset pods
		if errorInfo == nil {
			if condErr := r.conditionUpdater.SetConditionsError(ctx, instance, conditions.DriverNotReady, "Waiting for driver pod to be ready"); condErr != nil {
				logger.Error(condErr, "failed to set condition")
			}
		}
		return reconcile.Result{RequeueAfter: time.Second * 5}, nil
	}

	if condErr := r.conditionUpdater.SetConditionsReady(ctx, instance, conditions.Reconciled, "All resources have been successfully reconciled"); condErr != nil {
		logger.Error(condErr, "failed to set condition")
		return ctrl.Result{}, condErr
	}
	return reconcile.Result{}, nil
}

func (r *NVIDIADriverReconciler) updateCrStatus(
	ctx context.Context, cr *nvidiav1alpha1.NVIDIADriver, status state.Results) error {
	reqLogger := log.FromContext(ctx)

	// Fetch latest instance and update state to avoid version mismatch
	instance := &nvidiav1alpha1.NVIDIADriver{}
	err := r.Get(ctx, types.NamespacedName{Name: cr.Name}, instance)
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
		reqLogger.Error(err, "Failed to update CR status")
		return err
	}
	return nil
}

// enqueueAllNVIDIADrivers lists all NVIDIADriver instances in the cluster and enqueues a reconcile
// request for each instance. This is used to trigger reconciliation for all NVIDIADriver instances
// when a relevant event occurs (e.g. ClusterPolicy/NVIDIADriver update, node label change, etc).
func (r *NVIDIADriverReconciler) enqueueAllNVIDIADrivers(ctx context.Context) []reconcile.Request {
	logger := log.FromContext(ctx)
	list := &nvidiav1alpha1.NVIDIADriverList{}

	err := r.List(ctx, list)
	if err != nil {
		logger.Error(err, "Unable to list NVIDIADriver resources")
		return []reconcile.Request{}
	}

	reconcileRequests := make([]reconcile.Request, 0, len(list.Items))
	for _, nvidiaDriver := range list.Items {
		reconcileRequests = append(reconcileRequests,
			reconcile.Request{
				NamespacedName: types.NamespacedName{
					Name:      nvidiaDriver.GetName(),
					Namespace: nvidiaDriver.GetNamespace(),
				},
			})
	}

	return reconcileRequests
}

// enqueueNVIDIADriverReconcilers enqueues the NVIDIADriver that triggered the
// event and all current NVIDIADriver instances. The triggering object is
// included even for delete events so the NotFound reconcile path can clear
// stale node owner labels.
func (r *NVIDIADriverReconciler) enqueueNVIDIADriverReconcilers(ctx context.Context, driver *nvidiav1alpha1.NVIDIADriver) []reconcile.Request {
	requests := r.enqueueAllNVIDIADrivers(ctx)
	if driver != nil {
		requests = append(requests, reconcile.Request{
			NamespacedName: types.NamespacedName{
				Name:      driver.GetName(),
				Namespace: driver.GetNamespace(),
			},
		})
	}
	return dedupeReconcileRequests(requests)
}

func dedupeReconcileRequests(requests []reconcile.Request) []reconcile.Request {
	seen := map[types.NamespacedName]struct{}{}
	deduped := make([]reconcile.Request, 0, len(requests))
	for _, request := range requests {
		if _, ok := seen[request.NamespacedName]; ok {
			continue
		}
		seen[request.NamespacedName] = struct{}{}
		deduped = append(deduped, request)
	}
	return deduped
}

// SetupWithManager sets up the controller with the Manager.
func (r *NVIDIADriverReconciler) SetupWithManager(ctx context.Context, mgr ctrl.Manager) error {
	// Create state manager
	stateManager, err := state.NewManager(
		nvidiav1alpha1.NVIDIADriverCRDName,
		r.Namespace,
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
		RateLimiter:             workqueue.NewTypedItemExponentialFailureRateLimiter[reconcile.Request](minDelayCR, maxDelayCR),
	})
	if err != nil {
		return err
	}

	// Watch for changes to NVIDIADriver CRs. Whenever an event is generated for a NVIDIADriver CR,
	// enqueue a reconcile request for all NVIDIADriver instances.
	nvidiaDriverMapFn := func(ctx context.Context, driver *nvidiav1alpha1.NVIDIADriver) []reconcile.Request {
		return r.enqueueNVIDIADriverReconcilers(ctx, driver)
	}

	// Watch for changes to the primary resource NVIDIADriver
	err = c.Watch(source.Kind(
		mgr.GetCache(),
		&nvidiav1alpha1.NVIDIADriver{},
		handler.TypedEnqueueRequestsFromMapFunc(nvidiaDriverMapFn),
		predicate.TypedGenerationChangedPredicate[*nvidiav1alpha1.NVIDIADriver]{},
	),
	)
	if err != nil {
		return err
	}

	// Watch for changes to ClusterPolicy. Whenever an event is generated for ClusterPolicy, enqueue
	// a reconcile request for all NVIDIADriver instances.
	mapFn := func(ctx context.Context, _ *gpuv1.ClusterPolicy) []reconcile.Request {
		return r.enqueueAllNVIDIADrivers(ctx)
	}

	// Watch for changes to the Nodes. Whenever an event is generated for a Node, enqueue
	// a reconcile request for all NVIDIADriver instances.
	nodeMapFn := func(ctx context.Context, _ *corev1.Node) []reconcile.Request {
		return r.enqueueAllNVIDIADrivers(ctx)
	}

	err = c.Watch(
		source.Kind(
			mgr.GetCache(),
			&gpuv1.ClusterPolicy{},
			handler.TypedEnqueueRequestsFromMapFunc(mapFn),
			predicate.TypedGenerationChangedPredicate[*gpuv1.ClusterPolicy]{},
		),
	)
	if err != nil {
		return err
	}

	nodePredicate := predicate.TypedFuncs[*corev1.Node]{
		CreateFunc: func(e event.TypedCreateEvent[*corev1.Node]) bool {
			labels := e.Object.GetLabels()
			return hasGPULabels(labels)
		},
		UpdateFunc: func(e event.TypedUpdateEvent[*corev1.Node]) bool {
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
		DeleteFunc: func(e event.TypedDeleteEvent[*corev1.Node]) bool {
			labels := e.Object.GetLabels()
			return hasGPULabels(labels)
		},
	}

	// Watch for changes to node labels
	err = c.Watch(
		source.Kind(mgr.GetCache(),
			&corev1.Node{},
			handler.TypedEnqueueRequestsFromMapFunc(nodeMapFn),
			nodePredicate,
		),
	)
	if err != nil {
		return err
	}

	// Watch for changes to secondary resources which each state manager manages
	watchSources := stateManager.GetWatchSources(mgr)
	for _, watchSource := range watchSources {
		err = c.Watch(
			watchSource,
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
