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
