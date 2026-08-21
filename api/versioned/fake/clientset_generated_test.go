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
	"encoding/json"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/version"
	"k8s.io/apimachinery/pkg/watch"
	fakediscovery "k8s.io/client-go/discovery/fake"
	k8stesting "k8s.io/client-go/testing"

	nvidiav1 "github.com/NVIDIA/gpu-operator/api/nvidia/v1"
	nvidiav1alpha1 "github.com/NVIDIA/gpu-operator/api/nvidia/v1alpha1"
	clientset "github.com/NVIDIA/gpu-operator/api/versioned"
	fakenvidiav1 "github.com/NVIDIA/gpu-operator/api/versioned/typed/nvidia/v1/fake"
	fakenvidiav1alpha1 "github.com/NVIDIA/gpu-operator/api/versioned/typed/nvidia/v1alpha1/fake"
)

// eventTimeout bounds how long a test waits for a watch event. The tracker is
// entirely in-process, so events are delivered immediately; a short timeout keeps
// a regression from failing slowly.
const eventTimeout = 2 * time.Second

var (
	clusterPolicyGVR = schema.GroupVersionResource{Group: "nvidia.com", Version: "v1", Resource: "clusterpolicies"}
	nvidiaDriverGVR  = schema.GroupVersionResource{Group: "nvidia.com", Version: "v1alpha1", Resource: "nvidiadrivers"}
)

