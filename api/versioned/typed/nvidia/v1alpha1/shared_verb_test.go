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

// The verb tests every typed client in this package shares, plus the test
// infrastructure they run on. Both the NVIDIADriver and GPUCluster suites drive
// a real HTTP API server through a real REST client, so everything here stays
// resource agnostic.

// watchEventTimeout bounds the watch-channel read. The API server stub, the
// REST client and the test all run in the same process, so a frame that is
// going to arrive arrives in microseconds.
const watchEventTimeout = 2 * time.Second

// missingObjectName is the name the error-path tests ask for. No fixture ever
// serves an object under it.
const missingObjectName = "missing"

// fieldManagerName is the field manager the write-verb tests send.
const fieldManagerName = "gpu-operator"

// resourceCollectionPath builds the cluster-scoped collection path for a
// resource, derived from the registered group version rather than hardcoded.
// Asserting a request path against it therefore also pins the empty-namespace
// wiring in the generated constructors: a namespaced client would insert
// /namespaces/<ns>/.
func resourceCollectionPath(resource string) string {
	groupVersion := nvidiav1alpha1.SchemeGroupVersion
	return path.Join("/apis", groupVersion.Group, groupVersion.Version, resource)
}

// resourceNamedPath builds the path for a single named object, optionally below
// one or more subresources.
func resourceNamedPath(resource, name string, subresources ...string) string {
	return path.Join(append([]string{resourceCollectionPath(resource), name}, subresources...)...)
}

// recordedRequest is a snapshot of a request observed by the test server.
type recordedRequest struct {
	Method string
	Path   string
	Query  url.Values
	Header http.Header
	Body   []byte

	// BodyReadError carries a failure to drain the request body. The handler
	// runs on the server's goroutine, where no assertion helper may be called,
	// so the error is recorded here and raised on the test's goroutine instead.
	BodyReadError error
}

// recordingServer is an httptest.Server that records every request it serves.
// Access to the recorded requests is mutex guarded because the handler runs on
// the server's goroutine while assertions run on the test's.
type recordingServer struct {
	*httptest.Server

	requestsMutex sync.Mutex
	requests      []recordedRequest
}

// newRecordingServerAndClient stands up a real HTTP API server running handler,
// plus a typed group client wired to it through NewForConfig.
func newRecordingServerAndClient(t *testing.T, handler http.HandlerFunc) (*recordingServer, *NvidiaV1alpha1Client) {
	t.Helper()

	server := &recordingServer{}
	server.Server = httptest.NewServer(http.HandlerFunc(func(responseWriter http.ResponseWriter, incomingRequest *http.Request) {
		requestBody, bodyReadError := io.ReadAll(incomingRequest.Body)

		server.requestsMutex.Lock()
		server.requests = append(server.requests, recordedRequest{
			Method:        incomingRequest.Method,
			Path:          incomingRequest.URL.Path,
			Query:         incomingRequest.URL.Query(),
			Header:        incomingRequest.Header.Clone(),
			Body:          requestBody,
			BodyReadError: bodyReadError,
		})
		server.requestsMutex.Unlock()

		handler(responseWriter, incomingRequest)
	}))
	t.Cleanup(server.Close)

	client, err := NewForConfig(&rest.Config{Host: server.URL})
	require.NoError(t, err)

	return server, client
}

// onlyRequest returns the single request the server saw, failing if the count
// is not exactly one.
func (server *recordingServer) onlyRequest(t *testing.T) recordedRequest {
	t.Helper()

	server.requestsMutex.Lock()
	defer server.requestsMutex.Unlock()

	require.Len(t, server.requests, 1, "expected exactly one request to the API server")
	request := server.requests[0]
	require.NoError(t, request.BodyReadError, "failed to read the body of %s %s", request.Method, request.Path)
	return request
}

