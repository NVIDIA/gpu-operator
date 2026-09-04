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
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"sync"
	"testing"

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

// pathRecorder records the request paths observed by an httptest handler. The
// handler runs on the server goroutine while the assertions run on the test
// goroutine, so all access is serialized by the embedded mutex. It never calls
// into testify, because require/assert may only be used from the test
// goroutine.
type pathRecorder struct {
	mu    sync.Mutex
	paths []string
}

func (r *pathRecorder) record(path string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.paths = append(r.paths, path)
}

// snapshot returns a copy of the recorded paths and must be called from the
// test goroutine once the client calls have completed.
func (r *pathRecorder) snapshot() []string {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]string(nil), r.paths...)
}

// goodConfig returns a minimal *rest.Config that every constructor under test
// should accept.
func goodConfig() *rest.Config {
	return &rest.Config{Host: testHost}
}

// badConfig returns a *rest.Config that makes rest.HTTPClientFor fail, because
// the referenced CA bundle does not exist on disk.
func badConfig(t *testing.T) *rest.Config {
	t.Helper()
	return &rest.Config{
		Host: testHost,
		TLSClientConfig: rest.TLSClientConfig{
			CAFile: filepath.Join(t.TempDir(), "does-not-exist-ca.crt"),
		},
	}
}

// newTestRESTClient builds a real *rest.RESTClient so that New() can be
// verified against a concrete rest.Interface implementation.
func newTestRESTClient(t *testing.T) *rest.RESTClient {
	t.Helper()
	gv := schema.GroupVersion{Group: "nvidia.com", Version: "v1"}
	restClient, err := rest.RESTClientFor(&rest.Config{
		Host:    testHost,
		APIPath: "/apis",
		ContentConfig: rest.ContentConfig{
			GroupVersion:         &gv,
			NegotiatedSerializer: scheme.Codecs.WithoutConversion(),
		},
	})
	require.NoError(t, err)
	return restClient
}

func TestNewForConfig(t *testing.T) {
	t.Run("succeeds on a minimal config and wires every client", func(t *testing.T) {
		cs, err := NewForConfig(goodConfig())
		require.NoError(t, err)
		require.NotNil(t, cs)

		require.NotNil(t, cs.nvidiaV1)
		require.NotNil(t, cs.nvidiaV1alpha1)
		require.NotNil(t, cs.DiscoveryClient)

		// All group clients share a single REST transport-backed client per group,
		// each configured for its own group/version.
		assert.Equal(t, nvidiav1.SchemeGroupVersion, cs.NvidiaV1().RESTClient().APIVersion())
		assert.Equal(t, nvidiav1alpha1.SchemeGroupVersion, cs.NvidiaV1alpha1().RESTClient().APIVersion())
	})

	t.Run("returns the transport error when the CA bundle cannot be loaded", func(t *testing.T) {
		cs, err := NewForConfig(badConfig(t))
		require.Error(t, err)
		assert.Nil(t, cs)
		assert.Contains(t, err.Error(), "does-not-exist-ca.crt")
	})

	t.Run("propagates the burst validation error from NewForConfigAndClient", func(t *testing.T) {
		cfg := goodConfig()
		cfg.QPS = 10
		cfg.Burst = 0

		cs, err := NewForConfig(cfg)
		require.Error(t, err)
		assert.Nil(t, cs)
		assert.Contains(t, err.Error(), "burst is required to be greater than 0")
	})
}

func TestNewForConfigUserAgent(t *testing.T) {
	tests := []struct {
		name      string
		userAgent string
		expected  string
	}{
		{
			name:      "empty user agent is defaulted",
			userAgent: "",
			expected:  rest.DefaultKubernetesUserAgent(),
		},
		{
			name:      "caller supplied user agent is preserved",
			userAgent: "gpu-operator-test/1.2.3",
			expected:  "gpu-operator-test/1.2.3",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// The handler runs on the server goroutine, so the recorded header is
			// guarded by a mutex and only read back on the test goroutine.
			var (
				mu           sync.Mutex
				gotUserAgent string
			)
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				mu.Lock()
				gotUserAgent = r.Header.Get("User-Agent")
				mu.Unlock()
				w.Header().Set("Content-Type", "application/json")
				_ = json.NewEncoder(w).Encode(&nvidiav1.ClusterPolicyList{})
			}))
			defer server.Close()

			cfg := &rest.Config{Host: server.URL, UserAgent: tt.userAgent}
			cs, err := NewForConfig(cfg)
			require.NoError(t, err)

			_, err = cs.NvidiaV1().ClusterPolicies().List(t.Context(), metav1.ListOptions{})
			require.NoError(t, err)

			mu.Lock()
			recordedUserAgent := gotUserAgent
			mu.Unlock()
			assert.Equal(t, tt.expected, recordedUserAgent)

			// The caller's config must never be mutated: NewForConfig works on a
			// shallow copy.
			assert.Equal(t, tt.userAgent, cfg.UserAgent)
		})
	}
}

