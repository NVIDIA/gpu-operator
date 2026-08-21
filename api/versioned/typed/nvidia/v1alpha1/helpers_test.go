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
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"path"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	runtime "k8s.io/apimachinery/pkg/runtime"
	types "k8s.io/apimachinery/pkg/types"
	watch "k8s.io/apimachinery/pkg/watch"
	rest "k8s.io/client-go/rest"

	nvidiav1alpha1 "github.com/NVIDIA/gpu-operator/api/nvidia/v1alpha1"
)

// Shared test infrastructure for the typed clients in this package. Both the
// NVIDIADriver and GPUCluster suites drive a real HTTP API server through a
// real REST client, so these helpers stay resource agnostic.

// eventTimeout bounds every watch-channel read in this package. The API server
// stub, the REST client and the test all run in the same process, so a frame
// that is going to arrive arrives in microseconds; a longer bound would only
// make a regression fail slowly.
const eventTimeout = 2 * time.Second

// collectionPath builds the cluster-scoped collection path for a resource,
// derived from the registered group version rather than hardcoded.
func collectionPath(resource string) string {
	gv := nvidiav1alpha1.SchemeGroupVersion
	return path.Join("/apis", gv.Group, gv.Version, resource)
}

// namedPath builds the path for a single named object, optionally below one or
// more subresources.
func namedPath(resource, name string, subresources ...string) string {
	return path.Join(append([]string{collectionPath(resource), name}, subresources...)...)
}

// recordedRequest is a snapshot of a request observed by the test server.
type recordedRequest struct {
	Method string
	Path   string
	Query  url.Values
	Header http.Header
	Body   []byte
}

// recordingServer is an httptest.Server that records every request it serves.
// Access to the recorded requests is mutex guarded because the handler runs on
// the server's goroutine while assertions run on the test's.
type recordingServer struct {
	*httptest.Server

	mu       sync.Mutex
	requests []recordedRequest
}

// newRecordingServer stands up a real HTTP API server running handler, plus a
// typed group client wired to it through NewForConfig.
func newRecordingServer(t *testing.T, handler http.HandlerFunc) (*recordingServer, *NvidiaV1alpha1Client) {
	t.Helper()

	ts := &recordingServer{}
	ts.Server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, err := io.ReadAll(r.Body)
		if err != nil {
			body = nil
		}

		ts.mu.Lock()
		ts.requests = append(ts.requests, recordedRequest{
			Method: r.Method,
			Path:   r.URL.Path,
			Query:  r.URL.Query(),
			Header: r.Header.Clone(),
			Body:   body,
		})
		ts.mu.Unlock()

		handler(w, r)
	}))
	t.Cleanup(ts.Close)

	client, err := NewForConfig(&rest.Config{Host: ts.URL})
	require.NoError(t, err)

	return ts, client
}

// lastRequest returns the single request the server saw, failing if the count
// is not exactly one.
func (ts *recordingServer) lastRequest(t *testing.T) recordedRequest {
	t.Helper()

	ts.mu.Lock()
	defer ts.mu.Unlock()

	require.Len(t, ts.requests, 1, "expected exactly one request to the API server")
	return ts.requests[0]
}

