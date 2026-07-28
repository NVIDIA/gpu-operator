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
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func writeDedupFile(t *testing.T, base, rel string) {
	t.Helper()
	full := filepath.Join(base, rel)
	require.NoError(t, os.MkdirAll(filepath.Dir(full), 0o755))
	require.NoError(t, os.WriteFile(full, []byte("x"), 0o600))
}

// TestGetFilesWithSuffix_Dedupe exercises the break after the first matching
// suffix: a file whose name ends with more than one requested suffix must be
// returned exactly once, regardless of suffix order or directory depth.
func TestGetFilesWithSuffix_Dedupe(t *testing.T) {
	testCases := []struct {
		name     string
		files    []string
		suffixes []string
		want     []string
	}{
		{
			name:     "file matching two overlapping suffixes is returned once",
			files:    []string{"archive.tar.gz"},
			suffixes: []string{".gz", ".tar.gz"},
			want:     []string{"archive.tar.gz"},
		},
		{
			name:     "dedupe is independent of suffix order",
			files:    []string{"archive.tar.gz"},
			suffixes: []string{".tar.gz", ".gz"},
			want:     []string{"archive.tar.gz"},
		},
		{
			name:     "file matching exactly one of several suffixes is returned once",
			files:    []string{"config.json"},
			suffixes: []string{".yaml", ".yml", ".json"},
			want:     []string{"config.json"},
		},
		{
			name:     "each of several overlapping-match files is returned once",
			files:    []string{"a.tar.gz", "b.tar.gz"},
			suffixes: []string{".gz", ".tar.gz"},
			want:     []string{"a.tar.gz", "b.tar.gz"},
		},
		{
			name:     "non-matching files are excluded while overlapping matches dedupe",
			files:    []string{"keep.tar.gz", "skip.txt", "keep2.json"},
			suffixes: []string{".gz", ".tar.gz", ".json"},
			want:     []string{"keep.tar.gz", "keep2.json"},
		},
		{
			name:     "dedupe applies to files in subdirectories",
			files:    []string{filepath.Join("sub", "nested.tar.gz")},
			suffixes: []string{".gz", ".tar.gz"},
			want:     []string{filepath.Join("sub", "nested.tar.gz")},
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			base := t.TempDir()
			for _, rel := range tc.files {
				writeDedupFile(t, base, rel)
			}
			want := make([]string, len(tc.want))
			for i, rel := range tc.want {
				want[i] = filepath.Join(base, rel)
			}

			got, err := GetFilesWithSuffix(base, tc.suffixes...)
			require.NoError(t, err)
			// ElementsMatch also asserts multiplicity, so a duplicated path fails.
			assert.ElementsMatch(t, want, got)
		})
	}
}
