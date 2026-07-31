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

package v1

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	types "k8s.io/apimachinery/pkg/types"
	watch "k8s.io/apimachinery/pkg/watch"
	rest "k8s.io/client-go/rest"

	nvidiav1 "github.com/NVIDIA/gpu-operator/api/nvidia/v1"
)

// eventTimeout bounds every blocking operation in this file. The test server is
// in-process, so anything that has not completed within a couple of seconds is a
// regression rather than a slow machine.
const eventTimeout = 2 * time.Second

// receiveEvent reads a single event from ch, failing the test rather than
// blocking forever if the channel stays empty or is closed.
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

// collectionPath is the cluster-scoped collection path derived from the real
// SchemeGroupVersion, e.g. /apis/nvidia.com/v1/clusterpolicies.
func collectionPath() string {
	gv := nvidiav1.SchemeGroupVersion
	return fmt.Sprintf("/apis/%s/%s/clusterpolicies", gv.Group, gv.Version)
}

func namedPath(name string) string {
	return collectionPath() + "/" + name
}

// capturedRequest is a snapshot of a request as it arrived at the test server.
type capturedRequest struct {
	method      string
	path        string
	query       url.Values
	contentType string
	accept      string
	userAgent   string
	body        []byte
}

// recorder is an http.Handler that records every request it serves and then
// delegates to the test supplied handler.
type recorder struct {
	mu       sync.Mutex
	requests []capturedRequest
	handler  http.HandlerFunc
}

func (rec *recorder) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	body, _ := io.ReadAll(r.Body)
	// Hand the handler an intact body: the recorder has already drained it.
	r.Body = io.NopCloser(bytes.NewReader(body))

	rec.mu.Lock()
	rec.requests = append(rec.requests, capturedRequest{
		method:      r.Method,
		path:        r.URL.Path,
		query:       r.URL.Query(),
		contentType: r.Header.Get("Content-Type"),
		accept:      r.Header.Get("Accept"),
		userAgent:   r.Header.Get("User-Agent"),
		body:        body,
	})
	rec.mu.Unlock()

	rec.handler(w, r)
}

// only returns the single request the server is expected to have received.
func (rec *recorder) only(t *testing.T) capturedRequest {
	t.Helper()
	rec.mu.Lock()
	defer rec.mu.Unlock()
	require.Len(t, rec.requests, 1, "expected exactly one request to the test server")
	return rec.requests[0]
}

// newTestClient stands up a real HTTP server and points a generated client at it.
func newTestClient(t *testing.T, handler http.HandlerFunc) (ClusterPolicyInterface, *recorder) {
	t.Helper()

	rec := &recorder{handler: handler}
	srv := httptest.NewServer(rec)
	t.Cleanup(srv.Close)

	client, err := NewForConfig(&rest.Config{Host: srv.URL})
	require.NoError(t, err)

	return client.ClusterPolicies(), rec
}

