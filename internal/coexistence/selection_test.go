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

package coexistence

import (
	"testing"

	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
)

func TestSelect(t *testing.T) {
	for name, tc := range map[string]struct {
		mode, record, defaultStack, want string
		explicit, cp, gc, invalid        bool
	}{
		"sole CP":                            {cp: true, want: DP},
		"sole GC":                            {gc: true, want: DRA},
		"both":                               {cp: true, gc: true, want: DP},
		"neither":                            {},
		"default applies to sole CP":         {cp: true, defaultStack: DRA, want: DRA},
		"default applies to sole GC":         {gc: true, defaultStack: DP, want: DP},
		"default without sources":            {defaultStack: DRA, want: DRA},
		"record survives changed default":    {cp: true, gc: true, record: DRA, defaultStack: DP, want: DRA},
		"stale record after source removal":  {gc: true, record: DP, want: DRA},
		"explicit absent source":             {gc: true, explicit: true, mode: DP, want: DP},
		"explicit repairs invalid record":    {gc: true, explicit: true, mode: DRA, record: "bad", want: DRA},
		"invalid explicit with valid record": {cp: true, explicit: true, mode: "bad", record: DP, invalid: true},
		"empty explicit":                     {cp: true, explicit: true, invalid: true},
		"invalid record":                     {cp: true, record: "bad", invalid: true},
	} {
		t.Run(name, func(t *testing.T) {
			got, err := Select(tc.mode, tc.explicit, tc.record, tc.cp, tc.gc, tc.defaultStack)
			if tc.invalid {
				require.Error(t, err)
				return
			}
			require.NoError(t, err)
			require.Equal(t, tc.want, got)
		})
	}
}

func TestModeAffinityPreservesEveryAlternative(t *testing.T) {
	hardware := corev1.NodeSelectorRequirement{Key: "hardware", Operator: corev1.NodeSelectorOpIn, Values: []string{"gpu"}}
	nodeName := corev1.NodeSelectorRequirement{Key: "metadata.name", Operator: corev1.NodeSelectorOpIn, Values: []string{"special"}}
	modeAbsent := corev1.NodeSelectorRequirement{Key: ModeLabel, Operator: corev1.NodeSelectorOpDoesNotExist}
	modeDRA := corev1.NodeSelectorRequirement{Key: ModeLabel, Operator: corev1.NodeSelectorOpIn, Values: []string{DRA}}

	for name, tc := range map[string]struct {
		mode      string
		required  *corev1.NodeSelector
		wantTerms []corev1.NodeSelectorTerm
	}{
		"DRA intersects each existing alternative": {
			mode: DRA,
			required: &corev1.NodeSelector{NodeSelectorTerms: []corev1.NodeSelectorTerm{
				{MatchExpressions: []corev1.NodeSelectorRequirement{hardware}},
				{MatchFields: []corev1.NodeSelectorRequirement{nodeName}},
			}},
			wantTerms: []corev1.NodeSelectorTerm{
				{MatchExpressions: []corev1.NodeSelectorRequirement{hardware, modeAbsent}},
				{MatchExpressions: []corev1.NodeSelectorRequirement{hardware, modeDRA}},
				{MatchFields: []corev1.NodeSelectorRequirement{nodeName}, MatchExpressions: []corev1.NodeSelectorRequirement{modeAbsent}},
				{MatchFields: []corev1.NodeSelectorRequirement{nodeName}, MatchExpressions: []corev1.NodeSelectorRequirement{modeDRA}},
			},
		},
		"device plugin preserves a selector that matches no nodes": {
			mode:     DP,
			required: &corev1.NodeSelector{},
		},
	} {
		t.Run(name, func(t *testing.T) {
			spec := &corev1.PodSpec{
				NodeSelector: map[string]string{"os": "linux"},
				Affinity: &corev1.Affinity{NodeAffinity: &corev1.NodeAffinity{
					RequiredDuringSchedulingIgnoredDuringExecution: tc.required,
				}},
			}
			ConstrainMode(spec, tc.mode)
			terms := spec.Affinity.NodeAffinity.RequiredDuringSchedulingIgnoredDuringExecution.NodeSelectorTerms
			require.ElementsMatch(t, tc.wantTerms, terms)
			require.Equal(t, map[string]string{"os": "linux"}, spec.NodeSelector)
		})
	}
}

func TestDefaultStackConfiguration(t *testing.T) {
	for name, tc := range map[string]struct {
		value   string
		wantErr bool
	}{
		"device plugin": {value: DP},
		"DRA":           {value: DRA},
		"empty":         {wantErr: true},
		"uppercase":     {value: "DRA", wantErr: true},
		"unknown":       {value: "none", wantErr: true},
	} {
		t.Run(name, func(t *testing.T) {
			t.Setenv(DefaultStackEnv, tc.value)
			got, err := DefaultStack()
			if tc.wantErr {
				require.Error(t, err)
				return
			}
			require.NoError(t, err)
			require.Equal(t, tc.value, got)
		})
	}
}
