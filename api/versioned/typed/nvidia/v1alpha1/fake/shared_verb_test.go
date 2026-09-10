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

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/watch"
	k8stesting "k8s.io/client-go/testing"
)

// fieldManagerName is the field manager the option-forwarding cases send.
const fieldManagerName = "gpu-operator"

// Both resources of this group are served by the same generated
// gentype.FakeClientWithList over one shared reaction chain, so every verb
// behaves identically apart from the object type it carries. The functions
// below assert that shared behaviour once; each resource file supplies a
// resourceUnderTest and calls runSharedVerbTests.

// typedObject is the constraint for the typed object a resource client
// returns: the generated types are runtime.Objects with an ObjectMeta.
type typedObject interface {
	metav1.Object
	runtime.Object
}

// typedList is the constraint for the typed list a resource client returns.
type typedList interface {
	metav1.ListInterface
	runtime.Object
}

// typedResourceClient is the verb set every generated typed interface in this
// group shares. Both typednvidiav1alpha1.GPUClusterInterface and
// typednvidiav1alpha1.NVIDIADriverInterface satisfy it, which is what lets one
// set of generic functions drive both resources.
type typedResourceClient[T typedObject, L typedList] interface {
	Create(ctx context.Context, obj T, createOptions metav1.CreateOptions) (T, error)
	Update(ctx context.Context, obj T, updateOptions metav1.UpdateOptions) (T, error)
	UpdateStatus(ctx context.Context, obj T, updateOptions metav1.UpdateOptions) (T, error)
	Delete(ctx context.Context, name string, deleteOptions metav1.DeleteOptions) error
	DeleteCollection(ctx context.Context, deleteOptions metav1.DeleteOptions, listOptions metav1.ListOptions) error
	Get(ctx context.Context, name string, getOptions metav1.GetOptions) (T, error)
	List(ctx context.Context, listOptions metav1.ListOptions) (L, error)
	Watch(ctx context.Context, listOptions metav1.ListOptions) (watch.Interface, error)
	Patch(ctx context.Context, name string, patchType types.PatchType, patchData []byte, patchOptions metav1.PatchOptions, subresources ...string) (T, error)
}

// resourceUnderTest describes the one generated fake client under test. Only
// what is listed here differs between resources.
type resourceUnderTest[T typedObject, L typedList] struct {
	groupVersionResource schema.GroupVersionResource
	groupVersionKind     schema.GroupVersionKind

	// objectName names the object the single-object cases operate on; the
	// collection cases seed "<objectName>-a", "-b", "-c" and "-unlabeled".
	objectName string

	newClient func(*fakeGroupFixture) typedResourceClient[T, L]
	newObject func(name string, labels map[string]string) T
	items     func(L) []T
}

// runSharedVerbTests asserts everything the generated fake contributes that
// does not depend on the resource's own schema: the shape of the actions it
// records, and the list handling gentype drives through its generated hooks.
func runSharedVerbTests[T typedObject, L typedList](t *testing.T, resource resourceUnderTest[T, L]) {
	t.Helper()

	t.Run("every verb records a cluster scoped action", func(t *testing.T) { testEveryVerbRecordsAClusterScopedAction(t, resource) })
	t.Run("only the status verbs set a subresource", func(t *testing.T) { testOnlyStatusVerbsSetASubresource(t, resource) })
	t.Run("caller options reach the recorded action", func(t *testing.T) { testCallerOptionsReachTheRecordedAction(t, resource) })
	t.Run("List records the list kind", func(t *testing.T) { testListRecordsTheListKind(t, resource) })
	t.Run("List filters on the label selector", func(t *testing.T) { testListFiltersOnTheLabelSelector(t, resource) })
	t.Run("List copies the list meta onto a filtered list", func(t *testing.T) { testListCopiesListMeta(t, resource) })
}

