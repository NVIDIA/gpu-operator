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

package fake

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/watch"
	rest "k8s.io/client-go/rest"
	k8stesting "k8s.io/client-go/testing"

	v1alpha1 "github.com/NVIDIA/gpu-operator/api/nvidia/v1alpha1"
	"github.com/NVIDIA/gpu-operator/api/versioned/scheme"
	nvidiav1alpha1 "github.com/NVIDIA/gpu-operator/api/versioned/typed/nvidia/v1alpha1"
)

// Compile-time assertion that the generated fake group client satisfies the
// real typed group interface. If client-gen ever emits a client that drifts
// from the interface, this fails to build.
var _ nvidiav1alpha1.NvidiaV1alpha1Interface = &FakeNvidiaV1alpha1{}

// watchTimeout bounds every channel read so a broken watch fails the test
// instead of hanging the suite. Everything here runs in-process against an
// in-memory tracker, so events are delivered essentially immediately; a short
// bound keeps regressions failing fast.
const watchTimeout = 2 * time.Second

// Every GVR/GVK below is derived from the API package rather than hardcoded, so
// a group/version rename is caught by the type system.
var (
	nvidiaDriverGVR = v1alpha1.SchemeGroupVersion.WithResource("nvidiadrivers")
	nvidiaDriverGVK = v1alpha1.SchemeGroupVersion.WithKind("NVIDIADriver")
	gpuClusterGVR   = v1alpha1.SchemeGroupVersion.WithResource("gpuclusters")
	gpuClusterGVK   = v1alpha1.SchemeGroupVersion.WithKind("GPUCluster")
)

// fakeGroupFixture wires a bare testing.Fake to an ObjectTracker the same way
// the generated top-level fake clientset does. Building it here (instead of
// using api/versioned/fake) keeps this package free of an import cycle. Both
// resource clients of this group are served off the one shared testing.Fake.
type fakeGroupFixture struct {
	fake    *k8stesting.Fake
	group   *FakeNvidiaV1alpha1
	tracker k8stesting.ObjectTracker
}

func newFakeGroupFixture(t *testing.T, objects ...runtime.Object) *fakeGroupFixture {
	t.Helper()

	tracker := k8stesting.NewObjectTracker(scheme.Scheme, scheme.Codecs.UniversalDecoder())
	for _, obj := range objects {
		require.NoError(t, tracker.Add(obj))
	}

	f := &k8stesting.Fake{}
	f.AddReactor("*", "*", k8stesting.ObjectReaction(tracker))
	f.AddWatchReactor("*", func(action k8stesting.Action) (bool, watch.Interface, error) {
		var opts metav1.ListOptions
		if watchAction, ok := action.(k8stesting.WatchActionImpl); ok {
			opts = watchAction.ListOptions
		}
		w, err := tracker.Watch(action.GetResource(), action.GetNamespace(), opts)
		if err != nil {
			return false, nil, err
		}
		return true, w, nil
	})

	return &fakeGroupFixture{fake: f, group: &FakeNvidiaV1alpha1{Fake: f}, tracker: tracker}
}

// lastAction returns the most recently recorded action, failing if none exist.
func lastAction(t *testing.T, f *k8stesting.Fake) k8stesting.Action {
	t.Helper()
	actions := f.Actions()
	require.NotEmpty(t, actions)
	return actions[len(actions)-1]
}

// expectEvent reads one watch event with a hard timeout and asserts both its
// type and the concrete type of its payload. Every watch channel read in this
// package goes through this helper so no test can hang, and so every failure
// mode (closed channel, timeout, wrong event type, wrong payload type) is
// reported from the test goroutine.
func expectEvent[T runtime.Object](t *testing.T, ch <-chan watch.Event, want watch.EventType) T {
	t.Helper()
	var zero T
	timer := time.NewTimer(watchTimeout)
	defer timer.Stop()
	select {
	case ev, ok := <-ch:
		require.True(t, ok, "watch channel closed while waiting for %s", want)
		require.Equal(t, want, ev.Type)
		obj, ok := ev.Object.(T)
		require.True(t, ok, "expected %T watch event object, got %T", zero, ev.Object)
		return obj
	case <-timer.C:
		t.Fatalf("timed out after %s waiting for %s watch event", watchTimeout, want)
		return zero
	}
}

func TestFakeNvidiaV1alpha1_NVIDIADriversReturnsUsableClient(t *testing.T) {
	c := &FakeNvidiaV1alpha1{Fake: &k8stesting.Fake{}}

	drivers := c.NVIDIADrivers()
	require.NotNil(t, drivers)

	// The concrete type is the generated fake, wired back to the group client.
	concrete, ok := drivers.(*fakeNVIDIADrivers)
	require.True(t, ok, "expected NVIDIADrivers() to return *fakeNVIDIADrivers, got %T", drivers)
	assert.Same(t, c, concrete.Fake, "fake driver client must point back at its group client")
	assert.Equal(t, nvidiaDriverGVR, concrete.Resource())
	assert.Equal(t, nvidiaDriverGVK, concrete.Kind())
	// NVIDIADriver is a cluster-scoped resource (+genclient:nonNamespaced), so
	// the generated client is constructed with an empty namespace.
	assert.Empty(t, concrete.Namespace())
}