// writeJSON marshals obj as the response body, as the API server would.
//
// It deliberately takes no *testing.T and asserts nothing: it runs on the
// httptest server's goroutine, and testify's require calls t.FailNow, which
// the testing package only permits from the goroutine running the test. A
// marshal failure is surfaced to the client as a 500 so the assertion fails in
// the test goroutine instead.
func writeJSON(w http.ResponseWriter, code int, obj any) {
	raw, err := json.Marshal(obj)
	if err != nil {
		http.Error(w, "test handler: marshal failed: "+err.Error(), http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	_, _ = w.Write(raw)
}

func newClusterPolicy(name string) *nvidiav1.ClusterPolicy {
	cp := &nvidiav1.ClusterPolicy{
		ObjectMeta: metav1.ObjectMeta{
			Name:   name,
			Labels: map[string]string{"app": "gpu-operator"},
		},
	}
	cp.APIVersion = nvidiav1.SchemeGroupVersion.String()
	cp.Kind = "ClusterPolicy"
	cp.Spec.Operator.RuntimeClass = "nvidia"
	cp.Status.State = nvidiav1.Ready
	return cp
}

func newClusterPolicyList(names ...string) *nvidiav1.ClusterPolicyList {
	list := &nvidiav1.ClusterPolicyList{
		ListMeta: metav1.ListMeta{ResourceVersion: "4242"},
	}
	list.APIVersion = nvidiav1.SchemeGroupVersion.String()
	list.Kind = "ClusterPolicyList"
	for _, name := range names {
		list.Items = append(list.Items, *newClusterPolicy(name))
	}
	return list
}

// assertClusterScoped guards the empty-namespace wiring in newClusterPolicies.
func assertClusterScoped(t *testing.T, path string) {
	t.Helper()
	assert.NotContains(t, path, "/namespaces/", "ClusterPolicy is cluster scoped")
	assert.True(t, strings.HasPrefix(path, collectionPath()), "unexpected path %q", path)
}

func TestClusterPoliciesGet(t *testing.T) {
	t.Run("decodes the returned object", func(t *testing.T) {
		want := newClusterPolicy("cluster-policy")
		client, rec := newTestClient(t, func(w http.ResponseWriter, _ *http.Request) {
			writeJSON(w, http.StatusOK, want)
		})

		ctx, cancel := context.WithTimeout(t.Context(), eventTimeout)
		defer cancel()

		got, err := client.Get(ctx, "cluster-policy", metav1.GetOptions{ResourceVersion: "0"})
		require.NoError(t, err)
		require.NotNil(t, got)
		assert.Equal(t, "cluster-policy", got.Name)
		assert.Equal(t, "nvidia", got.Spec.Operator.RuntimeClass)
		assert.Equal(t, nvidiav1.Ready, got.Status.State)

		req := rec.only(t)
		assert.Equal(t, http.MethodGet, req.method)
		assert.Equal(t, namedPath("cluster-policy"), req.path)
		assertClusterScoped(t, req.path)
		assert.Equal(t, "0", req.query.Get("resourceVersion"))
		assert.NotEmpty(t, req.userAgent)
		assert.Contains(t, req.accept, "application/json", "negotiated serializer must ask for JSON")
	})

	t.Run("maps a 404 status body to IsNotFound", func(t *testing.T) {
		client, rec := newTestClient(t, func(w http.ResponseWriter, _ *http.Request) {
			writeJSON(w, http.StatusNotFound, &metav1.Status{
				TypeMeta: metav1.TypeMeta{APIVersion: "v1", Kind: "Status"},
				Status:   metav1.StatusFailure,
				Code:     http.StatusNotFound,
				Reason:   metav1.StatusReasonNotFound,
				Message:  `clusterpolicies.nvidia.com "missing" not found`,
				Details: &metav1.StatusDetails{
					Group: nvidiav1.SchemeGroupVersion.Group,
					Kind:  "clusterpolicies",
					Name:  "missing",
				},
			})
		})

		ctx, cancel := context.WithTimeout(t.Context(), eventTimeout)
		defer cancel()

		got, err := client.Get(ctx, "missing", metav1.GetOptions{})
		require.Error(t, err)
		assert.True(t, apierrors.IsNotFound(err), "expected NotFound, got %v", err)
		// The generated client returns a non-nil zero value alongside the error.
		require.NotNil(t, got)
		assert.Empty(t, got.Name)

		assert.Equal(t, namedPath("missing"), rec.only(t).path)
	})
}

func TestClusterPoliciesList(t *testing.T) {
	tests := map[string]struct {
		opts      metav1.ListOptions
		wantQuery map[string]string
	}{
		"no options": {
			opts:      metav1.ListOptions{},
			wantQuery: map[string]string{},
		},
		"selectors, resourceVersion and limit": {
			opts: metav1.ListOptions{
				LabelSelector:   "app=gpu-operator",
				FieldSelector:   "metadata.name=cluster-policy",
				ResourceVersion: "1234",
				Limit:           50,
			},
			wantQuery: map[string]string{
				"labelSelector":   "app=gpu-operator",
				"fieldSelector":   "metadata.name=cluster-policy",
				"resourceVersion": "1234",
				"limit":           "50",
			},
		},
		"timeout is propagated": {
			opts: metav1.ListOptions{TimeoutSeconds: ptr(int64(7))},
			wantQuery: map[string]string{
				"timeoutSeconds": "7",
				"timeout":        "7s",
			},
		},
	}

	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			want := newClusterPolicyList("policy-a", "policy-b")
			client, rec := newTestClient(t, func(w http.ResponseWriter, _ *http.Request) {
				writeJSON(w, http.StatusOK, want)
			})

			ctx, cancel := context.WithTimeout(t.Context(), eventTimeout)
			defer cancel()

			got, err := client.List(ctx, tc.opts)
			require.NoError(t, err)
			require.NotNil(t, got)
			require.Len(t, got.Items, 2)
			assert.Equal(t, "policy-a", got.Items[0].Name)
			assert.Equal(t, "policy-b", got.Items[1].Name)
			assert.Equal(t, "4242", got.ResourceVersion)

			req := rec.only(t)
			assert.Equal(t, http.MethodGet, req.method)
			assert.Equal(t, collectionPath(), req.path)
			assertClusterScoped(t, req.path)
			for k, v := range tc.wantQuery {
				assert.Equal(t, v, req.query.Get(k), "query param %q", k)
			}
			if len(tc.wantQuery) == 0 {
				assert.Empty(t, req.query, "expected no query params")
			}
		})
	}
}

