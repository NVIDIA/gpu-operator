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

package info

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

// setVersion overrides the package-level version and gitCommit variables for
// the duration of a test, restoring the originals afterwards.
func setVersion(t *testing.T, v, commit string) {
	t.Helper()
	origVersion, origCommit := version, gitCommit
	version, gitCommit = v, commit
	t.Cleanup(func() {
		version, gitCommit = origVersion, origCommit
	})
}

func TestGetVersionParts(t *testing.T) {
	testCases := []struct {
		description string
		version     string
		gitCommit   string
		expected    []string
	}{
		{
			description: "version only when gitCommit is empty",
			version:     "1.2.3",
			gitCommit:   "",
			expected:    []string{"1.2.3"},
		},
		{
			description: "version and commit when gitCommit is set",
			version:     "1.2.3",
			gitCommit:   "abcdef0",
			expected:    []string{"1.2.3", "commit: abcdef0"},
		},
		{
			description: "default unknown version",
			version:     "unknown",
			gitCommit:   "",
			expected:    []string{"unknown"},
		},
	}

	for _, tc := range testCases {
		t.Run(tc.description, func(t *testing.T) {
			setVersion(t, tc.version, tc.gitCommit)
			assert.Equal(t, tc.expected, GetVersionParts())
		})
	}
}

func TestGetVersionString(t *testing.T) {
	testCases := []struct {
		description string
		version     string
		gitCommit   string
		more        []string
		expected    string
	}{
		{
			description: "version only",
			version:     "1.2.3",
			gitCommit:   "",
			expected:    "1.2.3",
		},
		{
			description: "version with commit",
			version:     "1.2.3",
			gitCommit:   "abcdef0",
			expected:    "1.2.3, commit: abcdef0",
		},
		{
			description: "version with extra parts appended",
			version:     "1.2.3",
			gitCommit:   "",
			more:        []string{"go1.22", "linux/amd64"},
			expected:    "1.2.3, go1.22, linux/amd64",
		},
		{
			description: "version, commit and extra parts",
			version:     "1.2.3",
			gitCommit:   "abcdef0",
			more:        []string{"go1.22"},
			expected:    "1.2.3, commit: abcdef0, go1.22",
		},
	}

	for _, tc := range testCases {
		t.Run(tc.description, func(t *testing.T) {
			setVersion(t, tc.version, tc.gitCommit)
			assert.Equal(t, tc.expected, GetVersionString(tc.more...))
		})
	}
}
