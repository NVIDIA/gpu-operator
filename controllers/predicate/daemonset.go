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

package predicate

import (
	"maps"
	"reflect"

	appsv1 "k8s.io/api/apps/v1"
	"k8s.io/apimachinery/pkg/util/diff"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/event"
	"sigs.k8s.io/controller-runtime/pkg/predicate"

	"github.com/NVIDIA/gpu-operator/internal/consts"
)

var daemonSetLog = ctrl.Log.WithName("predicate").WithName("DaemonSet")

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

// daemonSetUpdateRequiresReconcile reports whether a DaemonSet update can change what the
// owning reconciler does. Spec changes (generation), metadata changes including owner
// references, deletion, and a change in the readiness summary are reconciled. A status update that
// changes only pod counts, such as one more pod ready during a rollout, is dropped since the
// reconciler would read the same summary and reach the same result. While a DaemonSet is
// not ready, the reconciler also requeues every 5 seconds and reads the status again,
// independent of this predicate.
func daemonSetUpdateRequiresReconcile(oldDS, newDS *appsv1.DaemonSet) bool {
	// An unchanged resource version means the update is an informer resync or relist, not
	// a change to the object. Old and new are identical, so every comparison below would
	// drop it. Reconcile it so the periodic resync still happens.
	// See https://pkg.go.dev/sigs.k8s.io/controller-runtime/pkg/cache#Options
	if oldDS.ResourceVersion == newDS.ResourceVersion {
		return true
	}
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
// events that daemonSetUpdateRequiresReconcile accepts.
func DaemonSetWatchPredicate() predicate.TypedPredicate[*appsv1.DaemonSet] {
	return predicate.TypedFuncs[*appsv1.DaemonSet]{
		UpdateFunc: func(e event.TypedUpdateEvent[*appsv1.DaemonSet]) bool {
			reconcile := daemonSetUpdateRequiresReconcile(e.ObjectOld, e.ObjectNew)
			if reconcile && daemonSetLog.V(consts.LogLevelDebug).Enabled() {
				daemonSetLog.V(consts.LogLevelDebug).Info("DaemonSet update requires reconcile",
					"namespace", e.ObjectNew.Namespace, "name", e.ObjectNew.Name,
					"diff", diff.Diff(e.ObjectOld, e.ObjectNew))
			}
			return reconcile
		},
	}
}
