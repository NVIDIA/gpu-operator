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
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/watch"
	k8stesting "k8s.io/client-go/testing"

	v1 "github.com/NVIDIA/gpu-operator/api/nvidia/v1"
	clientsetscheme "github.com/NVIDIA/gpu-operator/api/versioned/scheme"
	nvidiav1 "github.com/NVIDIA/gpu-operator/api/versioned/typed/nvidia/v1"
)

// fakeClusterPolicies must implement the generated typed interface.
var _ nvidiav1.ClusterPolicyInterface = &fakeClusterPolicies{}

// eventTimeout bounds every channel read so a broken watch fails the test
// instead of hanging the suite. Everything here is in-process, so a generous
// timeout would only make a regression fail slowly.
const eventTimeout = 2 * time.Second

func clusterPolicyGVR() schema.GroupVersionResource {
	return v1.SchemeGroupVersion.WithResource("clusterpolicies")
}

func clusterPolicyGVK() schema.GroupVersionKind {
	return v1.SchemeGroupVersion.WithKind("ClusterPolicy")
}

// fixture wires a bare testing.Fake to an ObjectTracker exactly the way the
// generated top-level fake clientset does, without importing it (that would
// create an import cycle).
type fixture struct {
	fake    *k8stesting.Fake
	tracker k8stesting.ObjectTracker
	group   *FakeNvidiaV1
	client  nvidiav1.ClusterPolicyInterface
}

func newFixture(t *testing.T, objects ...runtime.Object) *fixture {
	t.Helper()

	tracker := k8stesting.NewObjectTracker(clientsetscheme.Scheme, clientsetscheme.Codecs.UniversalDecoder())
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

	group := &FakeNvidiaV1{Fake: f}
	return &fixture{
		fake:    f,
		tracker: tracker,
		group:   group,
		client:  newFakeClusterPolicies(group),
	}
}

func newClusterPolicy(name string, labels map[string]string) *v1.ClusterPolicy {
	return &v1.ClusterPolicy{
		ObjectMeta: metav1.ObjectMeta{
			Name:   name,
			Labels: labels,
		},
		Spec: v1.ClusterPolicySpec{
			Operator: v1.OperatorSpec{
				RuntimeClass: "nvidia",
			},
		},
	}
}

func policyNames(list *v1.ClusterPolicyList) []string {
	names := make([]string, 0, len(list.Items))
	for i := range list.Items {
		names = append(names, list.Items[i].Name)
	}
	return names
}

// TestNewFakeClusterPoliciesWiring asserts the GVR/GVK/namespace baked into the
// generated constructor, comparing against SchemeGroupVersion rather than
// hardcoded group strings.
func TestNewFakeClusterPoliciesWiring(t *testing.T) {
	group := &FakeNvidiaV1{Fake: &k8stesting.Fake{}}

	client := newFakeClusterPolicies(group)
	require.NotNil(t, client)

	impl, ok := client.(*fakeClusterPolicies)
	require.True(t, ok)

	assert.Equal(t, clusterPolicyGVR(), impl.Resource())
	assert.Equal(t, "nvidia.com", impl.Resource().Group)
	assert.Equal(t, "v1", impl.Resource().Version)
	assert.Equal(t, "clusterpolicies", impl.Resource().Resource)

	assert.Equal(t, clusterPolicyGVK(), impl.Kind())
	assert.Equal(t, "ClusterPolicy", impl.Kind().Kind)

	// ClusterPolicy is a cluster-scoped resource: the generated constructor
	// passes "" as the namespace.
	assert.Empty(t, impl.Namespace())
}

