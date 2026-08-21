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

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/watch"
	k8stesting "k8s.io/client-go/testing"

	v1alpha1 "github.com/NVIDIA/gpu-operator/api/nvidia/v1alpha1"
	nvidiav1alpha1 "github.com/NVIDIA/gpu-operator/api/versioned/typed/nvidia/v1alpha1"
)

// gpuClusterFixture is the shared group fixture (see fake_nvidia_client_test.go)
// narrowed to the GPUCluster client.
type gpuClusterFixture struct {
	*fakeGroupFixture
	client nvidiav1alpha1.GPUClusterInterface
}

func newGPUClusterFixture(t *testing.T, objects ...runtime.Object) *gpuClusterFixture {
	t.Helper()
	base := newFakeGroupFixture(t, objects...)
	return &gpuClusterFixture{fakeGroupFixture: base, client: base.group.GPUClusters()}
}

// newGPUCluster builds a GPUCluster. Note that the CRD declares GPUCluster a
// singleton via a CEL rule pinning metadata.name to "gpu-cluster"; the fake
// performs no CEL validation, so list/selector tests below legitimately hold
// several objects at once.
func newGPUCluster(name string, labels map[string]string) *v1alpha1.GPUCluster {
	return &v1alpha1.GPUCluster{
		ObjectMeta: metav1.ObjectMeta{Name: name, Labels: labels},
		Spec: v1alpha1.GPUClusterSpec{
			DRADriver: v1alpha1.DRADriverSpec{
				Repository: "nvcr.io/nvidia/cloud-native",
				Image:      "k8s-dra-driver-gpu",
				Version:    "v25.3.0",
			},
		},
	}
}

func TestGPUClusters_CreateAndGet(t *testing.T) {
	f := newGPUClusterFixture(t)
	ctx := t.Context()

	created, err := f.client.Create(ctx, newGPUCluster("gpu-cluster", nil), metav1.CreateOptions{})
	require.NoError(t, err)
	require.NotNil(t, created)
	assert.Equal(t, "gpu-cluster", created.Name)
	assert.Equal(t, "v25.3.0", created.Spec.DRADriver.Version)

	got, err := f.client.Get(ctx, "gpu-cluster", metav1.GetOptions{})
	require.NoError(t, err)
	assert.Equal(t, created, got)

	// The tracker hands back copies: mutating the result must not leak back in.
	got.Spec.DRADriver.Version = "mutated"
	fresh, err := f.client.Get(ctx, "gpu-cluster", metav1.GetOptions{})
	require.NoError(t, err)
	assert.Equal(t, "v25.3.0", fresh.Spec.DRADriver.Version)

	createAction := f.fake.Actions()[0]
	assert.Equal(t, "create", createAction.GetVerb())
	assert.Equal(t, gpuClusterGVR, createAction.GetResource())
}

func TestGPUClusters_CreateDuplicateIsAlreadyExists(t *testing.T) {
	f := newGPUClusterFixture(t, newGPUCluster("gpu-cluster", nil))

	_, err := f.client.Create(t.Context(), newGPUCluster("gpu-cluster", nil), metav1.CreateOptions{})
	require.Error(t, err)
	assert.True(t, apierrors.IsAlreadyExists(err), "expected AlreadyExists, got %v", err)
}

func TestGPUClusters_GetMissingIsNotFound(t *testing.T) {
	f := newGPUClusterFixture(t)

	got, err := f.client.Get(t.Context(), "nope", metav1.GetOptions{})
	require.Error(t, err)
	assert.True(t, apierrors.IsNotFound(err), "expected NotFound, got %v", err)
	// On error the generated client still returns a non-nil zero value.
	require.NotNil(t, got)
	assert.Empty(t, got.Name)

	statusErr := &apierrors.StatusError{}
	require.True(t, errors.As(err, &statusErr))
	assert.Equal(t, gpuClusterGVR.Group, statusErr.ErrStatus.Details.Group)
	assert.Equal(t, gpuClusterGVR.Resource, statusErr.ErrStatus.Details.Kind)
	assert.Equal(t, "nope", statusErr.ErrStatus.Details.Name)
}

