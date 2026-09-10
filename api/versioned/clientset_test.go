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

package versioned

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/discovery"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/util/flowcontrol"

	nvidiav1 "github.com/NVIDIA/gpu-operator/api/nvidia/v1"
	nvidiav1alpha1 "github.com/NVIDIA/gpu-operator/api/nvidia/v1alpha1"
	"github.com/NVIDIA/gpu-operator/api/versioned/scheme"
	typednvidiav1 "github.com/NVIDIA/gpu-operator/api/versioned/typed/nvidia/v1"
	typednvidiav1alpha1 "github.com/NVIDIA/gpu-operator/api/versioned/typed/nvidia/v1alpha1"
)

// *Clientset must satisfy the generated Interface. This is a compile-time check.
var _ Interface = (*Clientset)(nil)

const testHost = "https://gpu-operator.test:6443"

// recordedRequest is what the httptest handler observed for a single request.
type recordedRequest struct {
	path   string
	header http.Header
}

// requestRecorder records the requests observed by an httptest handler. The
// handler runs on the server goroutine while assertions run on the test
// goroutine, so access is serialized by the mutex and nothing here calls into
// testify.
type requestRecorder struct {
	requestsMutex sync.Mutex
	requests      []recordedRequest
}

func (recorder *requestRecorder) record(incomingRequest *http.Request) {
	recorder.requestsMutex.Lock()
	defer recorder.requestsMutex.Unlock()
	recorder.requests = append(recorder.requests, recordedRequest{
		path:   incomingRequest.URL.Path,
		header: incomingRequest.Header.Clone(),
	})
}

// recordedRequests must be called from the test goroutine, once the client
// calls have completed.
func (recorder *requestRecorder) recordedRequests() []recordedRequest {
	recorder.requestsMutex.Lock()
	defer recorder.requestsMutex.Unlock()
	return append([]recordedRequest(nil), recorder.requests...)
}

func (recorder *requestRecorder) recordedPaths() []string {
	paths := []string{}
	for _, request := range recorder.recordedRequests() {
		paths = append(paths, request.path)
	}
	return paths
}

// countingRoundTripper counts every request that reaches the transport it wraps.
type countingRoundTripper struct {
	delegate     http.RoundTripper
	requestCount atomic.Int64
}

func (roundTripper *countingRoundTripper) RoundTrip(request *http.Request) (*http.Response, error) {
	roundTripper.requestCount.Add(1)
	return roundTripper.delegate.RoundTrip(request)
}

// minimalConfig is the smallest *rest.Config every constructor should accept.
func minimalConfig() *rest.Config {
	return &rest.Config{Host: testHost}
}

// configWithMissingCABundle returns a *rest.Config that makes rest.HTTPClientFor
// fail, because the referenced CA bundle does not exist on disk.
func configWithMissingCABundle(t *testing.T) *rest.Config {
	t.Helper()
	return &rest.Config{
		Host: testHost,
		TLSClientConfig: rest.TLSClientConfig{
			CAFile: filepath.Join(t.TempDir(), "does-not-exist-ca.crt"),
		},
	}
}

// newTestRESTClient builds a real *rest.RESTClient so that New can be verified
// against a concrete rest.Interface implementation.
func newTestRESTClient(t *testing.T) *rest.RESTClient {
	t.Helper()
	groupVersion := schema.GroupVersion{Group: "nvidia.com", Version: "v1"}
	restClient, err := rest.RESTClientFor(&rest.Config{
		Host:    testHost,
		APIPath: "/apis",
		ContentConfig: rest.ContentConfig{
			GroupVersion:         &groupVersion,
			NegotiatedSerializer: scheme.Codecs.WithoutConversion(),
		},
	})
	require.NoError(t, err)
	return restClient
}

