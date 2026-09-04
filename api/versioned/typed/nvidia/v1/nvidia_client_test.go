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
	"io/fs"
	"net/http"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	rest "k8s.io/client-go/rest"

	nvidiav1 "github.com/NVIDIA/gpu-operator/api/nvidia/v1"
)

// *NvidiaV1Client must satisfy the generated group interface.
var _ NvidiaV1Interface = &NvidiaV1Client{}

func TestSetConfigDefaults(t *testing.T) {
	t.Run("populates group version, api path, serializer and user agent", func(t *testing.T) {
		cfg := &rest.Config{}
		setConfigDefaults(cfg)

		require.NotNil(t, cfg.GroupVersion)
		assert.Equal(t, nvidiav1.SchemeGroupVersion, *cfg.GroupVersion)
		assert.Equal(t, "nvidia.com", cfg.GroupVersion.Group)
		assert.Equal(t, "v1", cfg.GroupVersion.Version)
		assert.Equal(t, "/apis", cfg.APIPath)
		assert.NotNil(t, cfg.NegotiatedSerializer)
		assert.Equal(t, rest.DefaultKubernetesUserAgent(), cfg.UserAgent)
		assert.NotEmpty(t, cfg.UserAgent)
	})

	t.Run("preserves a caller supplied user agent", func(t *testing.T) {
		cfg := &rest.Config{UserAgent: "my-operator/v1.2.3"}
		setConfigDefaults(cfg)

		assert.Equal(t, "my-operator/v1.2.3", cfg.UserAgent)
	})

	t.Run("overwrites a stale group version", func(t *testing.T) {
		wrong := nvidiav1.SchemeGroupVersion
		wrong.Group = "example.com"
		cfg := &rest.Config{APIPath: "/api"}
		cfg.GroupVersion = &wrong
		setConfigDefaults(cfg)

		require.NotNil(t, cfg.GroupVersion)
		assert.Equal(t, "nvidia.com", cfg.GroupVersion.Group)
		assert.Equal(t, "/apis", cfg.APIPath)
		// The caller's GroupVersion value must not be aliased/modified in place.
		assert.Equal(t, "example.com", wrong.Group)
	})
}

func TestNewForConfig(t *testing.T) {
	t.Run("success", func(t *testing.T) {
		cfg := &rest.Config{Host: "https://localhost:6443"}

		client, err := NewForConfig(cfg)
		require.NoError(t, err)
		require.NotNil(t, client)
		require.NotNil(t, client.RESTClient())
		assert.Equal(t, nvidiav1.SchemeGroupVersion, client.RESTClient().APIVersion())
	})

	t.Run("does not mutate the caller config", func(t *testing.T) {
		cfg := &rest.Config{Host: "https://localhost:6443"}

		_, err := NewForConfig(cfg)
		require.NoError(t, err)

		assert.Nil(t, cfg.GroupVersion, "caller GroupVersion should be untouched")
		assert.Empty(t, cfg.APIPath, "caller APIPath should be untouched")
		assert.Nil(t, cfg.NegotiatedSerializer, "caller NegotiatedSerializer should be untouched")
		assert.Empty(t, cfg.UserAgent, "caller UserAgent should be untouched")
	})

	// One malformed-TLS case is enough to prove that the transport error is
	// propagated instead of swallowed. The assertion is on the error's type,
	// not its text: the message comes from the standard library and client-go
	// and changes across versions and platforms.
	t.Run("propagates a transport construction error", func(t *testing.T) {
		cfg := &rest.Config{
			Host: "https://localhost:6443",
			TLSClientConfig: rest.TLSClientConfig{
				CAFile: filepath.Join(t.TempDir(), "does-not-exist.crt"),
			},
		}

		client, err := NewForConfig(cfg)
		require.Error(t, err)
		assert.Nil(t, client)

		var pathErr *os.PathError
		require.ErrorAs(t, err, &pathErr)
		assert.Equal(t, cfg.CAFile, pathErr.Path)
		assert.ErrorIs(t, err, fs.ErrNotExist)
	})
}

func TestNewForConfigAndClient(t *testing.T) {
	t.Run("uses the supplied http client", func(t *testing.T) {
		cfg := &rest.Config{Host: "https://localhost:6443"}
		httpClient := &http.Client{}

		client, err := NewForConfigAndClient(cfg, httpClient)
		require.NoError(t, err)
		require.NotNil(t, client)

		restClient, ok := client.RESTClient().(*rest.RESTClient)
		require.True(t, ok)
		assert.Same(t, httpClient, restClient.Client)
	})

	t.Run("errors on a malformed host", func(t *testing.T) {
		cfg := &rest.Config{Host: "https://localhost:6443/\x7f/bad"}

		client, err := NewForConfigAndClient(cfg, &http.Client{})
		require.Error(t, err)
		assert.Nil(t, client)
	})
}

func TestNewForConfigOrDie(t *testing.T) {
	t.Run("returns a client for a good config", func(t *testing.T) {
		var client *NvidiaV1Client
		require.NotPanics(t, func() {
			client = NewForConfigOrDie(&rest.Config{Host: "https://localhost:6443"})
		})
		require.NotNil(t, client)
		assert.NotNil(t, client.ClusterPolicies())
	})

	t.Run("panics for a bad config", func(t *testing.T) {
		bad := &rest.Config{
			Host: "https://localhost:6443",
			TLSClientConfig: rest.TLSClientConfig{
				CAFile: filepath.Join(t.TempDir(), "missing.crt"),
			},
		}
		assert.Panics(t, func() { _ = NewForConfigOrDie(bad) })
	})
}

func TestNew(t *testing.T) {
	seed, err := NewForConfig(&rest.Config{Host: "https://localhost:6443"})
	require.NoError(t, err)
	inner := seed.RESTClient()
	require.NotNil(t, inner)

	client := New(inner)
	require.NotNil(t, client)
	assert.Same(t, inner, client.RESTClient())
}

func TestRESTClientNilReceiver(t *testing.T) {
	var client *NvidiaV1Client
	assert.Nil(t, client.RESTClient())
}

func TestClusterPolicies(t *testing.T) {
	client, err := NewForConfig(&rest.Config{Host: "https://localhost:6443"})
	require.NoError(t, err)

	cp := client.ClusterPolicies()
	require.NotNil(t, cp)

	typed, ok := cp.(*clusterPolicies)
	require.True(t, ok)
	// ClusterPolicy is cluster scoped: the generated client is built with an empty namespace.
	assert.Empty(t, typed.GetNamespace())
	assert.Same(t, client.RESTClient(), typed.GetClient())
}