// TestGPUClusters_MissingObjectIsNotFoundTable covers the NotFound path of every
// name-addressed verb in one place.
func TestGPUClusters_MissingObjectIsNotFoundTable(t *testing.T) {
	for _, tc := range []struct {
		name string
		call func(context.Context, nvidiav1alpha1.GPUClusterInterface) error
	}{
		{"get", func(ctx context.Context, c nvidiav1alpha1.GPUClusterInterface) error {
			_, err := c.Get(ctx, "ghost", metav1.GetOptions{})
			return err
		}},
		{"update", func(ctx context.Context, c nvidiav1alpha1.GPUClusterInterface) error {
			_, err := c.Update(ctx, newGPUCluster("ghost", nil), metav1.UpdateOptions{})
			return err
		}},
		{"update status", func(ctx context.Context, c nvidiav1alpha1.GPUClusterInterface) error {
			_, err := c.UpdateStatus(ctx, newGPUCluster("ghost", nil), metav1.UpdateOptions{})
			return err
		}},
		{"delete", func(ctx context.Context, c nvidiav1alpha1.GPUClusterInterface) error {
			return c.Delete(ctx, "ghost", metav1.DeleteOptions{})
		}},
		{"patch", func(ctx context.Context, c nvidiav1alpha1.GPUClusterInterface) error {
			_, err := c.Patch(ctx, "ghost", types.MergePatchType, []byte(`{}`), metav1.PatchOptions{})
			return err
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := newGPUClusterFixture(t)

			err := tc.call(t.Context(), f.client)
			require.Error(t, err)
			assert.True(t, apierrors.IsNotFound(err), "expected NotFound, got %v", err)
		})
	}
}

func TestGPUClusters_Update(t *testing.T) {
	f := newGPUClusterFixture(t, newGPUCluster("gpu-cluster", nil))
	ctx := t.Context()

	current, err := f.client.Get(ctx, "gpu-cluster", metav1.GetOptions{})
	require.NoError(t, err)

	f.fake.ClearActions()
	current.Spec.DRADriver.Version = "v25.4.0"
	updated, err := f.client.Update(ctx, current, metav1.UpdateOptions{})
	require.NoError(t, err)
	assert.Equal(t, "v25.4.0", updated.Spec.DRADriver.Version)

	// The change is visible through the tracker on the next read.
	reread, err := f.client.Get(ctx, "gpu-cluster", metav1.GetOptions{})
	require.NoError(t, err)
	assert.Equal(t, "v25.4.0", reread.Spec.DRADriver.Version)

	updateAction := f.fake.Actions()[0]
	assert.Equal(t, "update", updateAction.GetVerb())
	assert.Equal(t, gpuClusterGVR, updateAction.GetResource())
	assert.Empty(t, updateAction.GetSubresource(), "Update must not target a subresource")
}

func TestGPUClusters_UpdateStatus(t *testing.T) {
	f := newGPUClusterFixture(t, newGPUCluster("gpu-cluster", nil))
	ctx := t.Context()

	current, err := f.client.Get(ctx, "gpu-cluster", metav1.GetOptions{})
	require.NoError(t, err)
	current.Status.State = v1alpha1.Ready
	current.Status.Namespace = "gpu-operator"

	f.fake.ClearActions()
	updated, err := f.client.UpdateStatus(ctx, current, metav1.UpdateOptions{})
	require.NoError(t, err)
	assert.Equal(t, v1alpha1.Ready, updated.Status.State)
	assert.Equal(t, "gpu-operator", updated.Status.Namespace)

	reread, err := f.client.Get(ctx, "gpu-cluster", metav1.GetOptions{})
	require.NoError(t, err)
	assert.Equal(t, v1alpha1.Ready, reread.Status.State)

	// UpdateStatus is recorded as an update against the "status" subresource.
	action := f.fake.Actions()[0]
	assert.Equal(t, "update", action.GetVerb())
	assert.Equal(t, "status", action.GetSubresource())
	assert.Equal(t, gpuClusterGVR, action.GetResource())
}