// writeJSONResponse serializes payload as the response body with a JSON content
// type. It asserts nothing because it runs on the httptest server's goroutine,
// where testify's t.FailNow may not be called; a marshal failure is surfaced to
// the client as a 500 so the assertion fails on the test goroutine instead.
func writeJSONResponse(responseWriter http.ResponseWriter, statusCode int, payload runtime.Object) {
	responseBody, err := json.Marshal(payload)
	if err != nil {
		http.Error(responseWriter, "test handler: marshal failed: "+err.Error(), http.StatusInternalServerError)
		return
	}

	responseWriter.Header().Set("Content-Type", "application/json")
	responseWriter.WriteHeader(statusCode)
	_, _ = responseWriter.Write(responseBody)
}

// successStatus is the metav1.Status an API server returns for a successful
// delete.
func successStatus() *metav1.Status {
	return &metav1.Status{
		TypeMeta: metav1.TypeMeta{APIVersion: "v1", Kind: "Status"},
		Status:   metav1.StatusSuccess,
	}
}

// failureStatus is the metav1.Status an API server returns for a rejected
// request, shaped so that the matching apierrors.Is* predicate recognizes it.
func failureStatus(code int32, reason metav1.StatusReason, message string) *metav1.Status {
	return &metav1.Status{
		TypeMeta: metav1.TypeMeta{APIVersion: "v1", Kind: "Status"},
		Status:   metav1.StatusFailure,
		Code:     code,
		Reason:   reason,
		Message:  message,
	}
}

// notFoundStatus is the metav1.Status an API server returns for a missing
// object, shaped so that errors.IsNotFound recognizes it.
func notFoundStatus(resource, name string) *metav1.Status {
	status := failureStatus(http.StatusNotFound, metav1.StatusReasonNotFound,
		resource+"."+nvidiav1alpha1.SchemeGroupVersion.Group+` "`+name+`" not found`)
	status.Details = &metav1.StatusDetails{
		Name:  name,
		Group: nvidiav1alpha1.SchemeGroupVersion.Group,
		Kind:  resource,
	}
	return status
}

// writeWatchFrames writes already-marshalled watch frames. They must be
// marshalled on the test goroutine: this runs on the server's goroutine, where
// no assertion helper may be called.
func writeWatchFrames(responseWriter http.ResponseWriter, frames []byte) {
	responseWriter.Header().Set("Content-Type", "application/json")
	responseWriter.WriteHeader(http.StatusOK)
	_, _ = responseWriter.Write(frames)
	if flusher, ok := responseWriter.(http.Flusher); ok {
		flusher.Flush()
	}
}

// marshalWatchFrame encodes eventObject as a single watch frame of the given
// type, on the test goroutine so the server handler stays assertion free.
func marshalWatchFrame(t *testing.T, eventType watch.EventType, eventObject runtime.Object) []byte {
	t.Helper()

	serializedObject, err := json.Marshal(eventObject)
	require.NoError(t, err)

	frame, err := json.Marshal(metav1.WatchEvent{
		Type:   string(eventType),
		Object: runtime.RawExtension{Raw: serializedObject},
	})
	require.NoError(t, err)

	return frame
}

// requireWatchEvent is a bounded read, used instead of an unbounded receive.
func requireWatchEvent(t *testing.T, events <-chan watch.Event) watch.Event {
	t.Helper()
	timer := time.NewTimer(watchEventTimeout)
	defer timer.Stop()
	select {
	case event, ok := <-events:
		require.True(t, ok, "watch channel closed unexpectedly")
		return event
	case <-timer.C:
		t.Fatal("timed out waiting for watch event")
		return watch.Event{}
	}
}

// decodeRequestMetadata decodes the metadata of a recorded request body: the
// shared verb tests are resource agnostic, and each resource's own file asserts
// that its spec round-trips.
func decodeRequestMetadata(t *testing.T, requestBody []byte) metav1.PartialObjectMetadata {
	t.Helper()

	var sentMetadata metav1.PartialObjectMetadata
	require.NoError(t, json.Unmarshal(requestBody, &sentMetadata))
	return sentMetadata
}