// writeJSONResponse serializes v as the response body with a JSON content type.
//
// It deliberately takes no *testing.T and asserts nothing: it runs on the
// httptest server's goroutine, and testify's require calls t.FailNow, which
// the testing package only permits from the goroutine running the test. A
// marshal failure is surfaced to the client as a 500 so the assertion fails in
// the test goroutine instead.
func writeJSONResponse(w http.ResponseWriter, statusCode int, v interface{}) {
	data, err := json.Marshal(v)
	if err != nil {
		http.Error(w, "test handler: marshal failed: "+err.Error(), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(statusCode)
	_, _ = w.Write(data)
}

// successStatus is the metav1.Status an API server returns for a successful
// delete.
func successStatus() *metav1.Status {
	return &metav1.Status{
		TypeMeta: metav1.TypeMeta{APIVersion: "v1", Kind: "Status"},
		Status:   metav1.StatusSuccess,
	}
}

// notFoundStatus is the metav1.Status an API server returns for a missing
// object, shaped so that errors.IsNotFound recognizes it.
func notFoundStatus(resource, name string) *metav1.Status {
	return &metav1.Status{
		TypeMeta: metav1.TypeMeta{APIVersion: "v1", Kind: "Status"},
		Status:   metav1.StatusFailure,
		Code:     http.StatusNotFound,
		Reason:   metav1.StatusReasonNotFound,
		Message:  resource + "." + nvidiav1alpha1.SchemeGroupVersion.Group + ` "` + name + `" not found`,
		Details: &metav1.StatusDetails{
			Name:  name,
			Group: nvidiav1alpha1.SchemeGroupVersion.Group,
			Kind:  resource,
		},
	}
}

// writeEmptyWatchStream returns an immediately-terminated watch stream.
func writeEmptyWatchStream(w http.ResponseWriter) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	if flusher, ok := w.(http.Flusher); ok {
		flusher.Flush()
	}
}

// writeWatchFrames writes already-marshalled watch frames. The frames must be
// marshalled on the test goroutine: this runs on the server's goroutine, where
// no assertion helper may be called.
func writeWatchFrames(w http.ResponseWriter, frames []byte) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(frames)
	if flusher, ok := w.(http.Flusher); ok {
		flusher.Flush()
	}
}

// marshalWatchFrame encodes obj as a single watch frame of the given type. It
// runs on the test goroutine so the server handler stays assertion free.
func marshalWatchFrame(t *testing.T, eventType watch.EventType, obj interface{}) []byte {
	t.Helper()

	raw, err := json.Marshal(obj)
	require.NoError(t, err)

	frame, err := json.Marshal(metav1.WatchEvent{
		Type:   string(eventType),
		Object: runtime.RawExtension{Raw: raw},
	})
	require.NoError(t, err)

	return frame
}

// receiveEvent is the bounded read every watch assertion uses instead of an
// unbounded receive.
func receiveEvent(t *testing.T, ch <-chan watch.Event) watch.Event {
	t.Helper()
	timer := time.NewTimer(eventTimeout)
	defer timer.Stop()
	select {
	case event, ok := <-ch:
		require.True(t, ok, "watch channel closed unexpectedly")
		return event
	case <-timer.C:
		t.Fatal("timed out waiting for watch event")
		return watch.Event{}
	}
}

// requireWatchClosed is the mirror of receiveEvent for streams that must end
// rather than deliver: same bound, opposite expectation.
func requireWatchClosed(t *testing.T, ch <-chan watch.Event) {
	t.Helper()
	timer := time.NewTimer(eventTimeout)
	defer timer.Stop()
	select {
	case event, ok := <-ch:
		assert.False(t, ok, "expected the result channel to be closed, got event %v", event)
	case <-timer.C:
		t.Fatal("timed out waiting for the watch stream to close")
	}
}

// assertClusterScoped guards the empty namespace passed to the generated
// constructors: cluster-scoped resources never carry a /namespaces/ segment.
func assertClusterScoped(t *testing.T, requestPath string) {
	t.Helper()
	assert.NotContains(t, requestPath, "/namespaces/", "resource is cluster scoped")
}

// assertGroupVersionPrefix checks the request targets this group version's
// collection.
func assertGroupVersionPrefix(t *testing.T, requestPath, resource string) {
	t.Helper()
	base := collectionPath(resource)
	assert.True(t,
		requestPath == base || strings.HasPrefix(requestPath, base+"/"),
		"unexpected path %q for resource %q", requestPath, resource,
	)
}

// -----------------------------------------------------------------------------
// Shared verb/HTTP matrix
// -----------------------------------------------------------------------------

// Every typed client in this package is a thin wrapper around the same
// gentype.ClientWithList, so the HTTP plumbing under each verb (method, path,
// query parameters, content type, request body, cluster scope) is identical
// across resources. resourceMatrix drives that plumbing once per resource;
// anything that depends on a resource's own schema stays in a focused test in
// that resource's file.

// matrixObject is the constraint for the typed object a resource client
// returns: the generated types are runtime.Objects with an ObjectMeta.
type matrixObject interface {
	metav1.Object
	runtime.Object
}

