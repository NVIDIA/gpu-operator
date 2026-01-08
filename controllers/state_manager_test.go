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
	"testing"

	"github.com/go-logr/logr"
	"github.com/stretchr/testify/assert"
	corev1 "k8s.io/api/core/v1"

	gpuv1 "github.com/NVIDIA/gpu-operator/api/nvidia/v1"
)

func TestGetRuntimeString(t *testing.T) {
	testCases := []struct {
		description     string
		runtimeVer      string
		expectedRuntime gpuv1.Runtime
	}{
		{
			"containerd",
			"containerd://1.0.0",
			gpuv1.Containerd,
		},
		{
			"docker",
			"docker://1.0.0",
			gpuv1.Docker,
		},
		{
			"crio",
			"cri-o://1.0.0",
			gpuv1.CRIO,
		},
		{
			"unknown",
			"unknown://1.0.0",
			"",
		},
	}

	for _, tc := range testCases {
		t.Run(tc.description, func(t *testing.T) {
			node := corev1.Node{
				Status: corev1.NodeStatus{
					NodeInfo: corev1.NodeSystemInfo{
						ContainerRuntimeVersion: tc.runtimeVer,
					},
				},
			}
			runtime, _ := getRuntimeString(node)
			// TODO: update to use require pkg after MR !311 is merged
			if runtime != tc.expectedRuntime {
				t.Errorf("expected %s but got %s", tc.expectedRuntime.String(), runtime.String())
			}
		})
	}
}

func TestIsValidWorkloadConfig(t *testing.T) {
	tests := []struct {
		config string
		want   bool
	}{
		{gpuWorkloadConfigContainer, true}, {gpuWorkloadConfigVMPassthrough, true}, {gpuWorkloadConfigVMVgpu, true},
		{"invalid", false}, {"", false},
	}
	for _, tc := range tests {
		if got := isValidWorkloadConfig(tc.config); got != tc.want {
			t.Errorf("isValidWorkloadConfig(%q) = %v, want %v", tc.config, got, tc.want)
		}
	}
}

func TestHasOperandsDisabled(t *testing.T) {
	tests := []struct {
		labels map[string]string
		want   bool
	}{
		{map[string]string{commonOperandsLabelKey: "false"}, true},
		{map[string]string{commonOperandsLabelKey: commonOperandsLabelValue}, false},
		{map[string]string{}, false},
	}
	for _, tc := range tests {
		if got := hasOperandsDisabled(tc.labels); got != tc.want {
			t.Errorf("hasOperandsDisabled(%v) = %v, want %v", tc.labels, got, tc.want)
		}
	}
}

func TestHasNFDLabels(t *testing.T) {
	tests := []struct {
		labels map[string]string
		want   bool
	}{
		{map[string]string{nfdLabelPrefix + "cpu": "true"}, true},
		{map[string]string{"other-label": "value"}, false},
		{map[string]string{}, false},
	}
	for _, tc := range tests {
		if got := hasNFDLabels(tc.labels); got != tc.want {
			t.Errorf("hasNFDLabels(%v) = %v, want %v", tc.labels, got, tc.want)
		}
	}
}

func TestHasMIGManagerLabel(t *testing.T) {
	tests := []struct {
		labels map[string]string
		want   bool
	}{
		{map[string]string{migManagerLabelKey: migManagerLabelValue}, true},
		{map[string]string{"other": "value"}, false},
	}
	for _, tc := range tests {
		if got := hasMIGManagerLabel(tc.labels); got != tc.want {
			t.Errorf("hasMIGManagerLabel(%v) = %v, want %v", tc.labels, got, tc.want)
		}
	}
}

func TestHasCommonGPULabel(t *testing.T) {
	tests := []struct {
		labels map[string]string
		want   bool
	}{
		{map[string]string{commonGPULabelKey: commonGPULabelValue}, true},
		{map[string]string{commonGPULabelKey: "false"}, false},
		{map[string]string{}, false},
	}
	for _, tc := range tests {
		if got := hasCommonGPULabel(tc.labels); got != tc.want {
			t.Errorf("hasCommonGPULabel(%v) = %v, want %v", tc.labels, got, tc.want)
		}
	}
}

