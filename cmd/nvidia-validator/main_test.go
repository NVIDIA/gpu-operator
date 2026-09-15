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
	"os"
	"testing"

	"github.com/NVIDIA/go-nvlib/pkg/nvmdev"
	"github.com/NVIDIA/go-nvlib/pkg/nvpci"
	"github.com/stretchr/testify/require"
)

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

func newTestPF(totalVFs, numVFs uint64) *nvpci.NvidiaPCIDevice {
	return &nvpci.NvidiaPCIDevice{
		SriovInfo: nvpci.SriovInfo{
			PhysicalFunction: &nvpci.SriovPhysicalFunction{
				TotalVFs: totalVFs,
				NumVFs:   numVFs,
			},
		},
	}
}

func TestIsDriverUsingSRIOV(t *testing.T) {
	nonSriovGPU := &nvpci.NvidiaPCIDevice{}

	testCases := []struct {
		description string
		gpus        []*nvpci.NvidiaPCIDevice
		expected    bool
	}{
		{
			description: "no GPUs",
			gpus:        nil,
			expected:    false,
		},
		{
			description: "non-SRIOV GPU",
			gpus:        []*nvpci.NvidiaPCIDevice{nonSriovGPU},
			expected:    false,
		},
		{
			description: "SRIOV-capable PF with no VFs enabled",
			gpus:        []*nvpci.NvidiaPCIDevice{newTestPF(16, 0)},
			expected:    false,
		},
		{
			description: "PF with VFs enabled",
			gpus:        []*nvpci.NvidiaPCIDevice{newTestPF(16, 16)},
			expected:    true,
		},
		{
			description: "mixed non-SRIOV GPU and PF with VFs enabled",
			gpus:        []*nvpci.NvidiaPCIDevice{nonSriovGPU, newTestPF(16, 4)},
			expected:    true,
		},
	}
	for _, tc := range testCases {
		t.Run(tc.description, func(t *testing.T) {
			require.Equal(t, tc.expected, isDriverUsingSRIOV(tc.gpus))
		})
	}
}

func TestAreAllVFsReady(t *testing.T) {
	testCases := []struct {
		description string
		gpus        []*nvpci.NvidiaPCIDevice
		expected    bool
	}{
		{
			description: "no GPUs",
			gpus:        nil,
			expected:    false,
		},
		{
			description: "non-SRIOV GPU only",
			gpus:        []*nvpci.NvidiaPCIDevice{{}},
			expected:    false,
		},
		{
			description: "PF with only some VFs enabled",
			gpus:        []*nvpci.NvidiaPCIDevice{newTestPF(16, 4)},
			expected:    false,
		},
		{
			description: "PF with all VFs enabled",
			gpus:        []*nvpci.NvidiaPCIDevice{newTestPF(16, 16)},
			expected:    true,
		},
		{
			description: "one PF complete, one PF incomplete",
			gpus:        []*nvpci.NvidiaPCIDevice{newTestPF(16, 16), newTestPF(16, 0)},
			expected:    false,
		},
		{
			description: "all PFs complete",
			gpus:        []*nvpci.NvidiaPCIDevice{newTestPF(16, 16), newTestPF(8, 8)},
			expected:    true,
		},
	}
	for _, tc := range testCases {
		t.Run(tc.description, func(t *testing.T) {
			require.Equal(t, tc.expected, AreAllVFsReady(tc.gpus))
		})
	}
}

func TestMdevParentDevicesExist(t *testing.T) {
	mock, err := nvmdev.NewMock()
	require.NoError(t, err)
	defer mock.Cleanup()

	require.False(t, mdevParentDevicesExist(mock))

	require.NoError(t, mock.AddMockA100Parent("0000:3b:00.0", 0))
	require.True(t, mdevParentDevicesExist(mock))
}

