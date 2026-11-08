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

package controllers

import (
	"context"
	"time"

	"sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
)

// loggingReconciler wraps a Reconciler and writes one log line per reconcile
// with the start time, the duration, and the outcome. The line is written at
// the default verbosity.
type loggingReconciler struct {
	inner reconcile.Reconciler
}

// newLoggingReconciler returns r wrapped so that every Reconcile call is logged.
func newLoggingReconciler(r reconcile.Reconciler) reconcile.Reconciler {
	return &loggingReconciler{inner: r}
}

func (l *loggingReconciler) Reconcile(ctx context.Context, req reconcile.Request) (res reconcile.Result, err error) {
	start := time.Now()
	// Log from a defer so a panic in the inner reconciler still produces a finished
	// line. controller-runtime recovers the panic above this wrapper, so it is re-raised.
	defer func() {
		p := recover()
		outcome := "success"
		switch {
		case p != nil:
			outcome = "panic"
		case err != nil:
			outcome = "error"
		case res.RequeueAfter > 0 || res.Requeue: //nolint:staticcheck // Deprecated Requeue is still set by callers
			outcome = "requeue"
		}
		log.FromContext(ctx).Info("Reconcile finished",
			"start", start.UTC().Format(time.RFC3339Nano),
			"durationSeconds", time.Since(start).Seconds(),
			"outcome", outcome,
		)
		if p != nil {
			panic(p)
		}
	}()
	return l.inner.Reconcile(ctx, req)
}
