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

package clusterinfo

import (
	"context"
	"encoding/json"
	"fmt"
	"maps"
	"net/http"
	"net/http/httptest"
	"net/url"
	"slices"
	"sync"
	"testing"

	"github.com/go-logr/logr"
	configv1 "github.com/openshift/api/config/v1"
	imagev1 "github.com/openshift/api/image/v1"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/rest"
	"sigs.k8s.io/controller-runtime/pkg/log"

	"github.com/NVIDIA/gpu-operator/internal/consts"
)

const (
	pathNodes                    = "/api/v1/nodes"
	pathClusterVersion           = "/apis/config.openshift.io/v1/clusterversions/version"
	pathProxy                    = "/apis/config.openshift.io/v1/proxies/cluster"
	pathDriverToolkitImageStream = "/apis/image.openshift.io/v1/namespaces/openshift/imagestreams/driver-toolkit"

	// The core group's resource list is reached through the legacy /api prefix, not through /apis.
	pathAPIVersions   = "/api"
	pathAPIGroups     = "/apis"
	pathCoreResources = "/api/v1"
)

func pathGroupVersionResources(groupVersion string) string {
	return "/apis/" + groupVersion
}

type recordedRequest struct {
	method string
	path   string
	query  url.Values
}

// stubAPIServer keeps "a handler answered 404" distinct from "no handler was registered for this
// URL", so a wrong-path assumption cannot pass as a legitimate NotFound.
type stubAPIServer struct {
	restConfig *rest.Config

	mu                    sync.Mutex
	requests              []recordedRequest
	unhandledRequestPaths []string
}

// An unregistered path fails the test during cleanup rather than at request time, so the case's
// own assertions report first.
func newStubAPIServer(t *testing.T, handlers map[string]http.HandlerFunc) *stubAPIServer {
	t.Helper()

	apiServer := &stubAPIServer{}
	mux := http.NewServeMux()
	for path, handler := range handlers {
		mux.HandleFunc(path, apiServer.recordingHandler(handler))
	}
	mux.HandleFunc("/", apiServer.recordingHandler(func(w http.ResponseWriter, r *http.Request) {
		apiServer.noteUnhandled(r.URL.Path)
		respondNotFound()(w, r)
	}))

	server := httptest.NewServer(mux)
	apiServer.restConfig = &rest.Config{
		Host: server.URL,
		// Pinning JSON makes rest.Request.UseProtobufAsDefault a no-op, so the core/v1 node
		// client — which prefers protobuf — also talks JSON to this stub.
		ContentConfig: rest.ContentConfig{ContentType: "application/json"},
	}

	t.Cleanup(func() {
		require.Empty(t, apiServer.unhandledPaths(), "client requested paths that no stub handler was registered for")
	})
	t.Cleanup(server.Close)

	return apiServer
}

func (s *stubAPIServer) recordingHandler(handler http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		s.mu.Lock()
		s.requests = append(s.requests, recordedRequest{method: r.Method, path: r.URL.Path, query: r.URL.Query()})
		s.mu.Unlock()
		handler(w, r)
	}
}

func (s *stubAPIServer) noteUnhandled(path string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.unhandledRequestPaths = append(s.unhandledRequestPaths, path)
}

func (s *stubAPIServer) unhandledPaths() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return slices.Clone(s.unhandledRequestPaths)
}

// recordedRequests is the only place the read side takes the mutex; the accessors below derive
// from its clone, so none of them can hand out a slice the recording handler still appends to.
func (s *stubAPIServer) recordedRequests() []recordedRequest {
	s.mu.Lock()
	defer s.mu.Unlock()
	return slices.Clone(s.requests)
}

func (s *stubAPIServer) requestedPaths() []string {
	var paths []string
	for _, request := range s.recordedRequests() {
		paths = append(paths, request.path)
	}
	return paths
}

func (s *stubAPIServer) requestsTo(path string) []recordedRequest {
	var matchingRequests []recordedRequest
	for _, request := range s.recordedRequests() {
		if request.path == path {
			matchingRequests = append(matchingRequests, request)
		}
	}
	return matchingRequests
}

func (s *stubAPIServer) distinctRequestedPaths() []string {
	return slices.Compact(slices.Sorted(slices.Values(s.requestedPaths())))
}

func assertRequestedPathSequence(t *testing.T, apiServer *stubAPIServer, expectedPaths []string) {
	t.Helper()

	assert.Equal(t, expectedPaths, apiServer.requestedPaths())
	assertEveryRequestWasARead(t, apiServer)
}