// newGroupAPIServerAndRecorder starts a server that answers exactly the requests
// the tests below issue, and records each one.
func newGroupAPIServerAndRecorder(t *testing.T) (*httptest.Server, *requestRecorder) {
	t.Helper()
	recorder := &requestRecorder{}
	server := httptest.NewServer(http.HandlerFunc(func(responseWriter http.ResponseWriter, incomingRequest *http.Request) {
		recorder.record(incomingRequest)
		responseWriter.Header().Set("Content-Type", "application/json")
		switch incomingRequest.URL.Path {
		case "/apis/nvidia.com/v1/clusterpolicies":
			_ = json.NewEncoder(responseWriter).Encode(&nvidiav1.ClusterPolicyList{
				Items: []nvidiav1.ClusterPolicy{{ObjectMeta: metav1.ObjectMeta{Name: "cluster-policy"}}},
			})
		case "/apis/nvidia.com/v1alpha1/nvidiadrivers":
			_ = json.NewEncoder(responseWriter).Encode(&nvidiav1alpha1.NVIDIADriverList{
				Items: []nvidiav1alpha1.NVIDIADriver{{ObjectMeta: metav1.ObjectMeta{Name: "nvidia-driver"}}},
			})
		case "/apis/nvidia.com/v1alpha1/gpuclusters":
			_ = json.NewEncoder(responseWriter).Encode(&nvidiav1alpha1.GPUClusterList{
				Items: []nvidiav1alpha1.GPUCluster{{ObjectMeta: metav1.ObjectMeta{Name: "gpu-cluster"}}},
			})
		case "/apis/nvidia.com/v1":
			_ = json.NewEncoder(responseWriter).Encode(&metav1.APIResourceList{
				GroupVersion: "nvidia.com/v1",
				APIResources: []metav1.APIResource{{Name: "clusterpolicies", Kind: "ClusterPolicy"}},
			})
		default:
			responseWriter.WriteHeader(http.StatusNotFound)
		}
	}))
	t.Cleanup(server.Close)
	return server, recorder
}

func TestNewForConfig(t *testing.T) {
	t.Run("succeeds on a minimal config and wires every client", func(t *testing.T) {
		clientset, err := NewForConfig(minimalConfig())
		require.NoError(t, err)
		require.NotNil(t, clientset)
		require.NotNil(t, clientset.DiscoveryClient)

		// Each group client is configured for its own group/version.
		assert.Equal(t, nvidiav1.SchemeGroupVersion, clientset.NvidiaV1().RESTClient().APIVersion())
		assert.Equal(t, nvidiav1alpha1.SchemeGroupVersion, clientset.NvidiaV1alpha1().RESTClient().APIVersion())
	})

	t.Run("returns the transport error when the CA bundle cannot be loaded", func(t *testing.T) {
		clientset, err := NewForConfig(configWithMissingCABundle(t))
		require.Error(t, err)
		assert.Nil(t, clientset)
		assert.Contains(t, err.Error(), "does-not-exist-ca.crt")
	})
}

func TestNewForConfigUserAgent(t *testing.T) {
	testCases := []struct {
		name              string
		userAgent         string
		expectedUserAgent string
	}{
		{
			name:              "empty user agent is defaulted",
			userAgent:         "",
			expectedUserAgent: rest.DefaultKubernetesUserAgent(),
		},
		{
			name:              "caller supplied user agent is preserved",
			userAgent:         "gpu-operator-test/1.2.3",
			expectedUserAgent: "gpu-operator-test/1.2.3",
		},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			server, recorder := newGroupAPIServerAndRecorder(t)

			clientset, err := NewForConfig(&rest.Config{Host: server.URL, UserAgent: testCase.userAgent})
			require.NoError(t, err)

			_, err = clientset.NvidiaV1().ClusterPolicies().List(t.Context(), metav1.ListOptions{})
			require.NoError(t, err)

			recordedRequests := recorder.recordedRequests()
			require.Len(t, recordedRequests, 1)
			assert.Equal(t, testCase.expectedUserAgent, recordedRequests[0].header.Get("User-Agent"))
		})
	}
}

func TestConstructorsDoNotMutateCallerConfig(t *testing.T) {
	testCases := []struct {
		name      string
		construct func(*rest.Config) (*Clientset, error)
	}{
		{
			name:      "NewForConfig",
			construct: NewForConfig,
		},
		{
			name: "NewForConfigAndClient",
			construct: func(config *rest.Config) (*Clientset, error) {
				return NewForConfigAndClient(config, &http.Client{})
			},
		},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			config := minimalConfig()
			config.QPS = 25
			config.Burst = 50

			clientset, err := testCase.construct(config)
			require.NoError(t, err)
			require.NotNil(t, clientset)

			// User agent defaulting and rate-limiter construction both happen on
			// shallow copies, so the caller's config is untouched.
			assert.Empty(t, config.UserAgent)
			assert.Nil(t, config.RateLimiter)
			assert.Equal(t, float32(25), config.QPS)
			assert.Equal(t, 50, config.Burst)
		})
	}
}