// TestGPUClusters_UpdateStatusWithSpecChangeStillTargetsStatusSubresource is
// the GPUCluster half of the assertion documented on the NVIDIADriver test of
// the same name: regardless of what the caller mutated, UpdateStatus is
// recorded as an "update" against the "status" subresource of the right
// cluster-scoped GVR. How the reaction chain applies it (today: as a
// full-object replace) is an upstream detail and is not asserted.
func TestGPUClusters_UpdateStatusWithSpecChangeStillTargetsStatusSubresource(t *testing.T) {
	f := newGPUClusterFixture(t, newGPUCluster("gpu-cluster", nil))
	ctx := t.Context()

	current, err := f.client.Get(ctx, "gpu-cluster", metav1.GetOptions{})
	require.NoError(t, err)
	current.Status.State = v1alpha1.NotReady
	current.Spec.DRADriver.Version = "spec-change-sent-alongside-the-status-update"

	f.fake.ClearActions()
	_, err = f.client.UpdateStatus(ctx, current, metav1.UpdateOptions{})
	require.NoError(t, err)

	action := lastAction(t, f.fake)
	assert.Equal(t, "update", action.GetVerb())
	assert.Equal(t, "status", action.GetSubresource())
	assert.Equal(t, gpuClusterGVR, action.GetResource())
	assert.Empty(t, action.GetNamespace(), "GPUCluster is cluster scoped")
}

func TestGPUClusters_List(t *testing.T) {
	f := newGPUClusterFixture(t,
		newGPUCluster("cluster-a", map[string]string{"tier": "prod"}),
		newGPUCluster("cluster-b", map[string]string{"tier": "dev"}),
		newGPUCluster("cluster-c", map[string]string{"tier": "prod"}),
	)

	all, err := f.client.List(t.Context(), metav1.ListOptions{})
	require.NoError(t, err)
	require.Len(t, all.Items, 3)
	assert.ElementsMatch(t,
		[]string{"cluster-a", "cluster-b", "cluster-c"},
		[]string{all.Items[0].Name, all.Items[1].Name, all.Items[2].Name},
	)

	// The recorded list action carries both the GVR and the GVK wired into
	// newFakeGPUClusters.
	listAction, ok := lastAction(t, f.fake).(k8stesting.ListActionImpl)
	require.True(t, ok)
	assert.Equal(t, "list", listAction.GetVerb())
	assert.Equal(t, gpuClusterGVR, listAction.GetResource())
	assert.Equal(t, gpuClusterGVK, listAction.Kind)
}

func TestGPUClusters_ListEmpty(t *testing.T) {
	f := newGPUClusterFixture(t)

	list, err := f.client.List(t.Context(), metav1.ListOptions{})
	require.NoError(t, err)
	require.NotNil(t, list)
	assert.Empty(t, list.Items)
}

// TestGPUClusters_ListLabelSelectorPreservesListMeta exercises the generated
// copyListMeta hook:
//
//	func(dst, src *v1alpha1.GPUClusterList) { dst.ListMeta = src.ListMeta }
//
// It is only invoked on the label-selector path, where gentype builds a fresh
// list and must carry the ListMeta (notably ResourceVersion) across.
func TestGPUClusters_ListLabelSelectorPreservesListMeta(t *testing.T) {
	f := newGPUClusterFixture(t,
		newGPUCluster("cluster-a", map[string]string{"tier": "prod"}),
		newGPUCluster("cluster-b", map[string]string{"tier": "dev"}),
		newGPUCluster("cluster-c", map[string]string{"tier": "prod"}),
	)
	ctx := t.Context()

	// The tracker stamps the collection ResourceVersion onto every list it returns.
	unfiltered, err := f.client.List(ctx, metav1.ListOptions{})
	require.NoError(t, err)
	seededRV := unfiltered.ResourceVersion
	require.NotEmpty(t, seededRV)
	require.NotEqual(t, "0", seededRV)

	filtered, err := f.client.List(ctx, metav1.ListOptions{LabelSelector: "tier=prod"})
	require.NoError(t, err)
	require.Len(t, filtered.Items, 2)
	assert.ElementsMatch(t,
		[]string{"cluster-a", "cluster-c"},
		[]string{filtered.Items[0].Name, filtered.Items[1].Name},
	)
	assert.Equal(t, seededRV, filtered.ResourceVersion,
		"copyListMeta must carry ListMeta onto the label-filtered list")
}

