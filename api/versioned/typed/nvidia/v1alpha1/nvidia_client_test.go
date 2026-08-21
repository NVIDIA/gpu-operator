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
	"net/http"
	"os"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	rest "k8s.io/client-go/rest"

	nvidiav1alpha1 "github.com/NVIDIA/gpu-operator/api/nvidia/v1alpha1"
)

// Compile-time assertion that the generated client satisfies the group interface.
var _ NvidiaV1alpha1Interface = &NvidiaV1alpha1Client{}

func TestSetConfigDefaults(t *testing.T) {
	tests := []struct {
		name              string
		in                rest.Config
		expectedUserAgent func(t *testing.T, got string)
	}{
		{
			name: "empty user agent gets the default kubernetes user agent",
			in:   rest.Config{},
			expectedUserAgent: func(t *testing.T, got string) {
				t.Helper()
				assert.Equal(t, rest.DefaultKubernetesUserAgent(), got)
			},
		},
		{
			name: "caller supplied user agent is preserved",
			in:   rest.Config{UserAgent: "gpu-operator-tests/1.2.3"},
			expectedUserAgent: func(t *testing.T, got string) {
				t.Helper()
				assert.Equal(t, "gpu-operator-tests/1.2.3", got)
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := tt.in
			setConfigDefaults(&cfg)

			require.NotNil(t, cfg.GroupVersion)
			assert.Equal(t, nvidiav1alpha1.SchemeGroupVersion, *cfg.GroupVersion)
			assert.Equal(t, "nvidia.com", cfg.GroupVersion.Group)
			assert.Equal(t, "v1alpha1", cfg.GroupVersion.Version)
			assert.Equal(t, "/apis", cfg.APIPath)
			assert.NotNil(t, cfg.NegotiatedSerializer)
			tt.expectedUserAgent(t, cfg.UserAgent)
		})
	}
}

func TestSetConfigDefaultsNegotiatedSerializerSupportsJSON(t *testing.T) {
	cfg := rest.Config{}
	setConfigDefaults(&cfg)

	require.NotNil(t, cfg.NegotiatedSerializer)

	var mediaTypes []string
	for _, info := range cfg.NegotiatedSerializer.SupportedMediaTypes() {
		mediaTypes = append(mediaTypes, info.MediaType)
	}
	assert.Contains(t, mediaTypes, "application/json")
}

func TestNewForConfig(t *testing.T) {
	t.Run("succeeds for a minimal config", func(t *testing.T) {
		client, err := NewForConfig(&rest.Config{Host: "https://192.0.2.1:6443"})
		require.NoError(t, err)
		require.NotNil(t, client)
		require.NotNil(t, client.RESTClient())

		// The REST client must have been built against the group's API path.
		got := client.RESTClient().Get().URL()
		assert.Equal(t, "/apis/nvidia.com/v1alpha1", got.Path)
	})

	t.Run("does not mutate the caller's config", func(t *testing.T) {
		in := &rest.Config{Host: "https://192.0.2.1:6443"}
		_, err := NewForConfig(in)
		require.NoError(t, err)

		assert.Nil(t, in.GroupVersion, "NewForConfig must operate on a copy")
		assert.Empty(t, in.APIPath)
		assert.Empty(t, in.UserAgent)
		assert.Nil(t, in.NegotiatedSerializer)
	})

	// One representative failure is enough to prove NewForConfig surfaces
	// rest.HTTPClientFor errors instead of swallowing them. The error text
	// itself belongs to client-go and the OS, so it is not asserted on; the
	// typed *os.PathError is the stable part.
	t.Run("propagates rest.HTTPClientFor errors", func(t *testing.T) {
		client, err := NewForConfig(&rest.Config{
			Host: "https://192.0.2.1:6443",
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
	t.Run("uses the supplied http client", func(t *testing.T) {
		httpClient := &http.Client{}
		client, err := NewForConfigAndClient(&rest.Config{Host: "https://192.0.2.1:6443"}, httpClient)
		require.NoError(t, err)
		require.NotNil(t, client)
		assert.Equal(t, "/apis/nvidia.com/v1alpha1", client.RESTClient().Get().URL().Path)
	})

	t.Run("does not mutate the caller's config", func(t *testing.T) {
		in := &rest.Config{Host: "https://192.0.2.1:6443"}
		_, err := NewForConfigAndClient(in, &http.Client{})
		require.NoError(t, err)

		assert.Nil(t, in.GroupVersion)
		assert.Empty(t, in.APIPath)
		assert.Empty(t, in.UserAgent)
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
			client = NewForConfigOrDie(&rest.Config{Host: "https://192.0.2.1:6443"})
		})
		require.NotNil(t, client)
		assert.NotNil(t, client.RESTClient())
	})

	t.Run("panics for a bad config", func(t *testing.T) {
		assert.Panics(t, func() {
			NewForConfigOrDie(&rest.Config{
				Host: "https://192.0.2.1:6443",
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

func TestRESTClientNilReceiver(t *testing.T) {
	var client *NvidiaV1alpha1Client
	assert.Nil(t, client.RESTClient(), "a nil receiver must return a nil rest.Interface")
}

func TestNVIDIADriversGetter(t *testing.T) {
	client, err := NewForConfig(&rest.Config{Host: "https://192.0.2.1:6443"})
	require.NoError(t, err)

	drivers := client.NVIDIADrivers()
	require.NotNil(t, drivers)

	concrete, ok := drivers.(*nVIDIADrivers)
	require.True(t, ok, "expected the generated *nVIDIADrivers implementation")
	assert.Empty(t, concrete.GetNamespace(), "NVIDIADriver is cluster scoped")
}

func TestGPUClustersGetter(t *testing.T) {
	client, err := NewForConfig(&rest.Config{Host: "https://192.0.2.1:6443"})
	require.NoError(t, err)

	clusters := client.GPUClusters()
	require.NotNil(t, clusters)

	concrete, ok := clusters.(*gPUClusters)
	require.True(t, ok, "expected the generated *gPUClusters implementation")
	assert.Empty(t, concrete.GetNamespace(), "GPUCluster is cluster scoped")
}

func TestGettersShareTheGroupRESTClient(t *testing.T) {
	// Both resource clients must be built on top of the very REST client the
	// group client exposes.
	restClient, err := rest.RESTClientFor(newTestRESTConfig(t))
	require.NoError(t, err)

	client := New(restClient)

	drivers, ok := client.NVIDIADrivers().(*nVIDIADrivers)
	require.True(t, ok)
	clusters, ok := client.GPUClusters().(*gPUClusters)
	require.True(t, ok)

	assert.Same(t, restClient, drivers.GetClient())
	assert.Same(t, restClient, clusters.GetClient())
}

// newTestRESTConfig returns a config that is already valid for rest.RESTClientFor.
func newTestRESTConfig(t *testing.T) *rest.Config {
	t.Helper()

	cfg := &rest.Config{Host: "https://192.0.2.1:6443"}
	setConfigDefaults(cfg)
	return cfg
}
