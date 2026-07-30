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

package images

import (
	"testing"

	v1 "github.com/NVIDIA/gpu-operator/api/nvidia/v1"
)

func newClusterPolicySpec() *v1.ClusterPolicySpec {
	return &v1.ClusterPolicySpec{
		Driver: v1.DriverSpec{
			Repository: "nvcr.io/nvidia",
			Image:      "driver",
			Version:    "550.127.05",
		},
		Toolkit: v1.ToolkitSpec{
			Repository: "nvcr.io/nvidia/k8s",
			Image:      "container-toolkit",
			Version:    "v1.16.1",
		},
		DevicePlugin: v1.DevicePluginSpec{
			Repository: "nvcr.io/nvidia",
			Image:      "k8s-device-plugin",
			Version:    "v0.16.1",
		},
		DCGMExporter: v1.DCGMExporterSpec{
			Repository: "nvcr.io/nvidia/k8s",
			Image:      "dcgm-exporter",
			Version:    "3.3.6",
		},
		DCGM: v1.DCGMSpec{
			Repository: "nvcr.io/nvidia/cloud-native",
			Image:      "dcgm",
			Version:    "3.3.6",
		},
		GPUFeatureDiscovery: v1.GPUFeatureDiscoverySpec{
			Repository: "nvcr.io/nvidia",
			Image:      "gpu-feature-discovery",
			Version:    "v0.16.1",
		},
		MIGManager: v1.MIGManagerSpec{
			Repository: "nvcr.io/nvidia/cloud-native",
			Image:      "k8s-mig-manager",
			Version:    "v0.8.0",
		},
		GPUDirectStorage: &v1.GPUDirectStorageSpec{
			Repository: "nvcr.io/nvidia/cloud-native",
			Image:      "nvidia-fs",
			Version:    "2.20.5",
		},
		VFIOManager: v1.VFIOManagerSpec{
			Repository: "nvcr.io/nvidia",
			Image:      "vfio-manager",
			Version:    "v0.4.0",
		},
		SandboxDevicePlugin: v1.SandboxDevicePluginSpec{
			Repository: "nvcr.io/nvidia",
			Image:      "kubevirt-gpu-device-plugin",
			Version:    "v1.2.7",
		},
		VGPUDeviceManager: v1.VGPUDeviceManagerSpec{
			Repository: "nvcr.io/nvidia/cloud-native",
			Image:      "vgpu-device-manager",
			Version:    "v0.2.7",
		},
	}
}

func Test_FromClusterPolicy(t *testing.T) {
	tests := []struct {
		name       string
		spec       *v1.ClusterPolicySpec
		wantImages []OperandImage
	}{
		{
			name: "constructs image paths from repository, image, and version",
			spec: newClusterPolicySpec(),
			wantImages: []OperandImage{
				{Name: "Driver", Image: "nvcr.io/nvidia/driver:550.127.05"},
				{Name: "Toolkit", Image: "nvcr.io/nvidia/k8s/container-toolkit:v1.16.1"},
				{Name: "DevicePlugin", Image: "nvcr.io/nvidia/k8s-device-plugin:v0.16.1"},
				{Name: "DCGMExporter", Image: "nvcr.io/nvidia/k8s/dcgm-exporter:3.3.6"},
				{Name: "DCGM", Image: "nvcr.io/nvidia/cloud-native/dcgm:3.3.6"},
				{Name: "GPUFeatureDiscovery", Image: "nvcr.io/nvidia/gpu-feature-discovery:v0.16.1"},
				{Name: "MIGManager", Image: "nvcr.io/nvidia/cloud-native/k8s-mig-manager:v0.8.0"},
				{Name: "GPUDirectStorage", Image: "nvcr.io/nvidia/cloud-native/nvidia-fs:2.20.5"},
				{Name: "VFIOManager", Image: "nvcr.io/nvidia/vfio-manager:v0.4.0"},
				{Name: "SandboxDevicePlugin", Image: "nvcr.io/nvidia/kubevirt-gpu-device-plugin:v1.2.7"},
				{Name: "VGPUDeviceManager", Image: "nvcr.io/nvidia/cloud-native/vgpu-device-manager:v0.2.7"},
			},
		},
		{
			name: "uses image as full path when repository and version are empty",
			spec: func() *v1.ClusterPolicySpec {
				s := newClusterPolicySpec()
				s.Driver = v1.DriverSpec{
					Image: "nvcr.io/nvidia/driver:550.127.05",
				}
				return s
			}(),
			wantImages: []OperandImage{
				{Name: "Driver", Image: "nvcr.io/nvidia/driver:550.127.05"},
				{Name: "Toolkit", Image: "nvcr.io/nvidia/k8s/container-toolkit:v1.16.1"},
				{Name: "DevicePlugin", Image: "nvcr.io/nvidia/k8s-device-plugin:v0.16.1"},
				{Name: "DCGMExporter", Image: "nvcr.io/nvidia/k8s/dcgm-exporter:3.3.6"},
				{Name: "DCGM", Image: "nvcr.io/nvidia/cloud-native/dcgm:3.3.6"},
				{Name: "GPUFeatureDiscovery", Image: "nvcr.io/nvidia/gpu-feature-discovery:v0.16.1"},
				{Name: "MIGManager", Image: "nvcr.io/nvidia/cloud-native/k8s-mig-manager:v0.8.0"},
				{Name: "GPUDirectStorage", Image: "nvcr.io/nvidia/cloud-native/nvidia-fs:2.20.5"},
				{Name: "VFIOManager", Image: "nvcr.io/nvidia/vfio-manager:v0.4.0"},
				{Name: "SandboxDevicePlugin", Image: "nvcr.io/nvidia/kubevirt-gpu-device-plugin:v1.2.7"},
				{Name: "VGPUDeviceManager", Image: "nvcr.io/nvidia/cloud-native/vgpu-device-manager:v0.2.7"},
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := FromClusterPolicy(tt.spec)
			if err != nil {
				t.Fatalf("FromClusterPolicy() unexpected error: %v", err)
			}
			if len(got) != len(tt.wantImages) {
				t.Fatalf("FromClusterPolicy() returned %d images, want %d", len(got), len(tt.wantImages))
			}
			for i, op := range got {
				if op.Name != tt.wantImages[i].Name {
					t.Errorf("FromClusterPolicy()[%d].Name = %q, want %q", i, op.Name, tt.wantImages[i].Name)
				}
				if op.Image != tt.wantImages[i].Image {
					t.Errorf("FromClusterPolicy()[%d].Image = %q, want %q", i, op.Image, tt.wantImages[i].Image)
				}
			}
		})
	}
}