func TestCreateAndGet(t *testing.T) {
	f := newFixture(t)
	ctx := t.Context()

	created, err := f.client.Create(ctx, newClusterPolicy("gpu-cluster-policy", nil), metav1.CreateOptions{})
	require.NoError(t, err)
	require.NotNil(t, created)
	assert.Equal(t, "gpu-cluster-policy", created.Name)
	assert.Equal(t, "nvidia", created.Spec.Operator.RuntimeClass)

	got, err := f.client.Get(ctx, "gpu-cluster-policy", metav1.GetOptions{})
	require.NoError(t, err)
	assert.Equal(t, created, got)

	// The tracker really holds it.
	tracked, err := f.tracker.Get(clusterPolicyGVR(), "", "gpu-cluster-policy")
	require.NoError(t, err)
	assert.Equal(t, "gpu-cluster-policy", tracked.(*v1.ClusterPolicy).Name)
}

func TestCreateDuplicateReturnsAlreadyExists(t *testing.T) {
	f := newFixture(t, newClusterPolicy("dup", nil))
	ctx := t.Context()

	_, err := f.client.Create(ctx, newClusterPolicy("dup", nil), metav1.CreateOptions{})
	require.Error(t, err)
	assert.True(t, apierrors.IsAlreadyExists(err), "expected AlreadyExists, got %v", err)
}

func TestGetMissingReturnsNotFound(t *testing.T) {
	f := newFixture(t)
	ctx := t.Context()

	got, err := f.client.Get(ctx, "does-not-exist", metav1.GetOptions{})
	require.Error(t, err)
	assert.True(t, apierrors.IsNotFound(err), "expected NotFound, got %v", err)
	// gentype returns the zero object (never a typed-nil surprise) alongside the error.
	assert.Equal(t, &v1.ClusterPolicy{}, got)

	// The NotFound status carries the GroupResource wired into the fake client.
	var statusErr *apierrors.StatusError
	require.True(t, errors.As(err, &statusErr))
	require.NotNil(t, statusErr.ErrStatus.Details)
	assert.Equal(t, clusterPolicyGVR().Group, statusErr.ErrStatus.Details.Group)
	assert.Equal(t, clusterPolicyGVR().Resource, statusErr.ErrStatus.Details.Kind)
	assert.Equal(t, "does-not-exist", statusErr.ErrStatus.Details.Name)
}

func TestUpdate(t *testing.T) {
	f := newFixture(t, newClusterPolicy("cp", map[string]string{"tier": "gold"}))
	ctx := t.Context()

	current, err := f.client.Get(ctx, "cp", metav1.GetOptions{})
	require.NoError(t, err)

	current.Spec.Operator.RuntimeClass = "nvidia-crio"
	current.Labels["tier"] = "silver"

	updated, err := f.client.Update(ctx, current, metav1.UpdateOptions{})
	require.NoError(t, err)
	assert.Equal(t, "nvidia-crio", updated.Spec.Operator.RuntimeClass)
	assert.Equal(t, "silver", updated.Labels["tier"])

	// The mutation is durable in the tracker.
	got, err := f.client.Get(ctx, "cp", metav1.GetOptions{})
	require.NoError(t, err)
	assert.Equal(t, "nvidia-crio", got.Spec.Operator.RuntimeClass)
	assert.Equal(t, "silver", got.Labels["tier"])
}

// TestUpdateStatus asserts the generated-client contract for UpdateStatus: the
// call is recorded as an "update" on the "status" subresource of the ClusterPolicy
// GVR, and the status the caller sent is what a subsequent Get returns.
//
// Note: the current testing.ObjectTracker has no spec/status separation, so the
// tracked object is replaced wholesale by an UpdateStatus. That is an upstream
// implementation detail rather than a contract of this generated client, so it is
// deliberately not asserted here.
func TestUpdateStatus(t *testing.T) {
	f := newFixture(t, newClusterPolicy("cp", nil))
	ctx := t.Context()

	current, err := f.client.Get(ctx, "cp", metav1.GetOptions{})
	require.NoError(t, err)
	require.Empty(t, current.Status.State)

	current.SetStatus(v1.Ready, "gpu-operator")

	updated, err := f.client.UpdateStatus(ctx, current, metav1.UpdateOptions{})
	require.NoError(t, err)
	assert.Equal(t, v1.Ready, updated.Status.State)
	assert.Equal(t, "gpu-operator", updated.Status.Namespace)

	got, err := f.client.Get(ctx, "cp", metav1.GetOptions{})
	require.NoError(t, err)
	assert.Equal(t, v1.Ready, got.Status.State)

	// UpdateStatus is recorded as an update on the "status" subresource.
	actions := f.fake.Actions()
	last := actions[len(actions)-2] // -1 is the trailing Get
	assert.Equal(t, "update", last.GetVerb())
	assert.Equal(t, "status", last.GetSubresource())
	assert.Equal(t, clusterPolicyGVR(), last.GetResource())
}

