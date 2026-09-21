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
	"sync"
	"testing"

	"github.com/stretchr/testify/require"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	nvidiav1alpha1 "github.com/NVIDIA/gpu-operator/api/nvidia/v1alpha1"
)

func TestNVIDIADriverNodeSelectorCache(t *testing.T) {
	ctx := context.Background()
	scheme := runtime.NewScheme()
	require.NoError(t, nvidiav1alpha1.AddToScheme(scheme))

	drivers := []nvidiav1alpha1.NVIDIADriver{
		{
			ObjectMeta: metav1.ObjectMeta{Name: "default"},
			Spec:       nvidiav1alpha1.NVIDIADriverSpec{Default: true},
		},
		{
			ObjectMeta: metav1.ObjectMeta{Name: "gold"},
			Spec: nvidiav1alpha1.NVIDIADriverSpec{
				NodeSelector: map[string]string{"region": "us-east-1"},
			},
		},
	}
	client := fake.NewClientBuilder().WithScheme(scheme).WithObjects(&drivers[0], &drivers[1]).Build()
	cache := &nvidiaDriverNodeSelectorCache{}

	tests := []struct {
		name      string
		oldLabels map[string]string
		newLabels map[string]string
		want      bool
	}{
		{name: "selector label added", oldLabels: map[string]string{}, newLabels: map[string]string{"region": "us-east-1"}, want: true},
		{name: "selector label value changed", oldLabels: map[string]string{"region": "us-east-1"}, newLabels: map[string]string{"region": "us-east-2"}, want: true},
		{name: "selector label removed", oldLabels: map[string]string{"region": "us-east-1"}, newLabels: map[string]string{}, want: true},
		{name: "empty selector label removed", oldLabels: map[string]string{"region": ""}, newLabels: map[string]string{}, want: true},
		{name: "unrelated label changed", oldLabels: map[string]string{"example.com/probe": "old"}, newLabels: map[string]string{"example.com/probe": "new"}, want: false},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, err := cache.selectorLabelsChanged(ctx, client, tc.oldLabels, tc.newLabels)
			require.NoError(t, err)
			require.Equal(t, tc.want, got)
		})
	}

	updatedDriver := drivers[1].DeepCopy()
	updatedDriver.Spec.NodeSelector = map[string]string{"zone": "us-east-1a"}
	cache.replace(updatedDriver)
	changed, err := cache.selectorLabelsChanged(ctx, client,
		map[string]string{"region": "us-east-1"}, map[string]string{"region": "us-east-2"})
	require.NoError(t, err)
	require.False(t, changed)

	changed, err = cache.selectorLabelsChanged(ctx, client, map[string]string{}, map[string]string{"zone": "us-east-1a"})
	require.NoError(t, err)
	require.True(t, changed)

	otherDriver := &nvidiav1alpha1.NVIDIADriver{
		ObjectMeta: metav1.ObjectMeta{Name: "silver"},
		Spec: nvidiav1alpha1.NVIDIADriverSpec{
			NodeSelector: map[string]string{"zone": "us-east-1b"},
		},
	}
	cache.replace(otherDriver)

	updatedDriver.DeletionTimestamp = ptr.To(metav1.Now())
	cache.replace(updatedDriver)
	changed, err = cache.selectorLabelsChanged(ctx, client,
		map[string]string{"region": "us-east-1"}, map[string]string{"region": "us-east-2"})
	require.NoError(t, err)
	require.False(t, changed)

	changed, err = cache.selectorLabelsChanged(ctx, client, map[string]string{}, map[string]string{"zone": "us-east-1b"})
	require.NoError(t, err)
	require.True(t, changed)

	cache.removeByKey(types.NamespacedName{Name: otherDriver.Name, Namespace: otherDriver.Namespace})
	changed, err = cache.selectorLabelsChanged(ctx, client, map[string]string{}, map[string]string{"zone": "us-east-1b"})
	require.NoError(t, err)
	require.False(t, changed)
}

func TestNVIDIADriverNodeSelectorCacheConcurrentAccess(t *testing.T) {
	cache := &nvidiaDriverNodeSelectorCache{initialized: true}
	driver := &nvidiav1alpha1.NVIDIADriver{
		ObjectMeta: metav1.ObjectMeta{Name: "gold"},
		Spec: nvidiav1alpha1.NVIDIADriverSpec{
			NodeSelector: map[string]string{"region": "us-east-1"},
		},
	}

	var wg sync.WaitGroup
	for range 8 {
		wg.Go(func() {
			for range 100 {
				cache.replace(driver)
				cache.removeByKey(types.NamespacedName{Name: driver.Name, Namespace: driver.Namespace})
			}
		})
		wg.Go(func() {
			for range 100 {
				_, _ = cache.selectorLabelsChanged(context.Background(), nil,
					map[string]string{}, map[string]string{"region": "us-east-1"})
			}
		})
	}
	wg.Wait()
}
