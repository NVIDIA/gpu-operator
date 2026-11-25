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

package kube

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/watch"
	"k8s.io/client-go/tools/cache"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	"github.com/NVIDIA/gpu-operator/internal/gpuconfig/config"
)

const ns = "gpu-operator"

func configMap(name, yaml string) *corev1.ConfigMap {
	return &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: ns, ResourceVersion: "1"},
		Data:       map[string]string{config.ConfigKey: yaml},
	}
}

// fakeDocs serves one fake watch per document name.
type fakeDocs struct {
	mu       sync.Mutex
	initial  map[string]*corev1.ConfigMap
	watchers map[string]*watch.FakeWatcher
	listErrs map[string]error
}

// noWatchList makes the reflector list and then watch, instead of waiting
// for the watch-list bookmark a fake watch never sends.
type noWatchList struct{ *cache.ListWatch }

func (noWatchList) IsWatchListSemanticsUnSupported() bool { return true }

func (f *fakeDocs) listWatch(name string) cache.ListerWatcher {
	return noWatchList{&cache.ListWatch{
		ListWithContextFunc: func(context.Context, metav1.ListOptions) (runtime.Object, error) {
			if err := f.listErrs[name]; err != nil {
				return nil, err
			}
			list := &corev1.ConfigMapList{ListMeta: metav1.ListMeta{ResourceVersion: "1"}}
			if cm := f.initial[name]; cm != nil {
				list.Items = append(list.Items, *cm)
			}
			return list, nil
		},
		WatchFuncWithContext: func(context.Context, metav1.ListOptions) (watch.Interface, error) {
			f.mu.Lock()
			defer f.mu.Unlock()
			w := watch.NewFake()
			f.watchers[name] = w
			return w, nil
		},
	}}
}

func (f *fakeDocs) watcher(t *testing.T, name string) *watch.FakeWatcher {
	t.Helper()
	var w *watch.FakeWatcher
	require.Eventually(t, func() bool {
		f.mu.Lock()
		defer f.mu.Unlock()
		w = f.watchers[name]
		return w != nil
	}, 5*time.Second, 10*time.Millisecond)
	return w
}

func TestDocuments(t *testing.T) {
	f := &fakeDocs{
		initial:  map[string]*corev1.ConfigMap{"gpu-config": configMap("gpu-config", "version: v1\n")},
		watchers: map[string]*watch.FakeWatcher{},
	}
	var changes atomic.Int32
	d, err := NewDocuments([]string{"gpu-config-node-n1", "", "gpu-config"}, f.listWatch, func() { changes.Add(1) })
	require.NoError(t, err)

	ctx := t.Context()
	go d.Run(ctx)
	require.NoError(t, d.WaitForSync(ctx, 5*time.Second))

	// Only the cluster document exists. The initial list isn't a change.
	src := d.Snapshot()
	require.Equal(t, "gpu-config", src.Name)
	require.Equal(t, "version: v1\n", src.Data[config.ConfigKey])
	require.Zero(t, changes.Load())

	// A node document takes precedence once it exists.
	node := configMap("gpu-config-node-n1", "version: v1\ndefault: {eccMode: enabled}\n")
	f.watcher(t, "gpu-config-node-n1").Add(node)
	require.Eventually(t, func() bool { return d.Snapshot().Name == "gpu-config-node-n1" }, 5*time.Second, 10*time.Millisecond)
	require.Eventually(t, func() bool { return changes.Load() == 1 }, 5*time.Second, 10*time.Millisecond)

	// The snapshot is a copy: editing it doesn't change the cache.
	snap := d.Snapshot()
	snap.Data[config.ConfigKey] = "edited"
	require.NotEqual(t, "edited", d.Snapshot().Data[config.ConfigKey])

	// An edit and a delete are changes too; deleting the node document falls
	// back to the cluster document.
	edited := node.DeepCopy()
	edited.ResourceVersion = "2"
	f.watcher(t, "gpu-config-node-n1").Modify(edited)
	f.watcher(t, "gpu-config-node-n1").Delete(edited)
	require.Eventually(t, func() bool { return changes.Load() == 3 }, 5*time.Second, 10*time.Millisecond)
	require.Equal(t, "gpu-config", d.Snapshot().Name)
}

func TestDocumentsNoneExists(t *testing.T) {
	f := &fakeDocs{initial: map[string]*corev1.ConfigMap{}, watchers: map[string]*watch.FakeWatcher{}}
	d, err := NewDocuments([]string{"gpu-config-node-n1", "gpu-config-pool-a", "gpu-config"}, f.listWatch, func() {})
	require.NoError(t, err)
	ctx := t.Context()
	go d.Run(ctx)
	require.NoError(t, d.WaitForSync(ctx, 5*time.Second))
	require.Nil(t, d.Snapshot())
}

func TestWaitForSyncTimesOutWhenAListFails(t *testing.T) {
	forbidden := apierrors.NewForbidden(schema.GroupResource{Resource: "configmaps"}, "", errors.New("rbac"))
	f := &fakeDocs{
		initial:  map[string]*corev1.ConfigMap{},
		watchers: map[string]*watch.FakeWatcher{},
		listErrs: map[string]error{"gpu-config-node-n1": forbidden},
	}
	d, err := NewDocuments([]string{"gpu-config-node-n1", "", "gpu-config"}, f.listWatch, func() {})
	require.NoError(t, err)
	ctx := t.Context()
	go d.Run(ctx)
	start := time.Now()
	err = d.WaitForSync(ctx, 300*time.Millisecond)
	require.ErrorIs(t, err, context.DeadlineExceeded)
	require.ErrorContains(t, err, "[gpu-config-node-n1]")
	require.Less(t, time.Since(start), 5*time.Second)
}

func TestConfigMapListWatchIsScopedByName(t *testing.T) {
	c := fake.NewClientBuilder().WithScheme(NewScheme()).
		WithObjects(configMap("gpu-config", "a"), configMap("other", "b")).
		WithIndex(&corev1.ConfigMap{}, "metadata.name", func(o client.Object) []string { return []string{o.GetName()} }).
		Build()
	lw := ConfigMapListWatch(c, c, ns)("gpu-config")
	obj, err := lw.(*cache.ListWatch).ListWithContextFunc(context.Background(), metav1.ListOptions{})
	require.NoError(t, err)
	list := obj.(*corev1.ConfigMapList)
	require.Len(t, list.Items, 1)
	require.Equal(t, "gpu-config", list.Items[0].Name)
}