// assertDeleteOptionsSent decodes a DeleteOptions request body and compares the
// fields that change what the API server does. The apiVersion the REST client
// encodes the body under is a client-go serializer detail and is not asserted.
func assertDeleteOptionsSent(t *testing.T, requestBody []byte, expectedOptions metav1.DeleteOptions) {
	t.Helper()

	var sentDeleteOptions metav1.DeleteOptions
	require.NoError(t, json.Unmarshal(requestBody, &sentDeleteOptions))
	assert.Equal(t, expectedOptions.GracePeriodSeconds, sentDeleteOptions.GracePeriodSeconds)
	assert.Equal(t, expectedOptions.DryRun, sentDeleteOptions.DryRun)
	assert.Equal(t, expectedOptions.PropagationPolicy, sentDeleteOptions.PropagationPolicy)
	assert.Equal(t, expectedOptions.Preconditions, sentDeleteOptions.Preconditions)
}

// typedObject is the constraint for the typed object a resource client
// returns: the generated types are runtime.Objects with an ObjectMeta.
type typedObject interface {
	metav1.Object
	runtime.Object
}

// typedList is the constraint for the typed list a resource client returns.
type typedList interface {
	metav1.ListInterface
	runtime.Object
}

// typedResourceClient is the method set client-gen emits for every resource in
// this group. Both GPUClusterInterface and NVIDIADriverInterface satisfy it, so
// the shared verb tests call the generated methods by name.
type typedResourceClient[T typedObject, L typedList] interface {
	Create(ctx context.Context, object T, createOptions metav1.CreateOptions) (T, error)
	Update(ctx context.Context, object T, updateOptions metav1.UpdateOptions) (T, error)
	UpdateStatus(ctx context.Context, object T, updateOptions metav1.UpdateOptions) (T, error)
	Delete(ctx context.Context, name string, deleteOptions metav1.DeleteOptions) error
	DeleteCollection(ctx context.Context, deleteOptions metav1.DeleteOptions, listOptions metav1.ListOptions) error
	Get(ctx context.Context, name string, getOptions metav1.GetOptions) (T, error)
	List(ctx context.Context, listOptions metav1.ListOptions) (L, error)
	Watch(ctx context.Context, listOptions metav1.ListOptions) (watch.Interface, error)
	Patch(ctx context.Context, name string, patchType types.PatchType, patchData []byte, patchOptions metav1.PatchOptions, subresources ...string) (T, error)
}

// resourceFixture carries the only facts that differ between the resources in
// this group.
type resourceFixture[T typedObject, L typedList, C typedResourceClient[T, L]] struct {
	// resource is the plural resource name the generated constructor passes to
	// gentype, and therefore the last path segment of the collection URL.
	resource string

	// objectName is a realistic name for this resource, so that a failure
	// message names something recognizable.
	objectName string

	// labelSelector and fieldSelector only have to prove option propagation.
	labelSelector string
	fieldSelector string

	clientFor func(*NvidiaV1alpha1Client) C
	newObject func(name string) T
	newList   func(items ...T) L

	// items reaches the decoded items of a list. The typedList constraint only
	// exposes the list metadata, so without it no test could observe what the
	// response body's items decoded into.
	items func(L) []T
}

func (fixture resourceFixture[T, L, C]) collectionPath() string {
	return resourceCollectionPath(fixture.resource)
}

func (fixture resourceFixture[T, L, C]) namedPath(name string, subresources ...string) string {
	return resourceNamedPath(fixture.resource, name, subresources...)
}

func (fixture resourceFixture[T, L, C]) newServerAndClient(t *testing.T, handler http.HandlerFunc) (*recordingServer, C) {
	t.Helper()

	server, groupClient := newRecordingServerAndClient(t, handler)
	return server, fixture.clientFor(groupClient)
}

