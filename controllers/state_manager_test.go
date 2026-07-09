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
	"context"
	"testing"

	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	gpuv1 "github.com/NVIDIA/gpu-operator/api/nvidia/v1"
)

func TestGetGPUNodeOSInfo(t *testing.T) {
	testCases := []struct {
		name              string
		osName            string
		osVersion         string
		expected          string
		expectError       bool
		errorContainsText string
	}{
		{
			name:      "talos version with v prefix",
			osName:    "talos",
			osVersion: "v1.12.6",
			expected:  "talosv1.12.6",
		},
		{
			name:      "rhel 10 omits minor version",
			osName:    "rhel",
			osVersion: "10.2",
			expected:  "rhel10",
		},
		{
			name:      "rocky omits minor version",
			osName:    "rocky",
			osVersion: "9.5",
			expected:  "rocky9",
		},
		{
			name:      "ubuntu preserves full version",
			osName:    "ubuntu",
			osVersion: "24.04",
			expected:  "ubuntu24.04",
		},
		{
			name:      "sles preserves dotted version",
			osName:    "sles",
			osVersion: "15.6",
			expected:  "sles15.6",
		},
		{
			name:      "sles preserves service-pack version",
			osName:    "sles",
			osVersion: "15-SP6",
			expected:  "sles15-SP6",
		},
		{
			name:      "sl-micro preserves dotted version",
			osName:    "sl-micro",
			osVersion: "6.0",
			expected:  "sl-micro6.0",
		},
		{
			name:      "archlinux preserves rolling version",
			osName:    "archlinux",
			osVersion: "rolling",
			expected:  "archlinuxrolling",
		},
		{
			name:              "rhel invalid major version errors",
			osName:            "rhel",
			osVersion:         "A.10",
			expectError:       true,
			errorContainsText: "error processing OS major version",
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			scheme := runtime.NewScheme()
			require.NoError(t, corev1.AddToScheme(scheme))

			node := &corev1.Node{
				ObjectMeta: metav1.ObjectMeta{
					Name: "gpu-node-1",
					Labels: map[string]string{
						commonGPULabelKey:      commonGPULabelValue,
						nfdOSReleaseIDLabelKey: tc.osName,
						nfdOSVersionIDLabelKey: tc.osVersion,
					},
				},
			}

			client := fake.NewClientBuilder().WithScheme(scheme).WithObjects(node).Build()
			controller := ClusterPolicyController{ctx: context.Background(), client: client}

			osName, osTag, err := controller.getGPUNodeOSInfo()
			if tc.expectError {
				require.Error(t, err)
				require.Contains(t, err.Error(), tc.errorContainsText)
				return
			}

			require.NoError(t, err)
			require.Equal(t, tc.osName, osName)
			require.Equal(t, tc.expected, osTag)
		})
	}
}

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

func TestGetEffectiveStateLabels(t *testing.T) {
	// getEffectiveStateLabels returns labels for workload config and sandbox mode.
	// For container and vm-vgpu, mode has no effect. For vm-passthrough, mode selects
	// sandbox-device-plugin (kubevirt) vs kata-device-plugin (kata).
	t.Run("container", func(t *testing.T) {
		got := getEffectiveStateLabels(gpuWorkloadConfigContainer, "kubevirt")
		require.NotNil(t, got)
		require.Contains(t, got, "nvidia.com/gpu.deploy.device-plugin")
		require.Equal(t, "true", got["nvidia.com/gpu.deploy.device-plugin"])
	})
	t.Run("vm-vgpu", func(t *testing.T) {
		got := getEffectiveStateLabels(gpuWorkloadConfigVMVgpu, "kata")
		require.NotNil(t, got)
		require.Contains(t, got, "nvidia.com/gpu.deploy.sandbox-device-plugin")
		require.Equal(t, "true", got["nvidia.com/gpu.deploy.sandbox-device-plugin"])
	})
	// vm-passthrough: test kubevirt first (map has sandbox-device-plugin), then kata.
	t.Run("vm-passthrough-kubevirt", func(t *testing.T) {
		got := getEffectiveStateLabels(gpuWorkloadConfigVMPassthrough, string(gpuv1.KubeVirt))
		require.NotNil(t, got)
		require.Contains(t, got, kubevirtDevicePluginDeployLabelKey)
		require.Equal(t, "true", got[kubevirtDevicePluginDeployLabelKey])
		require.NotContains(t, got, kataDevicePluginDeployLabelKey)
	})
	t.Run("vm-passthrough-kata", func(t *testing.T) {
		got := getEffectiveStateLabels(gpuWorkloadConfigVMPassthrough, string(gpuv1.Kata))
		require.NotNil(t, got)
		require.Contains(t, got, kataDevicePluginDeployLabelKey)
		require.Equal(t, "true", got[kataDevicePluginDeployLabelKey])
		require.NotContains(t, got, kubevirtDevicePluginDeployLabelKey)
	})
	t.Run("invalid config", func(t *testing.T) {
		got := getEffectiveStateLabels("invalid", "kubevirt")
		require.Nil(t, got)
	})
}