// assertRequestedPathSet, like the sequence form, asserts every request was a read, but discovery
// fans out in parallel and repeats the walk on failure, so its order and repeat count are not pinned.
func assertRequestedPathSet(t *testing.T, apiServer *stubAPIServer, expectedPaths []string) {
	t.Helper()

	assert.ElementsMatch(t, expectedPaths, apiServer.distinctRequestedPaths())
	assertEveryRequestWasARead(t, apiServer)
}

func assertRequestedPathSetExcluding(t *testing.T, apiServer *stubAPIServer, expectedPaths []string, excludedPaths ...string) {
	t.Helper()

	comparedPaths := slices.DeleteFunc(apiServer.distinctRequestedPaths(), func(path string) bool {
		return slices.Contains(excludedPaths, path)
	})
	assert.ElementsMatch(t, expectedPaths, comparedPaths)
	assertEveryRequestWasARead(t, apiServer)
}

func assertEveryRequestWasARead(t *testing.T, apiServer *stubAPIServer) {
	t.Helper()

	for _, request := range apiServer.recordedRequests() {
		assert.Equal(t, http.MethodGet, request.method, "unexpected %s to %s", request.method, request.path)
	}
}

// The Content-Type header is what client-go uses to choose a decoder; without it Go's
// DetectContentType labels a JSON body text/plain and decoding fails with "no serializer".
func respondWithJSON(statusCode int, responseObject any) http.HandlerFunc {
	return func(w http.ResponseWriter, _ *http.Request) {
		body, err := json.Marshal(responseObject)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(statusCode)
		_, _ = w.Write(body)
	}
}

func respondWithRawBody(statusCode int, body string) http.HandlerFunc {
	return func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(statusCode)
		_, _ = w.Write([]byte(body))
	}
}

func respondWithFailureStatus(statusCode int32, reason metav1.StatusReason, message string) http.HandlerFunc {
	return respondWithJSON(int(statusCode), &metav1.Status{
		TypeMeta: metav1.TypeMeta{APIVersion: "v1", Kind: "Status"},
		Status:   metav1.StatusFailure,
		Message:  message,
		Reason:   reason,
		Code:     statusCode,
	})
}

func respondNotFound() http.HandlerFunc {
	return respondWithFailureStatus(http.StatusNotFound, metav1.StatusReasonNotFound,
		"the server could not find the requested resource")
}

// respondServerError deliberately omits Retry-After. client-go only retries a 5xx that carries it,
// so the request fails immediately instead of sleeping through ten retries.
func respondServerError() http.HandlerFunc {
	return respondWithFailureStatus(http.StatusInternalServerError, metav1.StatusReasonInternalError,
		"the server encountered an internal error")
}

func respondForbidden() http.HandlerFunc {
	return respondWithFailureStatus(http.StatusForbidden, metav1.StatusReasonForbidden,
		"forbidden: user cannot get the requested resource")
}

// brokenRESTConfig makes every *client.NewForConfig call fail: transport.TLSConfigFor rejects a
// config that supplies CA data alongside Insecure.
func brokenRESTConfig() *rest.Config {
	return &rest.Config{
		Host:            "https://127.0.0.1:1",
		TLSClientConfig: rest.TLSClientConfig{Insecure: true, CAData: []byte("not-a-cert")},
	}
}

func testContext(t *testing.T) context.Context {
	return log.IntoContext(t.Context(), logr.Discard())
}

// requireAPIStatusError discriminates on the typed error the client built rather than on the stubs'
// own message text, so it also fails if production code drops the %w that carries the cause.
func requireAPIStatusError(t *testing.T, err error, expectedReason metav1.StatusReason, expectedCode int32) {
	t.Helper()

	var statusError *apierrors.StatusError
	require.ErrorAs(t, err, &statusError)
	require.Equal(t, expectedReason, statusError.Status().Reason)
	require.Equal(t, expectedCode, statusError.Status().Code)
}

func clusterVersion(history ...configv1.UpdateHistory) *configv1.ClusterVersion {
	return &configv1.ClusterVersion{
		TypeMeta:   metav1.TypeMeta{APIVersion: configv1.GroupVersion.String(), Kind: "ClusterVersion"},
		ObjectMeta: metav1.ObjectMeta{Name: "version"},
		Status:     configv1.ClusterVersionStatus{History: history},
	}
}

func updateHistory(state configv1.UpdateState, version string) configv1.UpdateHistory {
	return configv1.UpdateHistory{State: state, Version: version}
}