func TestGPUClusters_ListLabelSelectorTable(t *testing.T) {
	objects := []runtime.Object{
		newGPUCluster("cluster-a", map[string]string{"tier": "prod", "arch": "amd64"}),
		newGPUCluster("cluster-b", map[string]string{"tier": "dev", "arch": "amd64"}),
		newGPUCluster("cluster-c", map[string]string{"tier": "prod", "arch": "arm64"}),
		newGPUCluster("cluster-unlabeled", nil),
	}

	for _, tc := range []struct {
		name     string
		selector string
		want     []string
	}{
		{"empty selector matches everything", "", []string{"cluster-a", "cluster-b", "cluster-c", "cluster-unlabeled"}},
		{"single label", "tier=prod", []string{"cluster-a", "cluster-c"}},
		{"conjunction", "tier=prod,arch=arm64", []string{"cluster-c"}},
		{"set based", "tier in (dev,prod)", []string{"cluster-a", "cluster-b", "cluster-c"}},
		{"negation", "tier!=prod", []string{"cluster-b", "cluster-unlabeled"}},
		{"no match", "tier=staging", nil},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := newGPUClusterFixture(t, objects...)

			list, err := f.client.List(t.Context(), metav1.ListOptions{LabelSelector: tc.selector})
			require.NoError(t, err)

			got := make([]string, 0, len(list.Items))
			for i := range list.Items {
				got = append(got, list.Items[i].Name)
			}
			assert.ElementsMatch(t, tc.want, got)
		})
	}
}

func TestGPUClusters_Delete(t *testing.T) {
	f := newGPUClusterFixture(t, newGPUCluster("cluster-a", nil), newGPUCluster("cluster-b", nil))
	ctx := t.Context()

	require.NoError(t, f.client.Delete(ctx, "cluster-a", metav1.DeleteOptions{}))

	_, err := f.client.Get(ctx, "cluster-a", metav1.GetOptions{})
	require.Error(t, err)
	assert.True(t, apierrors.IsNotFound(err), "expected NotFound after delete, got %v", err)

	// Unrelated objects are untouched.
	_, err = f.client.Get(ctx, "cluster-b", metav1.GetOptions{})
	require.NoError(t, err)
}

// TestGPUClusters_DeleteCollectionRecordsTheAction asserts the generated client
// contract: DeleteCollection is recorded as a "delete-collection" action on the
// right cluster-scoped GVR, carrying through both the DeleteOptions and the
// ListOptions the caller supplied.
//
// As documented on the NVIDIADriver test of the same name, whether anything is
// removed is up to the reaction chain: testing.ObjectReaction has no case for
// DeleteCollectionActionImpl, so the default reactor leaves the tracker alone.
// TestGPUClusters_DeleteCollectionWithReactorRemovesAll covers the supported way
// to get collection semantics.
func TestGPUClusters_DeleteCollectionRecordsTheAction(t *testing.T) {
	f := newGPUClusterFixture(t, newGPUCluster("cluster-a", nil), newGPUCluster("cluster-b", nil))

	gracePeriod := int64(30)
	deleteOpts := metav1.DeleteOptions{GracePeriodSeconds: &gracePeriod}
	listOpts := metav1.ListOptions{LabelSelector: "tier=prod"}

	require.NoError(t, f.client.DeleteCollection(t.Context(), deleteOpts, listOpts))

	action, ok := lastAction(t, f.fake).(k8stesting.DeleteCollectionActionImpl)
	require.True(t, ok)
	assert.Equal(t, "delete-collection", action.GetVerb())
	assert.Equal(t, gpuClusterGVR, action.GetResource())
	// GPUCluster is cluster scoped, so the action carries no namespace.
	assert.Empty(t, action.GetNamespace())
	assert.Equal(t, deleteOpts, action.GetDeleteOptions())
	assert.Equal(t, listOpts, action.GetListOptions())
	assert.Equal(t, "tier=prod", action.GetListRestrictions().Labels.String())
}

