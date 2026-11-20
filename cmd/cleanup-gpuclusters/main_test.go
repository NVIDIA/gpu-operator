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

package main

import (
	"context"
	"testing"
	"testing/synctest"

	"github.com/stretchr/testify/require"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/client/interceptor"

	gpuv1 "github.com/NVIDIA/gpu-operator/api/nvidia/v1"
	gpu "github.com/NVIDIA/gpu-operator/api/nvidia/v1alpha1"
)

func TestDeleteStacks(t *testing.T) {
	scheme := runtime.NewScheme()
	require.NoError(t, gpuv1.AddToScheme(scheme))
	require.NoError(t, gpu.AddToScheme(scheme))

	for _, tc := range []struct {
		name        string
		all         bool
		wantGCFound bool
	}{
		{name: "release-owned CRs only", wantGCFound: true},
		{name: "CRD cleanup includes independently managed CRs", all: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			cp := &gpuv1.ClusterPolicy{ObjectMeta: metav1.ObjectMeta{Name: "cluster-policy", UID: "cp", Annotations: map[string]string{
				"meta.helm.sh/release-name":      "operator",
				"meta.helm.sh/release-namespace": "gpu-operator",
			}}}
			gc := &gpu.GPUCluster{ObjectMeta: metav1.ObjectMeta{Name: "gpu-cluster", UID: "gc"}}
			c := fake.NewClientBuilder().WithScheme(scheme).WithObjects(cp, gc).Build()
			require.NoError(t, deleteStacks(t.Context(), c, cp.Name, gc.Name, "operator", "gpu-operator", tc.all))
			require.True(t, apierrors.IsNotFound(c.Get(t.Context(), client.ObjectKeyFromObject(cp), cp)))
			err := c.Get(t.Context(), client.ObjectKeyFromObject(gc), gc)
			if tc.wantGCFound {
				require.NoError(t, err)
			} else {
				require.True(t, apierrors.IsNotFound(err))
			}
		})
	}
}

func TestDeleteStacksRetriesReadErrorsWhileWaiting(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		scheme := runtime.NewScheme()
		require.NoError(t, gpuv1.AddToScheme(scheme))
		cp := &gpuv1.ClusterPolicy{ObjectMeta: metav1.ObjectMeta{Name: "cluster-policy", UID: "cp"}}
		const readErrors = 7
		polls := 0
		deleteRequested := false
		c := fake.NewClientBuilder().WithScheme(scheme).WithObjects(cp).WithInterceptorFuncs(interceptor.Funcs{
			Delete: func(ctx context.Context, c client.WithWatch, obj client.Object, opts ...client.DeleteOption) error {
				deleteRequested = true
				return c.Delete(ctx, obj, opts...)
			},
			Get: func(ctx context.Context, c client.WithWatch, key client.ObjectKey, obj client.Object, opts ...client.GetOption) error {
				if deleteRequested {
					polls++
					if polls <= readErrors {
						return apierrors.NewTooManyRequests("API server is throttling requests", 1)
					}
				}
				return c.Get(ctx, key, obj, opts...)
			},
		}).Build()

		require.NoError(t, deleteStacks(t.Context(), c, cp.Name, "", "", "", false))
		require.Equal(t, readErrors+1, polls)
	})
}