func TestClusterPoliciesCreate(t *testing.T) {
	send := newClusterPolicy("new-policy")

	// The response is built here, on the test goroutine, rather than by
	// echoing the request inside the handler. The request body is asserted
	// below from the recorder, so the handler needs no test state at all.
	created := send.DeepCopy()
	created.ResourceVersion = "1"

	client, rec := newTestClient(t, func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, http.StatusCreated, created)
	})

	ctx, cancel := context.WithTimeout(t.Context(), eventTimeout)
	defer cancel()

	got, err := client.Create(ctx, send, metav1.CreateOptions{FieldManager: "gpu-operator"})
	require.NoError(t, err)
	require.NotNil(t, got)
	assert.Equal(t, "new-policy", got.Name)
	assert.Equal(t, "1", got.ResourceVersion)

	req := rec.only(t)
	assert.Equal(t, http.MethodPost, req.method)
	assert.Equal(t, collectionPath(), req.path)
	assertClusterScoped(t, req.path)
	assert.Equal(t, "gpu-operator", req.query.Get("fieldManager"))
	assert.Equal(t, "application/json", req.contentType)

	// The object must round-trip through the request body.
	var sent nvidiav1.ClusterPolicy
	require.NoError(t, json.Unmarshal(req.body, &sent))
	assert.Equal(t, "new-policy", sent.Name)
	assert.Equal(t, map[string]string{"app": "gpu-operator"}, sent.Labels)
	assert.Equal(t, "nvidia", sent.Spec.Operator.RuntimeClass)
}

