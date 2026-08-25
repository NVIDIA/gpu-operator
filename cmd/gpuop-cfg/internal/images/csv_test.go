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

	"github.com/operator-framework/api/pkg/operators/v1alpha1"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
)

func newCSV(relatedImages []v1alpha1.RelatedImage, containerImage string, envVars []corev1.EnvVar) *v1alpha1.ClusterServiceVersion {
	return &v1alpha1.ClusterServiceVersion{
		Spec: v1alpha1.ClusterServiceVersionSpec{
			RelatedImages: relatedImages,
			InstallStrategy: v1alpha1.NamedInstallStrategy{
				StrategySpec: v1alpha1.StrategyDetailsDeployment{
					DeploymentSpecs: []v1alpha1.StrategyDeploymentSpec{
						{
							Spec: appsv1.DeploymentSpec{
								Template: corev1.PodTemplateSpec{
									Spec: corev1.PodSpec{
										Containers: []corev1.Container{
											{
												Image: containerImage,
												Env:   envVars,
											},
										},
									},
								},
							},
						},
					},
				},
			},
		},
	}
}

func Test_FromCSV(t *testing.T) {
	tests := []struct {
		name string
		csv  *v1alpha1.ClusterServiceVersion
		want []string
	}{
		{
			name: "collects related images, container image, and IMAGE env vars",
			csv: newCSV(
				[]v1alpha1.RelatedImage{
					{Image: "nvcr.io/nvidia/gpu-operator:v24.9.0"},
					{Image: "nvcr.io/nvidia/driver:550.127.05"},
				},
				"nvcr.io/nvidia/gpu-operator:v24.9.0",
				[]corev1.EnvVar{
					{Name: "DRIVER_IMAGE", Value: "nvcr.io/nvidia/driver:550"},
					{Name: "TOOLKIT_IMAGE", Value: "nvcr.io/nvidia/toolkit:1.16"},
					{Name: "LOG_LEVEL", Value: "debug"},
				},
			),
			want: []string{
				"nvcr.io/nvidia/gpu-operator:v24.9.0",
				"nvcr.io/nvidia/driver:550.127.05",
				"nvcr.io/nvidia/gpu-operator:v24.9.0",
				"nvcr.io/nvidia/driver:550",
				"nvcr.io/nvidia/toolkit:1.16",
			},
		},
		{
			name: "no related images and no env vars",
			csv:  newCSV(nil, "nvcr.io/nvidia/gpu-operator:v24.9.0", nil),
			want: []string{
				"nvcr.io/nvidia/gpu-operator:v24.9.0",
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := FromCSV(tt.csv)
			if len(got) != len(tt.want) {
				t.Fatalf("FromCSV() returned %d images, want %d", len(got), len(tt.want))
			}
			for i, image := range got {
				if image != tt.want[i] {
					t.Errorf("FromCSV()[%d] = %q, want %q", i, image, tt.want[i])
				}
			}
		})
	}
}