// objectNames extracts the names of a list's items, which is what the
// collection assertions compare.
func objectNames[T typedObject](objects []T) []string {
	names := make([]string, 0, len(objects))
	for _, object := range objects {
		names = append(names, object.GetName())
	}
	return names
}

// testEveryVerbRecordsAClusterScopedAction is the contract a test using this
// fake reads back: every verb reaches the shared recorder tagged with its own
// name and this resource's GVR, and none of them invents a namespace for a
// cluster-scoped resource.
func testEveryVerbRecordsAClusterScopedAction[T typedObject, L typedList](t *testing.T, resource resourceUnderTest[T, L]) {
	fixture := newFakeGroupFixture(t, resource.newObject(resource.objectName, nil))
	client := resource.newClient(fixture)
	ctx := t.Context()

	// Call errors are discarded on purpose: this test asserts the shape of the
	// recorded actions, never the outcome of the calls.
	_, _ = client.Get(ctx, resource.objectName, metav1.GetOptions{})
	_, _ = client.List(ctx, metav1.ListOptions{})
	_, _ = client.Create(ctx, resource.newObject("other", nil), metav1.CreateOptions{})
	_, _ = client.Update(ctx, resource.newObject(resource.objectName, nil), metav1.UpdateOptions{})
	_, _ = client.UpdateStatus(ctx, resource.newObject(resource.objectName, nil), metav1.UpdateOptions{})
	_, _ = client.Patch(ctx, resource.objectName, types.MergePatchType, []byte(`{}`), metav1.PatchOptions{})
	_ = client.Delete(ctx, resource.objectName, metav1.DeleteOptions{})
	_ = client.DeleteCollection(ctx, metav1.DeleteOptions{}, metav1.ListOptions{})
	watcher, err := client.Watch(ctx, metav1.ListOptions{})
	require.NoError(t, err)
	require.NotNil(t, watcher)
	watcher.Stop()

	expectedVerbs := []string{"get", "list", "create", "update", "update", "patch", "delete", "delete-collection", "watch"}
	actions := fixture.testingFake.Actions()
	require.Len(t, actions, len(expectedVerbs))

	for actionIndex, action := range actions {
		assert.Equal(t, expectedVerbs[actionIndex], action.GetVerb(), "action %d verb", actionIndex)
		assert.Equal(t, resource.groupVersionResource, action.GetResource(), "action %d resource", actionIndex)
		assert.Empty(t, action.GetNamespace(), "action %d namespace: %s is cluster scoped", actionIndex, resource.groupVersionKind.Kind)
	}
}

func testOnlyStatusVerbsSetASubresource[T typedObject, L typedList](t *testing.T, resource resourceUnderTest[T, L]) {
	testCases := []struct {
		name                string
		expectedSubresource string
		call                func(context.Context, typedResourceClient[T, L])
	}{
		{name: "update", expectedSubresource: "", call: func(ctx context.Context, client typedResourceClient[T, L]) {
			_, _ = client.Update(ctx, resource.newObject(resource.objectName, nil), metav1.UpdateOptions{})
		}},
		{name: "update status", expectedSubresource: "status", call: func(ctx context.Context, client typedResourceClient[T, L]) {
			_, _ = client.UpdateStatus(ctx, resource.newObject(resource.objectName, nil), metav1.UpdateOptions{})
		}},
		{name: "patch", expectedSubresource: "", call: func(ctx context.Context, client typedResourceClient[T, L]) {
			_, _ = client.Patch(ctx, resource.objectName, types.MergePatchType, []byte(`{}`), metav1.PatchOptions{})
		}},
		{name: "patch status", expectedSubresource: "status", call: func(ctx context.Context, client typedResourceClient[T, L]) {
			_, _ = client.Patch(ctx, resource.objectName, types.MergePatchType, []byte(`{}`), metav1.PatchOptions{}, "status")
		}},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			fixture := newFakeGroupFixture(t, resource.newObject(resource.objectName, nil))

			testCase.call(t.Context(), resource.newClient(fixture))

			action := lastAction(t, fixture.testingFake)
			assert.Equal(t, testCase.expectedSubresource, action.GetSubresource())
			assert.Equal(t, resource.groupVersionResource, action.GetResource())
		})
	}
}