func TestClusterPoliciesUpdate(t *testing.T) {
	tests := map[string]struct {
		call     func(context.Context, ClusterPolicyInterface, *nvidiav1.ClusterPolicy) (*nvidiav1.ClusterPolicy, error)
		wantPath string
	}{
		"Update": {
			call: func(ctx context.Context, c ClusterPolicyInterface, cp *nvidiav1.ClusterPolicy) (*nvidiav1.ClusterPolicy, error) {
				return c.Update(ctx, cp, metav1.UpdateOptions{FieldManager: "gpu-operator"})
			},
			wantPath: namedPath("cluster-policy"),
		},
		"UpdateStatus": {
			call: func(ctx context.Context, c ClusterPolicyInterface, cp *nvidiav1.ClusterPolicy) (*nvidiav1.ClusterPolicy, error) {
				return c.UpdateStatus(ctx, cp, metav1.UpdateOptions{FieldManager: "gpu-operator"})
			},
			wantPath: namedPath("cluster-policy") + "/status",
		},
	}

	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			send := newClusterPolicy("cluster-policy")
			send.ResourceVersion = "9"
			client, rec := newTestClient(t, func(w http.ResponseWriter, _ *http.Request) {
				writeJSON(w, http.StatusOK, send)
			})

			ctx, cancel := context.WithTimeout(t.Context(), eventTimeout)
			defer cancel()

			got, err := tc.call(ctx, client, send)
			require.NoError(t, err)
			require.NotNil(t, got)
			assert.Equal(t, "cluster-policy", got.Name)
			assert.Equal(t, "9", got.ResourceVersion)

			req := rec.only(t)
			assert.Equal(t, http.MethodPut, req.method)
			assert.Equal(t, tc.wantPath, req.path)
			assertClusterScoped(t, req.path)
			assert.Equal(t, "gpu-operator", req.query.Get("fieldManager"))

			var sent nvidiav1.ClusterPolicy
			require.NoError(t, json.Unmarshal(req.body, &sent))
			assert.Equal(t, "cluster-policy", sent.Name)
			assert.Equal(t, nvidiav1.Ready, sent.Status.State)
		})
	}
}

func TestClusterPoliciesDelete(t *testing.T) {
	client, rec := newTestClient(t, func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, http.StatusOK, &metav1.Status{
			TypeMeta: metav1.TypeMeta{APIVersion: "v1", Kind: "Status"},
			Status:   metav1.StatusSuccess,
		})
	})

	ctx, cancel := context.WithTimeout(t.Context(), eventTimeout)
	defer cancel()

	policy := metav1.DeletePropagationForeground
	require.NoError(t, client.Delete(ctx, "cluster-policy", metav1.DeleteOptions{
		PropagationPolicy: &policy,
	}))

	req := rec.only(t)
	assert.Equal(t, http.MethodDelete, req.method)
	assert.Equal(t, namedPath("cluster-policy"), req.path)
	assertClusterScoped(t, req.path)

	var opts metav1.DeleteOptions
	require.NoError(t, json.Unmarshal(req.body, &opts))
	require.NotNil(t, opts.PropagationPolicy)
	assert.Equal(t, metav1.DeletePropagationForeground, *opts.PropagationPolicy)
}

func TestClusterPoliciesDeleteCollection(t *testing.T) {
	client, rec := newTestClient(t, func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, http.StatusOK, &metav1.Status{
			TypeMeta: metav1.TypeMeta{APIVersion: "v1", Kind: "Status"},
			Status:   metav1.StatusSuccess,
		})
	})

	ctx, cancel := context.WithTimeout(t.Context(), eventTimeout)
	defer cancel()

	grace := int64(30)
	require.NoError(t, client.DeleteCollection(ctx,
		metav1.DeleteOptions{GracePeriodSeconds: &grace},
		metav1.ListOptions{LabelSelector: "app=gpu-operator", Limit: 10},
	))

	req := rec.only(t)
	assert.Equal(t, http.MethodDelete, req.method)
	assert.Equal(t, collectionPath(), req.path, "DeleteCollection must target the collection, not a named resource")
	assertClusterScoped(t, req.path)
	assert.Equal(t, "app=gpu-operator", req.query.Get("labelSelector"))
	assert.Equal(t, "10", req.query.Get("limit"))

	var opts metav1.DeleteOptions
	require.NoError(t, json.Unmarshal(req.body, &opts))
	require.NotNil(t, opts.GracePeriodSeconds)
	assert.Equal(t, int64(30), *opts.GracePeriodSeconds)
}

