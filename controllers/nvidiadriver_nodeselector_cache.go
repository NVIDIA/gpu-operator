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

	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"

	nvidiav1alpha1 "github.com/NVIDIA/gpu-operator/api/nvidia/v1alpha1"
)

// nvidiaDriverNodeSelectorCache avoids listing drivers for unrelated Node updates.
type nvidiaDriverNodeSelectorCache struct {
	mu           sync.RWMutex
	keysByDriver map[types.NamespacedName]map[string]struct{}
	keyRefCounts map[string]int
	initialized  bool
}

// replace updates the selector keys for one NVIDIADriver.
func (cache *nvidiaDriverNodeSelectorCache) replace(driver *nvidiav1alpha1.NVIDIADriver) {
	cache.mu.Lock()
	defer cache.mu.Unlock()

	key := client.ObjectKeyFromObject(driver)
	if driver.HasDeletionTimestamp() {
		cache.removeLocked(key)
		return
	}

	if cache.keysByDriver == nil {
		cache.keysByDriver = make(map[types.NamespacedName]map[string]struct{})
		cache.keyRefCounts = make(map[string]int)
	}
	cache.removeLocked(key)

	selectorKeys := make(map[string]struct{}, len(driver.Spec.NodeSelector))
	for selectorKey := range driver.Spec.NodeSelector {
		selectorKeys[selectorKey] = struct{}{}
		cache.keyRefCounts[selectorKey]++
	}
	cache.keysByDriver[key] = selectorKeys
}

// removeByKey removes a NVIDIADriver's selector keys after deletion.
func (cache *nvidiaDriverNodeSelectorCache) removeByKey(key types.NamespacedName) {
	cache.mu.Lock()
	defer cache.mu.Unlock()
	cache.removeLocked(key)
}

// removeLocked removes a NVIDIADriver's selector keys while the cache is locked.
func (cache *nvidiaDriverNodeSelectorCache) removeLocked(key types.NamespacedName) {
	for selectorKey := range cache.keysByDriver[key] {
		cache.keyRefCounts[selectorKey]--
		if cache.keyRefCounts[selectorKey] == 0 {
			delete(cache.keyRefCounts, selectorKey)
		}
	}
	delete(cache.keysByDriver, key)
}

// initialize populates the cache once when a Node event arrives before reconciliation.
func (cache *nvidiaDriverNodeSelectorCache) initialize(ctx context.Context, c client.Client) error {
	cache.mu.Lock()
	defer cache.mu.Unlock()
	if cache.initialized {
		return nil
	}
	return cache.refreshLocked(ctx, c)
}

// refresh rebuilds the cache from the current NVIDIADriver list.
func (cache *nvidiaDriverNodeSelectorCache) refresh(ctx context.Context, c client.Client) error {
	cache.mu.Lock()
	defer cache.mu.Unlock()
	return cache.refreshLocked(ctx, c)
}

// refreshLocked rebuilds the cache while the cache is locked.
func (cache *nvidiaDriverNodeSelectorCache) refreshLocked(ctx context.Context, c client.Client) error {
	drivers := &nvidiav1alpha1.NVIDIADriverList{}
	if err := c.List(ctx, drivers); err != nil {
		return err
	}

	cache.keysByDriver = make(map[types.NamespacedName]map[string]struct{})
	cache.keyRefCounts = make(map[string]int)
	for index := range drivers.Items {
		driver := &drivers.Items[index]
		if driver.HasDeletionTimestamp() {
			continue
		}

		key := client.ObjectKeyFromObject(driver)
		selectorKeys := make(map[string]struct{}, len(driver.Spec.NodeSelector))
		for selectorKey := range driver.Spec.NodeSelector {
			selectorKeys[selectorKey] = struct{}{}
			cache.keyRefCounts[selectorKey]++
		}
		cache.keysByDriver[key] = selectorKeys
	}
	cache.initialized = true
	return nil
}

// selectorLabelsChanged reports whether a configured node-selector key changed.
func (cache *nvidiaDriverNodeSelectorCache) selectorLabelsChanged(ctx context.Context, c client.Client, oldLabels, newLabels map[string]string) (bool, error) {
	cache.mu.RLock()
	initialized := cache.initialized
	cache.mu.RUnlock()
	if !initialized {
		if err := cache.initialize(ctx, c); err != nil {
			return false, err
		}
	}

	cache.mu.RLock()
	defer cache.mu.RUnlock()
	for key, oldValue := range oldLabels {
		newValue, exists := newLabels[key]
		if (!exists || oldValue != newValue) && cache.keyRefCounts[key] > 0 {
			return true, nil
		}
	}
	for key := range newLabels {
		if _, exists := oldLabels[key]; !exists && cache.keyRefCounts[key] > 0 {
			return true, nil
		}
	}
	return false, nil
}
