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
	runtime "k8s.io/apimachinery/pkg/runtime"
	rest "k8s.io/client-go/rest"

	nvidiav1 "github.com/NVIDIA/gpu-operator/api/nvidia/v1"
)

// *NvidiaV1Client must satisfy the generated group interface.
var _ NvidiaV1Interface = &NvidiaV1Client{}

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

func TestSetConfigDefaults(t *testing.T) {
	t.Run("populates group version, api path, serializer and user agent", func(t *testing.T) {
		config := &rest.Config{}
		setConfigDefaults(config)

		require.NotNil(t, config.GroupVersion)
		assert.Equal(t, nvidiav1.SchemeGroupVersion, *config.GroupVersion)
		assert.Equal(t, "nvidia.com", config.GroupVersion.Group)
		assert.Equal(t, "v1", config.GroupVersion.Version)
		assert.Equal(t, "/apis", config.APIPath)
		assert.NotNil(t, config.NegotiatedSerializer)
		assert.Equal(t, rest.DefaultKubernetesUserAgent(), config.UserAgent)
	})

	t.Run("overwrites a caller supplied negotiated serializer", func(t *testing.T) {
		config := &rest.Config{}
		config.NegotiatedSerializer = callerSuppliedSerializer{}
		setConfigDefaults(config)

		require.NotNil(t, config.NegotiatedSerializer)
		_, stillCallerSupplied := config.NegotiatedSerializer.(callerSuppliedSerializer)
		assert.False(t, stillCallerSupplied,
			"the generated client must decode with the group's own scheme, not whatever the caller configured")
	})

	t.Run("preserves a caller supplied user agent", func(t *testing.T) {
		config := &rest.Config{UserAgent: "my-operator/v1.2.3"}
		setConfigDefaults(config)

		assert.Equal(t, "my-operator/v1.2.3", config.UserAgent)
	})

	t.Run("overwrites a stale group version", func(t *testing.T) {
		staleGroupVersion := nvidiav1.SchemeGroupVersion
		staleGroupVersion.Group = "example.com"
		config := &rest.Config{APIPath: "/api"}
		config.GroupVersion = &staleGroupVersion
		setConfigDefaults(config)

		require.NotNil(t, config.GroupVersion)
		assert.Equal(t, "nvidia.com", config.GroupVersion.Group)
		assert.Equal(t, "/apis", config.APIPath)
		// The caller's GroupVersion value must not be aliased/modified in place.
		assert.Equal(t, "example.com", staleGroupVersion.Group)
	})
}

func TestNewForConfig(t *testing.T) {
	t.Run("success", func(t *testing.T) {
		config := &rest.Config{Host: unreachableAPIServerHost}

		client, err := NewForConfig(config)
		require.NoError(t, err)
		require.NotNil(t, client)
		require.NotNil(t, client.RESTClient())
		assert.Equal(t, nvidiav1.SchemeGroupVersion, client.RESTClient().APIVersion())
	})

	t.Run("does not mutate the caller config", func(t *testing.T) {
		config := &rest.Config{Host: unreachableAPIServerHost}

		_, err := NewForConfig(config)
		require.NoError(t, err)

		assert.Nil(t, config.GroupVersion, "caller GroupVersion should be untouched")
		assert.Empty(t, config.APIPath, "caller APIPath should be untouched")
		assert.Nil(t, config.NegotiatedSerializer, "caller NegotiatedSerializer should be untouched")
		assert.Empty(t, config.UserAgent, "caller UserAgent should be untouched")
	})

	// One malformed-TLS case is enough to prove that the transport error is
	// propagated instead of swallowed. The assertion is on the error's type,
	// not its text: the message comes from the standard library and client-go
	// and changes across versions and platforms.
	t.Run("propagates a transport construction error", func(t *testing.T) {
		configWithMissingCABundle := &rest.Config{
			Host: unreachableAPIServerHost,
			TLSClientConfig: rest.TLSClientConfig{
				CAFile: filepath.Join(t.TempDir(), "does-not-exist.crt"),
			},
		}

		client, err := NewForConfig(configWithMissingCABundle)
		require.Error(t, err)
		assert.Nil(t, client)

		var pathErr *os.PathError
		require.ErrorAs(t, err, &pathErr)
		assert.Equal(t, configWithMissingCABundle.CAFile, pathErr.Path)
		assert.ErrorIs(t, err, fs.ErrNotExist)
	})
}

