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

// nvdResource is the plural resource name passed to newNVIDIADrivers.
const nvdResource = "nvidiadrivers"

func nvdCollectionPath() string {
	return collectionPath(nvdResource)
}

func nvdNamedPath(name string, subresources ...string) string {
	return namedPath(nvdResource, name, subresources...)
}

// nvdNewServer stands up a recording API server and returns the typed
// NVIDIADriver client wired to it.
func nvdNewServer(t *testing.T, handler http.HandlerFunc) (*recordingServer, NVIDIADriverInterface) {
	t.Helper()

	ts, client := newRecordingServer(t, handler)
	return ts, client.NVIDIADrivers()
}

// nvdDriver builds a fully typed NVIDIADriver, including the TypeMeta the
// client-side decoder needs to recognize the payload.
func nvdDriver(name string) *nvidiav1alpha1.NVIDIADriver {
	gv := nvidiav1alpha1.SchemeGroupVersion
	return &nvidiav1alpha1.NVIDIADriver{
		TypeMeta: metav1.TypeMeta{
			APIVersion: gv.String(),
			Kind:       "NVIDIADriver",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:            name,
			ResourceVersion: "42",
			Labels:          map[string]string{"app": "nvidia-driver"},
		},
		Spec: nvidiav1alpha1.NVIDIADriverSpec{
			Default:    true,
			DriverType: nvidiav1alpha1.GPU,
			Image:      "nvcr.io/nvidia/driver",
		},
	}
}

// nvdList builds a decodable list payload holding items.
func nvdList(items ...*nvidiav1alpha1.NVIDIADriver) *nvidiav1alpha1.NVIDIADriverList {
	gv := nvidiav1alpha1.SchemeGroupVersion
	list := &nvidiav1alpha1.NVIDIADriverList{
		TypeMeta: metav1.TypeMeta{APIVersion: gv.String(), Kind: "NVIDIADriverList"},
	}
	for _, item := range items {
		list.Items = append(list.Items, *item)
	}
	return list
}

// TestNVIDIADriverHTTPMatrix runs the shared verb/HTTP plumbing matrix against
// the generated NVIDIADriver client. Everything that depends on the
// NVIDIADriver schema lives in the focused tests below.
func TestNVIDIADriverHTTPMatrix(t *testing.T) {
	matrix := resourceMatrix[*nvidiav1alpha1.NVIDIADriver, *nvidiav1alpha1.NVIDIADriverList]{
		resource:      nvdResource,
		labelSelector: "app=nvidia-driver",
		fieldSelector: "metadata.name=driver-a",

		newObject: nvdDriver,
		newEmpty:  func() *nvidiav1alpha1.NVIDIADriver { return &nvidiav1alpha1.NVIDIADriver{} },
		newList:   nvdList,
		listItems: func(list *nvidiav1alpha1.NVIDIADriverList) []*nvidiav1alpha1.NVIDIADriver {
			items := make([]*nvidiav1alpha1.NVIDIADriver, 0, len(list.Items))
			for i := range list.Items {
				items = append(items, &list.Items[i])
			}
			return items
		},

		get: func(ctx context.Context, c *NvidiaV1alpha1Client, name string, opts metav1.GetOptions) (*nvidiav1alpha1.NVIDIADriver, error) {
			return c.NVIDIADrivers().Get(ctx, name, opts)
		},
		list: func(ctx context.Context, c *NvidiaV1alpha1Client, opts metav1.ListOptions) (*nvidiav1alpha1.NVIDIADriverList, error) {
			return c.NVIDIADrivers().List(ctx, opts)
		},
		create: func(ctx context.Context, c *NvidiaV1alpha1Client, obj *nvidiav1alpha1.NVIDIADriver, opts metav1.CreateOptions) (*nvidiav1alpha1.NVIDIADriver, error) {
			return c.NVIDIADrivers().Create(ctx, obj, opts)
		},
		update: func(ctx context.Context, c *NvidiaV1alpha1Client, obj *nvidiav1alpha1.NVIDIADriver, opts metav1.UpdateOptions) (*nvidiav1alpha1.NVIDIADriver, error) {
			return c.NVIDIADrivers().Update(ctx, obj, opts)
		},
		updateStatus: func(ctx context.Context, c *NvidiaV1alpha1Client, obj *nvidiav1alpha1.NVIDIADriver, opts metav1.UpdateOptions) (*nvidiav1alpha1.NVIDIADriver, error) {
			return c.NVIDIADrivers().UpdateStatus(ctx, obj, opts)
		},
		remove: func(ctx context.Context, c *NvidiaV1alpha1Client, name string, opts metav1.DeleteOptions) error {
			return c.NVIDIADrivers().Delete(ctx, name, opts)
		},
		removeCollection: func(ctx context.Context, c *NvidiaV1alpha1Client, opts metav1.DeleteOptions, listOpts metav1.ListOptions) error {
			return c.NVIDIADrivers().DeleteCollection(ctx, opts, listOpts)
		},
		patch: func(ctx context.Context, c *NvidiaV1alpha1Client, name string, pt types.PatchType, data []byte, opts metav1.PatchOptions, subresources ...string) (*nvidiav1alpha1.NVIDIADriver, error) {
			return c.NVIDIADrivers().Patch(ctx, name, pt, data, opts, subresources...)
		},
		watch: func(ctx context.Context, c *NvidiaV1alpha1Client, opts metav1.ListOptions) (watch.Interface, error) {
			return c.NVIDIADrivers().Watch(ctx, opts)
		},
	}

	matrix.run(t)
}