func TestUpdateMissingReturnsNotFound(t *testing.T) {
	f := newFixture(t)
	ctx := t.Context()

	_, err := f.client.Update(ctx, newClusterPolicy("ghost", nil), metav1.UpdateOptions{})
	require.Error(t, err)
	assert.True(t, apierrors.IsNotFound(err), "expected NotFound, got %v", err)
}

func TestList(t *testing.T) {
	f := newFixture(t,
		newClusterPolicy("cp-a", map[string]string{"tier": "gold"}),
		newClusterPolicy("cp-b", map[string]string{"tier": "silver"}),
		newClusterPolicy("cp-c", nil),
	)
	ctx := t.Context()

	list, err := f.client.List(ctx, metav1.ListOptions{})
	require.NoError(t, err)
	assert.ElementsMatch(t, []string{"cp-a", "cp-b", "cp-c"}, policyNames(list))

	action := f.fake.Actions()[0]
	listAction, ok := action.(k8stesting.ListActionImpl)
	require.True(t, ok)
	assert.Equal(t, "list", listAction.GetVerb())
	assert.Equal(t, clusterPolicyGVR(), listAction.GetResource())
	assert.Equal(t, clusterPolicyGVK(), listAction.GetKind())
	assert.Empty(t, listAction.GetNamespace())
}

func TestListEmpty(t *testing.T) {
	f := newFixture(t)

	list, err := f.client.List(t.Context(), metav1.ListOptions{})
	require.NoError(t, err)
	require.NotNil(t, list)
	assert.Empty(t, list.Items)
}

// TestListCopiesListMeta exercises the generated
// `func(dst, src *v1.ClusterPolicyList) { dst.ListMeta = src.ListMeta }` hook.
// gentype only builds a brand new list (and therefore only calls copyListMeta)
// when a label selector is supplied, so the ResourceVersion seeded by the
// tracker must survive that rebuild.
func TestListCopiesListMeta(t *testing.T) {
	f := newFixture(t,
		newClusterPolicy("cp-a", map[string]string{"tier": "gold"}),
		newClusterPolicy("cp-b", map[string]string{"tier": "silver"}),
	)
	ctx := t.Context()

	// ResourceVersion as seeded by the tracker for this GVR.
	raw, err := f.tracker.List(clusterPolicyGVR(), clusterPolicyGVK(), "")
	require.NoError(t, err)
	seededRV := raw.(*v1.ClusterPolicyList).ResourceVersion
	require.NotEmpty(t, seededRV)

	// Unfiltered list: gentype hands the tracker's list straight back.
	unfiltered, err := f.client.List(ctx, metav1.ListOptions{})
	require.NoError(t, err)
	assert.Equal(t, seededRV, unfiltered.ResourceVersion)

	// Filtered list: gentype allocates a fresh list and calls copyListMeta.
	filtered, err := f.client.List(ctx, metav1.ListOptions{LabelSelector: "tier=gold"})
	require.NoError(t, err)
	assert.Equal(t, seededRV, filtered.ResourceVersion,
		"copyListMeta must carry ListMeta over to the freshly allocated list")
	assert.Equal(t, []string{"cp-a"}, policyNames(filtered))
}

