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
	original := []corev1.NodeSelectorTerm{
		{MatchExpressions: []corev1.NodeSelectorRequirement{{Key: "hardware", Operator: corev1.NodeSelectorOpIn, Values: []string{"gpu"}}}},
		{MatchFields: []corev1.NodeSelectorRequirement{{Key: "metadata.name", Operator: corev1.NodeSelectorOpIn, Values: []string{"special"}}}},
	}
	spec := &corev1.PodSpec{NodeSelector: map[string]string{"os": "linux"}, Affinity: &corev1.Affinity{NodeAffinity: &corev1.NodeAffinity{RequiredDuringSchedulingIgnoredDuringExecution: &corev1.NodeSelector{NodeSelectorTerms: original}}}}
	ConstrainMode(spec, DRA)
	terms := spec.Affinity.NodeAffinity.RequiredDuringSchedulingIgnoredDuringExecution.NodeSelectorTerms
	require.Len(t, terms, 4)
	for i, term := range terms {
		require.Equal(t, original[i/2].MatchFields, term.MatchFields)
		require.ElementsMatch(t, original[i/2].MatchExpressions, term.MatchExpressions[:len(term.MatchExpressions)-1])
		requirement := term.MatchExpressions[len(term.MatchExpressions)-1]
		require.Equal(t, ModeLabel, requirement.Key)
		if i%2 == 0 {
			require.Equal(t, corev1.NodeSelectorOpDoesNotExist, requirement.Operator)
		} else {
			require.Equal(t, []string{DRA}, requirement.Values)
		}
	}
	require.Equal(t, map[string]string{"os": "linux"}, spec.NodeSelector)
	// An explicitly empty required selector matches no nodes and must stay empty.
	spec.Affinity.NodeAffinity.RequiredDuringSchedulingIgnoredDuringExecution.NodeSelectorTerms = nil
	ConstrainMode(spec, DP)
	require.Empty(t, spec.Affinity.NodeAffinity.RequiredDuringSchedulingIgnoredDuringExecution.NodeSelectorTerms)
}

func TestDefaultStackConfiguration(t *testing.T) {
	for _, value := range []string{DP, DRA, "", "DRA", "none"} {
		t.Run("value="+value, func(t *testing.T) {
			t.Setenv(DefaultStackEnv, value)
			got, err := DefaultStack()
			if value == DP || value == DRA {
				require.NoError(t, err)
				require.Equal(t, value, got)
			} else {
				require.Error(t, err)
			}
		})
	}
}
