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
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/watch"
	k8stesting "k8s.io/client-go/testing"

	nvidiav1 "github.com/NVIDIA/gpu-operator/api/nvidia/v1"
	clientsetscheme "github.com/NVIDIA/gpu-operator/api/versioned/scheme"
	typednvidiav1 "github.com/NVIDIA/gpu-operator/api/versioned/typed/nvidia/v1"
)

// fakeClusterPolicies must implement the generated typed interface.
var _ typednvidiav1.ClusterPolicyInterface = &fakeClusterPolicies{}

// Derived from the API package so every expectation below follows a group or
// version bump automatically. TestNewFakeClusterPolicies is the one place that
// pins the literal wire values these are built from.
var (
	clusterPolicyGVR = nvidiav1.SchemeGroupVersion.WithResource("clusterpolicies")
	clusterPolicyGVK = nvidiav1.SchemeGroupVersion.WithKind("ClusterPolicy")
)

const (
	clusterPolicyName = "cluster-policy"
	// fieldManagerName is the field manager the option-forwarding cases send.
	fieldManagerName = "gpu-operator"
)

// clusterPolicyFixture wires a bare testing.Fake to an ObjectTracker exactly the
// way the generated top-level fake clientset does, without importing it (that
// would create an import cycle).
type clusterPolicyFixture struct {
	testingFake *k8stesting.Fake
	tracker     k8stesting.ObjectTracker
	client      typednvidiav1.ClusterPolicyInterface
}

func newClusterPolicyFixture(t *testing.T, seedObjects ...runtime.Object) *clusterPolicyFixture {
	t.Helper()

	tracker := k8stesting.NewObjectTracker(clientsetscheme.Scheme, clientsetscheme.Codecs.UniversalDecoder())
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

	return &clusterPolicyFixture{
		testingFake: testingFake,
		tracker:     tracker,
		client:      newFakeClusterPolicies(&FakeNvidiaV1{Fake: testingFake}),
	}
}

func newClusterPolicy(name string, labels map[string]string) *nvidiav1.ClusterPolicy {
	clusterPolicy := &nvidiav1.ClusterPolicy{
		ObjectMeta: metav1.ObjectMeta{Name: name, Labels: labels},
	}
	clusterPolicy.Spec.Operator.RuntimeClass = "nvidia"
	return clusterPolicy
}

// TestNewFakeClusterPolicies spells the GVR, GVK and namespace baked into the
// generated constructor out literally. Every other expectation here derives from
// SchemeGroupVersion, so a group or version bump would move the constructor and
// the expectation together and go unnoticed; this is what pins them.
func TestNewFakeClusterPolicies(t *testing.T) {
	client, ok := newFakeClusterPolicies(&FakeNvidiaV1{Fake: &k8stesting.Fake{}}).(*fakeClusterPolicies)
	require.True(t, ok, "expected *fakeClusterPolicies")

	assert.Equal(t, schema.GroupVersionResource{Group: "nvidia.com", Version: "v1", Resource: "clusterpolicies"},
		client.Resource())
	assert.Equal(t, schema.GroupVersionKind{Group: "nvidia.com", Version: "v1", Kind: "ClusterPolicy"},
		client.Kind())
	assert.Empty(t, client.Namespace(), "ClusterPolicy is cluster scoped")
}