func TestListLabelSelectorFiltering(t *testing.T) {
	f := newFixture(t,
		newClusterPolicy("cp-a", map[string]string{"tier": "gold", "env": "prod"}),
		newClusterPolicy("cp-b", map[string]string{"tier": "silver", "env": "prod"}),
		newClusterPolicy("cp-c", map[string]string{"tier": "gold", "env": "dev"}),
		newClusterPolicy("cp-none", nil),
	)
	ctx := t.Context()

	for _, tc := range []struct {
		name     string
		selector string
		want     []string
	}{
		{"no selector matches everything", "", []string{"cp-a", "cp-b", "cp-c", "cp-none"}},
		{"single equality", "tier=gold", []string{"cp-a", "cp-c"}},
		{"conjunction", "tier=gold,env=prod", []string{"cp-a"}},
		{"inequality", "tier!=gold", []string{"cp-b", "cp-none"}},
		{"key existence", "tier", []string{"cp-a", "cp-b", "cp-c"}},
		{"set membership", "env in (dev)", []string{"cp-c"}},
		{"no match", "tier=bronze", nil},
	} {
		t.Run(tc.name, func(t *testing.T) {
			list, err := f.client.List(ctx, metav1.ListOptions{LabelSelector: tc.selector})
			require.NoError(t, err)
			assert.ElementsMatch(t, tc.want, policyNames(list))

			// The selector is recorded on the action for assertions in user tests.
			actions := f.fake.Actions()
			listAction := actions[len(actions)-1].(k8stesting.ListActionImpl)
			assert.Equal(t, tc.selector, listAction.GetListOptions().LabelSelector)
		})
	}
}

func TestDelete(t *testing.T) {
	f := newFixture(t, newClusterPolicy("cp", nil))
	ctx := t.Context()

	require.NoError(t, f.client.Delete(ctx, "cp", metav1.DeleteOptions{}))

	_, err := f.client.Get(ctx, "cp", metav1.GetOptions{})
	require.Error(t, err)
	assert.True(t, apierrors.IsNotFound(err), "expected NotFound after delete, got %v", err)

	deleteAction, ok := f.fake.Actions()[0].(k8stesting.DeleteActionImpl)
	require.True(t, ok)
	assert.Equal(t, "delete", deleteAction.GetVerb())
	assert.Equal(t, "cp", deleteAction.GetName())
	assert.Empty(t, deleteAction.GetNamespace())
}

func TestDeleteMissingReturnsNotFound(t *testing.T) {
	f := newFixture(t)

	err := f.client.Delete(t.Context(), "ghost", metav1.DeleteOptions{})
	require.Error(t, err)
	assert.True(t, apierrors.IsNotFound(err), "expected NotFound, got %v", err)
}

// TestDeleteCollectionRecordsAction asserts the generated-client contract for
// DeleteCollection: the call is recorded as a cluster-scoped "delete-collection"
// action against the ClusterPolicy GVR, and it carries both the DeleteOptions
// and the ListOptions the caller supplied so user reactors can act on them.
//
// Note: today's testing.ObjectReaction has no DeleteCollectionActionImpl case,
// so no reactor handles the action and the tracker is left untouched. That is an
// upstream implementation detail, not a contract of this generated client, so it
// is deliberately not asserted here — see TestDeleteCollectionRemovesAll for the
// behavior once a delete-collection reactor is installed.
func TestDeleteCollectionRecordsAction(t *testing.T) {
	f := newFixture(t, newClusterPolicy("cp-a", nil), newClusterPolicy("cp-b", nil))

	gracePeriod := int64(30)
	propagation := metav1.DeletePropagationForeground
	deleteOpts := metav1.DeleteOptions{
		GracePeriodSeconds: &gracePeriod,
		PropagationPolicy:  &propagation,
	}
	listOpts := metav1.ListOptions{LabelSelector: "tier=gold"}

	require.NoError(t, f.client.DeleteCollection(t.Context(), deleteOpts, listOpts))

	require.Len(t, f.fake.Actions(), 1)
	action, ok := f.fake.Actions()[0].(k8stesting.DeleteCollectionActionImpl)
	require.True(t, ok, "expected a DeleteCollectionActionImpl, got %T", f.fake.Actions()[0])

	assert.Equal(t, "delete-collection", action.GetVerb())
	assert.Equal(t, clusterPolicyGVR(), action.GetResource())
	assert.Empty(t, action.GetSubresource())
	// ClusterPolicy is cluster scoped, so the action carries no namespace.
	assert.Empty(t, action.GetNamespace())
	assert.True(t, action.Matches("delete-collection", "clusterpolicies"))

	assert.Equal(t, deleteOpts, action.GetDeleteOptions())
	assert.Equal(t, listOpts, action.GetListOptions())
	assert.Equal(t, "tier=gold", action.GetListRestrictions().Labels.String())
}