// matrixList is the constraint for the typed list a resource client returns.
type matrixList interface {
	metav1.ListInterface
	runtime.Object
}

// resourceMatrix describes the one generated typed client under test. Only the
// fields here differ between resources; everything else is shared.
type resourceMatrix[T matrixObject, L matrixList] struct {
	// resource is the plural resource name the generated constructor passes to
	// gentype, and therefore the last path segment of the collection URL.
	resource string

	// labelSelector and fieldSelector are realistic selectors for this
	// resource; their only job is to prove option propagation.
	labelSelector string
	fieldSelector string

	// newObject builds a fully populated fixture, including the TypeMeta the
	// client-side decoder needs. newEmpty allocates a zero-valued object for
	// decoding recorded request bodies. newList builds a decodable list.
	newObject func(name string) T
	newEmpty  func() T
	newList   func(items ...T) L
	listItems func(list L) []T

	// One field per verb under test, bound to the resource's typed methods.
	get              func(ctx context.Context, c *NvidiaV1alpha1Client, name string, opts metav1.GetOptions) (T, error)
	list             func(ctx context.Context, c *NvidiaV1alpha1Client, opts metav1.ListOptions) (L, error)
	create           func(ctx context.Context, c *NvidiaV1alpha1Client, obj T, opts metav1.CreateOptions) (T, error)
	update           func(ctx context.Context, c *NvidiaV1alpha1Client, obj T, opts metav1.UpdateOptions) (T, error)
	updateStatus     func(ctx context.Context, c *NvidiaV1alpha1Client, obj T, opts metav1.UpdateOptions) (T, error)
	remove           func(ctx context.Context, c *NvidiaV1alpha1Client, name string, opts metav1.DeleteOptions) error
	removeCollection func(ctx context.Context, c *NvidiaV1alpha1Client, opts metav1.DeleteOptions, listOpts metav1.ListOptions) error
	patch            func(ctx context.Context, c *NvidiaV1alpha1Client, name string, pt types.PatchType, data []byte, opts metav1.PatchOptions, subresources ...string) (T, error)
	watch            func(ctx context.Context, c *NvidiaV1alpha1Client, opts metav1.ListOptions) (watch.Interface, error)
}

func (m resourceMatrix[T, L]) collection() string {
	return collectionPath(m.resource)
}

func (m resourceMatrix[T, L]) named(name string, subresources ...string) string {
	return namedPath(m.resource, name, subresources...)
}

// decodeBody decodes a recorded request body into a freshly allocated object.
func (m resourceMatrix[T, L]) decodeBody(t *testing.T, body []byte) T {
	t.Helper()

	obj := m.newEmpty()
	require.NoError(t, json.Unmarshal(body, obj))
	return obj
}

// run executes the whole matrix. Subtest names carry the resource so a failure
// names the resource that broke.
func (m resourceMatrix[T, L]) run(t *testing.T) {
	t.Helper()

	for _, tc := range []struct {
		verb string
		fn   func(*testing.T)
	}{
		{"get", m.testGet},
		{"get_propagates_resource_version", m.testGetPropagatesResourceVersion},
		{"get_not_found", m.testGetNotFound},
		{"list", m.testList},
		{"list_options_become_query_params", m.testListOptionsBecomeQueryParams},
		{"list_timeout_seconds", m.testListTimeoutSeconds},
		{"create", m.testCreate},
		{"update", m.testUpdate},
		{"update_status", m.testUpdateStatus},
		{"delete", m.testDelete},
		{"delete_error", m.testDeleteError},
		{"delete_collection", m.testDeleteCollection},
		{"patch", m.testPatch},
		{"watch", m.testWatch},
		{"watch_delivers_events", m.testWatchDeliversEvents},
		{"no_namespace_segment_anywhere", m.testNoNamespaceSegmentAnywhere},
	} {
		t.Run(m.resource+"/"+tc.verb, tc.fn)
	}
}

