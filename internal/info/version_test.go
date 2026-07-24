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
			description: "default unknown version, no commit",
			version:     "unknown",
			gitCommit:   "",
			expected:    []string{"unknown"},
		},
		{
			description: "explicit version, no commit",
			version:     "1.2.3",
			gitCommit:   "",
			expected:    []string{"1.2.3"},
		},
		{
			description: "version and commit",
			version:     "1.2.3",
			gitCommit:   "abcdef0",
			expected:    []string{"1.2.3", "commit: abcdef0"},
		},
		{
			description: "semver with pre-release and build metadata",
			version:     "1.2.3-rc.1+build.5",
			gitCommit:   "",
			expected:    []string{"1.2.3-rc.1+build.5"},
		},
		// ---- Edge cases: no input validation ----
		{
			description: "empty version, no commit still yields a single empty element",
			version:     "",
			gitCommit:   "",
			expected:    []string{""},
		},
		{
			description: "empty version with commit",
			version:     "",
			gitCommit:   "deadbee",
			expected:    []string{"", "commit: deadbee"},
		},
		{
			description: "whitespace commit is non-empty and is included",
			version:     "1.2.3",
			gitCommit:   " ",
			expected:    []string{"1.2.3", "commit:  "},
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
			more:        []string{"go1.26", "linux/amd64"},
			expected:    "1.2.3, go1.26, linux/amd64",
		},
		{
			description: "version, commit and extra parts",
			version:     "1.2.3",
			gitCommit:   "abcdef0",
			more:        []string{"go1.26"},
			expected:    "1.2.3, commit: abcdef0, go1.26",
		},
		{
			description: "many extra parts are all joined",
			version:     "unknown",
			gitCommit:   "",
			more:        []string{"a", "b", "c"},
			expected:    "unknown, a, b, c",
		},
		// ---- Edge cases ----
		{
			description: "empty version, no commit, no extras yields empty string",
			version:     "",
			gitCommit:   "",
			expected:    "",
		},
		{
			description: "empty version, no commit, single empty extra yields a bare separator",
			version:     "",
			gitCommit:   "",
			more:        []string{""},
			expected:    ", ",
		},
		{
			description: "single empty extra appends a trailing separator",
			version:     "unknown",
			gitCommit:   "",
			more:        []string{""},
			expected:    "unknown, ",
		},
		{
			description: "empty version with commit and an extra",
			version:     "",
			gitCommit:   "cafe",
			more:        []string{"x"},
			expected:    ", commit: cafe, x",
		},
		{
			description: "empty extra elements each add a separator",
			version:     "1.2.3",
			gitCommit:   "abcdef0",
			more:        []string{"", ""},
			expected:    "1.2.3, commit: abcdef0, , ",
		},
	}

	for _, tc := range testCases {
		t.Run(tc.description, func(t *testing.T) {
			setVersion(t, tc.version, tc.gitCommit)
			assert.Equal(t, tc.expected, GetVersionString(tc.more...))
		})
	}
}

// TestGetVersionParts_ReturnsFreshSlice ensures each call allocates a new slice,
// so a caller mutating the result cannot corrupt subsequent calls.
func TestGetVersionParts_ReturnsFreshSlice(t *testing.T) {
	setVersion(t, "1.0.0", "abcdef0")

	first := GetVersionParts()
	first[0] = "MUTATED"

	second := GetVersionParts()
	assert.Equal(t, []string{"1.0.0", "commit: abcdef0"}, second)
}

// TestGetVersionString_DoesNotMutateParts ensures that appending the variadic
// "more" args does not leak back into what GetVersionParts returns (guards
// against an append-aliasing regression).
func TestGetVersionString_DoesNotMutateParts(t *testing.T) {
	setVersion(t, "1.0.0", "abcdef0")

	got := GetVersionString("extra")
	assert.Equal(t, "1.0.0, commit: abcdef0, extra", got)

	// The underlying parts must be unchanged by the call above.
	assert.Equal(t, []string{"1.0.0", "commit: abcdef0"}, GetVersionParts())
}
