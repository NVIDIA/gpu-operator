package controllers

import (
	gpuv1 "github.com/NVIDIA/gpu-operator/api/v1"
	corev1 "k8s.io/api/core/v1"
	"testing"
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