// runSharedVerbTests exercises the HTTP plumbing every generated typed client in
// this group shares, since each is a thin wrapper around the same
// gentype.ClientWithList. Anything that depends on a resource's own schema stays
// in a focused test in that resource's file.
func runSharedVerbTests[T typedObject, L typedList, C typedResourceClient[T, L]](t *testing.T, fixture resourceFixture[T, L, C]) {
	t.Helper()

	t.Run("Get reads the named path", func(t *testing.T) { testGet(t, fixture) })
	t.Run("Get surfaces a NotFound status and an empty object", func(t *testing.T) { testGetNotFound(t, fixture) })
	t.Run("a verb surfaces a server error", func(t *testing.T) { testVerbsSurfaceServerErrors(t, fixture) })
	t.Run("List reads the collection path", func(t *testing.T) { testList(t, fixture) })
	t.Run("Create posts to the collection path", func(t *testing.T) { testCreate(t, fixture) })
	t.Run("Update and UpdateStatus put to their paths", func(t *testing.T) { testUpdateVerbs(t, fixture) })
	t.Run("Delete deletes the named path", func(t *testing.T) { testDelete(t, fixture) })
	t.Run("DeleteCollection selects on the collection path", func(t *testing.T) { testDeleteCollection(t, fixture) })
	t.Run("Patch sends each patch type", func(t *testing.T) { testPatch(t, fixture) })
	t.Run("Watch opens the collection watch", func(t *testing.T) { testWatch(t, fixture) })
	t.Run("Watch returns no watcher for a rejected request", func(t *testing.T) { testWatchRejected(t, fixture) })
}

func testGet[T typedObject, L typedList, C typedResourceClient[T, L]](t *testing.T, fixture resourceFixture[T, L, C]) {
	expectedObject := fixture.newObject(fixture.objectName)

	server, client := fixture.newServerAndClient(t, func(responseWriter http.ResponseWriter, _ *http.Request) {
		writeJSONResponse(responseWriter, http.StatusOK, expectedObject)
	})

	gotObject, err := client.Get(t.Context(), fixture.objectName, metav1.GetOptions{ResourceVersion: "0"})
	require.NoError(t, err)

	request := server.onlyRequest(t)
	assert.Equal(t, http.MethodGet, request.Method)
	assert.Equal(t, fixture.namedPath(fixture.objectName), request.Path)
	assert.Equal(t, url.Values{"resourceVersion": {"0"}}, request.Query)
	// The generated setConfigDefaults negotiates JSON and installs the client-go
	// user agent; net/http would otherwise substitute its own, so this is
	// compared rather than merely checked for emptiness.
	assert.Equal(t, "application/json, */*", request.Header.Get("Accept"))
	assert.Equal(t, rest.DefaultKubernetesUserAgent(), request.Header.Get("User-Agent"))

	require.NotNil(t, gotObject)
	assert.Equal(t, fixture.objectName, gotObject.GetName())
	assert.Equal(t, expectedObject.GetResourceVersion(), gotObject.GetResourceVersion())
	assert.Equal(t, expectedObject.GetLabels(), gotObject.GetLabels())
}

// testGetNotFound is also the one place this package pins the generated
// client's habit of returning a freshly allocated object alongside an error
// instead of a nil pointer. Every other error-path test asserts the error
// alone, because the error is the contract.
func testGetNotFound[T typedObject, L typedList, C typedResourceClient[T, L]](t *testing.T, fixture resourceFixture[T, L, C]) {
	server, client := fixture.newServerAndClient(t, func(responseWriter http.ResponseWriter, _ *http.Request) {
		writeJSONResponse(responseWriter, http.StatusNotFound, notFoundStatus(fixture.resource, missingObjectName))
	})

	gotObject, err := client.Get(t.Context(), missingObjectName, metav1.GetOptions{})
	require.Error(t, err)
	assert.True(t, apierrors.IsNotFound(err), "expected a NotFound error, got %v", err)
	assert.Equal(t, fixture.namedPath(missingObjectName), server.onlyRequest(t).Path)

	require.NotNil(t, gotObject)
	assert.Empty(t, gotObject.GetName())
}

