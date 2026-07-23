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

package main

import (
	"testing"

	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	resourcev1 "k8s.io/api/resource/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
)

func allocatedClaim(name, driver string) *resourcev1.ResourceClaim {
	claim := &resourcev1.ResourceClaim{
		ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: "default"},
	}
	if driver != "" {
		claim.Status.Allocation = &resourcev1.AllocationResult{
			Devices: resourcev1.DeviceAllocationResult{
				Results: []resourcev1.DeviceRequestAllocationResult{
					{Request: "gpu", Driver: driver, Pool: "pool", Device: "gpu-0"},
				},
			},
		}
	}
	return claim
}

func claimPod(phase corev1.PodPhase, claims ...corev1.PodResourceClaim) corev1.Pod {
	return corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{Name: "pod", Namespace: "default"},
		Spec:       corev1.PodSpec{ResourceClaims: claims},
		Status:     corev1.PodStatus{Phase: phase},
	}
}

func TestGPUPodSpecFilterResourceClaims(t *testing.T) {
	scheme := runtime.NewScheme()
	require.NoError(t, clientgoscheme.AddToScheme(scheme))

	testCases := []struct {
		name     string
		objs     []runtime.Object
		pod      corev1.Pod
		expected bool
	}{
		{
			name:     "pod without GPU resources or claims",
			pod:      claimPod(corev1.PodRunning),
			expected: false,
		},
		{
			name: "pod with a claim allocated by the NVIDIA GPU DRA driver",
			objs: []runtime.Object{allocatedClaim("gpu-claim", "gpu.nvidia.com")},
			pod: claimPod(corev1.PodRunning,
				corev1.PodResourceClaim{Name: "gpu", ResourceClaimName: ptr.To("gpu-claim")}),
			expected: true,
		},
		{
			name: "pod with an admin-access claim is not a GPU pod",
			objs: []runtime.Object{func() *resourcev1.ResourceClaim {
				claim := allocatedClaim("admin-claim", "gpu.nvidia.com")
				claim.Status.Allocation.Devices.Results[0].AdminAccess = ptr.To(true)
				return claim
			}()},
			pod: claimPod(corev1.PodRunning,
				corev1.PodResourceClaim{Name: "gpu", ResourceClaimName: ptr.To("admin-claim")}),
			expected: false,
		},
		{
			name: "pod with a claim allocated by another DRA driver",
			objs: []runtime.Object{allocatedClaim("nic-claim", "net.example.com")},
			pod: claimPod(corev1.PodRunning,
				corev1.PodResourceClaim{Name: "nic", ResourceClaimName: ptr.To("nic-claim")}),
			expected: false,
		},
		{
			name: "pod with an unallocated claim",
			objs: []runtime.Object{allocatedClaim("pending-claim", "")},
			pod: claimPod(corev1.PodRunning,
				corev1.PodResourceClaim{Name: "gpu", ResourceClaimName: ptr.To("pending-claim")}),
			expected: false,
		},
		{
			name: "pod with a template-generated claim resolved via status",
			objs: []runtime.Object{allocatedClaim("pod-gpu-claim", "gpu.nvidia.com")},
			pod: func() corev1.Pod {
				p := claimPod(corev1.PodRunning,
					corev1.PodResourceClaim{Name: "gpu", ResourceClaimTemplateName: ptr.To("single-gpu")})
				p.Status.ResourceClaimStatuses = []corev1.PodResourceClaimStatus{
					{Name: "gpu", ResourceClaimName: ptr.To("pod-gpu-claim")},
				}
				return p
			}(),
			expected: true,
		},
		{
			name: "unresolvable claim counts as a GPU pod",
			pod: claimPod(corev1.PodRunning,
				corev1.PodResourceClaim{Name: "gpu", ResourceClaimName: ptr.To("missing-claim")}),
			expected: true,
		},
		{
			name: "completed pod with a GPU claim is ignored",
			objs: []runtime.Object{allocatedClaim("gpu-claim", "gpu.nvidia.com")},
			pod: claimPod(corev1.PodSucceeded,
				corev1.PodResourceClaim{Name: "gpu", ResourceClaimName: ptr.To("gpu-claim")}),
			expected: false,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			c := fake.NewClientBuilder().WithScheme(scheme).WithRuntimeObjects(tc.objs...).Build()
			require.Equal(t, tc.expected, gpuPodSpecFilter(t.Context(), c)(tc.pod))
		})
	}
}
