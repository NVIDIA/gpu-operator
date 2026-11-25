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
	"time"

	"github.com/go-logr/logr"
	"github.com/stretchr/testify/require"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/client/interceptor"

	gpuv1 "github.com/NVIDIA/gpu-operator/api/nvidia/v1"
)

func newExporterAccountController(t *testing.T) ClusterPolicyController {
	t.Helper()
	scheme := runtime.NewScheme()
	require.NoError(t, corev1.AddToScheme(scheme))
	require.NoError(t, appsv1.AddToScheme(scheme))
	require.NoError(t, gpuv1.AddToScheme(scheme))
	return ClusterPolicyController{
		client: fake.NewClientBuilder().WithScheme(scheme).Build(), ctx: t.Context(),
		singleton: &gpuv1.ClusterPolicy{ObjectMeta: metav1.ObjectMeta{Name: "policy", UID: "policy-uid"}},
		scheme:    scheme, operatorNamespace: "operator", logger: logr.Discard(),
		stateNames: []string{"state-dcgm-exporter"},
		resources:  []Resources{{ServiceAccount: corev1.ServiceAccount{ObjectMeta: metav1.ObjectMeta{Name: gpuv1.DCGMExporterDefaultServiceAccountName}}}},
		controls:   []controlFunc{{ServiceAccount}},
	}
}

func TestDCGMExporterServiceAccountReference(t *testing.T) {
	for name, tc := range map[string]struct {
		configured, namespace            string
		missing, terminating, emptyBlock bool
		err                              string
	}{
		"unset creates default":                     {},
		"empty block creates default":               {emptyBlock: true},
		"external account is read only":             {configured: "metrics", namespace: "operator"},
		"missing account blocks consumers":          {configured: "metrics", missing: true, err: "cannot use"},
		"account in another namespace is missing":   {configured: "metrics", namespace: "another", err: "cannot use"},
		"reserved name is rejected even if present": {configured: gpuv1.DCGMExporterDefaultServiceAccountName, namespace: "operator", err: "reserved"},
		"reserved name is never created explicitly": {configured: gpuv1.DCGMExporterDefaultServiceAccountName, missing: true, err: "reserved"},
		"terminating account is not ready":          {configured: "metrics", namespace: "operator", terminating: true, err: "being deleted"},
	} {
		t.Run(name, func(t *testing.T) {
			n := newExporterAccountController(t)
			if tc.configured != "" || tc.emptyBlock {
				n.singleton.Spec.DCGMExporter.ServiceAccount = &gpuv1.DCGMExporterServiceAccountConfig{Name: tc.configured}
			}
			var before *corev1.ServiceAccount
			if tc.configured != "" && !tc.missing {
				sa := &corev1.ServiceAccount{ObjectMeta: metav1.ObjectMeta{
					Name: tc.configured, Namespace: tc.namespace,
					Labels:          map[string]string{"app": "nvidia-dcgm-exporter"},
					Annotations:     map[string]string{"example.com/identity": "keep"},
					OwnerReferences: []metav1.OwnerReference{{APIVersion: "v1", Kind: "ConfigMap", Name: "external-owner", UID: "external-owner"}},
				}, ImagePullSecrets: []corev1.LocalObjectReference{{Name: "pull-secret"}}}
				require.NoError(t, n.client.Create(t.Context(), sa))
				if tc.terminating {
					sa.Finalizers = []string{"example.com/keep"}
					require.NoError(t, n.client.Update(t.Context(), sa))
					require.NoError(t, n.client.Delete(t.Context(), sa))
				}
				before = &corev1.ServiceAccount{}
				require.NoError(t, n.client.Get(t.Context(), client.ObjectKeyFromObject(sa), before))
			}
			consumers := 0
			n.controls[0] = append(n.controls[0], func(ClusterPolicyController) (gpuv1.State, error) { consumers++; return gpuv1.Ready, nil })
			state, err := n.step()
			if tc.err != "" {
				require.ErrorContains(t, err, tc.err)
				require.Equal(t, gpuv1.NotReady, state)
				require.Zero(t, consumers)
			} else {
				require.NoError(t, err)
				require.Equal(t, gpuv1.Ready, state)
				require.Equal(t, 1, consumers)
			}
			if before != nil {
				after := &corev1.ServiceAccount{}
				require.NoError(t, n.client.Get(t.Context(), client.ObjectKeyFromObject(before), after))
				require.Equal(t, before, after)
			}
			accounts := &corev1.ServiceAccountList{}
			require.NoError(t, n.client.List(t.Context(), accounts))
			switch {
			case tc.configured == "":
				require.Len(t, accounts.Items, 1)
				require.Equal(t, gpuv1.DCGMExporterDefaultServiceAccountName, accounts.Items[0].Name)
				require.True(t, metav1.IsControlledBy(&accounts.Items[0], n.singleton))
			case tc.missing:
				require.Empty(t, accounts.Items)
			default:
				require.Len(t, accounts.Items, 1, "a custom reference must not create an account")
			}
		})
	}
}