// testVerbsSurfaceServerErrors covers the two shapes the generated methods
// delegate in: one that sends a body and decodes a reply, and one that returns
// an error alone. The remaining verbs reach the server through the same gentype
// plumbing; Get's error path is covered by testGetNotFound and Watch's by
// testWatchRejected.
func testVerbsSurfaceServerErrors[T typedObject, L typedList, C typedResourceClient[T, L]](t *testing.T, fixture resourceFixture[T, L, C]) {
	testCases := []struct {
		name string
		call func(ctx context.Context, client C) error
	}{
		{
			name: "Create",
			call: func(ctx context.Context, client C) error {
				_, err := client.Create(ctx, fixture.newObject(fixture.objectName), metav1.CreateOptions{})
				return err
			},
		},
		{
			name: "Delete",
			call: func(ctx context.Context, client C) error {
				return client.Delete(ctx, fixture.objectName, metav1.DeleteOptions{})
			},
		},
	}
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			_, client := fixture.newServerAndClient(t, func(responseWriter http.ResponseWriter, _ *http.Request) {
				writeJSONResponse(responseWriter, http.StatusConflict,
					failureStatus(http.StatusConflict, metav1.StatusReasonConflict, "the object has been modified"))
			})

			err := testCase.call(t.Context(), client)
			require.Error(t, err)
			assert.True(t, apierrors.IsConflict(err), "expected a Conflict error, got %v", err)
		})
	}
}

func testList[T typedObject, L typedList, C typedResourceClient[T, L]](t *testing.T, fixture resourceFixture[T, L, C]) {
	expectedList := fixture.newList(fixture.newObject(fixture.objectName))
	expectedList.SetResourceVersion("99")
	expectedList.SetContinue("next-token")

	sendInitialEvents := true
	timeoutSeconds := int64(17)

	server, client := fixture.newServerAndClient(t, func(responseWriter http.ResponseWriter, _ *http.Request) {
		writeJSONResponse(responseWriter, http.StatusOK, expectedList)
	})

	gotList, err := client.List(t.Context(), metav1.ListOptions{
		LabelSelector:        fixture.labelSelector,
		FieldSelector:        fixture.fieldSelector,
		ResourceVersion:      "0",
		ResourceVersionMatch: metav1.ResourceVersionMatchNotOlderThan,
		Limit:                50,
		Continue:             "abc",
		AllowWatchBookmarks:  true,
		SendInitialEvents:    &sendInitialEvents,
		TimeoutSeconds:       &timeoutSeconds,
	})
	require.NoError(t, err)

	request := server.onlyRequest(t)
	assert.Equal(t, http.MethodGet, request.Method)
	assert.Equal(t, fixture.collectionPath(), request.Path)
	// Compared whole rather than key by key, so a single option that stops
	// reaching the URL and a parameter the client leaks into it both fail here.
	// client-go sends TimeoutSeconds twice: once as the encoded list option and
	// once as the request's own "timeout" duration parameter.
	assert.Equal(t, url.Values{
		"labelSelector":        {fixture.labelSelector},
		"fieldSelector":        {fixture.fieldSelector},
		"resourceVersion":      {"0"},
		"resourceVersionMatch": {string(metav1.ResourceVersionMatchNotOlderThan)},
		"limit":                {"50"},
		"continue":             {"abc"},
		"allowWatchBookmarks":  {"true"},
		"sendInitialEvents":    {"true"},
		"timeoutSeconds":       {"17"},
		"timeout":              {"17s"},
	}, request.Query)

	assert.Equal(t, "99", gotList.GetResourceVersion())
	assert.Equal(t, "next-token", gotList.GetContinue())

	gotItems := fixture.items(gotList)
	require.Len(t, gotItems, 1)
	assert.Equal(t, fixture.objectName, gotItems[0].GetName())
}

func testCreate[T typedObject, L typedList, C typedResourceClient[T, L]](t *testing.T, fixture resourceFixture[T, L, C]) {
	objectToCreate := fixture.newObject(fixture.objectName)

	server, client := fixture.newServerAndClient(t, func(responseWriter http.ResponseWriter, _ *http.Request) {
		writeJSONResponse(responseWriter, http.StatusCreated, objectToCreate)
	})

	gotObject, err := client.Create(t.Context(), objectToCreate, metav1.CreateOptions{
		DryRun:          []string{metav1.DryRunAll},
		FieldManager:    fieldManagerName,
		FieldValidation: metav1.FieldValidationWarn,
	})
	require.NoError(t, err)

	request := server.onlyRequest(t)
	assert.Equal(t, http.MethodPost, request.Method)
	assert.Equal(t, fixture.collectionPath(), request.Path)
	assert.Equal(t, "application/json", request.Header.Get("Content-Type"))
	assert.Equal(t, url.Values{
		"dryRun":          {metav1.DryRunAll},
		"fieldManager":    {fieldManagerName},
		"fieldValidation": {metav1.FieldValidationWarn},
	}, request.Query)

	sentMetadata := decodeRequestMetadata(t, request.Body)
	assert.Equal(t, fixture.objectName, sentMetadata.Name)
	assert.Equal(t, objectToCreate.GetLabels(), sentMetadata.Labels)

	assert.Equal(t, fixture.objectName, gotObject.GetName())
}

