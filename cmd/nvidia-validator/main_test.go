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
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/NVIDIA/go-nvlib/pkg/nvmdev"
	"github.com/NVIDIA/go-nvlib/pkg/nvpci"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	meta_v1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func TestLoadPodSpec(t *testing.T) {
	tests := map[string]struct {
		missingFile bool
		manifest    string
		// wantErr is the loadPodSpec message with %s for the manifest path; a wrapped cause may follow after ": ".
		wantErr   string
		wantErrIs error
		wantPod   *corev1.Pod
	}{
		"file cannot be read": {
			missingFile: true,
			wantErr:     "failed to read pod spec %s",
			wantErrIs:   fs.ErrNotExist,
		},
		"file is empty": {
			manifest: "",
			wantErr:  "pod spec %s is missing kind",
		},
		"kind is missing": {
			manifest: "metadata: {}\nspec: {}\n",
			wantErr:  "pod spec %s is missing kind",
		},
		"manifest is not valid YAML": {
			manifest: "kind: [\n",
			wantErr:  "failed to decode pod spec from %s",
		},
		"apiVersion is missing": {
			manifest: "kind: Pod\nmetadata:\n  name: validation\n",
			wantErr:  "failed to decode pod spec from %s",
		},
		"Pod in unregistered group version": {
			manifest: "apiVersion: apps/v1\nkind: Pod\nmetadata:\n  name: validation\n",
			wantErr:  "failed to decode pod spec from %s",
		},
		"kind has wrong case": {
			manifest: "apiVersion: v1\nkind: pod\nmetadata:\n  name: validation\n",
			wantErr:  "failed to decode pod spec from %s",
		},
		"kind is not Pod": {
			manifest: `apiVersion: v1
kind: ConfigMap
metadata:
  name: validation
data:
  key: value
`,
			wantErr: "pod spec %s must have kind Pod",
		},
		"init containers are missing": {
			manifest: `apiVersion: v1
kind: Pod
metadata:
  name: validation
spec:
  containers:
  - name: validation
    image: example:latest
`,
			wantErr: "pod spec %s must have at least one init container and one container",
		},
		"containers are missing": {
			manifest: `apiVersion: v1
kind: Pod
metadata:
  name: validation
spec:
  initContainers:
  - name: init-validation
    image: example:latest
`,
			wantErr: "pod spec %s must have at least one init container and one container",
		},
		"valid pod": {
			manifest: `apiVersion: v1
kind: Pod
metadata:
  name: validation
  namespace: gpu-operator
  labels:
    app: validation
spec:
  restartPolicy: OnFailure
  initContainers:
  - name: init-validation
    image: example:latest
    command: ["sh", "-c", "true"]
  containers:
  - name: validation
    image: example:latest
`,
			wantPod: &corev1.Pod{
				TypeMeta: meta_v1.TypeMeta{APIVersion: "v1", Kind: "Pod"},
				ObjectMeta: meta_v1.ObjectMeta{
					Name:      "validation",
					Namespace: "gpu-operator",
					Labels:    map[string]string{"app": "validation"},
				},
				Spec: corev1.PodSpec{
					RestartPolicy: corev1.RestartPolicyOnFailure,
					InitContainers: []corev1.Container{{
						Name:    "init-validation",
						Image:   "example:latest",
						Command: []string{"sh", "-c", "true"},
					}},
					Containers: []corev1.Container{{
						Name:  "validation",
						Image: "example:latest",
					}},
				},
			},
		},
	}

	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "pod.yaml")
			if !tc.missingFile {
				require.NoError(t, os.WriteFile(path, []byte(tc.manifest), 0600))
			}

			var pod *corev1.Pod
			var err error
			require.NotPanics(t, func() {
				pod, err = loadPodSpec(path)
			})

			if tc.wantErr == "" {
				require.NoError(t, err)
				require.Equal(t, tc.wantPod, pod)
				return
			}
			require.Nil(t, pod)
			require.Error(t, err)
			wantErr := fmt.Sprintf(tc.wantErr, path)
			require.Truef(t, err.Error() == wantErr || strings.HasPrefix(err.Error(), wantErr+": "),
				"error %q does not match %q", err, wantErr)
			if tc.wantErrIs != nil {
				require.ErrorIs(t, err, tc.wantErrIs)
			}
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
