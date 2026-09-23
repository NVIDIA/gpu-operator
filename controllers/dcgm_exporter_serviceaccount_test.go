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
	"testing"
	"time"

	"github.com/go-logr/logr"
	"github.com/stretchr/testify/require"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/client/interceptor"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"

	gpuv1 "github.com/NVIDIA/gpu-operator/api/nvidia/v1"
)

func TestDCGMExporterManagedAccountValidation(t *testing.T) {
	for name, tc := range map[string]struct{ replacement, changed, patchDenied bool }{
		"current managed account is validated without replacing metadata": {},
		"stale managed cache must not authorize a replacement":            {replacement: true},
		"same UID with changed ownership is rejected":                     {changed: true},
		"validation write failure stops later controls":                   {patchDenied: true},
	} {
		t.Run(name, func(t *testing.T) {
			ctx := t.Context()
			scheme := runtime.NewScheme()
			require.NoError(t, corev1.AddToScheme(scheme))
			require.NoError(t, appsv1.AddToScheme(scheme))
			require.NoError(t, gpuv1.AddToScheme(scheme))
			cp := &gpuv1.ClusterPolicy{ObjectMeta: metav1.ObjectMeta{Name: "policy", UID: "policy-uid"}}
			cp.Spec.DCGMExporter.ServiceAccount = &gpuv1.DCGMExporterServiceAccountConfig{Name: "metrics"}
			cached := &corev1.ServiceAccount{ObjectMeta: metav1.ObjectMeta{Name: "metrics", Namespace: "operator", UID: "old-uid", ResourceVersion: "10", Annotations: map[string]string{"example.com/identity": "keep"}}, ImagePullSecrets: []corev1.LocalObjectReference{{Name: "pull-secret"}}}
			dcgmExporterServiceAccountMarker.Apply(cached)
			require.NoError(t, controllerutil.SetControllerReference(cp, cached, scheme))
			live := cached.DeepCopy()
			if tc.replacement || tc.changed {
				live.ResourceVersion = "20"
				live.OwnerReferences = nil
				if tc.replacement {
					live.UID = "replacement-uid"
				}
			}
			base := fake.NewClientBuilder().WithScheme(scheme).WithObjects(live).Build()
			creates := 0
			c := interceptor.NewClient(base, interceptor.Funcs{
				Get: func(ctx context.Context, c client.WithWatch, key client.ObjectKey, obj client.Object, opts ...client.GetOption) error {
					if sa, ok := obj.(*corev1.ServiceAccount); ok && key == client.ObjectKeyFromObject(cached) {
						*sa = *cached.DeepCopy()
						return nil
					}
					return c.Get(ctx, key, obj, opts...)
				},
				Create: func(ctx context.Context, c client.WithWatch, obj client.Object, opts ...client.CreateOption) error {
					creates++
					return c.Create(ctx, obj, opts...)
				},
				Patch: func(ctx context.Context, c client.WithWatch, obj client.Object, patch client.Patch, opts ...client.PatchOption) error {
					if tc.patchDenied {
						return apierrors.NewForbidden(corev1.Resource("serviceaccounts"), obj.GetName(), nil)
					}
					return c.Patch(ctx, obj, patch, opts...)
				},
			})
			consumers := 0
			n := ClusterPolicyController{client: c, ctx: ctx, singleton: cp, scheme: scheme, operatorNamespace: "operator", logger: logr.Discard(),
				stateNames: []string{"state-dcgm-exporter"}, resources: []Resources{{ServiceAccount: corev1.ServiceAccount{}}},
				controls: []controlFunc{{ServiceAccount, func(ClusterPolicyController) (gpuv1.State, error) { consumers++; return gpuv1.Ready, nil }}},
			}
			state, err := n.step()
			if tc.replacement || tc.changed || tc.patchDenied {
				require.Error(t, err)
				require.Equal(t, gpuv1.NotReady, state)
				require.Zero(t, consumers, "unvalidated identities must not reach RBAC or DaemonSet controls")
				if !tc.patchDenied {
					require.True(t, apierrors.IsConflict(err))
				}
			} else {
				require.NoError(t, err)
				require.Equal(t, gpuv1.Ready, state)
				require.Equal(t, 1, consumers)
			}
			require.Zero(t, creates, "an existing account must be validated with its resource version")
			found := &corev1.ServiceAccount{}
			require.NoError(t, base.Get(ctx, client.ObjectKeyFromObject(live), found))
			require.Equal(t, live.UID, found.UID)
			require.Equal(t, live.OwnerReferences, found.OwnerReferences)
			require.Equal(t, live.Labels, found.Labels)
			require.Equal(t, live.Annotations, found.Annotations)
			require.Equal(t, live.ImagePullSecrets, found.ImagePullSecrets)
		})
	}
}

