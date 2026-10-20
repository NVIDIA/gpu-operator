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
			logger := funcr.New(func(prefix, args string) { lines = append(lines, args) }, funcr.Options{})
			ctx := log.IntoContext(t.Context(), logger)

			res, err := Wrap(tt.inner).Reconcile(ctx, reconcile.Request{})

			require.Equal(t, tt.inner.res, res)
			require.Equal(t, tt.inner.err, err)
			require.Len(t, lines, 1)
			require.Contains(t, lines[0], `"msg"="Reconcile finished"`)
			require.Contains(t, lines[0], `"outcome"="`+tt.outcome+`"`)
			require.Contains(t, lines[0], `"start"=`)
			require.Contains(t, lines[0], `"durationSeconds"=`)
		})
	}
}
