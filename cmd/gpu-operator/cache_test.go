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
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	apiimagev1 "github.com/openshift/api/image/v1"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	resourcev1 "k8s.io/api/resource/v1"
	apiextensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/rest"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/cache"
	"sigs.k8s.io/controller-runtime/pkg/client"
	metricsserver "sigs.k8s.io/controller-runtime/pkg/metrics/server"

	"github.com/NVIDIA/gpu-operator/controllers"
)

func TestOperatorCacheManagerCreation(t *testing.T) {
	for name, tc := range map[string]struct {
		imageStreams   bool
		resourceClaims bool
	}{
		"Kubernetes with DRA":       {resourceClaims: true},
		"Kubernetes without DRA v1": {},
		"OpenShift with DRA":        {imageStreams: true, resourceClaims: true},
		"OpenShift without DRA v1":  {imageStreams: true},
	} {
		t.Run(name, func(t *testing.T) {
			mapper := meta.NewDefaultRESTMapper(nil)
			mapper.Add(corev1.SchemeGroupVersion.WithKind("Node"), meta.RESTScopeRoot)
			mapper.Add(corev1.SchemeGroupVersion.WithKind("Pod"), meta.RESTScopeNamespace)
			mapper.Add(apiextensionsv1.SchemeGroupVersion.WithKind("CustomResourceDefinition"), meta.RESTScopeRoot)
			if tc.imageStreams {
				mapper.Add(apiimagev1.SchemeGroupVersion.WithKind("ImageStream"), meta.RESTScopeNamespace)
			}
			if tc.resourceClaims {
				mapper.Add(resourcev1.SchemeGroupVersion.WithKind("ResourceClaim"), meta.RESTScopeNamespace)
			}
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				t.Errorf("unexpected API request during manager creation: %s %s", r.Method, r.URL)
				http.NotFound(w, r)
			}))
			t.Cleanup(server.Close)
			options := operatorCacheOptions("gpu-operator")
			manager, err := ctrl.NewManager(&rest.Config{Host: server.URL}, ctrl.Options{
				Scheme: scheme,
				MapperProvider: func(*rest.Config, *http.Client) (meta.RESTMapper, error) {
					return mapper, nil
				},
				Cache:                  options,
				NewCache:               newOperatorCache,
				Metrics:                metricsserver.Options{BindAddress: "0"},
				HealthProbeBindAddress: "0",
			})
			require.NoError(t, err)
			require.NotNil(t, manager.GetCache())
			// Cache construction must not mutate the caller's configuration.
			require.Len(t, options.ByObject, 5)
		})
	}
}

type failingCacheRESTMapper struct {
	meta.RESTMapper
	kind schema.GroupKind
	err  error
}

func (m failingCacheRESTMapper) RESTMapping(kind schema.GroupKind, versions ...string) (*meta.RESTMapping, error) {
	if kind == m.kind {
		return nil, m.err
	}
	return m.RESTMapper.RESTMapping(kind, versions...)
}

func TestOperatorCacheDiscoveryError(t *testing.T) {
	for _, gvk := range []schema.GroupVersionKind{
		apiimagev1.SchemeGroupVersion.WithKind("ImageStream"),
		resourcev1.SchemeGroupVersion.WithKind("ResourceClaim"),
	} {
		t.Run(gvk.Kind, func(t *testing.T) {
			discoveryErr := errors.New("API discovery unavailable")
			mapper := meta.NewDefaultRESTMapper(nil)
			mapper.Add(corev1.SchemeGroupVersion.WithKind("Node"), meta.RESTScopeRoot)
			mapper.Add(corev1.SchemeGroupVersion.WithKind("Pod"), meta.RESTScopeNamespace)
			mapper.Add(apiextensionsv1.SchemeGroupVersion.WithKind("CustomResourceDefinition"), meta.RESTScopeRoot)
			mapper.Add(apiimagev1.SchemeGroupVersion.WithKind("ImageStream"), meta.RESTScopeNamespace)
			mapper.Add(resourcev1.SchemeGroupVersion.WithKind("ResourceClaim"), meta.RESTScopeNamespace)
			options := operatorCacheOptions("gpu-operator")
			options.Scheme = scheme
			options.Mapper = failingCacheRESTMapper{RESTMapper: mapper, kind: gvk.GroupKind(), err: discoveryErr}
			_, err := newOperatorCache(&rest.Config{Host: "http://unused.invalid"}, options)
			require.ErrorIs(t, err, discoveryErr)
		})
	}
}