// TestGPUClusters_DeleteCollectionWithReactorRemovesAll shows the supported way
// to get collection semantics: a reactor that fans the request out to the
// tracker. This also proves DeleteCollection flows through the reaction chain.
func TestGPUClusters_DeleteCollectionWithReactorRemovesAll(t *testing.T) {
	f := newGPUClusterFixture(t, newGPUCluster("cluster-a", nil), newGPUCluster("cluster-b", nil))
	ctx := t.Context()

	f.fake.PrependReactor("delete-collection", "gpuclusters", func(action k8stesting.Action) (bool, runtime.Object, error) {
		obj, err := f.tracker.List(gpuClusterGVR, gpuClusterGVK, action.GetNamespace())
		if err != nil {
			return true, nil, err
		}
		list, ok := obj.(*v1alpha1.GPUClusterList)
		if !ok {
			return true, nil, errors.New("unexpected list type")
		}
		for i := range list.Items {
			if err := f.tracker.Delete(gpuClusterGVR, action.GetNamespace(), list.Items[i].Name); err != nil {
				return true, nil, err
			}
		}
		return true, nil, nil
	})

	require.NoError(t, f.client.DeleteCollection(ctx, metav1.DeleteOptions{}, metav1.ListOptions{}))

	remaining, err := f.client.List(ctx, metav1.ListOptions{})
	require.NoError(t, err)
	assert.Empty(t, remaining.Items)
}

func TestGPUClusters_Patch(t *testing.T) {
	f := newGPUClusterFixture(t, newGPUCluster("gpu-cluster", map[string]string{"tier": "dev"}))
	ctx := t.Context()

	patch := []byte(`{"metadata":{"labels":{"tier":"prod"}},"spec":{"draDriver":{"version":"v25.5.0"}}}`)
	patched, err := f.client.Patch(ctx, "gpu-cluster", types.MergePatchType, patch, metav1.PatchOptions{})
	require.NoError(t, err)
	assert.Equal(t, "v25.5.0", patched.Spec.DRADriver.Version)
	assert.Equal(t, "prod", patched.Labels["tier"])
	// A merge patch of a nested object leaves sibling fields intact.
	assert.Equal(t, "k8s-dra-driver-gpu", patched.Spec.DRADriver.Image)

	reread, err := f.client.Get(ctx, "gpu-cluster", metav1.GetOptions{})
	require.NoError(t, err)
	assert.Equal(t, "v25.5.0", reread.Spec.DRADriver.Version)
	assert.Equal(t, "prod", reread.Labels["tier"])

	action, ok := f.fake.Actions()[0].(k8stesting.PatchAction)
	require.True(t, ok)
	assert.Equal(t, "patch", action.GetVerb())
	assert.Equal(t, types.MergePatchType, action.GetPatchType())
	assert.Equal(t, "gpu-cluster", action.GetName())
	assert.Equal(t, gpuClusterGVR, action.GetResource())
}

func TestGPUClusters_PatchSubresource(t *testing.T) {
	f := newGPUClusterFixture(t, newGPUCluster("gpu-cluster", nil))

	patch := []byte(`{"status":{"state":"ready"}}`)
	patched, err := f.client.Patch(t.Context(), "gpu-cluster", types.MergePatchType, patch, metav1.PatchOptions{}, "status")
	require.NoError(t, err)
	assert.Equal(t, v1alpha1.Ready, patched.Status.State)

	assert.Equal(t, "status", f.fake.Actions()[0].GetSubresource())
}

