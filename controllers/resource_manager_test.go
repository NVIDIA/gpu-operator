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

package controllers

import (
	"io/fs"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestManifestKind(t *testing.T) {
	tests := []struct {
		name         string
		manifest     string
		expectedKind string
		errorMessage string
	}{
		{
			name: "YAML",
			manifest: `apiVersion: v1
kind: ServiceAccount
metadata:
  name: test
`,
			expectedKind: "ServiceAccount",
		},
		{
			name:         "JSON",
			manifest:     `{"apiVersion":"v1","kind":"ConfigMap","metadata":{"name":"test"}}`,
			expectedKind: "ConfigMap",
		},
		{
			name: "kind-like text in another field",
			manifest: `apiVersion: v1
metadata:
  annotations:
    example.com/value: "kind: WrongKind"
  name: test
kind: Service
`,
			expectedKind: "Service",
		},
		{
			name: "missing kind",
			manifest: `apiVersion: v1
metadata:
  name: test
`,
			errorMessage: "manifest is missing kind",
		},
		{
			name:         "malformed YAML",
			manifest:     "kind: [",
			errorMessage: "failed to decode manifest metadata",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			kind, err := manifestKind([]byte(tc.manifest))
			if tc.errorMessage != "" {
				require.ErrorContains(t, err, tc.errorMessage)
				require.Empty(t, kind)
				return
			}
			require.NoError(t, err)
			require.Equal(t, tc.expectedKind, kind)
		})
	}
}

func TestAllAssetsHaveManifestKind(t *testing.T) {
	err := filepath.WalkDir("../assets", func(path string, entry fs.DirEntry, walkErr error) error {
		require.NoError(t, walkErr)
		if entry.IsDir() {
			return nil
		}

		manifest, err := os.ReadFile(path)
		require.NoError(t, err)
		kind, err := manifestKind(manifest)
		require.NoErrorf(t, err, "failed to read kind from %s", path)
		require.NotEmptyf(t, kind, "empty kind in %s", path)
		return nil
	})
	require.NoError(t, err)
}
