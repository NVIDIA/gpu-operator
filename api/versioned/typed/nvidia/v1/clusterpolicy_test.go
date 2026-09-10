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
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	runtime "k8s.io/apimachinery/pkg/runtime"
	types "k8s.io/apimachinery/pkg/types"
	watch "k8s.io/apimachinery/pkg/watch"
	rest "k8s.io/client-go/rest"

	nvidiav1 "github.com/NVIDIA/gpu-operator/api/nvidia/v1"
)

// watchEventTimeout bounds the watch-channel read below. The API server stub,
// the REST client and the test all run in one process, so a frame that is going
// to arrive arrives in microseconds; a longer bound would only make a
// regression fail slowly.
const watchEventTimeout = 2 * time.Second

const (
	clusterPolicyResource = "clusterpolicies"
	clusterPolicyName     = "cluster-policy"
	fieldManagerName      = "gpu-operator"
	appLabelValue         = "gpu-operator"
	appLabelSelector      = "app=" + appLabelValue
	jsonContentType       = "application/json"
)

// resourceCollectionPath builds the cluster-scoped collection path out of the
// registered group version rather than a literal. Asserting a request path
// against it therefore also pins the empty namespace newClusterPolicies hands
// to gentype: a namespaced client would insert /namespaces/<ns>/.
func resourceCollectionPath(resource string) string {
	groupVersion := nvidiav1.SchemeGroupVersion
	return path.Join("/apis", groupVersion.Group, groupVersion.Version, resource)
}

// resourceNamedPath builds the path for a single named object, optionally below
// one or more subresources.
func resourceNamedPath(resource, name string, subresources ...string) string {
	return path.Join(append([]string{resourceCollectionPath(resource), name}, subresources...)...)
}

// recordedRequest is a snapshot of a request as it arrived at the test server.
type recordedRequest struct {
	Method string
	Path   string
	Query  url.Values
	Header http.Header
	Body   []byte

	// BodyReadError is carried rather than asserted on, because it is filled in
	// on the server's goroutine, where t.FailNow may not be called.
	BodyReadError error
}

// recordingServer is an httptest.Server that records every request it serves.
// The mutex guards the handler goroutine against the test goroutine.
type recordingServer struct {
	*httptest.Server

	requestsMutex sync.Mutex
	requests      []recordedRequest
}

// newRecordingServerAndClient stands up a real HTTP API server running handler
// and points a generated ClusterPolicy client at it. The pair is returned in
// the same order as its v1alpha1 twin, so cases copied between the two packages
// keep working.
func newRecordingServerAndClient(t *testing.T, handler http.HandlerFunc) (*recordingServer, ClusterPolicyInterface) {
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

	return server, client.ClusterPolicies()
}

// onlyRequest returns the single request the server is expected to have served.
func (server *recordingServer) onlyRequest(t *testing.T) recordedRequest {
	t.Helper()

	server.requestsMutex.Lock()
	defer server.requestsMutex.Unlock()

	require.Len(t, server.requests, 1, "expected exactly one request to the API server")
	request := server.requests[0]
	require.NoError(t, request.BodyReadError, "failed to read the body of %s %s", request.Method, request.Path)
	return request
}

// writeJSONResponse serializes payload as the response body, as the API server
// would. It asserts nothing: it runs on the httptest server's goroutine, where
// t.FailNow may not be called, so a marshal failure is surfaced to the client as
// a 500 and fails an assertion on the test goroutine instead.
func writeJSONResponse(responseWriter http.ResponseWriter, statusCode int, payload runtime.Object) {
	responseBody, err := json.Marshal(payload)
	if err != nil {
		http.Error(responseWriter, "test handler: marshal failed: "+err.Error(), http.StatusInternalServerError)
		return
	}
	responseWriter.Header().Set("Content-Type", jsonContentType)
	responseWriter.WriteHeader(statusCode)
	_, _ = responseWriter.Write(responseBody)
}

// respondWithSuccessStatus answers with the metav1.Status an API server returns
// for a successful delete.
func respondWithSuccessStatus(responseWriter http.ResponseWriter, _ *http.Request) {
	writeJSONResponse(responseWriter, http.StatusOK, &metav1.Status{
		TypeMeta: metav1.TypeMeta{APIVersion: "v1", Kind: "Status"},
		Status:   metav1.StatusSuccess,
	})
}

