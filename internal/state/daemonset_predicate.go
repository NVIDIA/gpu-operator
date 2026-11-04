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
	"maps"
	"reflect"

	appsv1 "k8s.io/api/apps/v1"
	"sigs.k8s.io/controller-runtime/pkg/event"
	"sigs.k8s.io/controller-runtime/pkg/predicate"
)

// daemonSetReadinessSummary holds the comparisons that the ClusterPolicy and GPUCluster
// reconcilers make on DaemonSet status when they decide whether an operand is ready or
// needs cleanup. The reconcilers never read a raw pod count. They read only these
// comparisons. If a status update changes the counts but none of these comparisons, the
// reconciler would reach the same result as before.
type daemonSetReadinessSummary struct {
	generationObserved bool
	desiredIsZero      bool
	currentIsZero      bool
	unavailableIsZero  bool
	misscheduledIsZero bool
	allUpdated         bool
	allAvailable       bool
}

func summarizeDaemonSetReadiness(ds *appsv1.DaemonSet) daemonSetReadinessSummary {
	s := ds.Status
	return daemonSetReadinessSummary{
		generationObserved: s.ObservedGeneration >= ds.Generation,
		desiredIsZero:      s.DesiredNumberScheduled == 0,
		currentIsZero:      s.CurrentNumberScheduled == 0,
		unavailableIsZero:  s.NumberUnavailable == 0,
		misscheduledIsZero: s.NumberMisscheduled == 0,
		allUpdated:         s.UpdatedNumberScheduled == s.DesiredNumberScheduled,
		allAvailable:       s.NumberAvailable == s.DesiredNumberScheduled,
	}
}

// DaemonSetUpdateRequiresReconcile reports whether a DaemonSet update can change what the
// owning reconciler does. Spec changes (generation), metadata changes including owner
// references, deletion, and a change in the readiness summary pass. A pod count that moves without changing the
// summary, such as one more pod ready during a rollout, does not pass; the not-ready
// requeue still polls those DaemonSets.
func DaemonSetUpdateRequiresReconcile(oldDS, newDS *appsv1.DaemonSet) bool {
	if oldDS.Generation != newDS.Generation {
		return true
	}
	if oldDS.DeletionTimestamp.IsZero() != newDS.DeletionTimestamp.IsZero() {
		return true
	}
	if !maps.Equal(oldDS.Labels, newDS.Labels) || !maps.Equal(oldDS.Annotations, newDS.Annotations) {
		return true
	}
	if !reflect.DeepEqual(oldDS.OwnerReferences, newDS.OwnerReferences) {
		return true
	}
	return summarizeDaemonSetReadiness(oldDS) != summarizeDaemonSetReadiness(newDS)
}

// DaemonSetWatchPredicate passes create, delete, and generic events, and only the update
// events that DaemonSetUpdateRequiresReconcile accepts.
func DaemonSetWatchPredicate() predicate.TypedPredicate[*appsv1.DaemonSet] {
	return predicate.TypedFuncs[*appsv1.DaemonSet]{
		UpdateFunc: func(e event.TypedUpdateEvent[*appsv1.DaemonSet]) bool {
			return DaemonSetUpdateRequiresReconcile(e.ObjectOld, e.ObjectNew)
		},
	}
}
