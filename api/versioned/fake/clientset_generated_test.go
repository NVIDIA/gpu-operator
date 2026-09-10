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
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/version"
	"k8s.io/apimachinery/pkg/watch"
	fakediscovery "k8s.io/client-go/discovery/fake"
	k8stesting "k8s.io/client-go/testing"

	nvidiav1 "github.com/NVIDIA/gpu-operator/api/nvidia/v1"
	nvidiav1alpha1 "github.com/NVIDIA/gpu-operator/api/nvidia/v1alpha1"
	fakenvidiav1 "github.com/NVIDIA/gpu-operator/api/versioned/typed/nvidia/v1/fake"
	fakenvidiav1alpha1 "github.com/NVIDIA/gpu-operator/api/versioned/typed/nvidia/v1alpha1/fake"
)

const watchEventTimeout = 2 * time.Second

var (
	clusterPolicyGVR = nvidiav1.SchemeGroupVersion.WithResource("clusterpolicies")
	nvidiaDriverGVR  = nvidiav1alpha1.SchemeGroupVersion.WithResource("nvidiadrivers")
)

type unregisteredTestType struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitzero"`
}

func (testType *unregisteredTestType) DeepCopyObject() runtime.Object {
	return &unregisteredTestType{
		TypeMeta:   testType.TypeMeta,
		ObjectMeta: *testType.DeepCopy(),
	}
}

type objectWithMetadata interface {
	runtime.Object
	metav1.Object
}

type resourceClient[T objectWithMetadata] interface {
	Create(ctx context.Context, object T, opts metav1.CreateOptions) (T, error)
	Get(ctx context.Context, name string, opts metav1.GetOptions) (T, error)
	Update(ctx context.Context, object T, opts metav1.UpdateOptions) (T, error)
	Delete(ctx context.Context, name string, opts metav1.DeleteOptions) error
}

func assertCRUDRoundTrip[T objectWithMetadata](t *testing.T, client resourceClient[T], object T) {
	t.Helper()
	ctx := t.Context()
	name := object.GetName()

	createdObject, err := client.Create(ctx, object, metav1.CreateOptions{})
	require.NoError(t, err)
	assert.Equal(t, name, createdObject.GetName())

	_, err = client.Create(ctx, object, metav1.CreateOptions{})
	require.Error(t, err)
	assert.True(t, apierrors.IsAlreadyExists(err), "expected AlreadyExists, got %v", err)

	gotObject, err := client.Get(ctx, name, metav1.GetOptions{})
	require.NoError(t, err)

	gotObject.SetLabels(map[string]string{"app": "updated"})
	updatedObject, err := client.Update(ctx, gotObject, metav1.UpdateOptions{})
	require.NoError(t, err)
	assert.Equal(t, map[string]string{"app": "updated"}, updatedObject.GetLabels())

	gotObject, err = client.Get(ctx, name, metav1.GetOptions{})
	require.NoError(t, err)
	assert.Equal(t, map[string]string{"app": "updated"}, gotObject.GetLabels())

	require.NoError(t, client.Delete(ctx, name, metav1.DeleteOptions{}))

	_, err = client.Get(ctx, name, metav1.GetOptions{})
	require.Error(t, err)
	assert.True(t, apierrors.IsNotFound(err), "expected NotFound after delete, got %v", err)
}

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

func newClusterPolicy(name string) *nvidiav1.ClusterPolicy {
	return &nvidiav1.ClusterPolicy{
		ObjectMeta: metav1.ObjectMeta{
			Name:   name,
			Labels: map[string]string{"app": "gpu-operator"},
		},
		Spec: nvidiav1.ClusterPolicySpec{
			Operator: nvidiav1.OperatorSpec{
				RuntimeClass: "nvidia",
			},
		},
	}
}

func newNVIDIADriver(name string) *nvidiav1alpha1.NVIDIADriver {
	return &nvidiav1alpha1.NVIDIADriver{
		ObjectMeta: metav1.ObjectMeta{Name: name},
		Spec: nvidiav1alpha1.NVIDIADriverSpec{
			DriverType: nvidiav1alpha1.GPU,
		},
	}
}

