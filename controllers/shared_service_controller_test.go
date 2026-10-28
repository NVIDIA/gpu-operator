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
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/intstr"
	"k8s.io/utils/ptr"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"

	gpuv1 "github.com/NVIDIA/gpu-operator/api/nvidia/v1"
	gpuv1alpha1 "github.com/NVIDIA/gpu-operator/api/nvidia/v1alpha1"
	"github.com/NVIDIA/gpu-operator/internal/consts"
)

func newSharedServiceTestReconciler(t *testing.T, objects ...client.Object) *SharedServiceReconciler {
	t.Helper()
	scheme := runtime.NewScheme()
	require.NoError(t, corev1.AddToScheme(scheme))
	require.NoError(t, gpuv1.AddToScheme(scheme))
	require.NoError(t, gpuv1alpha1.AddToScheme(scheme))
	return &SharedServiceReconciler{Client: fake.NewClientBuilder().WithScheme(scheme).WithObjects(objects...).Build(), Scheme: scheme, Namespace: "gpu-operator", ManifestDir: "../manifests/shared"}
}

func sharedServiceTestCRs() (*gpuv1.ClusterPolicy, *gpuv1alpha1.GPUCluster) {
	cp := &gpuv1.ClusterPolicy{ObjectMeta: metav1.ObjectMeta{Name: "cluster-policy", UID: "cp"}}
	cp.Spec.DCGM.Enabled = new(true)
	gc := &gpuv1alpha1.GPUCluster{ObjectMeta: metav1.ObjectMeta{Name: "gpu-cluster", UID: "gc"}}
	gc.Spec.DCGM = &gpuv1.DCGMSpec{Enabled: new(true)}
	gc.Spec.DCGMExporter = &gpuv1.DCGMExporterSpec{}
	return cp, gc
}

func TestSharedServiceEnablement(t *testing.T) {
	cases := map[string]struct {
		cp, gc bool
		dcgm   bool
	}{
		"ClusterPolicy only": {cp: true, dcgm: true},
		"GPUCluster only":    {gc: true, dcgm: true},
		"both stacks":        {cp: true, gc: true, dcgm: true},
		"no stacks":          {},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			cp, gc := sharedServiceTestCRs()
			var objects []client.Object
			if tc.cp {
				objects = append(objects, cp)
			}
			if tc.gc {
				objects = append(objects, gc)
			}
			r := newSharedServiceTestReconciler(t, objects...)
			_, err := r.Reconcile(context.Background(), ctrl.Request{})
			require.NoError(t, err)
			for component, enabled := range map[string]bool{"nvidia-dcgm": tc.dcgm, "nvidia-dcgm-exporter": false} {
				svc := &corev1.Service{}
				err := r.Get(context.Background(), client.ObjectKey{Name: component, Namespace: r.Namespace}, svc)
				if !enabled {
					assert.True(t, apierrors.IsNotFound(err))
					continue
				}
				require.NoError(t, err)
				assert.Equal(t, map[string]string{AppComponentLabelKey: component}, svc.Spec.Selector)
				count := 0
				if tc.cp {
					count++
				}
				if tc.gc {
					count++
				}
				require.Len(t, svc.OwnerReferences, count)
				for _, owner := range svc.OwnerReferences {
					assert.False(t, ptr.Deref(owner.Controller, false))
				}
				if component == "nvidia-dcgm" {
					assert.Equal(t, corev1.ServiceTypeClusterIP, svc.Spec.Type)
					assert.Equal(t, corev1.ServiceInternalTrafficPolicyLocal, *svc.Spec.InternalTrafficPolicy)
				}
			}
		})
	}
}

func TestSharedServiceDisableAndRecover(t *testing.T) {
	ctx := context.Background()
	cp, gc := sharedServiceTestCRs()
	r := newSharedServiceTestReconciler(t, cp, gc)
	_, err := r.Reconcile(ctx, ctrl.Request{})
	require.NoError(t, err)
	require.NoError(t, r.Get(ctx, client.ObjectKeyFromObject(cp), cp))
	cp.Spec.DCGM.Enabled = new(false)
	cp.Spec.DCGMExporter.Enabled = new(false)
	require.NoError(t, r.Update(ctx, cp))
	_, err = r.Reconcile(ctx, ctrl.Request{})
	require.NoError(t, err)
	svc := &corev1.Service{}
	key := client.ObjectKey{Namespace: r.Namespace, Name: "nvidia-dcgm"}
	require.NoError(t, r.Get(ctx, key, svc))
	require.Len(t, svc.OwnerReferences, 1)
	assert.Equal(t, gc.UID, svc.OwnerReferences[0].UID)
	require.NoError(t, r.Delete(ctx, svc))
	_, err = r.Reconcile(ctx, ctrl.Request{})
	require.NoError(t, err)
	require.NoError(t, r.Get(ctx, key, svc))
	require.NoError(t, r.Get(ctx, client.ObjectKeyFromObject(gc), gc))
	gc.Spec.DCGM.Enabled = new(false)
	gc.Spec.DCGMExporter.Enabled = new(false)
	require.NoError(t, r.Update(ctx, gc))
	_, err = r.Reconcile(ctx, ctrl.Request{})
	require.NoError(t, err)
	assert.True(t, apierrors.IsNotFound(r.Get(ctx, key, &corev1.Service{})))
	key.Name = "nvidia-dcgm-exporter"
	assert.True(t, apierrors.IsNotFound(r.Get(ctx, key, &corev1.Service{})))
}

