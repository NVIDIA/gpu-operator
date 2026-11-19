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

package statefile

import (
	"encoding/json"
	"maps"
	"slices"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestParseErrors(t *testing.T) {
	cases := map[string]string{
		"empty file":                ``,
		"invalid JSON":              `{"version":1`,
		"trailing data":             `{"version":1} {}`,
		"top-level null":            `null`,
		"top-level empty object":    `{}`,
		"top-level array":           `[]`,
		"version 0":                 `{"version":0}`,
		"version 2":                 `{"version":2}`,
		"version as string":         `{"version":"1"}`,
		"version negative":          `{"version":-1}`,
		"version fractional":        `{"version":1.0}`,
		"version exponent":          `{"version":1e0}`,
		"apply as number":           `{"version":1,"apply":5}`,
		"apply.module negative":     `{"version":1,"apply":{"module":-1}}`,
		"apply.module as string":    `{"version":1,"apply":{"module":"7"}}`,
		"apply.value as number":     `{"version":1,"apply":{"module":7,"value":1}}`,
		"apply.config as number":    `{"version":1,"apply":{"module":7,"config":1}}`,
		"gate as string":            `{"version":1,"gate":"x"}`,
		"gate.module fractional":    `{"version":1,"gate":{"module":7.5}}`,
		"created not RFC 3339":      `{"version":1,"created":"2026-09-26 08:15:02"}`,
		"created as number":         `{"version":1,"created":0}`,
		"apply.time not RFC 3339":   `{"version":1,"apply":{"module":7,"time":"yesterday"}}`,
		"gate.time missing seconds": `{"version":1,"gate":{"module":7,"time":"2026-09-26T08:15Z"}}`,
	}
	for name, data := range cases {
		t.Run(name, func(t *testing.T) {
			_, err := parse([]byte(data))
			require.ErrorIs(t, err, ErrParse)
		})
	}
}

func TestParse(t *testing.T) {
	ts := time.Date(2026, 9, 26, 8, 15, 2, 0, time.UTC)
	cases := map[string]struct {
		data string
		want State
	}{
		"every field set": {
			data: `{"version":1,"created":"2026-09-26T08:15:02Z",` +
				`"apply":{"module":110711,"value":"applied","config":"sha256:50d8","time":"2026-09-26T08:15:02Z"},` +
				`"gate":{"module":110711,"time":"2026-09-26T08:15:02Z"}}`,
			want: State{
				Version: 1, Created: ts,
				Apply: &Apply{Module: 110711, Value: Applied, Config: "sha256:50d8", Time: ts},
				Gate:  &Gate{Module: 110711, Time: ts},
			},
		},
		"objects absent": {
			data: `{"version":1}`,
			want: State{Version: 1},
		},
		"objects null": {
			data: `{"version":1,"apply":null,"gate":null}`,
			want: State{Version: 1},
		},
		"objects empty are present": {
			data: `{"version":1,"apply":{},"gate":{}}`,
			want: State{Version: 1, Apply: &Apply{}, Gate: &Gate{}},
		},
		"null timestamps read as zero": {
			data: `{"version":1,"created":null,"gate":{"module":3,"time":null}}`,
			want: State{Version: 1, Gate: &Gate{Module: 3}},
		},
		"unknown value is not a parse error": {
			data: `{"version":1,"apply":{"module":3,"value":"reticulating"}}`,
			want: State{Version: 1, Apply: &Apply{Module: 3, Value: "reticulating"}},
		},
		"unknown keys are ignored": {
			data: `{"version":1,"future":{"module":3},"apply":{"module":3,"extra":true},"gate":{"module":3,"extra":[1]}}`,
			want: State{Version: 1, Apply: &Apply{Module: 3}, Gate: &Gate{Module: 3}},
		},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			f, err := parse([]byte(tc.data))
			require.NoError(t, err)
			require.True(t, f.Exists())
			require.Equal(t, tc.want, f.State)
		})
	}
}

func TestPredicates(t *testing.T) {
	const l = 110711
	type preds struct {
		applyAt, decided, applied, undecided, passed, noDecision, passedBoot bool
		config                                                               string
	}
	apply := func(module uint64, v Decision) *Apply {
		return &Apply{Module: module, Value: v, Config: "sha256:aa"}
	}
	cases := map[string]struct {
		state State
		want  preds
	}{
		"no file": {
			state: State{},
			want:  preds{},
		},
		"applied at the current load, gate absent": {
			state: State{Apply: apply(l, Applied)},
			want:  preds{applyAt: true, decided: true, applied: true, config: "sha256:aa"},
		},
		"skipped and gate passed at the current load": {
			state: State{Apply: apply(l, Skipped), Gate: &Gate{Module: l}},
			want:  preds{applyAt: true, decided: true, passed: true, passedBoot: true, config: "sha256:aa"},
		},
		"pending at the current load": {
			state: State{Apply: apply(l, "pending"), Gate: &Gate{Module: l}},
			want:  preds{applyAt: true, undecided: true, passed: true, passedBoot: true, config: "sha256:aa"},
		},
		"missing value at the current load": {
			state: State{Apply: apply(l, "")},
			want:  preds{applyAt: true, undecided: true, config: "sha256:aa"},
		},
		"unknown value at the current load": {
			state: State{Apply: apply(l, "future")},
			want:  preds{applyAt: true, undecided: true, config: "sha256:aa"},
		},
		"applied at an earlier driver load, gate passed then": {
			state: State{Apply: apply(l-1, Applied), Gate: &Gate{Module: l - 1}},
			want:  preds{passedBoot: true},
		},
		"gate passed without a decision": {
			state: State{Apply: apply(l-1, Applied), Gate: &Gate{Module: 0}},
			want:  preds{noDecision: true, passedBoot: true},
		},
		"empty objects": {
			state: State{Apply: &Apply{}, Gate: &Gate{}},
			want:  preds{noDecision: true, passedBoot: true},
		},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			s := tc.state
			got := preds{
				applyAt:    s.ApplyAt(l),
				decided:    s.Decided(l),
				applied:    s.AppliedAt(l),
				undecided:  s.Undecided(l),
				passed:     s.Passed(l),
				noDecision: s.GatePassedWithoutDecision(),
				passedBoot: s.GatePassedThisBoot(),
				config:     s.ConfigAt(l),
			}
			require.Equal(t, tc.want, got)
		})
	}
}

// The encoder silently drops two fields that share a JSON name, so pin the
// exact key set of each object.
func TestObjectKeys(t *testing.T) {
	keys := func(v any) []string {
		b, err := json.Marshal(v)
		require.NoError(t, err)
		var m map[string]json.RawMessage
		require.NoError(t, json.Unmarshal(b, &m))
		return slices.Sorted(maps.Keys(m))
	}
	require.Equal(t, []string{"config", "module", "time", "value"}, keys(Apply{}))
	require.Equal(t, []string{"module", "time"}, keys(Gate{}))
	require.Equal(t, []string{"apply", "created", "gate", "version"}, keys(State{}))
}