// testUpdateVerbs covers Update and UpdateStatus together: the generated
// methods differ only in the subresource client-gen appends to the path.
func testUpdateVerbs[T typedObject, L typedList, C typedResourceClient[T, L]](t *testing.T, fixture resourceFixture[T, L, C]) {
	testCases := []struct {
		name         string
		update       func(ctx context.Context, client C, object T, options metav1.UpdateOptions) (T, error)
		subresources []string
	}{
		{
			name: "Update",
			update: func(ctx context.Context, client C, object T, options metav1.UpdateOptions) (T, error) {
				return client.Update(ctx, object, options)
			},
		},
		{
			name: "UpdateStatus",
			update: func(ctx context.Context, client C, object T, options metav1.UpdateOptions) (T, error) {
				return client.UpdateStatus(ctx, object, options)
			},
			subresources: []string{"status"},
		},
	}
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			objectToUpdate := fixture.newObject(fixture.objectName)

			server, client := fixture.newServerAndClient(t, func(responseWriter http.ResponseWriter, _ *http.Request) {
				writeJSONResponse(responseWriter, http.StatusOK, objectToUpdate)
			})

			gotObject, err := testCase.update(t.Context(), client, objectToUpdate, metav1.UpdateOptions{
				DryRun:          []string{metav1.DryRunAll},
				FieldManager:    fieldManagerName,
				FieldValidation: metav1.FieldValidationWarn,
			})
			require.NoError(t, err)

			request := server.onlyRequest(t)
			assert.Equal(t, http.MethodPut, request.Method)
			assert.Equal(t, fixture.namedPath(fixture.objectName, testCase.subresources...), request.Path)
			assert.Equal(t, "application/json", request.Header.Get("Content-Type"))
			assert.Equal(t, url.Values{
				"dryRun":          {metav1.DryRunAll},
				"fieldManager":    {fieldManagerName},
				"fieldValidation": {metav1.FieldValidationWarn},
			}, request.Query)

			assert.Equal(t, fixture.objectName, decodeRequestMetadata(t, request.Body).Name)
			assert.Equal(t, fixture.objectName, gotObject.GetName())
		})
	}
}

func testDelete[T typedObject, L typedList, C typedResourceClient[T, L]](t *testing.T, fixture resourceFixture[T, L, C]) {
	server, client := fixture.newServerAndClient(t, func(responseWriter http.ResponseWriter, _ *http.Request) {
		writeJSONResponse(responseWriter, http.StatusOK, successStatus())
	})

	gracePeriodSeconds := int64(30)
	objectUID := types.UID("0f1e2d3c-4b5a-6978-8796-a5b4c3d2e1f0")
	resourceVersion := "77"
	propagationPolicy := metav1.DeletePropagationForeground
	deleteOptions := metav1.DeleteOptions{
		GracePeriodSeconds: &gracePeriodSeconds,
		DryRun:             []string{metav1.DryRunAll},
		PropagationPolicy:  &propagationPolicy,
		Preconditions:      &metav1.Preconditions{UID: &objectUID, ResourceVersion: &resourceVersion},
	}

	require.NoError(t, client.Delete(t.Context(), fixture.objectName, deleteOptions))

	request := server.onlyRequest(t)
	assert.Equal(t, http.MethodDelete, request.Method)
	assert.Equal(t, fixture.namedPath(fixture.objectName), request.Path)
	// Unlike every other verb, Delete puts all of its options in the body; not
	// even dryRun reaches the URL.
	assert.Empty(t, request.Query)

	assertDeleteOptionsSent(t, request.Body, deleteOptions)
}

