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
	"errors"
	"testing"

	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/utils/ptr"

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

func TestValidateClusterPolicySpec(t *testing.T) {
	tests := []struct {
		description string
		spec        *gpuv1.ClusterPolicySpec
		err         error
	}{
		{
			description: "valid CDI object in spec",
			spec: &gpuv1.ClusterPolicySpec{
				CDI: gpuv1.CDIConfigSpec{
					Enabled:          ptr.To(true),
					NRIPluginEnabled: ptr.To(true),
				},
			},
		},
		{
			description: "invalid CDI object in spec",
			spec: &gpuv1.ClusterPolicySpec{
				CDI: gpuv1.CDIConfigSpec{
					Enabled:          ptr.To(false),
					NRIPluginEnabled: ptr.To(true),
				},
			},
			err: errors.New("the NRI Plugin cannot be enabled when CDI is disabled"),
		},
	}

	for _, tc := range tests {
		t.Run(tc.description, func(t *testing.T) {
			err := validateClusterPolicySpec(tc.spec)
			if tc.err == nil {
				require.NoError(t, err)
			} else {
				require.Error(t, err)
				require.Equal(t, tc.err.Error(), err.Error())
			}
		})
	}
}