func clusterProxy(spec configv1.ProxySpec) *configv1.Proxy {
	return &configv1.Proxy{
		TypeMeta:   metav1.TypeMeta{APIVersion: configv1.GroupVersion.String(), Kind: "Proxy"},
		ObjectMeta: metav1.ObjectMeta{Name: "cluster"},
		Spec:       spec,
	}
}

func driverToolkitImageStream(tags ...imagev1.TagReference) *imagev1.ImageStream {
	return &imagev1.ImageStream{
		TypeMeta:   metav1.TypeMeta{APIVersion: imagev1.GroupVersion.String(), Kind: "ImageStream"},
		ObjectMeta: metav1.ObjectMeta{Name: "driver-toolkit", Namespace: consts.OpenshiftNamespace},
		Spec:       imagev1.ImageStreamSpec{Tags: tags},
	}
}

func imageStreamTag(name, image string) imagev1.TagReference {
	return imagev1.TagReference{
		Name: name,
		From: &corev1.ObjectReference{Kind: "DockerImage", Name: image},
	}
}

// Real ImageStreams carry both the ImageStreamTag and the DockerImage reference forms.
func imageStreamTagReferencingTag(name, referencedTag string) imagev1.TagReference {
	return imagev1.TagReference{
		Name: name,
		From: &corev1.ObjectReference{Kind: "ImageStreamTag", Name: referencedTag},
	}
}

func imageStreamTagWithoutFrom(name string) imagev1.TagReference {
	return imagev1.TagReference{Name: name}
}

func gpuNodeList(containerRuntimeVersions ...string) *corev1.NodeList {
	nodes := make([]corev1.Node, 0, len(containerRuntimeVersions))
	for index, containerRuntimeVersion := range containerRuntimeVersions {
		nodes = append(nodes, corev1.Node{
			ObjectMeta: metav1.ObjectMeta{
				Name:   fmt.Sprintf("gpu-node-%d", index),
				Labels: map[string]string{consts.GPUPresentLabel: "true"},
			},
			Status: corev1.NodeStatus{
				NodeInfo: corev1.NodeSystemInfo{ContainerRuntimeVersion: containerRuntimeVersion},
			},
		})
	}
	return &corev1.NodeList{
		TypeMeta: metav1.TypeMeta{APIVersion: "v1", Kind: "NodeList"},
		Items:    nodes,
	}
}

// The first version listed is the group's preferred version, which is the one discovery keeps for
// a resource served by several.
type discoveredAPIGroup struct {
	name                      string
	versionsInPreferenceOrder []string
	resources                 []metav1.APIResource
	// Discovery deduplicates on group plus resource name, so only per-version resource sets let two
	// versions of a resource both survive.
	resourcesByVersion   map[string][]metav1.APIResource
	versionEndpointsFail bool
}

func (group discoveredAPIGroup) resourcesServedAt(version string) []metav1.APIResource {
	if resourcesForVersion, found := group.resourcesByVersion[version]; found {
		return resourcesForVersion
	}
	return group.resources
}

func draGVR(version string) schema.GroupVersionResource {
	return schema.GroupVersionResource{Group: "resource.k8s.io", Version: version, Resource: "deviceclasses"}
}

// draAPIGroup lists the group's other resources ahead of deviceclasses, so the scan has to walk
// past non-matching entries rather than finding its match at index 0.
func draAPIGroup(versionsInPreferenceOrder ...string) discoveredAPIGroup {
	return discoveredAPIGroup{
		name:                      "resource.k8s.io",
		versionsInPreferenceOrder: versionsInPreferenceOrder,
		resources: []metav1.APIResource{
			{Name: "resourceclaims", Kind: "ResourceClaim"},
			{Name: "resourceclaimtemplates", Kind: "ResourceClaimTemplate"},
			{Name: "resourceslices", Kind: "ResourceSlice"},
			deviceClassAPIResource(),
		},
	}
}

// A group that is advertised but whose version endpoints all fail is what drives
// ServerPreferredResources into its partial-failure path.
func unreachableDRAAPIGroup(versionsInPreferenceOrder ...string) discoveredAPIGroup {
	draGroup := draAPIGroup(versionsInPreferenceOrder...)
	draGroup.versionEndpointsFail = true
	return draGroup
}

func unrelatedDeviceClassAPIGroup() discoveredAPIGroup {
	return discoveredAPIGroup{
		name:                      "devices.example.com",
		versionsInPreferenceOrder: []string{"v1"},
		resources:                 []metav1.APIResource{deviceClassAPIResource()},
	}
}

func deviceClassAPIResource() metav1.APIResource {
	return metav1.APIResource{Name: "deviceclasses", Kind: "DeviceClass"}
}