// testCallerOptionsReachTheRecordedAction is the core contract of this layer:
// whatever a caller passes has to be readable off the recorded action, because
// that is all a test using this fake can assert against.
func testCallerOptionsReachTheRecordedAction[T typedObject, L typedList](t *testing.T, resource resourceUnderTest[T, L]) {
	dryRun := []string{metav1.DryRunAll}
	getOptions := metav1.GetOptions{ResourceVersion: "17"}
	createOptions := metav1.CreateOptions{DryRun: dryRun, FieldManager: fieldManagerName, FieldValidation: metav1.FieldValidationStrict}
	updateOptions := metav1.UpdateOptions{DryRun: dryRun, FieldManager: fieldManagerName, FieldValidation: metav1.FieldValidationWarn}
	deleteOptions := metav1.DeleteOptions{
		DryRun:             dryRun,
		GracePeriodSeconds: new(int64(30)),
		Preconditions:      &metav1.Preconditions{UID: new(types.UID("d1cf8a0e")), ResourceVersion: new("17")},
		PropagationPolicy:  new(metav1.DeletePropagationForeground),
	}
	patchOptions := metav1.PatchOptions{DryRun: dryRun, FieldManager: fieldManagerName, Force: new(true)}
	patchData := []byte(`{"metadata":{"labels":{"tier":"prod"}}}`)
	listOptions := metav1.ListOptions{
		LabelSelector:   "tier=prod",
		FieldSelector:   "metadata.name=" + resource.objectName,
		ResourceVersion: "17",
		Limit:           7,
		Continue:        "continue-token",
		TimeoutSeconds:  new(int64(45)),
	}
	watchListOptions := listOptions
	watchListOptions.Watch = true

	testCases := []struct {
		name           string
		call           func(context.Context, typedResourceClient[T, L])
		requireOptions func(*testing.T, k8stesting.Action)
	}{
		{
			name: "get",
			call: func(ctx context.Context, client typedResourceClient[T, L]) {
				_, _ = client.Get(ctx, resource.objectName, getOptions)
			},
			requireOptions: func(t *testing.T, action k8stesting.Action) {
				getAction, ok := action.(k8stesting.GetActionImpl)
				require.True(t, ok, "expected %T, got %T", k8stesting.GetActionImpl{}, action)
				assert.Equal(t, getOptions, getAction.GetGetOptions())
			},
		},
		{
			name: "list",
			call: func(ctx context.Context, client typedResourceClient[T, L]) {
				_, _ = client.List(ctx, listOptions)
			},
			requireOptions: func(t *testing.T, action k8stesting.Action) {
				listAction, ok := action.(k8stesting.ListActionImpl)
				require.True(t, ok, "expected %T, got %T", k8stesting.ListActionImpl{}, action)
				assert.Equal(t, listOptions, listAction.GetListOptions())
			},
		},
		{
			name: "create",
			call: func(ctx context.Context, client typedResourceClient[T, L]) {
				_, _ = client.Create(ctx, resource.newObject("created", nil), createOptions)
			},
			requireOptions: func(t *testing.T, action k8stesting.Action) {
				createAction, ok := action.(k8stesting.CreateActionImpl)
				require.True(t, ok, "expected %T, got %T", k8stesting.CreateActionImpl{}, action)
				assert.Equal(t, createOptions, createAction.GetCreateOptions())
			},
		},
		{
			name: "update",
			call: func(ctx context.Context, client typedResourceClient[T, L]) {
				_, _ = client.Update(ctx, resource.newObject(resource.objectName, nil), updateOptions)
			},
			requireOptions: func(t *testing.T, action k8stesting.Action) {
				updateAction, ok := action.(k8stesting.UpdateActionImpl)
				require.True(t, ok, "expected %T, got %T", k8stesting.UpdateActionImpl{}, action)
				assert.Equal(t, updateOptions, updateAction.GetUpdateOptions())
			},
		},
		{
			name: "update status",
			call: func(ctx context.Context, client typedResourceClient[T, L]) {
				_, _ = client.UpdateStatus(ctx, resource.newObject(resource.objectName, nil), updateOptions)
			},
			requireOptions: func(t *testing.T, action k8stesting.Action) {
				updateStatusAction, ok := action.(k8stesting.UpdateActionImpl)
				require.True(t, ok, "expected %T, got %T", k8stesting.UpdateActionImpl{}, action)
				assert.Equal(t, updateOptions, updateStatusAction.GetUpdateOptions())
			},
		},
		{
			name: "delete",
			call: func(ctx context.Context, client typedResourceClient[T, L]) {
				_ = client.Delete(ctx, resource.objectName, deleteOptions)
			},
			requireOptions: func(t *testing.T, action k8stesting.Action) {
				deleteAction, ok := action.(k8stesting.DeleteActionImpl)
				require.True(t, ok, "expected %T, got %T", k8stesting.DeleteActionImpl{}, action)
				assert.Equal(t, deleteOptions, deleteAction.GetDeleteOptions())
			},
		},
		{
			name: "delete collection",
			call: func(ctx context.Context, client typedResourceClient[T, L]) {
				_ = client.DeleteCollection(ctx, deleteOptions, listOptions)
			},
			requireOptions: func(t *testing.T, action k8stesting.Action) {
				deleteCollectionAction, ok := action.(k8stesting.DeleteCollectionActionImpl)
				require.True(t, ok, "expected %T, got %T", k8stesting.DeleteCollectionActionImpl{}, action)
				assert.Equal(t, deleteOptions, deleteCollectionAction.GetDeleteOptions())
				assert.Equal(t, listOptions, deleteCollectionAction.GetListOptions())
			},
		},
		{
			name: "patch",
			call: func(ctx context.Context, client typedResourceClient[T, L]) {
				_, _ = client.Patch(ctx, resource.objectName, types.MergePatchType, patchData, patchOptions)
			},
			requireOptions: func(t *testing.T, action k8stesting.Action) {
				patchAction, ok := action.(k8stesting.PatchActionImpl)
				require.True(t, ok, "expected %T, got %T", k8stesting.PatchActionImpl{}, action)
				assert.Equal(t, patchOptions, patchAction.GetPatchOptions())
				assert.Equal(t, types.MergePatchType, patchAction.GetPatchType())
				assert.Equal(t, patchData, patchAction.GetPatch())
				assert.Equal(t, resource.objectName, patchAction.GetName())
			},
		},
		{
			name: "watch",
			call: func(ctx context.Context, client typedResourceClient[T, L]) {
				watcher, err := client.Watch(ctx, listOptions)
				require.NoError(t, err)
				require.NotNil(t, watcher)
				watcher.Stop()
			},
			requireOptions: func(t *testing.T, action k8stesting.Action) {
				watchAction, ok := action.(k8stesting.WatchActionImpl)
				require.True(t, ok, "expected %T, got %T", k8stesting.WatchActionImpl{}, action)
				// The generated client flips Watch on before recording.
				assert.Equal(t, watchListOptions, watchAction.GetListOptions())
			},
		},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			fixture := newFakeGroupFixture(t, resource.newObject(resource.objectName, nil))

			testCase.call(t.Context(), resource.newClient(fixture))

			testCase.requireOptions(t, lastAction(t, fixture.testingFake))
		})
	}
}

