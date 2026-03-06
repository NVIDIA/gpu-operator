/**
# Copyright (c), NVIDIA CORPORATION.  All rights reserved.
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

package listimages

import (
	"context"
	"os"
	"testing"

	"github.com/sirupsen/logrus"
)

func Test_getContents(t *testing.T) {
	tests := []struct {
		name       string
		fileData   string
		createFile bool
		wantErr    bool
	}{
		{
			name:       "reads from file",
			fileData:   "apiVersion: v1\nkind: ClusterPolicy\n",
			createFile: true,
			wantErr:    false,
		},
		{
			name:       "file does not exist",
			createFile: false,
			wantErr:    true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tmpDir := t.TempDir()
			testFile := tmpDir + "/input.yaml"

			if tt.createFile {
				err := os.WriteFile(testFile, []byte(tt.fileData), 0600)
				if err != nil {
					t.Fatalf("Failed to create test file: %v", err)
				}
			}

			got, err := getContents(testFile)
			if tt.wantErr {
				if err == nil {
					t.Errorf("getContents() expected error but got none")
				}
				return
			}
			if err != nil {
				t.Fatalf("getContents() unexpected error: %v", err)
			}
			if string(got) != tt.fileData {
				t.Errorf("getContents() = %q, want %q", string(got), tt.fileData)
			}
		})
	}
}

func Test_buildCSV(t *testing.T) {
	tests := []struct {
		name       string
		fileData   string
		createFile bool
		wantErr    bool
	}{
		{
			name: "valid CSV with related images and container env vars",
			fileData: `apiVersion: operators.coreos.com/v1alpha1
kind: ClusterServiceVersion
spec:
  relatedImages:
    - image: nvcr.io/nvidia/gpu-operator:v24.9.0
  install:
    strategy: deployment
    spec:
      deployments:
        - spec:
            template:
              spec:
                containers:
                  - image: nvcr.io/nvidia/gpu-operator:v24.9.0
                    env:
                      - name: DRIVER_IMAGE
                        value: nvcr.io/nvidia/driver:550
`,
			createFile: true,
			wantErr:    false,
		},
		{
			name:       "invalid YAML",
			fileData:   `{{{ not yaml`,
			createFile: true,
			wantErr:    true,
		},
		{
			name:       "file does not exist",
			createFile: false,
			wantErr:    true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tmpDir := t.TempDir()
			testFile := tmpDir + "/csv.yaml"

			if tt.createFile {
				err := os.WriteFile(testFile, []byte(tt.fileData), 0600)
				if err != nil {
					t.Fatalf("Failed to create test file: %v", err)
				}
			}

			cmd := buildCSV()
			err := cmd.Run(context.Background(), []string{"csv", "--input", testFile})

			if tt.wantErr {
				if err == nil {
					t.Errorf("buildCSV() expected error but got none")
				}
			} else {
				if err != nil {
					t.Errorf("buildCSV() unexpected error: %v", err)
				}
			}
		})
	}
}

func Test_buildClusterPolicy(t *testing.T) {
	tests := []struct {
		name       string
		fileData   string
		createFile bool
		wantErr    bool
	}{
		{
			name: "valid ClusterPolicy",
			fileData: `apiVersion: nvidia.com/v1
kind: ClusterPolicy
spec:
  driver:
    repository: nvcr.io/nvidia
    image: driver
    version: "550.127.05"
  toolkit:
    repository: nvcr.io/nvidia/k8s
    image: container-toolkit
    version: v1.16.1
  devicePlugin:
    repository: nvcr.io/nvidia
    image: k8s-device-plugin
    version: v0.16.1
  dcgmExporter:
    repository: nvcr.io/nvidia/k8s
    image: dcgm-exporter
    version: "3.3.6"
  dcgm:
    repository: nvcr.io/nvidia/cloud-native
    image: dcgm
    version: "3.3.6"
  gfd:
    repository: nvcr.io/nvidia
    image: gpu-feature-discovery
    version: v0.16.1
  migManager:
    repository: nvcr.io/nvidia/cloud-native
    image: k8s-mig-manager
    version: v0.8.0
  gds:
    repository: nvcr.io/nvidia/cloud-native
    image: nvidia-fs
    version: "2.20.5"
  vfioManager:
    repository: nvcr.io/nvidia
    image: vfio-manager
    version: v0.4.0
  sandboxDevicePlugin:
    repository: nvcr.io/nvidia
    image: kubevirt-gpu-device-plugin
    version: v1.2.7
  vgpuDeviceManager:
    repository: nvcr.io/nvidia/cloud-native
    image: vgpu-device-manager
    version: v0.2.7
`,
			createFile: true,
			wantErr:    false,
		},
		{
			name:       "invalid YAML",
			fileData:   `{{{ not yaml`,
			createFile: true,
			wantErr:    true,
		},
		{
			name:       "file does not exist",
			createFile: false,
			wantErr:    true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tmpDir := t.TempDir()
			testFile := tmpDir + "/clusterpolicy.yaml"

			if tt.createFile {
				err := os.WriteFile(testFile, []byte(tt.fileData), 0600)
				if err != nil {
					t.Fatalf("Failed to create test file: %v", err)
				}
			}

			cmd := buildClusterPolicy()
			err := cmd.Run(context.Background(), []string{"clusterpolicy", "--input", testFile})

			if tt.wantErr {
				if err == nil {
					t.Errorf("buildClusterPolicy() expected error but got none")
				}
			} else {
				if err != nil {
					t.Errorf("buildClusterPolicy() unexpected error: %v", err)
				}
			}
		})
	}
}

func Test_NewCommand(t *testing.T) {
	logger := logrus.New()
	cmd := NewCommand(logger)

	if cmd.Name != "list-images" {
		t.Errorf("NewCommand().Name = %q, want %q", cmd.Name, "list-images")
	}
	if len(cmd.Commands) != 2 {
		t.Fatalf("NewCommand() has %d subcommands, want 2", len(cmd.Commands))
	}
	if cmd.Commands[0].Name != "csv" {
		t.Errorf("NewCommand().Commands[0].Name = %q, want %q", cmd.Commands[0].Name, "csv")
	}
	if cmd.Commands[1].Name != "clusterpolicy" {
		t.Errorf("NewCommand().Commands[1].Name = %q, want %q", cmd.Commands[1].Name, "clusterpolicy")
	}
}