func TestGPUClusters_Watch(t *testing.T) {
	f := newGPUClusterFixture(t)
	ctx := t.Context()

	w, err := f.client.Watch(ctx, metav1.ListOptions{})
	require.NoError(t, err)
	require.NotNil(t, w)
	defer w.Stop()

	created, err := f.client.Create(ctx, newGPUCluster("gpu-cluster", nil), metav1.CreateOptions{})
	require.NoError(t, err)
	added := expectEvent[*v1alpha1.GPUCluster](t, w.ResultChan(), watch.Added)
	assert.Equal(t, "gpu-cluster", added.Name)

	created.Spec.DRADriver.Version = "v25.4.0"
	_, err = f.client.Update(ctx, created, metav1.UpdateOptions{})
	require.NoError(t, err)
	modified := expectEvent[*v1alpha1.GPUCluster](t, w.ResultChan(), watch.Modified)
	assert.Equal(t, "v25.4.0", modified.Spec.DRADriver.Version)

	require.NoError(t, f.client.Delete(ctx, "gpu-cluster", metav1.DeleteOptions{}))
	deleted := expectEvent[*v1alpha1.GPUCluster](t, w.ResultChan(), watch.Deleted)
	assert.Equal(t, "gpu-cluster", deleted.Name)

	// The watch action is recorded with the right GVR.
	watchAction, ok := f.fake.Actions()[0].(k8stesting.WatchAction)
	require.True(t, ok)
	assert.Equal(t, "watch", watchAction.GetVerb())
	assert.Equal(t, gpuClusterGVR, watchAction.GetResource())
}

// TestGPUClusters_WatchIsScopedToItsOwnResource proves the tracker's per-GVR
// watcher registry does not leak NVIDIADriver events into a GPUCluster watch.
func TestGPUClusters_WatchIsScopedToItsOwnResource(t *testing.T) {
	f := newGPUClusterFixture(t)
	ctx := t.Context()

	w, err := f.client.Watch(ctx, metav1.ListOptions{})
	require.NoError(t, err)
	defer w.Stop()

	// Mutate the sibling resource first; it must not produce an event here.
	_, err = f.group.NVIDIADrivers().Create(ctx, newDriver("gpu-driver", nil), metav1.CreateOptions{})
	require.NoError(t, err)

	_, err = f.client.Create(ctx, newGPUCluster("gpu-cluster", nil), metav1.CreateOptions{})
	require.NoError(t, err)

	// The first (and only) event delivered is the GPUCluster one.
	added := expectEvent[*v1alpha1.GPUCluster](t, w.ResultChan(), watch.Added)
	assert.Equal(t, "gpu-cluster", added.Name)
}

// TestGPUClusters_WatchForwardsListOptions asserts the generated client hands
// the caller's ListOptions straight through onto the recorded watch action, and
// therefore on to tracker.Watch.
//
// Nothing is asserted about replayed events: whether the tracker replays
// already-known objects, and whether it applies the label selector when it
// does, is client-go's business rather than part of the generated client's
// contract. Event delivery is covered by TestGPUClusters_Watch.
func TestGPUClusters_WatchForwardsListOptions(t *testing.T) {
	f := newGPUClusterFixture(t, newGPUCluster("cluster-a", map[string]string{"tier": "prod"}))

	opts := metav1.ListOptions{ResourceVersion: "0", LabelSelector: "tier=prod"}
	w, err := f.client.Watch(t.Context(), opts)
	require.NoError(t, err)
	defer w.Stop()

	watchAction, ok := lastAction(t, f.fake).(k8stesting.WatchActionImpl)
	require.True(t, ok)
	assert.Equal(t, "watch", watchAction.GetVerb())
	assert.Equal(t, gpuClusterGVR, watchAction.GetResource())
	assert.Empty(t, watchAction.GetNamespace())
	assert.Equal(t, "0", watchAction.ListOptions.ResourceVersion)
	assert.Equal(t, "tier=prod", watchAction.ListOptions.LabelSelector)
	// The generated client also flips Watch on before recording the action.
	assert.True(t, watchAction.ListOptions.Watch)
	assert.Equal(t, "tier=prod", watchAction.GetWatchRestrictions().Labels.String())
}