func TestHasGPULabels(t *testing.T) {
	tests := []struct {
		labels map[string]string
		want   bool
	}{
		{map[string]string{nfdLabelPrefix + "pci-10de.present": "true"}, true},
		{map[string]string{nfdLabelPrefix + "pci-0302_10de.present": "true"}, true},
		{map[string]string{nfdLabelPrefix + "pci-0300_10de.present": "true"}, true},
		{map[string]string{nfdLabelPrefix + "pci-10de.present": "false"}, false},
		{map[string]string{"other": "true"}, false},
	}
	for _, tc := range tests {
		if got := hasGPULabels(tc.labels); got != tc.want {
			t.Errorf("hasGPULabels(%v) = %v, want %v", tc.labels, got, tc.want)
		}
	}
}

func TestHasMIGCapableGPU(t *testing.T) {
	tests := []struct {
		labels map[string]string
		want   bool
	}{
		{map[string]string{migCapableLabelKey: migCapableLabelValue}, true},
		{map[string]string{migCapableLabelKey: "false"}, false},
		{map[string]string{gpuProductLabelKey: "NVIDIA-A100"}, true},
		{map[string]string{gpuProductLabelKey: "NVIDIA-H100"}, true},
		{map[string]string{gpuProductLabelKey: "NVIDIA-A30"}, true},
		{map[string]string{gpuProductLabelKey: "NVIDIA-T4"}, false},
		{map[string]string{vgpuHostDriverLabelKey: "535.54"}, false},
		{map[string]string{}, false},
	}
	for _, tc := range tests {
		if got := hasMIGCapableGPU(tc.labels); got != tc.want {
			t.Errorf("hasMIGCapableGPU(%v) = %v, want %v", tc.labels, got, tc.want)
		}
	}
}

