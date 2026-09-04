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

// The state factories load manifests from a path hardcoded as a string literal,
// so only their failure path is reachable outside an operator image.
func skipIfManifestDirExists(t *testing.T, manifestDirPath string) {
	t.Helper()
	if _, err := os.Stat(manifestDirPath); err == nil {
		t.Skipf("manifests installed at %s; the factory would not fail here", manifestDirPath)
	}
}

type fakeState struct {
	name         string
	description  string
	syncState    SyncState
	syncErr      error
	watchSources map[string]SyncingSource
}

func (s *fakeState) Name() string        { return s.name }
func (s *fakeState) Description() string { return s.description }
func (s *fakeState) Sync(_ context.Context, _ any, _ InfoCatalog) (SyncState, error) {
	return s.syncState, s.syncErr
}
func (s *fakeState) GetWatchSources(_ ctrlManager) map[string]SyncingSource {
	return s.watchSources
}

// id keeps instances distinct under interface equality, which is what lets the
// deduplication test tell a retained source from a dropped one.
type fakeSyncingSource struct{ id string }

func (fakeSyncingSource) Start(context.Context, workqueue.TypedRateLimitingInterface[reconcile.Request]) error {
	return nil
}
func (fakeSyncingSource) WaitForSync(context.Context) error { return nil }

func TestSyncState(t *testing.T) {
	syncFailure := fmt.Errorf("sync failed")

	testCases := []struct {
		description    string
		states         []*fakeState
		expectedStatus SyncState
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
				{name: "state-a", syncState: SyncStateError, syncErr: syncFailure},
			},
			expectedStatus: SyncStateNotReady,
		},
		{
			description: "ignored states aggregate to ready",
			states: []*fakeState{
				{name: "state-a", syncState: SyncStateIgnore},
				{name: "state-b", syncState: SyncStateReady},
			},
			expectedStatus: SyncStateReady,
		},
		{
			description:    "an empty state list aggregates to ready",
			states:         nil,
			expectedStatus: SyncStateReady,
		},
	}

	for _, testCase := range testCases {
		t.Run(testCase.description, func(t *testing.T) {
			states := make([]State, len(testCase.states))
			for i, state := range testCase.states {
				states[i] = state
			}
			manager := &stateManager{states: states}

			results := manager.SyncState(context.Background(), nil, NewInfoCatalog())

			assert.Equal(t, testCase.expectedStatus, results.Status)
			require.Len(t, results.StatesStatus, len(testCase.states))
			for i, state := range testCase.states {
				assert.Equal(t, state.name, results.StatesStatus[i].StateName)
				assert.Equal(t, state.syncState, results.StatesStatus[i].Status)
				if state.syncErr != nil {
					assert.ErrorIs(t, results.StatesStatus[i].ErrInfo, state.syncErr)
				} else {
					assert.NoError(t, results.StatesStatus[i].ErrInfo)
				}
			}
		})
	}
}

func TestGetWatchSourcesDeduplicates(t *testing.T) {
	daemonSetFromFirstState := fakeSyncingSource{id: "daemonset-from-first-state"}
	daemonSetFromSecondState := fakeSyncingSource{id: "daemonset-from-second-state"}
	configMapFromSecondState := fakeSyncingSource{id: "configmap-from-second-state"}

	manager := &stateManager{
		states: []State{
			&fakeState{
				name:         "state-a",
				watchSources: map[string]SyncingSource{"DaemonSet": daemonSetFromFirstState},
			},
			&fakeState{
				name: "state-b",
				watchSources: map[string]SyncingSource{
					"DaemonSet": daemonSetFromSecondState,
					"ConfigMap": configMapFromSecondState,
				},
			},
		},
	}

	gotSources := manager.GetWatchSources(nil)

	// GetWatchSources returns map values, so the result order is nondeterministic.
	expectedSources := []SyncingSource{daemonSetFromFirstState, configMapFromSecondState}
	assert.ElementsMatch(t, expectedSources, gotSources)
}

func TestNewManagerUnsupportedCRD(t *testing.T) {
	manager, err := NewManager("UnsupportedKind", "test-ns", nil, runtime.NewScheme())
	require.Error(t, err)
	assert.Nil(t, manager)
	assert.Contains(t, err.Error(), "failed to add states: unsupported CRD for state manager factory: UnsupportedKind")
}

func TestNewStatesUnsupportedCRD(t *testing.T) {
	states, err := newStates("UnsupportedKind", "test-ns", nil, runtime.NewScheme())
	require.Error(t, err)
	assert.Nil(t, states)
	assert.Contains(t, err.Error(), "unsupported CRD for state manager factory: UnsupportedKind")
}

func TestNewStatesNVIDIADriver(t *testing.T) {
	skipIfManifestDirExists(t, "/opt/gpu-operator/manifests/state-driver")

	states, err := newStates(nvidiav1alpha1.NVIDIADriverCRDName, "test-ns", nil, runtime.NewScheme())
	require.Error(t, err)
	assert.Nil(t, states)
	assert.Contains(t, err.Error(), "failed to create NVIDIA driver state")
}

func TestNewStatesGPUCluster(t *testing.T) {
	skipIfManifestDirExists(t, "/opt/gpu-operator/manifests/state-dra-driver")

	states, err := newStates(nvidiav1alpha1.GPUClusterCRDName, "test-ns", nil, runtime.NewScheme())
	require.Error(t, err)
	assert.Nil(t, states)
	assert.Contains(t, err.Error(), "failed to create DRA driver state")
}

func TestNewManagerNVIDIADriver(t *testing.T) {
	skipIfManifestDirExists(t, "/opt/gpu-operator/manifests/state-driver")

	manager, err := NewManager(nvidiav1alpha1.NVIDIADriverCRDName, "test-ns", nil, runtime.NewScheme())
	require.Error(t, err)
	assert.Nil(t, manager)
	assert.Contains(t, err.Error(), "failed to add states: failed to create NVIDIA driver state")
}
