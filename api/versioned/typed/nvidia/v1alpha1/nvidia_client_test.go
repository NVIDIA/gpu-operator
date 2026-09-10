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
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/url"
	"os"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	runtime "k8s.io/apimachinery/pkg/runtime"
	rest "k8s.io/client-go/rest"

	nvidiav1alpha1 "github.com/NVIDIA/gpu-operator/api/nvidia/v1alpha1"
)

// Compile-time assertion that the generated client satisfies the group interface.
var _ NvidiaV1alpha1Interface = &NvidiaV1alpha1Client{}

// unreachableAPIServerHost is in the range RFC 5737 reserves for documentation.
// Nothing listens there, so any request that appears to succeed against it must
// have been answered by a test transport rather than by the network.
const unreachableAPIServerHost = "https://192.0.2.1:6443"

// callerSuppliedSerializer is a sentinel NegotiatedSerializer used to prove that
// setConfigDefaults replaces a caller's value outright. The embedded interface
// supplies the method set; nothing ever calls through it.
type callerSuppliedSerializer struct {
	runtime.NegotiatedSerializer
}

// recordingRoundTripper answers every request with a canned JSON response and
// records what it saw.
type recordingRoundTripper struct {
	responseBody   []byte
	requestCount   int
	lastRequestURL *url.URL
}

func (roundTripper *recordingRoundTripper) RoundTrip(request *http.Request) (*http.Response, error) {
	roundTripper.requestCount++
	roundTripper.lastRequestURL = request.URL

	return &http.Response{
		StatusCode: http.StatusOK,
		Header:     http.Header{"Content-Type": []string{"application/json"}},
		Body:       io.NopCloser(bytes.NewReader(roundTripper.responseBody)),
		Request:    request,
	}, nil
}

func TestSetConfigDefaults(t *testing.T) {
	testCases := []struct {
		name              string
		config            rest.Config
		expectedUserAgent string
	}{
		{
			name:              "empty user agent gets the default kubernetes user agent",
			config:            rest.Config{},
			expectedUserAgent: rest.DefaultKubernetesUserAgent(),
		},
		{
			name:              "caller supplied user agent is preserved",
			config:            rest.Config{UserAgent: "gpu-operator-tests/1.2.3"},
			expectedUserAgent: "gpu-operator-tests/1.2.3",
		},
	}
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			config := testCase.config
			setConfigDefaults(&config)

			require.NotNil(t, config.GroupVersion)
			assert.Equal(t, nvidiav1alpha1.SchemeGroupVersion, *config.GroupVersion)
			assert.Equal(t, "nvidia.com", config.GroupVersion.Group)
			assert.Equal(t, "v1alpha1", config.GroupVersion.Version)
			assert.Equal(t, "/apis", config.APIPath)
			assert.NotNil(t, config.NegotiatedSerializer)
			assert.Equal(t, testCase.expectedUserAgent, config.UserAgent)
		})
	}

	t.Run("overwrites a caller supplied negotiated serializer", func(t *testing.T) {
		config := rest.Config{}
		config.NegotiatedSerializer = callerSuppliedSerializer{}
		setConfigDefaults(&config)

		require.NotNil(t, config.NegotiatedSerializer)
		_, stillCallerSupplied := config.NegotiatedSerializer.(callerSuppliedSerializer)
		assert.False(t, stillCallerSupplied,
			"the generated client must decode with the group's own scheme, not whatever the caller configured")
	})

	t.Run("overwrites a stale group version", func(t *testing.T) {
		staleGroupVersion := nvidiav1alpha1.SchemeGroupVersion
		staleGroupVersion.Group = "example.com"
		config := rest.Config{APIPath: "/api"}
		config.GroupVersion = &staleGroupVersion
		setConfigDefaults(&config)

		require.NotNil(t, config.GroupVersion)
		assert.Equal(t, "nvidia.com", config.GroupVersion.Group)
		assert.Equal(t, "/apis", config.APIPath)
		// Installing a fresh GroupVersion rather than writing through the
		// caller's pointer is part of the contract: the caller's value must
		// come back unchanged.
		assert.Equal(t, "example.com", staleGroupVersion.Group)
	})
}