func TestClusterPolicyRechecksReadyExporterServiceAccount(t *testing.T) {
	cp := clusterPolicyForUpgradeTest(false)
	cp.Spec.DCGMExporter.ServiceAccount = &gpuv1.DCGMExporterServiceAccountConfig{Name: "byo", Create: new(false)}
	node := nodeWithLabels("nfd-node", map[string]string{"feature.node.kubernetes.io/test": "true"})
	r, c, _ := newClusterPolicyUpgradeTestReconciler(t, cp, node)
	require.NoError(t, appsv1.AddToScheme(r.Scheme))
	clusterPolicyCtrl.operatorNamespace = "operator"
	clusterPolicyCtrl.stateNames = []string{"state-dcgm-exporter"}
	clusterPolicyCtrl.resources = []Resources{{ServiceAccount: corev1.ServiceAccount{}}}
	clusterPolicyCtrl.controls = []controlFunc{{ServiceAccount}}
	sa := &corev1.ServiceAccount{ObjectMeta: metav1.ObjectMeta{Name: "byo", Namespace: "operator"}}
	require.NoError(t, c.Create(t.Context(), sa))
	req := ctrl.Request{NamespacedName: client.ObjectKeyFromObject(cp)}
	result, err := r.Reconcile(t.Context(), req)
	require.NoError(t, err)
	require.Equal(t, gpuv1.Ready, clusterPolicyState(t, c, cp.Name))
	require.Equal(t, time.Minute, result.RequeueAfter, "Ready must schedule a follow-up even without a ServiceAccount watch")
	require.NoError(t, c.Delete(t.Context(), sa))
	_, err = r.Reconcile(t.Context(), req)
	require.True(t, apierrors.IsNotFound(err))
	require.Equal(t, gpuv1.NotReady, clusterPolicyState(t, c, cp.Name))
	restored := &corev1.ServiceAccount{ObjectMeta: metav1.ObjectMeta{Name: "byo", Namespace: "operator"}}
	require.NoError(t, c.Create(t.Context(), restored))
	result, err = r.Reconcile(t.Context(), req)
	require.NoError(t, err)
	require.Equal(t, gpuv1.Ready, clusterPolicyState(t, c, cp.Name))
	require.Equal(t, time.Minute, result.RequeueAfter)
	require.NoError(t, c.Get(t.Context(), client.ObjectKeyFromObject(restored), restored))
	require.Empty(t, restored.OwnerReferences)
}

// A benign metadata conflict must retry cleanup rather than leave an obsolete
// managed identity behind after the state reports Ready.
func TestDCGMExporterCleanupRetriesMetadataConflict(t *testing.T) {
	ctx := t.Context()
	scheme := runtime.NewScheme()
	require.NoError(t, corev1.AddToScheme(scheme))
	require.NoError(t, appsv1.AddToScheme(scheme))
	require.NoError(t, gpuv1.AddToScheme(scheme))
	cp := &gpuv1.ClusterPolicy{ObjectMeta: metav1.ObjectMeta{Name: "policy", UID: "policy-uid"}}
	old := &corev1.ServiceAccount{ObjectMeta: metav1.ObjectMeta{Name: "previous", Namespace: "operator", UID: "old-uid"}}
	dcgmExporterServiceAccountMarker.Apply(old)
	require.NoError(t, controllerutil.SetControllerReference(cp, old, scheme))
	base := fake.NewClientBuilder().WithScheme(scheme).WithObjects(old).Build()
	deletes := 0
	c := interceptor.NewClient(base, interceptor.Funcs{Delete: func(ctx context.Context, c client.WithWatch, obj client.Object, opts ...client.DeleteOption) error {
		deletes++
		if deletes == 1 {
			current := &corev1.ServiceAccount{}
			require.NoError(t, c.Get(ctx, client.ObjectKeyFromObject(old), current))
			current.Annotations = map[string]string{"example.com/last-audit": "updated"}
			require.NoError(t, c.Update(ctx, current))
		}
		return c.Delete(ctx, obj, opts...)
	}})
	n := ClusterPolicyController{client: c, ctx: ctx, singleton: cp, scheme: scheme, operatorNamespace: "operator", logger: logr.Discard(), stateNames: []string{"state-dcgm-exporter"}, controls: []controlFunc{{func(ClusterPolicyController) (gpuv1.State, error) { return gpuv1.Ready, nil }}}}
	state, err := n.step()
	require.True(t, apierrors.IsConflict(err))
	require.Equal(t, gpuv1.NotReady, state)
	require.Zero(t, n.idx, "a failed cleanup must not advance the state")
	require.NoError(t, base.Get(ctx, client.ObjectKeyFromObject(old), &corev1.ServiceAccount{}))
	state, err = n.step()
	require.NoError(t, err)
	require.Equal(t, gpuv1.Ready, state)
	require.Equal(t, 2, deletes)
	require.True(t, apierrors.IsNotFound(base.Get(ctx, client.ObjectKeyFromObject(old), &corev1.ServiceAccount{})))
}
