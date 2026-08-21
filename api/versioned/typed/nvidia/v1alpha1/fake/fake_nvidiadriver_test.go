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

// driverFixture is the shared group fixture (see fake_nvidia_client_test.go)
// narrowed to the NVIDIADriver client.
type driverFixture struct {
	*fakeGroupFixture
	client nvidiav1alpha1.NVIDIADriverInterface
}

func newDriverFixture(t *testing.T, objects ...runtime.Object) *driverFixture {
	t.Helper()
	base := newFakeGroupFixture(t, objects...)
	return &driverFixture{fakeGroupFixture: base, client: base.group.NVIDIADrivers()}
}

func newDriver(name string, labels map[string]string) *v1alpha1.NVIDIADriver {
	return &v1alpha1.NVIDIADriver{
		ObjectMeta: metav1.ObjectMeta{Name: name, Labels: labels},
		Spec: v1alpha1.NVIDIADriverSpec{
			DriverType: v1alpha1.GPU,
			Image:      "nvcr.io/nvidia/driver",
		},
	}
}

func TestNVIDIADrivers_CreateAndGet(t *testing.T) {
	f := newDriverFixture(t)
	ctx := t.Context()

	created, err := f.client.Create(ctx, newDriver("gpu-driver", nil), metav1.CreateOptions{})
	require.NoError(t, err)
	require.NotNil(t, created)
	assert.Equal(t, "gpu-driver", created.Name)
	assert.Equal(t, v1alpha1.GPU, created.Spec.DriverType)

	got, err := f.client.Get(ctx, "gpu-driver", metav1.GetOptions{})
	require.NoError(t, err)
	assert.Equal(t, created, got)

	// The tracker hands back copies: mutating the result must not leak back in.
	got.Spec.Image = "mutated"
	fresh, err := f.client.Get(ctx, "gpu-driver", metav1.GetOptions{})
	require.NoError(t, err)
	assert.Equal(t, "nvcr.io/nvidia/driver", fresh.Spec.Image)
}

func TestNVIDIADrivers_CreateDuplicateIsAlreadyExists(t *testing.T) {
	f := newDriverFixture(t, newDriver("gpu-driver", nil))
	ctx := t.Context()

	_, err := f.client.Create(ctx, newDriver("gpu-driver", nil), metav1.CreateOptions{})
	require.Error(t, err)
	assert.True(t, apierrors.IsAlreadyExists(err), "expected AlreadyExists, got %v", err)
}

func TestNVIDIADrivers_GetMissingIsNotFound(t *testing.T) {
	f := newDriverFixture(t)

	got, err := f.client.Get(t.Context(), "nope", metav1.GetOptions{})
	require.Error(t, err)
	assert.True(t, apierrors.IsNotFound(err), "expected NotFound, got %v", err)
	// On error the generated client still returns a non-nil zero value.
	require.NotNil(t, got)
	assert.Empty(t, got.Name)

	statusErr := &apierrors.StatusError{}
	require.True(t, errors.As(err, &statusErr))
	assert.Equal(t, nvidiaDriverGVR.Group, statusErr.ErrStatus.Details.Group)
	assert.Equal(t, nvidiaDriverGVR.Resource, statusErr.ErrStatus.Details.Kind)
	assert.Equal(t, "nope", statusErr.ErrStatus.Details.Name)
}

func TestNVIDIADrivers_Update(t *testing.T) {
	f := newDriverFixture(t, newDriver("gpu-driver", nil))
	ctx := t.Context()

	current, err := f.client.Get(ctx, "gpu-driver", metav1.GetOptions{})
	require.NoError(t, err)

	f.fake.ClearActions()
	current.Spec.Image = "nvcr.io/nvidia/driver-next"
	updated, err := f.client.Update(ctx, current, metav1.UpdateOptions{})
	require.NoError(t, err)
	assert.Equal(t, "nvcr.io/nvidia/driver-next", updated.Spec.Image)

	// The change is visible through the tracker on the next read.
	reread, err := f.client.Get(ctx, "gpu-driver", metav1.GetOptions{})
	require.NoError(t, err)
	assert.Equal(t, "nvcr.io/nvidia/driver-next", reread.Spec.Image)

	updateAction := f.fake.Actions()[0]
	assert.Equal(t, "update", updateAction.GetVerb())
	assert.Empty(t, updateAction.GetSubresource(), "Update must not target a subresource")
}