func testDeleteCollection[T typedObject, L typedList, C typedResourceClient[T, L]](t *testing.T, fixture resourceFixture[T, L, C]) {
	server, client := fixture.newServerAndClient(t, func(responseWriter http.ResponseWriter, _ *http.Request) {
		writeJSONResponse(responseWriter, http.StatusOK, successStatus())
	})

	gracePeriodSeconds := int64(45)
	propagationPolicy := metav1.DeletePropagationOrphan
	deleteOptions := metav1.DeleteOptions{
		GracePeriodSeconds: &gracePeriodSeconds,
		DryRun:             []string{metav1.DryRunAll},
		PropagationPolicy:  &propagationPolicy,
	}

	err := client.DeleteCollection(t.Context(), deleteOptions, metav1.ListOptions{
		LabelSelector: fixture.labelSelector,
		FieldSelector: fixture.fieldSelector,
	})
	require.NoError(t, err)

	request := server.onlyRequest(t)
	assert.Equal(t, http.MethodDelete, request.Method)
	assert.Equal(t, fixture.collectionPath(), request.Path)
	// Only the list options select the collection; the delete options stay in
	// the body, so dryRun is absent from the URL here too.
	assert.Equal(t, url.Values{
		"labelSelector": {fixture.labelSelector},
		"fieldSelector": {fixture.fieldSelector},
	}, request.Query)

	assertDeleteOptionsSent(t, request.Body, deleteOptions)
}

func testPatch[T typedObject, L typedList, C typedResourceClient[T, L]](t *testing.T, fixture resourceFixture[T, L, C]) {
	forceOwnership := true
	// The generated Patch forwards its options untouched, so the same set is
	// sent for every patch type even though the API server only honours Force
	// on an apply patch.
	patchOptions := metav1.PatchOptions{
		DryRun:          []string{metav1.DryRunAll},
		Force:           &forceOwnership,
		FieldManager:    fieldManagerName,
		FieldValidation: metav1.FieldValidationWarn,
	}
	expectedQuery := url.Values{
		"dryRun":          {metav1.DryRunAll},
		"force":           {"true"},
		"fieldManager":    {fieldManagerName},
		"fieldValidation": {metav1.FieldValidationWarn},
	}

	testCases := []struct {
		name                string
		patchType           types.PatchType
		expectedContentType string
		subresources        []string
	}{
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
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			patch := []byte(`{"metadata":{"labels":{"patched":"true"}}}`)

			server, client := fixture.newServerAndClient(t, func(responseWriter http.ResponseWriter, _ *http.Request) {
				writeJSONResponse(responseWriter, http.StatusOK, fixture.newObject(fixture.objectName))
			})

			gotObject, err := client.Patch(t.Context(), fixture.objectName, testCase.patchType, patch,
				patchOptions, testCase.subresources...)
			require.NoError(t, err)

			request := server.onlyRequest(t)
			assert.Equal(t, http.MethodPatch, request.Method)
			assert.Equal(t, fixture.namedPath(fixture.objectName, testCase.subresources...), request.Path)
			assert.Equal(t, testCase.expectedContentType, request.Header.Get("Content-Type"))
			assert.Equal(t, patch, request.Body)
			assert.Equal(t, expectedQuery, request.Query)

			assert.Equal(t, fixture.objectName, gotObject.GetName())
		})
	}
}

