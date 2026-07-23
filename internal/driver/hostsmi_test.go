//go:build linux

/*
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
*/

package driver

import (
	"maps"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestResolveHostNvidiaSMI(t *testing.T) {
	testCases := []struct {
		description  string
		contents     map[string]string
		expectsError bool
	}{
		{
			description: "nvidia-smi exists in /usr/bin",
			contents: map[string]string{
				"/usr/bin/nvidia-smi": "fake nvidia-smi",
			},
		},
		{
			description: "nvidia-smi exists through absolute /usr/bin symlink",
			contents: map[string]string{
				"/run/current-system/sw/bin/nvidia-smi": "fake nvidia-smi",
				"/usr/bin":                              "symlink=/run/current-system/sw/bin",
			},
		},
		{
			description: "nvidia-smi exists through relative /usr/bin symlink",
			contents: map[string]string{
				"/run/current-system/sw/bin/nvidia-smi": "fake nvidia-smi",
				"/usr/bin":                              "symlink=../run/current-system/sw/bin",
			},
		},
		{
			description: "parent dir is symlink to path not within root",
			contents: map[string]string{
				"/usr/bin": "symlink=../../",
			},
			expectsError: true,
		},
		{
			description:  "nvidia-smi does not exist",
			expectsError: true,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.description, func(t *testing.T) {
			hostRoot := t.TempDir()
			// Iterate in sorted order so parent symlinks are created before paths under them.
			for _, name := range slices.Sorted(maps.Keys(tc.contents)) {
				contents := tc.contents[name]
				target := filepath.Join(hostRoot, name)
				require.NoError(t, os.MkdirAll(filepath.Dir(target), 0755))

				if strings.HasPrefix(contents, "symlink=") {
					require.NoError(t, os.Symlink(strings.TrimPrefix(contents, "symlink="), target))
					continue
				}

				require.NoError(t, os.WriteFile(target, []byte(contents), 0600))
			}

			fileInfo, err := ResolveHostNvidiaSMI(hostRoot)
			if tc.expectsError {
				require.Error(t, err)
				return
			}

			require.NoError(t, err)
			require.NotZero(t, fileInfo.Size())
		})
	}
}
