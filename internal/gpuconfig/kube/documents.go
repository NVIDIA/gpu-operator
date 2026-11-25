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
	"fmt"
	"maps"
	"time"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/fields"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/watch"
	"k8s.io/client-go/tools/cache"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/NVIDIA/gpu-operator/internal/gpuconfig/config"
)

// ListWatchFor returns the list and watch calls for one ConfigMap.
type ListWatchFor func(name string) cache.ListerWatcher

// ConfigMapListWatch watches one ConfigMap by name with w. client-go streams
// the initial list over that watch by default, so it isn't bounded by c's
// timeout; c lists only when the API server can't stream the initial list.
// A metadata.name field selector lets the API server's watch cache scope
// the watch to that name, so an applier never receives other nodes'
// documents.
func ConfigMapListWatch(c client.Client, w client.WithWatch, namespace string) ListWatchFor {
	return func(name string) cache.ListerWatcher {
		scoped := func(opts metav1.ListOptions) *client.ListOptions {
			return &client.ListOptions{
				Namespace:     namespace,
				FieldSelector: fields.OneTermEqualSelector("metadata.name", name),
				Raw:           &opts,
			}
		}
		return &cache.ListWatch{
			ListWithContextFunc: func(ctx context.Context, opts metav1.ListOptions) (runtime.Object, error) {
				list := &corev1.ConfigMapList{}
				err := c.List(ctx, list, scoped(opts))
				return list, err
			},
			WatchFuncWithContext: func(ctx context.Context, opts metav1.ListOptions) (watch.Interface, error) {
				return w.Watch(ctx, &corev1.ConfigMapList{}, scoped(opts))
			},
		}
	}
}

// Documents watches the node, pool and cluster documents by name.
type Documents struct {
	// names in precedence order; "" for a document the node can't have.
	names     []string
	informers []cache.SharedInformer
}

// NewDocuments watches the documents with the given names, in precedence
// order: node, pool, cluster. An empty name is skipped. onChange runs on
// every change after the initial list: a create, an edit or a delete.
func NewDocuments(names []string, lw ListWatchFor, onChange func()) (*Documents, error) {
	d := &Documents{}
	handler := cache.ResourceEventHandlerDetailedFuncs{
		AddFunc: func(_ any, isInInitialList bool) {
			if !isInInitialList {
				onChange()
			}
		},
		UpdateFunc: func(_, _ any) { onChange() },
		DeleteFunc: func(_ any) { onChange() },
	}
	for _, name := range names {
		if name == "" {
			continue
		}
		inf := cache.NewSharedInformer(lw(name), &corev1.ConfigMap{}, 0)
		if _, err := inf.AddEventHandler(handler); err != nil {
			return nil, err
		}
		d.names = append(d.names, name)
		d.informers = append(d.informers, inf)
	}
	return d, nil
}

// Run starts the watches and returns once ctx is done.
func (d *Documents) Run(ctx context.Context) {
	for _, inf := range d.informers {
		go inf.RunWithContext(ctx)
	}
	<-ctx.Done()
}

// WaitForSync waits up to timeout until every watch has listed its document.
// An unsynced watch would read as a missing document, and the node would use
// the wrong one.
func (d *Documents) WaitForSync(ctx context.Context, timeout time.Duration) error {
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	synced := make([]cache.InformerSynced, len(d.informers))
	for i, inf := range d.informers {
		synced[i] = inf.HasSynced
	}
	if cache.WaitForCacheSync(ctx.Done(), synced...) {
		return nil
	}
	var pending []string
	for i, inf := range d.informers {
		if !inf.HasSynced() {
			pending = append(pending, d.names[i])
		}
	}
	return fmt.Errorf("listing ConfigMaps %v didn't succeed within %s: %w", pending, timeout, ctx.Err())
}

// Snapshot returns a copy of the document the node uses: the first of its
// watched documents that exists, or nil when none does.
func (d *Documents) Snapshot() *config.Source {
	for i, inf := range d.informers {
		for _, obj := range inf.GetStore().List() {
			cm, ok := obj.(*corev1.ConfigMap)
			if !ok || cm.Name != d.names[i] {
				continue
			}
			return &config.Source{Name: cm.Name, Data: maps.Clone(cm.Data)}
		}
	}
	return nil
}
