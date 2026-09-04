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

package v1alpha1

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	types "k8s.io/apimachinery/pkg/types"
	watch "k8s.io/apimachinery/pkg/watch"

	nvidiav1alpha1 "github.com/NVIDIA/gpu-operator/api/nvidia/v1alpha1"
)

// gpuClusterResource is the plural resource name passed to newGPUClusters.
const gpuClusterResource = "gpuclusters"

// gpuClusterSingletonName is the only name the CRD's CEL rule admits. No CEL
// validation runs at this client layer, but the fixtures use the real name.
const gpuClusterSingletonName = "gpu-cluster"

func gpuClusterCollectionPath() string {
	return collectionPath(gpuClusterResource)
}

func gpuClusterNamedPath(name string, subresources ...string) string {
	return namedPath(gpuClusterResource, name, subresources...)
}

// gpuClusterNewServer stands up a recording API server and returns the typed
// GPUCluster client wired to it.
func gpuClusterNewServer(t *testing.T, handler http.HandlerFunc) (*recordingServer, GPUClusterInterface) {
	t.Helper()

	ts, client := newRecordingServer(t, handler)
	return ts, client.GPUClusters()
}

// gpuCluster builds a fully typed GPUCluster, including the TypeMeta the
// client-side decoder needs to recognize the payload.
func gpuCluster(name string) *nvidiav1alpha1.GPUCluster {
	gv := nvidiav1alpha1.SchemeGroupVersion
	return &nvidiav1alpha1.GPUCluster{
		TypeMeta: metav1.TypeMeta{
			APIVersion: gv.String(),
			Kind:       nvidiav1alpha1.GPUClusterCRDName,
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:            name,
			ResourceVersion: "7",
			Labels:          map[string]string{"app": "gpu-cluster"},
		},
		Spec: nvidiav1alpha1.GPUClusterSpec{
			DRADriver: nvidiav1alpha1.DRADriverSpec{
				Repository:   "nvcr.io/nvidia/cloud-native",
				Image:        "k8s-dra-driver-gpu",
				Version:      "v25.3.0",
				FeatureGates: map[string]bool{"ComputeDomains": true},
			},
		},
	}
}

// gpuClusterList builds a decodable list payload holding items.
func gpuClusterList(items ...*nvidiav1alpha1.GPUCluster) *nvidiav1alpha1.GPUClusterList {
	gv := nvidiav1alpha1.SchemeGroupVersion
	list := &nvidiav1alpha1.GPUClusterList{
		TypeMeta: metav1.TypeMeta{APIVersion: gv.String(), Kind: "GPUClusterList"},
	}
	for _, item := range items {
		list.Items = append(list.Items, *item)
	}
	return list
}

// TestGPUClusterHTTPMatrix runs the shared verb/HTTP plumbing matrix against
// the generated GPUCluster client. Everything that depends on the GPUCluster
// schema lives in the focused tests below.
func TestGPUClusterHTTPMatrix(t *testing.T) {
	matrix := resourceMatrix[*nvidiav1alpha1.GPUCluster, *nvidiav1alpha1.GPUClusterList]{
		resource:      gpuClusterResource,
		labelSelector: "app=gpu-cluster",
		fieldSelector: "metadata.name=gpu-cluster",

		newObject: gpuCluster,
		newEmpty:  func() *nvidiav1alpha1.GPUCluster { return &nvidiav1alpha1.GPUCluster{} },
		newList:   gpuClusterList,
		listItems: func(list *nvidiav1alpha1.GPUClusterList) []*nvidiav1alpha1.GPUCluster {
			items := make([]*nvidiav1alpha1.GPUCluster, 0, len(list.Items))
			for i := range list.Items {
				items = append(items, &list.Items[i])
			}
			return items
		},

		get: func(ctx context.Context, c *NvidiaV1alpha1Client, name string, opts metav1.GetOptions) (*nvidiav1alpha1.GPUCluster, error) {
			return c.GPUClusters().Get(ctx, name, opts)
		},
		list: func(ctx context.Context, c *NvidiaV1alpha1Client, opts metav1.ListOptions) (*nvidiav1alpha1.GPUClusterList, error) {
			return c.GPUClusters().List(ctx, opts)
		},
		create: func(ctx context.Context, c *NvidiaV1alpha1Client, obj *nvidiav1alpha1.GPUCluster, opts metav1.CreateOptions) (*nvidiav1alpha1.GPUCluster, error) {
			return c.GPUClusters().Create(ctx, obj, opts)
		},
		update: func(ctx context.Context, c *NvidiaV1alpha1Client, obj *nvidiav1alpha1.GPUCluster, opts metav1.UpdateOptions) (*nvidiav1alpha1.GPUCluster, error) {
			return c.GPUClusters().Update(ctx, obj, opts)
		},
		updateStatus: func(ctx context.Context, c *NvidiaV1alpha1Client, obj *nvidiav1alpha1.GPUCluster, opts metav1.UpdateOptions) (*nvidiav1alpha1.GPUCluster, error) {
			return c.GPUClusters().UpdateStatus(ctx, obj, opts)
		},
		remove: func(ctx context.Context, c *NvidiaV1alpha1Client, name string, opts metav1.DeleteOptions) error {
			return c.GPUClusters().Delete(ctx, name, opts)
		},
		removeCollection: func(ctx context.Context, c *NvidiaV1alpha1Client, opts metav1.DeleteOptions, listOpts metav1.ListOptions) error {
			return c.GPUClusters().DeleteCollection(ctx, opts, listOpts)
		},
		patch: func(ctx context.Context, c *NvidiaV1alpha1Client, name string, pt types.PatchType, data []byte, opts metav1.PatchOptions, subresources ...string) (*nvidiav1alpha1.GPUCluster, error) {
			return c.GPUClusters().Patch(ctx, name, pt, data, opts, subresources...)
		},
		watch: func(ctx context.Context, c *NvidiaV1alpha1Client, opts metav1.ListOptions) (watch.Interface, error) {
			return c.GPUClusters().Watch(ctx, opts)
		},
	}

	matrix.run(t)
}

