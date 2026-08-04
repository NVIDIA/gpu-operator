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

package state

import (
	"context"
	"fmt"
	"os"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/util/workqueue"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	nvidiav1alpha1 "github.com/NVIDIA/gpu-operator/api/nvidia/v1alpha1"
)

// requireMissingManifestDir skips the test when the hardcoded manifest directory
// exists: the factory error under test only occurs when it is absent (CI/dev),
// not inside an operator image where the manifests are installed.
func requireMissingManifestDir(t *testing.T, dir string) {
	t.Helper()
	if _, err := os.Stat(dir); err == nil {
		t.Skipf("manifests installed at %s; the factory would not fail here", dir)
	}
}

// fakeState is a minimal State implementation used to drive the stateManager.
type fakeState struct {
	name         string
	description  string
	syncState    SyncState
	syncErr      error
	watchSources map[string]SyncingSource
}

func (f *fakeState) Name() string        { return f.name }
func (f *fakeState) Description() string { return f.description }
func (f *fakeState) Sync(_ context.Context, _ interface{}, _ InfoCatalog) (SyncState, error) {
	return f.syncState, f.syncErr
}
func (f *fakeState) GetWatchSources(_ ctrlManager) map[string]SyncingSource {
	return f.watchSources
}

// fakeSyncingSource is a comparable SyncingSource so tests can assert which
// concrete source instance survives watch-source deduplication.
type fakeSyncingSource struct{ id string }

func (fakeSyncingSource) Start(context.Context, workqueue.TypedRateLimitingInterface[reconcile.Request]) error {
	return nil
}
func (fakeSyncingSource) WaitForSync(context.Context) error { return nil }

func TestSyncState(t *testing.T) {
	testCases := []struct {
		description    string
		states         []*fakeState
		expectedStatus SyncState
		expectErrInfo  bool
	}{
		{
			description: "all states ready aggregates to ready",
			states: []*fakeState{
				{name: "state-a", syncState: SyncStateReady},
				{name: "state-b", syncState: SyncStateReady},
			},
			expectedStatus: SyncStateReady,
		},
		{
			description: "any not-ready state aggregates to not ready",
			states: []*fakeState{
				{name: "state-a", syncState: SyncStateReady},
				{name: "state-b", syncState: SyncStateNotReady},
			},
			expectedStatus: SyncStateNotReady,
		},
		{
			description: "an errored state aggregates to not ready and records the error",
			states: []*fakeState{
				{name: "state-a", syncState: SyncStateError, syncErr: fmt.Errorf("boom")},
			},
			expectedStatus: SyncStateNotReady,
			expectErrInfo:  true,
		},
		{
			// Only NotReady and Error hold back readiness, so Ignore (returned by a
			// state after its objects are deleted) still aggregates to ready.
			description: "ignored states aggregate to ready",
			states: []*fakeState{
				{name: "state-a", syncState: SyncStateIgnore},
				{name: "state-b", syncState: SyncStateReady},
			},
			expectedStatus: SyncStateReady,
		},
		{
			// No states to hold back readiness, so the aggregate is ready.
			description:    "an empty state list aggregates to ready",
			states:         nil,
			expectedStatus: SyncStateReady,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.description, func(t *testing.T) {
			states := make([]State, len(tc.states))
			for i := range tc.states {
				states[i] = tc.states[i]
			}
			mgr := &stateManager{states: states}

			res := mgr.SyncState(context.Background(), nil, NewInfoCatalog())

			assert.Equal(t, tc.expectedStatus, res.Status)
			require.Len(t, res.StatesStatus, len(tc.states))
			for i, state := range tc.states {
				assert.Equal(t, state.name, res.StatesStatus[i].StateName)
				assert.Equal(t, state.syncState, res.StatesStatus[i].Status)
			}
			if tc.expectErrInfo {
				assert.Error(t, res.StatesStatus[0].ErrInfo)
			}
		})
	}
}

func TestGetWatchSourcesDeduplicates(t *testing.T) {
	dsFromA := fakeSyncingSource{id: "ds-from-a"}
	dsFromB := fakeSyncingSource{id: "ds-from-b"}
	cmFromB := fakeSyncingSource{id: "cm-from-b"}

	mgr := &stateManager{
		states: []State{
			&fakeState{name: "state-a", watchSources: map[string]SyncingSource{"DaemonSet": dsFromA}},
			// state-b re-advertises "DaemonSet"; for a duplicate key the first state wins.
			&fakeState{name: "state-b", watchSources: map[string]SyncingSource{"DaemonSet": dsFromB, "ConfigMap": cmFromB}},
		},
	}

	sources := mgr.GetWatchSources(nil)

	// Result order is nondeterministic (map values), so compare as a set.
	got := make(map[SyncingSource]bool, len(sources))
	for _, source := range sources {
		got[source] = true
	}
	assert.Len(t, sources, 2)
	assert.True(t, got[dsFromA], "first state's DaemonSet source should be retained")
	assert.False(t, got[dsFromB], "second state's duplicate DaemonSet source should be dropped")
	assert.True(t, got[cmFromB], "unique ConfigMap source should be retained")
}

func TestNewManagerUnsupportedCRD(t *testing.T) {
	mgr, err := NewManager("UnsupportedKind", "test-ns", nil, runtime.NewScheme())
	require.Error(t, err)
	assert.Nil(t, mgr)
	assert.Contains(t, err.Error(), "failed to add states")
}

func TestNewStatesUnsupportedCRD(t *testing.T) {
	states, err := newStates("UnsupportedKind", "test-ns", nil, runtime.NewScheme())
	require.Error(t, err)
	assert.Nil(t, states)
	assert.Contains(t, err.Error(), "unsupported CRD")
}

// TestNewStatesNVIDIADriverCase exercises the NVIDIADriver branch of newStates
// and newNVIDIADriverStates. NewStateDriver fails because the hardcoded manifest
// directory does not exist, so the error is propagated.
func TestNewStatesNVIDIADriverCase(t *testing.T) {
	requireMissingManifestDir(t, "/opt/gpu-operator/manifests/state-driver")
	states, err := newStates(nvidiav1alpha1.NVIDIADriverCRDName, "test-ns", nil, runtime.NewScheme())
	require.Error(t, err)
	assert.Nil(t, states)
	assert.Contains(t, err.Error(), "failed to create NVIDIA driver state")
}

// TestNewStatesGPUClusterCase exercises the GPUCluster dispatch branch of newStates
// and newGPUClusterStates. The first operand (DRA driver) fails on its missing
// hardcoded manifest directory, so the error is propagated.
func TestNewStatesGPUClusterCase(t *testing.T) {
	requireMissingManifestDir(t, "/opt/gpu-operator/manifests/state-dra-driver")
	states, err := newStates(nvidiav1alpha1.GPUClusterCRDName, "test-ns", nil, runtime.NewScheme())
	require.Error(t, err)
	assert.Nil(t, states)
	assert.Contains(t, err.Error(), "failed to create DRA driver state")
}

// TestNewManagerNVIDIADriverCase drives NewManager through the NVIDIADriver
// state factory (which fails on the missing manifest directory).
func TestNewManagerNVIDIADriverCase(t *testing.T) {
	requireMissingManifestDir(t, "/opt/gpu-operator/manifests/state-driver")
	mgr, err := NewManager(nvidiav1alpha1.NVIDIADriverCRDName, "test-ns", nil, runtime.NewScheme())
	require.Error(t, err)
	assert.Nil(t, mgr)
	assert.Contains(t, err.Error(), "failed to add states")
}