func TestFakeNvidiaV1alpha1_GPUClustersReturnsUsableClient(t *testing.T) {
	c := &FakeNvidiaV1alpha1{Fake: &k8stesting.Fake{}}

	clusters := c.GPUClusters()
	require.NotNil(t, clusters)

	concrete, ok := clusters.(*fakeGPUClusters)
	require.True(t, ok, "expected GPUClusters() to return *fakeGPUClusters, got %T", clusters)
	assert.Same(t, c, concrete.Fake, "fake cluster client must point back at its group client")
	assert.Equal(t, gpuClusterGVR, concrete.Resource())
	assert.Equal(t, gpuClusterGVK, concrete.Kind())
	// GPUCluster is likewise cluster scoped (+genclient:nonNamespaced).
	assert.Empty(t, concrete.Namespace())
}

func TestFakeNvidiaV1alpha1_AccessorsShareActionRecorder(t *testing.T) {
	f := &k8stesting.Fake{}
	c := &FakeNvidiaV1alpha1{Fake: f}

	firstDrivers := c.NVIDIADrivers()
	secondDrivers := c.NVIDIADrivers()
	// Each call builds a fresh struct...
	assert.NotSame(t, firstDrivers, secondDrivers)

	// ...but every accessor funnels its actions into the single shared
	// testing.Fake, so a test can assert across both resources at once.
	ctx := t.Context()
	_, _ = firstDrivers.Get(ctx, "a", metav1.GetOptions{})
	_, _ = secondDrivers.Get(ctx, "b", metav1.GetOptions{})
	_, _ = c.GPUClusters().Get(ctx, "c", metav1.GetOptions{})

	actions := f.Actions()
	require.Len(t, actions, 3)
	assert.Equal(t, "a", actions[0].(k8stesting.GetAction).GetName())
	assert.Equal(t, nvidiaDriverGVR, actions[0].GetResource())
	assert.Equal(t, "b", actions[1].(k8stesting.GetAction).GetName())
	assert.Equal(t, nvidiaDriverGVR, actions[1].GetResource())
	assert.Equal(t, "c", actions[2].(k8stesting.GetAction).GetName())
	assert.Equal(t, gpuClusterGVR, actions[2].GetResource())
}

// TestFakeNvidiaV1alpha1_ResourcesAreIndependentlyTracked proves the two
// resources of this group do not alias each other in the tracker even though
// they share a testing.Fake and a reaction chain.
func TestFakeNvidiaV1alpha1_ResourcesAreIndependentlyTracked(t *testing.T) {
	f := newFakeGroupFixture(t, newDriver("shared-name", nil), newGPUCluster("shared-name", nil))
	ctx := t.Context()

	require.NoError(t, f.group.NVIDIADrivers().Delete(ctx, "shared-name", metav1.DeleteOptions{}))

	_, err := f.group.NVIDIADrivers().Get(ctx, "shared-name", metav1.GetOptions{})
	assert.Error(t, err)

	// The identically named GPUCluster is untouched.
	cluster, err := f.group.GPUClusters().Get(ctx, "shared-name", metav1.GetOptions{})
	require.NoError(t, err)
	assert.Equal(t, "shared-name", cluster.Name)
}

// TestFakeNvidiaV1alpha1_RESTClientIsTypedNil documents a sharp edge of the
// generated stub:
//
//	func (c *FakeNvidiaV1alpha1) RESTClient() rest.Interface {
//	    var ret *rest.RESTClient
//	    return ret
//	}
//
// The returned rest.Interface is NOT the nil interface: it carries the dynamic
// type *rest.RESTClient with a nil value. Callers that guard with `if rc != nil`
// will take the non-nil branch and then panic on first use. The fake group
// client simply has no REST transport behind it.
func TestFakeNvidiaV1alpha1_RESTClientIsTypedNil(t *testing.T) {
	c := &FakeNvidiaV1alpha1{Fake: &k8stesting.Fake{}}

	rc := c.RESTClient()

	// Reflectively nil (this is what assert.Nil checks)...
	assert.Nil(t, rc)
	// ...but not the nil interface value.
	//nolint:staticcheck // deliberately asserting the typed-nil behavior
	assert.False(t, rc == nil, "generated stub returns a typed nil, not a nil interface")
	assert.IsType(t, (*rest.RESTClient)(nil), rc)

	// GetRateLimiter is explicitly nil-receiver safe upstream, so it is the one
	// method that survives the typed nil.
	assert.Nil(t, rc.GetRateLimiter())

	// Anything that actually builds a request (Verb/Post/Get/...) is unusable:
	// the returned client has no transport, base URL or content config behind
	// it. Exactly how it fails is client-go's business, not this package's, so
	// it is documented here rather than asserted.
}