func TestNewForConfigDoesNotMutateCallerConfig(t *testing.T) {
	cfg := goodConfig()
	cfg.QPS = 25
	cfg.Burst = 50

	cs, err := NewForConfig(cfg)
	require.NoError(t, err)
	require.NotNil(t, cs)

	// UserAgent defaulting and rate-limiter construction happen on shallow
	// copies, so the caller's config is untouched.
	assert.Empty(t, cfg.UserAgent)
	assert.Nil(t, cfg.RateLimiter)
	assert.Equal(t, float32(25), cfg.QPS)
	assert.Equal(t, 50, cfg.Burst)
}

func TestNewForConfigAndClient(t *testing.T) {
	t.Run("QPS set with non-positive burst is rejected", func(t *testing.T) {
		for _, burst := range []int{0, -1} {
			cfg := goodConfig()
			cfg.QPS = 5
			cfg.Burst = burst

			cs, err := NewForConfigAndClient(cfg, &http.Client{})
			require.Error(t, err)
			assert.Nil(t, cs)
			assert.EqualError(t, err,
				"burst is required to be greater than 0 when RateLimiter is not set and QPS is set to greater than 0")
		}
	})

	t.Run("returns the error from the first group client that fails to build", func(t *testing.T) {
		cfg := &rest.Config{Host: "http://[::1]:not-a-port"}

		cs, err := NewForConfigAndClient(cfg, &http.Client{})
		require.Error(t, err)
		assert.Nil(t, cs)
	})

	t.Run("QPS with a positive burst installs a rate limiter on every client", func(t *testing.T) {
		cfg := goodConfig()
		cfg.QPS = 5
		cfg.Burst = 10

		cs, err := NewForConfigAndClient(cfg, &http.Client{})
		require.NoError(t, err)
		require.NotNil(t, cs)

		v1Limiter := cs.NvidiaV1().RESTClient().GetRateLimiter()
		v1alpha1Limiter := cs.NvidiaV1alpha1().RESTClient().GetRateLimiter()
		require.NotNil(t, v1Limiter)
		require.NotNil(t, v1alpha1Limiter)
		// A single limiter is generated in the shallow copy and shared by all
		// group clients built from it.
		assert.Same(t, v1Limiter, v1alpha1Limiter)

		// The caller's config keeps its nil RateLimiter.
		assert.Nil(t, cfg.RateLimiter)
	})

	t.Run("a pre-set rate limiter is left alone", func(t *testing.T) {
		limiter := flowcontrol.NewTokenBucketRateLimiter(1, 1)
		cfg := goodConfig()
		cfg.RateLimiter = limiter
		// Burst is invalid, but must not be validated because RateLimiter is set.
		cfg.QPS = 5
		cfg.Burst = 0

		cs, err := NewForConfigAndClient(cfg, &http.Client{})
		require.NoError(t, err)
		require.NotNil(t, cs)

		assert.Same(t, limiter, cs.NvidiaV1().RESTClient().GetRateLimiter())
		assert.Same(t, limiter, cs.NvidiaV1alpha1().RESTClient().GetRateLimiter())
		assert.Same(t, limiter, cfg.RateLimiter)
	})

	t.Run("no shared rate limiter is generated when QPS is zero", func(t *testing.T) {
		cfg := goodConfig()
		cfg.QPS = 0
		cfg.Burst = 0

		cs, err := NewForConfigAndClient(cfg, &http.Client{})
		require.NoError(t, err)
		require.NotNil(t, cs)
		assert.Nil(t, cfg.RateLimiter)

		// Nothing is installed in the shallow copy, so each group client falls
		// back to the rest package's own per-client default limiter rather than
		// sharing one.
		assert.NotSame(t,
			cs.NvidiaV1().RESTClient().GetRateLimiter(),
			cs.NvidiaV1alpha1().RESTClient().GetRateLimiter())
	})
}

func TestNewForConfigOrDie(t *testing.T) {
	t.Run("returns a clientset for a good config", func(t *testing.T) {
		var cs *Clientset
		require.NotPanics(t, func() {
			cs = NewForConfigOrDie(goodConfig())
		})
		require.NotNil(t, cs)
		assert.NotNil(t, cs.NvidiaV1())
		assert.NotNil(t, cs.NvidiaV1alpha1())
		assert.NotNil(t, cs.Discovery())
	})

	t.Run("panics for a bad config", func(t *testing.T) {
		cfg := badConfig(t)
		assert.Panics(t, func() {
			_ = NewForConfigOrDie(cfg)
		})
	})

	t.Run("panics when burst validation fails", func(t *testing.T) {
		cfg := goodConfig()
		cfg.QPS = 10
		cfg.Burst = -5
		assert.Panics(t, func() {
			_ = NewForConfigOrDie(cfg)
		})
	})
}

