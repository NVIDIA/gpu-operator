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
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/client/interceptor"

	gpuv1 "github.com/NVIDIA/gpu-operator/api/nvidia/v1"
	nvidiav1alpha1 "github.com/NVIDIA/gpu-operator/api/nvidia/v1alpha1"
	"github.com/NVIDIA/gpu-operator/internal/consts"
)

func TestResolveActiveConfig(t *testing.T) {
	scheme := runtime.NewScheme()
	require.NoError(t, gpuv1.AddToScheme(scheme))
	require.NoError(t, nvidiav1alpha1.AddToScheme(scheme))

	clusterPolicy := &gpuv1.ClusterPolicy{ObjectMeta: metav1.ObjectMeta{Name: "cluster-policy"}}
	gpuCluster := &nvidiav1alpha1.GPUCluster{ObjectMeta: metav1.ObjectMeta{Name: "cluster-config"}}

	t.Run("both CRs present are both returned", func(t *testing.T) {
		c := fake.NewClientBuilder().WithScheme(scheme).
			WithObjects(clusterPolicy, gpuCluster).Build()

		cp, gc, err := resolveActiveConfig(context.Background(), c)
		require.NoError(t, err)
		require.NotNil(t, cp)
		assert.Equal(t, "cluster-policy", cp.Name)
		require.NotNil(t, gc)
		assert.Equal(t, "cluster-config", gc.Name)
	})

	t.Run("no GPUCluster returns only ClusterPolicy", func(t *testing.T) {
		c := fake.NewClientBuilder().WithScheme(scheme).
			WithObjects(clusterPolicy).Build()

		cp, gc, err := resolveActiveConfig(context.Background(), c)
		require.NoError(t, err)
		require.NotNil(t, cp)
		assert.Equal(t, "cluster-policy", cp.Name)
		assert.Nil(t, gc)
	})

	t.Run("no ClusterPolicy falls back to GPUCluster", func(t *testing.T) {
		c := fake.NewClientBuilder().WithScheme(scheme).
			WithObjects(gpuCluster).Build()

		cp, gc, err := resolveActiveConfig(context.Background(), c)
		require.NoError(t, err)
		assert.Nil(t, cp)
		require.NotNil(t, gc)
		assert.Equal(t, "cluster-config", gc.Name)
	})

	t.Run("neither CR present returns all nil", func(t *testing.T) {
		c := fake.NewClientBuilder().WithScheme(scheme).Build()

		cp, gc, err := resolveActiveConfig(context.Background(), c)
		require.NoError(t, err)
		assert.Nil(t, cp)
		assert.Nil(t, gc)
	})

	t.Run("ClusterPolicy list error is surfaced", func(t *testing.T) {
		listErr := errors.New("boom")
		c := fake.NewClientBuilder().WithScheme(scheme).
			WithInterceptorFuncs(interceptor.Funcs{
				List: func(ctx context.Context, cl client.WithWatch, list client.ObjectList, opts ...client.ListOption) error {
					if _, ok := list.(*gpuv1.ClusterPolicyList); ok {
						return listErr
					}
					return cl.List(ctx, list, opts...)
				},
			}).Build()

		cp, gc, err := resolveActiveConfig(context.Background(), c)
		require.ErrorIs(t, err, listErr)
		assert.Nil(t, cp)
		assert.Nil(t, gc)
	})

	t.Run("GPUCluster list error is surfaced", func(t *testing.T) {
		listErr := errors.New("boom")
		c := fake.NewClientBuilder().WithScheme(scheme).
			WithInterceptorFuncs(interceptor.Funcs{
				List: func(ctx context.Context, cl client.WithWatch, list client.ObjectList, opts ...client.ListOption) error {
					if _, ok := list.(*nvidiav1alpha1.GPUClusterList); ok {
						return listErr
					}
					return cl.List(ctx, list, opts...)
				},
			}).Build()

		cp, gc, err := resolveActiveConfig(context.Background(), c)
		require.ErrorIs(t, err, listErr)
		assert.Nil(t, cp)
		assert.Nil(t, gc)
	})
}

func TestResolveDefaultMode(t *testing.T) {
	testCases := []struct {
		description         string
		clusterPolicyExists bool
		gpuClusterExists    bool
		envDefaultMode      consts.GPUAllocationMode
		expected            consts.GPUAllocationMode
	}{
		{"both CRs, DEFAULT_GPU_ALLOCATION_MODE=dra", true, true, consts.GPUAllocationModeDRA, consts.GPUAllocationModeDRA},
		{"both CRs, DEFAULT_GPU_ALLOCATION_MODE=device-plugin", true, true, consts.GPUAllocationModeDevicePlugin, consts.GPUAllocationModeDevicePlugin},
		{"both CRs, DEFAULT_GPU_ALLOCATION_MODE unset defaults to device-plugin", true, true, "", consts.GPUAllocationModeDevicePlugin},
		{"only ClusterPolicy ignores DEFAULT_GPU_ALLOCATION_MODE", true, false, consts.GPUAllocationModeDRA, consts.GPUAllocationModeDevicePlugin},
		{"only GPUCluster ignores DEFAULT_GPU_ALLOCATION_MODE", false, true, consts.GPUAllocationModeDevicePlugin, consts.GPUAllocationModeDRA},
		{"neither CR resolves to no mode", false, false, consts.GPUAllocationModeDRA, ""},
	}
	for _, tc := range testCases {
		t.Run(tc.description, func(t *testing.T) {
			mode := resolveDefaultMode(tc.clusterPolicyExists, tc.gpuClusterExists, tc.envDefaultMode)
			assert.Equal(t, tc.expected, mode)
		})
	}
}
