package clusterpolicy

import (
	"context"
	"time"

	gpuv1 "github.com/NVIDIA/gpu-operator/pkg/apis/nvidia/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller"
	"sigs.k8s.io/controller-runtime/pkg/event"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/manager"
	"sigs.k8s.io/controller-runtime/pkg/predicate"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
	"sigs.k8s.io/controller-runtime/pkg/source"
)

var log = logf.Log.WithName("controller_clusterpolicy")

/**
* USER ACTION REQUIRED: This is a scaffold file intended for the user to modify with their own Controller
* business logic.  Delete these comments after modifying this file.*
 */

// Add creates a new ClusterPolicy Controller and adds it to the Manager. The Manager will set fields on the Controller
// and Start it when the Manager is Started.
func Add(mgr manager.Manager) error {
	return add(mgr, newReconciler(mgr))
}

// newReconciler returns a new reconcile.Reconciler
func newReconciler(mgr manager.Manager) reconcile.Reconciler {
	return &ReconcileClusterPolicy{client: mgr.GetClient(), scheme: mgr.GetScheme()}
}

// add adds a new Controller to mgr with r as the reconcile.Reconciler
func add(mgr manager.Manager, r reconcile.Reconciler) error {
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
	err = addWatchNewGPUNode(c, mgr, r)
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

func addWatchNewGPUNode(c controller.Controller, mgr manager.Manager, r reconcile.Reconciler) error {
	// https://book-v1.book.kubebuilder.io/beyond_basics/controller_watches.html

	// 'UpdateFunc' and 'CreateFunc' used to judge if a event about the object is
	// what we want. If that is true, the event will be processed by the reconciler.
	p := predicate.Funcs{
		CreateFunc: func(e event.CreateEvent) bool {
			labels := e.Meta.GetLabels()

			gpuCommonLabelMissing := hasGPULabels(labels) && !hasCommonGPULabel(labels)
			if gpuCommonLabelMissing {
				log.Info("New node needs an update, GPU common label missing.",
					"name", e.Meta.GetName())
			}

			return gpuCommonLabelMissing
		},

		UpdateFunc: func(e event.UpdateEvent) bool {
			newLabels := e.MetaNew.GetLabels()

			gpuCommonLabelMissing := hasGPULabels(newLabels) && !hasCommonGPULabel(newLabels)
			gpuCommonLabelOutdated := !hasGPULabels(newLabels) && hasCommonGPULabel(newLabels)
			needsUpdate := gpuCommonLabelMissing || gpuCommonLabelOutdated
			if needsUpdate {
				log.Info("Node needs an update",
					"name", e.MetaNew.GetName(),
					"gpuCommonLabelMissing", gpuCommonLabelMissing,
					"gpuCommonLabelOutdated", gpuCommonLabelOutdated)
			}

			return needsUpdate
		},
	}

	// Define a mapping from the Node object in the event to one or more
	// ClusterPolicy objects to Reconcile
	mapFn := handler.ToRequestsFunc(
		func(a handler.MapObject) []reconcile.Request {
			// find all the ClusterPolicy to trigger their reconciliation
			opts := []client.ListOption{} // Namespace = "" to list across all namespaces.
			list := &gpuv1.ClusterPolicyList{}

			err := mgr.GetClient().List(context.TODO(), list, opts...)
			if err != nil {
				log.Error(err, "Unable to list ClusterPolicies")
				return []reconcile.Request{}
			}

			cp_to_rec := []reconcile.Request{}

			for _, cp := range list.Items {
				cp_to_rec = append(cp_to_rec, reconcile.Request{NamespacedName: types.NamespacedName{
					Name:      cp.ObjectMeta.GetName(),
					Namespace: cp.ObjectMeta.GetNamespace(),
				}})
			}
			log.Info("Reconciliate ClusterPolicies after node label update", "nb", len(cp_to_rec))

			return cp_to_rec
		})

	err := c.Watch(&source.Kind{Type: &corev1.Node{}}, &handler.EnqueueRequestsFromMapFunc{
		ToRequests: mapFn,
	}, p)

	return err
}

// blank assignment to verify that ReconcileClusterPolicy implements reconcile.Reconciler
var _ reconcile.Reconciler = &ReconcileClusterPolicy{}
var ctrl ClusterPolicyController

// ReconcileClusterPolicy reconciles a ClusterPolicy object
type ReconcileClusterPolicy struct {
	// This client, initialized using mgr.Client() above, is a split client
	// that reads objects from the cache and writes to the apiserver
	client client.Client
	scheme *runtime.Scheme
}

// Reconcile reads that state of the cluster for a ClusterPolicy object and makes changes based on the state read
// and what is in the ClusterPolicy.Spec
// Note:
// The Controller will requeue the Request to be processed again if the returned error is non-nil or
// Result.Requeue is true, otherwise upon completion it will remove the work from the queue.
func (r *ReconcileClusterPolicy) Reconcile(request reconcile.Request) (reconcile.Result, error) {
	ctx := log.WithValues("Request.Name", request.Name)
	ctx.Info("Reconciling ClusterPolicy")

	// Fetch the ClusterPolicy instance
	instance := &gpuv1.ClusterPolicy{}
	err := r.client.Get(context.TODO(), request.NamespacedName, instance)
	if err != nil {
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
	if ctrl.singleton != nil && ctrl.singleton.ObjectMeta.Name != instance.ObjectMeta.Name {
		instance.SetState(gpuv1.Ignored)
		return reconcile.Result{}, err
	}

	err = ctrl.init(r, instance)
	if err != nil {
		log.Error(err, "Failed to initialize ClusterPolicy controller")
		return reconcile.Result{}, err
	}

	for {
		status, statusError := ctrl.step()
		// Update the CR status
		instance = &gpuv1.ClusterPolicy{}
		err := r.client.Get(context.TODO(), request.NamespacedName, instance)
		if err != nil {
			log.Error(err, "Failed to get ClusterPolicy instance for status update")
			return reconcile.Result{RequeueAfter: time.Second * 5}, err
		}
		if instance.Status.State != status {
			instance.Status.State = status
			err = r.client.Status().Update(context.TODO(), instance)
			if err != nil {
				log.Error(err, "Failed to update ClusterPolicy status")
				return reconcile.Result{RequeueAfter: time.Second * 5}, err
			}
		}
		if statusError != nil {
			return reconcile.Result{RequeueAfter: time.Second * 5}, statusError
		}

		if status == gpuv1.NotReady {
			// If the resource is not ready, wait 5 secs and reconcile
			log.Info("ClusterPolicy step wasn't ready", "State:", status)
			return reconcile.Result{RequeueAfter: time.Second * 5}, nil
		}

		if ctrl.last() {
			break
		}
	}

	instance.SetState(gpuv1.Ready)
	return reconcile.Result{}, nil
}