// ServerPreferredResources fetches every version of every group, not only each group's preferred
// version, so each listed version needs a handler.
func discoveryHandlers(groups ...discoveredAPIGroup) map[string]http.HandlerFunc {
	handlers := map[string]http.HandlerFunc{
		pathAPIVersions: respondWithJSON(http.StatusOK, apiVersions("v1")),
		pathAPIGroups:   respondWithJSON(http.StatusOK, apiGroupList(groups...)),
		pathCoreResources: respondWithJSON(http.StatusOK,
			apiResourceList("v1", metav1.APIResource{Name: "nodes", Kind: "Node"})),
	}
	for _, group := range groups {
		for _, version := range group.versionsInPreferenceOrder {
			groupVersion := group.name + "/" + version
			if group.versionEndpointsFail {
				handlers[pathGroupVersionResources(groupVersion)] = respondServerError()
				continue
			}
			handlers[pathGroupVersionResources(groupVersion)] = respondWithJSON(http.StatusOK,
				apiResourceList(groupVersion, group.resourcesServedAt(version)...))
		}
	}
	return handlers
}

// Must stay in sync with discoveryHandlers.
func discoveryPaths(groups ...discoveredAPIGroup) []string {
	paths := []string{pathAPIVersions, pathAPIGroups, pathCoreResources}
	for _, group := range groups {
		for _, version := range group.versionsInPreferenceOrder {
			paths = append(paths, pathGroupVersionResources(group.name+"/"+version))
		}
	}
	return paths
}

func mergeHandlers(handlerMaps ...map[string]http.HandlerFunc) map[string]http.HandlerFunc {
	mergedHandlers := map[string]http.HandlerFunc{}
	for _, handlerMap := range handlerMaps {
		maps.Copy(mergedHandlers, handlerMap)
	}
	return mergedHandlers
}

var openshiftClusterDRAGroups = []discoveredAPIGroup{draAPIGroup("v1")}

var openshiftClusterHandlers = mergeHandlers(discoveryHandlers(openshiftClusterDRAGroups...),
	map[string]http.HandlerFunc{
		pathClusterVersion: respondWithJSON(http.StatusOK,
			clusterVersion(updateHistory(configv1.CompletedUpdate, "4.14.10"))),
		pathNodes: respondWithJSON(http.StatusOK, gpuNodeList("cri-o://1.28.2")),
		pathDriverToolkitImageStream: respondWithJSON(http.StatusOK, driverToolkitImageStream(
			imageStreamTag("410.84", "quay.io/openshift/driver-toolkit@sha256:aaa"),
			imageStreamTag("412.86", "quay.io/openshift/driver-toolkit@sha256:bbb"),
		)),
		pathProxy: respondWithJSON(http.StatusOK, clusterProxy(configv1.ProxySpec{
			HTTPProxy: "http://proxy.example.com:3128",
		})),
	})

func apiVersions(versions ...string) *metav1.APIVersions {
	return &metav1.APIVersions{
		TypeMeta: metav1.TypeMeta{APIVersion: "v1", Kind: "APIVersions"},
		Versions: versions,
	}
}

func apiGroupList(groups ...discoveredAPIGroup) *metav1.APIGroupList {
	groupList := &metav1.APIGroupList{
		TypeMeta: metav1.TypeMeta{APIVersion: "v1", Kind: "APIGroupList"},
	}
	for _, group := range groups {
		groupList.Groups = append(groupList.Groups, apiGroup(group))
	}
	return groupList
}

func apiGroup(group discoveredAPIGroup) metav1.APIGroup {
	advertisedGroup := metav1.APIGroup{Name: group.name}
	for _, version := range group.versionsInPreferenceOrder {
		advertisedGroup.Versions = append(advertisedGroup.Versions, metav1.GroupVersionForDiscovery{
			GroupVersion: group.name + "/" + version,
			Version:      version,
		})
	}
	if len(advertisedGroup.Versions) == 0 {
		panic(fmt.Sprintf("API group fixture %q lists no versions, so it has no preferred version", group.name))
	}
	advertisedGroup.PreferredVersion = advertisedGroup.Versions[0]
	return advertisedGroup
}

func apiResourceList(groupVersion string, resources ...metav1.APIResource) *metav1.APIResourceList {
	return &metav1.APIResourceList{
		TypeMeta:     metav1.TypeMeta{APIVersion: "v1", Kind: "APIResourceList"},
		GroupVersion: groupVersion,
		APIResources: resources,
	}
}
