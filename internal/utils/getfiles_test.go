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

// mustWriteFile creates path (and any parent directories) with dummy content.
func mustWriteFile(t *testing.T, path string) {
	t.Helper()
	require.NoError(t, os.MkdirAll(filepath.Dir(path), 0o755))
	require.NoError(t, os.WriteFile(path, []byte("x"), 0o600))
}

func TestGetFilesWithSuffix(t *testing.T) {
	base := t.TempDir()
	// Build a small tree with files at multiple depths.
	files := []string{
		"a.txt",
		"b.yaml",
		"c.json",
		filepath.Join("sub", "d.txt"),
		filepath.Join("sub", "e.yaml"),
		filepath.Join("sub", "deep", "f.txt"),
	}
	for _, rel := range files {
		mustWriteFile(t, filepath.Join(base, rel))
	}

	abs := func(rels ...string) []string {
		out := make([]string, len(rels))
		for i, r := range rels {
			out[i] = filepath.Join(base, r)
		}
		return out
	}

	testCases := []struct {
		name     string
		suffixes []string
		want     []string
	}{
		{
			name:     "single suffix recurses into subdirectories",
			suffixes: []string{".txt"},
			want:     abs("a.txt", filepath.Join("sub", "d.txt"), filepath.Join("sub", "deep", "f.txt")),
		},
		{
			name:     "multiple suffixes match a union of files",
			suffixes: []string{".yaml", ".json"},
			want:     abs("b.yaml", "c.json", filepath.Join("sub", "e.yaml")),
		},
		{
			name:     "suffix matching nothing returns empty",
			suffixes: []string{".md"},
			want:     nil,
		},
		{
			name:     "no suffixes provided returns empty",
			suffixes: nil,
			want:     nil,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := GetFilesWithSuffix(base, tc.suffixes...)
			require.NoError(t, err)
			assert.ElementsMatch(t, tc.want, got)
		})
	}
}

// TestGetFilesWithSuffix_NonExistentDir covers the traversal-error path: the
// walk callback receives a non-nil error for the missing root, which is then
// wrapped and returned.
func TestGetFilesWithSuffix_NonExistentDir(t *testing.T) {
	missing := filepath.Join(t.TempDir(), "does-not-exist")

	got, err := GetFilesWithSuffix(missing, ".txt")
	require.Error(t, err)
	assert.Nil(t, got)
	assert.ErrorContains(t, err, "error traversing directory tree")
}

// TestGetFilesWithSuffix_EmptyDir returns no files and no error for a directory
// that exists but has no matching entries.
func TestGetFilesWithSuffix_EmptyDir(t *testing.T) {
	got, err := GetFilesWithSuffix(t.TempDir(), ".txt")
	require.NoError(t, err)
	assert.Empty(t, got)
}

// TestGetFilesWithSuffix_BaseDirIsFile covers passing a file (not a directory)
// as the base: Walk visits the single file, which is not a directory and is
// matched by suffix.
func TestGetFilesWithSuffix_BaseDirIsFile(t *testing.T) {
	f := filepath.Join(t.TempDir(), "solo.txt")
	mustWriteFile(t, f)

	got, err := GetFilesWithSuffix(f, ".txt")
	require.NoError(t, err)
	assert.Equal(t, []string{f}, got)
}

// TestGetFilesWithSuffix_MatchesMultipleSuffixes verifies that a file matching
// more than one of the provided suffixes is returned exactly once (not once per
// matching suffix).
func TestGetFilesWithSuffix_MatchesMultipleSuffixes(t *testing.T) {
	dir := t.TempDir()
	f := filepath.Join(dir, "archive.tar.gz")
	mustWriteFile(t, f)

	got, err := GetFilesWithSuffix(dir, ".gz", ".tar.gz")
	require.NoError(t, err)
	assert.Equal(t, []string{f}, got)
}