func TestNewForConfigAppliesTheConfiguredTimeout(t *testing.T) {
	handlerReleased := make(chan struct{})
	server := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		<-handlerReleased
	}))
	defer server.Close()
	// The handler must be released before the server is closed, because
	// Close waits for outstanding requests to finish.
	defer close(handlerReleased)

	clientset, err := NewForConfig(&rest.Config{Host: server.URL, Timeout: 50 * time.Millisecond})
	require.NoError(t, err)

	// The outer deadline only bounds the failure when the config timeout is
	// not honoured; it must never be the deadline that fires.
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()

	startedAt := time.Now()
	_, err = clientset.NvidiaV1().ClusterPolicies().List(ctx, metav1.ListOptions{})
	elapsed := time.Since(startedAt)
	require.Error(t, err)

	// rest applies Config.Timeout twice, as http.Client.Timeout and again as a
	// per-request context deadline, so which of the two timeout messages wins is
	// a race; url.Error.Timeout() is true for both.
	var urlErr *url.Error
	require.ErrorAs(t, err, &urlErr)
	assert.True(t, urlErr.Timeout(), "the config Timeout must be what cut the request short")
	assert.Lessf(t, elapsed, 2*time.Second, "the 50ms config timeout must fire long before the outer deadline, took %s", elapsed)
}

func TestNewForConfigAndClient(t *testing.T) {
	t.Run("propagates a group client construction error verbatim", func(t *testing.T) {
		config := &rest.Config{Host: "http://[::1]:not-a-port"}
		httpClient := &http.Client{}

		clientset, err := NewForConfigAndClient(config, httpClient)
		require.Error(t, err)
		assert.Nil(t, clientset)

		// Every group constructor rejects this host through the same code path and
		// returns byte-identical text, so this pins the wording, not which one ran:
		// no rest.Config can fail a later constructor without failing the first,
		// which leaves those error returns unreachable.
		_, v1ClientErr := typednvidiav1.NewForConfigAndClient(config, httpClient)
		require.Error(t, v1ClientErr)
		assert.EqualError(t, err, v1ClientErr.Error())
	})

	t.Run("QPS with a positive burst installs one rate limiter on every client", func(t *testing.T) {
		config := minimalConfig()
		config.QPS = 5
		config.Burst = 2

		clientset, err := NewForConfigAndClient(config, &http.Client{})
		require.NoError(t, err)
		require.NotNil(t, clientset)

		v1Limiter := clientset.NvidiaV1().RESTClient().GetRateLimiter()
		v1alpha1Limiter := clientset.NvidiaV1alpha1().RESTClient().GetRateLimiter()
		require.NotNil(t, v1Limiter)
		// A single limiter is generated in the shallow copy and shared by all
		// group clients built from it, discovery included.
		assert.Same(t, v1Limiter, v1alpha1Limiter)
		assert.Same(t, v1Limiter, clientset.Discovery().RESTClient().GetRateLimiter())
		assert.Equal(t, config.QPS, v1Limiter.QPS())

		// The caller's config keeps its nil RateLimiter.
		assert.Nil(t, config.RateLimiter)
	})

	t.Run("a pre-set rate limiter is left alone", func(t *testing.T) {
		limiter := flowcontrol.NewTokenBucketRateLimiter(1, 1)
		config := minimalConfig()
		config.RateLimiter = limiter
		// Burst is invalid, but must not be validated because RateLimiter is set.
		config.QPS = 5
		config.Burst = 0

		clientset, err := NewForConfigAndClient(config, &http.Client{})
		require.NoError(t, err)
		require.NotNil(t, clientset)

		assert.Same(t, limiter, clientset.NvidiaV1().RESTClient().GetRateLimiter())
		assert.Same(t, limiter, clientset.NvidiaV1alpha1().RESTClient().GetRateLimiter())
		assert.Same(t, limiter, config.RateLimiter)
	})

	t.Run("no rate limiter is generated when QPS is not positive", func(t *testing.T) {
		config := minimalConfig()
		config.QPS = -1

		clientset, err := NewForConfigAndClient(config, &http.Client{})
		require.NoError(t, err)
		require.NotNil(t, clientset)
		assert.Nil(t, config.RateLimiter)

		// Nothing is installed in the shallow copy, and a negative QPS also
		// suppresses the rest package's own per-client default, so the group
		// clients end up unthrottled. The interface must hold an untyped nil,
		// which assert.Nil alone would not distinguish from a typed nil.
		var nilRateLimiter flowcontrol.RateLimiter
		assert.Equal(t, nilRateLimiter, clientset.NvidiaV1().RESTClient().GetRateLimiter())
		assert.Equal(t, nilRateLimiter, clientset.NvidiaV1alpha1().RESTClient().GetRateLimiter())
	})
}