func (m resourceMatrix[T, L]) testGet(t *testing.T) {
	want := m.newObject("fixture-one")

	ts, client := newRecordingServer(t, func(w http.ResponseWriter, _ *http.Request) {
		writeJSONResponse(w, http.StatusOK, want)
	})

	got, err := m.get(t.Context(), client, "fixture-one", metav1.GetOptions{})
	require.NoError(t, err)

	req := ts.lastRequest(t)
	assert.Equal(t, http.MethodGet, req.Method)
	assert.Equal(t, m.named("fixture-one"), req.Path)
	assertClusterScoped(t, req.Path)
	assertGroupVersionPrefix(t, req.Path, m.resource)

	require.NotNil(t, got)
	assert.Equal(t, "fixture-one", got.GetName())
	assert.Equal(t, want.GetResourceVersion(), got.GetResourceVersion())
	assert.Equal(t, want.GetLabels(), got.GetLabels())
}

func (m resourceMatrix[T, L]) testGetPropagatesResourceVersion(t *testing.T) {
	ts, client := newRecordingServer(t, func(w http.ResponseWriter, _ *http.Request) {
		writeJSONResponse(w, http.StatusOK, m.newObject("fixture-one"))
	})

	_, err := m.get(t.Context(), client, "fixture-one", metav1.GetOptions{ResourceVersion: "0"})
	require.NoError(t, err)

	assert.Equal(t, "0", ts.lastRequest(t).Query.Get("resourceVersion"))
}

func (m resourceMatrix[T, L]) testGetNotFound(t *testing.T) {
	ts, client := newRecordingServer(t, func(w http.ResponseWriter, _ *http.Request) {
		writeJSONResponse(w, http.StatusNotFound, notFoundStatus(m.resource, "missing"))
	})

	got, err := m.get(t.Context(), client, "missing", metav1.GetOptions{})
	require.Error(t, err)
	assert.True(t, apierrors.IsNotFound(err), "expected a NotFound error, got %v", err)
	// The generated client returns a freshly allocated (empty) object on error.
	require.NotNil(t, got)
	assert.Empty(t, got.GetName())

	assert.Equal(t, m.named("missing"), ts.lastRequest(t).Path)
}

func (m resourceMatrix[T, L]) testList(t *testing.T) {
	want := m.newList(m.newObject("fixture-one"), m.newObject("fixture-two"))
	want.SetResourceVersion("99")
	want.SetContinue("next-token")

	ts, client := newRecordingServer(t, func(w http.ResponseWriter, _ *http.Request) {
		writeJSONResponse(w, http.StatusOK, want)
	})

	got, err := m.list(t.Context(), client, metav1.ListOptions{})
	require.NoError(t, err)

	req := ts.lastRequest(t)
	assert.Equal(t, http.MethodGet, req.Method)
	assert.Equal(t, m.collection(), req.Path)
	assertClusterScoped(t, req.Path)

	items := m.listItems(got)
	require.Len(t, items, 2)
	assert.Equal(t, "fixture-one", items[0].GetName())
	assert.Equal(t, "fixture-two", items[1].GetName())
	assert.Equal(t, "99", got.GetResourceVersion())
	assert.Equal(t, "next-token", got.GetContinue())
}

func (m resourceMatrix[T, L]) testListOptionsBecomeQueryParams(t *testing.T) {
	tests := []struct {
		name  string
		opts  metav1.ListOptions
		query map[string]string
	}{
		{
			name:  "label selector",
			opts:  metav1.ListOptions{LabelSelector: m.labelSelector},
			query: map[string]string{"labelSelector": m.labelSelector},
		},
		{
			name:  "field selector",
			opts:  metav1.ListOptions{FieldSelector: m.fieldSelector},
			query: map[string]string{"fieldSelector": m.fieldSelector},
		},
		{
			name:  "resource version",
			opts:  metav1.ListOptions{ResourceVersion: "1234"},
			query: map[string]string{"resourceVersion": "1234"},
		},
		{
			name:  "limit and continue",
			opts:  metav1.ListOptions{Limit: 50, Continue: "abc"},
			query: map[string]string{"limit": "50", "continue": "abc"},
		},
		{
			name: "all selectors together",
			opts: metav1.ListOptions{
				LabelSelector:   m.labelSelector,
				FieldSelector:   m.fieldSelector,
				ResourceVersion: "7",
				Limit:           10,
			},
			query: map[string]string{
				"labelSelector":   m.labelSelector,
				"fieldSelector":   m.fieldSelector,
				"resourceVersion": "7",
				"limit":           "10",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ts, client := newRecordingServer(t, func(w http.ResponseWriter, _ *http.Request) {
				writeJSONResponse(w, http.StatusOK, m.newList())
			})

			_, err := m.list(t.Context(), client, tt.opts)
			require.NoError(t, err)

			req := ts.lastRequest(t)
			assert.Equal(t, m.collection(), req.Path)
			for key, want := range tt.query {
				assert.Equal(t, want, req.Query.Get(key), "query param %q", key)
			}
		})
	}
}

