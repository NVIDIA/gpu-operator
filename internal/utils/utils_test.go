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
	"testing"

	"github.com/stretchr/testify/assert"
)

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