// TestDeleteCollectionRemovesAll wires the delete-collection verb to the tracker
// (as callers must do themselves) and proves the typed method drives it.
func TestDeleteCollectionRemovesAll(t *testing.T) {
	f := newFixture(t,
		newClusterPolicy("cp-a", nil),
		newClusterPolicy("cp-b", nil),
		newClusterPolicy("cp-c", nil),
	)
	ctx := t.Context()

	f.fake.PrependReactor("delete-collection", "clusterpolicies",
		func(action k8stesting.Action) (bool, runtime.Object, error) {
			listObj, err := f.tracker.List(clusterPolicyGVR(), clusterPolicyGVK(), action.GetNamespace())
			if err != nil {
				return true, nil, err
			}
			for i := range listObj.(*v1.ClusterPolicyList).Items {
				name := listObj.(*v1.ClusterPolicyList).Items[i].Name
				if err := f.tracker.Delete(clusterPolicyGVR(), action.GetNamespace(), name); err != nil {
					return true, nil, err
				}
			}
			return true, nil, nil
		})

	require.NoError(t, f.client.DeleteCollection(ctx, metav1.DeleteOptions{}, metav1.ListOptions{}))

	list, err := f.client.List(ctx, metav1.ListOptions{})
	require.NoError(t, err)
	assert.Empty(t, list.Items)
}

func TestPatch(t *testing.T) {
	f := newFixture(t, newClusterPolicy("cp", map[string]string{"tier": "gold"}))
	ctx := t.Context()

	patch := []byte(`{"metadata":{"labels":{"tier":"platinum","patched":"yes"}},"status":{"state":"ready"}}`)

	patched, err := f.client.Patch(ctx, "cp", types.MergePatchType, patch, metav1.PatchOptions{})
	require.NoError(t, err)
	assert.Equal(t, "platinum", patched.Labels["tier"])
	assert.Equal(t, "yes", patched.Labels["patched"])
	assert.Equal(t, v1.Ready, patched.Status.State)

	got, err := f.client.Get(ctx, "cp", metav1.GetOptions{})
	require.NoError(t, err)
	assert.Equal(t, "platinum", got.Labels["tier"])
	assert.Equal(t, v1.Ready, got.Status.State)

	patchAction, ok := f.fake.Actions()[0].(k8stesting.PatchActionImpl)
	require.True(t, ok)
	assert.Equal(t, "patch", patchAction.GetVerb())
	assert.Equal(t, types.MergePatchType, patchAction.GetPatchType())
	assert.Equal(t, patch, patchAction.GetPatch())
	assert.Empty(t, patchAction.GetSubresource())
	assert.Empty(t, patchAction.GetNamespace())
}