func TestGPUClusters_ActionsAreClusterScoped(t *testing.T) {
	f := newGPUClusterFixture(t, newGPUCluster("gpu-cluster", nil))
	ctx := t.Context()

	_, _ = f.client.Get(ctx, "gpu-cluster", metav1.GetOptions{})
	_, _ = f.client.List(ctx, metav1.ListOptions{})
	_, _ = f.client.Create(ctx, newGPUCluster("other", nil), metav1.CreateOptions{})
	_, _ = f.client.Update(ctx, newGPUCluster("gpu-cluster", nil), metav1.UpdateOptions{})
	_, _ = f.client.UpdateStatus(ctx, newGPUCluster("gpu-cluster", nil), metav1.UpdateOptions{})
	_, _ = f.client.Patch(ctx, "gpu-cluster", types.MergePatchType, []byte(`{}`), metav1.PatchOptions{})
	_ = f.client.Delete(ctx, "gpu-cluster", metav1.DeleteOptions{})
	_ = f.client.DeleteCollection(ctx, metav1.DeleteOptions{}, metav1.ListOptions{})
	w, err := f.client.Watch(ctx, metav1.ListOptions{})
	require.NoError(t, err)
	w.Stop()

	actions := f.fake.Actions()
	require.Len(t, actions, 9)

	wantVerbs := []string{"get", "list", "create", "update", "update", "patch", "delete", "delete-collection", "watch"}
	for i, action := range actions {
		assert.Equal(t, wantVerbs[i], action.GetVerb(), "action %d verb", i)
		assert.Equal(t, gpuClusterGVR, action.GetResource(), "action %d resource", i)
		// GPUCluster is cluster scoped, so no action ever carries a namespace.
		assert.Empty(t, action.GetNamespace(), "action %d namespace", i)
		assert.True(t, action.Matches(wantVerbs[i], "gpuclusters"), "action %d should match its own verb/resource", i)
	}
}

