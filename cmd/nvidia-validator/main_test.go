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

	"github.com/NVIDIA/go-nvlib/pkg/nvpci"
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

// pfDevice returns an SR-IOV physical function GPU with the given VF counts.
func pfDevice(address string, totalVFs, numVFs uint64) *nvpci.NvidiaPCIDevice {
	return &nvpci.NvidiaPCIDevice{
		Address: address,
		SriovInfo: nvpci.SriovInfo{
			PhysicalFunction: &nvpci.SriovPhysicalFunction{
				TotalVFs: totalVFs,
				NumVFs:   numVFs,
			},
		},
	}
}

// TestCountVFs verifies the shared VF-accounting helper that drives both the
// idempotency guard in enableVFs and the readiness check in waitForVFs. Getting
// this wrong would either skip a needed 'sriov-manage -e' (VFs never come back
// after a reboot) or disturb VFs already assigned to running VMs, so the guard
// (totalEnabled >= totalExpected) is exercised across the boundary cases.
func TestCountVFs(t *testing.T) {
	testCases := []struct {
		description       string
		gpus              []*nvpci.NvidiaPCIDevice
		wantExpected      uint64
		wantEnabled       uint64
		wantPFCount       int
		wantNeedsEnabling bool
	}{
		{
			description:       "no SR-IOV capable GPUs",
			gpus:              []*nvpci.NvidiaPCIDevice{{Address: "0000:41:00.0"}},
			wantExpected:      0,
			wantEnabled:       0,
			wantPFCount:       0,
			wantNeedsEnabling: false,
		},
		{
			description:       "VFs missing after reboot",
			gpus:              []*nvpci.NvidiaPCIDevice{pfDevice("0000:41:00.0", 16, 0)},
			wantExpected:      16,
			wantEnabled:       0,
			wantPFCount:       1,
			wantNeedsEnabling: true,
		},
		{
			description:       "VFs fully enabled",
			gpus:              []*nvpci.NvidiaPCIDevice{pfDevice("0000:41:00.0", 16, 16)},
			wantExpected:      16,
			wantEnabled:       16,
			wantPFCount:       1,
			wantNeedsEnabling: false,
		},
		{
			description: "partially enabled across multiple PFs",
			gpus: []*nvpci.NvidiaPCIDevice{
				pfDevice("0000:41:00.0", 16, 16),
				pfDevice("0000:c1:00.0", 16, 0),
			},
			wantExpected:      32,
			wantEnabled:       16,
			wantPFCount:       2,
			wantNeedsEnabling: true,
		},
		{
			description: "virtual functions are not counted as PFs",
			gpus: []*nvpci.NvidiaPCIDevice{
				pfDevice("0000:41:00.0", 16, 16),
				{
					Address: "0000:41:00.4",
					SriovInfo: nvpci.SriovInfo{
						VirtualFunction: &nvpci.SriovVirtualFunction{},
					},
				},
			},
			wantExpected:      16,
			wantEnabled:       16,
			wantPFCount:       1,
			wantNeedsEnabling: false,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.description, func(t *testing.T) {
			totalExpected, totalEnabled, pfCount := countVFs(tc.gpus)
			require.Equal(t, tc.wantExpected, totalExpected, "totalExpected")
			require.Equal(t, tc.wantEnabled, totalEnabled, "totalEnabled")
			require.Equal(t, tc.wantPFCount, pfCount, "pfCount")

			// This mirrors the guard enableVFs uses to decide whether to invoke
			// sriov-manage: enable only when there is at least one SR-IOV GPU and
			// not every VF is already present.
			needsEnabling := totalExpected > 0 && totalEnabled < totalExpected
			require.Equal(t, tc.wantNeedsEnabling, needsEnabling, "needsEnabling")
		})
	}
}
