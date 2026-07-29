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

func mustWriteFile(t *testing.T, path string) {
	t.Helper()
	require.NoError(t, os.MkdirAll(filepath.Dir(path), 0o755))
	require.NoError(t, os.WriteFile(path, []byte("x"), 0o600))
}

func TestGetFilesWithSuffix(t *testing.T) {
	base := t.TempDir()
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

func TestGetFilesWithSuffix_NonExistentDir(t *testing.T) {
	missing := filepath.Join(t.TempDir(), "does-not-exist")

	got, err := GetFilesWithSuffix(missing, ".txt")
	require.Error(t, err)
	assert.Nil(t, got)
	assert.ErrorContains(t, err, "error traversing directory tree")
}

func TestGetFilesWithSuffix_EmptyDir(t *testing.T) {
	got, err := GetFilesWithSuffix(t.TempDir(), ".txt")
	require.NoError(t, err)
	assert.Empty(t, got)
}

func TestGetFilesWithSuffix_BaseDirIsFile(t *testing.T) {
	f := filepath.Join(t.TempDir(), "solo.txt")
	mustWriteFile(t, f)

	got, err := GetFilesWithSuffix(f, ".txt")
	require.NoError(t, err)
	assert.Equal(t, []string{f}, got)
}