// TestGPUClusters_ReactorChainIsHonored proves a PrependReactor short-circuits
// the tracker-backed reactor for every typed method.
func TestGPUClusters_ReactorChainIsHonored(t *testing.T) {
	boom := errors.New("boom")

	for _, tc := range []struct {
		name string
		verb string
		call func(context.Context, nvidiav1alpha1.GPUClusterInterface) error
	}{
		{"get", "get", func(ctx context.Context, c nvidiav1alpha1.GPUClusterInterface) error {
			_, err := c.Get(ctx, "gpu-cluster", metav1.GetOptions{})
			return err
		}},
		{"list", "list", func(ctx context.Context, c nvidiav1alpha1.GPUClusterInterface) error {
			_, err := c.List(ctx, metav1.ListOptions{})
			return err
		}},
		{"create", "create", func(ctx context.Context, c nvidiav1alpha1.GPUClusterInterface) error {
			_, err := c.Create(ctx, newGPUCluster("new", nil), metav1.CreateOptions{})
			return err
		}},
		{"update", "update", func(ctx context.Context, c nvidiav1alpha1.GPUClusterInterface) error {
			_, err := c.Update(ctx, newGPUCluster("gpu-cluster", nil), metav1.UpdateOptions{})
			return err
		}},
		{"update status", "update", func(ctx context.Context, c nvidiav1alpha1.GPUClusterInterface) error {
			_, err := c.UpdateStatus(ctx, newGPUCluster("gpu-cluster", nil), metav1.UpdateOptions{})
			return err
		}},
		{"patch", "patch", func(ctx context.Context, c nvidiav1alpha1.GPUClusterInterface) error {
			_, err := c.Patch(ctx, "gpu-cluster", types.MergePatchType, []byte(`{}`), metav1.PatchOptions{})
			return err
		}},
		{"delete", "delete", func(ctx context.Context, c nvidiav1alpha1.GPUClusterInterface) error {
			return c.Delete(ctx, "gpu-cluster", metav1.DeleteOptions{})
		}},
		{"delete collection", "delete-collection", func(ctx context.Context, c nvidiav1alpha1.GPUClusterInterface) error {
			return c.DeleteCollection(ctx, metav1.DeleteOptions{}, metav1.ListOptions{})
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := newGPUClusterFixture(t, newGPUCluster("gpu-cluster", nil))

			var seen k8stesting.Action
			f.fake.PrependReactor(tc.verb, "gpuclusters", func(action k8stesting.Action) (bool, runtime.Object, error) {
				seen = action
				return true, nil, boom
			})

			err := tc.call(t.Context(), f.client)
			require.Error(t, err)
			assert.ErrorIs(t, err, boom)

			require.NotNil(t, seen, "reactor should have observed the action")
			assert.Equal(t, tc.verb, seen.GetVerb())
			assert.Equal(t, gpuClusterGVR, seen.GetResource())

			// The object graph is untouched because the reactor never reached the tracker.
			untouched, trackerErr := f.tracker.Get(gpuClusterGVR, "", "gpu-cluster")
			require.NoError(t, trackerErr)
			require.NotNil(t, untouched)
		})
	}
}

// TestGPUClusters_ReactorIsScopedByResource proves a reactor registered for
// "gpuclusters" does not intercept the sibling NVIDIADriver resource even
// though both share one reaction chain.
func TestGPUClusters_ReactorIsScopedByResource(t *testing.T) {
	f := newFakeGroupFixture(t, newGPUCluster("gpu-cluster", nil), newDriver("gpu-driver", nil))
	boom := errors.New("boom")

	f.fake.PrependReactor("get", "gpuclusters", func(action k8stesting.Action) (bool, runtime.Object, error) {
		return true, nil, boom
	})

	_, err := f.group.GPUClusters().Get(t.Context(), "gpu-cluster", metav1.GetOptions{})
	assert.ErrorIs(t, err, boom)

	driver, err := f.group.NVIDIADrivers().Get(t.Context(), "gpu-driver", metav1.GetOptions{})
	require.NoError(t, err)
	assert.Equal(t, "gpu-driver", driver.Name)
}

// TestGPUClusters_WatchReactorErrorSurfaces covers the parallel watch chain.
func TestGPUClusters_WatchReactorErrorSurfaces(t *testing.T) {
	f := newGPUClusterFixture(t)
	boom := errors.New("watch boom")

	f.fake.PrependWatchReactor("gpuclusters", func(action k8stesting.Action) (bool, watch.Interface, error) {
		return true, nil, boom
	})

	w, err := f.client.Watch(t.Context(), metav1.ListOptions{})
	require.Error(t, err)
	assert.ErrorIs(t, err, boom)
	assert.Nil(t, w)
}

// TestGPUClusters_ReactorCanReturnASubstituteObject proves the reaction chain
// can fabricate results without any tracker involvement at all.
func TestGPUClusters_ReactorCanReturnASubstituteObject(t *testing.T) {
	f := newGPUClusterFixture(t)

	substitute := newGPUCluster("synthetic", map[string]string{"source": "reactor"})
	f.fake.PrependReactor("get", "gpuclusters", func(action k8stesting.Action) (bool, runtime.Object, error) {
		return true, substitute, nil
	})

	got, err := f.client.Get(t.Context(), "anything", metav1.GetOptions{})
	require.NoError(t, err)
	assert.Equal(t, "synthetic", got.Name)
	assert.Equal(t, "reactor", got.Labels["source"])
}