func TestClusterPoliciesPatch(t *testing.T) {
	tests := map[string]struct {
		patchType       types.PatchType
		subresources    []string
		wantPath        string
		wantContentType string
	}{
		"merge patch": {
			patchType:       types.MergePatchType,
			wantPath:        namedPath("cluster-policy"),
			wantContentType: "application/merge-patch+json",
		},
		"json patch": {
			patchType:       types.JSONPatchType,
			wantPath:        namedPath("cluster-policy"),
			wantContentType: "application/json-patch+json",
		},
		"strategic merge patch on the status subresource": {
			patchType:       types.StrategicMergePatchType,
			subresources:    []string{"status"},
			wantPath:        namedPath("cluster-policy") + "/status",
			wantContentType: "application/strategic-merge-patch+json",
		},
	}

	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			patch := []byte(`{"spec":{"operator":{"runtimeClass":"nvidia-crio"}}}`)
			client, rec := newTestClient(t, func(w http.ResponseWriter, _ *http.Request) {
				patched := newClusterPolicy("cluster-policy")
				patched.Spec.Operator.RuntimeClass = "nvidia-crio"
				writeJSON(w, http.StatusOK, patched)
			})

			ctx, cancel := context.WithTimeout(t.Context(), eventTimeout)
			defer cancel()

			got, err := client.Patch(ctx, "cluster-policy", tc.patchType, patch,
				metav1.PatchOptions{FieldManager: "gpu-operator"}, tc.subresources...)
			require.NoError(t, err)
			require.NotNil(t, got)
			assert.Equal(t, "nvidia-crio", got.Spec.Operator.RuntimeClass)

			req := rec.only(t)
			assert.Equal(t, http.MethodPatch, req.method)
			assert.Equal(t, tc.wantPath, req.path)
			assertClusterScoped(t, req.path)
			assert.Equal(t, tc.wantContentType, req.contentType)
			assert.Equal(t, patch, req.body)
			assert.Equal(t, "gpu-operator", req.query.Get("fieldManager"))
		})
	}
}

func TestClusterPoliciesWatch(t *testing.T) {
	want := newClusterPolicy("watched-policy")

	// Marshal the frame on the test goroutine so the handler carries no
	// assertions; require may not be called from the server's goroutine.
	raw, err := json.Marshal(want)
	require.NoError(t, err)
	event, err := json.Marshal(map[string]any{
		"type":   string(watch.Added),
		"object": json.RawMessage(raw),
	})
	require.NoError(t, err)

	client, rec := newTestClient(t, func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write(event)
		w.(http.Flusher).Flush()
		// Returning closes the response body, which terminates the watch.
	})

	ctx, cancel := context.WithTimeout(t.Context(), eventTimeout)
	defer cancel()

	watcher, err := client.Watch(ctx, metav1.ListOptions{
		LabelSelector:   "app=gpu-operator",
		ResourceVersion: "77",
	})
	require.NoError(t, err)
	defer watcher.Stop()

	got := receiveEvent(t, watcher.ResultChan())
	assert.Equal(t, watch.Added, got.Type)
	cp, ok := got.Object.(*nvidiav1.ClusterPolicy)
	require.True(t, ok, "unexpected watch object type %T", got.Object)
	assert.Equal(t, "watched-policy", cp.Name)

	req := rec.only(t)
	assert.Equal(t, http.MethodGet, req.method)
	assert.Equal(t, collectionPath(), req.path)
	assertClusterScoped(t, req.path)
	assert.Equal(t, "true", req.query.Get("watch"))
	assert.Equal(t, "app=gpu-operator", req.query.Get("labelSelector"))
	assert.Equal(t, "77", req.query.Get("resourceVersion"))
}

func TestClusterPoliciesServerError(t *testing.T) {
	client, _ := newTestClient(t, func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, http.StatusInternalServerError, &metav1.Status{
			TypeMeta: metav1.TypeMeta{APIVersion: "v1", Kind: "Status"},
			Status:   metav1.StatusFailure,
			Code:     http.StatusInternalServerError,
			Reason:   metav1.StatusReasonInternalError,
			Message:  "boom",
		})
	})

	ctx, cancel := context.WithTimeout(t.Context(), eventTimeout)
	defer cancel()

	_, err := client.List(ctx, metav1.ListOptions{})
	require.Error(t, err)
	assert.True(t, apierrors.IsInternalError(err), "expected InternalError, got %v", err)
}

func ptr[T any](v T) *T { return &v }
