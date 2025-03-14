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

	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	gpuv1 "github.com/NVIDIA/gpu-operator/api/nvidia/v1"
)

func TestGetRuntimeVersionString(t *testing.T) {
	testCases := []struct {
		description     string
		runtimeVer      string
		expectedRuntime gpuv1.Runtime
		expectedVersion string
		errorExpected   bool
	}{
		{
			"containerd",
			"containerd://1.0.0",
			gpuv1.Containerd,
			"v1.0.0",
			false,
		},
		{
			"docker",
			"docker://1.0.0",
			gpuv1.Docker,
			"v1.0.0",
			false,
		},
		{
			"crio",
			"cri-o://1.0.0",
			gpuv1.CRIO,
			"v1.0.0",
			false,
		},
		{
			"unknown",
			"unknown://1.0.0",
			"",
			"v1.0.0",
			true,
		},
		{
			"containerd with v prefix",
			"containerd://v1.0.0",
			gpuv1.Containerd,
			"v1.0.0",
			false,
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
			runtime, version, err := getRuntimeVersionString(node)
			if tc.errorExpected {
				require.Error(t, err)
				return
			}
			require.NoError(t, err)
			require.EqualValues(t, tc.expectedRuntime, runtime)
			require.EqualValues(t, tc.expectedVersion, version)
		})
	}
}

func TestGetContainerRuntimeInfo(t *testing.T) {
	testCases := []struct {
		description        string
		ctrl               *ClusterPolicyController
		expectedRuntime    gpuv1.Runtime
		runtimeSupportsCDI bool
		errorExpected      bool
	}{
		{
			description: "containerd",
			ctrl: &ClusterPolicyController{
				client: fake.NewFakeClient(
					&corev1.Node{
						ObjectMeta: metav1.ObjectMeta{
							Name:   "node-1",
							Labels: map[string]string{commonGPULabelKey: "true"},
						},
						Status: corev1.NodeStatus{
							NodeInfo: corev1.NodeSystemInfo{ContainerRuntimeVersion: "containerd://1.7.0"},
						},
					}),
			},
			expectedRuntime:    gpuv1.Containerd,
			runtimeSupportsCDI: true,
			errorExpected:      false,
		},
		{
			description: "cri-o",
			ctrl: &ClusterPolicyController{
				client: fake.NewFakeClient(
					&corev1.Node{
						ObjectMeta: metav1.ObjectMeta{
							Name:   "node-1",
							Labels: map[string]string{commonGPULabelKey: "true"},
						},
						Status: corev1.NodeStatus{
							NodeInfo: corev1.NodeSystemInfo{ContainerRuntimeVersion: "cri-o://1.30.0"},
						},
					}),
			},
			expectedRuntime:    gpuv1.CRIO,
			runtimeSupportsCDI: true,
			errorExpected:      false,
		},
		{
			description: "openshift",
			ctrl: &ClusterPolicyController{
				openshift: "1.0.0",
			},
			expectedRuntime:    gpuv1.CRIO,
			runtimeSupportsCDI: true,
			errorExpected:      false,
		},
		{
			description: "containerd, multiple nodes, cdi not supported",
			ctrl: &ClusterPolicyController{
				client: fake.NewFakeClient(
					&corev1.Node{
						ObjectMeta: metav1.ObjectMeta{
							Name:   "node-1",
							Labels: map[string]string{commonGPULabelKey: "true"},
						},
						Status: corev1.NodeStatus{
							NodeInfo: corev1.NodeSystemInfo{
								ContainerRuntimeVersion: "containerd://1.7.0",
							},
						},
					},
					&corev1.Node{
						ObjectMeta: metav1.ObjectMeta{
							Name:   "node-2",
							Labels: map[string]string{commonGPULabelKey: "true"},
						},
						Status: corev1.NodeStatus{
							NodeInfo: corev1.NodeSystemInfo{
								ContainerRuntimeVersion: "containerd://1.6.30",
							},
						},
					}),
			},
			expectedRuntime:    gpuv1.Containerd,
			runtimeSupportsCDI: false,
			errorExpected:      false,
		},
		{
			description: "multiple nodes, different runtimes",
			ctrl: &ClusterPolicyController{
				client: fake.NewFakeClient(
					&corev1.Node{
						ObjectMeta: metav1.ObjectMeta{
							Name:   "node-1",
							Labels: map[string]string{commonGPULabelKey: "true"},
						},
						Status: corev1.NodeStatus{
							NodeInfo: corev1.NodeSystemInfo{
								ContainerRuntimeVersion: "containerd://1.7.0",
							},
						},
					},
					&corev1.Node{
						ObjectMeta: metav1.ObjectMeta{
							Name:   "node-2",
							Labels: map[string]string{commonGPULabelKey: "true"},
						},
						Status: corev1.NodeStatus{
							NodeInfo: corev1.NodeSystemInfo{
								ContainerRuntimeVersion: "cri-o://1.30.0",
							},
						},
					}),
			},
			errorExpected: true,
		},
	}
	for _, tc := range testCases {
		t.Run(tc.description, func(t *testing.T) {
			err := tc.ctrl.getContainerRuntimeInfo()
			if tc.errorExpected {
				require.Error(t, err)
				return
			}
			require.NoError(t, err)
			require.EqualValues(t, tc.expectedRuntime, tc.ctrl.runtime)
			require.EqualValues(t, tc.runtimeSupportsCDI, tc.ctrl.runtimeSupportsCDI)
		})
	}
}
