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
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/watch"
	rest "k8s.io/client-go/rest"
	k8stesting "k8s.io/client-go/testing"

	nvidiav1alpha1 "github.com/NVIDIA/gpu-operator/api/nvidia/v1alpha1"
	"github.com/NVIDIA/gpu-operator/api/versioned/scheme"
	typednvidiav1alpha1 "github.com/NVIDIA/gpu-operator/api/versioned/typed/nvidia/v1alpha1"
)

// Compile-time assertion that the generated fake group client satisfies the
// real typed group interface. If client-gen ever emits a client that drifts
// from the interface, this fails to build.
var _ typednvidiav1alpha1.NvidiaV1alpha1Interface = &FakeNvidiaV1alpha1{}

// Every GVR/GVK below is derived from the API package rather than hardcoded, so
// a group/version rename is caught by the type system.
var (
	nvidiaDriverGVR = nvidiav1alpha1.SchemeGroupVersion.WithResource("nvidiadrivers")
	nvidiaDriverGVK = nvidiav1alpha1.SchemeGroupVersion.WithKind("NVIDIADriver")
	gpuClusterGVR   = nvidiav1alpha1.SchemeGroupVersion.WithResource("gpuclusters")
	gpuClusterGVK   = nvidiav1alpha1.SchemeGroupVersion.WithKind("GPUCluster")
)

// fakeGroupFixture wires a bare testing.Fake to an ObjectTracker the same way
// the generated top-level fake clientset does. Building it here (instead of
// using api/versioned/fake) keeps this package free of an import cycle. Both
// resource clients of this group are served off the one shared testing.Fake.
type fakeGroupFixture struct {
	testingFake *k8stesting.Fake
	groupClient *FakeNvidiaV1alpha1
}

func newFakeGroupFixture(t *testing.T, seedObjects ...runtime.Object) *fakeGroupFixture {
	t.Helper()

	tracker := k8stesting.NewObjectTracker(scheme.Scheme, scheme.Codecs.UniversalDecoder())
	for _, seedObject := range seedObjects {
		require.NoError(t, tracker.Add(seedObject))
	}

	testingFake := &k8stesting.Fake{}
	testingFake.AddReactor("*", "*", k8stesting.ObjectReaction(tracker))
	testingFake.AddWatchReactor("*", func(action k8stesting.Action) (bool, watch.Interface, error) {
		watchAction, ok := action.(k8stesting.WatchActionImpl)
		if !ok {
			return true, nil, fmt.Errorf("watch reactor: unexpected action type %T", action)
		}
		watcher, err := tracker.Watch(action.GetResource(), action.GetNamespace(), watchAction.ListOptions)
		if err != nil {
			return false, nil, err
		}
		return true, watcher, nil
	})

	return &fakeGroupFixture{
		testingFake: testingFake,
		groupClient: &FakeNvidiaV1alpha1{Fake: testingFake},
	}
}

// lastAction returns the most recently recorded action, failing if none exist.
func lastAction(t *testing.T, testingFake *k8stesting.Fake) k8stesting.Action {
	t.Helper()
	actions := testingFake.Actions()
	require.NotEmpty(t, actions)
	return actions[len(actions)-1]
}

func TestFakeNvidiaV1alpha1NVIDIADriversReturnsUsableClient(t *testing.T) {
	groupClient := &FakeNvidiaV1alpha1{Fake: &k8stesting.Fake{}}

	drivers := groupClient.NVIDIADrivers()
	require.NotNil(t, drivers)

	// The concrete type is the generated fake, wired back to the group client.
	fakeDrivers, ok := drivers.(*fakeNVIDIADrivers)
	require.True(t, ok, "expected NVIDIADrivers() to return *fakeNVIDIADrivers, got %T", drivers)
	assert.Same(t, groupClient, fakeDrivers.Fake, "fake driver client must point back at its group client")
	assert.Equal(t, nvidiaDriverGVR, fakeDrivers.Resource())
	assert.Equal(t, nvidiaDriverGVK, fakeDrivers.Kind())
	// NVIDIADriver is a cluster-scoped resource (+genclient:nonNamespaced), so
	// the generated client is constructed with an empty namespace.
	assert.Empty(t, fakeDrivers.Namespace())
}

func TestFakeNvidiaV1alpha1GPUClustersReturnsUsableClient(t *testing.T) {
	groupClient := &FakeNvidiaV1alpha1{Fake: &k8stesting.Fake{}}

	clusters := groupClient.GPUClusters()
	require.NotNil(t, clusters)

	fakeClusters, ok := clusters.(*fakeGPUClusters)
	require.True(t, ok, "expected GPUClusters() to return *fakeGPUClusters, got %T", clusters)
	assert.Same(t, groupClient, fakeClusters.Fake, "fake cluster client must point back at its group client")
	assert.Equal(t, gpuClusterGVR, fakeClusters.Resource())
	assert.Equal(t, gpuClusterGVK, fakeClusters.Kind())
	// GPUCluster is likewise cluster scoped (+genclient:nonNamespaced).
	assert.Empty(t, fakeClusters.Namespace())
}