func newGPUCluster(name string) *nvidiav1alpha1.GPUCluster {
	return &nvidiav1alpha1.GPUCluster{
		ObjectMeta: metav1.ObjectMeta{Name: name},
		Spec: nvidiav1alpha1.GPUClusterSpec{
			DRADriver: nvidiav1alpha1.DRADriverSpec{Repository: "nvcr.io/nvidia/cloud-native"},
		},
	}
}

func TestEveryResourceSurvivesACRUDRoundTrip(t *testing.T) {
	clientset := NewSimpleClientset()

	t.Run("ClusterPolicy", func(t *testing.T) {
		assertCRUDRoundTrip(t, clientset.NvidiaV1().ClusterPolicies(), newClusterPolicy("cluster-policy-a"))
	})
	t.Run("NVIDIADriver", func(t *testing.T) {
		assertCRUDRoundTrip(t, clientset.NvidiaV1alpha1().NVIDIADrivers(), newNVIDIADriver("nvidia-driver-a"))
	})
	t.Run("GPUCluster", func(t *testing.T) {
		assertCRUDRoundTrip(t, clientset.NvidiaV1alpha1().GPUClusters(), newGPUCluster("gpu-cluster"))
	})
}

func TestNewSimpleClientsetSeedsTracker(t *testing.T) {
	ctx := t.Context()
	clientset := NewSimpleClientset(
		newClusterPolicy("cluster-policy-a"),
		newClusterPolicy("cluster-policy-b"),
		newNVIDIADriver("nvidia-driver-a"),
	)

	clusterPolicyList, err := clientset.NvidiaV1().ClusterPolicies().List(ctx, metav1.ListOptions{})
	require.NoError(t, err)
	clusterPolicyNames := make([]string, 0, len(clusterPolicyList.Items))
	for _, clusterPolicy := range clusterPolicyList.Items {
		clusterPolicyNames = append(clusterPolicyNames, clusterPolicy.Name)
	}
	assert.ElementsMatch(t, []string{"cluster-policy-a", "cluster-policy-b"}, clusterPolicyNames)

	nvidiaDriver, err := clientset.NvidiaV1alpha1().NVIDIADrivers().Get(ctx, "nvidia-driver-a", metav1.GetOptions{})
	require.NoError(t, err)
	assert.Equal(t, nvidiav1alpha1.GPU, nvidiaDriver.Spec.DriverType)

	gpuClusterList, err := clientset.NvidiaV1alpha1().GPUClusters().List(ctx, metav1.ListOptions{})
	require.NoError(t, err)
	assert.Empty(t, gpuClusterList.Items)

	_, err = clientset.NvidiaV1alpha1().NVIDIADrivers().Get(ctx, "cluster-policy-a", metav1.GetOptions{})
	require.Error(t, err)
	assert.True(t, apierrors.IsNotFound(err), "expected NotFound, got %v", err)
}

func TestNewSimpleClientsetPanicsOnUnregisteredType(t *testing.T) {
	var recoveredPanic any
	func() {
		defer func() { recoveredPanic = recover() }()
		NewSimpleClientset(&unregisteredTestType{ObjectMeta: metav1.ObjectMeta{Name: "unregistered-object"}})
	}()
	require.NotNil(t, recoveredPanic, "seeding an unregistered type must panic")
	err, ok := recoveredPanic.(error)
	require.True(t, ok, "expected the panic value to be an error, got %T", recoveredPanic)
	assert.True(t, runtime.IsNotRegisteredError(err), "expected a not-registered error, got %v", err)

	assert.NotPanics(t, func() {
		NewSimpleClientset(newClusterPolicy("cluster-policy-a"))
	})
}