// Exercise the real cache against an HTTP API stub: selectors must reach the
// server, namespace restrictions must affect LIST/WATCH, and repeated reads must
// stay local. A fake client would not exercise any of those properties.
func TestOperatorCache(t *testing.T) {
	const namespace = "gpu-operator"
	managedFields := []metav1.ManagedFieldsEntry{{Manager: "test-manager", Operation: metav1.ManagedFieldsOperationUpdate, APIVersion: "v1"}}
	node := &corev1.Node{
		ObjectMeta: metav1.ObjectMeta{
			Name: "unlabeled-node", ResourceVersion: "1", ManagedFields: managedFields,
			Labels: map[string]string{"kubernetes.io/os": "linux"},
		},
		Spec: corev1.NodeSpec{Unschedulable: true},
		Status: corev1.NodeStatus{
			Images:     []corev1.ContainerImage{{Names: []string{"example.com/large-image:v1"}, SizeBytes: 1000000}},
			NodeInfo:   corev1.NodeSystemInfo{ContainerRuntimeVersion: "containerd://2.0.0"},
			Conditions: []corev1.NodeCondition{{Type: corev1.NodeReady, Status: corev1.ConditionTrue}},
		},
	}
	configMap := &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{Name: "config", Namespace: namespace, ResourceVersion: "1", ManagedFields: managedFields},
		Data:       map[string]string{"config": "keep this payload"},
	}

	type resource struct {
		gvk      schema.GroupVersionKind
		selector string
		items    []client.Object
	}
	resources := map[string]resource{
		"/api/v1/nodes": {
			gvk: corev1.SchemeGroupVersion.WithKind("Node"), items: []client.Object{node},
		},
		"/api/v1/namespaces/gpu-operator/configmaps": {
			gvk: corev1.SchemeGroupVersion.WithKind("ConfigMap"), items: []client.Object{configMap},
		},
		"/apis/apiextensions.k8s.io/v1/customresourcedefinitions": {
			gvk:      apiextensionsv1.SchemeGroupVersion.WithKind("CustomResourceDefinition"),
			selector: "metadata.name=" + controllers.ServiceMonitorCRDName,
			items: []client.Object{&apiextensionsv1.CustomResourceDefinition{
				ObjectMeta: metav1.ObjectMeta{Name: controllers.ServiceMonitorCRDName, ResourceVersion: "1"},
			}},
		},
		"/apis/image.openshift.io/v1/namespaces/openshift/imagestreams": {
			gvk: apiimagev1.SchemeGroupVersion.WithKind("ImageStream"), selector: "metadata.name=driver-toolkit",
			items: []client.Object{&apiimagev1.ImageStream{
				ObjectMeta: metav1.ObjectMeta{Name: "driver-toolkit", Namespace: "openshift", ResourceVersion: "1"},
			}},
		},
		"/api/v1/namespaces/gpu-operator/pods": {
			gvk: corev1.SchemeGroupVersion.WithKind("Pod"),
		},
		"/api/v1/namespaces/openshift/pods": {
			gvk: corev1.SchemeGroupVersion.WithKind("Pod"),
		},
		"/apis/resource.k8s.io/v1/namespaces/gpu-operator/resourceclaims": {
			gvk: resourcev1.SchemeGroupVersion.WithKind("ResourceClaim"),
		},
		"/apis/resource.k8s.io/v1/namespaces/openshift/resourceclaims": {
			gvk: resourcev1.SchemeGroupVersion.WithKind("ResourceClaim"),
		},
	}

	var mu sync.Mutex
	initialReadCounts := map[string]int{}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		res, ok := resources[r.URL.Path]
		if !ok {
			t.Errorf("unexpected API request: %s %s", r.Method, r.URL)
			http.NotFound(w, r)
			return
		}
		if r.Method != http.MethodGet || r.URL.Query().Get("fieldSelector") != res.selector || r.URL.Query().Get("labelSelector") != "" {
			t.Errorf("unexpected method or selectors: %s %s", r.Method, r.URL)
		}
		w.Header().Set("Content-Type", "application/json")
		encoder := json.NewEncoder(w)
		writeJSON := func(value any) {
			if err := encoder.Encode(value); err != nil {
				t.Errorf("encode API response: %v", err)
			}
		}
		// Support both classic LIST/WATCH and client-go's streaming initial list.
		if r.URL.Query().Get("watch") != "true" || r.URL.Query().Get("sendInitialEvents") == "true" {
			mu.Lock()
			initialReadCounts[r.URL.Path]++
			mu.Unlock()
		}
		if r.URL.Query().Get("watch") == "true" {
			w.WriteHeader(http.StatusOK)
			if r.URL.Query().Get("sendInitialEvents") == "true" {
				for _, item := range res.items {
					obj := item.DeepCopyObject()
					obj.GetObjectKind().SetGroupVersionKind(res.gvk)
					writeJSON(map[string]any{"type": "ADDED", "object": obj})
				}
				writeJSON(map[string]any{
					"type": "BOOKMARK",
					"object": map[string]any{
						"apiVersion": res.gvk.GroupVersion().String(), "kind": res.gvk.Kind,
						"metadata": map[string]any{
							"resourceVersion": "1", "annotations": map[string]string{metav1.InitialEventsAnnotationKey: "true"},
						},
					},
				})
			}
			w.(http.Flusher).Flush()
			<-r.Context().Done()
			return
		}
		items := res.items
		if items == nil {
			items = []client.Object{}
		}
		writeJSON(map[string]any{
			"apiVersion": res.gvk.GroupVersion().String(), "kind": res.gvk.Kind + "List",
			"metadata": map[string]string{"resourceVersion": "1"}, "items": items,
		})
	}))
	t.Cleanup(server.Close)

	mapper := meta.NewDefaultRESTMapper(nil)
	mapper.Add(corev1.SchemeGroupVersion.WithKind("Node"), meta.RESTScopeRoot)
	mapper.Add(apiextensionsv1.SchemeGroupVersion.WithKind("CustomResourceDefinition"), meta.RESTScopeRoot)
	for _, gvk := range []schema.GroupVersionKind{
		corev1.SchemeGroupVersion.WithKind("Pod"),
		corev1.SchemeGroupVersion.WithKind("ConfigMap"),
		resourcev1.SchemeGroupVersion.WithKind("ResourceClaim"),
		apiimagev1.SchemeGroupVersion.WithKind("ImageStream"),
	} {
		mapper.Add(gvk, meta.RESTScopeNamespace)
		mapper.Add(gvk.GroupVersion().WithKind(gvk.Kind+"List"), meta.RESTScopeNamespace)
	}
	options := operatorCacheOptions(namespace)
	options.Scheme = scheme
	options.Mapper = mapper
	config := &rest.Config{Host: server.URL}
	objectCache, err := newOperatorCache(config, options)
	require.NoError(t, err)
	cachedClient, err := client.New(config, client.Options{
		Scheme: scheme, Mapper: mapper, Cache: &client.CacheOptions{Reader: objectCache},
	})
	require.NoError(t, err)

	ctx, cancel := context.WithTimeout(t.Context(), 15*time.Second)
	done := make(chan error, 1)
	go func() { done <- objectCache.Start(ctx) }()
	t.Cleanup(func() {
		cancel()
		require.NoError(t, <-done)
	})
	// Register before waiting for sync so this also works when Start has not yet
	// scheduled its goroutine. Informers are shared by subsequent client reads.
	for _, obj := range []client.Object{&corev1.Node{}, &corev1.ConfigMap{}, &corev1.Pod{}, &resourcev1.ResourceClaim{}, &apiextensionsv1.CustomResourceDefinition{}, &apiimagev1.ImageStream{}} {
		_, err := objectCache.GetInformer(ctx, obj, cache.BlockUntilSynced(false))
		require.NoError(t, err)
	}
	require.True(t, objectCache.WaitForCacheSync(ctx))

	for range 3 {
		gotNode := &corev1.Node{}
		require.NoError(t, cachedClient.Get(ctx, client.ObjectKey{Name: node.Name}, gotNode))
		expectedNode := node.DeepCopy()
		expectedNode.ManagedFields = nil
		expectedNode.Status.Images = nil
		require.Equal(t, expectedNode.ObjectMeta, gotNode.ObjectMeta)
		require.Equal(t, expectedNode.Spec, gotNode.Spec)
		require.Equal(t, expectedNode.Status, gotNode.Status)
		// Reads must remain detached copies, since reconcilers mutate labels before patching.
		gotNode.Labels["nvidia.com/gpu.present"] = "true"

		gotConfigMap := &corev1.ConfigMap{}
		require.NoError(t, cachedClient.Get(ctx, client.ObjectKeyFromObject(configMap), gotConfigMap))
		require.Empty(t, gotConfigMap.ManagedFields)
		require.Equal(t, configMap.Data, gotConfigMap.Data)
		require.NoError(t, cachedClient.Get(ctx, client.ObjectKey{Name: controllers.ServiceMonitorCRDName}, &apiextensionsv1.CustomResourceDefinition{}))
		require.NoError(t, cachedClient.Get(ctx, client.ObjectKey{Namespace: "openshift", Name: "driver-toolkit"}, &apiimagev1.ImageStream{}))
		require.NoError(t, cachedClient.List(ctx, &corev1.PodList{}))
		require.NoError(t, cachedClient.List(ctx, &resourcev1.ResourceClaimList{}))
	}
	mu.Lock()
	defer mu.Unlock()
	for path := range resources {
		require.Equal(t, 1, initialReadCounts[path], "expected a single initial LIST or streaming list for %s", path)
	}
}