// respondWithConflictStatus answers with the metav1.Status an API server
// returns for a rejected request.
func respondWithConflictStatus(responseWriter http.ResponseWriter, _ *http.Request) {
	writeJSONResponse(responseWriter, http.StatusConflict, &metav1.Status{
		TypeMeta: metav1.TypeMeta{APIVersion: "v1", Kind: "Status"},
		Status:   metav1.StatusFailure,
		Code:     http.StatusConflict,
		Reason:   metav1.StatusReasonConflict,
		Message:  "the object has been modified",
	})
}

// assertSentClusterPolicy checks that the object the caller handed the client is
// what reaches the wire, rather than a blank one built from the client's own
// type.
func assertSentClusterPolicy(t *testing.T, body []byte) {
	t.Helper()

	var sentClusterPolicy nvidiav1.ClusterPolicy
	require.NoError(t, json.Unmarshal(body, &sentClusterPolicy))
	assert.Equal(t, clusterPolicyName, sentClusterPolicy.Name)
	assert.Equal(t, "nvidia", sentClusterPolicy.Spec.Operator.RuntimeClass)
}

// decodeDeleteOptions decodes a DeleteOptions request body. Only the fields
// that change what the API server does are asserted on; the apiVersion the REST
// client encodes the body under is a client-go serializer detail.
func decodeDeleteOptions(t *testing.T, body []byte) metav1.DeleteOptions {
	t.Helper()

	var sentDeleteOptions metav1.DeleteOptions
	require.NoError(t, json.Unmarshal(body, &sentDeleteOptions))
	return sentDeleteOptions
}

// requireWatchEvent is the bounded read the watch assertion uses instead of an
// unbounded receive.
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

// newClusterPolicy builds a decodable ClusterPolicy, including the TypeMeta the
// client-side decoder needs to recognize the payload.
func newClusterPolicy(name string) *nvidiav1.ClusterPolicy {
	clusterPolicy := &nvidiav1.ClusterPolicy{
		TypeMeta:   metav1.TypeMeta{APIVersion: nvidiav1.SchemeGroupVersion.String(), Kind: "ClusterPolicy"},
		ObjectMeta: metav1.ObjectMeta{Name: name, Labels: map[string]string{"app": appLabelValue}},
	}
	clusterPolicy.Spec.Operator.RuntimeClass = "nvidia"
	clusterPolicy.Status.State = nvidiav1.Ready
	return clusterPolicy
}