func TestWatchReactorDeliversEvents(t *testing.T) {
	ctx := t.Context()
	clientset := NewSimpleClientset()
	clusterPolicyClient := clientset.NvidiaV1().ClusterPolicies()

	watcher, err := clusterPolicyClient.Watch(ctx, metav1.ListOptions{})
	require.NoError(t, err)
	defer watcher.Stop()

	createdClusterPolicy, err := clusterPolicyClient.Create(ctx, newClusterPolicy("cluster-policy-a"), metav1.CreateOptions{})
	require.NoError(t, err)
	event := requireWatchEvent(t, watcher.ResultChan())
	assert.Equal(t, watch.Added, event.Type)
	addedClusterPolicy, ok := event.Object.(*nvidiav1.ClusterPolicy)
	require.True(t, ok, "expected *nvidiav1.ClusterPolicy, got %T", event.Object)
	assert.Equal(t, "cluster-policy-a", addedClusterPolicy.Name)

	createdClusterPolicy.Spec.Operator.RuntimeClass = "nvidia-crio"
	_, err = clusterPolicyClient.Update(ctx, createdClusterPolicy, metav1.UpdateOptions{})
	require.NoError(t, err)
	event = requireWatchEvent(t, watcher.ResultChan())
	assert.Equal(t, watch.Modified, event.Type)
	modifiedClusterPolicy, ok := event.Object.(*nvidiav1.ClusterPolicy)
	require.True(t, ok, "expected *nvidiav1.ClusterPolicy, got %T", event.Object)
	assert.Equal(t, "nvidia-crio", modifiedClusterPolicy.Spec.Operator.RuntimeClass)

	require.NoError(t, clusterPolicyClient.Delete(ctx, "cluster-policy-a", metav1.DeleteOptions{}))
	event = requireWatchEvent(t, watcher.ResultChan())
	assert.Equal(t, watch.Deleted, event.Type)
}

func TestWatchReplaysSeededObjectsAsAdded(t *testing.T) {
	ctx := t.Context()
	clientset := NewSimpleClientset(newClusterPolicy("cluster-policy-a"))

	watcher, err := clientset.NvidiaV1().ClusterPolicies().Watch(ctx, metav1.ListOptions{})
	require.NoError(t, err)
	defer watcher.Stop()

	event := requireWatchEvent(t, watcher.ResultChan())
	assert.Equal(t, watch.Added, event.Type)
	replayedClusterPolicy, ok := event.Object.(*nvidiav1.ClusterPolicy)
	require.True(t, ok, "expected *nvidiav1.ClusterPolicy, got %T", event.Object)
	assert.Equal(t, "cluster-policy-a", replayedClusterPolicy.Name)
}

func TestWatchOnlyDeliversEventsForItsOwnResource(t *testing.T) {
	ctx := t.Context()
	clientset := NewSimpleClientset()

	nvidiaDriverWatcher, err := clientset.NvidiaV1alpha1().NVIDIADrivers().Watch(ctx, metav1.ListOptions{})
	require.NoError(t, err)
	defer nvidiaDriverWatcher.Stop()

	_, err = clientset.NvidiaV1().ClusterPolicies().Create(ctx, newClusterPolicy("cluster-policy-a"), metav1.CreateOptions{})
	require.NoError(t, err)

	_, err = clientset.NvidiaV1alpha1().NVIDIADrivers().Create(ctx, newNVIDIADriver("nvidia-driver-a"), metav1.CreateOptions{})
	require.NoError(t, err)

	firstEventAfterTheForeignCreate := requireWatchEvent(t, nvidiaDriverWatcher.ResultChan())
	assert.Equal(t, watch.Added, firstEventAfterTheForeignCreate.Type)
	addedNVIDIADriver, ok := firstEventAfterTheForeignCreate.Object.(*nvidiav1alpha1.NVIDIADriver)
	require.True(t, ok, "expected *nvidiav1alpha1.NVIDIADriver, got %T", firstEventAfterTheForeignCreate.Object)
	assert.Equal(t, "nvidia-driver-a", addedNVIDIADriver.Name)
}

