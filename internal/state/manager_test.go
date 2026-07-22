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
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"k8s.io/apimachinery/pkg/runtime"

	nvidiav1alpha1 "github.com/NVIDIA/gpu-operator/api/nvidia/v1alpha1"
)

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
			// Each per-state result must reflect that state's name and status.
			for i, s := range tc.states {
				assert.Equal(t, s.name, res.StatesStatus[i].StateName)
				assert.Equal(t, s.syncState, res.StatesStatus[i].Status)
			}
			if tc.expectErrInfo {
				assert.Error(t, res.StatesStatus[0].ErrInfo)
			}
		})
	}
}

func TestGetWatchSourcesDeduplicates(t *testing.T) {
	mgr := &stateManager{
		states: []State{
			&fakeState{name: "state-a", watchSources: map[string]SyncingSource{"DaemonSet": nil}},
			// second state advertises the same source name, which must be deduplicated
			&fakeState{name: "state-b", watchSources: map[string]SyncingSource{"DaemonSet": nil, "ConfigMap": nil}},
		},
	}

	sources := mgr.GetWatchSources(nil)
	assert.Len(t, sources, 2)
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
	states, err := newStates(nvidiav1alpha1.NVIDIADriverCRDName, "test-ns", nil, runtime.NewScheme())
	require.Error(t, err)
	assert.Nil(t, states)
	assert.Contains(t, err.Error(), "failed to create NVIDIA driver state")
}

// TestNewManagerNVIDIADriverCase drives NewManager through the NVIDIADriver
// state factory (which fails on the missing manifest directory).
func TestNewManagerNVIDIADriverCase(t *testing.T) {
	mgr, err := NewManager(nvidiav1alpha1.NVIDIADriverCRDName, "test-ns", nil, runtime.NewScheme())
	require.Error(t, err)
	assert.Nil(t, mgr)
	assert.Contains(t, err.Error(), "failed to add states")
}
