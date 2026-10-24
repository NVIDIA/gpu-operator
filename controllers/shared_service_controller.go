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
	"path/filepath"
	"slices"
	"time"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/utils/ptr"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
	"sigs.k8s.io/controller-runtime/pkg/source"

	gpuv1 "github.com/NVIDIA/gpu-operator/api/nvidia/v1"
	gpuv1alpha1 "github.com/NVIDIA/gpu-operator/api/nvidia/v1alpha1"
	"github.com/NVIDIA/gpu-operator/internal/consts"
	"github.com/NVIDIA/gpu-operator/internal/render"
)

const sharedServiceLabel = "nvidia.com/shared-service"

// SharedServiceReconciler keeps the standalone DCGM endpoint independent of the allocation stack.
// DCGM Exporter resources remain owned by each stack controller.
type SharedServiceReconciler struct {
	client.Client
	Scheme      *runtime.Scheme
	Namespace   string
	ManifestDir string
}

type sharedServiceData struct {
	Namespace                    string
	ServiceType                  corev1.ServiceType
	ServiceInternalTrafficPolicy corev1.ServiceInternalTrafficPolicy
}

type sharedServiceState struct {
	name   string
	owners []client.Object
	data   sharedServiceData
}

// Existing operator RBAC already grants these permissions.
//+kubebuilder:rbac:groups=nvidia.com,resources=clusterpolicies;gpuclusters,verbs=get;list;watch
//+kubebuilder:rbac:groups="",resources=services,verbs=get;list;watch;create;update;patch;delete

func (r *SharedServiceReconciler) Reconcile(ctx context.Context, _ ctrl.Request) (ctrl.Result, error) {
	// Resolve both CRs because they can be created or edited independently of Helm.
	cp, gc, err := resolveActiveConfig(ctx, r.Client)
	if err != nil {
		return ctrl.Result{}, err
	}

	dcgm := r.dcgmServiceState(cp, gc)
	dcgmErr := r.syncService(ctx, dcgm)
	if dcgmErr != nil {
		dcgmErr = fmt.Errorf("failed to reconcile shared Service %s: %w", dcgm.name, dcgmErr)
	}

	// Periodic recovery also covers ownership handoff and desired-state changes
	// between reading CRs and writing Services, which cannot be made atomic.
	return ctrl.Result{RequeueAfter: time.Minute}, dcgmErr
}

func (r *SharedServiceReconciler) dcgmServiceState(cp *gpuv1.ClusterPolicy, gc *gpuv1alpha1.GPUCluster) sharedServiceState {
	state := sharedServiceState{
		name: "nvidia-dcgm",
		data: sharedServiceData{
			Namespace:                    r.Namespace,
			ServiceType:                  corev1.ServiceTypeClusterIP,
			ServiceInternalTrafficPolicy: corev1.ServiceInternalTrafficPolicyLocal,
		},
	}
	if cp != nil && cp.DeletionTimestamp.IsZero() && cp.Spec.DCGM.IsEnabled() {
		state.owners = append(state.owners, cp)
	}
	// GPUCluster defaults standalone DCGM to disabled, unlike DCGMSpec.IsEnabled.
	if gc != nil && gc.DeletionTimestamp.IsZero() && gc.Spec.DCGM != nil && ptr.Deref(gc.Spec.DCGM.Enabled, false) {
		state.owners = append(state.owners, gc)
	}
	return state
}

func stackServiceOwner(ref metav1.OwnerReference) bool {
	return (ref.APIVersion == gpuv1.SchemeGroupVersion.String() && ref.Kind == "ClusterPolicy") ||
		(ref.APIVersion == gpuv1alpha1.SchemeGroupVersion.String() && ref.Kind == "GPUCluster")
}

func managedDCGMService(svc *corev1.Service) bool {
	if svc.Labels[sharedServiceLabel] == "true" {
		return true
	}
	return slices.ContainsFunc(svc.OwnerReferences, stackServiceOwner)
}

func (r *SharedServiceReconciler) deleteService(ctx context.Context, name string) error {
	svc := &corev1.Service{}
	if err := r.Get(ctx, client.ObjectKey{Namespace: r.Namespace, Name: name}, svc); err != nil {
		return client.IgnoreNotFound(err)
	}
	// Never remove a same-named Service not managed by GPU Operator.
	if !managedDCGMService(svc) {
		return nil
	}
	if !svc.DeletionTimestamp.IsZero() {
		return fmt.Errorf("Service %s is still terminating", name)
	}
	return client.IgnoreNotFound(r.Delete(ctx, svc, client.Preconditions{UID: &svc.UID, ResourceVersion: &svc.ResourceVersion}))
}