func TestNVIDIADrivers_UpdateOfMissingObjectIsNotFound(t *testing.T) {
	f := newDriverFixture(t)

	_, err := f.client.Update(t.Context(), newDriver("ghost", nil), metav1.UpdateOptions{})
	require.Error(t, err)
	assert.True(t, apierrors.IsNotFound(err), "expected NotFound, got %v", err)
}

func TestNVIDIADrivers_UpdateStatus(t *testing.T) {
	f := newDriverFixture(t, newDriver("gpu-driver", nil))
	ctx := t.Context()

	current, err := f.client.Get(ctx, "gpu-driver", metav1.GetOptions{})
	require.NoError(t, err)
	current.Status.State = v1alpha1.Ready
	current.Status.Namespace = "gpu-operator"

	f.fake.ClearActions()
	updated, err := f.client.UpdateStatus(ctx, current, metav1.UpdateOptions{})
	require.NoError(t, err)
	assert.Equal(t, v1alpha1.Ready, updated.Status.State)
	assert.Equal(t, "gpu-operator", updated.Status.Namespace)

	reread, err := f.client.Get(ctx, "gpu-driver", metav1.GetOptions{})
	require.NoError(t, err)
	assert.Equal(t, v1alpha1.Ready, reread.Status.State)

	// UpdateStatus is recorded as an update against the "status" subresource.
	action := f.fake.Actions()[0]
	assert.Equal(t, "update", action.GetVerb())
	assert.Equal(t, "status", action.GetSubresource())
	assert.Equal(t, nvidiaDriverGVR, action.GetResource())
}

// TestNVIDIADrivers_UpdateStatusWithSpecChangeStillTargetsStatusSubresource
// asserts the part of this flow the generated client actually owns: no matter
// what the caller mutated, UpdateStatus is recorded as an "update" against the
// "status" subresource of the right cluster-scoped GVR.
//
// What the reaction chain then does with the spec change is not this package's
// contract. For the record, testing.ObjectReaction applies subresource updates
// as a full-object replace, so today the spec change is persisted rather than
// dropped the way a real API server would drop it. That is an upstream detail
// which may legitimately change, so it is deliberately not asserted here.
func TestNVIDIADrivers_UpdateStatusWithSpecChangeStillTargetsStatusSubresource(t *testing.T) {
	f := newDriverFixture(t, newDriver("gpu-driver", nil))
	ctx := t.Context()

	current, err := f.client.Get(ctx, "gpu-driver", metav1.GetOptions{})
	require.NoError(t, err)
	current.Status.State = v1alpha1.NotReady
	current.Spec.Image = "spec-change-sent-alongside-the-status-update"

	f.fake.ClearActions()
	_, err = f.client.UpdateStatus(ctx, current, metav1.UpdateOptions{})
	require.NoError(t, err)

	action := lastAction(t, f.fake)
	assert.Equal(t, "update", action.GetVerb())
	assert.Equal(t, "status", action.GetSubresource())
	assert.Equal(t, nvidiaDriverGVR, action.GetResource())
	assert.Empty(t, action.GetNamespace(), "NVIDIADriver is cluster scoped")
}

func TestNVIDIADrivers_List(t *testing.T) {
	f := newDriverFixture(t,
		newDriver("driver-a", map[string]string{"tier": "prod"}),
		newDriver("driver-b", map[string]string{"tier": "dev"}),
		newDriver("driver-c", map[string]string{"tier": "prod"}),
	)
	ctx := t.Context()

	all, err := f.client.List(ctx, metav1.ListOptions{})
	require.NoError(t, err)
	require.Len(t, all.Items, 3)
	assert.ElementsMatch(t,
		[]string{"driver-a", "driver-b", "driver-c"},
		[]string{all.Items[0].Name, all.Items[1].Name, all.Items[2].Name},
	)

	// The recorded list action carries both the GVR and the GVK wired into
	// newFakeNVIDIADrivers.
	listAction, ok := lastAction(t, f.fake).(k8stesting.ListActionImpl)
	require.True(t, ok)
	assert.Equal(t, "list", listAction.GetVerb())
	assert.Equal(t, nvidiaDriverGVR, listAction.GetResource())
	assert.Equal(t, nvidiaDriverGVK, listAction.Kind)
}