func TestWatchReactorAcceptsActionWithoutListOptions(t *testing.T) {
	clientset := NewSimpleClientset(newClusterPolicy("cluster-policy-a"))

	watcher, err := clientset.InvokesWatch(k8stesting.ActionImpl{Verb: "watch", Resource: clusterPolicyGVR})
	require.NoError(t, err)
	require.NotNil(t, watcher)
	defer watcher.Stop()

	event := requireWatchEvent(t, watcher.ResultChan())
	assert.Equal(t, watch.Added, event.Type)
	replayedClusterPolicy, ok := event.Object.(*nvidiav1.ClusterPolicy)
	require.True(t, ok, "expected *nvidiav1.ClusterPolicy, got %T", event.Object)
	assert.Equal(t, "cluster-policy-a", replayedClusterPolicy.Name)
}

func TestWatchOnAnInvalidResourceVersionReturnsNoWatcher(t *testing.T) {
	ctx := t.Context()
	clientset := NewSimpleClientset()

	watcher, err := clientset.NvidiaV1().ClusterPolicies().Watch(ctx, metav1.ListOptions{ResourceVersion: "not-an-int"})
	require.Error(t, err)
	var nilWatcher watch.Interface
	assert.Equal(t, nilWatcher, watcher)
}

func TestClientsetRecordsEveryActionInOrder(t *testing.T) {
	ctx := t.Context()
	clientset := NewSimpleClientset()

	clusterPolicy, err := clientset.NvidiaV1().ClusterPolicies().Create(ctx, newClusterPolicy("cluster-policy-a"), metav1.CreateOptions{})
	require.NoError(t, err)
	_, err = clientset.NvidiaV1().ClusterPolicies().Get(ctx, "cluster-policy-a", metav1.GetOptions{})
	require.NoError(t, err)
	_, err = clientset.NvidiaV1().ClusterPolicies().List(ctx, metav1.ListOptions{})
	require.NoError(t, err)
	_, err = clientset.NvidiaV1().ClusterPolicies().UpdateStatus(ctx, clusterPolicy, metav1.UpdateOptions{})
	require.NoError(t, err)
	require.NoError(t, clientset.NvidiaV1().ClusterPolicies().Delete(ctx, "cluster-policy-a", metav1.DeleteOptions{}))

	expectedActions := []struct {
		verb        string
		subresource string
	}{
		{verb: "create"},
		{verb: "get"},
		{verb: "list"},
		{verb: "update", subresource: "status"},
		{verb: "delete"},
	}

	actions := clientset.Actions()
	require.Len(t, actions, len(expectedActions))

	for actionIndex, expectedAction := range expectedActions {
		assert.Equalf(t, expectedAction.verb, actions[actionIndex].GetVerb(), "action %d verb", actionIndex)
		assert.Equalf(t, expectedAction.subresource, actions[actionIndex].GetSubresource(), "action %d subresource", actionIndex)
		assert.Equalf(t, clusterPolicyGVR, actions[actionIndex].GetResource(), "action %d resource", actionIndex)
		assert.Emptyf(t, actions[actionIndex].GetNamespace(), "action %d namespace", actionIndex)
	}

	_, err = clientset.NvidiaV1alpha1().NVIDIADrivers().List(ctx, metav1.ListOptions{})
	require.NoError(t, err)
	actions = clientset.Actions()
	require.Len(t, actions, len(expectedActions)+1, "every group client must record onto the same Fake")
	assert.Equal(t, nvidiaDriverGVR, actions[len(expectedActions)].GetResource())
}