func TestSetConfigDefaultsNegotiatedSerializerSupportsJSON(t *testing.T) {
	config := rest.Config{}
	setConfigDefaults(&config)

	require.NotNil(t, config.NegotiatedSerializer)

	var mediaTypes []string
	for _, mediaTypeInfo := range config.NegotiatedSerializer.SupportedMediaTypes() {
		mediaTypes = append(mediaTypes, mediaTypeInfo.MediaType)
	}
	assert.Contains(t, mediaTypes, "application/json")
}

func TestNewForConfig(t *testing.T) {
	t.Run("succeeds for a minimal config", func(t *testing.T) {
		client, err := NewForConfig(&rest.Config{Host: unreachableAPIServerHost})
		require.NoError(t, err)
		require.NotNil(t, client)
		require.NotNil(t, client.RESTClient())

		// The REST client must have been built against the group's API path.
		gotURL := client.RESTClient().Get().URL()
		assert.Equal(t, "/apis/nvidia.com/v1alpha1", gotURL.Path)
	})

	t.Run("does not mutate the caller's config", func(t *testing.T) {
		callerConfig := &rest.Config{Host: unreachableAPIServerHost}
		_, err := NewForConfig(callerConfig)
		require.NoError(t, err)

		assert.Nil(t, callerConfig.GroupVersion, "NewForConfig must operate on a copy")
		assert.Empty(t, callerConfig.APIPath)
		assert.Empty(t, callerConfig.UserAgent)
		assert.Nil(t, callerConfig.NegotiatedSerializer)
	})

	// One representative failure is enough to prove NewForConfig surfaces
	// rest.HTTPClientFor errors instead of swallowing them. The error text
	// itself belongs to client-go and the OS, so it is not asserted on; the
	// typed *os.PathError is the stable part.
	t.Run("propagates rest.HTTPClientFor errors", func(t *testing.T) {
		client, err := NewForConfig(&rest.Config{
			Host: unreachableAPIServerHost,
			TLSClientConfig: rest.TLSClientConfig{
				CAFile: "/does/not/exist/ca.crt",
			},
		})
		require.Error(t, err)
		assert.Nil(t, client)

		var pathErr *os.PathError
		require.ErrorAs(t, err, &pathErr)
		assert.Equal(t, "/does/not/exist/ca.crt", pathErr.Path)
	})
}

func TestNewForConfigAndClient(t *testing.T) {
	t.Run("sends requests through the supplied http client", func(t *testing.T) {
		responseBody, err := json.Marshal(newGPUCluster(gpuClusterSingletonName))
		require.NoError(t, err)
		roundTripper := &recordingRoundTripper{responseBody: responseBody}

		client, err := NewForConfigAndClient(&rest.Config{Host: unreachableAPIServerHost}, &http.Client{Transport: roundTripper})
		require.NoError(t, err)
		require.NotNil(t, client)
		assert.Equal(t, "/apis/nvidia.com/v1alpha1", client.RESTClient().Get().URL().Path)

		gotGPUCluster, err := client.GPUClusters().Get(t.Context(), gpuClusterSingletonName, metav1.GetOptions{})
		require.NoError(t, err)
		assert.Equal(t, gpuClusterSingletonName, gotGPUCluster.Name)

		require.Equal(t, 1, roundTripper.requestCount, "the request must have travelled over the supplied http client")
		require.NotNil(t, roundTripper.lastRequestURL)
		assert.Equal(t, gpuClusterNamedPath(gpuClusterSingletonName), roundTripper.lastRequestURL.Path)
	})

	t.Run("does not mutate the caller's config", func(t *testing.T) {
		callerConfig := &rest.Config{Host: unreachableAPIServerHost}
		_, err := NewForConfigAndClient(callerConfig, &http.Client{})
		require.NoError(t, err)

		assert.Nil(t, callerConfig.GroupVersion, "NewForConfigAndClient must operate on a copy")
		assert.Empty(t, callerConfig.APIPath)
		assert.Empty(t, callerConfig.UserAgent)
		assert.Nil(t, callerConfig.NegotiatedSerializer)
	})

	// One malformed host is enough to prove the error is propagated rather
	// than swallowed. The message is produced by client-go and net/url and
	// changes across versions without any change to the generated client, so
	// it is deliberately not asserted on.
	t.Run("propagates RESTClientForConfigAndClient errors", func(t *testing.T) {
		client, err := NewForConfigAndClient(&rest.Config{Host: "://malformed"}, &http.Client{})
		require.Error(t, err)
		assert.Nil(t, client)
	})
}