func TestDCGMExporterServiceAccountTransitions(t *testing.T) {
	n := newExporterAccountController(t)
	ctx := t.Context()
	for _, name := range []string{"metrics-a", "metrics-b"} {
		require.NoError(t, n.client.Create(ctx, &corev1.ServiceAccount{ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: "operator", Labels: map[string]string{"app": "nvidia-dcgm-exporter"}}}))
	}
	for _, name := range []string{"", "metrics-a", "metrics-b", ""} {
		n.idx = 0
		n.singleton.Spec.DCGMExporter.ServiceAccount = &gpuv1.DCGMExporterServiceAccountConfig{Name: name}
		_, err := n.step()
		require.NoError(t, err)
		accounts := &corev1.ServiceAccountList{}
		require.NoError(t, n.client.List(ctx, accounts))
		require.Len(t, accounts.Items, 3, "switching identity must retain the default and both external accounts")
	}
	// A missing new identity must not destroy the account still used by running pods.
	n.singleton.Spec.DCGMExporter.ServiceAccount.Name = "missing"
	n.idx = 0
	_, err := n.step()
	require.True(t, apierrors.IsNotFound(err))
	n.singleton.Spec.DCGMExporter.Enabled = new(false)
	state, err := n.step()
	require.NoError(t, err, "missing external accounts must not prevent teardown")
	require.Equal(t, gpuv1.Disabled, state)
	accounts := &corev1.ServiceAccountList{}
	require.NoError(t, n.client.List(ctx, accounts))
	require.Len(t, accounts.Items, 2)
	for _, sa := range accounts.Items {
		require.Empty(t, sa.OwnerReferences)
	}
}

func TestDCGMExporterDefaultAccountDeletionConflict(t *testing.T) {
	for _, replacement := range []bool{false, true} {
		t.Run(fmt.Sprintf("replacement=%t", replacement), func(t *testing.T) {
			n := newExporterAccountController(t)
			_, err := ServiceAccount(n)
			require.NoError(t, err)
			n.singleton.Spec.DCGMExporter.Enabled = new(false)
			base := n.client
			deletes := 0
			n.client = interceptor.NewClient(base.(client.WithWatch), interceptor.Funcs{Delete: func(ctx context.Context, c client.WithWatch, obj client.Object, opts ...client.DeleteOption) error {
				deletes++
				if deletes == 1 {
					current := &corev1.ServiceAccount{}
					require.NoError(t, c.Get(ctx, client.ObjectKeyFromObject(obj), current))
					if replacement {
						require.NoError(t, c.Delete(ctx, current))
						current = &corev1.ServiceAccount{ObjectMeta: metav1.ObjectMeta{Name: obj.GetName(), Namespace: obj.GetNamespace(), UID: "replacement", Labels: map[string]string{"app": "nvidia-dcgm-exporter"}}}
						require.NoError(t, c.Create(ctx, current))
						options := (&client.DeleteOptions{}).ApplyOptions(opts)
						require.NotNil(t, options.Preconditions)
						require.NotNil(t, options.Preconditions.UID)
						return apierrors.NewConflict(corev1.Resource("serviceaccounts"), obj.GetName(), fmt.Errorf("UID changed"))
					}
					current.Annotations = map[string]string{"audit": "updated"}
					require.NoError(t, c.Update(ctx, current))
				}
				return c.Delete(ctx, obj, opts...)
			}})
			_, err = ServiceAccount(n)
			require.True(t, apierrors.IsConflict(err))
			_, err = ServiceAccount(n)
			require.NoError(t, err)
			found := &corev1.ServiceAccount{}
			err = base.Get(t.Context(), client.ObjectKey{Namespace: "operator", Name: gpuv1.DCGMExporterDefaultServiceAccountName}, found)
			if replacement {
				require.NoError(t, err)
				require.Empty(t, found.OwnerReferences)
				require.Equal(t, 1, deletes)
			} else {
				require.True(t, apierrors.IsNotFound(err))
				require.Equal(t, 2, deletes)
			}
		})
	}
}

