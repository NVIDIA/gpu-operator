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

package validator

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/client/interceptor"

	nvidiav1alpha1 "github.com/NVIDIA/gpu-operator/api/nvidia/v1alpha1"
	"github.com/NVIDIA/gpu-operator/internal/consts"
)

func makeSelectorTestNode(name string, labels map[string]string) *corev1.Node {
	return &corev1.Node{ObjectMeta: metav1.ObjectMeta{Name: name, Labels: labels}}
}

func TestNVIDIADriverNodeSelectorConflictModes(t *testing.T) {
	deleting := makeTestDriver("deleting", map[string]string{"pool": "a"}, false)
	deleting.DeletionTimestamp = new(metav1.Now())
	deleting.Finalizers = []string{"test-finalizer"}

	tests := map[string]struct {
		incoming         *nvidiav1alpha1.NVIDIADriver
		existing         []*nvidiav1alpha1.NVIDIADriver
		nodes            []*corev1.Node
		wantPotentialErr error
		wantCurrentErr   error
		wantOtherName    string
		wantNodeName     string
	}{
		"same selector without current nodes": {
			incoming:         makeTestDriver("incoming", map[string]string{"pool": "a"}, false),
			existing:         []*nvidiav1alpha1.NVIDIADriver{makeTestDriver("existing", map[string]string{"pool": "a"}, false)},
			wantPotentialErr: ErrConflictingNVIDIADriverNodeSelectors,
			wantOtherName:    "existing",
		},
		"compatible selectors without current nodes": {
			incoming:         makeTestDriver("incoming", map[string]string{"pool": "a"}, false),
			existing:         []*nvidiav1alpha1.NVIDIADriver{makeTestDriver("existing", map[string]string{"region": "east"}, false)},
			wantPotentialErr: ErrConflictingNVIDIADriverNodeSelectors,
			wantOtherName:    "existing",
		},
		"same selector on a current GPU node": {
			incoming:         makeTestDriver("incoming", map[string]string{"pool": "a"}, false),
			existing:         []*nvidiav1alpha1.NVIDIADriver{makeTestDriver("existing", map[string]string{"pool": "a"}, false)},
			nodes:            []*corev1.Node{makeSelectorTestNode("gpu-a", map[string]string{consts.GPUPresentLabel: "true", "pool": "a"})},
			wantPotentialErr: ErrConflictingNVIDIADriverNodeSelectors,
			wantCurrentErr:   ErrConflictingNVIDIADriverNodeSelectors,
			wantOtherName:    "existing",
			wantNodeName:     "gpu-a",
		},
		"compatible selectors on a current GPU node": {
			incoming:         makeTestDriver("incoming", map[string]string{"pool": "a"}, false),
			existing:         []*nvidiav1alpha1.NVIDIADriver{makeTestDriver("existing", map[string]string{"region": "east"}, false)},
			nodes:            []*corev1.Node{makeSelectorTestNode("gpu-a", map[string]string{consts.GPUPresentLabel: "true", "pool": "a", "region": "east"})},
			wantPotentialErr: ErrConflictingNVIDIADriverNodeSelectors,
			wantCurrentErr:   ErrConflictingNVIDIADriverNodeSelectors,
			wantOtherName:    "existing",
			wantNodeName:     "gpu-a",
		},
		"compatible selectors select different current nodes": {
			incoming: makeTestDriver("incoming", map[string]string{"pool": "a"}, false),
			existing: []*nvidiav1alpha1.NVIDIADriver{makeTestDriver("existing", map[string]string{"region": "east"}, false)},
			nodes: []*corev1.Node{
				makeSelectorTestNode("gpu-a", map[string]string{consts.GPUPresentLabel: "true", "pool": "a", "region": "west"}),
				makeSelectorTestNode("gpu-b", map[string]string{consts.GPUPresentLabel: "true", "pool": "b", "region": "east"}),
			},
			wantPotentialErr: ErrConflictingNVIDIADriverNodeSelectors,
			wantOtherName:    "existing",
		},
		"contradictory selectors": {
			incoming: makeTestDriver("incoming", map[string]string{"pool": "a"}, false),
			existing: []*nvidiav1alpha1.NVIDIADriver{makeTestDriver("existing", map[string]string{"pool": "b"}, false)},
			nodes:    []*corev1.Node{makeSelectorTestNode("gpu-a", map[string]string{consts.GPUPresentLabel: "true", "pool": "a"})},
		},
		"checks every existing driver": {
			incoming: makeTestDriver("incoming", map[string]string{"pool": "a"}, false),
			existing: []*nvidiav1alpha1.NVIDIADriver{
				makeTestDriver("disjoint", map[string]string{"pool": "b"}, false),
				makeTestDriver("conflicting", map[string]string{"region": "east"}, false),
			},
			nodes:            []*corev1.Node{makeSelectorTestNode("gpu-a", map[string]string{consts.GPUPresentLabel: "true", "pool": "a", "region": "east"})},
			wantPotentialErr: ErrConflictingNVIDIADriverNodeSelectors,
			wantCurrentErr:   ErrConflictingNVIDIADriverNodeSelectors,
			wantOtherName:    "conflicting",
			wantNodeName:     "gpu-a",
		},
		"update does not conflict with itself": {
			incoming: makeTestDriver("existing", map[string]string{"pool": "a"}, false),
			existing: []*nvidiav1alpha1.NVIDIADriver{makeTestDriver("existing", map[string]string{"pool": "a"}, false)},
			nodes:    []*corev1.Node{makeSelectorTestNode("gpu-a", map[string]string{consts.GPUPresentLabel: "true", "pool": "a"})},
		},
		"update still checks another driver": {
			incoming: makeTestDriver("incoming", map[string]string{"pool": "a"}, false),
			existing: []*nvidiav1alpha1.NVIDIADriver{
				makeTestDriver("incoming", map[string]string{"pool": "b"}, false),
				makeTestDriver("existing", map[string]string{"pool": "a"}, false),
			},
			nodes:            []*corev1.Node{makeSelectorTestNode("gpu-a", map[string]string{consts.GPUPresentLabel: "true", "pool": "a"})},
			wantPotentialErr: ErrConflictingNVIDIADriverNodeSelectors,
			wantCurrentErr:   ErrConflictingNVIDIADriverNodeSelectors,
			wantOtherName:    "existing",
			wantNodeName:     "gpu-a",
		},
		"incoming default is a fallback": {
			incoming: makeTestDriver("incoming", nil, true),
			existing: []*nvidiav1alpha1.NVIDIADriver{makeTestDriver("existing", map[string]string{"pool": "a"}, false)},
			nodes:    []*corev1.Node{makeSelectorTestNode("gpu-a", map[string]string{consts.GPUPresentLabel: "true", "pool": "a"})},
		},
		"existing default is a fallback": {
			incoming: makeTestDriver("incoming", map[string]string{"pool": "a"}, false),
			existing: []*nvidiav1alpha1.NVIDIADriver{makeTestDriver("existing", nil, true)},
			nodes:    []*corev1.Node{makeSelectorTestNode("gpu-a", map[string]string{consts.GPUPresentLabel: "true", "pool": "a"})},
		},
		"second default without persisted incoming driver": {
			incoming:         makeTestDriver("incoming", nil, true),
			existing:         []*nvidiav1alpha1.NVIDIADriver{makeTestDriver("existing", nil, true)},
			wantPotentialErr: ErrMultipleDefaultNVIDIADrivers,
			wantCurrentErr:   ErrMultipleDefaultNVIDIADrivers,
			wantOtherName:    "existing",
		},
		"deleting driver is ignored": {
			incoming: makeTestDriver("incoming", map[string]string{"pool": "a"}, false),
			existing: []*nvidiav1alpha1.NVIDIADriver{deleting},
			nodes:    []*corev1.Node{makeSelectorTestNode("gpu-a", map[string]string{consts.GPUPresentLabel: "true", "pool": "a"})},
		},
		"missing selector targets GPU nodes": {
			incoming:         makeTestDriver("incoming", nil, false),
			existing:         []*nvidiav1alpha1.NVIDIADriver{makeTestDriver("existing", map[string]string{"pool": "a"}, false)},
			nodes:            []*corev1.Node{makeSelectorTestNode("gpu-a", map[string]string{consts.GPUPresentLabel: "true", "pool": "a"})},
			wantPotentialErr: ErrConflictingNVIDIADriverNodeSelectors,
			wantCurrentErr:   ErrConflictingNVIDIADriverNodeSelectors,
			wantOtherName:    "existing",
			wantNodeName:     "gpu-a",
		},
		"empty selector targets GPU nodes": {
			incoming:         makeTestDriver("incoming", map[string]string{}, false),
			existing:         []*nvidiav1alpha1.NVIDIADriver{makeTestDriver("existing", map[string]string{"pool": "a"}, false)},
			nodes:            []*corev1.Node{makeSelectorTestNode("gpu-a", map[string]string{consts.GPUPresentLabel: "true", "pool": "a"})},
			wantPotentialErr: ErrConflictingNVIDIADriverNodeSelectors,
			wantCurrentErr:   ErrConflictingNVIDIADriverNodeSelectors,
			wantOtherName:    "existing",
			wantNodeName:     "gpu-a",
		},
		"non-GPU nodes do not cause current conflicts": {
			incoming:         makeTestDriver("incoming", map[string]string{"pool": "a"}, false),
			existing:         []*nvidiav1alpha1.NVIDIADriver{makeTestDriver("existing", map[string]string{"pool": "a"}, false)},
			nodes:            []*corev1.Node{makeSelectorTestNode("cpu-a", map[string]string{consts.GPUPresentLabel: "false", "pool": "a"})},
			wantPotentialErr: ErrConflictingNVIDIADriverNodeSelectors,
			wantOtherName:    "existing",
		},
		"selector excluding GPU nodes has no target": {
			incoming: makeTestDriver("incoming", map[string]string{consts.GPUPresentLabel: "false"}, false),
			existing: []*nvidiav1alpha1.NVIDIADriver{makeTestDriver("existing", map[string]string{"pool": "a"}, false)},
			nodes:    []*corev1.Node{makeSelectorTestNode("gpu-a", map[string]string{consts.GPUPresentLabel: "true", "pool": "a"})},
		},
	}

	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			objects := make([]client.Object, 0, len(tc.existing)+len(tc.nodes))
			for _, driver := range tc.existing {
				objects = append(objects, driver)
			}
			for _, node := range tc.nodes {
				objects = append(objects, node)
			}
			c := fake.NewClientBuilder().WithScheme(newValidatorScheme(t)).WithObjects(objects...).Build()

			checks := []struct {
				name     string
				validate func(context.Context, client.Reader, *nvidiav1alpha1.NVIDIADriver) error
				wantErr  error
				wantNode string
			}{
				{"potential", ValidatePotentialNVIDIADriverNodeSelectorConflicts, tc.wantPotentialErr, ""},
				{"existing nodes", ValidateNVIDIADriverNodeSelectorConflictsOnExistingNodes, tc.wantCurrentErr, tc.wantNodeName},
			}
			for _, check := range checks {
				t.Run(check.name, func(t *testing.T) {
					err := check.validate(context.Background(), c, tc.incoming)
					if check.wantErr == nil {
						require.NoError(t, err)
						return
					}
					require.ErrorIs(t, err, check.wantErr)
					require.Contains(t, err.Error(), tc.incoming.Name)
					require.Contains(t, err.Error(), tc.wantOtherName)
					if check.wantNode != "" {
						require.Contains(t, err.Error(), check.wantNode)
					}
				})
			}
		})
	}
}

