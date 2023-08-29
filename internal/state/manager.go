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

	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"

	nvidiav1alpha1 "github.com/NVIDIA/gpu-operator/api/v1alpha1"
)

type Manager interface {
	GetWatchSources(ctrlManager) []SyncingSource
	SyncState(ctx context.Context, customResource interface{}, infoCatalog InfoCatalog) Results
}

type stateManager struct {
	states []State
	client client.Client
}

var _ Manager = (*stateManager)(nil)

func NewManager(crdKind string, k8sClient client.Client, scheme *runtime.Scheme) (Manager, error) {
	states, err := newStates(crdKind, k8sClient, scheme)
	if err != nil {
		return nil, fmt.Errorf("failed to add states: %v", err)
	}

	manager := &stateManager{
		states: states,
		client: k8sClient,
	}
	return manager, nil
}

func (m *stateManager) GetWatchSources(ctrlManager ctrlManager) []SyncingSource {
	sourceMap := make(map[string]SyncingSource)
	for _, state := range m.states {
		wr := state.GetWatchSources(ctrlManager)
		// append to kindMap
		for name, source := range wr {
			if _, ok := sourceMap[name]; !ok {
				sourceMap[name] = source
			}
		}
	}

	sources := make([]SyncingSource, 0, len(sourceMap))
	for _, source := range sourceMap {
		sources = append(sources, source)
	}
	return sources
}

func (m *stateManager) SyncState(ctx context.Context, customResource interface{}, infoCatalog InfoCatalog) Results {
	return Results{}
}

func newStates(crdKind string, k8sClient client.Client, scheme *runtime.Scheme) ([]State, error) {
	switch crdKind {
	case nvidiav1alpha1.NVIDIADriverCRDName:
		return newNVIDIADriverStates(k8sClient, scheme)
	default:
		break
	}
	return nil, fmt.Errorf("unsupported CRD for state manager factory: %s", crdKind)
}

func newNVIDIADriverStates(k8sClient client.Client, scheme *runtime.Scheme) ([]State, error) {
	driverState, err := NewStateDriver(k8sClient, scheme, "/opt/gpu-operator/manifests/state-driver")
	if err != nil {
		return nil, fmt.Errorf("failed to create NVIDIA driver state: %v", err)
	}

	return []State{driverState}, nil
}