func (m resourceMatrix[T, L]) testListTimeoutSeconds(t *testing.T) {
	timeoutSeconds := int64(17)

	ts, client := newRecordingServer(t, func(w http.ResponseWriter, _ *http.Request) {
		writeJSONResponse(w, http.StatusOK, m.newList())
	})

	_, err := m.list(t.Context(), client, metav1.ListOptions{TimeoutSeconds: &timeoutSeconds})
	require.NoError(t, err)

	assert.Equal(t, "17", ts.lastRequest(t).Query.Get("timeoutSeconds"))
}

func (m resourceMatrix[T, L]) testCreate(t *testing.T) {
	in := m.newObject("fixture-created")

	ts, client := newRecordingServer(t, func(w http.ResponseWriter, _ *http.Request) {
		writeJSONResponse(w, http.StatusCreated, in)
	})

	got, err := m.create(t.Context(), client, in, metav1.CreateOptions{FieldManager: "gpu-operator"})
	require.NoError(t, err)

	req := ts.lastRequest(t)
	assert.Equal(t, http.MethodPost, req.Method)
	assert.Equal(t, m.collection(), req.Path)
	assertClusterScoped(t, req.Path)
	assert.Equal(t, "gpu-operator", req.Query.Get("fieldManager"))
	assert.Equal(t, "application/json", req.Header.Get("Content-Type"))

	// The object must round-trip through the request body.
	sent := m.decodeBody(t, req.Body)
	assert.Equal(t, "fixture-created", sent.GetName())
	assert.Equal(t, in.GetLabels(), sent.GetLabels())

	assert.Equal(t, "fixture-created", got.GetName())
}

func (m resourceMatrix[T, L]) testUpdate(t *testing.T) {
	in := m.newObject("fixture-existing")

	ts, client := newRecordingServer(t, func(w http.ResponseWriter, _ *http.Request) {
		writeJSONResponse(w, http.StatusOK, in)
	})

	got, err := m.update(t.Context(), client, in, metav1.UpdateOptions{})
	require.NoError(t, err)

	req := ts.lastRequest(t)
	assert.Equal(t, http.MethodPut, req.Method)
	assert.Equal(t, m.named("fixture-existing"), req.Path)
	assert.NotContains(t, req.Path, "/status")
	assertClusterScoped(t, req.Path)

	sent := m.decodeBody(t, req.Body)
	assert.Equal(t, "fixture-existing", sent.GetName())

	assert.Equal(t, "fixture-existing", got.GetName())
}

func (m resourceMatrix[T, L]) testUpdateStatus(t *testing.T) {
	in := m.newObject("fixture-existing")

	ts, client := newRecordingServer(t, func(w http.ResponseWriter, _ *http.Request) {
		writeJSONResponse(w, http.StatusOK, in)
	})

	got, err := m.updateStatus(t.Context(), client, in, metav1.UpdateOptions{})
	require.NoError(t, err)

	req := ts.lastRequest(t)
	assert.Equal(t, http.MethodPut, req.Method)
	assert.Equal(t, m.named("fixture-existing", "status"), req.Path)
	assert.True(t, strings.HasSuffix(req.Path, "/status"))
	assertClusterScoped(t, req.Path)

	sent := m.decodeBody(t, req.Body)
	assert.Equal(t, "fixture-existing", sent.GetName())

	assert.Equal(t, "fixture-existing", got.GetName())
}