func TestNewForConfigOrDie(t *testing.T) {
	t.Run("returns a client for a good config", func(t *testing.T) {
		var client *NvidiaV1alpha1Client
		require.NotPanics(t, func() {
			client = NewForConfigOrDie(&rest.Config{Host: unreachableAPIServerHost})
		})
		require.NotNil(t, client)
		assert.NotNil(t, client.RESTClient())
	})

	t.Run("panics for a bad config", func(t *testing.T) {
		assert.Panics(t, func() {
			NewForConfigOrDie(&rest.Config{
				Host: unreachableAPIServerHost,
				TLSClientConfig: rest.TLSClientConfig{
					CAFile: "/does/not/exist/ca.crt",
				},
			})
		})
	})
}

func TestNew(t *testing.T) {
	restClient, err := rest.RESTClientFor(newTestRESTConfig(t))
	require.NoError(t, err)

	client := New(restClient)
	require.NotNil(t, client)
	assert.Same(t, restClient, client.RESTClient(), "New must wrap the passed rest.Interface verbatim")
}

func TestNvidiaV1alpha1ClientRESTClientNilReceiver(t *testing.T) {
	var client *NvidiaV1alpha1Client

	// Compared against a nil rest.Interface rather than asserted with
	// assert.Nil: that helper is reflection based and also passes for a typed
	// nil pointer boxed in a non-nil interface, which is exactly the sharp
	// edge this test exists to rule out.
	var nilRESTInterface rest.Interface
	assert.Equal(t, nilRESTInterface, client.RESTClient(), "a nil receiver must return a nil rest.Interface")
}

// TestNvidiaV1alpha1ClientResourceGetters exercises both getters through the
// public interface. Their implementations are unexported generator internals;
// what the group client actually promises is that each getter reaches the API
// server over the group client's own REST client, at a cluster-scoped path.
func TestNvidiaV1alpha1ClientResourceGetters(t *testing.T) {
	testCases := []struct {
		name         string
		resource     string
		objectName   string
		expectedPath string
		get          func(ctx context.Context, client *NvidiaV1alpha1Client) error
	}{
		{
			name:         "NVIDIADrivers",
			resource:     nvidiaDriverResource,
			objectName:   nvidiaDriverName,
			expectedPath: nvidiaDriverNamedPath(nvidiaDriverName),
			get: func(ctx context.Context, client *NvidiaV1alpha1Client) error {
				_, err := client.NVIDIADrivers().Get(ctx, nvidiaDriverName, metav1.GetOptions{})
				return err
			},
		},
		{
			name:         "GPUClusters",
			resource:     gpuClusterResource,
			objectName:   gpuClusterSingletonName,
			expectedPath: gpuClusterNamedPath(gpuClusterSingletonName),
			get: func(ctx context.Context, client *NvidiaV1alpha1Client) error {
				_, err := client.GPUClusters().Get(ctx, gpuClusterSingletonName, metav1.GetOptions{})
				return err
			},
		},
	}
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			server, client := newRecordingServerAndClient(t, func(responseWriter http.ResponseWriter, _ *http.Request) {
				writeJSONResponse(responseWriter, http.StatusNotFound, notFoundStatus(testCase.resource, testCase.objectName))
			})

			require.Error(t, testCase.get(t.Context(), client))

			// The request arrived here at all only because the getter built its
			// client on the REST client the group client exposes.
			request := server.onlyRequest(t)
			assert.Equal(t, testCase.expectedPath, request.Path, "every resource in this group is cluster scoped")
		})
	}
}

// newTestRESTConfig returns a config that is already valid for rest.RESTClientFor.
func newTestRESTConfig(t *testing.T) *rest.Config {
	t.Helper()

	config := &rest.Config{Host: unreachableAPIServerHost}
	setConfigDefaults(config)
	return config
}