func (r *SharedServiceReconciler) syncService(ctx context.Context, state sharedServiceState) error {
	if len(state.owners) == 0 {
		return r.deleteService(ctx, state.name)
	}
	objects, err := render.NewRenderer([]string{filepath.Join(r.ManifestDir, "state-dcgm", "service.yaml")}).RenderObjects(&render.TemplatingData{Data: state.data})
	if err != nil {
		return err
	}
	if len(objects) != 1 || objects[0].GetKind() != "Service" {
		return fmt.Errorf("expected one Service in shared template")
	}
	desired := &corev1.Service{}
	if err := runtime.DefaultUnstructuredConverter.FromUnstructured(objects[0].Object, desired); err != nil {
		return err
	}
	svc := &corev1.Service{ObjectMeta: metav1.ObjectMeta{Name: state.name, Namespace: r.Namespace}}
	_, err = controllerutil.CreateOrUpdate(ctx, r.Client, svc, func() error {
		if svc.ResourceVersion != "" && !managedDCGMService(svc) {
			return fmt.Errorf("refusing to adopt unmanaged Service %s", svc.Name)
		}
		if !svc.DeletionTimestamp.IsZero() {
			return fmt.Errorf("Service %s is still terminating", svc.Name)
		}
		if svc.Labels == nil {
			svc.Labels = map[string]string{}
		}
		maps.Copy(svc.Labels, desired.Labels)
		svc.Labels[sharedServiceLabel] = "true"
		// Stack state cleanup must never include shared Services.
		delete(svc.Labels, consts.StateLabel)
		if svc.Annotations == nil {
			svc.Annotations = map[string]string{}
		}
		maps.Copy(svc.Annotations, desired.Annotations)
		refs := svc.OwnerReferences[:0]
		for _, ref := range svc.OwnerReferences {
			if !stackServiceOwner(ref) {
				if ptr.Deref(ref.Controller, false) {
					return fmt.Errorf("Service %s has another controller owner", svc.Name)
				}
				refs = append(refs, ref)
			}
		}
		svc.OwnerReferences = refs
		for _, owner := range state.owners {
			if err := controllerutil.SetOwnerReference(owner, svc, r.Scheme); err != nil {
				return err
			}
		}
		svc.Spec.Selector = desired.Spec.Selector
		svc.Spec.Type = desired.Spec.Type
		svc.Spec.InternalTrafficPolicy = desired.Spec.InternalTrafficPolicy
		ports := desired.Spec.Ports
		if desired.Spec.Type == corev1.ServiceTypeNodePort || desired.Spec.Type == corev1.ServiceTypeLoadBalancer {
			for i := range ports {
				for _, existing := range svc.Spec.Ports {
					if existing.Name == ports[i].Name && existing.Protocol == ports[i].Protocol {
						ports[i].NodePort = existing.NodePort
					}
				}
			}
		}
		svc.Spec.Ports = ports
		if desired.Spec.Type != corev1.ServiceTypeLoadBalancer {
			svc.Spec.HealthCheckNodePort = 0
			svc.Spec.AllocateLoadBalancerNodePorts = nil
		}
		if desired.Spec.Type == corev1.ServiceTypeClusterIP {
			svc.Spec.ExternalTrafficPolicy = ""
		}
		return nil
	})
	return err
}

func (r *SharedServiceReconciler) SetupWithManager(_ context.Context, mgr ctrl.Manager) error {
	if r.ManifestDir == "" {
		r.ManifestDir = "/opt/gpu-operator/manifests/shared"
	}
	c, err := controller.New("shared-service-controller", mgr, controller.Options{
		Reconciler:              r,
		MaxConcurrentReconciles: 1,
	})
	if err != nil {
		return err
	}
	requests := func() []reconcile.Request {
		return []reconcile.Request{
			{NamespacedName: types.NamespacedName{
				Namespace: r.Namespace,
				Name:      "shared-services",
			}},
		}
	}
	// Watch CR metadata updates too: deletionTimestamp can change without generation.
	if err := c.Watch(source.Kind(
		mgr.GetCache(),
		&gpuv1.ClusterPolicy{},
		handler.TypedEnqueueRequestsFromMapFunc(func(context.Context, *gpuv1.ClusterPolicy) []reconcile.Request {
			return requests()
		}),
	)); err != nil {
		return err
	}
	if err := c.Watch(source.Kind(
		mgr.GetCache(),
		&gpuv1alpha1.GPUCluster{},
		handler.TypedEnqueueRequestsFromMapFunc(func(context.Context, *gpuv1alpha1.GPUCluster) []reconcile.Request {
			return requests()
		}),
	)); err != nil {
		return err
	}
	return c.Watch(source.Kind(
		mgr.GetCache(),
		&corev1.Service{},
		handler.TypedEnqueueRequestsFromMapFunc(func(_ context.Context, svc *corev1.Service) []reconcile.Request {
			if svc.Namespace != r.Namespace {
				return nil
			}
			switch svc.Name {
			case "nvidia-dcgm":
				return requests()
			default:
				return nil
			}
		}),
	))
}