// TestClusterPoliciesRecordedActions walks the whole interface. Nothing below
// this layer consumes the metav1 options, so a verb that does not land on the
// recorded action with the caller's options intact leaves a user reactor with
// nothing to match on. Each expected action is built with the same
// k8s.io/client-go/testing constructor the generated client is supposed to
// reach for, so comparing them whole pins the verb, the GVR, the subresource,
// the cluster scope and the options in one assertion.
func TestClusterPoliciesRecordedActions(t *testing.T) {
	dryRunAll := []string{metav1.DryRunAll}
	createOptions := metav1.CreateOptions{DryRun: dryRunAll, FieldManager: fieldManagerName, FieldValidation: metav1.FieldValidationStrict}
	updateOptions := metav1.UpdateOptions{DryRun: dryRunAll, FieldManager: fieldManagerName, FieldValidation: metav1.FieldValidationStrict}
	patchOptions := metav1.PatchOptions{DryRun: dryRunAll, Force: new(true), FieldManager: fieldManagerName}
	deleteOptions := metav1.DeleteOptions{DryRun: dryRunAll, GracePeriodSeconds: new(int64(30)), PropagationPolicy: new(metav1.DeletePropagationForeground)}
	listOptions := metav1.ListOptions{LabelSelector: "tier=gold", FieldSelector: "metadata.name=cluster-policy", Limit: 7, Continue: "continue-token"}
	// gentype forces Watch on before handing the options over.
	watchOptions := listOptions
	watchOptions.Watch = true

	clusterPolicyToCreate := newClusterPolicy("new-cluster-policy", nil)
	clusterPolicyToUpdate := newClusterPolicy(clusterPolicyName, map[string]string{"tier": "gold"})
	labelPatch := []byte(`{"metadata":{"labels":{"tier":"platinum"}}}`)

	testCases := []struct {
		name           string
		call           func(t *testing.T, client typednvidiav1.ClusterPolicyInterface)
		expectedAction k8stesting.Action
	}{
		{
			name: "create",
			call: func(t *testing.T, client typednvidiav1.ClusterPolicyInterface) {
				_, err := client.Create(t.Context(), clusterPolicyToCreate, createOptions)
				require.NoError(t, err)
			},
			expectedAction: k8stesting.NewCreateActionWithOptions(clusterPolicyGVR, "", clusterPolicyToCreate, createOptions),
		},
		{
			name: "get",
			call: func(t *testing.T, client typednvidiav1.ClusterPolicyInterface) {
				_, err := client.Get(t.Context(), clusterPolicyName, metav1.GetOptions{ResourceVersion: "0"})
				require.NoError(t, err)
			},
			expectedAction: k8stesting.NewGetActionWithOptions(clusterPolicyGVR, "", clusterPolicyName,
				metav1.GetOptions{ResourceVersion: "0"}),
		},
		{
			name: "list",
			call: func(t *testing.T, client typednvidiav1.ClusterPolicyInterface) {
				_, err := client.List(t.Context(), listOptions)
				require.NoError(t, err)
			},
			expectedAction: k8stesting.NewListActionWithOptions(clusterPolicyGVR, clusterPolicyGVK, "", listOptions),
		},
		{
			name: "update",
			call: func(t *testing.T, client typednvidiav1.ClusterPolicyInterface) {
				_, err := client.Update(t.Context(), clusterPolicyToUpdate, updateOptions)
				require.NoError(t, err)
			},
			expectedAction: k8stesting.NewUpdateActionWithOptions(clusterPolicyGVR, "", clusterPolicyToUpdate, updateOptions),
		},
		{
			name: "update status",
			call: func(t *testing.T, client typednvidiav1.ClusterPolicyInterface) {
				_, err := client.UpdateStatus(t.Context(), clusterPolicyToUpdate, updateOptions)
				require.NoError(t, err)
			},
			expectedAction: k8stesting.NewUpdateSubresourceActionWithOptions(clusterPolicyGVR, "status", "",
				clusterPolicyToUpdate, updateOptions),
		},
		{
			name: "patch",
			call: func(t *testing.T, client typednvidiav1.ClusterPolicyInterface) {
				_, err := client.Patch(t.Context(), clusterPolicyName, types.MergePatchType, labelPatch, patchOptions)
				require.NoError(t, err)
			},
			expectedAction: k8stesting.NewPatchActionWithOptions(clusterPolicyGVR, "", clusterPolicyName,
				types.MergePatchType, labelPatch, patchOptions),
		},
		{
			name: "patch status",
			call: func(t *testing.T, client typednvidiav1.ClusterPolicyInterface) {
				_, err := client.Patch(t.Context(), clusterPolicyName, types.MergePatchType,
					[]byte(`{"status":{"state":"ready"}}`), metav1.PatchOptions{}, "status")
				require.NoError(t, err)
			},
			expectedAction: k8stesting.NewPatchSubresourceActionWithOptions(clusterPolicyGVR, "", clusterPolicyName,
				types.MergePatchType, []byte(`{"status":{"state":"ready"}}`), metav1.PatchOptions{}, "status"),
		},
		{
			name: "delete",
			call: func(t *testing.T, client typednvidiav1.ClusterPolicyInterface) {
				require.NoError(t, client.Delete(t.Context(), clusterPolicyName, deleteOptions))
			},
			expectedAction: k8stesting.NewDeleteActionWithOptions(clusterPolicyGVR, "", clusterPolicyName, deleteOptions),
		},
		{
			name: "delete collection",
			call: func(t *testing.T, client typednvidiav1.ClusterPolicyInterface) {
				require.NoError(t, client.DeleteCollection(t.Context(), deleteOptions, listOptions))
			},
			expectedAction: k8stesting.NewDeleteCollectionActionWithOptions(clusterPolicyGVR, "", deleteOptions, listOptions),
		},
		{
			name: "watch",
			call: func(t *testing.T, client typednvidiav1.ClusterPolicyInterface) {
				watcher, err := client.Watch(t.Context(), listOptions)
				require.NoError(t, err)
				watcher.Stop()
			},
			expectedAction: k8stesting.NewWatchActionWithOptions(clusterPolicyGVR, "", watchOptions),
		},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			fixture := newClusterPolicyFixture(t, newClusterPolicy(clusterPolicyName, nil))

			testCase.call(t, fixture.client)

			actions := fixture.testingFake.Actions()
			require.Len(t, actions, 1)
			assert.Equal(t, testCase.expectedAction, actions[0])
		})
	}
}

// TestClusterPoliciesListRebuildsTheList exercises the three hooks the generated
// constructor hands to gentype: getItems, setItems and copyListMeta. Every List
// rebuilds the list through them, filtered or not, so the ResourceVersion the
// tracker reports has to survive the rebuild.
func TestClusterPoliciesListRebuildsTheList(t *testing.T) {
	fixture := newClusterPolicyFixture(t,
		newClusterPolicy("cluster-policy-a", map[string]string{"tier": "gold"}),
		newClusterPolicy("cluster-policy-b", map[string]string{"tier": "silver"}),
	)
	ctx := t.Context()

	trackedObject, err := fixture.tracker.List(clusterPolicyGVR, clusterPolicyGVK, "")
	require.NoError(t, err)
	trackedList, ok := trackedObject.(*nvidiav1.ClusterPolicyList)
	require.True(t, ok, "tracker returned %T", trackedObject)
	require.NotEmpty(t, trackedList.ResourceVersion)

	allClusterPolicies, err := fixture.client.List(ctx, metav1.ListOptions{})
	require.NoError(t, err)
	assert.Equal(t, trackedList.ResourceVersion, allClusterPolicies.ResourceVersion)
	assert.Len(t, allClusterPolicies.Items, 2)

	matchingClusterPolicies, err := fixture.client.List(ctx, metav1.ListOptions{LabelSelector: "tier=gold"})
	require.NoError(t, err)
	assert.Equal(t, trackedList.ResourceVersion, matchingClusterPolicies.ResourceVersion)
	require.Len(t, matchingClusterPolicies.Items, 1)
	assert.Equal(t, "cluster-policy-a", matchingClusterPolicies.Items[0].Name)
}
