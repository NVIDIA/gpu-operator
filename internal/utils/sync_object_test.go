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

package utils

import (
	"context"
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	rbacv1 "k8s.io/api/rbac/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/kubernetes/scheme"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/apiutil"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/client/interceptor"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
)

func newTestRole(verbs ...string) *rbacv1.Role {
	return &rbacv1.Role{
		ObjectMeta: metav1.ObjectMeta{Name: "role-a", Namespace: "test-ns", Labels: map[string]string{"app": "test"}},
		Rules:      []rbacv1.PolicyRule{{APIGroups: []string{""}, Resources: []string{"configmaps"}, Verbs: verbs}},
	}
}

// newLiveRole returns a Role with the metadata that the API server sets on a stored object.
func newLiveRole(verbs ...string) *rbacv1.Role {
	role := newTestRole(verbs...)
	role.UID = "uid-a"
	role.ResourceVersion = "42"
	return role
}

func toUnstructured(t *testing.T, obj client.Object) *unstructured.Unstructured {
	t.Helper()
	gvk, err := apiutil.GVKForObject(obj, scheme.Scheme)
	require.NoError(t, err)
	content, err := runtime.DefaultUnstructuredConverter.ToUnstructured(obj)
	require.NoError(t, err)
	u := &unstructured.Unstructured{Object: content}
	u.SetGroupVersionKind(gvk)
	return u
}

func newRenderedConfigMap(data any) *unstructured.Unstructured {
	return &unstructured.Unstructured{Object: map[string]any{
		"apiVersion": "v1",
		"kind":       "ConfigMap",
		"metadata":   map[string]any{"name": "cm-a", "namespace": "test-ns"},
		"data":       data,
	}}
}

func TestContentEqual(t *testing.T) {
	liveConfigMap := &corev1.ConfigMap{ObjectMeta: metav1.ObjectMeta{Name: "cm-a", Namespace: "test-ns", ResourceVersion: "7"}}
	liveConfigMapWithData := liveConfigMap.DeepCopy()
	liveConfigMapWithData.Data = map[string]string{"key": "value"}
	annotatedRole := newLiveRole("get")
	annotatedRole.Annotations = map[string]string{"example.com/note": "added"}
	ownedRole := newLiveRole("get")
	ownedRole.OwnerReferences = []metav1.OwnerReference{{APIVersion: "v1", Kind: "ConfigMap", Name: "owner", UID: "uid-owner"}}
	service := &corev1.Service{ObjectMeta: metav1.ObjectMeta{Name: "svc-a", Namespace: "test-ns"}}

	testCases := map[string]struct {
		// scheme defaults to the client-go scheme.
		scheme  *runtime.Scheme
		desired client.Object
		live    client.Object
		want    bool
	}{
		"typed objects with the same content": {
			desired: newTestRole("get"),
			live:    newLiveRole("get"),
			want:    true,
		},
		"unstructured objects with the same content": {
			desired: toUnstructured(t, newTestRole("get")),
			live:    toUnstructured(t, newLiveRole("get")),
			want:    true,
		},
		"unstructured desired and typed live with the same content": {
			desired: toUnstructured(t, newTestRole("get")),
			live:    newLiveRole("get"),
			want:    true,
		},
		"different rules": {
			desired: newTestRole("get", "list"),
			live:    newLiveRole("get"),
			want:    false,
		},
		"extra annotation on live": {
			desired: newTestRole("get"),
			live:    annotatedRole,
			want:    false,
		},
		"extra owner reference on live": {
			desired: newTestRole("get"),
			live:    ownedRole,
			want:    false,
		},
		"empty data and missing data": {
			desired: newRenderedConfigMap(map[string]any{}),
			live:    liveConfigMap,
			want:    true,
		},
		"null data and missing data": {
			desired: newRenderedConfigMap(nil),
			live:    liveConfigMap,
			want:    true,
		},
		"empty data and live data": {
			desired: newRenderedConfigMap(map[string]any{}),
			live:    liveConfigMapWithData,
			want:    false,
		},
		"kind that the API server defaults": {
			desired: service,
			live:    service.DeepCopy(),
			want:    false,
		},
		"data that cannot be converted": {
			desired: newRenderedConfigMap("not-a-map"),
			live:    liveConfigMap,
			want:    false,
		},
		"typed kind not in the scheme": {
			scheme:  runtime.NewScheme(),
			desired: newTestRole("get"),
			live:    newLiveRole("get"),
			want:    false,
		},
		"unstructured kind not in the scheme": {
			scheme:  runtime.NewScheme(),
			desired: toUnstructured(t, newTestRole("get")),
			live:    toUnstructured(t, newLiveRole("get")),
			want:    false,
		},
	}
	for name, testCase := range testCases {
		t.Run(name, func(t *testing.T) {
			testScheme := testCase.scheme
			if testScheme == nil {
				testScheme = scheme.Scheme
			}
			assert.Equal(t, testCase.want, ContentEqual(testScheme, testCase.desired, testCase.live))
		})
	}
}