func TestNew(t *testing.T) {
	restClient := newTestRESTClient(t)

	cs := New(restClient)
	require.NotNil(t, cs)

	require.NotNil(t, cs.nvidiaV1)
	require.NotNil(t, cs.nvidiaV1alpha1)
	require.NotNil(t, cs.DiscoveryClient)

	// Every group client, and discovery, must be backed by the exact RESTClient
	// that was handed to New.
	assert.Same(t, restClient, cs.NvidiaV1().RESTClient())
	assert.Same(t, restClient, cs.NvidiaV1alpha1().RESTClient())
	assert.Same(t, restClient, cs.Discovery().RESTClient())
}

func TestDiscovery(t *testing.T) {
	t.Run("returns the embedded discovery client", func(t *testing.T) {
		cs := New(newTestRESTClient(t))
		got := cs.Discovery()
		require.NotNil(t, got)
		assert.Same(t, cs.DiscoveryClient, got)
		assert.IsType(t, &discovery.DiscoveryClient{}, got)
	})

	t.Run("nil receiver returns a nil interface", func(t *testing.T) {
		var cs *Clientset
		got := cs.Discovery()
		assert.Nil(t, got)
		// The explicit nil check must return an untyped nil, not a typed nil
		// wrapped in the interface.
		assert.True(t, got == nil)
	})
}

func TestGroupClientAccessors(t *testing.T) {
	cs := New(newTestRESTClient(t))

	v1Client := cs.NvidiaV1()
	require.NotNil(t, v1Client)
	assert.IsType(t, &typednvidiav1.NvidiaV1Client{}, v1Client)
	assert.Same(t, cs.nvidiaV1, v1Client)
	assert.NotNil(t, v1Client.ClusterPolicies())

	v1alpha1Client := cs.NvidiaV1alpha1()
	require.NotNil(t, v1alpha1Client)
	assert.IsType(t, &typednvidiav1alpha1.NvidiaV1alpha1Client{}, v1alpha1Client)
	assert.Same(t, cs.nvidiaV1alpha1, v1alpha1Client)
	assert.NotNil(t, v1alpha1Client.GPUClusters())
	assert.NotNil(t, v1alpha1Client.NVIDIADrivers())
}

func TestClientsetImplementsInterface(t *testing.T) {
	assert.Implements(t, (*Interface)(nil), &Clientset{})
	assert.Implements(t, (*discovery.DiscoveryInterface)(nil), New(newTestRESTClient(t)).Discovery())
}

// TestClientsetRoundTrip exercises the wiring end to end: a clientset built by
// NewForConfig must reach the right API paths for discovery and for each group.
func TestClientsetRoundTrip(t *testing.T) {
	// pathRecorder collects the paths seen by the httptest handler. The handler
	// runs on the server goroutine, so every access is taken under the mutex and
	// the snapshot is read back on the test goroutine.
	var recorder pathRecorder
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		recorder.record(r.URL.Path)
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/apis/nvidia.com/v1/clusterpolicies":
			_ = json.NewEncoder(w).Encode(&nvidiav1.ClusterPolicyList{
				Items: []nvidiav1.ClusterPolicy{{ObjectMeta: metav1.ObjectMeta{Name: "cluster-policy"}}},
			})
		case "/apis/nvidia.com/v1alpha1/nvidiadrivers":
			_ = json.NewEncoder(w).Encode(&nvidiav1alpha1.NVIDIADriverList{
				Items: []nvidiav1alpha1.NVIDIADriver{{ObjectMeta: metav1.ObjectMeta{Name: "driver"}}},
			})
		case "/apis/nvidia.com/v1":
			_ = json.NewEncoder(w).Encode(&metav1.APIResourceList{
				GroupVersion: "nvidia.com/v1",
				APIResources: []metav1.APIResource{{Name: "clusterpolicies", Kind: "ClusterPolicy"}},
			})
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer server.Close()

	cs, err := NewForConfig(&rest.Config{Host: server.URL})
	require.NoError(t, err)

	policies, err := cs.NvidiaV1().ClusterPolicies().List(t.Context(), metav1.ListOptions{})
	require.NoError(t, err)
	require.Len(t, policies.Items, 1)
	assert.Equal(t, "cluster-policy", policies.Items[0].Name)

	drivers, err := cs.NvidiaV1alpha1().NVIDIADrivers().List(t.Context(), metav1.ListOptions{})
	require.NoError(t, err)
	require.Len(t, drivers.Items, 1)
	assert.Equal(t, "driver", drivers.Items[0].Name)

	resources, err := cs.Discovery().ServerResourcesForGroupVersion("nvidia.com/v1")
	require.NoError(t, err)
	require.Len(t, resources.APIResources, 1)
	assert.Equal(t, "clusterpolicies", resources.APIResources[0].Name)

	assert.Equal(t, []string{
		"/apis/nvidia.com/v1/clusterpolicies",
		"/apis/nvidia.com/v1alpha1/nvidiadrivers",
		"/apis/nvidia.com/v1",
	}, recorder.snapshot())
}