func TestPatchSubresource(t *testing.T) {
	f := newFixture(t, newClusterPolicy("cp", nil))
	ctx := t.Context()

	_, err := f.client.Patch(ctx, "cp", types.MergePatchType,
		[]byte(`{"status":{"state":"notReady"}}`), metav1.PatchOptions{}, "status")
	require.NoError(t, err)

	assert.Equal(t, "status", f.fake.Actions()[0].GetSubresource())
}

func TestPatchMissingReturnsNotFound(t *testing.T) {
	f := newFixture(t)

	_, err := f.client.Patch(t.Context(), "ghost", types.MergePatchType,
		[]byte(`{}`), metav1.PatchOptions{})
	require.Error(t, err)
	assert.True(t, apierrors.IsNotFound(err), "expected NotFound, got %v", err)
}

// receiveEvent reads a single watch event with a hard timeout so the test can
// never block indefinitely. Every read that expects an event goes through it.
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

func requireEvent(t *testing.T, ch <-chan watch.Event, want watch.EventType, name string) *v1.ClusterPolicy {
	t.Helper()
	ev := receiveEvent(t, ch)
	require.Equal(t, want, ev.Type)
	cp, ok := ev.Object.(*v1.ClusterPolicy)
	require.True(t, ok, "expected *v1.ClusterPolicy, got %T", ev.Object)
	require.Equal(t, name, cp.Name)
	return cp
}

func TestWatchDeliversEvents(t *testing.T) {
	f := newFixture(t)
	ctx := t.Context()

	w, err := f.client.Watch(ctx, metav1.ListOptions{})
	require.NoError(t, err)
	require.NotNil(t, w)
	defer w.Stop()

	created, err := f.client.Create(ctx, newClusterPolicy("cp", nil), metav1.CreateOptions{})
	require.NoError(t, err)
	requireEvent(t, w.ResultChan(), watch.Added, "cp")

	created.SetStatus(v1.Ready, "gpu-operator")
	_, err = f.client.Update(ctx, created, metav1.UpdateOptions{})
	require.NoError(t, err)
	modified := requireEvent(t, w.ResultChan(), watch.Modified, "cp")
	assert.Equal(t, v1.Ready, modified.Status.State)

	require.NoError(t, f.client.Delete(ctx, "cp", metav1.DeleteOptions{}))
	requireEvent(t, w.ResultChan(), watch.Deleted, "cp")

	// The watch action itself is recorded, cluster-scoped, with Watch set.
	watchAction, ok := f.fake.Actions()[0].(k8stesting.WatchActionImpl)
	require.True(t, ok)
	assert.Equal(t, "watch", watchAction.GetVerb())
	assert.Equal(t, clusterPolicyGVR(), watchAction.GetResource())
	assert.Empty(t, watchAction.GetNamespace())
	assert.True(t, watchAction.ListOptions.Watch)
}

func TestWatchStopClosesChannel(t *testing.T) {
	f := newFixture(t)

	w, err := f.client.Watch(t.Context(), metav1.ListOptions{})
	require.NoError(t, err)

	w.Stop()

	// receiveEvent cannot be used here: it asserts an event arrives, whereas this
	// test asserts the exact opposite (the channel is closed). The read is still
	// bounded by eventTimeout.
	timer := time.NewTimer(eventTimeout)
	defer timer.Stop()
	select {
	case _, ok := <-w.ResultChan():
		assert.False(t, ok, "channel must be closed after Stop()")
	case <-timer.C:
		t.Fatal("timed out waiting for the watch channel to close")
	}
}

func TestWatchWithoutReactorReturnsError(t *testing.T) {
	// Only the observable contract is asserted: a Fake with no watch reactor
	// yields an error and no watcher. The wording client-go uses for it can
	// change on a dependency bump without the behaviour changing.
	client := newFakeClusterPolicies(&FakeNvidiaV1{Fake: &k8stesting.Fake{}})

	w, err := client.Watch(t.Context(), metav1.ListOptions{})
	require.Error(t, err)
	assert.Nil(t, w)
}