func TestGPUClustersRequestPaths(t *testing.T) {
	// Spelled out literally: the shared matrix derives its expectations from
	// the same helpers the client uses, so this pins the actual URLs.
	assert.Equal(t, "/apis/nvidia.com/v1alpha1/gpuclusters", gpuClusterCollectionPath())
	assert.Equal(t, "/apis/nvidia.com/v1alpha1/gpuclusters/gpu-cluster", gpuClusterNamedPath(gpuClusterSingletonName))
	assert.Equal(t, "/apis/nvidia.com/v1alpha1/gpuclusters/gpu-cluster/status", gpuClusterNamedPath(gpuClusterSingletonName, "status"))

	ts, client := gpuClusterNewServer(t, func(w http.ResponseWriter, _ *http.Request) {
		writeJSONResponse(w, http.StatusOK, gpuCluster(gpuClusterSingletonName))
	})

	_, err := client.Get(t.Context(), gpuClusterSingletonName, metav1.GetOptions{})
	require.NoError(t, err)

	req := ts.lastRequest(t)
	assert.Equal(t, "/apis/nvidia.com/v1alpha1/gpuclusters/gpu-cluster", req.Path)
	assertClusterScoped(t, req.Path)
}

func TestGPUClustersGroupVersionKind(t *testing.T) {
	gvk := gpuCluster(gpuClusterSingletonName).GroupVersionKind()
	assert.Equal(t, "nvidia.com", gvk.Group)
	assert.Equal(t, "v1alpha1", gvk.Version)
	assert.Equal(t, nvidiav1alpha1.GPUClusterCRDName, gvk.Kind)
	assert.Equal(t, "GPUClusterList", gpuClusterList().GroupVersionKind().Kind)
}

func TestGPUClustersGetDecodesSpec(t *testing.T) {
	want := gpuCluster(gpuClusterSingletonName)

	_, client := gpuClusterNewServer(t, func(w http.ResponseWriter, _ *http.Request) {
		writeJSONResponse(w, http.StatusOK, want)
	})

	got, err := client.Get(t.Context(), gpuClusterSingletonName, metav1.GetOptions{})
	require.NoError(t, err)

	require.NotNil(t, got)
	assert.Equal(t, gpuClusterSingletonName, got.Name)
	assert.Equal(t, "7", got.ResourceVersion)
	assert.Equal(t, "nvcr.io/nvidia/cloud-native", got.Spec.DRADriver.Repository)
	assert.Equal(t, "k8s-dra-driver-gpu", got.Spec.DRADriver.Image)
	assert.Equal(t, "v25.3.0", got.Spec.DRADriver.Version)
	assert.Equal(t, map[string]bool{"ComputeDomains": true}, got.Spec.DRADriver.FeatureGates)
}

func TestGPUClustersListDecodesItems(t *testing.T) {
	// The CRD restricts GPUCluster to a singleton name, but no CEL validation
	// runs at this layer, so a multi-item list still exercises the decoder.
	want := gpuClusterList(gpuCluster(gpuClusterSingletonName), gpuCluster("gpu-cluster-legacy"))
	want.ResourceVersion = "512"
	want.Continue = "next-token"

	_, client := gpuClusterNewServer(t, func(w http.ResponseWriter, _ *http.Request) {
		writeJSONResponse(w, http.StatusOK, want)
	})

	got, err := client.List(t.Context(), metav1.ListOptions{})
	require.NoError(t, err)

	require.Len(t, got.Items, 2)
	assert.Equal(t, gpuClusterSingletonName, got.Items[0].Name)
	assert.Equal(t, "gpu-cluster-legacy", got.Items[1].Name)
	assert.Equal(t, "v25.3.0", got.Items[0].Spec.DRADriver.Version)
	assert.Equal(t, "512", got.ResourceVersion)
	assert.Equal(t, "next-token", got.Continue)
}