// TestClusterPolicies drives every verb of the generated client against a real
// HTTP server and pins what reaches the wire. A regeneration that renamed the
// resource, moved the group version or made the resource namespaced lands on the
// request line, which is what each case compares. Error classification, retries
// and watch-stream framing belong to client-go and are not retested here.
func TestClusterPolicies(t *testing.T) {
	// Marshalled here, on the test goroutine, so the watch handler below stays
	// assertion free.
	serializedClusterPolicy, err := json.Marshal(newClusterPolicy(clusterPolicyName))
	require.NoError(t, err)
	addedFrame, err := json.Marshal(metav1.WatchEvent{
		Type:   string(watch.Added),
		Object: runtime.RawExtension{Raw: serializedClusterPolicy},
	})
	require.NoError(t, err)

	testCases := []struct {
		name string
		// respond serves the API server's reply. A nil respond answers with the
		// seeded ClusterPolicy.
		respond             http.HandlerFunc
		call                func(t *testing.T, client ClusterPolicyInterface)
		expectedMethod      string
		expectedPath        string
		expectedQuery       url.Values
		expectedContentType string
		assertRequestBody   func(t *testing.T, body []byte)
	}{
		{
			name: "get",
			call: func(t *testing.T, client ClusterPolicyInterface) {
				gotClusterPolicy, err := client.Get(t.Context(), clusterPolicyName, metav1.GetOptions{ResourceVersion: "0"})
				require.NoError(t, err)
				assert.Equal(t, clusterPolicyName, gotClusterPolicy.Name)
				assert.Equal(t, "nvidia", gotClusterPolicy.Spec.Operator.RuntimeClass)
				assert.Equal(t, nvidiav1.Ready, gotClusterPolicy.Status.State)
			},
			expectedMethod: http.MethodGet,
			expectedPath:   resourceNamedPath(clusterPolicyResource, clusterPolicyName),
			expectedQuery:  url.Values{"resourceVersion": {"0"}},
		},
		{
			name: "list",
			respond: func(responseWriter http.ResponseWriter, _ *http.Request) {
				writeJSONResponse(responseWriter, http.StatusOK, &nvidiav1.ClusterPolicyList{
					TypeMeta: metav1.TypeMeta{APIVersion: nvidiav1.SchemeGroupVersion.String(), Kind: "ClusterPolicyList"},
					ListMeta: metav1.ListMeta{ResourceVersion: "4242"},
					Items:    []nvidiav1.ClusterPolicy{*newClusterPolicy(clusterPolicyName)},
				})
			},
			call: func(t *testing.T, client ClusterPolicyInterface) {
				gotList, err := client.List(t.Context(), metav1.ListOptions{
					LabelSelector:  appLabelSelector,
					FieldSelector:  "metadata.name=cluster-policy",
					Limit:          50,
					TimeoutSeconds: new(int64(7)),
				})
				require.NoError(t, err)
				assert.Equal(t, "4242", gotList.ResourceVersion)
				require.Len(t, gotList.Items, 1)
				assert.Equal(t, clusterPolicyName, gotList.Items[0].Name)
			},
			expectedMethod: http.MethodGet,
			expectedPath:   resourceCollectionPath(clusterPolicyResource),
			// "timeout" comes from the REST request timeout, "timeoutSeconds"
			// from the encoded ListOptions, so both have to be here.
			expectedQuery: url.Values{
				"labelSelector":  {appLabelSelector},
				"fieldSelector":  {"metadata.name=cluster-policy"},
				"limit":          {"50"},
				"timeoutSeconds": {"7"},
				"timeout":        {"7s"},
			},
		},
		{
			name: "create",
			call: func(t *testing.T, client ClusterPolicyInterface) {
				_, err := client.Create(t.Context(), newClusterPolicy(clusterPolicyName), metav1.CreateOptions{
					FieldManager:    fieldManagerName,
					DryRun:          []string{metav1.DryRunAll},
					FieldValidation: metav1.FieldValidationStrict,
				})
				require.NoError(t, err)
			},
			expectedMethod: http.MethodPost,
			expectedPath:   resourceCollectionPath(clusterPolicyResource),
			expectedQuery: url.Values{
				"fieldManager":    {fieldManagerName},
				"dryRun":          {metav1.DryRunAll},
				"fieldValidation": {metav1.FieldValidationStrict},
			},
			expectedContentType: jsonContentType,
			assertRequestBody: func(t *testing.T, body []byte) {
				var sentClusterPolicy nvidiav1.ClusterPolicy
				require.NoError(t, json.Unmarshal(body, &sentClusterPolicy))
				assert.Equal(t, nvidiav1.SchemeGroupVersion.String(), sentClusterPolicy.APIVersion)
				assert.Equal(t, "ClusterPolicy", sentClusterPolicy.Kind)
				assert.Equal(t, clusterPolicyName, sentClusterPolicy.Name)
				assert.Equal(t, "nvidia", sentClusterPolicy.Spec.Operator.RuntimeClass)
			},
		},
		{
			name: "update",
			call: func(t *testing.T, client ClusterPolicyInterface) {
				_, err := client.Update(t.Context(), newClusterPolicy(clusterPolicyName),
					metav1.UpdateOptions{FieldManager: fieldManagerName})
				require.NoError(t, err)
			},
			expectedMethod:      http.MethodPut,
			expectedPath:        resourceNamedPath(clusterPolicyResource, clusterPolicyName),
			expectedQuery:       url.Values{"fieldManager": {fieldManagerName}},
			expectedContentType: jsonContentType,
			assertRequestBody:   assertSentClusterPolicy,
		},
		{
			name: "update status",
			call: func(t *testing.T, client ClusterPolicyInterface) {
				_, err := client.UpdateStatus(t.Context(), newClusterPolicy(clusterPolicyName),
					metav1.UpdateOptions{FieldManager: fieldManagerName})
				require.NoError(t, err)
			},
			expectedMethod:      http.MethodPut,
			expectedPath:        resourceNamedPath(clusterPolicyResource, clusterPolicyName, "status"),
			expectedQuery:       url.Values{"fieldManager": {fieldManagerName}},
			expectedContentType: jsonContentType,
			assertRequestBody:   assertSentClusterPolicy,
		},
		{
			name:    "delete",
			respond: respondWithSuccessStatus,
			call: func(t *testing.T, client ClusterPolicyInterface) {
				require.NoError(t, client.Delete(t.Context(), clusterPolicyName, metav1.DeleteOptions{
					PropagationPolicy: new(metav1.DeletePropagationForeground),
					Preconditions:     &metav1.Preconditions{ResourceVersion: new("9")},
				}))
			},
			expectedMethod: http.MethodDelete,
			expectedPath:   resourceNamedPath(clusterPolicyResource, clusterPolicyName),
			// Delete puts its options in the body, so nothing may leak into the query.
			expectedQuery:       url.Values{},
			expectedContentType: jsonContentType,
			assertRequestBody: func(t *testing.T, body []byte) {
				sentDeleteOptions := decodeDeleteOptions(t, body)
				require.NotNil(t, sentDeleteOptions.PropagationPolicy)
				assert.Equal(t, metav1.DeletePropagationForeground, *sentDeleteOptions.PropagationPolicy)
				require.NotNil(t, sentDeleteOptions.Preconditions)
				require.NotNil(t, sentDeleteOptions.Preconditions.ResourceVersion)
				assert.Equal(t, "9", *sentDeleteOptions.Preconditions.ResourceVersion)
			},
		},
		{
			name:    "delete collection",
			respond: respondWithSuccessStatus,
			call: func(t *testing.T, client ClusterPolicyInterface) {
				require.NoError(t, client.DeleteCollection(t.Context(),
					metav1.DeleteOptions{GracePeriodSeconds: new(int64(30))},
					metav1.ListOptions{LabelSelector: appLabelSelector, Limit: 10}))
			},
			expectedMethod: http.MethodDelete,
			expectedPath:   resourceCollectionPath(clusterPolicyResource),
			// Only the list options reach the query; the delete options ride in the body.
			expectedQuery: url.Values{
				"labelSelector": {appLabelSelector},
				"limit":         {"10"},
			},
			expectedContentType: jsonContentType,
			assertRequestBody: func(t *testing.T, body []byte) {
				sentDeleteOptions := decodeDeleteOptions(t, body)
				require.NotNil(t, sentDeleteOptions.GracePeriodSeconds)
				assert.Equal(t, int64(30), *sentDeleteOptions.GracePeriodSeconds)
			},
		},
		{
			name: "merge patch",
			call: func(t *testing.T, client ClusterPolicyInterface) {
				_, err := client.Patch(t.Context(), clusterPolicyName, types.MergePatchType,
					[]byte(`{"spec":{"operator":{"runtimeClass":"nvidia-crio"}}}`),
					metav1.PatchOptions{FieldManager: fieldManagerName})
				require.NoError(t, err)
			},
			expectedMethod:      http.MethodPatch,
			expectedPath:        resourceNamedPath(clusterPolicyResource, clusterPolicyName),
			expectedQuery:       url.Values{"fieldManager": {fieldManagerName}},
			expectedContentType: string(types.MergePatchType),
			assertRequestBody: func(t *testing.T, body []byte) {
				assert.Equal(t, []byte(`{"spec":{"operator":{"runtimeClass":"nvidia-crio"}}}`), body)
			},
		},
		{
			name: "strategic merge patch on the status subresource",
			call: func(t *testing.T, client ClusterPolicyInterface) {
				_, err := client.Patch(t.Context(), clusterPolicyName, types.StrategicMergePatchType,
					[]byte(`{"status":{"state":"ready"}}`), metav1.PatchOptions{}, "status")
				require.NoError(t, err)
			},
			expectedMethod:      http.MethodPatch,
			expectedPath:        resourceNamedPath(clusterPolicyResource, clusterPolicyName, "status"),
			expectedQuery:       url.Values{},
			expectedContentType: string(types.StrategicMergePatchType),
		},
		{
			name: "watch",
			respond: func(responseWriter http.ResponseWriter, _ *http.Request) {
				responseWriter.Header().Set("Content-Type", jsonContentType)
				_, _ = responseWriter.Write(addedFrame)
			},
			call: func(t *testing.T, client ClusterPolicyInterface) {
				watcher, err := client.Watch(t.Context(), metav1.ListOptions{
					LabelSelector:   appLabelSelector,
					ResourceVersion: "77",
				})
				require.NoError(t, err)
				defer watcher.Stop()

				event := requireWatchEvent(t, watcher.ResultChan())
				assert.Equal(t, watch.Added, event.Type)
				gotClusterPolicy, ok := event.Object.(*nvidiav1.ClusterPolicy)
				require.True(t, ok, "expected a *ClusterPolicy, got %T", event.Object)
				assert.Equal(t, clusterPolicyName, gotClusterPolicy.Name)
			},
			expectedMethod: http.MethodGet,
			expectedPath:   resourceCollectionPath(clusterPolicyResource),
			expectedQuery: url.Values{
				"watch":           {"true"},
				"labelSelector":   {appLabelSelector},
				"resourceVersion": {"77"},
			},
		},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			respond := testCase.respond
			if respond == nil {
				respond = func(responseWriter http.ResponseWriter, _ *http.Request) {
					writeJSONResponse(responseWriter, http.StatusOK, newClusterPolicy(clusterPolicyName))
				}
			}
			server, client := newRecordingServerAndClient(t, respond)

			testCase.call(t, client)

			request := server.onlyRequest(t)
			assert.Equal(t, testCase.expectedMethod, request.Method)
			assert.Equal(t, testCase.expectedPath, request.Path)
			// Compared as a whole so that a leaked extra parameter fails too.
			assert.Equal(t, testCase.expectedQuery, request.Query)
			assert.Equal(t, testCase.expectedContentType, request.Header.Get("Content-Type"))
			// Pinned exactly rather than by substring, so that a switch to
			// protobuf or CBOR fails here instead of as a decode error far from
			// its cause.
			assert.Equal(t, "application/json, */*", request.Header.Get("Accept"))
			if testCase.assertRequestBody != nil {
				testCase.assertRequestBody(t, request.Body)
			}
		})
	}
}

