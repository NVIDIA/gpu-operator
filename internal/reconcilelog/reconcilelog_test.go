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

package reconcilelog

import (
	"context"
	"errors"
	"regexp"
	"strconv"
	"testing"
	"time"

	"github.com/go-logr/logr/funcr"
	"github.com/stretchr/testify/require"
	"sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
)

type fakeReconciler struct {
	res reconcile.Result
	err error
}

func (f fakeReconciler) Reconcile(context.Context, reconcile.Request) (reconcile.Result, error) {
	return f.res, f.err
}

type panicReconciler struct{}

func (panicReconciler) Reconcile(context.Context, reconcile.Request) (reconcile.Result, error) {
	panic("boom")
}

// captureLog returns a context whose logger appends each line's key/value text to *lines.
func captureLog(t *testing.T, lines *[]string) context.Context {
	t.Helper()
	logger := funcr.New(func(_, args string) { *lines = append(*lines, args) }, funcr.Options{})
	return log.IntoContext(t.Context(), logger)
}

// requireFinishedLine checks that line is a well-formed "Reconcile finished" line.
func requireFinishedLine(t *testing.T, line, outcome string) {
	t.Helper()
	require.Contains(t, line, `"msg"="Reconcile finished"`)
	require.Contains(t, line, `"outcome"="`+outcome+`"`)

	start := regexp.MustCompile(`"start"="([^"]+)"`).FindStringSubmatch(line)
	require.Len(t, start, 2, line)
	_, err := time.Parse(time.RFC3339Nano, start[1])
	require.NoError(t, err)

	dur := regexp.MustCompile(`"durationSeconds"=([^ ]+)`).FindStringSubmatch(line)
	require.Len(t, dur, 2, line)
	seconds, err := strconv.ParseFloat(dur[1], 64)
	require.NoError(t, err)
	require.GreaterOrEqual(t, seconds, 0.0)
}

func TestWrap(t *testing.T) {
	tests := []struct {
		name    string
		inner   fakeReconciler
		outcome string
	}{
		{"success", fakeReconciler{}, "success"},
		{"requeue", fakeReconciler{res: reconcile.Result{RequeueAfter: time.Second}}, "requeue"},
		{"error", fakeReconciler{err: errors.New("boom")}, "error"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var lines []string
			ctx := captureLog(t, &lines)

			res, err := Wrap(tt.inner).Reconcile(ctx, reconcile.Request{})

			require.Equal(t, tt.inner.res, res)
			require.Equal(t, tt.inner.err, err)
			require.Len(t, lines, 1)
			requireFinishedLine(t, lines[0], tt.outcome)
		})
	}
}

// A panic in the inner reconciler is logged as finished and then re-raised, so
// controller-runtime's own recovery still sees it.
func TestWrapPanic(t *testing.T) {
	var lines []string
	ctx := captureLog(t, &lines)

	require.PanicsWithValue(t, "boom", func() {
		_, _ = Wrap(panicReconciler{}).Reconcile(ctx, reconcile.Request{})
	})
	require.Len(t, lines, 1)
	requireFinishedLine(t, lines[0], "panic")
}