func TestFakeNvidiaV1alpha1AccessorsShareActionRecorder(t *testing.T) {
	testingFake := &k8stesting.Fake{}
	groupClient := &FakeNvidiaV1alpha1{Fake: testingFake}

	firstDrivers := groupClient.NVIDIADrivers()
	secondDrivers := groupClient.NVIDIADrivers()
	// Each call builds a fresh struct...
	assert.NotSame(t, firstDrivers, secondDrivers)

	// ...but every accessor funnels its actions into the single shared
	// testing.Fake, so a test can assert across both resources at once.
	ctx := t.Context()
	_, _ = firstDrivers.Get(ctx, "nvidia-driver-a", metav1.GetOptions{})
	_, _ = secondDrivers.Get(ctx, "nvidia-driver-b", metav1.GetOptions{})
	_, _ = groupClient.GPUClusters().Get(ctx, "gpu-cluster-a", metav1.GetOptions{})

	actions := testingFake.Actions()
	require.Len(t, actions, 3)

	expectedGetActions := []struct {
		objectName           string
		groupVersionResource schema.GroupVersionResource
	}{
		{objectName: "nvidia-driver-a", groupVersionResource: nvidiaDriverGVR},
		{objectName: "nvidia-driver-b", groupVersionResource: nvidiaDriverGVR},
		{objectName: "gpu-cluster-a", groupVersionResource: gpuClusterGVR},
	}

	for actionIndex, expectedAction := range expectedGetActions {
		getAction, ok := actions[actionIndex].(k8stesting.GetAction)
		require.True(t, ok, "action %d: expected a GetAction, got %T", actionIndex, actions[actionIndex])
		assert.Equal(t, expectedAction.objectName, getAction.GetName(), "action %d name", actionIndex)
		assert.Equal(t, expectedAction.groupVersionResource, getAction.GetResource(), "action %d resource", actionIndex)
	}
}

// TestFakeNvidiaV1alpha1ResourcesAreIndependentlyTracked proves the two
// resources of this group do not alias each other in the tracker even though
// they share a testing.Fake and a reaction chain.
func TestFakeNvidiaV1alpha1ResourcesAreIndependentlyTracked(t *testing.T) {
	fixture := newFakeGroupFixture(t, newNVIDIADriver("shared-name", nil), newGPUCluster("shared-name", nil))
	ctx := t.Context()

	require.NoError(t, fixture.groupClient.NVIDIADrivers().Delete(ctx, "shared-name", metav1.DeleteOptions{}))

	_, err := fixture.groupClient.NVIDIADrivers().Get(ctx, "shared-name", metav1.GetOptions{})
	require.Error(t, err)

	// The identically named GPUCluster is untouched.
	cluster, err := fixture.groupClient.GPUClusters().Get(ctx, "shared-name", metav1.GetOptions{})
	require.NoError(t, err)
	assert.Equal(t, "shared-name", cluster.Name)
}

// TestFakeNvidiaV1alpha1RESTClientIsTypedNil documents a sharp edge of the
// generated stub:
//
//	func (c *FakeNvidiaV1alpha1) RESTClient() rest.Interface {
//	    var ret *rest.RESTClient
//	    return ret
//	}
//
// The caller receives a NON-nil rest.Interface whose dynamic value is a nil
// *rest.RESTClient, so a plain `if c.RESTClient() == nil` check does NOT fire.
// The returned client has no usable transport (no config, no base URL, no
// round-tripper), so it cannot be used to issue requests. Exactly how
// client-go's rest package reacts to that is an upstream detail and is not
// asserted here.
func TestFakeNvidiaV1alpha1RESTClientIsTypedNil(t *testing.T) {
	groupClient := &FakeNvidiaV1alpha1{Fake: &k8stesting.Fake{}}

	gotRESTClient := groupClient.RESTClient()

	// The interface value itself is not the untyped nil...
	var nilRESTInterface rest.Interface
	assert.NotEqual(t, nilRESTInterface, gotRESTClient,
		"RESTClient() returns a non-nil interface holding a nil pointer")

	// ...but the dynamic value is a nil *rest.RESTClient.
	restClient, ok := gotRESTClient.(*rest.RESTClient)
	require.True(t, ok, "expected dynamic type *rest.RESTClient, got %T", gotRESTClient)
	assert.Nil(t, restClient)

	// testify's assert.Nil is reflection based, so it agrees the value is nil.
	assert.Nil(t, gotRESTClient)
}