// TestRecordedActions walks the whole interface and asserts each recorded
// action carries the expected verb, GVR and cluster scope.
func TestRecordedActions(t *testing.T) {
	f := newFixture(t)
	ctx := t.Context()

	_, err := f.client.Create(ctx, newClusterPolicy("cp", nil), metav1.CreateOptions{})
	require.NoError(t, err)
	_, err = f.client.Get(ctx, "cp", metav1.GetOptions{})
	require.NoError(t, err)
	_, err = f.client.List(ctx, metav1.ListOptions{})
	require.NoError(t, err)
	cp, err := f.client.Get(ctx, "cp", metav1.GetOptions{})
	require.NoError(t, err)
	_, err = f.client.Update(ctx, cp, metav1.UpdateOptions{})
	require.NoError(t, err)
	_, err = f.client.UpdateStatus(ctx, cp, metav1.UpdateOptions{})
	require.NoError(t, err)
	_, err = f.client.Patch(ctx, "cp", types.MergePatchType, []byte(`{}`), metav1.PatchOptions{})
	require.NoError(t, err)
	w, err := f.client.Watch(ctx, metav1.ListOptions{})
	require.NoError(t, err)
	w.Stop()
	require.NoError(t, f.client.Delete(ctx, "cp", metav1.DeleteOptions{}))
	require.NoError(t, f.client.DeleteCollection(ctx, metav1.DeleteOptions{}, metav1.ListOptions{}))

	want := []struct {
		verb        string
		subresource string
	}{
		{"create", ""},
		{"get", ""},
		{"list", ""},
		{"get", ""},
		{"update", ""},
		{"update", "status"},
		{"patch", ""},
		{"watch", ""},
		{"delete", ""},
		{"delete-collection", ""},
	}

	actions := f.fake.Actions()
	require.Len(t, actions, len(want))
	for i, exp := range want {
		action := actions[i]
		assert.Equalf(t, exp.verb, action.GetVerb(), "action %d verb", i)
		assert.Equalf(t, exp.subresource, action.GetSubresource(), "action %d subresource", i)
		assert.Equalf(t, clusterPolicyGVR(), action.GetResource(), "action %d resource", i)
		// ClusterPolicy is cluster scoped, so every action is namespace-less.
		assert.Emptyf(t, action.GetNamespace(), "action %d namespace", i)
		assert.Truef(t, action.Matches(exp.verb, "clusterpolicies"), "action %d Matches()", i)
	}

	f.fake.ClearActions()
	assert.Empty(t, f.fake.Actions())
}

