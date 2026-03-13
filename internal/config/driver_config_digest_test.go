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

package config

import (
	"testing"

	"github.com/stretchr/testify/assert"
	corev1 "k8s.io/api/core/v1"
)

func TestExtractEnvVars(t *testing.T) {
	tests := []struct {
		name     string
		input    []corev1.EnvVar
		expected []EnvVar
	}{
		{
			name:     "nil input",
			input:    nil,
			expected: nil,
		},
		{
			name:     "empty input",
			input:    []corev1.EnvVar{},
			expected: nil,
		},
		{
			name: "direct values only",
			input: []corev1.EnvVar{
				{Name: "B_VAR", Value: "b-val"},
				{Name: "A_VAR", Value: "a-val"},
			},
			expected: []EnvVar{
				{Name: "A_VAR", Value: "a-val"},
				{Name: "B_VAR", Value: "b-val"},
			},
		},
		{
			name: "ValueFrom entries are skipped",
			input: []corev1.EnvVar{
				{Name: "DIRECT", Value: "yes"},
				{Name: "NODE_NAME", ValueFrom: &corev1.EnvVarSource{
					FieldRef: &corev1.ObjectFieldSelector{FieldPath: "spec.nodeName"},
				}},
				{Name: "SECRET_VAL", ValueFrom: &corev1.EnvVarSource{
					SecretKeyRef: &corev1.SecretKeySelector{
						LocalObjectReference: corev1.LocalObjectReference{Name: "my-secret"},
						Key:                  "key",
					},
				}},
			},
			expected: []EnvVar{
				{Name: "DIRECT", Value: "yes"},
			},
		},
		{
			name: "all ValueFrom entries",
			input: []corev1.EnvVar{
				{Name: "NODE_NAME", ValueFrom: &corev1.EnvVarSource{
					FieldRef: &corev1.ObjectFieldSelector{FieldPath: "spec.nodeName"},
				}},
			},
			expected: nil,
		},
		{
			name: "empty string Value is kept",
			input: []corev1.EnvVar{
				{Name: "EMPTY_VAL", Value: ""},
			},
			expected: []EnvVar{
				{Name: "EMPTY_VAL", Value: ""},
			},
		},
		{
			name: "mixed entries sorted by name",
			input: []corev1.EnvVar{
				{Name: "ZEBRA", Value: "z"},
				{Name: "NODE_IP", ValueFrom: &corev1.EnvVarSource{
					FieldRef: &corev1.ObjectFieldSelector{FieldPath: "status.hostIP"},
				}},
				{Name: "ALPHA", Value: "a"},
				{Name: "MIDDLE", Value: "m"},
			},
			expected: []EnvVar{
				{Name: "ALPHA", Value: "a"},
				{Name: "MIDDLE", Value: "m"},
				{Name: "ZEBRA", Value: "z"},
			},
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			result := ExtractEnvVars(tc.input)
			assert.Equal(t, tc.expected, result)
		})
	}
}

func TestSortEnvVars(t *testing.T) {
	tests := []struct {
		name     string
		input    []EnvVar
		expected []EnvVar
	}{
		{
			name:     "nil input",
			input:    nil,
			expected: nil,
		},
		{
			name:     "empty input",
			input:    []EnvVar{},
			expected: []EnvVar{},
		},
		{
			name:     "single element",
			input:    []EnvVar{{Name: "A", Value: "1"}},
			expected: []EnvVar{{Name: "A", Value: "1"}},
		},
		{
			name: "already sorted",
			input: []EnvVar{
				{Name: "A", Value: "1"},
				{Name: "B", Value: "2"},
			},
			expected: []EnvVar{
				{Name: "A", Value: "1"},
				{Name: "B", Value: "2"},
			},
		},
		{
			name: "reverse order",
			input: []EnvVar{
				{Name: "C", Value: "3"},
				{Name: "B", Value: "2"},
				{Name: "A", Value: "1"},
			},
			expected: []EnvVar{
				{Name: "A", Value: "1"},
				{Name: "B", Value: "2"},
				{Name: "C", Value: "3"},
			},
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			result := SortEnvVars(tc.input)
			assert.Equal(t, tc.expected, result)
		})
	}

	t.Run("does not mutate input", func(t *testing.T) {
		input := []EnvVar{
			{Name: "B", Value: "2"},
			{Name: "A", Value: "1"},
		}
		original := make([]EnvVar, len(input))
		copy(original, input)

		_ = SortEnvVars(input)
		assert.Equal(t, original, input, "input slice must not be mutated")
	})
}