// TestNVIDIADrivers_ListLabelSelectorPreservesListMeta exercises the generated
// copyListMeta hook:
//
//	func(dst, src *v1alpha1.NVIDIADriverList) { dst.ListMeta = src.ListMeta }
//
// It is only invoked on the label-selector path, where gentype builds a fresh
// list and must carry the ListMeta (notably ResourceVersion) across.
func TestNVIDIADrivers_ListLabelSelectorPreservesListMeta(t *testing.T) {
	f := newDriverFixture(t,
		newDriver("driver-a", map[string]string{"tier": "prod"}),
		newDriver("driver-b", map[string]string{"tier": "dev"}),
		newDriver("driver-c", map[string]string{"tier": "prod"}),
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
		[]string{"driver-a", "driver-c"},
		[]string{filtered.Items[0].Name, filtered.Items[1].Name},
	)
	assert.Equal(t, seededRV, filtered.ResourceVersion,
		"copyListMeta must carry ListMeta onto the label-filtered list")
}

func TestNVIDIADrivers_ListLabelSelectorTable(t *testing.T) {
	objects := []runtime.Object{
		newDriver("driver-a", map[string]string{"tier": "prod", "arch": "amd64"}),
		newDriver("driver-b", map[string]string{"tier": "dev", "arch": "amd64"}),
		newDriver("driver-c", map[string]string{"tier": "prod", "arch": "arm64"}),
		newDriver("driver-unlabeled", nil),
	}

	for _, tc := range []struct {
		name     string
		selector string
		want     []string
	}{
		{"empty selector matches everything", "", []string{"driver-a", "driver-b", "driver-c", "driver-unlabeled"}},
		{"single label", "tier=prod", []string{"driver-a", "driver-c"}},
		{"conjunction", "tier=prod,arch=arm64", []string{"driver-c"}},
		{"set based", "tier in (dev,prod)", []string{"driver-a", "driver-b", "driver-c"}},
		{"negation", "tier!=prod", []string{"driver-b", "driver-unlabeled"}},
		{"no match", "tier=staging", nil},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := newDriverFixture(t, objects...)

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

func TestNVIDIADrivers_Delete(t *testing.T) {
	f := newDriverFixture(t, newDriver("driver-a", nil), newDriver("driver-b", nil))
	ctx := t.Context()

	require.NoError(t, f.client.Delete(ctx, "driver-a", metav1.DeleteOptions{}))

	_, err := f.client.Get(ctx, "driver-a", metav1.GetOptions{})
	require.Error(t, err)
	assert.True(t, apierrors.IsNotFound(err), "expected NotFound after delete, got %v", err)

	// Unrelated objects are untouched.
	_, err = f.client.Get(ctx, "driver-b", metav1.GetOptions{})
	require.NoError(t, err)
}

func TestNVIDIADrivers_DeleteMissingIsNotFound(t *testing.T) {
	f := newDriverFixture(t)

	err := f.client.Delete(t.Context(), "ghost", metav1.DeleteOptions{})
	require.Error(t, err)
	assert.True(t, apierrors.IsNotFound(err), "expected NotFound, got %v", err)
}

// TestNVIDIADrivers_DeleteCollectionRecordsTheAction asserts the generated
// client contract: DeleteCollection is recorded as a "delete-collection" action
// on the right cluster-scoped GVR, carrying through both the DeleteOptions and
// the ListOptions the caller supplied.
//
// Whether anything is actually removed is up to the reaction chain, not the
// generated client. For the record, testing.ObjectReaction has no case for
// DeleteCollectionActionImpl, so with only the default reactor installed the
// action falls through unhandled and the tracker keeps every object; see
// TestNVIDIADrivers_DeleteCollectionWithReactorRemovesAll for the supported way
// to get collection semantics.
func TestNVIDIADrivers_DeleteCollectionRecordsTheAction(t *testing.T) {
	f := newDriverFixture(t, newDriver("driver-a", nil), newDriver("driver-b", nil))

	gracePeriod := int64(30)
	deleteOpts := metav1.DeleteOptions{GracePeriodSeconds: &gracePeriod}
	listOpts := metav1.ListOptions{LabelSelector: "tier=prod"}

	require.NoError(t, f.client.DeleteCollection(t.Context(), deleteOpts, listOpts))

	action, ok := lastAction(t, f.fake).(k8stesting.DeleteCollectionActionImpl)
	require.True(t, ok)
	assert.Equal(t, "delete-collection", action.GetVerb())
	assert.Equal(t, nvidiaDriverGVR, action.GetResource())
	// NVIDIADriver is cluster scoped, so the action carries no namespace.
	assert.Empty(t, action.GetNamespace())
	assert.Equal(t, deleteOpts, action.GetDeleteOptions())
	assert.Equal(t, listOpts, action.GetListOptions())
	assert.Equal(t, "tier=prod", action.GetListRestrictions().Labels.String())
}

// TestNVIDIADrivers_DeleteCollectionWithReactorRemovesAll shows the supported
// way to get collection semantics: a reactor that fans the request out to the
// tracker. This also proves DeleteCollection flows through the reaction chain.
func TestNVIDIADrivers_DeleteCollectionWithReactorRemovesAll(t *testing.T) {
	f := newDriverFixture(t, newDriver("driver-a", nil), newDriver("driver-b", nil))
	ctx := t.Context()

	f.fake.PrependReactor("delete-collection", "nvidiadrivers", func(action k8stesting.Action) (bool, runtime.Object, error) {
		obj, err := f.tracker.List(nvidiaDriverGVR, nvidiaDriverGVK, action.GetNamespace())
		if err != nil {
			return true, nil, err
		}
		list, ok := obj.(*v1alpha1.NVIDIADriverList)
		if !ok {
			return true, nil, errors.New("unexpected list type")
		}
		for i := range list.Items {
			if err := f.tracker.Delete(nvidiaDriverGVR, action.GetNamespace(), list.Items[i].Name); err != nil {
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

func TestNVIDIADrivers_Patch(t *testing.T) {
	f := newDriverFixture(t, newDriver("gpu-driver", map[string]string{"tier": "dev"}))
	ctx := t.Context()

	patch := []byte(`{"metadata":{"labels":{"tier":"prod"}},"spec":{"image":"nvcr.io/nvidia/driver-patched"}}`)
	patched, err := f.client.Patch(ctx, "gpu-driver", types.MergePatchType, patch, metav1.PatchOptions{})
	require.NoError(t, err)
	assert.Equal(t, "nvcr.io/nvidia/driver-patched", patched.Spec.Image)
	assert.Equal(t, "prod", patched.Labels["tier"])

	reread, err := f.client.Get(ctx, "gpu-driver", metav1.GetOptions{})
	require.NoError(t, err)
	assert.Equal(t, "nvcr.io/nvidia/driver-patched", reread.Spec.Image)
	assert.Equal(t, "prod", reread.Labels["tier"])

	action, ok := f.fake.Actions()[0].(k8stesting.PatchAction)
	require.True(t, ok)
	assert.Equal(t, "patch", action.GetVerb())
	assert.Equal(t, types.MergePatchType, action.GetPatchType())
	assert.Equal(t, "gpu-driver", action.GetName())
	assert.Equal(t, nvidiaDriverGVR, action.GetResource())
}

func TestNVIDIADrivers_PatchSubresource(t *testing.T) {
	f := newDriverFixture(t, newDriver("gpu-driver", nil))
	ctx := t.Context()

	patch := []byte(`{"status":{"state":"ready"}}`)
	patched, err := f.client.Patch(ctx, "gpu-driver", types.MergePatchType, patch, metav1.PatchOptions{}, "status")
	require.NoError(t, err)
	assert.Equal(t, v1alpha1.Ready, patched.Status.State)

	assert.Equal(t, "status", f.fake.Actions()[0].GetSubresource())
}

func TestNVIDIADrivers_PatchMissingIsNotFound(t *testing.T) {
	f := newDriverFixture(t)

	_, err := f.client.Patch(t.Context(), "ghost", types.MergePatchType, []byte(`{}`), metav1.PatchOptions{})
	require.Error(t, err)
	assert.True(t, apierrors.IsNotFound(err), "expected NotFound, got %v", err)
}

func TestNVIDIADrivers_Watch(t *testing.T) {
	f := newDriverFixture(t)
	ctx := t.Context()

	w, err := f.client.Watch(ctx, metav1.ListOptions{})
	require.NoError(t, err)
	require.NotNil(t, w)
	defer w.Stop()

	created, err := f.client.Create(ctx, newDriver("gpu-driver", nil), metav1.CreateOptions{})
	require.NoError(t, err)
	added := expectEvent[*v1alpha1.NVIDIADriver](t, w.ResultChan(), watch.Added)
	assert.Equal(t, "gpu-driver", added.Name)

	created.Spec.Image = "nvcr.io/nvidia/driver-next"
	_, err = f.client.Update(ctx, created, metav1.UpdateOptions{})
	require.NoError(t, err)
	modified := expectEvent[*v1alpha1.NVIDIADriver](t, w.ResultChan(), watch.Modified)
	assert.Equal(t, "nvcr.io/nvidia/driver-next", modified.Spec.Image)

	require.NoError(t, f.client.Delete(ctx, "gpu-driver", metav1.DeleteOptions{}))
	deleted := expectEvent[*v1alpha1.NVIDIADriver](t, w.ResultChan(), watch.Deleted)
	assert.Equal(t, "gpu-driver", deleted.Name)

	// The watch action is recorded with Watch=true and the right GVR.
	watchAction, ok := f.fake.Actions()[0].(k8stesting.WatchAction)
	require.True(t, ok)
	assert.Equal(t, "watch", watchAction.GetVerb())
	assert.Equal(t, nvidiaDriverGVR, watchAction.GetResource())
}

// TestNVIDIADrivers_WatchForwardsListOptions asserts the generated client hands
// the caller's ListOptions straight through onto the recorded watch action, and
// therefore on to tracker.Watch.
//
// Nothing is asserted about replayed events: whether the tracker replays
// already-known objects, and whether it applies the label selector when it
// does, is client-go's business rather than part of the generated client's
// contract. Event delivery is covered by TestNVIDIADrivers_Watch.
func TestNVIDIADrivers_WatchForwardsListOptions(t *testing.T) {
	f := newDriverFixture(t, newDriver("driver-a", map[string]string{"tier": "prod"}))

	opts := metav1.ListOptions{ResourceVersion: "0", LabelSelector: "tier=prod"}
	w, err := f.client.Watch(t.Context(), opts)
	require.NoError(t, err)
	defer w.Stop()

	watchAction, ok := lastAction(t, f.fake).(k8stesting.WatchActionImpl)
	require.True(t, ok)
	assert.Equal(t, "watch", watchAction.GetVerb())
	assert.Equal(t, nvidiaDriverGVR, watchAction.GetResource())
	assert.Empty(t, watchAction.GetNamespace())
	assert.Equal(t, "0", watchAction.ListOptions.ResourceVersion)
	assert.Equal(t, "tier=prod", watchAction.ListOptions.LabelSelector)
	// The generated client also flips Watch on before recording the action.
	assert.True(t, watchAction.ListOptions.Watch)
	assert.Equal(t, "tier=prod", watchAction.GetWatchRestrictions().Labels.String())
}

func TestNVIDIADrivers_ActionsAreClusterScoped(t *testing.T) {
	f := newDriverFixture(t, newDriver("gpu-driver", nil))
	ctx := t.Context()

	_, _ = f.client.Get(ctx, "gpu-driver", metav1.GetOptions{})
	_, _ = f.client.List(ctx, metav1.ListOptions{})
	_, _ = f.client.Create(ctx, newDriver("other", nil), metav1.CreateOptions{})
	_, _ = f.client.Update(ctx, newDriver("gpu-driver", nil), metav1.UpdateOptions{})
	_, _ = f.client.UpdateStatus(ctx, newDriver("gpu-driver", nil), metav1.UpdateOptions{})
	_, _ = f.client.Patch(ctx, "gpu-driver", types.MergePatchType, []byte(`{}`), metav1.PatchOptions{})
	_ = f.client.Delete(ctx, "gpu-driver", metav1.DeleteOptions{})
	_ = f.client.DeleteCollection(ctx, metav1.DeleteOptions{}, metav1.ListOptions{})
	w, err := f.client.Watch(ctx, metav1.ListOptions{})
	require.NoError(t, err)
	w.Stop()

	actions := f.fake.Actions()
	require.Len(t, actions, 9)

	wantVerbs := []string{"get", "list", "create", "update", "update", "patch", "delete", "delete-collection", "watch"}
	for i, action := range actions {
		assert.Equal(t, wantVerbs[i], action.GetVerb(), "action %d verb", i)
		assert.Equal(t, nvidiaDriverGVR, action.GetResource(), "action %d resource", i)
		// NVIDIADriver is cluster scoped, so no action ever carries a namespace.
		assert.Empty(t, action.GetNamespace(), "action %d namespace", i)
		assert.True(t, action.Matches(wantVerbs[i], "nvidiadrivers"), "action %d should match its own verb/resource", i)
	}
}

// TestNVIDIADrivers_ReactorChainIsHonored proves a PrependReactor short-circuits
// the tracker-backed reactor for every typed method.
func TestNVIDIADrivers_ReactorChainIsHonored(t *testing.T) {
	boom := errors.New("boom")

	for _, tc := range []struct {
		name string
		verb string
		call func(context.Context, nvidiav1alpha1.NVIDIADriverInterface) error
	}{
		{"get", "get", func(ctx context.Context, c nvidiav1alpha1.NVIDIADriverInterface) error {
			_, err := c.Get(ctx, "gpu-driver", metav1.GetOptions{})
			return err
		}},
		{"list", "list", func(ctx context.Context, c nvidiav1alpha1.NVIDIADriverInterface) error {
			_, err := c.List(ctx, metav1.ListOptions{})
			return err
		}},
		{"create", "create", func(ctx context.Context, c nvidiav1alpha1.NVIDIADriverInterface) error {
			_, err := c.Create(ctx, newDriver("new", nil), metav1.CreateOptions{})
			return err
		}},
		{"update", "update", func(ctx context.Context, c nvidiav1alpha1.NVIDIADriverInterface) error {
			_, err := c.Update(ctx, newDriver("gpu-driver", nil), metav1.UpdateOptions{})
			return err
		}},
		{"update status", "update", func(ctx context.Context, c nvidiav1alpha1.NVIDIADriverInterface) error {
			_, err := c.UpdateStatus(ctx, newDriver("gpu-driver", nil), metav1.UpdateOptions{})
			return err
		}},
		{"patch", "patch", func(ctx context.Context, c nvidiav1alpha1.NVIDIADriverInterface) error {
			_, err := c.Patch(ctx, "gpu-driver", types.MergePatchType, []byte(`{}`), metav1.PatchOptions{})
			return err
		}},
		{"delete", "delete", func(ctx context.Context, c nvidiav1alpha1.NVIDIADriverInterface) error {
			return c.Delete(ctx, "gpu-driver", metav1.DeleteOptions{})
		}},
		{"delete collection", "delete-collection", func(ctx context.Context, c nvidiav1alpha1.NVIDIADriverInterface) error {
			return c.DeleteCollection(ctx, metav1.DeleteOptions{}, metav1.ListOptions{})
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := newDriverFixture(t, newDriver("gpu-driver", nil))

			var seen k8stesting.Action
			f.fake.PrependReactor(tc.verb, "nvidiadrivers", func(action k8stesting.Action) (bool, runtime.Object, error) {
				seen = action
				return true, nil, boom
			})

			err := tc.call(t.Context(), f.client)
			require.Error(t, err)
			assert.ErrorIs(t, err, boom)

			require.NotNil(t, seen, "reactor should have observed the action")
			assert.Equal(t, tc.verb, seen.GetVerb())
			assert.Equal(t, nvidiaDriverGVR, seen.GetResource())

			// The object graph is untouched because the reactor never reached the tracker.
			untouched, trackerErr := f.tracker.Get(nvidiaDriverGVR, "", "gpu-driver")
			require.NoError(t, trackerErr)
			require.NotNil(t, untouched)
		})
	}
}

// TestNVIDIADrivers_WatchReactorErrorSurfaces covers the parallel watch chain.
func TestNVIDIADrivers_WatchReactorErrorSurfaces(t *testing.T) {
	f := newDriverFixture(t)
	boom := errors.New("watch boom")

	f.fake.PrependWatchReactor("nvidiadrivers", func(action k8stesting.Action) (bool, watch.Interface, error) {
		return true, nil, boom
	})

	w, err := f.client.Watch(t.Context(), metav1.ListOptions{})
	require.Error(t, err)
	assert.ErrorIs(t, err, boom)
	assert.Nil(t, w)
}

// TestNVIDIADrivers_ReactorCanReturnASubstituteObject proves the reaction chain
// can fabricate results without any tracker involvement at all.
func TestNVIDIADrivers_ReactorCanReturnASubstituteObject(t *testing.T) {
	f := newDriverFixture(t)

	substitute := newDriver("synthetic", map[string]string{"source": "reactor"})
	f.fake.PrependReactor("get", "nvidiadrivers", func(action k8stesting.Action) (bool, runtime.Object, error) {
		return true, substitute, nil
	})

	got, err := f.client.Get(t.Context(), "anything", metav1.GetOptions{})
	require.NoError(t, err)
	assert.Equal(t, "synthetic", got.Name)
	assert.Equal(t, "reactor", got.Labels["source"])
}