func TestNVIDIADriverNodeSelectorConflictModesRejectInvalidIncomingSelector(t *testing.T) {
	incoming := makeTestDriver("incoming", map[string]string{consts.NVIDIADriverOwnerLabel: "other"}, false)
	c := fake.NewClientBuilder().WithScheme(newValidatorScheme(t)).Build()

	for _, validate := range []func(context.Context, client.Reader, *nvidiav1alpha1.NVIDIADriver) error{
		ValidatePotentialNVIDIADriverNodeSelectorConflicts,
		ValidateNVIDIADriverNodeSelectorConflictsOnExistingNodes,
	} {
		err := validate(context.Background(), c, incoming)
		require.ErrorContains(t, err, "reserved label")
	}
}

func TestNVIDIADriverNodeSelectorConflictModesReturnDriverListError(t *testing.T) {
	c := fake.NewClientBuilder().
		WithScheme(newValidatorScheme(t)).
		WithInterceptorFuncs(interceptor.Funcs{
			List: func(ctx context.Context, cl client.WithWatch, list client.ObjectList, opts ...client.ListOption) error {
				return errors.New("list failed")
			},
		}).
		Build()

	for _, validate := range []func(context.Context, client.Reader, *nvidiav1alpha1.NVIDIADriver) error{
		ValidatePotentialNVIDIADriverNodeSelectorConflicts,
		ValidateNVIDIADriverNodeSelectorConflictsOnExistingNodes,
	} {
		err := validate(context.Background(), c, makeTestDriver("incoming", nil, false))
		require.ErrorContains(t, err, "list failed")
	}
}

