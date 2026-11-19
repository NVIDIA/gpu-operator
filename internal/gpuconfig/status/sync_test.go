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

package status

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/NVIDIA/gpu-operator/internal/gpuconfig/statefile"
)

func TestSync(t *testing.T) {
	const (
		l      = 110711
		hA     = "sha256:aa"
		hB     = "sha256:bb"
		noDocs = ""
	)
	apply := func(module uint64, v statefile.Decision, config string) *statefile.State {
		return &statefile.State{Version: 1, Apply: &statefile.Apply{Module: module, Value: v, Config: config}}
	}
	cases := map[string]struct {
		state *statefile.State
		h     string
		want  string
	}{
		"applied, document unchanged": {
			state: apply(l, statefile.Applied, hA), h: hA, want: InSync,
		},
		"applied, then the document was edited": {
			state: apply(l, statefile.Applied, hA), h: hB, want: OutOfSync,
		},
		"applied, then the document was deleted and none applies": {
			state: apply(l, statefile.Applied, hA), h: noDocs, want: OutOfSync,
		},
		"applied with no document, still none": {
			state: apply(l, statefile.Applied, noDocs), h: noDocs, want: InSync,
		},
		"applied with no document, then one was created": {
			state: apply(l, statefile.Applied, noDocs), h: hA, want: OutOfSync,
		},
		"skipped, keeping the previous config": {
			state: apply(l, statefile.Skipped, hA), h: hA, want: OutOfSync,
		},
		"skipped with no config, no document applies": {
			state: apply(l, statefile.Skipped, noDocs), h: noDocs, want: OutOfSync,
		},
		"applied at an earlier driver load": {
			state: apply(l-1, statefile.Applied, hA), h: hA, want: OutOfSync,
		},
		"pending at the current load": {
			state: apply(l, "pending", hA), h: hA, want: OutOfSync,
		},
		"unknown value at the current load": {
			state: apply(l, "future", hA), h: hA, want: OutOfSync,
		},
		"no apply object": {
			state: &statefile.State{Version: 1, Gate: &statefile.Gate{Module: l}}, h: noDocs, want: OutOfSync,
		},
		"no state file": {
			state: &statefile.State{}, h: noDocs, want: OutOfSync,
		},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			require.Equal(t, tc.want, Sync(l, tc.state, tc.h))
		})
	}
}