func TestNewForConfigAndClient(t *testing.T) {
	t.Run("uses the supplied http client", func(t *testing.T) {
		config := &rest.Config{Host: unreachableAPIServerHost}
		httpClient := &http.Client{}

		client, err := NewForConfigAndClient(config, httpClient)
		require.NoError(t, err)
		require.NotNil(t, client)

		restClient, ok := client.RESTClient().(*rest.RESTClient)
		require.True(t, ok)
		assert.Same(t, httpClient, restClient.Client)
	})

	t.Run("does not mutate the caller config", func(t *testing.T) {
		config := &rest.Config{Host: unreachableAPIServerHost, UserAgent: "my-operator/v1.2.3"}

		_, err := NewForConfigAndClient(config, &http.Client{})
		require.NoError(t, err)

		assert.Nil(t, config.GroupVersion, "caller GroupVersion should be untouched")
		assert.Empty(t, config.APIPath, "caller APIPath should be untouched")
		assert.Nil(t, config.NegotiatedSerializer, "caller NegotiatedSerializer should be untouched")
		assert.Equal(t, "my-operator/v1.2.3", config.UserAgent, "caller UserAgent should be untouched")
	})

	t.Run("errors on a malformed host", func(t *testing.T) {
		config := &rest.Config{Host: unreachableAPIServerHost + "/\x7f/bad"}

		client, err := NewForConfigAndClient(config, &http.Client{})
		require.Error(t, err)
		assert.Nil(t, client)
	})
}

func TestNewForConfigOrDie(t *testing.T) {
	t.Run("returns a client for a good config", func(t *testing.T) {
		var client *NvidiaV1Client
		require.NotPanics(t, func() {
			client = NewForConfigOrDie(&rest.Config{Host: unreachableAPIServerHost})
		})
		require.NotNil(t, client)
		assert.NotNil(t, client.ClusterPolicies())
	})

	t.Run("panics for a bad config", func(t *testing.T) {
		configWithMissingCABundle := &rest.Config{
			Host: unreachableAPIServerHost,
			TLSClientConfig: rest.TLSClientConfig{
				CAFile: filepath.Join(t.TempDir(), "missing.crt"),
			},
		}
		assert.Panics(t, func() { _ = NewForConfigOrDie(configWithMissingCABundle) })
	})
}

func TestNew(t *testing.T) {
	sourceClient, err := NewForConfig(&rest.Config{Host: unreachableAPIServerHost})
	require.NoError(t, err)
	restClient := sourceClient.RESTClient()
	require.NotNil(t, restClient)

	client := New(restClient)
	require.NotNil(t, client)
	assert.Same(t, restClient, client.RESTClient())
}

func TestNvidiaV1ClientRESTClientOnNilReceiver(t *testing.T) {
	var client *NvidiaV1Client

	// The untyped nil is asserted explicitly: assert.Nil is reflection based and
	// would also accept an interface holding a typed nil *rest.RESTClient, which
	// a caller's `if c.RESTClient() == nil` guard would not catch.
	var nilRESTInterface rest.Interface
	assert.Equal(t, nilRESTInterface, client.RESTClient())
}

func TestNvidiaV1ClientClusterPolicies(t *testing.T) {
	client, err := NewForConfig(&rest.Config{Host: unreachableAPIServerHost})
	require.NoError(t, err)

	clusterPolicyClient := client.ClusterPolicies()
	require.NotNil(t, clusterPolicyClient)

	typedClusterPolicies, ok := clusterPolicyClient.(*clusterPolicies)
	require.True(t, ok)
	// ClusterPolicy is cluster scoped: the generated client is built with an empty namespace.
	assert.Empty(t, typedClusterPolicies.GetNamespace())
	assert.Same(t, client.RESTClient(), typedClusterPolicies.GetClient())
}