func (m resourceMatrix[T, L]) testDelete(t *testing.T) {
	for _, policy := range []metav1.DeletionPropagation{
		metav1.DeletePropagationForeground,
		metav1.DeletePropagationBackground,
	} {
		t.Run(string(policy), func(t *testing.T) {
			ts, client := newRecordingServer(t, func(w http.ResponseWriter, _ *http.Request) {
				writeJSONResponse(w, http.StatusOK, successStatus())
			})

			err := m.remove(t.Context(), client, "fixture-doomed", metav1.DeleteOptions{PropagationPolicy: &policy})
			require.NoError(t, err)

			req := ts.lastRequest(t)
			assert.Equal(t, http.MethodDelete, req.Method)
			assert.Equal(t, m.named("fixture-doomed"), req.Path)
			assertClusterScoped(t, req.Path)

			var sent metav1.DeleteOptions
			require.NoError(t, json.Unmarshal(req.Body, &sent))
			require.NotNil(t, sent.PropagationPolicy)
			assert.Equal(t, policy, *sent.PropagationPolicy)
		})
	}
}

func (m resourceMatrix[T, L]) testDeleteError(t *testing.T) {
	_, client := newRecordingServer(t, func(w http.ResponseWriter, _ *http.Request) {
		writeJSONResponse(w, http.StatusNotFound, notFoundStatus(m.resource, "missing"))
	})

	err := m.remove(t.Context(), client, "missing", metav1.DeleteOptions{})
	require.Error(t, err)
	assert.True(t, apierrors.IsNotFound(err))
}

func (m resourceMatrix[T, L]) testDeleteCollection(t *testing.T) {
	ts, client := newRecordingServer(t, func(w http.ResponseWriter, _ *http.Request) {
		writeJSONResponse(w, http.StatusOK, successStatus())
	})

	gracePeriod := int64(30)
	err := m.removeCollection(
		t.Context(),
		client,
		metav1.DeleteOptions{GracePeriodSeconds: &gracePeriod},
		metav1.ListOptions{LabelSelector: m.labelSelector, FieldSelector: m.fieldSelector},
	)
	require.NoError(t, err)

	req := ts.lastRequest(t)
	assert.Equal(t, http.MethodDelete, req.Method)
	assert.Equal(t, m.collection(), req.Path)
	assertClusterScoped(t, req.Path)
	assert.Equal(t, m.labelSelector, req.Query.Get("labelSelector"))
	assert.Equal(t, m.fieldSelector, req.Query.Get("fieldSelector"))

	var sent metav1.DeleteOptions
	require.NoError(t, json.Unmarshal(req.Body, &sent))
	require.NotNil(t, sent.GracePeriodSeconds)
	assert.Equal(t, int64(30), *sent.GracePeriodSeconds)
}

func (m resourceMatrix[T, L]) testPatch(t *testing.T) {
	tests := []struct {
		name                string
		patchType           types.PatchType
		expectedContentType string
		subresources        []string
	}{
		{
			name:                "merge patch on the resource",
			patchType:           types.MergePatchType,
			expectedContentType: "application/merge-patch+json",
		},
		{
			name:                "json patch on the resource",
			patchType:           types.JSONPatchType,
			expectedContentType: "application/json-patch+json",
		},
		{
			name:                "strategic merge patch on the resource",
			patchType:           types.StrategicMergePatchType,
			expectedContentType: "application/strategic-merge-patch+json",
		},
		{
			name:                "apply patch on the resource",
			patchType:           types.ApplyPatchType,
			expectedContentType: "application/apply-patch+yaml",
		},
		{
			name:                "merge patch on the status subresource",
			patchType:           types.MergePatchType,
			expectedContentType: "application/merge-patch+json",
			subresources:        []string{"status"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			patch := []byte(`{"metadata":{"labels":{"patched":"true"}}}`)
			result := m.newObject("fixture-patched")

			ts, client := newRecordingServer(t, func(w http.ResponseWriter, _ *http.Request) {
				writeJSONResponse(w, http.StatusOK, result)
			})

			got, err := m.patch(
				t.Context(),
				client,
				"fixture-patched",
				tt.patchType,
				patch,
				metav1.PatchOptions{FieldManager: "gpu-operator"},
				tt.subresources...,
			)
			require.NoError(t, err)

			req := ts.lastRequest(t)
			assert.Equal(t, http.MethodPatch, req.Method)
			assert.Equal(t, m.named("fixture-patched", tt.subresources...), req.Path)
			assertClusterScoped(t, req.Path)
			assert.Equal(t, tt.expectedContentType, req.Header.Get("Content-Type"))
			assert.Equal(t, patch, req.Body)
			assert.Equal(t, "gpu-operator", req.Query.Get("fieldManager"))

			assert.Equal(t, "fixture-patched", got.GetName())
		})
	}
}

