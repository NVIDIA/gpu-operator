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

	"github.com/stretchr/testify/assert"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
)

func TestGpuPodSpecFilter(t *testing.T) {
	oneGPU := resource.MustParse("1")

	testCases := []struct {
		name     string
		pod      corev1.Pod
		expected bool
	}{
		{
			name: "Running pod with nvidia.com/gpu in Limits",
			pod: corev1.Pod{
				Status: corev1.PodStatus{Phase: corev1.PodRunning},
				Spec: corev1.PodSpec{
					Containers: []corev1.Container{
						{
							Resources: corev1.ResourceRequirements{
								Limits: corev1.ResourceList{
									"nvidia.com/gpu": oneGPU,
								},
							},
						},
					},
				},
			},
			expected: true,
		},
		{
			name: "Pending pod with nvidia.com/gpu in Requests",
			pod: corev1.Pod{
				Status: corev1.PodStatus{Phase: corev1.PodPending},
				Spec: corev1.PodSpec{
					Containers: []corev1.Container{
						{
							Resources: corev1.ResourceRequirements{
								Requests: corev1.ResourceList{
									"nvidia.com/gpu": oneGPU,
								},
							},
						},
					},
				},
			},
			expected: true,
		},
		{
			name: "Pod with nvidia.com/mig- prefix resource in Limits",
			pod: corev1.Pod{
				Status: corev1.PodStatus{Phase: corev1.PodRunning},
				Spec: corev1.PodSpec{
					Containers: []corev1.Container{
						{
							Resources: corev1.ResourceRequirements{
								Limits: corev1.ResourceList{
									"nvidia.com/mig-1g.5gb": oneGPU,
								},
							},
						},
					},
				},
			},
			expected: true,
		},
		{
			name: "Resource named exactly nvidia.com/gpu matches HasPrefix",
			pod: corev1.Pod{
				Status: corev1.PodStatus{Phase: corev1.PodRunning},
				Spec: corev1.PodSpec{
					Containers: []corev1.Container{
						{
							Resources: corev1.ResourceRequirements{
								Requests: corev1.ResourceList{
									"nvidia.com/gpu": oneGPU,
								},
							},
						},
					},
				},
			},
			expected: true,
		},
		{
			name: "Resource named nvidia.com/gpu-shared matches HasPrefix",
			pod: corev1.Pod{
				Status: corev1.PodStatus{Phase: corev1.PodPending},
				Spec: corev1.PodSpec{
					Containers: []corev1.Container{
						{
							Resources: corev1.ResourceRequirements{
								Limits: corev1.ResourceList{
									"nvidia.com/gpu-shared": oneGPU,
								},
							},
						},
					},
				},
			},
			expected: true,
		},
		{
			name: "Multi-container pod where only the second container has the gpu resource",
			pod: corev1.Pod{
				Status: corev1.PodStatus{Phase: corev1.PodRunning},
				Spec: corev1.PodSpec{
					Containers: []corev1.Container{
						{
							Resources: corev1.ResourceRequirements{
								Limits: corev1.ResourceList{
									corev1.ResourceCPU: oneGPU,
								},
							},
						},
						{
							Resources: corev1.ResourceRequirements{
								Limits: corev1.ResourceList{
									"nvidia.com/gpu": oneGPU,
								},
							},
						},
					},
				},
			},
			expected: true,
		},
		{
			name: "Running pod with no gpu resources",
			pod: corev1.Pod{
				Status: corev1.PodStatus{Phase: corev1.PodRunning},
				Spec: corev1.PodSpec{
					Containers: []corev1.Container{
						{
							Resources: corev1.ResourceRequirements{
								Limits: corev1.ResourceList{},
							},
						},
					},
				},
			},
			expected: false,
		},
		{
			name: "Pod with only unrelated cpu/memory resources",
			pod: corev1.Pod{
				Status: corev1.PodStatus{Phase: corev1.PodRunning},
				Spec: corev1.PodSpec{
					Containers: []corev1.Container{
						{
							Resources: corev1.ResourceRequirements{
								Limits: corev1.ResourceList{
									corev1.ResourceCPU:    oneGPU,
									corev1.ResourceMemory: oneGPU,
								},
								Requests: corev1.ResourceList{
									corev1.ResourceCPU: oneGPU,
								},
							},
						},
					},
				},
			},
			expected: false,
		},
		{
			name: "Succeeded pod with a gpu resource - phase gate takes precedence",
			pod: corev1.Pod{
				Status: corev1.PodStatus{Phase: corev1.PodSucceeded},
				Spec: corev1.PodSpec{
					Containers: []corev1.Container{
						{
							Resources: corev1.ResourceRequirements{
								Limits: corev1.ResourceList{
									"nvidia.com/gpu": oneGPU,
								},
							},
						},
					},
				},
			},
			expected: false,
		},
		{
			name: "Failed pod with a gpu resource - phase gate takes precedence",
			pod: corev1.Pod{
				Status: corev1.PodStatus{Phase: corev1.PodFailed},
				Spec: corev1.PodSpec{
					Containers: []corev1.Container{
						{
							Resources: corev1.ResourceRequirements{
								Requests: corev1.ResourceList{
									"nvidia.com/mig-1g.5gb": oneGPU,
								},
							},
						},
					},
				},
			},
			expected: false,
		},
		{
			name: "Unknown-phase pod with a gpu resource - phase gate takes precedence",
			pod: corev1.Pod{
				Status: corev1.PodStatus{Phase: corev1.PodUnknown},
				Spec: corev1.PodSpec{
					Containers: []corev1.Container{
						{
							Resources: corev1.ResourceRequirements{
								Limits: corev1.ResourceList{
									"nvidia.com/gpu": oneGPU,
								},
							},
						},
					},
				},
			},
			expected: false,
		},
		{
			name: "Running pod with no containers",
			pod: corev1.Pod{
				Status: corev1.PodStatus{Phase: corev1.PodRunning},
				Spec:   corev1.PodSpec{Containers: []corev1.Container{}},
			},
			expected: false,
		},
		{
			name: "Running pod with container but empty resource lists",
			pod: corev1.Pod{
				Status: corev1.PodStatus{Phase: corev1.PodRunning},
				Spec: corev1.PodSpec{
					Containers: []corev1.Container{
						{Resources: corev1.ResourceRequirements{}},
					},
				},
			},
			expected: false,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			assert.Equal(t, tc.expected, gpuPodSpecFilter(tc.pod))
		})
	}
}
