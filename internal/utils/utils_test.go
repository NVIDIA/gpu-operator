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

package utils

import (
	"reflect"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestGetObjectHash(t *testing.T) {
	type simple struct {
		Name  string
		Count int
	}

	t.Run("deterministic", func(t *testing.T) {
		obj := simple{Name: "a", Count: 1}
		assert.Equal(t, GetObjectHash(obj), GetObjectHash(obj))
	})

	t.Run("different values produce different hashes", func(t *testing.T) {
		obj1 := simple{Name: "a", Count: 1}
		obj2 := simple{Name: "b", Count: 1}
		assert.NotEqual(t, GetObjectHash(obj1), GetObjectHash(obj2))
	})

	t.Run("zero-valued field affects hash", func(t *testing.T) {
		withZero := simple{Name: "a", Count: 0}
		withNonZero := simple{Name: "a", Count: 1}
		assert.NotEqual(t, GetObjectHash(withZero), GetObjectHash(withNonZero))
	})

}

func TestGetObjectHashIgnoreEmptyKeys(t *testing.T) {
	type simple struct {
		Name  string
		Count int
	}

	t.Run("deterministic", func(t *testing.T) {
		obj := simple{Name: "a", Count: 1}
		assert.Equal(t, GetObjectHashIgnoreEmptyKeys(&obj), GetObjectHashIgnoreEmptyKeys(&obj))
	})

	t.Run("different values produce different hashes", func(t *testing.T) {
		obj1 := simple{Name: "a", Count: 1}
		obj2 := simple{Name: "b", Count: 1}
		assert.NotEqual(t, GetObjectHashIgnoreEmptyKeys(&obj1), GetObjectHashIgnoreEmptyKeys(&obj2))
	})

	t.Run("zero-valued field does not affect hash", func(t *testing.T) {
		type base struct {
			Name string
		}
		type extended struct {
			Name       string
			ExtraField string
		}
		fewer := base{Name: "a"}
		withZeroExtra := extended{Name: "a", ExtraField: ""}
		assert.Equal(t, GetObjectHashIgnoreEmptyKeys(&fewer), GetObjectHashIgnoreEmptyKeys(&withZeroExtra))
	})

	t.Run("non-zero field changes hash", func(t *testing.T) {
		type extended struct {
			Name       string
			ExtraField string
		}
		withZeroExtra := extended{Name: "a", ExtraField: ""}
		withSetExtra := extended{Name: "a", ExtraField: "set"}
		assert.NotEqual(t, GetObjectHashIgnoreEmptyKeys(&withZeroExtra), GetObjectHashIgnoreEmptyKeys(&withSetExtra))
	})

	t.Run("nil slice and empty slice produce same hash", func(t *testing.T) {
		type withSlice struct {
			Name  string
			Items []string
		}
		nilSlice := withSlice{Name: "a", Items: nil}
		emptySlice := withSlice{Name: "a", Items: []string{}}
		assert.Equal(t, GetObjectHashIgnoreEmptyKeys(&nilSlice), GetObjectHashIgnoreEmptyKeys(&emptySlice))
	})

	t.Run("nil map and empty map produce same hash", func(t *testing.T) {
		type withMap struct {
			Name   string
			Labels map[string]string
		}
		nilMap := withMap{Name: "a", Labels: nil}
		emptyMap := withMap{Name: "a", Labels: map[string]string{}}
		assert.Equal(t, GetObjectHashIgnoreEmptyKeys(&nilMap), GetObjectHashIgnoreEmptyKeys(&emptyMap))
	})

	t.Run("embedded struct fields are flattened", func(t *testing.T) {
		type inner struct {
			X string
		}
		type outer struct {
			inner
			Y string
		}
		type flat struct {
			X string
			Y string
		}
		nested := outer{inner: inner{X: "a"}, Y: "b"}
		flattened := flat{X: "a", Y: "b"}
		assert.Equal(t, GetObjectHashIgnoreEmptyKeys(&nested), GetObjectHashIgnoreEmptyKeys(&flattened))
	})
}

func TestIsEffectivelyZero(t *testing.T) {
	tests := []struct {
		name     string
		value    interface{}
		expected bool
	}{
		{"zero int", 0, true},
		{"non-zero int", 1, false},
		{"empty string", "", true},
		{"non-empty string", "a", false},
		{"false bool", false, true},
		{"true bool", true, false},
		{"nil slice", ([]string)(nil), true},
		{"empty slice", []string{}, true},
		{"non-empty slice", []string{"a"}, false},
		{"nil map", (map[string]string)(nil), true},
		{"empty map", map[string]string{}, true},
		{"non-empty map", map[string]string{"k": "v"}, false},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			assert.Equal(t, tc.expected, isEffectivelyZero(reflect.ValueOf(tc.value)))
		})
	}
}

func TestGetStringHash(t *testing.T) {
	type test struct {
		input    string
		expected string
	}

	testcases := []test{
		{
			input:    "2269c984-db9a-4b0e-9fd5-86df0ad269f7",
			expected: "7c6d7bd86b",
		},
		{
			input:    "2269c984-db9a-4b0e-9fd5-86df0ad269f7-5.15.0-1041-azure",
			expected: "79d6bd954f",
		},
		{
			input:    "2269c984-db9a-4b0e-9fd5-86df0ad269f7-rhcos4.14-414.92.202309282257",
			expected: "646cdfdb96",
		},
		{
			input:    "rhcos4.14-414.92.202309282257",
			expected: "5bbdb464cb",
		},
		{
			input:    "nvidia-gpu-driver-2269c984-db9a-4b0e-9fd5-86df0ad269f7-rhcos4.14-414.92.202309282257",
			expected: "7bf6859b6d",
		},
		{
			input:    "nvidia-vgpu-driver-2269c984-db9a-4b0e-9fd5-868df0ad269f7-rhcos4.14-414.92.202309282257",
			expected: "7469f59898",
		},
	}

	for _, tc := range testcases {
		actual := GetStringHash(tc.input)
		assert.Equal(t, tc.expected, actual)
	}
}