func TestClusterPolicyExporterServiceAccountPollingTransitions(t *testing.T) {
	cp := clusterPolicyForUpgradeTest(false)
	node := nodeWithLabels("nfd-node", map[string]string{"feature.node.kubernetes.io/test": "true"})
	r, c, _ := newClusterPolicyUpgradeTestReconciler(t, cp, node)
	require.NoError(t, appsv1.AddToScheme(r.Scheme))
	clusterPolicyCtrl.operatorNamespace = "operator"
	clusterPolicyCtrl.stateNames = []string{"state-dcgm-exporter"}
	clusterPolicyCtrl.resources = []Resources{{ServiceAccount: corev1.ServiceAccount{ObjectMeta: metav1.ObjectMeta{Name: gpuv1.DCGMExporterDefaultServiceAccountName}}}}
	clusterPolicyCtrl.controls = []controlFunc{{ServiceAccount}}
	require.NoError(t, c.Create(t.Context(), &corev1.ServiceAccount{ObjectMeta: metav1.ObjectMeta{Name: "byo", Namespace: "operator"}}))
	req := ctrl.Request{NamespacedName: client.ObjectKeyFromObject(cp)}

	// Reuse the policy to verify polling follows configuration changes in both directions.
	for _, tc := range []struct {
		name           string
		enabled        *bool
		serviceAccount *gpuv1.DCGMExporterServiceAccountConfig
		requeueAfter   time.Duration
	}{
		{name: "default account does not poll"},
		{name: "external account starts polling", serviceAccount: &gpuv1.DCGMExporterServiceAccountConfig{Name: "byo"}, requeueAfter: time.Minute},
		{name: "empty account name stops polling", serviceAccount: &gpuv1.DCGMExporterServiceAccountConfig{}},
		{name: "explicitly enabled external account polls", enabled: new(true), serviceAccount: &gpuv1.DCGMExporterServiceAccountConfig{Name: "byo"}, requeueAfter: time.Minute},
		{name: "disabling exporter stops polling", enabled: new(false), serviceAccount: &gpuv1.DCGMExporterServiceAccountConfig{Name: "byo"}},
		{name: "reenabling exporter resumes polling", serviceAccount: &gpuv1.DCGMExporterServiceAccountConfig{Name: "byo"}, requeueAfter: time.Minute},
		{name: "removing account configuration stops polling"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			require.NoError(t, c.Get(t.Context(), req.NamespacedName, cp))
			cp.Spec.DCGMExporter.Enabled = tc.enabled
			cp.Spec.DCGMExporter.ServiceAccount = tc.serviceAccount
			require.NoError(t, c.Update(t.Context(), cp))

			result, err := r.Reconcile(t.Context(), req)
			require.NoError(t, err)
			require.Equal(t, gpuv1.Ready, clusterPolicyState(t, c, cp.Name))
			require.Equal(t, tc.requeueAfter, result.RequeueAfter)
		})
	}
}

func TestClusterPolicyRechecksReadyExporterServiceAccount(t *testing.T) {
	cp := clusterPolicyForUpgradeTest(false)
	cp.Spec.DCGMExporter.ServiceAccount = &gpuv1.DCGMExporterServiceAccountConfig{Name: "byo"}
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

func TestDCGMExporterDefaultAccountDeletionUsesOwner(t *testing.T) {
	for name, tc := range map[string]struct {
		labels     map[string]string
		ownerUID   string
		wantDelete bool
	}{
		"controlled account without labels": {ownerUID: "policy-uid", wantDelete: true},
		"controlled account with changed app label": {
			labels: map[string]string{"app": "custom-label"}, ownerUID: "policy-uid", wantDelete: true,
		},
		"unowned account with exporter label": {labels: map[string]string{"app": "nvidia-dcgm-exporter"}},
		"another owner's account":             {ownerUID: "another-policy"},
	} {
		t.Run(name, func(t *testing.T) {
			n := newExporterAccountController(t)
			n.singleton.Spec.DCGMExporter.Enabled = new(false)
			sa := &corev1.ServiceAccount{ObjectMeta: metav1.ObjectMeta{
				Name: gpuv1.DCGMExporterDefaultServiceAccountName, Namespace: "operator", Labels: tc.labels,
			}}
			if tc.ownerUID != "" {
				sa.OwnerReferences = []metav1.OwnerReference{{
					APIVersion: "nvidia.com/v1", Kind: "ClusterPolicy", Name: "policy",
					UID: types.UID(tc.ownerUID), Controller: new(true),
				}}
			}
			require.NoError(t, n.client.Create(t.Context(), sa))
			state, err := ServiceAccount(n)
			require.NoError(t, err)
			require.Equal(t, gpuv1.Disabled, state)
			err = n.client.Get(t.Context(), client.ObjectKeyFromObject(sa), &corev1.ServiceAccount{})
			if tc.wantDelete {
				require.True(t, apierrors.IsNotFound(err))
			} else {
				require.NoError(t, err)
			}
		})
	}
}

func TestNonExporterServiceAccountDeletionIsUnchanged(t *testing.T) {
	n := newExporterAccountController(t)
	n.stateNames[0] = "state-driver"
	n.singleton.Spec.Driver.Enabled = new(false)
	sa := &corev1.ServiceAccount{ObjectMeta: metav1.ObjectMeta{Name: "driver", Namespace: "operator"}}
	n.resources[0].ServiceAccount = *sa.DeepCopy()
	require.NoError(t, n.client.Create(t.Context(), sa))
	state, err := ServiceAccount(n)
	require.NoError(t, err)
	require.Equal(t, gpuv1.Disabled, state)
	require.True(t, apierrors.IsNotFound(n.client.Get(t.Context(), client.ObjectKeyFromObject(sa), &corev1.ServiceAccount{})))
}