func TestPrependReactorInterceptsChain(t *testing.T) {
	ctx := t.Context()
	clientset := NewSimpleClientset(newClusterPolicy("cluster-policy-a"))

	clientset.PrependReactor("get", "clusterpolicies", func(_ k8stesting.Action) (bool, runtime.Object, error) {
		return true, nil, apierrors.NewInternalError(assert.AnError)
	})

	_, err := clientset.NvidiaV1().ClusterPolicies().Get(ctx, "cluster-policy-a", metav1.GetOptions{})
	require.Error(t, err)
	assert.True(t, apierrors.IsInternalError(err), "expected the canned internal error, got %v", err)

	clusterPolicyList, err := clientset.NvidiaV1().ClusterPolicies().List(ctx, metav1.ListOptions{})
	require.NoError(t, err)
	assert.Len(t, clusterPolicyList.Items, 1)

	_, err = clientset.NvidiaV1alpha1().NVIDIADrivers().Get(ctx, "nvidia-driver-a", metav1.GetOptions{})
	require.Error(t, err)
	assert.True(t, apierrors.IsNotFound(err), "expected NotFound from the tracker, got %v", err)
}

func TestTrackerIsShared(t *testing.T) {
	ctx := t.Context()
	clientset := NewSimpleClientset()

	require.NoError(t, clientset.Tracker().Add(newClusterPolicy("cluster-policy-a")))
	gotClusterPolicy, err := clientset.NvidiaV1().ClusterPolicies().Get(ctx, "cluster-policy-a", metav1.GetOptions{})
	require.NoError(t, err)
	assert.Equal(t, "cluster-policy-a", gotClusterPolicy.Name)

	_, err = clientset.NvidiaV1alpha1().NVIDIADrivers().Create(ctx, newNVIDIADriver("nvidia-driver-a"), metav1.CreateOptions{})
	require.NoError(t, err)
	storedObject, err := clientset.Tracker().Get(nvidiaDriverGVR, "", "nvidia-driver-a")
	require.NoError(t, err)
	nvidiaDriver, ok := storedObject.(*nvidiav1alpha1.NVIDIADriver)
	require.True(t, ok, "expected *nvidiav1alpha1.NVIDIADriver, got %T", storedObject)
	assert.Equal(t, "nvidia-driver-a", nvidiaDriver.Name)

	assert.Same(t, clientset.tracker, clientset.Tracker())
}

func TestDiscoveryIsWiredToTheSharedFake(t *testing.T) {
	clientset := NewSimpleClientset()

	discoveryClient := clientset.Discovery()
	require.NotNil(t, discoveryClient)
	assert.Same(t, discoveryClient, clientset.Discovery(), "every call must hand back the one discovery client")

	fakeDiscovery, ok := discoveryClient.(*fakediscovery.FakeDiscovery)
	require.True(t, ok, "expected *fakediscovery.FakeDiscovery, got %T", discoveryClient)
	assert.Same(t, &clientset.Fake, fakeDiscovery.Fake, "discovery must be wired to the clientset's Fake")

	fakeDiscovery.FakedServerVersion = &version.Info{GitVersion: "v1.31.0", Major: "1", Minor: "31"}
	serverVersion, err := discoveryClient.ServerVersion()
	require.NoError(t, err)
	assert.Equal(t, "v1.31.0", serverVersion.GitVersion)

	actions := clientset.Actions()
	require.Len(t, actions, 1, "discovery calls must land on the clientset's action log")
	assert.Equal(t, "version", actions[0].GetResource().Resource)
}

func TestClientsetOptsOutOfWatchListSemantics(t *testing.T) {
	assert.True(t, NewSimpleClientset().IsWatchListSemanticsUnSupported())
}

func TestGroupClientsShareTheSameFake(t *testing.T) {
	clientset := NewSimpleClientset()

	v1Group, ok := clientset.NvidiaV1().(*fakenvidiav1.FakeNvidiaV1)
	require.True(t, ok, "expected *fakenvidiav1.FakeNvidiaV1, got %T", clientset.NvidiaV1())
	assert.Same(t, &clientset.Fake, v1Group.Fake)

	v1alpha1Group, ok := clientset.NvidiaV1alpha1().(*fakenvidiav1alpha1.FakeNvidiaV1alpha1)
	require.True(t, ok, "expected *fakenvidiav1alpha1.FakeNvidiaV1alpha1, got %T", clientset.NvidiaV1alpha1())
	assert.Same(t, &clientset.Fake, v1alpha1Group.Fake)
}
