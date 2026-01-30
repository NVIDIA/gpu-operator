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

package state

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestGetOSTag(t *testing.T) {
	tests := []struct {
		description  string
		osRelease    string
		osVersion    string
		expected     string
		expectError  bool
		errorMessage string
	}{
		{
			description: "valid os release & version",
			osRelease:   "rhel",
			osVersion:   "9.4",
			expected:    "rhel9.4",
			expectError: false,
		},
		{
			description: "valid os release & version - ubuntu",
			osRelease:   "ubuntu",
			osVersion:   "24.04",
			expected:    "ubuntu24.04",
			expectError: false,
		},
		{
			description: "rocky linux",
			osRelease:   "rocky",
			osVersion:   "9.4",
			expected:    "rocky9",
			expectError: false,
		},
		{
			description: "RHEL 10",
			osRelease:   "rhel",
			osVersion:   "10.1",
			expected:    "rhel10",
			expectError: false,
		},
		{
			description:  "invalid os version",
			osRelease:    "rhel",
			osVersion:    "A.10",
			expectError:  true,
			errorMessage: "failed to parse os version: strconv.Atoi: parsing \"A\": invalid syntax",
		},
	}

	for _, test := range tests {
		t.Run(test.description, func(t *testing.T) {
			actual, err := getOSTag(test.osRelease, test.osVersion)
			if test.expectError {
				require.Error(t, err)
				require.Equal(t, test.errorMessage, err.Error())
			} else {
				require.NoError(t, err)
			}
			require.Equal(t, test.expected, actual)
		})
	}
}