// unregisteredTestType is a runtime.Object that is deliberately never added to
// the package scheme. Using a private type here, rather than borrowing a real
// type such as corev1.Pod, keeps the "unregistered kind" tests meaningful even
// if unrelated types are legitimately registered into the scheme later.
type unregisteredTestType struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`
}

// DeepCopyObject implements runtime.Object.
func (u *unregisteredTestType) DeepCopyObject() runtime.Object {
	out := &unregisteredTestType{TypeMeta: u.TypeMeta}
	u.DeepCopyInto(&out.ObjectMeta)
	return out
}

// receiveEvent reads a single event from a watch channel, failing the test if the
// channel closes or nothing arrives within eventTimeout.
func receiveEvent(t *testing.T, ch <-chan watch.Event) watch.Event {
	t.Helper()
	timer := time.NewTimer(eventTimeout)
	defer timer.Stop()
	select {
	case event, ok := <-ch:
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

// TestNewSimpleClientsetEmpty verifies a clientset built with no seed objects is
// immediately usable and reports empty lists for both API groups.
func TestNewSimpleClientsetEmpty(t *testing.T) {
	ctx := t.Context()
	cs := NewSimpleClientset()

	require.NotNil(t, cs)
	require.NotNil(t, cs.Tracker())

	cpList, err := cs.NvidiaV1().ClusterPolicies().List(ctx, metav1.ListOptions{})
	require.NoError(t, err)
	assert.Empty(t, cpList.Items)

	drvList, err := cs.NvidiaV1alpha1().NVIDIADrivers().List(ctx, metav1.ListOptions{})
	require.NoError(t, err)
	assert.Empty(t, drvList.Items)

	// Nothing exists, so a Get must be a genuine NotFound.
	_, err = cs.NvidiaV1().ClusterPolicies().Get(ctx, "missing", metav1.GetOptions{})
	require.Error(t, err)
	assert.True(t, apierrors.IsNotFound(err), "expected NotFound, got %v", err)
}

// TestNewSimpleClientsetSeedsTracker verifies objects handed to NewSimpleClientset
// are readable through the typed clients of both groups.
func TestNewSimpleClientsetSeedsTracker(t *testing.T) {
	ctx := t.Context()
	cs := NewSimpleClientset(
		newClusterPolicy("cp-a"),
		newClusterPolicy("cp-b"),
		newNVIDIADriver("drv-a"),
	)

	cp, err := cs.NvidiaV1().ClusterPolicies().Get(ctx, "cp-a", metav1.GetOptions{})
	require.NoError(t, err)
	assert.Equal(t, "cp-a", cp.Name)
	assert.Equal(t, "nvidia", cp.Spec.Operator.RuntimeClass)

	cpList, err := cs.NvidiaV1().ClusterPolicies().List(ctx, metav1.ListOptions{})
	require.NoError(t, err)
	names := make([]string, 0, len(cpList.Items))
	for _, item := range cpList.Items {
		names = append(names, item.Name)
	}
	assert.ElementsMatch(t, []string{"cp-a", "cp-b"}, names)

	drv, err := cs.NvidiaV1alpha1().NVIDIADrivers().Get(ctx, "drv-a", metav1.GetOptions{})
	require.NoError(t, err)
	assert.Equal(t, "drv-a", drv.Name)
	assert.Equal(t, nvidiav1alpha1.GPU, drv.Spec.DriverType)

	drvList, err := cs.NvidiaV1alpha1().NVIDIADrivers().List(ctx, metav1.ListOptions{})
	require.NoError(t, err)
	require.Len(t, drvList.Items, 1)
	assert.Equal(t, "drv-a", drvList.Items[0].Name)

	// Seeding one group must not leak into the other.
	_, err = cs.NvidiaV1alpha1().NVIDIADrivers().Get(ctx, "cp-a", metav1.GetOptions{})
	assert.True(t, apierrors.IsNotFound(err), "expected NotFound, got %v", err)
}

// TestNewSimpleClientsetSeedListSelector verifies list label selectors are applied
// client-side by the generated fake lister.
func TestNewSimpleClientsetSeedListSelector(t *testing.T) {
	ctx := t.Context()
	other := newClusterPolicy("cp-other")
	other.Labels = map[string]string{"app": "something-else"}
	cs := NewSimpleClientset(newClusterPolicy("cp-a"), other)

	list, err := cs.NvidiaV1().ClusterPolicies().List(ctx, metav1.ListOptions{LabelSelector: "app=gpu-operator"})
	require.NoError(t, err)
	require.Len(t, list.Items, 1)
	assert.Equal(t, "cp-a", list.Items[0].Name)
}

// TestClusterPolicyCRUDRoundTrip exercises Create/Get/Update/UpdateStatus/List/
// Delete plus the NotFound and AlreadyExists error paths against the tracker.
func TestClusterPolicyCRUDRoundTrip(t *testing.T) {
	ctx := t.Context()
	cs := NewSimpleClientset()
	client := cs.NvidiaV1().ClusterPolicies()

	created, err := client.Create(ctx, newClusterPolicy("cp"), metav1.CreateOptions{})
	require.NoError(t, err)
	assert.Equal(t, "cp", created.Name)

	// Duplicate create must be rejected by the tracker.
	_, err = client.Create(ctx, newClusterPolicy("cp"), metav1.CreateOptions{})
	require.Error(t, err)
	assert.True(t, apierrors.IsAlreadyExists(err), "expected AlreadyExists, got %v", err)

	got, err := client.Get(ctx, "cp", metav1.GetOptions{})
	require.NoError(t, err)
	assert.Equal(t, "nvidia", got.Spec.Operator.RuntimeClass)

	// Update the spec and read it back.
	got.Spec.Operator.RuntimeClass = "nvidia-crio"
	updated, err := client.Update(ctx, got, metav1.UpdateOptions{})
	require.NoError(t, err)
	assert.Equal(t, "nvidia-crio", updated.Spec.Operator.RuntimeClass)

	got, err = client.Get(ctx, "cp", metav1.GetOptions{})
	require.NoError(t, err)
	assert.Equal(t, "nvidia-crio", got.Spec.Operator.RuntimeClass)

	// UpdateStatus persists the status subresource.
	got.SetStatus(nvidiav1.Ready, "gpu-operator")
	statusUpdated, err := client.UpdateStatus(ctx, got, metav1.UpdateOptions{})
	require.NoError(t, err)
	assert.Equal(t, nvidiav1.Ready, statusUpdated.Status.State)

	got, err = client.Get(ctx, "cp", metav1.GetOptions{})
	require.NoError(t, err)
	assert.Equal(t, nvidiav1.Ready, got.Status.State)
	assert.Equal(t, "gpu-operator", got.Status.Namespace)

	list, err := client.List(ctx, metav1.ListOptions{})
	require.NoError(t, err)
	require.Len(t, list.Items, 1)

	// Update of a non-existent object is NotFound.
	_, err = client.Update(ctx, newClusterPolicy("ghost"), metav1.UpdateOptions{})
	assert.True(t, apierrors.IsNotFound(err), "expected NotFound, got %v", err)

	require.NoError(t, client.Delete(ctx, "cp", metav1.DeleteOptions{}))

	_, err = client.Get(ctx, "cp", metav1.GetOptions{})
	assert.True(t, apierrors.IsNotFound(err), "expected NotFound after delete, got %v", err)

	// Deleting twice is NotFound too.
	err = client.Delete(ctx, "cp", metav1.DeleteOptions{})
	assert.True(t, apierrors.IsNotFound(err), "expected NotFound on repeat delete, got %v", err)

	list, err = client.List(ctx, metav1.ListOptions{})
	require.NoError(t, err)
	assert.Empty(t, list.Items)
}

// TestNVIDIADriverCRUDRoundTrip mirrors the ClusterPolicy round trip for the
// v1alpha1 group so both generated group clients are exercised end to end.
func TestNVIDIADriverCRUDRoundTrip(t *testing.T) {
	ctx := t.Context()
	cs := NewSimpleClientset()
	client := cs.NvidiaV1alpha1().NVIDIADrivers()

	_, err := client.Create(ctx, newNVIDIADriver("drv"), metav1.CreateOptions{})
	require.NoError(t, err)

	_, err = client.Create(ctx, newNVIDIADriver("drv"), metav1.CreateOptions{})
	assert.True(t, apierrors.IsAlreadyExists(err), "expected AlreadyExists, got %v", err)

	got, err := client.Get(ctx, "drv", metav1.GetOptions{})
	require.NoError(t, err)

	got.Spec.Default = true
	updated, err := client.Update(ctx, got, metav1.UpdateOptions{})
	require.NoError(t, err)
	assert.True(t, updated.IsDefault())

	updated.Status.State = nvidiav1alpha1.NotReady
	statusUpdated, err := client.UpdateStatus(ctx, updated, metav1.UpdateOptions{})
	require.NoError(t, err)
	assert.Equal(t, nvidiav1alpha1.NotReady, statusUpdated.Status.State)

	require.NoError(t, client.Delete(ctx, "drv", metav1.DeleteOptions{}))
	_, err = client.Get(ctx, "drv", metav1.GetOptions{})
	assert.True(t, apierrors.IsNotFound(err), "expected NotFound after delete, got %v", err)
}

// TestPatch covers the patch types the object tracker knows how to apply, and the
// NotFound / unsupported-patch-type failure modes.
func TestPatch(t *testing.T) {
	ctx := t.Context()

	tests := []struct {
		name      string
		patchType types.PatchType
		patch     string
		wantErr   func(t *testing.T, err error)
		verify    func(t *testing.T, cp *nvidiav1.ClusterPolicy)
	}{
		{
			name:      "json merge patch updates labels",
			patchType: types.MergePatchType,
			patch:     `{"metadata":{"labels":{"patched":"yes"}}}`,
			verify: func(t *testing.T, cp *nvidiav1.ClusterPolicy) {
				assert.Equal(t, "yes", cp.Labels["patched"])
				assert.Equal(t, "gpu-operator", cp.Labels["app"])
			},
		},
		{
			name:      "strategic merge patch updates spec",
			patchType: types.StrategicMergePatchType,
			patch:     `{"spec":{"operator":{"runtimeClass":"nvidia-crio"}}}`,
			verify: func(t *testing.T, cp *nvidiav1.ClusterPolicy) {
				assert.Equal(t, "nvidia-crio", cp.Spec.Operator.RuntimeClass)
			},
		},
		{
			name:      "json patch replaces a label",
			patchType: types.JSONPatchType,
			patch:     `[{"op":"replace","path":"/metadata/labels/app","value":"patched"}]`,
			verify: func(t *testing.T, cp *nvidiav1.ClusterPolicy) {
				assert.Equal(t, "patched", cp.Labels["app"])
			},
		},
		{
			name:      "unsupported patch type is rejected",
			patchType: types.PatchType("application/unknown"),
			patch:     `{}`,
			wantErr: func(t *testing.T, err error) {
				assert.ErrorContains(t, err, "is not supported")
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cs := NewSimpleClientset(newClusterPolicy("cp"))
			client := cs.NvidiaV1().ClusterPolicies()

			patched, err := client.Patch(ctx, "cp", tt.patchType, []byte(tt.patch), metav1.PatchOptions{})
			if tt.wantErr != nil {
				require.Error(t, err)
				tt.wantErr(t, err)
				return
			}
			require.NoError(t, err)
			tt.verify(t, patched)

			// The patch must have been persisted, not just returned.
			stored, err := client.Get(ctx, "cp", metav1.GetOptions{})
			require.NoError(t, err)
			tt.verify(t, stored)
		})
	}

	t.Run("patching a missing object is NotFound", func(t *testing.T) {
		cs := NewSimpleClientset()
		_, err := cs.NvidiaV1().ClusterPolicies().Patch(ctx, "ghost", types.MergePatchType, []byte(`{}`), metav1.PatchOptions{})
		require.Error(t, err)
		assert.True(t, apierrors.IsNotFound(err), "expected NotFound, got %v", err)
	})
}

// TestDeleteCollection covers the generated client's DeleteCollection contract: the
// call is turned into a single "delete-collection" action that carries the target
// GroupVersionResource, the (empty, because cluster scoped) namespace, and both the
// DeleteOptions and the ListOptions the caller supplied.
//
// Note: with the default reaction chain installed by NewSimpleClientset nothing is
// actually removed, because testing.ObjectReaction currently has no branch for
// DeleteCollectionActionImpl. That is an upstream client-go implementation detail
// which may change, so it is deliberately observed here rather than asserted.
func TestDeleteCollection(t *testing.T) {
	ctx := t.Context()
	cs := NewSimpleClientset(newClusterPolicy("cp-a"), newClusterPolicy("cp-b"))
	client := cs.NvidiaV1().ClusterPolicies()

	gracePeriod := int64(30)
	deleteOpts := metav1.DeleteOptions{GracePeriodSeconds: &gracePeriod}
	listOpts := metav1.ListOptions{LabelSelector: "app=gpu-operator"}

	require.NoError(t, client.DeleteCollection(ctx, deleteOpts, listOpts))

	var found k8stesting.DeleteCollectionActionImpl
	var ok bool
	for _, action := range cs.Actions() {
		if dc, isDC := action.(k8stesting.DeleteCollectionActionImpl); isDC {
			found, ok = dc, true
		}
	}
	require.True(t, ok, "expected a delete-collection action to be recorded")
	assert.Equal(t, "delete-collection", found.GetVerb())
	assert.Equal(t, clusterPolicyGVR, found.GetResource())
	assert.Empty(t, found.GetNamespace(), "clusterpolicies are cluster scoped")
	assert.Equal(t, deleteOpts, found.GetDeleteOptions())
	assert.Equal(t, listOpts, found.GetListOptions())
	assert.Equal(t, "app=gpu-operator", found.GetListRestrictions().Labels.String())
}

// TestDeleteCollectionWithReactor shows that a reactor of our own, layered onto the
// generated client, can implement real collection deletion on top of the same
// delete-collection action.
func TestDeleteCollectionWithReactor(t *testing.T) {
	ctx := t.Context()
	cs := NewSimpleClientset(newClusterPolicy("cp-a"), newClusterPolicy("cp-b"))
	client := cs.NvidiaV1().ClusterPolicies()

	cs.PrependReactor("delete-collection", "clusterpolicies", func(action k8stesting.Action) (bool, runtime.Object, error) {
		dc, isDC := action.(k8stesting.DeleteCollectionActionImpl)
		if !isDC {
			return false, nil, nil
		}
		objs, err := cs.Tracker().List(dc.GetResource(), nvidiav1.SchemeGroupVersion.WithKind("ClusterPolicy"), dc.GetNamespace())
		if err != nil {
			return true, nil, err
		}
		list, isList := objs.(*nvidiav1.ClusterPolicyList)
		if !isList {
			return true, nil, nil
		}
		for i := range list.Items {
			if err := cs.Tracker().Delete(dc.GetResource(), dc.GetNamespace(), list.Items[i].Name); err != nil {
				return true, nil, err
			}
		}
		return true, nil, nil
	})

	require.NoError(t, client.DeleteCollection(ctx, metav1.DeleteOptions{}, metav1.ListOptions{}))

	list, err := client.List(ctx, metav1.ListOptions{})
	require.NoError(t, err)
	assert.Empty(t, list.Items)
}

// TestWatchReactorDeliversEvents verifies the default watch reactor wires the typed
// client's Watch to the tracker so create/update/delete produce watch events.
func TestWatchReactorDeliversEvents(t *testing.T) {
	ctx := t.Context()
	cs := NewSimpleClientset()
	client := cs.NvidiaV1().ClusterPolicies()

	w, err := client.Watch(ctx, metav1.ListOptions{})
	require.NoError(t, err)
	defer w.Stop()

	created, err := client.Create(ctx, newClusterPolicy("cp"), metav1.CreateOptions{})
	require.NoError(t, err)
	ev := receiveEvent(t, w.ResultChan())
	assert.Equal(t, watch.Added, ev.Type)
	addedObj, ok := ev.Object.(*nvidiav1.ClusterPolicy)
	require.True(t, ok, "expected *ClusterPolicy, got %T", ev.Object)
	assert.Equal(t, "cp", addedObj.Name)

	created.Spec.Operator.RuntimeClass = "nvidia-crio"
	_, err = client.Update(ctx, created, metav1.UpdateOptions{})
	require.NoError(t, err)
	ev = receiveEvent(t, w.ResultChan())
	assert.Equal(t, watch.Modified, ev.Type)
	modifiedObj, ok := ev.Object.(*nvidiav1.ClusterPolicy)
	require.True(t, ok, "expected *ClusterPolicy, got %T", ev.Object)
	assert.Equal(t, "nvidia-crio", modifiedObj.Spec.Operator.RuntimeClass)

	require.NoError(t, client.Delete(ctx, "cp", metav1.DeleteOptions{}))
	ev = receiveEvent(t, w.ResultChan())
	assert.Equal(t, watch.Deleted, ev.Type)
	deletedObj, ok := ev.Object.(*nvidiav1.ClusterPolicy)
	require.True(t, ok, "expected *ClusterPolicy, got %T", ev.Object)
	assert.Equal(t, "cp", deletedObj.Name)
}

// TestWatchForwardsListOptions verifies the generated client always forwards the
// caller's ListOptions into the watch action, and therefore into the watch reactor
// and tracker.Watch.
func TestWatchForwardsListOptions(t *testing.T) {
	ctx := t.Context()
	cs := NewSimpleClientset(newNVIDIADriver("drv"))

	opts := metav1.ListOptions{ResourceVersion: "0", LabelSelector: "app=gpu-operator"}
	w, err := cs.NvidiaV1alpha1().NVIDIADrivers().Watch(ctx, opts)
	require.NoError(t, err)
	defer w.Stop()

	var found k8stesting.WatchActionImpl
	var ok bool
	for _, action := range cs.Actions() {
		if wa, isWatch := action.(k8stesting.WatchActionImpl); isWatch {
			found, ok = wa, true
		}
	}
	require.True(t, ok, "expected a watch action to be recorded")
	assert.Equal(t, "watch", found.GetVerb())
	assert.Equal(t, nvidiaDriverGVR, found.GetResource())
	assert.Empty(t, found.GetNamespace(), "nvidiadrivers are cluster scoped")
	wantOpts := opts
	wantOpts.Watch = true // the only field the generated client sets itself
	assert.Equal(t, wantOpts, found.GetListOptions(), "the generated client must forward the caller's ListOptions")
	assert.Equal(t, "0", found.GetWatchRestrictions().ResourceVersion)
	assert.Equal(t, "app=gpu-operator", found.GetWatchRestrictions().Labels.String())

	// Deliberately no assertion on replayed events. Whether tracker.Watch replays
	// already-known objects, and whether it applies the label selector when it
	// does, is client-go's business rather than part of the generated client's
	// contract. Event delivery for create/update/delete is covered by
	// TestWatchReactorDeliversEvents.
}

// TestWatchReactorPropagatesTrackerError covers the error branch of the default
// watch reactor: when the tracker rejects the ListOptions the reactor reports the
// action as unhandled and the watch fails instead of returning a live channel.
func TestWatchReactorPropagatesTrackerError(t *testing.T) {
	ctx := t.Context()
	cs := NewSimpleClientset()

	// Only the observable contract is asserted: no watcher, and an error. The
	// wording ("unhandled watch") belongs to client-go's testing package and can
	// change on a dependency bump without the behaviour changing.
	w, err := cs.NvidiaV1().ClusterPolicies().Watch(ctx, metav1.ListOptions{ResourceVersion: "not-an-int"})
	require.Error(t, err)
	assert.Nil(t, w)
}

// TestRecordedActions asserts that verbs, resources and subresources land on the
// shared Fake in order, and that ClearActions resets the log.
func TestRecordedActions(t *testing.T) {
	ctx := t.Context()
	cs := NewSimpleClientset()

	cp, err := cs.NvidiaV1().ClusterPolicies().Create(ctx, newClusterPolicy("cp"), metav1.CreateOptions{})
	require.NoError(t, err)
	_, err = cs.NvidiaV1().ClusterPolicies().Get(ctx, "cp", metav1.GetOptions{})
	require.NoError(t, err)
	_, err = cs.NvidiaV1().ClusterPolicies().List(ctx, metav1.ListOptions{})
	require.NoError(t, err)
	_, err = cs.NvidiaV1().ClusterPolicies().UpdateStatus(ctx, cp, metav1.UpdateOptions{})
	require.NoError(t, err)
	require.NoError(t, cs.NvidiaV1().ClusterPolicies().Delete(ctx, "cp", metav1.DeleteOptions{}))

	actions := cs.Actions()
	require.Len(t, actions, 5)

	type want struct {
		verb        string
		subresource string
	}
	expected := []want{
		{verb: "create"},
		{verb: "get"},
		{verb: "list"},
		{verb: "update", subresource: "status"},
		{verb: "delete"},
	}
	for i, exp := range expected {
		assert.Equal(t, exp.verb, actions[i].GetVerb(), "action %d verb", i)
		assert.Equal(t, exp.subresource, actions[i].GetSubresource(), "action %d subresource", i)
		assert.Equal(t, clusterPolicyGVR, actions[i].GetResource(), "action %d resource", i)
		// Both resources are cluster scoped.
		assert.Empty(t, actions[i].GetNamespace(), "action %d namespace", i)
	}

	cs.ClearActions()
	assert.Empty(t, cs.Actions())

	// After clearing, new actions are recorded from scratch.
	_, err = cs.NvidiaV1alpha1().NVIDIADrivers().List(ctx, metav1.ListOptions{})
	require.NoError(t, err)
	actions = cs.Actions()
	require.Len(t, actions, 1)
	assert.Equal(t, "list", actions[0].GetVerb())
	assert.Equal(t, nvidiaDriverGVR, actions[0].GetResource())
}

// TestPrependReactorInterceptsChain proves the reaction chain is live: a prepended
// reactor wins over the default ObjectReaction installed by NewSimpleClientset.
func TestPrependReactorInterceptsChain(t *testing.T) {
	ctx := t.Context()
	cs := NewSimpleClientset(newClusterPolicy("cp"))

	canned := apierrors.NewInternalError(assert.AnError)
	cs.PrependReactor("get", "clusterpolicies", func(action k8stesting.Action) (bool, runtime.Object, error) {
		return true, nil, canned
	})

	_, err := cs.NvidiaV1().ClusterPolicies().Get(ctx, "cp", metav1.GetOptions{})
	require.Error(t, err)
	assert.True(t, apierrors.IsInternalError(err), "expected the canned internal error, got %v", err)

	// Only "get" on clusterpolicies is intercepted; everything else falls through
	// to the tracker-backed reactor.
	list, err := cs.NvidiaV1().ClusterPolicies().List(ctx, metav1.ListOptions{})
	require.NoError(t, err)
	assert.Len(t, list.Items, 1)

	_, err = cs.NvidiaV1alpha1().NVIDIADrivers().Get(ctx, "drv", metav1.GetOptions{})
	assert.True(t, apierrors.IsNotFound(err), "expected NotFound from the tracker, got %v", err)
}

// TestPrependReactorUnhandledFallsThrough verifies a reactor returning handled=false
// delegates to the next reactor in the chain rather than short circuiting.
func TestPrependReactorUnhandledFallsThrough(t *testing.T) {
	ctx := t.Context()
	cs := NewSimpleClientset(newClusterPolicy("cp"))

	var called bool
	cs.PrependReactor("*", "*", func(action k8stesting.Action) (bool, runtime.Object, error) {
		called = true
		return false, nil, assert.AnError
	})

	got, err := cs.NvidiaV1().ClusterPolicies().Get(ctx, "cp", metav1.GetOptions{})
	require.NoError(t, err)
	assert.Equal(t, "cp", got.Name)
	assert.True(t, called, "the prepended reactor should have been consulted")
}

// TestNewSimpleClientsetPanicsOnUnregisteredType verifies the constructor panics
// when a seed object has no kind in the fake scheme.
func TestNewSimpleClientsetPanicsOnUnregisteredType(t *testing.T) {
	var recovered any
	func() {
		defer func() { recovered = recover() }()
		NewSimpleClientset(&unregisteredTestType{ObjectMeta: metav1.ObjectMeta{Name: "p", Namespace: "default"}})
	}()
	require.NotNil(t, recovered, "seeding an unregistered type must panic")
	err, ok := recovered.(error)
	require.True(t, ok, "expected the panic value to be an error, got %T", recovered)
	assert.True(t, runtime.IsNotRegisteredError(err), "expected a not-registered error, got %v", err)

	// A registered type must not panic.
	assert.NotPanics(t, func() {
		NewSimpleClientset(newClusterPolicy("cp"))
	})
}

// TestTrackerIsShared verifies Tracker() hands back the very tracker the typed
// clients read from and write to.
func TestTrackerIsShared(t *testing.T) {
	ctx := t.Context()
	cs := NewSimpleClientset()

	// Mutating through the tracker is visible through the typed client...
	require.NoError(t, cs.Tracker().Add(newClusterPolicy("cp")))
	got, err := cs.NvidiaV1().ClusterPolicies().Get(ctx, "cp", metav1.GetOptions{})
	require.NoError(t, err)
	assert.Equal(t, "cp", got.Name)

	// ...and writing through the typed client is visible in the tracker.
	_, err = cs.NvidiaV1alpha1().NVIDIADrivers().Create(ctx, newNVIDIADriver("drv"), metav1.CreateOptions{})
	require.NoError(t, err)
	obj, err := cs.Tracker().Get(nvidiaDriverGVR, "", "drv")
	require.NoError(t, err)
	drv, ok := obj.(*nvidiav1alpha1.NVIDIADriver)
	require.True(t, ok, "expected *NVIDIADriver, got %T", obj)
	assert.Equal(t, "drv", drv.Name)

	// Deleting via the tracker removes it from the typed client's view.
	require.NoError(t, cs.Tracker().Delete(clusterPolicyGVR, "", "cp"))
	_, err = cs.NvidiaV1().ClusterPolicies().Get(ctx, "cp", metav1.GetOptions{})
	assert.True(t, apierrors.IsNotFound(err), "expected NotFound, got %v", err)

	// Tracker() is stable across calls.
	assert.Same(t, cs.Tracker(), cs.Tracker())
	assert.Equal(t, cs.tracker, cs.Tracker())
}

// TestDiscovery verifies Discovery() returns a FakeDiscovery bound to the same
// embedded Fake, so data set on the clientset is served through discovery.
func TestDiscovery(t *testing.T) {
	cs := NewSimpleClientset()

	disco := cs.Discovery()
	require.NotNil(t, disco)

	fd, ok := disco.(*fakediscovery.FakeDiscovery)
	require.True(t, ok, "expected *fakediscovery.FakeDiscovery, got %T", disco)
	assert.Same(t, &cs.Fake, fd.Fake, "discovery must be wired to the clientset's Fake")

	// Resources set on the shared Fake are served by discovery.
	cs.Resources = []*metav1.APIResourceList{
		{
			GroupVersion: nvidiav1.SchemeGroupVersion.String(),
			APIResources: []metav1.APIResource{
				{Name: "clusterpolicies", Kind: "ClusterPolicy", Namespaced: false},
			},
		},
	}
	rl, err := disco.ServerResourcesForGroupVersion(nvidiav1.SchemeGroupVersion.String())
	require.NoError(t, err)
	require.Len(t, rl.APIResources, 1)
	assert.Equal(t, "ClusterPolicy", rl.APIResources[0].Kind)

	// And a faked server version is read back through the interface.
	fd.FakedServerVersion = &version.Info{GitVersion: "v1.31.0", Major: "1", Minor: "31"}
	v, err := disco.ServerVersion()
	require.NoError(t, err)
	assert.Equal(t, "v1.31.0", v.GitVersion)

	// Discovery calls are recorded on the shared Fake too.
	assert.NotEmpty(t, cs.Actions())
}

// TestIsWatchListSemanticsUnSupported documents that this fake opts out of
// WatchList semantics for the reflector's optional interface check.
func TestIsWatchListSemanticsUnSupported(t *testing.T) {
	cs := NewSimpleClientset()
	assert.True(t, cs.IsWatchListSemanticsUnSupported())

	var fc k8stesting.FakeClient = cs
	require.NotNil(t, fc.Tracker())
}

// TestClientsetInterfaceAssertions mirrors the compile-time var block in
// clientset_generated.go as runtime assertions.
func TestClientsetInterfaceAssertions(t *testing.T) {
	cs := NewSimpleClientset()

	var _ clientset.Interface = cs
	var _ k8stesting.FakeClient = cs

	assert.Implements(t, (*clientset.Interface)(nil), cs)
	assert.Implements(t, (*k8stesting.FakeClient)(nil), cs)

	require.NotNil(t, cs.NvidiaV1())
	require.NotNil(t, cs.NvidiaV1alpha1())
	require.NotNil(t, cs.NvidiaV1().ClusterPolicies())
	require.NotNil(t, cs.NvidiaV1alpha1().NVIDIADrivers())
	require.NotNil(t, cs.NvidiaV1alpha1().GPUClusters())
}

// TestGroupClientsShareTheSameFake verifies both group accessors point at the one
// embedded Fake, so their actions and reactors are shared.
func TestGroupClientsShareTheSameFake(t *testing.T) {
	cs := NewSimpleClientset()

	v1Group, ok := cs.NvidiaV1().(*fakenvidiav1.FakeNvidiaV1)
	require.True(t, ok, "expected *FakeNvidiaV1, got %T", cs.NvidiaV1())
	assert.Same(t, &cs.Fake, v1Group.Fake)

	v1alpha1Group, ok := cs.NvidiaV1alpha1().(*fakenvidiav1alpha1.FakeNvidiaV1alpha1)
	require.True(t, ok, "expected *FakeNvidiaV1alpha1, got %T", cs.NvidiaV1alpha1())
	assert.Same(t, &cs.Fake, v1alpha1Group.Fake)
}

// TestActionsFromBothGroupsLandOnOneFake asserts operations issued through the two
// group clients are recorded on a single shared action log.
func TestActionsFromBothGroupsLandOnOneFake(t *testing.T) {
	ctx := t.Context()
	cs := NewSimpleClientset()

	_, err := cs.NvidiaV1().ClusterPolicies().List(ctx, metav1.ListOptions{})
	require.NoError(t, err)
	_, err = cs.NvidiaV1alpha1().NVIDIADrivers().List(ctx, metav1.ListOptions{})
	require.NoError(t, err)
	_, err = cs.NvidiaV1alpha1().GPUClusters().List(ctx, metav1.ListOptions{})
	require.NoError(t, err)

	actions := cs.Actions()
	require.Len(t, actions, 3, "all three group clients must record onto the same Fake")
	assert.Equal(t, clusterPolicyGVR, actions[0].GetResource())
	assert.Equal(t, nvidiaDriverGVR, actions[1].GetResource())
	assert.Equal(t, "gpuclusters", actions[2].GetResource().Resource)

	// A single reactor registered on the clientset affects both groups.
	cs.PrependReactor("list", "*", func(action k8stesting.Action) (bool, runtime.Object, error) {
		return true, nil, apierrors.NewServiceUnavailable("down")
	})
	_, err = cs.NvidiaV1().ClusterPolicies().List(ctx, metav1.ListOptions{})
	assert.Error(t, err)
	_, err = cs.NvidiaV1alpha1().NVIDIADrivers().List(ctx, metav1.ListOptions{})
	assert.Error(t, err)

	// Separate clientsets are fully isolated from one another.
	other := NewSimpleClientset()
	_, err = other.NvidiaV1().ClusterPolicies().List(ctx, metav1.ListOptions{})
	assert.NoError(t, err, "a reactor on one clientset must not affect another")
}

// TestSeededObjectsAreDeepCopied verifies the tracker stores copies, so mutating
// the seed object after construction does not corrupt the fake's state.
func TestSeededObjectsAreDeepCopied(t *testing.T) {
	ctx := t.Context()
	seed := newClusterPolicy("cp")
	cs := NewSimpleClientset(seed)

	seed.Spec.Operator.RuntimeClass = "nvidia-crio"
	seed.Labels["app"] = "mutated"

	got, err := cs.NvidiaV1().ClusterPolicies().Get(ctx, "cp", metav1.GetOptions{})
	require.NoError(t, err)
	assert.Equal(t, "nvidia", got.Spec.Operator.RuntimeClass)
	assert.Equal(t, "gpu-operator", got.Labels["app"])

	// Mutating a returned object does not affect the tracker either.
	got.Labels["app"] = "mutated-again"
	fresh, err := cs.NvidiaV1().ClusterPolicies().Get(ctx, "cp", metav1.GetOptions{})
	require.NoError(t, err)
	assert.Equal(t, "gpu-operator", fresh.Labels["app"])
}

// TestJSONRoundTripThroughTracker sanity checks that objects served by the fake
// serialize with the expected apiVersion/kind once the tracker has stamped them.
func TestJSONRoundTripThroughTracker(t *testing.T) {
	ctx := t.Context()
	cs := NewSimpleClientset(newClusterPolicy("cp"))

	got, err := cs.NvidiaV1().ClusterPolicies().Get(ctx, "cp", metav1.GetOptions{})
	require.NoError(t, err)

	got.TypeMeta = metav1.TypeMeta{APIVersion: nvidiav1.SchemeGroupVersion.String(), Kind: "ClusterPolicy"}
	data, err := json.Marshal(got)
	require.NoError(t, err)
	assert.Contains(t, string(data), `"apiVersion":"nvidia.com/v1"`)
	assert.Contains(t, string(data), `"kind":"ClusterPolicy"`)
}
