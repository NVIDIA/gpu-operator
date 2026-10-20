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

package baseline

import (
	"context"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/NVIDIA/gpu-operator/internal/certification"
	"github.com/NVIDIA/gpu-operator/internal/certification/k8s"
)

const gpuTestPodName = "gpu-certification-test"

type Workload struct{}

func (w *Workload) Name() string {
	return "gpu-workload"
}

func (w *Workload) Run(ctx context.Context, clients *k8s.Clients, cfg certification.Config) *certification.TestResult {
	return runPodTest(ctx, clients, cfg, podTestOpts{
		testName:       w.Name(),
		podName:        gpuTestPodName,
		imageDesc:      cfg.TestWorkloadImage,
		successMessage: "default GPU workload completed successfully",
		pod:            w.buildPod(cfg),
	})
}

func (w *Workload) buildPod(cfg certification.Config) *corev1.Pod {
	return &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      gpuTestPodName,
			Namespace: cfg.Namespace,
		},
		Spec: corev1.PodSpec{
			RestartPolicy: corev1.RestartPolicyNever,
			Containers: []corev1.Container{
				{
					Name:  "cuda-test",
					Image: cfg.TestWorkloadImage,
					Resources: corev1.ResourceRequirements{
						Limits: corev1.ResourceList{
							"nvidia.com/gpu": resource.MustParse("1"),
						},
					},
				},
			},
		},
	}
}