func TestNVIDIADriversRequestPaths(t *testing.T) {
	// Spelled out literally: the shared matrix derives its expectations from
	// the same helpers the client uses, so this pins the actual URLs.
	assert.Equal(t, "/apis/nvidia.com/v1alpha1/nvidiadrivers", nvdCollectionPath())
	assert.Equal(t, "/apis/nvidia.com/v1alpha1/nvidiadrivers/gpu-driver", nvdNamedPath("gpu-driver"))
	assert.Equal(t, "/apis/nvidia.com/v1alpha1/nvidiadrivers/gpu-driver/status", nvdNamedPath("gpu-driver", "status"))

	ts, client := nvdNewServer(t, func(w http.ResponseWriter, _ *http.Request) {
		writeJSONResponse(w, http.StatusOK, nvdDriver("gpu-driver"))
	})

	_, err := client.Get(t.Context(), "gpu-driver", metav1.GetOptions{})
	require.NoError(t, err)

	req := ts.lastRequest(t)
	assert.Equal(t, "/apis/nvidia.com/v1alpha1/nvidiadrivers/gpu-driver", req.Path)
	assertClusterScoped(t, req.Path)
}

func TestNVIDIADriversGroupVersionKind(t *testing.T) {
	gvk := nvdDriver("gpu-driver").GroupVersionKind()
	assert.Equal(t, "nvidia.com", gvk.Group)
	assert.Equal(t, "v1alpha1", gvk.Version)
	assert.Equal(t, "NVIDIADriver", gvk.Kind)
	assert.Equal(t, "NVIDIADriverList", nvdList().GroupVersionKind().Kind)
}

func TestNVIDIADriversGetDecodesSpec(t *testing.T) {
	want := nvdDriver("gpu-driver")

	_, client := nvdNewServer(t, func(w http.ResponseWriter, _ *http.Request) {
		writeJSONResponse(w, http.StatusOK, want)
	})

	got, err := client.Get(t.Context(), "gpu-driver", metav1.GetOptions{})
	require.NoError(t, err)

	require.NotNil(t, got)
	assert.Equal(t, "gpu-driver", got.Name)
	assert.Equal(t, "42", got.ResourceVersion)
	assert.True(t, got.Spec.Default)
	assert.Equal(t, nvidiav1alpha1.GPU, got.Spec.DriverType)
	assert.Equal(t, "nvcr.io/nvidia/driver", got.Spec.Image)
}

func TestNVIDIADriversListDecodesItems(t *testing.T) {
	want := nvdList(nvdDriver("driver-a"), nvdDriver("driver-b"))
	want.ResourceVersion = "99"
	want.Continue = "next-token"

	_, client := nvdNewServer(t, func(w http.ResponseWriter, _ *http.Request) {
		writeJSONResponse(w, http.StatusOK, want)
	})

	got, err := client.List(t.Context(), metav1.ListOptions{})
	require.NoError(t, err)

	require.Len(t, got.Items, 2)
	assert.Equal(t, "driver-a", got.Items[0].Name)
	assert.Equal(t, "driver-b", got.Items[1].Name)
	assert.Equal(t, "nvcr.io/nvidia/driver", got.Items[0].Spec.Image)
	assert.Equal(t, "99", got.ResourceVersion)
	assert.Equal(t, "next-token", got.Continue)
}