func TestExistingNodeNVIDIADriverSelectorConflictsReturnNodeListError(t *testing.T) {
	existing := makeTestDriver("existing", map[string]string{"pool": "a"}, false)
	c := fake.NewClientBuilder().
		WithScheme(newValidatorScheme(t)).
		WithObjects(existing).
		WithInterceptorFuncs(interceptor.Funcs{
			List: func(ctx context.Context, cl client.WithWatch, list client.ObjectList, opts ...client.ListOption) error {
				if _, ok := list.(*corev1.NodeList); ok {
					return errors.New("node list failed")
				}
				return cl.List(ctx, list, opts...)
			},
		}).
		Build()

	err := ValidateNVIDIADriverNodeSelectorConflictsOnExistingNodes(context.Background(), c, makeTestDriver("incoming", map[string]string{"pool": "a"}, false))
	require.ErrorContains(t, err, "node list failed")
}

func TestExistingNodeNVIDIADriverSelectorConflictsSkipNodeListForDisjointSelectors(t *testing.T) {
	existing := makeTestDriver("existing", map[string]string{"pool": "b"}, false)
	nodeListCalls := 0
	c := fake.NewClientBuilder().
		WithScheme(newValidatorScheme(t)).
		WithObjects(existing).
		WithInterceptorFuncs(interceptor.Funcs{
			List: func(ctx context.Context, cl client.WithWatch, list client.ObjectList, opts ...client.ListOption) error {
				if _, ok := list.(*corev1.NodeList); ok {
					nodeListCalls++
					return errors.New("node list should not be called")
				}
				return cl.List(ctx, list, opts...)
			},
		}).
		Build()

	err := ValidateNVIDIADriverNodeSelectorConflictsOnExistingNodes(context.Background(), c, makeTestDriver("incoming", map[string]string{"pool": "a"}, false))
	require.NoError(t, err)
	require.Zero(t, nodeListCalls)
}