// TestPrependReactorShortCircuits proves the reactor chain is honored: a canned
// error prepended for a verb surfaces through the corresponding typed method
// while the other verbs still hit the tracker.
func TestPrependReactorShortCircuits(t *testing.T) {
	sentinel := errors.New("boom")

	for _, tc := range []struct {
		name string
		verb string
		call func(ctx context.Context, c nvidiav1.ClusterPolicyInterface) error
	}{
		{"create", "create", func(ctx context.Context, c nvidiav1.ClusterPolicyInterface) error {
			_, err := c.Create(ctx, newClusterPolicy("cp", nil), metav1.CreateOptions{})
			return err
		}},
		{"get", "get", func(ctx context.Context, c nvidiav1.ClusterPolicyInterface) error {
			_, err := c.Get(ctx, "cp", metav1.GetOptions{})
			return err
		}},
		{"list", "list", func(ctx context.Context, c nvidiav1.ClusterPolicyInterface) error {
			_, err := c.List(ctx, metav1.ListOptions{})
			return err
		}},
		{"update", "update", func(ctx context.Context, c nvidiav1.ClusterPolicyInterface) error {
			_, err := c.Update(ctx, newClusterPolicy("cp", nil), metav1.UpdateOptions{})
			return err
		}},
		{"update status", "update", func(ctx context.Context, c nvidiav1.ClusterPolicyInterface) error {
			_, err := c.UpdateStatus(ctx, newClusterPolicy("cp", nil), metav1.UpdateOptions{})
			return err
		}},
		{"patch", "patch", func(ctx context.Context, c nvidiav1.ClusterPolicyInterface) error {
			_, err := c.Patch(ctx, "cp", types.MergePatchType, []byte(`{}`), metav1.PatchOptions{})
			return err
		}},
		{"delete", "delete", func(ctx context.Context, c nvidiav1.ClusterPolicyInterface) error {
			return c.Delete(ctx, "cp", metav1.DeleteOptions{})
		}},
		{"delete collection", "delete-collection", func(ctx context.Context, c nvidiav1.ClusterPolicyInterface) error {
			return c.DeleteCollection(ctx, metav1.DeleteOptions{}, metav1.ListOptions{})
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := newFixture(t, newClusterPolicy("cp", nil))

			var reacted int
			f.fake.PrependReactor(tc.verb, "clusterpolicies",
				func(action k8stesting.Action) (bool, runtime.Object, error) {
					reacted++
					return true, nil, sentinel
				})

			err := tc.call(t.Context(), f.client)
			require.Error(t, err)
			assert.ErrorIs(t, err, sentinel)
			assert.Equal(t, 1, reacted, "the prepended reactor must run exactly once")

			// The object was never touched: the reactor short-circuited the chain.
			tracked, getErr := f.tracker.Get(clusterPolicyGVR(), "", "cp")
			require.NoError(t, getErr)
			assert.Equal(t, "cp", tracked.(*v1.ClusterPolicy).Name)
		})
	}
}

// TestPrependReactorCanSubstituteObjects shows the reactor chain can also return
// synthetic objects, not just errors.
func TestPrependReactorCanSubstituteObjects(t *testing.T) {
	f := newFixture(t)

	canned := newClusterPolicy("canned", nil)
	canned.SetStatus(v1.NotReady, "elsewhere")

	f.fake.PrependReactor("get", "clusterpolicies",
		func(action k8stesting.Action) (bool, runtime.Object, error) {
			return true, canned, nil
		})

	got, err := f.client.Get(t.Context(), "anything", metav1.GetOptions{})
	require.NoError(t, err)
	assert.Equal(t, "canned", got.Name)
	assert.Equal(t, v1.NotReady, got.Status.State)

	// The requested name is still recorded even though the reactor ignored it.
	getAction, ok := f.fake.Actions()[0].(k8stesting.GetActionImpl)
	require.True(t, ok)
	assert.Equal(t, "anything", getAction.GetName())
}

// TestReactorScopedToOtherResourceIsIgnored guards against the reactor matching
// on the wrong resource name.
func TestReactorScopedToOtherResourceIsIgnored(t *testing.T) {
	f := newFixture(t, newClusterPolicy("cp", nil))

	f.fake.PrependReactor("get", "nvidiadrivers",
		func(action k8stesting.Action) (bool, runtime.Object, error) {
			return true, nil, errors.New("should not fire")
		})

	got, err := f.client.Get(t.Context(), "cp", metav1.GetOptions{})
	require.NoError(t, err)
	assert.Equal(t, "cp", got.Name)
}

// TestActionsAreSharedAcrossClientInstances confirms every ClusterPolicies()
// value writes into the one testing.Fake owned by the group client.
func TestActionsAreSharedAcrossClientInstances(t *testing.T) {
	f := newFixture(t)
	ctx := t.Context()

	first := f.group.ClusterPolicies()
	second := f.group.ClusterPolicies()

	_, err := first.Create(ctx, newClusterPolicy("cp", nil), metav1.CreateOptions{})
	require.NoError(t, err)

	// A different client instance sees the object created through the first one.
	got, err := second.Get(ctx, "cp", metav1.GetOptions{})
	require.NoError(t, err)
	assert.Equal(t, "cp", got.Name)

	assert.Len(t, f.fake.Actions(), 2)
}