func TestNVIDIADriversCreateSendsSpec(t *testing.T) {
	in := nvdDriver("new-driver")

	ts, client := nvdNewServer(t, func(w http.ResponseWriter, _ *http.Request) {
		writeJSONResponse(w, http.StatusCreated, in)
	})

	got, err := client.Create(t.Context(), in, metav1.CreateOptions{})
	require.NoError(t, err)

	// The spec must round-trip through the request body.
	var sent nvidiav1alpha1.NVIDIADriver
	require.NoError(t, json.Unmarshal(ts.lastRequest(t).Body, &sent))
	assert.Equal(t, "new-driver", sent.Name)
	assert.Equal(t, in.Spec.Image, sent.Spec.Image)
	assert.Equal(t, in.Spec.DriverType, sent.Spec.DriverType)
	assert.True(t, sent.Spec.Default)
	assert.Equal(t, map[string]string{"app": "nvidia-driver"}, sent.Labels)

	assert.Equal(t, "new-driver", got.Name)
}

func TestNVIDIADriversUpdateSendsSpec(t *testing.T) {
	in := nvdDriver("existing-driver")
	in.Spec.Image = "nvcr.io/nvidia/driver-updated"

	ts, client := nvdNewServer(t, func(w http.ResponseWriter, _ *http.Request) {
		writeJSONResponse(w, http.StatusOK, in)
	})

	got, err := client.Update(t.Context(), in, metav1.UpdateOptions{})
	require.NoError(t, err)

	req := ts.lastRequest(t)
	assert.Equal(t, nvdNamedPath("existing-driver"), req.Path)
	assert.NotContains(t, req.Path, "/status")

	var sent nvidiav1alpha1.NVIDIADriver
	require.NoError(t, json.Unmarshal(req.Body, &sent))
	assert.Equal(t, "nvcr.io/nvidia/driver-updated", sent.Spec.Image)

	assert.Equal(t, "nvcr.io/nvidia/driver-updated", got.Spec.Image)
}

func TestNVIDIADriversUpdateStatusSendsStatus(t *testing.T) {
	in := nvdDriver("existing-driver")
	in.Status.State = nvidiav1alpha1.Ready

	ts, client := nvdNewServer(t, func(w http.ResponseWriter, _ *http.Request) {
		writeJSONResponse(w, http.StatusOK, in)
	})

	got, err := client.UpdateStatus(t.Context(), in, metav1.UpdateOptions{})
	require.NoError(t, err)

	req := ts.lastRequest(t)
	assert.Equal(t, nvdNamedPath("existing-driver", "status"), req.Path)
	assert.True(t, strings.HasSuffix(req.Path, "/status"), "UpdateStatus must target the status subresource")

	var sent nvidiav1alpha1.NVIDIADriver
	require.NoError(t, json.Unmarshal(req.Body, &sent))
	assert.Equal(t, nvidiav1alpha1.Ready, sent.Status.State)

	assert.Equal(t, nvidiav1alpha1.Ready, got.Status.State)
}

func TestNVIDIADriversPatchDecodesSpec(t *testing.T) {
	result := nvdDriver("patched-driver")
	result.Spec.Image = "nvcr.io/nvidia/driver-patched"

	ts, client := nvdNewServer(t, func(w http.ResponseWriter, _ *http.Request) {
		writeJSONResponse(w, http.StatusOK, result)
	})

	patch := []byte(`{"spec":{"image":"nvcr.io/nvidia/driver-patched"}}`)
	got, err := client.Patch(t.Context(), "patched-driver", types.MergePatchType, patch, metav1.PatchOptions{})
	require.NoError(t, err)

	assert.Equal(t, patch, ts.lastRequest(t).Body)
	assert.Equal(t, "nvcr.io/nvidia/driver-patched", got.Spec.Image)
}

func TestNVIDIADriversWatchDecodesObject(t *testing.T) {
	added := nvdDriver("watched-driver")
	// The frame is marshalled here, on the test goroutine, so the handler below
	// carries no assertions.
	frame := marshalWatchFrame(t, watch.Added, added)

	_, client := nvdNewServer(t, func(w http.ResponseWriter, _ *http.Request) {
		writeWatchFrames(w, frame)
	})

	watcher, err := client.Watch(t.Context(), metav1.ListOptions{})
	require.NoError(t, err)
	defer watcher.Stop()

	event := receiveEvent(t, watcher.ResultChan())
	assert.Equal(t, watch.Added, event.Type)
	obj, ok := event.Object.(*nvidiav1alpha1.NVIDIADriver)
	require.True(t, ok, "expected a *NVIDIADriver, got %T", event.Object)
	assert.Equal(t, "watched-driver", obj.Name)
	assert.Equal(t, "nvcr.io/nvidia/driver", obj.Spec.Image)
}