func (m resourceMatrix[T, L]) testWatch(t *testing.T) {
	ts, client := newRecordingServer(t, func(w http.ResponseWriter, _ *http.Request) {
		writeEmptyWatchStream(w)
	})

	watcher, err := m.watch(t.Context(), client, metav1.ListOptions{
		LabelSelector:   m.labelSelector,
		ResourceVersion: "1234",
	})
	require.NoError(t, err)
	defer watcher.Stop()

	req := ts.lastRequest(t)
	assert.Equal(t, http.MethodGet, req.Method)
	assert.Equal(t, m.collection(), req.Path)
	assertClusterScoped(t, req.Path)
	assert.Equal(t, "true", req.Query.Get("watch"), "Watch must set watch=true")
	assert.Equal(t, m.labelSelector, req.Query.Get("labelSelector"))
	assert.Equal(t, "1234", req.Query.Get("resourceVersion"))

	// The empty stream must terminate rather than block forever.
	requireWatchClosed(t, watcher.ResultChan())
}

func (m resourceMatrix[T, L]) testWatchDeliversEvents(t *testing.T) {
	added := m.newObject("fixture-watched")
	frame := marshalWatchFrame(t, watch.Added, added)

	_, client := newRecordingServer(t, func(w http.ResponseWriter, _ *http.Request) {
		writeWatchFrames(w, frame)
	})

	watcher, err := m.watch(t.Context(), client, metav1.ListOptions{})
	require.NoError(t, err)
	defer watcher.Stop()

	event := receiveEvent(t, watcher.ResultChan())
	assert.Equal(t, watch.Added, event.Type)
	obj, ok := event.Object.(T)
	require.True(t, ok, "unexpected watch payload type %T", event.Object)
	assert.Equal(t, "fixture-watched", obj.GetName())
}

func (m resourceMatrix[T, L]) testNoNamespaceSegmentAnywhere(t *testing.T) {
	// A single sweep across every verb asserting the cluster-scoped path shape.
	obj := m.newObject("scope-check")

	tests := []struct {
		name string
		call func(t *testing.T, c *NvidiaV1alpha1Client)
	}{
		{"get", func(t *testing.T, c *NvidiaV1alpha1Client) {
			_, err := m.get(t.Context(), c, "scope-check", metav1.GetOptions{})
			require.NoError(t, err)
		}},
		{"create", func(t *testing.T, c *NvidiaV1alpha1Client) {
			_, err := m.create(t.Context(), c, obj, metav1.CreateOptions{})
			require.NoError(t, err)
		}},
		{"update", func(t *testing.T, c *NvidiaV1alpha1Client) {
			_, err := m.update(t.Context(), c, obj, metav1.UpdateOptions{})
			require.NoError(t, err)
		}},
		{"updateStatus", func(t *testing.T, c *NvidiaV1alpha1Client) {
			_, err := m.updateStatus(t.Context(), c, obj, metav1.UpdateOptions{})
			require.NoError(t, err)
		}},
		{"patch", func(t *testing.T, c *NvidiaV1alpha1Client) {
			_, err := m.patch(t.Context(), c, "scope-check", types.MergePatchType, []byte(`{}`), metav1.PatchOptions{})
			require.NoError(t, err)
		}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ts, client := newRecordingServer(t, func(w http.ResponseWriter, _ *http.Request) {
				writeJSONResponse(w, http.StatusOK, obj)
			})

			tt.call(t, client)

			req := ts.lastRequest(t)
			assertClusterScoped(t, req.Path)
			assertGroupVersionPrefix(t, req.Path, m.resource)
		})
	}
}