type syncCalls struct {
	creates, updates, deletes int
}

func TestSyncObject(t *testing.T) {
	owner := &corev1.ConfigMap{ObjectMeta: metav1.ObjectMeta{Name: "owner", Namespace: "test-ns", UID: "uid-owner"}}
	owned := func(obj client.Object) client.Object {
		require.NoError(t, controllerutil.SetControllerReference(owner, obj, scheme.Scheme))
		return obj
	}
	newServiceAccount := func(labels map[string]string) *corev1.ServiceAccount {
		return &corev1.ServiceAccount{ObjectMeta: metav1.ObjectMeta{Name: "sa-a", Namespace: "test-ns", Labels: labels}}
	}
	newService := func(clusterIP string) *corev1.Service {
		return &corev1.Service{
			ObjectMeta: metav1.ObjectMeta{Name: "svc-a", Namespace: "test-ns"},
			Spec:       corev1.ServiceSpec{ClusterIP: clusterIP, Ports: []corev1.ServicePort{{Port: 80}}},
		}
	}

	testCases := map[string]struct {
		existing    []client.Object
		desired     client.Object
		live        client.Object
		shouldExist bool
		opts        SyncOptions
		failOn      string
		wantErr     bool
		wantCalls   syncCalls
		// check inspects the stored object and the last object sent in an update.
		check func(t *testing.T, c client.Client, updated client.Object)
	}{
		"object that should not exist and is missing": {
			desired:   newTestRole("get"),
			live:      &rbacv1.Role{},
			wantCalls: syncCalls{},
		},
		"object that should not exist and is present is deleted": {
			existing:  []client.Object{newTestRole("get")},
			desired:   newTestRole("get"),
			live:      &rbacv1.Role{},
			wantCalls: syncCalls{deletes: 1},
			check: func(t *testing.T, c client.Client, _ client.Object) {
				err := c.Get(context.Background(), client.ObjectKey{Name: "role-a", Namespace: "test-ns"}, &rbacv1.Role{})
				assert.True(t, apierrors.IsNotFound(err))
			},
		},
		"missing object is created with an owner reference": {
			desired:     newTestRole("get"),
			live:        &rbacv1.Role{},
			shouldExist: true,
			wantCalls:   syncCalls{creates: 1},
			check: func(t *testing.T, c client.Client, _ client.Object) {
				stored := &rbacv1.Role{}
				require.NoError(t, c.Get(context.Background(), client.ObjectKey{Name: "role-a", Namespace: "test-ns"}, stored))
				require.NotNil(t, metav1.GetControllerOf(stored))
				assert.Equal(t, owner.UID, metav1.GetControllerOf(stored).UID)
			},
		},
		"CreateOnly keeps an existing object": {
			existing:    []client.Object{newServiceAccount(map[string]string{"version": "old"})},
			desired:     newServiceAccount(map[string]string{"version": "new"}),
			live:        &corev1.ServiceAccount{},
			shouldExist: true,
			opts:        SyncOptions{CreateOnly: true},
			wantCalls:   syncCalls{},
		},
		"unchanged object is not updated": {
			existing:    []client.Object{owned(newTestRole("get"))},
			desired:     newTestRole("get"),
			live:        &rbacv1.Role{},
			shouldExist: true,
			wantCalls:   syncCalls{},
		},
		"changed object is updated with the live resource version": {
			existing:    []client.Object{owned(newTestRole("get"))},
			desired:     newTestRole("get", "list"),
			live:        &rbacv1.Role{},
			shouldExist: true,
			wantCalls:   syncCalls{updates: 1},
			check: func(t *testing.T, c client.Client, updated client.Object) {
				assert.Equal(t, "999", updated.GetResourceVersion())
				stored := &rbacv1.Role{}
				require.NoError(t, c.Get(context.Background(), client.ObjectKey{Name: "role-a", Namespace: "test-ns"}, stored))
				assert.Equal(t, []string{"get", "list"}, stored.Rules[0].Verbs)
			},
		},
		"PreserveLiveFields keeps the live ClusterIP": {
			existing:    []client.Object{owned(newService("10.0.0.1"))},
			desired:     newService(""),
			live:        &corev1.Service{},
			shouldExist: true,
			opts:        SyncOptions{PreserveLiveFields: PreserveServiceClusterIP},
			wantCalls:   syncCalls{updates: 1},
			check: func(t *testing.T, c client.Client, _ client.Object) {
				stored := &corev1.Service{}
				require.NoError(t, c.Get(context.Background(), client.ObjectKey{Name: "svc-a", Namespace: "test-ns"}, stored))
				assert.Equal(t, "10.0.0.1", stored.Spec.ClusterIP)
			},
		},
		"read error is returned": {
			desired:     newTestRole("get"),
			live:        &rbacv1.Role{},
			shouldExist: true,
			failOn:      "get",
			wantErr:     true,
			wantCalls:   syncCalls{},
		},
		"create error is returned": {
			desired:     newTestRole("get"),
			live:        &rbacv1.Role{},
			shouldExist: true,
			failOn:      "create",
			wantErr:     true,
			wantCalls:   syncCalls{creates: 1},
		},
		"update error is returned": {
			existing:    []client.Object{owned(newTestRole("get"))},
			desired:     newTestRole("get", "list"),
			live:        &rbacv1.Role{},
			shouldExist: true,
			failOn:      "update",
			wantErr:     true,
			wantCalls:   syncCalls{updates: 1},
		},
		"delete error is returned": {
			existing:  []client.Object{newTestRole("get")},
			desired:   newTestRole("get"),
			live:      &rbacv1.Role{},
			failOn:    "delete",
			wantErr:   true,
			wantCalls: syncCalls{deletes: 1},
		},
	}
	for name, testCase := range testCases {
		t.Run(name, func(t *testing.T) {
			var gotCalls syncCalls
			var updated client.Object
			injectedError := func(call string) error {
				if testCase.failOn == call {
					return fmt.Errorf("injected %s error", call)
				}
				return nil
			}
			fakeClient := fake.NewClientBuilder().WithScheme(scheme.Scheme).WithObjects(testCase.existing...).
				WithInterceptorFuncs(interceptor.Funcs{
					Get: func(ctx context.Context, wrappedClient client.WithWatch, key client.ObjectKey, object client.Object, opts ...client.GetOption) error {
						if err := injectedError("get"); err != nil {
							return err
						}
						return wrappedClient.Get(ctx, key, object, opts...)
					},
					Create: func(ctx context.Context, wrappedClient client.WithWatch, object client.Object, opts ...client.CreateOption) error {
						gotCalls.creates++
						if err := injectedError("create"); err != nil {
							return err
						}
						return wrappedClient.Create(ctx, object, opts...)
					},
					Update: func(ctx context.Context, wrappedClient client.WithWatch, object client.Object, opts ...client.UpdateOption) error {
						gotCalls.updates++
						updated = object.DeepCopyObject().(client.Object)
						if err := injectedError("update"); err != nil {
							return err
						}
						return wrappedClient.Update(ctx, object, opts...)
					},
					Delete: func(ctx context.Context, wrappedClient client.WithWatch, object client.Object, opts ...client.DeleteOption) error {
						gotCalls.deletes++
						if err := injectedError("delete"); err != nil {
							return err
						}
						return wrappedClient.Delete(ctx, object, opts...)
					},
				}).Build()

			err := SyncObject(context.Background(), fakeClient, owner, testCase.desired, testCase.live, testCase.shouldExist, testCase.opts)
			if testCase.wantErr {
				require.Error(t, err)
			} else {
				require.NoError(t, err)
			}
			assert.Equal(t, testCase.wantCalls, gotCalls)
			if testCase.check != nil {
				testCase.check(t, fakeClient, updated)
			}
		})
	}
}