func TestRemoveAllGPUStateLabels(t *testing.T) {
	// removeAllGPUStateLabels removes all gpuStateLabels keys plus kata-device-plugin and mig-manager.
	t.Run("removes kata device plugin label", func(t *testing.T) {
		labels := map[string]string{
			kataDevicePluginDeployLabelKey: "true",
			"other":                        "keep",
		}
		modified := removeAllGPUStateLabels(labels)
		require.True(t, modified)
		require.NotContains(t, labels, kataDevicePluginDeployLabelKey)
		require.Equal(t, "keep", labels["other"])
	})
	t.Run("removes sandbox deploy label", func(t *testing.T) {
		labels := map[string]string{
			kubevirtDevicePluginDeployLabelKey: "true",
		}
		modified := removeAllGPUStateLabels(labels)
		require.True(t, modified)
		require.Empty(t, labels[kubevirtDevicePluginDeployLabelKey])
	})
}

func TestIsStateEnabled_SandboxAndKataDevicePlugin(t *testing.T) {
	boolTrue := ptr.To(true)
	boolFalse := ptr.To(false)
	tests := []struct {
		name           string
		sandboxEnabled bool
		spec           gpuv1.ClusterPolicySpec
		stateName      string
		wantEnabled    bool
	}{
		{
			name:           "state-sandbox-device-plugin enabled when sandbox+plugin+mode kubevirt",
			sandboxEnabled: true,
			spec: gpuv1.ClusterPolicySpec{
				SandboxWorkloads:    gpuv1.SandboxWorkloadsSpec{Enabled: boolTrue, Mode: "kubevirt"},
				SandboxDevicePlugin: gpuv1.SandboxDevicePluginSpec{Enabled: boolTrue},
			},
			stateName:   "state-sandbox-device-plugin",
			wantEnabled: true,
		},
		{
			name:           "state-sandbox-device-plugin disabled when mode kata",
			sandboxEnabled: true,
			spec: gpuv1.ClusterPolicySpec{
				SandboxWorkloads:    gpuv1.SandboxWorkloadsSpec{Enabled: boolTrue, Mode: "kata"},
				SandboxDevicePlugin: gpuv1.SandboxDevicePluginSpec{Enabled: boolTrue},
			},
			stateName:   "state-sandbox-device-plugin",
			wantEnabled: false,
		},
		{
			name:           "state-kata-device-plugin enabled when sandbox+kata plugin+mode kata",
			sandboxEnabled: true,
			spec: gpuv1.ClusterPolicySpec{
				SandboxWorkloads:        gpuv1.SandboxWorkloadsSpec{Enabled: boolTrue, Mode: "kata"},
				KataSandboxDevicePlugin: gpuv1.KataDevicePluginSpec{ComponentCommonSpec: gpuv1.ComponentCommonSpec{Enabled: boolTrue}},
			},
			stateName:   "state-kata-device-plugin",
			wantEnabled: true,
		},
		{
			name:           "state-kata-device-plugin disabled when mode kubevirt",
			sandboxEnabled: true,
			spec: gpuv1.ClusterPolicySpec{
				SandboxWorkloads:        gpuv1.SandboxWorkloadsSpec{Enabled: boolTrue, Mode: "kubevirt"},
				KataSandboxDevicePlugin: gpuv1.KataDevicePluginSpec{ComponentCommonSpec: gpuv1.ComponentCommonSpec{Enabled: boolTrue}},
			},
			stateName:   "state-kata-device-plugin",
			wantEnabled: false,
		},
		{
			name:           "state-kata-device-plugin disabled when KataSandboxDevicePlugin.Enabled false",
			sandboxEnabled: true,
			spec: gpuv1.ClusterPolicySpec{
				SandboxWorkloads:        gpuv1.SandboxWorkloadsSpec{Enabled: boolTrue, Mode: "kata"},
				KataSandboxDevicePlugin: gpuv1.KataDevicePluginSpec{ComponentCommonSpec: gpuv1.ComponentCommonSpec{Enabled: boolFalse}},
			},
			stateName:   "state-kata-device-plugin",
			wantEnabled: false,
		},
		{
			name:           "state-kata-device-plugin disabled when sandbox workloads disabled",
			sandboxEnabled: false,
			spec: gpuv1.ClusterPolicySpec{
				SandboxWorkloads:        gpuv1.SandboxWorkloadsSpec{Enabled: boolTrue, Mode: "kata"},
				KataSandboxDevicePlugin: gpuv1.KataDevicePluginSpec{ComponentCommonSpec: gpuv1.ComponentCommonSpec{Enabled: boolTrue}},
			},
			stateName:   "state-kata-device-plugin",
			wantEnabled: false,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			n := ClusterPolicyController{
				singleton:      &gpuv1.ClusterPolicy{Spec: tc.spec},
				sandboxEnabled: tc.sandboxEnabled,
			}
			got := n.isStateEnabled(tc.stateName)
			require.Equal(t, tc.wantEnabled, got, "isStateEnabled(%q)", tc.stateName)
		})
	}
}
