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

package state

import (
	"context"
	"fmt"
	"testing"

	"github.com/go-logr/logr"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/client/interceptor"
)

// TestCreateOrUpdateObjsGetError covers the branch where an object already
// exists but the subsequent Get fails.
func TestCreateOrUpdateObjsGetError(t *testing.T) {
	existing := newConfigMapUnstructured("cm-a", "test-ns")
	cl := fake.NewClientBuilder().WithScheme(skelTestScheme(t)).WithObjects(existing).
		WithInterceptorFuncs(interceptor.Funcs{
			Get: func(_ context.Context, _ client.WithWatch, _ client.ObjectKey, _ client.Object, _ ...client.GetOption) error {
				return fmt.Errorf("injected get error")
			},
		}).Build()
	s := newTestSkel(t, cl)

	desired := newConfigMapUnstructured("cm-a", "test-ns")
	noop := func(_ *unstructured.Unstructured) error { return nil }
	err := s.createOrUpdateObjs(context.Background(), noop, []*unstructured.Unstructured{desired})
	require.ErrorContains(t, err, "injected get error")
}

// TestCreateOrUpdateObjsMergeError covers the mergeObjects error branch: an
// existing ServiceAccount whose "secrets" field is malformed makes
// mergeServiceAccount fail.
func TestCreateOrUpdateObjsMergeError(t *testing.T) {
	existing := newServiceAccountUnstructured("sa-a", "test-ns")
	cl := fake.NewClientBuilder().WithScheme(skelTestScheme(t)).WithObjects(existing).
		WithInterceptorFuncs(interceptor.Funcs{
			Get: func(_ context.Context, _ client.WithWatch, _ client.ObjectKey, obj client.Object, _ ...client.GetOption) error {
				// Return a ServiceAccount whose secrets field is not a slice.
				u, ok := obj.(*unstructured.Unstructured)
				if !ok {
					return fmt.Errorf("unexpected object type")
				}
				u.SetGroupVersionKind(schema.GroupVersionKind{Version: "v1", Kind: "ServiceAccount"})
				u.SetName("sa-a")
				u.SetNamespace("test-ns")
				u.Object["secrets"] = "not-a-slice"
				return nil
			},
		}).Build()
	s := newTestSkel(t, cl)

	desired := newServiceAccountUnstructured("sa-a", "test-ns")
	noop := func(_ *unstructured.Unstructured) error { return nil }
	err := s.createOrUpdateObjs(context.Background(), noop, []*unstructured.Unstructured{desired})
	require.Error(t, err)
}

// TestCreateOrUpdateObjsUpdateError covers the updateObj error branch during
// create-or-update of an existing object.
func TestCreateOrUpdateObjsUpdateError(t *testing.T) {
	existing := newConfigMapUnstructured("cm-a", "test-ns")
	cl := fake.NewClientBuilder().WithScheme(skelTestScheme(t)).WithObjects(existing).
		WithInterceptorFuncs(interceptor.Funcs{
			Update: func(_ context.Context, _ client.WithWatch, _ client.Object, _ ...client.UpdateOption) error {
				return fmt.Errorf("injected update error")
			},
		}).Build()
	s := newTestSkel(t, cl)

	desired := newConfigMapUnstructured("cm-a", "test-ns")
	require.NoError(t, unstructured.SetNestedStringMap(desired.Object, map[string]string{"key": "new"}, "data"))
	noop := func(_ *unstructured.Unstructured) error { return nil }
	err := s.createOrUpdateObjs(context.Background(), noop, []*unstructured.Unstructured{desired})
	require.ErrorContains(t, err, "failed to update resource")
}

// TestMergeServiceAccountErrors covers the NestedSlice error branches for both
// secrets and imagePullSecrets fields.
func TestMergeServiceAccountErrors(t *testing.T) {
	s := newTestSkel(t, fake.NewClientBuilder().WithScheme(skelTestScheme(t)).Build())

	t.Run("malformed secrets", func(t *testing.T) {
		updated := newServiceAccountUnstructured("sa", "test-ns")
		current := newServiceAccountUnstructured("sa", "test-ns")
		current.Object["secrets"] = "not-a-slice"
		err := s.mergeServiceAccount(updated, current)
		require.Error(t, err)
	})

	t.Run("malformed imagePullSecrets", func(t *testing.T) {
		updated := newServiceAccountUnstructured("sa", "test-ns")
		current := newServiceAccountUnstructured("sa", "test-ns")
		require.NoError(t, unstructured.SetNestedSlice(current.Object,
			[]interface{}{map[string]interface{}{"name": "s"}}, "secrets"))
		current.Object["imagePullSecrets"] = "not-a-slice"
		err := s.mergeServiceAccount(updated, current)
		require.Error(t, err)
	})
}

// TestIsDaemonSetReadyErrors covers the JSON marshal and unmarshal error paths.
func TestIsDaemonSetReadyErrors(t *testing.T) {
	s := newTestSkel(t, fake.NewClientBuilder().WithScheme(skelTestScheme(t)).Build())

	t.Run("marshal error", func(t *testing.T) {
		daemonSet := newDaemonSetUnstructured("ds", "test-ns")
		// A channel value cannot be marshalled to JSON.
		daemonSet.Object["bad"] = make(chan int)
		_, err := s.isDaemonSetReady(daemonSet, logr.Discard())
		require.ErrorContains(t, err, "failed to marshall unstructured daemonset object")
	})

	t.Run("unmarshal error", func(t *testing.T) {
		daemonSet := newDaemonSetUnstructured("ds", "test-ns")
		// status must be an object; a string marshals fine but fails to unmarshal
		// into the typed DaemonSet.Status struct.
		daemonSet.Object["status"] = "not-an-object"
		_, err := s.isDaemonSetReady(daemonSet, logr.Discard())
		require.ErrorContains(t, err, "failed to unmarshall to daemonset object")
	})
}

// TestGetObjNotFoundHelper verifies IsNotFound classification on a missing object.
func TestGetObjNotFound(t *testing.T) {
	cl := fake.NewClientBuilder().WithScheme(skelTestScheme(t)).Build()
	s := newTestSkel(t, cl)
	missing := newConfigMapUnstructured("missing", "test-ns")
	err := s.getObj(context.Background(), missing)
	require.True(t, apierrors.IsNotFound(err))
}

// TestCreateObjAlreadyExists verifies the AlreadyExists branch in createObj.
func TestCreateObjAlreadyExists(t *testing.T) {
	obj := newConfigMapUnstructured("cm", "test-ns")
	cl := fake.NewClientBuilder().WithScheme(skelTestScheme(t)).WithObjects(obj).Build()
	s := newTestSkel(t, cl)

	err := s.createObj(context.Background(), newConfigMapUnstructured("cm", "test-ns"))
	require.True(t, apierrors.IsAlreadyExists(err))
	assert.Error(t, err)
}