func testListRecordsTheListKind[T typedObject, L typedList](t *testing.T, resource resourceUnderTest[T, L]) {
	fixture := newFakeGroupFixture(t)

	_, err := resource.newClient(fixture).List(t.Context(), metav1.ListOptions{})
	require.NoError(t, err)

	action := lastAction(t, fixture.testingFake)
	listAction, ok := action.(k8stesting.ListActionImpl)
	require.True(t, ok, "expected %T, got %T", k8stesting.ListActionImpl{}, action)
	assert.Equal(t, resource.groupVersionResource, listAction.GetResource())
	assert.Equal(t, resource.groupVersionKind, listAction.Kind)
}

func testListFiltersOnTheLabelSelector[T typedObject, L typedList](t *testing.T, resource resourceUnderTest[T, L]) {
	nameA := resource.objectName + "-a"
	nameB := resource.objectName + "-b"
	nameC := resource.objectName + "-c"
	nameUnlabeled := resource.objectName + "-unlabeled"

	seedObjects := []runtime.Object{
		resource.newObject(nameA, map[string]string{"tier": "prod", "arch": "amd64"}),
		resource.newObject(nameB, map[string]string{"tier": "dev", "arch": "amd64"}),
		resource.newObject(nameC, map[string]string{"tier": "prod", "arch": "arm64"}),
		resource.newObject(nameUnlabeled, nil),
	}

	testCases := []struct {
		name          string
		selector      string
		expectedNames []string
	}{
		{name: "empty selector matches everything", selector: "", expectedNames: []string{nameA, nameB, nameC, nameUnlabeled}},
		{name: "single label", selector: "tier=prod", expectedNames: []string{nameA, nameC}},
		{name: "conjunction", selector: "tier=prod,arch=arm64", expectedNames: []string{nameC}},
		{name: "set based", selector: "tier in (dev,prod)", expectedNames: []string{nameA, nameB, nameC}},
		{name: "negation", selector: "tier!=prod", expectedNames: []string{nameB, nameUnlabeled}},
		{name: "no match", selector: "tier=staging", expectedNames: nil},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			fixture := newFakeGroupFixture(t, seedObjects...)

			objectList, err := resource.newClient(fixture).List(t.Context(), metav1.ListOptions{LabelSelector: testCase.selector})
			require.NoError(t, err)
			assert.ElementsMatch(t, testCase.expectedNames, objectNames(resource.items(objectList)))
		})
	}
}

