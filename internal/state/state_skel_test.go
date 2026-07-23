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
	"testing"

	"github.com/go-logr/logr"
	"github.com/stretchr/testify/require"
	appsv1 "k8s.io/api/apps/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
)

func toUnstructuredDaemonSet(t *testing.T, ds *appsv1.DaemonSet) *unstructured.Unstructured {
	t.Helper()
	obj, err := runtime.DefaultUnstructuredConverter.ToUnstructured(ds)
	require.NoError(t, err)
	return &unstructured.Unstructured{Object: obj}
}

func TestIsDaemonSetReady(t *testing.T) {
	testCases := []struct {
		name     string
		ds       *appsv1.DaemonSet
		expected bool
	}{
		{
			// A mode-gated DaemonSet on a cluster where no node selects this stack.
			name: "zero desired pods after the DaemonSet controller processed the object",
			ds: &appsv1.DaemonSet{
				ObjectMeta: metav1.ObjectMeta{Generation: 1},
				Status:     appsv1.DaemonSetStatus{ObservedGeneration: 1},
			},
			expected: true,
		},
		{
			name: "zero desired pods but the DaemonSet controller has not processed the object yet",
			ds: &appsv1.DaemonSet{
				ObjectMeta: metav1.ObjectMeta{Generation: 1},
				Status:     appsv1.DaemonSetStatus{ObservedGeneration: 0},
			},
			expected: false,
		},
		{
			name: "zero desired pods with a misscheduled pod still running",
			ds: &appsv1.DaemonSet{
				ObjectMeta: metav1.ObjectMeta{Generation: 1},
				Status: appsv1.DaemonSetStatus{
					ObservedGeneration: 1,
					NumberMisscheduled: 1,
				},
			},
			expected: false,
		},
		{
			name: "zero desired pods with a pod still scheduled (draining)",
			ds: &appsv1.DaemonSet{
				ObjectMeta: metav1.ObjectMeta{Generation: 1},
				Status: appsv1.DaemonSetStatus{
					ObservedGeneration:     1,
					CurrentNumberScheduled: 1,
				},
			},
			expected: false,
		},
		{
			name: "all desired pods available and updated",
			ds: &appsv1.DaemonSet{
				ObjectMeta: metav1.ObjectMeta{Generation: 2},
				Status: appsv1.DaemonSetStatus{
					ObservedGeneration:     2,
					DesiredNumberScheduled: 2,
					CurrentNumberScheduled: 2,
					NumberAvailable:        2,
					UpdatedNumberScheduled: 2,
				},
			},
			expected: true,
		},
		{
			name: "desired pods not yet available",
			ds: &appsv1.DaemonSet{
				ObjectMeta: metav1.ObjectMeta{Generation: 1},
				Status: appsv1.DaemonSetStatus{
					ObservedGeneration:     1,
					DesiredNumberScheduled: 1,
					CurrentNumberScheduled: 1,
					NumberAvailable:        0,
				},
			},
			expected: false,
		},
	}

	s := &stateSkel{}
	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			ready, err := s.isDaemonSetReady(toUnstructuredDaemonSet(t, tc.ds), logr.Discard())
			require.NoError(t, err)
			require.Equal(t, tc.expected, ready)
		})
	}
}
