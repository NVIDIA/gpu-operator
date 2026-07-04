/*
 * Copyright (c) 2021, NVIDIA CORPORATION.  All rights reserved.
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
	"context"
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

			fileInfo, err := resolveHostNvidiaSMI(hostRoot)
			if tc.expectsError {
				require.Error(t, err)
				return
			}

			require.NoError(t, err)
			require.NotZero(t, fileInfo.Size())
		})
	}
}

func Test_isValidComponent(t *testing.T) {
	tests := []struct {
		name      string
		component string
		want      bool
	}{
		{
			name:      "valid driver component",
			component: "driver",
			want:      true,
		},
		{
			name:      "valid cuda component",
			component: "cuda",
			want:      true,
		},
		{
			name:      "valid plugin component",
			component: "plugin",
			want:      true,
		},
		{
			name:      "valid toolkit component",
			component: "toolkit",
			want:      true,
		},
		{
			name:      "valid nvidia-fs component using constant",
			component: NVIDIAFS,
			want:      true,
		},
		{
			name:      "valid gdrcopy component using constant",
			component: GDRCOPY,
			want:      true,
		},
		{
			name:      "valid nvidia-peermem component using constant",
			component: NVIDIAPEERMEM,
			want:      true,
		},
		{
			name:      "valid mofed component",
			component: "mofed",
			want:      true,
		},
		{
			name:      "valid vgpu-manager component",
			component: "vgpu-manager",
			want:      true,
		},
		{
			name:      "valid vgpu-devices component",
			component: "vgpu-devices",
			want:      true,
		},
		{
			name:      "valid cc-manager component",
			component: "cc-manager",
			want:      true,
		},
		{
			name:      "invalid empty component",
			component: "",
			want:      false,
		},
		{
			name:      "invalid unknown component",
			component: "unknown",
			want:      false,
		},
		{
			name:      "invalid random string",
			component: "foobar",
			want:      false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Temporarily set componentFlag for the test
			originalComponent := componentFlag
			componentFlag = tt.component
			defer func() { componentFlag = originalComponent }()

			got := isValidComponent()
			if got != tt.want {
				t.Errorf("isValidComponent() = %v, want %v", got, tt.want)
			}
		})
	}
}

func Test_validateAdditionalDriverComponents(t *testing.T) {
	tests := []struct {
		name           string
		statusFileData string
		createFile     bool
		wantErr        bool
	}{
		{
			name:       "status file does not exist",
			createFile: false,
			wantErr:    true,
		},
		{
			name: "all features disabled",
			statusFileData: `GDRCOPY_ENABLED: false
GDS_ENABLED: false
GPU_DIRECT_RDMA_ENABLED: false`,
			createFile: true,
			wantErr:    false,
		},
		{
			name: "GDRCOPY enabled",
			statusFileData: `GDRCOPY_ENABLED: true
GDS_ENABLED: false
GPU_DIRECT_RDMA_ENABLED: false`,
			createFile: true,
			wantErr:    true, // will fail validation without actual kernel module
		},
		{
			name: "GDS (nvidia-fs) enabled",
			statusFileData: `GDRCOPY_ENABLED: false
GDS_ENABLED: true
GPU_DIRECT_RDMA_ENABLED: false`,
			createFile: true,
			wantErr:    true, // will fail validation without actual kernel module
		},
		{
			name: "GPU_DIRECT_RDMA (nvidia-peermem) enabled",
			statusFileData: `GDRCOPY_ENABLED: false
GDS_ENABLED: false
GPU_DIRECT_RDMA_ENABLED: true`,
			createFile: true,
			wantErr:    true, // will fail validation without actual kernel module
		},
		{
			name: "all features enabled",
			statusFileData: `GDRCOPY_ENABLED: true
GDS_ENABLED: true
GPU_DIRECT_RDMA_ENABLED: true`,
			createFile: true,
			wantErr:    true, // will fail validation without actual kernel modules
		},
		{
			name: "unknown feature flag is ignored",
			statusFileData: `GDRCOPY_ENABLED: false
GDS_ENABLED: false
GPU_DIRECT_RDMA_ENABLED: false
UNKNOWN_FEATURE: true`,
			createFile: true,
			wantErr:    false,
		},
		{
			name:           "invalid YAML format",
			statusFileData: `invalid yaml content {{{`,
			createFile:     true,
			wantErr:        true,
		},
		{
			name:           "empty status file",
			statusFileData: ``,
			createFile:     true,
			wantErr:        false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Create a temporary directory for the test
			tmpDir := t.TempDir()
			testStatusFile := tmpDir + "/.driver-ctr-ready"

			// Create the status file if needed
			if tt.createFile {
				err := os.WriteFile(testStatusFile, []byte(tt.statusFileData), 0600)
				if err != nil {
					t.Fatalf("Failed to create test status file: %v", err)
				}
			}

			err := validateAdditionalDriverComponents(context.Background(), testStatusFile)

			if tt.wantErr {
				if err == nil {
					t.Errorf("validateAdditionalDriverComponents() expected error but got none")
				}
			} else {
				if err != nil {
					t.Errorf("validateAdditionalDriverComponents() unexpected error: %v", err)
				}
			}
		})
	}
}

// TestNormalizePCIAddress verifies that PCI addresses coming from nvidia-smi
// (8-hex-digit domain, upper case) and go-nvlib (4-hex-digit domain, lower
// case) normalize to the same key, since commitMIGMode joins the two sources on
// this key to decide which GPU to reset.
func TestNormalizePCIAddress(t *testing.T) {
	testCases := []struct {
		description string
		address     string
		want        string
	}{
		{
			description: "nvidia-smi form with 8-digit domain",
			address:     "00000000:41:00.0",
			want:        "0000:41:00.0",
		},
		{
			description: "go-nvlib form is unchanged",
			address:     "0000:41:00.0",
			want:        "0000:41:00.0",
		},
		{
			description: "uppercase is lowercased",
			address:     "0000:C1:00.0",
			want:        "0000:c1:00.0",
		},
		{
			description: "surrounding whitespace is trimmed",
			address:     " 00000000:41:00.0 ",
			want:        "0000:41:00.0",
		},
		{
			description: "non-zero domain is preserved",
			address:     "00010000:41:00.0",
			want:        "10000:41:00.0",
		},
		{
			description: "malformed input is passed through lowercased",
			address:     "not-an-address",
			want:        "not-an-address",
		},
	}

	for _, tc := range testCases {
		t.Run(tc.description, func(t *testing.T) {
			require.Equal(t, tc.want, normalizePCIAddress(tc.address))
		})
	}
}

// TestParseMIGModes verifies parsing of the nvidia-smi MIG-mode CSV, including
// address normalization, that malformed rows are skipped, and that only a
// pending-but-uncommitted enable is flagged via needsCommit — the guard
// commitMIGMode uses to decide whether a GPU reset is warranted.
func TestParseMIGModes(t *testing.T) {
	output := strings.Join([]string{
		"00000000:41:00.0, Disabled, Enabled",
		"00000000:C1:00.0, Enabled, Enabled",
		"0000:81:00.0, Disabled, Disabled",
		"0000:a1:00.0, [N/A], [N/A]",
		"",
		"malformed line without enough fields",
	}, "\n")

	modes := parseMIGModes(output)

	require.Len(t, modes, 4)
	require.Equal(t, migMode{current: "Disabled", pending: "Enabled"}, modes["0000:41:00.0"])
	require.Equal(t, migMode{current: "Enabled", pending: "Enabled"}, modes["0000:c1:00.0"])
	require.Equal(t, migMode{current: "Disabled", pending: "Disabled"}, modes["0000:81:00.0"])
	require.Equal(t, migMode{current: "[N/A]", pending: "[N/A]"}, modes["0000:a1:00.0"])

	// Only the GPU with a requested-but-not-applied enable needs a reset to
	// commit; already-committed, disabled, and unsupported GPUs do not.
	require.True(t, modes["0000:41:00.0"].needsCommit(), "pending enable, not yet current")
	require.False(t, modes["0000:c1:00.0"].needsCommit(), "already enabled")
	require.False(t, modes["0000:81:00.0"].needsCommit(), "disabled")
	require.False(t, modes["0000:a1:00.0"].needsCommit(), "MIG not supported")
}

// TestMIGModeNeedsCommit exercises needsCommit directly across the value
// combinations nvidia-smi can report, including case-insensitivity.
func TestMIGModeNeedsCommit(t *testing.T) {
	testCases := []struct {
		description string
		mode        migMode
		want        bool
	}{
		{
			description: "pending enable not yet committed",
			mode:        migMode{current: "Disabled", pending: "Enabled"},
			want:        true,
		},
		{
			description: "already enabled",
			mode:        migMode{current: "Enabled", pending: "Enabled"},
			want:        false,
		},
		{
			description: "no pending change while disabled",
			mode:        migMode{current: "Disabled", pending: "Disabled"},
			want:        false,
		},
		{
			description: "pending disable is out of scope",
			mode:        migMode{current: "Enabled", pending: "Disabled"},
			want:        false,
		},
		{
			description: "MIG unsupported",
			mode:        migMode{current: "[N/A]", pending: "[N/A]"},
			want:        false,
		},
		{
			description: "case-insensitive match",
			mode:        migMode{current: "disabled", pending: "enabled"},
			want:        true,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.description, func(t *testing.T) {
			require.Equal(t, tc.want, tc.mode.needsCommit())
		})
	}
}

// TestShouldResetForMIGCommit exercises the single decision point behind the
// destructive GPU reset across the guard matrix, since a wrong combination
// would either reset a GPU carrying a live workload or fail to recover MIG mode
// after a reboot. A reset is warranted only for an uncommitted MIG-mode enable
// on a GPU with no VFs and no running workload.
func TestShouldResetForMIGCommit(t *testing.T) {
	pendingEnable := migMode{current: "Disabled", pending: "Enabled"}

	testCases := []struct {
		description string
		mode        migMode
		numVFs      uint64
		busy        bool
		want        bool
	}{
		{
			description: "uncommitted enable, no VFs, idle",
			mode:        pendingEnable,
			numVFs:      0,
			busy:        false,
			want:        true,
		},
		{
			description: "VFs still enabled (vGPU VM attached)",
			mode:        pendingEnable,
			numVFs:      16,
			busy:        false,
			want:        false,
		},
		{
			description: "running compute process",
			mode:        pendingEnable,
			numVFs:      0,
			busy:        true,
			want:        false,
		},
		{
			description: "VFs enabled and busy",
			mode:        pendingEnable,
			numVFs:      16,
			busy:        true,
			want:        false,
		},
		{
			description: "MIG already committed",
			mode:        migMode{current: "Enabled", pending: "Enabled"},
			numVFs:      0,
			busy:        false,
			want:        false,
		},
		{
			description: "no pending MIG-mode change",
			mode:        migMode{current: "Disabled", pending: "Disabled"},
			numVFs:      0,
			busy:        false,
			want:        false,
		},
		{
			description: "MIG unsupported",
			mode:        migMode{current: "[N/A]", pending: "[N/A]"},
			numVFs:      0,
			busy:        false,
			want:        false,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.description, func(t *testing.T) {
			require.Equal(t, tc.want, shouldResetForMIGCommit(tc.mode, tc.numVFs, tc.busy))
		})
	}
}