func TestNewForConfigAndClientSharesOneTransport(t *testing.T) {
	server, _ := newGroupAPIServerAndRecorder(t)
	sharedTransport := &countingRoundTripper{delegate: http.DefaultTransport}

	clientset, err := NewForConfigAndClient(&rest.Config{Host: server.URL}, &http.Client{Transport: sharedTransport})
	require.NoError(t, err)

	_, err = clientset.NvidiaV1().ClusterPolicies().List(t.Context(), metav1.ListOptions{})
	require.NoError(t, err)
	_, err = clientset.NvidiaV1alpha1().NVIDIADrivers().List(t.Context(), metav1.ListOptions{})
	require.NoError(t, err)
	_, err = clientset.Discovery().ServerResourcesForGroupVersion(nvidiav1.SchemeGroupVersion.String())
	require.NoError(t, err)

	// Every group client, and discovery, went through the caller's transport.
	assert.Equal(t, int64(3), sharedTransport.requestCount.Load())
}

func TestNewForConfigOrDie(t *testing.T) {
	t.Run("returns a clientset for a minimal config", func(t *testing.T) {
		var clientset *Clientset
		require.NotPanics(t, func() {
			clientset = NewForConfigOrDie(minimalConfig())
		})
		require.NotNil(t, clientset)
	})

	t.Run("panics with the error NewForConfig returned", func(t *testing.T) {
		config := minimalConfig()
		config.QPS = 10
		config.Burst = -5
		require.PanicsWithError(t,
			"burst is required to be greater than 0 when RateLimiter is not set and QPS is set to greater than 0",
			func() {
				_ = NewForConfigOrDie(config)
			})
	})
}

func TestNew(t *testing.T) {
	restClient := newTestRESTClient(t)

	clientset := New(restClient)
	require.NotNil(t, clientset)

	// Every group client, and discovery, must be backed by the exact RESTClient
	// that was handed to New, and reached through the generated accessors.
	v1Client := clientset.NvidiaV1()
	assert.IsType(t, &typednvidiav1.NvidiaV1Client{}, v1Client)
	assert.Same(t, clientset.nvidiaV1, v1Client)
	assert.Same(t, restClient, v1Client.RESTClient())
	assert.NotNil(t, v1Client.ClusterPolicies())

	v1alpha1Client := clientset.NvidiaV1alpha1()
	assert.IsType(t, &typednvidiav1alpha1.NvidiaV1alpha1Client{}, v1alpha1Client)
	assert.Same(t, clientset.nvidiaV1alpha1, v1alpha1Client)
	assert.Same(t, restClient, v1alpha1Client.RESTClient())
	assert.NotNil(t, v1alpha1Client.GPUClusters())
	assert.NotNil(t, v1alpha1Client.NVIDIADrivers())

	discoveryClient := clientset.Discovery()
	assert.IsType(t, &discovery.DiscoveryClient{}, discoveryClient)
	assert.Same(t, clientset.DiscoveryClient, discoveryClient)
	assert.Same(t, restClient, discoveryClient.RESTClient())
}

func TestDiscoveryNilReceiver(t *testing.T) {
	var clientset *Clientset
	gotDiscoveryClient := clientset.Discovery()

	// The explicit nil check must return an untyped nil, not a typed nil
	// wrapped in the interface, which assert.Nil alone would also accept.
	var nilDiscoveryInterface discovery.DiscoveryInterfaces
	assert.Equal(t, nilDiscoveryInterface, gotDiscoveryClient)
}

// TestClientsetRoundTrip exercises the wiring end to end: a clientset built by
// NewForConfig must reach the right API paths for discovery and for each group.
func TestClientsetRoundTrip(t *testing.T) {
	server, recorder := newGroupAPIServerAndRecorder(t)

	clientset, err := NewForConfig(&rest.Config{Host: server.URL})
	require.NoError(t, err)

	clusterPolicyList, err := clientset.NvidiaV1().ClusterPolicies().List(t.Context(), metav1.ListOptions{})
	require.NoError(t, err)
	require.Len(t, clusterPolicyList.Items, 1)
	assert.Equal(t, "cluster-policy", clusterPolicyList.Items[0].Name)

	_, err = clientset.NvidiaV1alpha1().NVIDIADrivers().List(t.Context(), metav1.ListOptions{})
	require.NoError(t, err)

	_, err = clientset.NvidiaV1alpha1().GPUClusters().List(t.Context(), metav1.ListOptions{})
	require.NoError(t, err)

	apiResourceList, err := clientset.Discovery().ServerResourcesForGroupVersion("nvidia.com/v1")
	require.NoError(t, err)
	require.Len(t, apiResourceList.APIResources, 1)
	assert.Equal(t, "clusterpolicies", apiResourceList.APIResources[0].Name)

	assert.Equal(t, []string{
		"/apis/nvidia.com/v1/clusterpolicies",
		"/apis/nvidia.com/v1alpha1/nvidiadrivers",
		"/apis/nvidia.com/v1alpha1/gpuclusters",
		"/apis/nvidia.com/v1",
	}, recorder.recordedPaths())
}
