/*
 * Copyright (c) 2024, NVIDIA CORPORATION.  All rights reserved.
 *
 * Licensed under the Apache License, Version 2.0 (the "License");
 * you may not use this file except in compliance with the License.
 * You may obtain a copy of the License at
 *
 *     http://www.apache.org/licenses/LICENSE-2.0
 *
 * Unless required by applicable law or agreed to in writing, software
 * distributed under the License is distributed on an "AS IS" BASIS,
 * WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
 * See the License for the specific language governing permissions and
 * limitations under the License.
 */

package controllers

import (
	"testing"

	"github.com/stretchr/testify/require"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// Daemonset is a DaemonSet wrapper used for testing
type Daemonset struct {
	*appsv1.DaemonSet
}

func NewDaemonset() Daemonset {
	ds := &appsv1.DaemonSet{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-ds",
			Namespace: "test-ns",
		},
		Spec: appsv1.DaemonSetSpec{
			Template: corev1.PodTemplateSpec{
				Spec: corev1.PodSpec{
					InitContainers: []corev1.Container{
						{Name: "foo", Image: "foo"},
					},
					Containers: []corev1.Container{
						{Name: "foo", Image: "foo"},
					},
				},
			},
		},
	}
	return Daemonset{ds}
}

func (d Daemonset) WithHostPathVolume(name string, path string) Daemonset {
	volume := corev1.Volume{
		Name: name,
		VolumeSource: corev1.VolumeSource{
			HostPath: &corev1.HostPathVolumeSource{
				Path: path,
			},
		},
	}
	d.Spec.Template.Spec.Volumes = append(d.Spec.Template.Spec.Volumes, volume)
	return d
}

func (d Daemonset) WithEnvVar(name string, value string) Daemonset {
	for index := range d.Spec.Template.Spec.InitContainers {
		ctr := &d.Spec.Template.Spec.InitContainers[index]
		ctr.Env = append(ctr.Env, corev1.EnvVar{Name: name, Value: value})
	}
	for index := range d.Spec.Template.Spec.Containers {
		ctr := &d.Spec.Template.Spec.Containers[index]
		ctr.Env = append(ctr.Env, corev1.EnvVar{Name: name, Value: value})
	}
	return d
}

func TestTransformForHostRoot(t *testing.T) {
	hostRootVolumeName := "host-root"
	hostDevCharVolumeName := "host-dev-char"
	testCases := []struct {
		description    string
		hostRoot       string
		input          Daemonset
		expectedOutput Daemonset
	}{
		{
			description:    "no host root or host-dev-char volume in daemonset",
			hostRoot:       "/custom-root",
			input:          NewDaemonset(),
			expectedOutput: NewDaemonset(),
		},
		{
			description: "empty host root is a no-op",
			hostRoot:    "",
			input: NewDaemonset().
				WithHostPathVolume(hostRootVolumeName, "/").
				WithHostPathVolume(hostDevCharVolumeName, "/"),
			expectedOutput: NewDaemonset().
				WithHostPathVolume(hostRootVolumeName, "/").
				WithHostPathVolume(hostDevCharVolumeName, "/"),
		},
		{
			description: "custom host root with host-root and host-dev-char volumes",
			hostRoot:    "/custom-root",
			input: NewDaemonset().
				WithHostPathVolume(hostRootVolumeName, "/").
				WithHostPathVolume(hostDevCharVolumeName, "/"),
			expectedOutput: NewDaemonset().
				WithHostPathVolume(hostRootVolumeName, "/custom-root").
				WithHostPathVolume(hostDevCharVolumeName, "/custom-root/dev/char").
				WithEnvVar(HostRootEnvName, "/custom-root"),
		},
		{
			description: "custom host root with host-root volume",
			hostRoot:    "/custom-root",
			input: NewDaemonset().
				WithHostPathVolume(hostRootVolumeName, "/"),
			expectedOutput: NewDaemonset().
				WithHostPathVolume(hostRootVolumeName, "/custom-root").
				WithEnvVar(HostRootEnvName, "/custom-root"),
		},
		{
			description: "custom host root with host-dev-char volume",
			hostRoot:    "/custom-root",
			input: NewDaemonset().
				WithHostPathVolume(hostDevCharVolumeName, "/"),
			expectedOutput: NewDaemonset().
				WithHostPathVolume(hostDevCharVolumeName, "/custom-root/dev/char"),
		},
	}

	for _, tc := range testCases {
		t.Run(tc.description, func(t *testing.T) {
			transformForHostRoot(tc.input.DaemonSet, tc.hostRoot)
			require.EqualValues(t, tc.expectedOutput, tc.input)
		})
	}
}