func TestExtractVolumeMounts(t *testing.T) {
	tests := []struct {
		name     string
		input    []corev1.VolumeMount
		expected []VolumeMountConfig
	}{
		{
			name:     "nil input",
			input:    nil,
			expected: nil,
		},
		{
			name:     "empty input",
			input:    []corev1.VolumeMount{},
			expected: nil,
		},
		{
			name: "single mount",
			input: []corev1.VolumeMount{
				{Name: "vol", MountPath: "/mnt", SubPath: "sub", ReadOnly: true},
			},
			expected: []VolumeMountConfig{
				{Name: "vol", MountPath: "/mnt", SubPath: "sub", ReadOnly: true},
			},
		},
		{
			name: "sorted by name then mount path",
			input: []corev1.VolumeMount{
				{Name: "b-vol", MountPath: "/z"},
				{Name: "a-vol", MountPath: "/y"},
				{Name: "a-vol", MountPath: "/x"},
			},
			expected: []VolumeMountConfig{
				{Name: "a-vol", MountPath: "/x"},
				{Name: "a-vol", MountPath: "/y"},
				{Name: "b-vol", MountPath: "/z"},
			},
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			result := ExtractVolumeMounts(tc.input)
			assert.Equal(t, tc.expected, result)
		})
	}
}

func TestExtractVolumes(t *testing.T) {
	tests := []struct {
		name     string
		input    []corev1.Volume
		expected []VolumeConfig
	}{
		{
			name:     "nil input",
			input:    nil,
			expected: nil,
		},
		{
			name:     "empty input",
			input:    []corev1.Volume{},
			expected: nil,
		},
		{
			name: "hostpath volume",
			input: []corev1.Volume{
				{Name: "host-root", VolumeSource: corev1.VolumeSource{
					HostPath: &corev1.HostPathVolumeSource{Path: "/"},
				}},
			},
			expected: []VolumeConfig{
				{Name: "host-root", HostPath: "/"},
			},
		},
		{
			name: "configmap volume",
			input: []corev1.Volume{
				{Name: "cfg", VolumeSource: corev1.VolumeSource{
					ConfigMap: &corev1.ConfigMapVolumeSource{
						LocalObjectReference: corev1.LocalObjectReference{Name: "my-cm"},
					},
				}},
			},
			expected: []VolumeConfig{
				{Name: "cfg", ConfigMapName: "my-cm"},
			},
		},
		{
			name: "secret volume",
			input: []corev1.Volume{
				{Name: "sec", VolumeSource: corev1.VolumeSource{
					Secret: &corev1.SecretVolumeSource{SecretName: "my-secret"},
				}},
			},
			expected: []VolumeConfig{
				{Name: "sec", SecretName: "my-secret"},
			},
		},
		{
			name: "emptyDir contributes name only",
			input: []corev1.Volume{
				{Name: "scratch", VolumeSource: corev1.VolumeSource{
					EmptyDir: &corev1.EmptyDirVolumeSource{},
				}},
			},
			expected: []VolumeConfig{
				{Name: "scratch"},
			},
		},
		{
			name: "mixed volumes sorted by name",
			input: []corev1.Volume{
				{Name: "z-vol", VolumeSource: corev1.VolumeSource{
					HostPath: &corev1.HostPathVolumeSource{Path: "/z"},
				}},
				{Name: "a-vol", VolumeSource: corev1.VolumeSource{
					Secret: &corev1.SecretVolumeSource{SecretName: "s"},
				}},
			},
			expected: []VolumeConfig{
				{Name: "a-vol", SecretName: "s"},
				{Name: "z-vol", HostPath: "/z"},
			},
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			result := ExtractVolumes(tc.input)
			assert.Equal(t, tc.expected, result)
		})
	}
}