func TestCountNvidiaDevices(t *testing.T) {
	testCases := []struct {
		name     string
		output   string
		expected int
	}{
		{name: "empty output", expected: 0},
		{name: "no NVIDIA devices", output: "Intel Corporation\nAMD", expected: 0},
		{name: "multiple NVIDIA devices", output: "NVIDIA GPU\nIntel\nnvidia controller", expected: 2},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			require.Equal(t, tc.expected, countNvidiaDevices(tc.output))
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
// pending-but-uncommitted enable is flagged via needsCommit, one of the
// conditions commitMIGMode requires before a reset.
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

	// Only the GPU with a requested-but-not-applied enable has an uncommitted
	// mode; already-committed, disabled, and unsupported GPUs do not.
	require.True(t, modes["0000:41:00.0"].needsCommit(), "pending enable, not yet current")
	require.False(t, modes["0000:c1:00.0"].needsCommit(), "already enabled")
	require.False(t, modes["0000:81:00.0"].needsCommit(), "disabled")
	require.False(t, modes["0000:a1:00.0"].needsCommit(), "MIG not supported")
}

// TestMIGModeNeedsCommit exercises needsCommit directly across representative
// value combinations, including case-insensitivity.
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

// TestShouldResetForMIGCommit exercises shouldResetForMIGCommit across the
// combinations that matter, since a wrong combination would either reset a GPU
// running compute work or fail to commit a pending MIG-mode enable. A reset is
// warranted only for an uncommitted MIG-mode enable on a GPU with no VFs and
// no running compute processes.
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
			description: "VFs still enabled",
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

// TestVGPUNvidiaSMI pins that the host-driver path hands back the RESOLVED
// nvidia-smi path rather than a bare command name. Everything commitMIGMode
// runs (the MIG-mode query, the running-process probe and the reset itself)
// takes its binary from here, and chroot resolves a bare name through the
// validator's own PATH. Two of the locations searched on the host (/opt/bin
// and the WSL path) are not on the image's PATH, so regressing this makes the
// opt-in feature a no-op on hosts where nvidia-smi lives only there.
func TestVGPUNvidiaSMI(t *testing.T) {
	testCases := []struct {
		description    string
		hostDriver     bool
		contents       map[string]string
		wantDriverRoot string
		wantNvidiaSMI  string
		expectsError   bool
	}{
		{
			description:    "container driver uses the driver install dir",
			hostDriver:     false,
			wantDriverRoot: defaultDriverInstallDir,
			wantNvidiaSMI:  "nvidia-smi",
		},
		{
			description: "host driver on the default PATH still resolves to a full path",
			hostDriver:  true,
			contents: map[string]string{
				"/usr/bin/nvidia-smi": "fake nvidia-smi",
			},
			wantDriverRoot: "/host",
			wantNvidiaSMI:  "/usr/bin/nvidia-smi",
		},
		{
			description: "host driver outside the default PATH resolves rather than falling back",
			hostDriver:  true,
			contents: map[string]string{
				"/opt/bin/nvidia-smi": "fake nvidia-smi",
			},
			wantDriverRoot: "/host",
			wantNvidiaSMI:  "/opt/bin/nvidia-smi",
		},
		{
			description: "host driver on the WSL path resolves rather than falling back",
			hostDriver:  true,
			contents: map[string]string{
				wslNvidiaSMIPath: "fake nvidia-smi",
			},
			wantDriverRoot: "/host",
			wantNvidiaSMI:  wslNvidiaSMIPath,
		},
		{
			description:  "host driver with no nvidia-smi errors instead of guessing",
			hostDriver:   true,
			expectsError: true,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.description, func(t *testing.T) {
			hostRoot := t.TempDir()
			// resolveHostNvidiaSMI only accepts an executable file, so the
			// fixture has to be written executable.
			mode := os.FileMode(0755)
			for name, contents := range tc.contents {
				target := filepath.Join(hostRoot, name)
				require.NoError(t, os.MkdirAll(filepath.Dir(target), 0755))
				require.NoError(t, os.WriteFile(target, []byte(contents), mode))
			}

			driverRoot, nvidiaSMI, err := vgpuNvidiaSMI(tc.hostDriver, hostRoot)
			if tc.expectsError {
				require.Error(t, err)
				return
			}

			require.NoError(t, err)
			require.Equal(t, tc.wantDriverRoot, driverRoot, "driverRoot")
			require.Equal(t, tc.wantNvidiaSMI, nvidiaSMI, "nvidiaSMI")
		})
	}
}
