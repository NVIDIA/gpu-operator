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

	"github.com/stretchr/testify/require"
	appsv1 "k8s.io/api/apps/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/event"
)

func TestDaemonSetUpdateRequiresReconcile(t *testing.T) {
	// A RollingUpdate DaemonSet in the middle of a rollout on a 10-node fleet.
	rolling := func() *appsv1.DaemonSet {
		return &appsv1.DaemonSet{
			ObjectMeta: metav1.ObjectMeta{
				Name:        "nvidia-device-plugin-daemonset",
				Generation:  3,
				Labels:      map[string]string{"app": "nvidia-device-plugin-daemonset"},
				Annotations: map[string]string{"nvidia.com/last-applied-hash": "abc"},
			},
			Status: appsv1.DaemonSetStatus{
				ObservedGeneration:     3,
				DesiredNumberScheduled: 10,
				CurrentNumberScheduled: 10,
				UpdatedNumberScheduled: 10,
				NumberAvailable:        6,
				NumberReady:            6,
				NumberUnavailable:      4,
			},
		}
	}
	// The same DaemonSet fully rolled out.
	ready := func() *appsv1.DaemonSet {
		ds := rolling()
		ds.Status.NumberAvailable = 10
		ds.Status.NumberReady = 10
		ds.Status.NumberUnavailable = 0
		return ds
	}

	tests := []struct {
		name     string
		oldDS    *appsv1.DaemonSet
		newDS    *appsv1.DaemonSet
		expected bool
	}{
		{
			name:     "identical objects",
			oldDS:    rolling(),
			newDS:    rolling(),
			expected: false,
		},
		{
			name:  "one more pod ready during a rollout",
			oldDS: rolling(),
			newDS: func() *appsv1.DaemonSet {
				ds := rolling()
				ds.Status.NumberAvailable = 7
				ds.Status.NumberReady = 7
				ds.Status.NumberUnavailable = 3
				return ds
			}(),
			expected: false,
		},
		{
			name:  "one pod becomes unready while others are already unready",
			oldDS: rolling(),
			newDS: func() *appsv1.DaemonSet {
				ds := rolling()
				ds.Status.NumberAvailable = 5
				ds.Status.NumberReady = 5
				ds.Status.NumberUnavailable = 5
				return ds
			}(),
			expected: false,
		},
		{
			name:     "last pod becomes ready",
			oldDS:    rolling(),
			newDS:    ready(),
			expected: true,
		},
		{
			name:     "a ready DaemonSet loses one pod",
			oldDS:    ready(),
			newDS:    rolling(),
			expected: true,
		},
		{
			name:  "spec change bumps the generation",
			oldDS: ready(),
			newDS: func() *appsv1.DaemonSet {
				ds := ready()
				ds.Generation = 4
				return ds
			}(),
			expected: true,
		},
		{
			name: "DaemonSet controller observes the new generation",
			oldDS: func() *appsv1.DaemonSet {
				ds := ready()
				ds.Generation = 4
				return ds
			}(),
			newDS: func() *appsv1.DaemonSet {
				ds := ready()
				ds.Generation = 4
				ds.Status.ObservedGeneration = 4
				return ds
			}(),
			expected: true,
		},
		{
			name:  "a node joins and the new pod is not yet available",
			oldDS: ready(),
			newDS: func() *appsv1.DaemonSet {
				ds := ready()
				ds.Status.DesiredNumberScheduled = 11
				ds.Status.CurrentNumberScheduled = 11
				ds.Status.UpdatedNumberScheduled = 11
				ds.Status.NumberUnavailable = 1
				return ds
			}(),
			expected: true,
		},
		{
			name:  "a node joins and its pod is available at once",
			oldDS: ready(),
			newDS: func() *appsv1.DaemonSet {
				ds := ready()
				ds.Status.DesiredNumberScheduled = 11
				ds.Status.CurrentNumberScheduled = 11
				ds.Status.UpdatedNumberScheduled = 11
				ds.Status.NumberAvailable = 11
				ds.Status.NumberReady = 11
				return ds
			}(),
			expected: false,
		},
		{
			name:  "desired count drops to zero",
			oldDS: ready(),
			newDS: func() *appsv1.DaemonSet {
				ds := ready()
				ds.Status.DesiredNumberScheduled = 0
				ds.Status.CurrentNumberScheduled = 0
				ds.Status.UpdatedNumberScheduled = 0
				ds.Status.NumberAvailable = 0
				ds.Status.NumberReady = 0
				return ds
			}(),
			expected: true,
		},
		{
			name: "a stale pod remains after desired count drops to zero, then goes away",
			oldDS: func() *appsv1.DaemonSet {
				ds := ready()
				ds.Status.DesiredNumberScheduled = 0
				ds.Status.CurrentNumberScheduled = 1
				ds.Status.UpdatedNumberScheduled = 0
				ds.Status.NumberAvailable = 0
				ds.Status.NumberReady = 0
				return ds
			}(),
			newDS: func() *appsv1.DaemonSet {
				ds := ready()
				ds.Status.DesiredNumberScheduled = 0
				ds.Status.CurrentNumberScheduled = 0
				ds.Status.UpdatedNumberScheduled = 0
				ds.Status.NumberAvailable = 0
				ds.Status.NumberReady = 0
				return ds
			}(),
			expected: true,
		},
		{
			name:  "a pod is misscheduled",
			oldDS: ready(),
			newDS: func() *appsv1.DaemonSet {
				ds := ready()
				ds.Status.NumberMisscheduled = 1
				return ds
			}(),
			expected: true,
		},
		{
			name:  "annotation changes",
			oldDS: ready(),
			newDS: func() *appsv1.DaemonSet {
				ds := ready()
				ds.Annotations["nvidia.com/last-applied-hash"] = "def"
				return ds
			}(),
			expected: true,
		},
		{
			name:  "label changes",
			oldDS: ready(),
			newDS: func() *appsv1.DaemonSet {
				ds := ready()
				ds.Labels["extra"] = "value"
				return ds
			}(),
			expected: true,
		},
		{
			name:  "owner reference is added",
			oldDS: ready(),
			newDS: func() *appsv1.DaemonSet {
				ds := ready()
				ds.OwnerReferences = []metav1.OwnerReference{{
					APIVersion: "nvidia.com/v1",
					Kind:       "ClusterPolicy",
					Name:       "cluster-policy",
					Controller: new(true),
				}}
				return ds
			}(),
			expected: true,
		},
		{
			name:  "deletion starts",
			oldDS: ready(),
			newDS: func() *appsv1.DaemonSet {
				ds := ready()
				now := metav1.Now()
				ds.DeletionTimestamp = &now
				return ds
			}(),
			expected: true,
		},
		{
			name:  "resource version only",
			oldDS: ready(),
			newDS: func() *appsv1.DaemonSet {
				ds := ready()
				ds.ResourceVersion = "12345"
				return ds
			}(),
			expected: false,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			require.Equal(t, tc.expected, DaemonSetUpdateRequiresReconcile(tc.oldDS, tc.newDS))
		})
	}
}

func TestDaemonSetWatchPredicatePassesCreateAndDelete(t *testing.T) {
	p := DaemonSetWatchPredicate()
	ds := &appsv1.DaemonSet{ObjectMeta: metav1.ObjectMeta{Name: "ds"}}
	require.True(t, p.Create(event.TypedCreateEvent[*appsv1.DaemonSet]{Object: ds}))
	require.True(t, p.Delete(event.TypedDeleteEvent[*appsv1.DaemonSet]{Object: ds}))
	require.True(t, p.Generic(event.TypedGenericEvent[*appsv1.DaemonSet]{Object: ds}))
	require.False(t, p.Update(event.TypedUpdateEvent[*appsv1.DaemonSet]{ObjectOld: ds, ObjectNew: ds.DeepCopy()}))
}
