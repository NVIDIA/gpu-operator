//go:build linux

/*
 * Copyright (c) NVIDIA CORPORATION.  All rights reserved.
 *
 * Licensed under the Apache License, Version 2.0 (the "License");
 * you may not use this file except in compliance with the License.
 * You may obtain a copy of the License at
 *
 *     http://www.apache.org/licenses/LICENSE-2.0
 *
 * Unless required by applicable law or agreed to in writing, software
 * distributed under the License is distributed on an "AS IS" BASIS,
 * WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
 * See the License for the specific language governing permissions and
 * limitations under the License.
 */

package main

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
		perms        map[string]os.FileMode
		expectedPath string
		expectsError bool
	}{
		{
			description: "nvidia-smi exists in /usr/bin",
			contents: map[string]string{
				"/usr/bin/nvidia-smi": "fake nvidia-smi",
			},
			expectedPath: "/usr/bin/nvidia-smi",
		},
		{
			description: "nvidia-smi exists through absolute /usr/bin symlink",
			contents: map[string]string{
				"/run/current-system/sw/bin/nvidia-smi": "fake nvidia-smi",
				"/usr/bin":                              "symlink=/run/current-system/sw/bin",
			},
			expectedPath: "/usr/bin/nvidia-smi",
		},
		{
			description: "nvidia-smi exists through relative /usr/bin symlink",
			contents: map[string]string{
				"/run/current-system/sw/bin/nvidia-smi": "fake nvidia-smi",
				"/usr/bin":                              "symlink=../run/current-system/sw/bin",
			},
			expectedPath: "/usr/bin/nvidia-smi",
		},
		{
			description: "nvidia-smi exists in /opt/bin",
			contents: map[string]string{
				"/opt/bin/nvidia-smi": "fake nvidia-smi",
			},
			expectedPath: "/opt/bin/nvidia-smi",
		},
		{
			description: "nvidia-smi exists in /bin",
			contents: map[string]string{
				"/bin/nvidia-smi": "fake nvidia-smi",
			},
			expectedPath: "/bin/nvidia-smi",
		},
		{
			description: "nvidia-smi exists in /usr/sbin",
			contents: map[string]string{
				"/usr/sbin/nvidia-smi": "fake nvidia-smi",
			},
			expectedPath: "/usr/sbin/nvidia-smi",
		},
		{
			description: "nvidia-smi exists through absolute /usr/sbin symlink",
			contents: map[string]string{
				"/run/current-system/sw/bin/nvidia-smi": "fake nvidia-smi",
				"/usr/sbin":                             "symlink=/run/current-system/sw/bin",
			},
			expectedPath: "/usr/sbin/nvidia-smi",
		},
		{
			description: "nvidia-smi exists in WSL path",
			contents: map[string]string{
				"/usr/lib/wsl/lib/nvidia-smi": "fake nvidia-smi",
			},
			expectedPath: "/usr/lib/wsl/lib/nvidia-smi",
		},
		{
			description: "nvidia-smi exists through absolute WSL path symlink",
			contents: map[string]string{
				"/run/wsl/lib/nvidia-smi": "fake nvidia-smi",
				"/usr/lib/wsl/lib":        "symlink=/run/wsl/lib",
			},
			expectedPath: "/usr/lib/wsl/lib/nvidia-smi",
		},
		{
			description: "earlier search path is preferred when multiple exist",
			contents: map[string]string{
				"/usr/bin/nvidia-smi":  "fake nvidia-smi in usr/bin",
				"/usr/sbin/nvidia-smi": "fake nvidia-smi in usr/sbin",
			},
			expectedPath: "/usr/bin/nvidia-smi",
		},
		{
			description: "/opt/bin is searched last",
			contents: map[string]string{
				"/opt/bin/nvidia-smi":  "fake nvidia-smi in opt/bin",
				"/usr/sbin/nvidia-smi": "fake nvidia-smi in usr/sbin",
			},
			expectedPath: "/usr/sbin/nvidia-smi",
		},
		{
			description: "non-executable nvidia-smi is skipped",
			contents: map[string]string{
				"/usr/bin/nvidia-smi": "fake nvidia-smi",
			},
			perms: map[string]os.FileMode{
				"/usr/bin/nvidia-smi": 0600,
			},
			expectsError: true,
		},
		{
			description: "empty nvidia-smi is skipped",
			contents: map[string]string{
				"/usr/bin/nvidia-smi": "",
			},
			expectsError: true,
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

				if after, ok := strings.CutPrefix(contents, "symlink="); ok {
					require.NoError(t, os.Symlink(after, target))
					continue
				}

				mode := os.FileMode(0755)
				if m, ok := tc.perms[name]; ok {
					mode = m
				}
				require.NoError(t, os.WriteFile(target, []byte(contents), mode))
			}

			nvidiaSMIPath, err := resolveHostNvidiaSMI(hostRoot)
			if tc.expectsError {
				require.Error(t, err)
				return
			}

			require.NoError(t, err)
			require.Equal(t, tc.expectedPath, nvidiaSMIPath)
		})
	}
}