func testWatch[T typedObject, L typedList, C typedResourceClient[T, L]](t *testing.T, fixture resourceFixture[T, L, C]) {
	frame := marshalWatchFrame(t, watch.Added, fixture.newObject(fixture.objectName))
	sendInitialEvents := true
	timeoutSeconds := int64(11)

	server, client := fixture.newServerAndClient(t, func(responseWriter http.ResponseWriter, _ *http.Request) {
		writeWatchFrames(responseWriter, frame)
	})

	watcher, err := client.Watch(t.Context(), metav1.ListOptions{
		LabelSelector:        fixture.labelSelector,
		FieldSelector:        fixture.fieldSelector,
		ResourceVersion:      "0",
		ResourceVersionMatch: metav1.ResourceVersionMatchNotOlderThan,
		AllowWatchBookmarks:  true,
		SendInitialEvents:    &sendInitialEvents,
		TimeoutSeconds:       &timeoutSeconds,
	})
	require.NoError(t, err)
	defer watcher.Stop()

	request := server.onlyRequest(t)
	assert.Equal(t, http.MethodGet, request.Method)
	assert.Equal(t, fixture.collectionPath(), request.Path)
	// Watch is the one verb the generated client forces a parameter of its own
	// onto, so the whole query is compared here too.
	assert.Equal(t, url.Values{
		"watch":                {"true"},
		"labelSelector":        {fixture.labelSelector},
		"fieldSelector":        {fixture.fieldSelector},
		"resourceVersion":      {"0"},
		"resourceVersionMatch": {string(metav1.ResourceVersionMatchNotOlderThan)},
		"allowWatchBookmarks":  {"true"},
		"sendInitialEvents":    {"true"},
		"timeoutSeconds":       {"11"},
		"timeout":              {"11s"},
	}, request.Query)

	event := requireWatchEvent(t, watcher.ResultChan())
	assert.Equal(t, watch.Added, event.Type)
	watchedObject, ok := event.Object.(T)
	require.True(t, ok, "unexpected watch payload type %T", event.Object)
	assert.Equal(t, fixture.objectName, watchedObject.GetName())
}

func testWatchRejected[T typedObject, L typedList, C typedResourceClient[T, L]](t *testing.T, fixture resourceFixture[T, L, C]) {
	_, client := fixture.newServerAndClient(t, func(responseWriter http.ResponseWriter, _ *http.Request) {
		writeJSONResponse(responseWriter, http.StatusForbidden,
			failureStatus(http.StatusForbidden, metav1.StatusReasonForbidden, "watch is not allowed"))
	})

	watcher, err := client.Watch(t.Context(), metav1.ListOptions{})
	require.Error(t, err)
	assert.True(t, apierrors.IsForbidden(err), "expected a Forbidden error, got %v", err)

	// Compared against a nil watch.Interface rather than asserted with
	// assert.Nil: that helper is reflection based and also passes for a typed
	// nil boxed in a non-nil interface, which a caller's `defer w.Stop()` would
	// then panic on.
	var nilWatcher watch.Interface
	assert.Equal(t, nilWatcher, watcher)
}

// newReadyCondition builds the status condition both resources in this group
// carry. Its timestamp is fixed so the RFC3339 encoding can be asserted
// literally.
func newReadyCondition(status metav1.ConditionStatus, reason string) metav1.Condition {
	return metav1.Condition{
		Type:               "Ready",
		Status:             status,
		ObservedGeneration: 3,
		LastTransitionTime: metav1.NewTime(time.Date(2025, time.March, 14, 15, 9, 26, 0, time.UTC)),
		Reason:             reason,
		Message:            "reported by the gpu-operator controller",
	}
}

// assertConditionRoundTripped checks that a status condition survives both
// encodings: metav1.Time is written as an RFC3339 string on the way out and
// parsed back on the way in, which a struct comparison of Go values alone would
// not prove.
func assertConditionRoundTripped(t *testing.T, requestBody []byte, gotConditions []metav1.Condition, expectedStatus metav1.ConditionStatus, expectedReason string) {
	t.Helper()

	assert.Contains(t, string(requestBody), `"lastTransitionTime":"2025-03-14T15:09:26Z"`)

	require.Len(t, gotConditions, 1)
	gotCondition := gotConditions[0]
	assert.Equal(t, "Ready", gotCondition.Type)
	assert.Equal(t, expectedStatus, gotCondition.Status)
	assert.Equal(t, expectedReason, gotCondition.Reason)
	assert.Equal(t, int64(3), gotCondition.ObservedGeneration)
	assert.Equal(t, "reported by the gpu-operator controller", gotCondition.Message)
	assert.Equal(t, time.Date(2025, time.March, 14, 15, 9, 26, 0, time.UTC), gotCondition.LastTransitionTime.UTC())
}