func TestSharedServiceDefaults(t *testing.T) {
	_, gc := sharedServiceTestCRs()
	gc.Spec.DCGM = &gpuv1.DCGMSpec{}
	gc.Spec.DCGMExporter = nil
	r := newSharedServiceTestReconciler(t, gc)
	_, err := r.Reconcile(context.Background(), ctrl.Request{})
	require.NoError(t, err)
	list := &corev1.ServiceList{}
	require.NoError(t, r.List(context.Background(), list))
	assert.Empty(t, list.Items)

}

func TestSharedServicePreservesAssignedFields(t *testing.T) {
	for _, serviceType := range []corev1.ServiceType{corev1.ServiceTypeClusterIP, corev1.ServiceTypeNodePort, corev1.ServiceTypeLoadBalancer} {
		t.Run(string(serviceType), func(t *testing.T) {
			ctx := context.Background()
			cp, _ := sharedServiceTestCRs()
			r := newSharedServiceTestReconciler(t, cp)
			old := &corev1.Service{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "nvidia-dcgm",
					Namespace: r.Namespace,
					Labels:    map[string]string{consts.StateLabel: "state-dcgm", "custom": "value"},
				},
				Spec: corev1.ServiceSpec{
					ClusterIP:      "10.0.0.5",
					ClusterIPs:     []string{"10.0.0.5"},
					IPFamilies:     []corev1.IPFamily{corev1.IPv4Protocol},
					IPFamilyPolicy: new(corev1.IPFamilyPolicySingleStack),
					Type:           serviceType,
					Ports:          []corev1.ServicePort{{Name: "dcgm", Port: 5555, TargetPort: intstr.FromInt32(5555), Protocol: corev1.ProtocolTCP}},
				},
			}
			if serviceType != corev1.ServiceTypeClusterIP {
				old.Spec.Ports[0].NodePort = 30555
				old.Spec.ExternalTrafficPolicy = corev1.ServiceExternalTrafficPolicyLocal
			}
			if serviceType == corev1.ServiceTypeLoadBalancer {
				old.Spec.HealthCheckNodePort = 30556
				old.Spec.AllocateLoadBalancerNodePorts = new(true)
			}
			require.NoError(t, controllerutil.SetControllerReference(cp, old, r.Scheme))
			require.NoError(t, r.Create(ctx, old))
			_, err := r.Reconcile(ctx, ctrl.Request{})
			require.NoError(t, err)
			svc := &corev1.Service{}
			require.NoError(t, r.Get(ctx, client.ObjectKeyFromObject(old), svc))
			assert.Equal(t, old.Spec.ClusterIP, svc.Spec.ClusterIP)
			assert.Equal(t, old.Spec.ClusterIPs, svc.Spec.ClusterIPs)
			assert.Equal(t, old.Spec.IPFamilies, svc.Spec.IPFamilies)
			assert.Equal(t, old.Spec.IPFamilyPolicy, svc.Spec.IPFamilyPolicy)
			assert.Equal(t, corev1.ServiceTypeClusterIP, svc.Spec.Type)
			assert.Equal(t, []corev1.ServicePort{{Name: "dcgm", Port: 5555, TargetPort: intstr.FromInt32(5555), Protocol: corev1.ProtocolTCP}}, svc.Spec.Ports)
			assert.Zero(t, svc.Spec.HealthCheckNodePort)
			assert.Nil(t, svc.Spec.AllocateLoadBalancerNodePorts)
			assert.Empty(t, svc.Spec.ExternalTrafficPolicy)
			assert.Equal(t, "value", svc.Labels["custom"])
			assert.NotContains(t, svc.Labels, consts.StateLabel)
			assert.False(t, ptr.Deref(svc.OwnerReferences[0].Controller, false))
			// A steady-state reconcile should not write the Service again.
			rv := svc.ResourceVersion
			_, err = r.Reconcile(ctx, ctrl.Request{})
			require.NoError(t, err)
			require.NoError(t, r.Get(ctx, client.ObjectKeyFromObject(old), svc))
			assert.Equal(t, rv, svc.ResourceVersion)
		})
	}
}

func TestSharedServiceRejectsUnmanagedService(t *testing.T) {
	cp, _ := sharedServiceTestCRs()
	svc := &corev1.Service{ObjectMeta: metav1.ObjectMeta{Name: "nvidia-dcgm", Namespace: "gpu-operator", UID: types.UID("user-service")}}
	r := newSharedServiceTestReconciler(t, cp, svc)
	_, err := r.Reconcile(context.Background(), ctrl.Request{})
	require.ErrorContains(t, err, "unmanaged")
}

func TestSharedServiceLeavesStackExportersUntouched(t *testing.T) {
	for _, enabled := range []bool{true, false} {
		t.Run(fmt.Sprint(enabled), func(t *testing.T) {
			ctx := context.Background()
			cp, gc := sharedServiceTestCRs()
			cp.Spec.DCGMExporter.Enabled = new(enabled)
			gc.Spec.DCGMExporter.Enabled = new(enabled)
			gc.Spec.DCGMExporter.ServiceSpec = &gpuv1.DCGMExporterServiceConfig{Type: corev1.ServiceTypeNodePort}
			r := newSharedServiceTestReconciler(t, cp, gc)
			for name, owner := range map[string]client.Object{"nvidia-dcgm-exporter": cp, "nvidia-dcgm-exporter-dra": gc} {
				svc := &corev1.Service{ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: r.Namespace}}
				require.NoError(t, controllerutil.SetControllerReference(owner, svc, r.Scheme))
				require.NoError(t, r.Create(ctx, svc))
				before := svc.DeepCopy()
				_, err := r.Reconcile(ctx, ctrl.Request{})
				require.NoError(t, err)
				require.NoError(t, r.Get(ctx, client.ObjectKeyFromObject(svc), svc))
				assert.Equal(t, before, svc)
			}
		})
	}
}