// TestClusterPoliciesSurfaceServerErrors keeps the error path symmetric with the
// table above, which serves only 2xx. Without it a verb that dropped the error
// client-go handed it would report success for a request the API server
// rejected. Which Go error a failure Status maps to is client-go's
// classification and is not retested here.
func TestClusterPoliciesSurfaceServerErrors(t *testing.T) {
	clusterPolicy := newClusterPolicy(clusterPolicyName)

	testCases := []struct {
		name string
		call func(ctx context.Context, client ClusterPolicyInterface) error
	}{
		{
			name: "get",
			call: func(ctx context.Context, client ClusterPolicyInterface) error {
				_, err := client.Get(ctx, clusterPolicyName, metav1.GetOptions{})
				return err
			},
		},
		{
			name: "list",
			call: func(ctx context.Context, client ClusterPolicyInterface) error {
				_, err := client.List(ctx, metav1.ListOptions{})
				return err
			},
		},
		{
			name: "create",
			call: func(ctx context.Context, client ClusterPolicyInterface) error {
				_, err := client.Create(ctx, clusterPolicy, metav1.CreateOptions{})
				return err
			},
		},
		{
			name: "update",
			call: func(ctx context.Context, client ClusterPolicyInterface) error {
				_, err := client.Update(ctx, clusterPolicy, metav1.UpdateOptions{})
				return err
			},
		},
		{
			name: "update status",
			call: func(ctx context.Context, client ClusterPolicyInterface) error {
				_, err := client.UpdateStatus(ctx, clusterPolicy, metav1.UpdateOptions{})
				return err
			},
		},
		{
			name: "delete",
			call: func(ctx context.Context, client ClusterPolicyInterface) error {
				return client.Delete(ctx, clusterPolicyName, metav1.DeleteOptions{})
			},
		},
		{
			name: "delete collection",
			call: func(ctx context.Context, client ClusterPolicyInterface) error {
				return client.DeleteCollection(ctx, metav1.DeleteOptions{}, metav1.ListOptions{})
			},
		},
		{
			name: "patch",
			call: func(ctx context.Context, client ClusterPolicyInterface) error {
				_, err := client.Patch(ctx, clusterPolicyName, types.MergePatchType, []byte(`{}`), metav1.PatchOptions{})
				return err
			},
		},
		{
			name: "watch",
			call: func(ctx context.Context, client ClusterPolicyInterface) error {
				_, err := client.Watch(ctx, metav1.ListOptions{})
				return err
			},
		},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			_, client := newRecordingServerAndClient(t, respondWithConflictStatus)

			require.Error(t, testCase.call(t.Context(), client))
		})
	}
}

// TestClusterPoliciesPaths spells the request URLs out literally. The table
// above builds its expectations from SchemeGroupVersion, so a group or version
// bump would move the client and the expectation together and go unnoticed;
// this is what pins them.
func TestClusterPoliciesPaths(t *testing.T) {
	assert.Equal(t, "/apis/nvidia.com/v1/clusterpolicies", resourceCollectionPath(clusterPolicyResource))
	assert.Equal(t, "/apis/nvidia.com/v1/clusterpolicies/cluster-policy",
		resourceNamedPath(clusterPolicyResource, clusterPolicyName))
	assert.Equal(t, "/apis/nvidia.com/v1/clusterpolicies/cluster-policy/status",
		resourceNamedPath(clusterPolicyResource, clusterPolicyName, "status"))
}