// testListCopiesListMeta exercises the generated copyListMeta hook, for
// example:
//
//	func(dst, src *v1alpha1.GPUClusterList) { dst.ListMeta = src.ListMeta }
//
// testing.ExtractFromListOptions defaults an unset label selector to
// labels.Everything() rather than nil, so gentype rebuilds the list and calls
// the hook on every List, filtered or not.
func testListCopiesListMeta[T typedObject, L typedList](t *testing.T, resource resourceUnderTest[T, L]) {
	fixture := newFakeGroupFixture(t,
		resource.newObject(resource.objectName+"-a", map[string]string{"tier": "prod"}),
		resource.newObject(resource.objectName+"-b", map[string]string{"tier": "dev"}),
	)
	client := resource.newClient(fixture)
	ctx := t.Context()

	// The tracker stamps the collection ResourceVersion onto every list it returns.
	unfilteredList, err := client.List(ctx, metav1.ListOptions{})
	require.NoError(t, err)
	seededResourceVersion := unfilteredList.GetResourceVersion()
	require.NotEmpty(t, seededResourceVersion)
	require.NotEqual(t, "0", seededResourceVersion)

	matchingList, err := client.List(ctx, metav1.ListOptions{LabelSelector: "tier=prod"})
	require.NoError(t, err)
	assert.Equal(t, []string{resource.objectName + "-a"}, objectNames(resource.items(matchingList)))
	assert.Equal(t, seededResourceVersion, matchingList.GetResourceVersion(),
		"copyListMeta must carry ListMeta onto the label-filtered list")
}