func TestGpuWorkloadConfiguration_ShouldDeployDriverForVMPassthrough(t *testing.T) {
	tests := []struct {
		name          string
		config        string
		clusterPolicy *gpuv1.ClusterPolicy
		expected      bool
	}{
		{
			name:   "non-vm-passthrough workload",
			config: gpuWorkloadConfigContainer,
			clusterPolicy: &gpuv1.ClusterPolicy{
				Spec: gpuv1.ClusterPolicySpec{
					FabricManager: gpuv1.FabricManagerSpec{
						Mode: gpuv1.FabricModeSharedNVSwitch,
					},
				},
			},
			expected: false,
		},
		{
			name:          "vm-passthrough with nil cluster policy",
			config:        gpuWorkloadConfigVMPassthrough,
			clusterPolicy: nil,
			expected:      false,
		},
		{
			name:   "vm-passthrough with shared-nvswitch mode",
			config: gpuWorkloadConfigVMPassthrough,
			clusterPolicy: &gpuv1.ClusterPolicy{
				Spec: gpuv1.ClusterPolicySpec{
					FabricManager: gpuv1.FabricManagerSpec{
						Mode: gpuv1.FabricModeSharedNVSwitch,
					},
				},
			},
			expected: true,
		},
		{
			name:   "vm-passthrough with full-passthrough mode",
			config: gpuWorkloadConfigVMPassthrough,
			clusterPolicy: &gpuv1.ClusterPolicy{
				Spec: gpuv1.ClusterPolicySpec{
					FabricManager: gpuv1.FabricManagerSpec{
						Mode: gpuv1.FabricModeFullPassthrough,
					},
				},
			},
			expected: false,
		},
		{
			name:   "vm-passthrough with default (empty) fabric manager mode",
			config: gpuWorkloadConfigVMPassthrough,
			clusterPolicy: &gpuv1.ClusterPolicy{
				Spec: gpuv1.ClusterPolicySpec{
					FabricManager: gpuv1.FabricManagerSpec{
						Mode: "", // empty defaults to full-passthrough
					},
				},
			},
			expected: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			workloadConfig := &gpuWorkloadConfiguration{
				config:        tt.config,
				node:          "test-node",
				log:           logr.Discard(),
				clusterPolicy: tt.clusterPolicy,
			}

			result := workloadConfig.shouldDeployDriverForVMPassthrough()
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestGpuWorkloadConfiguration_AddGPUStateLabels(t *testing.T) {
	tests := []struct {
		name           string
		config         string
		clusterPolicy  *gpuv1.ClusterPolicy
		inputLabels    map[string]string
		expectedLabels map[string]string
		expectModified bool
	}{
		{
			name:   "vm-passthrough with shared-nvswitch adds driver label",
			config: gpuWorkloadConfigVMPassthrough,
			clusterPolicy: &gpuv1.ClusterPolicy{
				Spec: gpuv1.ClusterPolicySpec{
					FabricManager: gpuv1.FabricManagerSpec{
						Mode: gpuv1.FabricModeSharedNVSwitch,
					},
				},
			},
			inputLabels: map[string]string{},
			expectedLabels: map[string]string{
				"nvidia.com/gpu.deploy.sandbox-device-plugin": "true",
				"nvidia.com/gpu.deploy.sandbox-validator":     "true",
				"nvidia.com/gpu.deploy.vfio-manager":          "true",
				"nvidia.com/gpu.deploy.kata-manager":          "true",
				"nvidia.com/gpu.deploy.cc-manager":            "true",
				"nvidia.com/gpu.deploy.driver":                "true",
			},
			expectModified: true,
		},
		{
			name:   "vm-passthrough with full-passthrough does not add driver label",
			config: gpuWorkloadConfigVMPassthrough,
			clusterPolicy: &gpuv1.ClusterPolicy{
				Spec: gpuv1.ClusterPolicySpec{
					FabricManager: gpuv1.FabricManagerSpec{
						Mode: gpuv1.FabricModeFullPassthrough,
					},
				},
			},
			inputLabels: map[string]string{},
			expectedLabels: map[string]string{
				"nvidia.com/gpu.deploy.sandbox-device-plugin": "true",
				"nvidia.com/gpu.deploy.sandbox-validator":     "true",
				"nvidia.com/gpu.deploy.vfio-manager":          "true",
				"nvidia.com/gpu.deploy.kata-manager":          "true",
				"nvidia.com/gpu.deploy.cc-manager":            "true",
			},
			expectModified: true,
		},
		{
			name:   "container workload is not affected",
			config: gpuWorkloadConfigContainer,
			clusterPolicy: &gpuv1.ClusterPolicy{
				Spec: gpuv1.ClusterPolicySpec{
					FabricManager: gpuv1.FabricManagerSpec{
						Mode: gpuv1.FabricModeSharedNVSwitch,
					},
				},
			},
			inputLabels: map[string]string{
				"existing-label": "value",
			},
			expectedLabels: map[string]string{
				"existing-label":                              "value",
				"nvidia.com/gpu.deploy.driver":                "true",
				"nvidia.com/gpu.deploy.gpu-feature-discovery": "true",
				"nvidia.com/gpu.deploy.container-toolkit":     "true",
				"nvidia.com/gpu.deploy.device-plugin":         "true",
				"nvidia.com/gpu.deploy.dcgm":                  "true",
				"nvidia.com/gpu.deploy.dcgm-exporter":         "true",
				"nvidia.com/gpu.deploy.node-status-exporter":  "true",
				"nvidia.com/gpu.deploy.operator-validator":    "true",
			},
			expectModified: true,
		},
		{
			name:          "vm-passthrough with nil cluster policy does not add driver label",
			config:        gpuWorkloadConfigVMPassthrough,
			clusterPolicy: nil,
			inputLabels:   map[string]string{},
			expectedLabels: map[string]string{
				"nvidia.com/gpu.deploy.sandbox-device-plugin": "true",
				"nvidia.com/gpu.deploy.sandbox-validator":     "true",
				"nvidia.com/gpu.deploy.vfio-manager":          "true",
				"nvidia.com/gpu.deploy.kata-manager":          "true",
				"nvidia.com/gpu.deploy.cc-manager":            "true",
			},
			expectModified: true,
		},
		{
			name:   "driver label already exists - no modification",
			config: gpuWorkloadConfigVMPassthrough,
			clusterPolicy: &gpuv1.ClusterPolicy{
				Spec: gpuv1.ClusterPolicySpec{
					FabricManager: gpuv1.FabricManagerSpec{
						Mode: gpuv1.FabricModeSharedNVSwitch,
					},
				},
			},
			inputLabels: map[string]string{
				"nvidia.com/gpu.deploy.sandbox-device-plugin": "true",
				"nvidia.com/gpu.deploy.sandbox-validator":     "true",
				"nvidia.com/gpu.deploy.vfio-manager":          "true",
				"nvidia.com/gpu.deploy.kata-manager":          "true",
				"nvidia.com/gpu.deploy.cc-manager":            "true",
				"nvidia.com/gpu.deploy.driver":                "true",
			},
			expectedLabels: map[string]string{
				"nvidia.com/gpu.deploy.sandbox-device-plugin": "true",
				"nvidia.com/gpu.deploy.sandbox-validator":     "true",
				"nvidia.com/gpu.deploy.vfio-manager":          "true",
				"nvidia.com/gpu.deploy.kata-manager":          "true",
				"nvidia.com/gpu.deploy.cc-manager":            "true",
				"nvidia.com/gpu.deploy.driver":                "true",
			},
			expectModified: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			workloadConfig := &gpuWorkloadConfiguration{
				config:        tt.config,
				node:          "test-node",
				log:           logr.Discard(),
				clusterPolicy: tt.clusterPolicy,
			}

			// Make a copy of input labels to avoid modifying the test data
			labels := make(map[string]string)
			for k, v := range tt.inputLabels {
				labels[k] = v
			}

			modified := workloadConfig.addGPUStateLabels(labels)

			assert.Equal(t, tt.expectModified, modified)
			assert.Equal(t, tt.expectedLabels, labels)
		})
	}
}

func TestClusterPolicyValidateFabricManagerConfig(t *testing.T) {
	tests := []struct {
		name          string
		clusterPolicy *gpuv1.ClusterPolicySpec
		expectError   bool
		errorMessage  string
	}{
		{
			name: "valid configuration - vm-passthrough with shared-nvswitch and driver enabled",
			clusterPolicy: &gpuv1.ClusterPolicySpec{
				SandboxWorkloads: gpuv1.SandboxWorkloadsSpec{
					DefaultWorkload: "vm-passthrough",
				},
				FabricManager: gpuv1.FabricManagerSpec{
					Mode: gpuv1.FabricModeSharedNVSwitch,
				},
				Driver: gpuv1.DriverSpec{
					Enabled: newBoolPtr(true),
				},
			},
			expectError: false,
		},
		{
			name: "valid configuration - vm-passthrough with full-passthrough mode",
			clusterPolicy: &gpuv1.ClusterPolicySpec{
				SandboxWorkloads: gpuv1.SandboxWorkloadsSpec{
					DefaultWorkload: "vm-passthrough",
				},
				FabricManager: gpuv1.FabricManagerSpec{
					Mode: gpuv1.FabricModeFullPassthrough,
				},
				Driver: gpuv1.DriverSpec{
					Enabled: newBoolPtr(false),
				},
			},
			expectError: false,
		},
		{
			name: "valid configuration - container workload with any fabric manager mode",
			clusterPolicy: &gpuv1.ClusterPolicySpec{
				SandboxWorkloads: gpuv1.SandboxWorkloadsSpec{
					DefaultWorkload: "container",
				},
				FabricManager: gpuv1.FabricManagerSpec{
					Mode: gpuv1.FabricModeSharedNVSwitch,
				},
				Driver: gpuv1.DriverSpec{
					Enabled: newBoolPtr(false),
				},
			},
			expectError: false,
		},
		{
			name: "invalid configuration - vm-passthrough with shared-nvswitch but driver disabled",
			clusterPolicy: &gpuv1.ClusterPolicySpec{
				SandboxWorkloads: gpuv1.SandboxWorkloadsSpec{
					DefaultWorkload: "vm-passthrough",
				},
				FabricManager: gpuv1.FabricManagerSpec{
					Mode: gpuv1.FabricModeSharedNVSwitch,
				},
				Driver: gpuv1.DriverSpec{
					Enabled: newBoolPtr(false),
				},
			},
			expectError:  true,
			errorMessage: "driver must be enabled when using vm-passthrough with Fabric Manager Shared NVSwitch mode",
		},
		{
			name: "valid configuration - vm-passthrough with shared-nvswitch and driver not specified (defaults to enabled)",
			clusterPolicy: &gpuv1.ClusterPolicySpec{
				SandboxWorkloads: gpuv1.SandboxWorkloadsSpec{
					DefaultWorkload: "vm-passthrough",
				},
				FabricManager: gpuv1.FabricManagerSpec{
					Mode: gpuv1.FabricModeSharedNVSwitch,
				},
				Driver: gpuv1.DriverSpec{
					// Enabled not specified, defaults to true
				},
			},
			expectError: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.clusterPolicy.ValidateFabricManagerConfig()

			if tt.expectError {
				assert.Error(t, err)
				assert.Contains(t, err.Error(), tt.errorMessage)
			} else {
				assert.NoError(t, err)
			}
		})
	}
}