func TestGPUClustersCreateSendsSpec(t *testing.T) {
	in := gpuCluster(gpuClusterSingletonName)

	ts, client := gpuClusterNewServer(t, func(w http.ResponseWriter, _ *http.Request) {
		writeJSONResponse(w, http.StatusCreated, in)
	})

	got, err := client.Create(t.Context(), in, metav1.CreateOptions{})
	require.NoError(t, err)

	// The spec must round-trip through the request body.
	var sent nvidiav1alpha1.GPUCluster
	require.NoError(t, json.Unmarshal(ts.lastRequest(t).Body, &sent))
	assert.Equal(t, gpuClusterSingletonName, sent.Name)
	assert.Equal(t, in.Spec.DRADriver.Repository, sent.Spec.DRADriver.Repository)
	assert.Equal(t, in.Spec.DRADriver.Image, sent.Spec.DRADriver.Image)
	assert.Equal(t, in.Spec.DRADriver.Version, sent.Spec.DRADriver.Version)
	assert.Equal(t, map[string]bool{"ComputeDomains": true}, sent.Spec.DRADriver.FeatureGates)
	assert.Equal(t, map[string]string{"app": "gpu-cluster"}, sent.Labels)

	assert.Equal(t, gpuClusterSingletonName, got.Name)
}

func TestGPUClustersUpdateSendsSpec(t *testing.T) {
	in := gpuCluster(gpuClusterSingletonName)
	in.Spec.DRADriver.Version = "v25.8.0"

	ts, client := gpuClusterNewServer(t, func(w http.ResponseWriter, _ *http.Request) {
		writeJSONResponse(w, http.StatusOK, in)
	})

	got, err := client.Update(t.Context(), in, metav1.UpdateOptions{})
	require.NoError(t, err)

	req := ts.lastRequest(t)
	assert.Equal(t, gpuClusterNamedPath(gpuClusterSingletonName), req.Path)
	assert.NotContains(t, req.Path, "/status")

	var sent nvidiav1alpha1.GPUCluster
	require.NoError(t, json.Unmarshal(req.Body, &sent))
	assert.Equal(t, "v25.8.0", sent.Spec.DRADriver.Version)

	assert.Equal(t, "v25.8.0", got.Spec.DRADriver.Version)
}

func TestGPUClustersUpdateStatusSendsStatus(t *testing.T) {
	in := gpuCluster(gpuClusterSingletonName)
	in.Status.State = nvidiav1alpha1.Ready
	in.Status.Namespace = "gpu-operator"

	ts, client := gpuClusterNewServer(t, func(w http.ResponseWriter, _ *http.Request) {
		writeJSONResponse(w, http.StatusOK, in)
	})

	got, err := client.UpdateStatus(t.Context(), in, metav1.UpdateOptions{})
	require.NoError(t, err)

	req := ts.lastRequest(t)
	assert.Equal(t, gpuClusterNamedPath(gpuClusterSingletonName, "status"), req.Path)
	assert.True(t, strings.HasSuffix(req.Path, "/status"), "UpdateStatus must target the status subresource")

	var sent nvidiav1alpha1.GPUCluster
	require.NoError(t, json.Unmarshal(req.Body, &sent))
	assert.Equal(t, nvidiav1alpha1.Ready, sent.Status.State)
	assert.Equal(t, "gpu-operator", sent.Status.Namespace)

	assert.Equal(t, nvidiav1alpha1.Ready, got.Status.State)
	assert.Equal(t, "gpu-operator", got.Status.Namespace)
}

func TestGPUClustersPatchDecodesSpec(t *testing.T) {
	result := gpuCluster(gpuClusterSingletonName)
	result.Spec.DRADriver.Version = "v25.8.0"

	ts, client := gpuClusterNewServer(t, func(w http.ResponseWriter, _ *http.Request) {
		writeJSONResponse(w, http.StatusOK, result)
	})

	patch := []byte(`{"spec":{"draDriver":{"version":"v25.8.0"}}}`)
	got, err := client.Patch(t.Context(), gpuClusterSingletonName, types.MergePatchType, patch, metav1.PatchOptions{})
	require.NoError(t, err)

	assert.Equal(t, patch, ts.lastRequest(t).Body)
	assert.Equal(t, "v25.8.0", got.Spec.DRADriver.Version)
}

func TestGPUClustersWatchDecodesObject(t *testing.T) {
	modified := gpuCluster(gpuClusterSingletonName)
	modified.Status.State = nvidiav1alpha1.NotReady
	// The frame is marshalled here, on the test goroutine, so the handler below
	// carries no assertions.
	frame := marshalWatchFrame(t, watch.Modified, modified)

	_, client := gpuClusterNewServer(t, func(w http.ResponseWriter, _ *http.Request) {
		writeWatchFrames(w, frame)
	})

	watcher, err := client.Watch(t.Context(), metav1.ListOptions{})
	require.NoError(t, err)
	defer watcher.Stop()

	event := receiveEvent(t, watcher.ResultChan())
	assert.Equal(t, watch.Modified, event.Type)
	obj, ok := event.Object.(*nvidiav1alpha1.GPUCluster)
	require.True(t, ok, "expected a *GPUCluster, got %T", event.Object)
	assert.Equal(t, gpuClusterSingletonName, obj.Name)
	assert.Equal(t, nvidiav1alpha1.NotReady, obj.Status.State)
	assert.Equal(t, "v25.3.0", obj.Spec.DRADriver.Version)
}
