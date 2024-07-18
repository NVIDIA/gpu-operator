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
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/intstr"

	gpuv1 "github.com/NVIDIA/gpu-operator/api/nvidia/v1"
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
						{Name: "initCtr", Image: "initCtrImage"},
					},
					Containers: []corev1.Container{
						{Name: "mainCtr", Image: "mainCtrImage"},
					},
				},
			},
		},
	}
	return Daemonset{ds}
}

func (d Daemonset) WithHostPathVolume(name string, path string, hostPathType *corev1.HostPathType) Daemonset {
	volume := corev1.Volume{
		Name: name,
		VolumeSource: corev1.VolumeSource{
			HostPath: &corev1.HostPathVolumeSource{
				Path: path,
				Type: hostPathType,
			},
		},
	}
	d.Spec.Template.Spec.Volumes = append(d.Spec.Template.Spec.Volumes, volume)
	return d
}

func (d Daemonset) WithVolumeMount(name string, path string, containerName string) Daemonset {
	var ctr *corev1.Container
	for i, c := range d.Spec.Template.Spec.InitContainers {
		if c.Name == containerName {
			ctr = &d.Spec.Template.Spec.InitContainers[i]
			break
		}
	}
	for i, c := range d.Spec.Template.Spec.Containers {
		if c.Name == containerName {
			ctr = &d.Spec.Template.Spec.Containers[i]
			break
		}
	}

	if ctr == nil {
		return d
	}

	volumeMount := corev1.VolumeMount{
		Name:      name,
		MountPath: path,
	}
	ctr.VolumeMounts = append(ctr.VolumeMounts, volumeMount)
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

func (d Daemonset) WithEnvVarForCtr(name string, value string, containerName string) Daemonset {
	for index, c := range d.Spec.Template.Spec.Containers {
		if c.Name == containerName {
			ctr := &d.Spec.Template.Spec.Containers[index]
			ctr.Env = append(ctr.Env, corev1.EnvVar{Name: name, Value: value})
		}
	}
	return d
}

func (d Daemonset) WithInitContainer(container corev1.Container) Daemonset {
	d.Spec.Template.Spec.InitContainers = append(d.Spec.Template.Spec.InitContainers, container)
	return d
}

func (d Daemonset) WithName(name string) Daemonset {
	d.Name = name
	return d
}

func (d Daemonset) WithUpdateStrategy(strategy appsv1.DaemonSetUpdateStrategy) Daemonset {
	d.Spec.UpdateStrategy = strategy
	return d
}

func (d Daemonset) WithPriorityClass(name string) Daemonset {
	d.Spec.Template.Spec.PriorityClassName = name
	return d
}

func (d Daemonset) WithTolerations(tolerations []corev1.Toleration) Daemonset {
	d.Spec.Template.Spec.Tolerations = tolerations
	return d
}

func (d Daemonset) WithPodLabels(labels map[string]string) Daemonset {
	d.Spec.Template.Labels = labels
	return d
}

func (d Daemonset) WithPodAnnotations(annotations map[string]string) Daemonset {
	d.Spec.Template.Annotations = annotations
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
				WithHostPathVolume(hostRootVolumeName, "/", nil).
				WithHostPathVolume(hostDevCharVolumeName, "/", nil),
			expectedOutput: NewDaemonset().
				WithHostPathVolume(hostRootVolumeName, "/", nil).
				WithHostPathVolume(hostDevCharVolumeName, "/", nil),
		},
		{
			description: "custom host root with host-root and host-dev-char volumes",
			hostRoot:    "/custom-root",
			input: NewDaemonset().
				WithHostPathVolume(hostRootVolumeName, "/", nil).
				WithHostPathVolume(hostDevCharVolumeName, "/", nil),
			expectedOutput: NewDaemonset().
				WithHostPathVolume(hostRootVolumeName, "/custom-root", nil).
				WithHostPathVolume(hostDevCharVolumeName, "/custom-root/dev/char", nil).
				WithEnvVar(HostRootEnvName, "/custom-root"),
		},
		{
			description: "custom host root with host-root volume",
			hostRoot:    "/custom-root",
			input: NewDaemonset().
				WithHostPathVolume(hostRootVolumeName, "/", nil),
			expectedOutput: NewDaemonset().
				WithHostPathVolume(hostRootVolumeName, "/custom-root", nil).
				WithEnvVar(HostRootEnvName, "/custom-root"),
		},
		{
			description: "custom host root with host-dev-char volume",
			hostRoot:    "/custom-root",
			input: NewDaemonset().
				WithHostPathVolume(hostDevCharVolumeName, "/", nil),
			expectedOutput: NewDaemonset().
				WithHostPathVolume(hostDevCharVolumeName, "/custom-root/dev/char", nil),
		},
	}

	for _, tc := range testCases {
		t.Run(tc.description, func(t *testing.T) {
			transformForHostRoot(tc.input.DaemonSet, tc.hostRoot)
			require.EqualValues(t, tc.expectedOutput, tc.input)
		})
	}
}

func TestTransformForDriverInstallDir(t *testing.T) {
	driverInstallDirVolumeName := "driver-install-dir"
	testCases := []struct {
		description      string
		driverInstallDir string
		input            Daemonset
		expectedOutput   Daemonset
	}{
		{
			description:      "no driver-install-dir volume in daemonset",
			driverInstallDir: "/custom-root",
			input:            NewDaemonset(),
			expectedOutput:   NewDaemonset(),
		},
		{
			description:      "empty driverInstallDir is a no-op",
			driverInstallDir: "",
			input: NewDaemonset().
				WithHostPathVolume(driverInstallDirVolumeName, "/run/nvidia/driver", nil).
				WithInitContainer(
					corev1.Container{
						Name: "driver-validation",
						VolumeMounts: []corev1.VolumeMount{
							{Name: driverInstallDirVolumeName, MountPath: "/run/nvidia/driver"},
						},
					}),
			expectedOutput: NewDaemonset().
				WithHostPathVolume(driverInstallDirVolumeName, "/run/nvidia/driver", nil).
				WithInitContainer(
					corev1.Container{
						Name: "driver-validation",
						VolumeMounts: []corev1.VolumeMount{
							{Name: driverInstallDirVolumeName, MountPath: "/run/nvidia/driver"},
						},
					}),
		},
		{
			description:      "custom driverInstallDir with driver-install-dir volume",
			driverInstallDir: "/custom-root",
			input: NewDaemonset().
				WithHostPathVolume(driverInstallDirVolumeName, "/run/nvidia/driver", nil),
			expectedOutput: NewDaemonset().
				WithHostPathVolume(driverInstallDirVolumeName, "/custom-root", nil),
		},
		{
			description:      "custom driverInstallDir with driver-install-dir volume and driver-validation initContainer",
			driverInstallDir: "/custom-root",
			input: NewDaemonset().
				WithHostPathVolume(driverInstallDirVolumeName, "/run/nvidia/driver", nil).
				WithInitContainer(
					corev1.Container{
						Name: "driver-validation",
						VolumeMounts: []corev1.VolumeMount{
							{Name: driverInstallDirVolumeName, MountPath: "/run/nvidia/driver"},
						},
					}),
			expectedOutput: NewDaemonset().
				WithHostPathVolume(driverInstallDirVolumeName, "/custom-root", nil).
				WithInitContainer(
					corev1.Container{
						Name: "driver-validation",
						VolumeMounts: []corev1.VolumeMount{
							{Name: driverInstallDirVolumeName, MountPath: "/custom-root"},
						},
						Env: []corev1.EnvVar{
							{Name: DriverInstallDirEnvName, Value: "/custom-root"},
							{Name: DriverInstallDirCtrPathEnvName, Value: "/custom-root"},
						},
					}),
		},
	}

	for _, tc := range testCases {
		t.Run(tc.description, func(t *testing.T) {
			transformForDriverInstallDir(tc.input.DaemonSet, tc.driverInstallDir)
			require.EqualValues(t, tc.expectedOutput, tc.input)
		})
	}
}

func TestTransformForRuntime(t *testing.T) {
	testCases := []struct {
		description    string
		runtime        gpuv1.Runtime
		input          Daemonset
		expectedOutput Daemonset
	}{
		{
			description: "containerd",
			runtime:     gpuv1.Containerd,
			input:       NewDaemonset(),
			expectedOutput: NewDaemonset().
				WithHostPathVolume("containerd-config", filepath.Dir(DefaultContainerdConfigFile), newHostPathType(corev1.HostPathDirectoryOrCreate)).
				WithHostPathVolume("containerd-socket", filepath.Dir(DefaultContainerdSocketFile), nil).
				WithVolumeMount("containerd-config", DefaultRuntimeConfigTargetDir, "mainCtr").
				WithVolumeMount("containerd-socket", DefaultRuntimeSocketTargetDir, "mainCtr").
				WithEnvVarForCtr("RUNTIME", gpuv1.Containerd.String(), "mainCtr").
				WithEnvVarForCtr("CONTAINERD_RUNTIME_CLASS", DefaultRuntimeClass, "mainCtr").
				WithEnvVarForCtr("CONTAINERD_CONFIG", filepath.Join(DefaultRuntimeConfigTargetDir, filepath.Base(DefaultContainerdConfigFile)), "mainCtr").
				WithEnvVarForCtr("CONTAINERD_SOCKET", filepath.Join(DefaultRuntimeSocketTargetDir, filepath.Base(DefaultContainerdSocketFile)), "mainCtr"),
		},
		{
			description: "crio",
			runtime:     gpuv1.CRIO,
			input:       NewDaemonset(),
			expectedOutput: NewDaemonset().
				WithHostPathVolume("crio-config", filepath.Dir(DefaultCRIOConfigFile), newHostPathType(corev1.HostPathDirectoryOrCreate)).
				WithVolumeMount("crio-config", DefaultRuntimeConfigTargetDir, "mainCtr").
				WithEnvVarForCtr("RUNTIME", gpuv1.CRIO.String(), "mainCtr").
				WithEnvVarForCtr("CRIO_CONFIG", filepath.Join(DefaultRuntimeConfigTargetDir, filepath.Base(DefaultCRIOConfigFile)), "mainCtr"),
		},
	}

	cp := &gpuv1.ClusterPolicySpec{Operator: gpuv1.OperatorSpec{RuntimeClass: DefaultRuntimeClass}}
	for _, tc := range testCases {
		t.Run(tc.description, func(t *testing.T) {
			err := transformForRuntime(tc.input.DaemonSet, cp, tc.runtime.String(), "mainCtr")
			require.NoError(t, err)
			require.EqualValues(t, tc.expectedOutput, tc.input)
		})
	}
}

func TestApplyUpdateStrategyConfig(t *testing.T) {
	testCases := []struct {
		description   string
		ds            Daemonset
		dsSpec        gpuv1.DaemonsetsSpec
		errorExpected bool
		expectedDs    Daemonset
	}{
		{
			description:   "empty daemonset spec configuration",
			ds:            NewDaemonset(),
			dsSpec:        gpuv1.DaemonsetsSpec{},
			errorExpected: false,
			expectedDs:    NewDaemonset(),
		},
		{
			description:   "invalid update strategy string, no rolling update fields configured",
			ds:            NewDaemonset(),
			dsSpec:        gpuv1.DaemonsetsSpec{UpdateStrategy: "invalid"},
			errorExpected: false,
			expectedDs:    NewDaemonset(),
		},
		{
			description:   "RollingUpdate update strategy string, no rolling update fields configured",
			ds:            NewDaemonset(),
			dsSpec:        gpuv1.DaemonsetsSpec{UpdateStrategy: "RollingUpdate"},
			errorExpected: false,
			expectedDs:    NewDaemonset(),
		},
		{
			description: "RollingUpdate update strategy string, daemonset is driver pod",
			ds:          NewDaemonset().WithName(commonDriverDaemonsetName),
			dsSpec: gpuv1.DaemonsetsSpec{
				UpdateStrategy: "RollingUpdate",
				RollingUpdate: &gpuv1.RollingUpdateSpec{
					MaxUnavailable: "1",
				}},
			errorExpected: false,
			expectedDs:    NewDaemonset().WithName(commonDriverDaemonsetName),
		},
		{
			description: "RollingUpdate update strategy string, integer maxUnavailable",
			ds:          NewDaemonset(),
			dsSpec: gpuv1.DaemonsetsSpec{
				UpdateStrategy: "RollingUpdate",
				RollingUpdate: &gpuv1.RollingUpdateSpec{
					MaxUnavailable: "1",
				}},
			errorExpected: false,
			expectedDs: NewDaemonset().WithUpdateStrategy(appsv1.DaemonSetUpdateStrategy{
				Type:          appsv1.RollingUpdateDaemonSetStrategyType,
				RollingUpdate: &appsv1.RollingUpdateDaemonSet{MaxUnavailable: &intstr.IntOrString{Type: intstr.Int, IntVal: 1}},
			}),
		},
		{
			description: "RollingUpdate update strategy string, percentage maxUnavailable",
			ds:          NewDaemonset(),
			dsSpec: gpuv1.DaemonsetsSpec{
				UpdateStrategy: "RollingUpdate",
				RollingUpdate: &gpuv1.RollingUpdateSpec{
					MaxUnavailable: "10%",
				}},
			errorExpected: false,
			expectedDs: NewDaemonset().WithUpdateStrategy(appsv1.DaemonSetUpdateStrategy{
				Type:          appsv1.RollingUpdateDaemonSetStrategyType,
				RollingUpdate: &appsv1.RollingUpdateDaemonSet{MaxUnavailable: &intstr.IntOrString{Type: intstr.String, StrVal: "10%"}},
			}),
		},
		{
			description: "RollingUpdate update strategy string, invalid maxUnavailable",
			ds:          NewDaemonset(),
			dsSpec: gpuv1.DaemonsetsSpec{
				UpdateStrategy: "RollingUpdate",
				RollingUpdate: &gpuv1.RollingUpdateSpec{
					MaxUnavailable: "10%abc",
				}},
			errorExpected: true,
		},
		{
			description:   "OnDelete update strategy",
			ds:            NewDaemonset(),
			dsSpec:        gpuv1.DaemonsetsSpec{UpdateStrategy: "OnDelete"},
			errorExpected: false,
			expectedDs:    NewDaemonset().WithUpdateStrategy(appsv1.DaemonSetUpdateStrategy{Type: appsv1.OnDeleteDaemonSetStrategyType}),
		},
	}

	for _, tc := range testCases {
		t.Run(tc.description, func(t *testing.T) {
			cpSpec := &gpuv1.ClusterPolicySpec{
				Daemonsets: tc.dsSpec,
			}
			err := applyUpdateStrategyConfig(tc.ds.DaemonSet, cpSpec)
			if tc.errorExpected {
				require.Error(t, err)
				return
			}
			require.NoError(t, err)
			require.EqualValues(t, tc.expectedDs, tc.ds)
		})
	}
}

func TestApplyCommonDaemonSetConfig(t *testing.T) {
	testCases := []struct {
		description   string
		ds            Daemonset
		dsSpec        gpuv1.DaemonsetsSpec
		errorExpected bool
		expectedDs    Daemonset
	}{
		{
			description: "empty daemonset spec configuration",
			ds:          NewDaemonset(),
			dsSpec:      gpuv1.DaemonsetsSpec{},
			expectedDs:  NewDaemonset(),
		},
		{
			description: "priorityclass configured",
			ds:          NewDaemonset(),
			dsSpec:      gpuv1.DaemonsetsSpec{PriorityClassName: "test-priority-class"},
			expectedDs:  NewDaemonset().WithPriorityClass("test-priority-class"),
		},
		{
			description: "toleration configured",
			ds:          NewDaemonset(),
			dsSpec: gpuv1.DaemonsetsSpec{
				Tolerations: []corev1.Toleration{
					{
						Key:      "test-key",
						Operator: corev1.TolerationOpExists,
						Effect:   corev1.TaintEffectNoSchedule,
					},
				},
			},
			expectedDs: NewDaemonset().WithTolerations([]corev1.Toleration{
				{
					Key:      "test-key",
					Operator: corev1.TolerationOpExists,
					Effect:   corev1.TaintEffectNoSchedule,
				},
			}),
		},
		{
			description: "invalid updatestrategy configured",
			ds:          NewDaemonset(),
			dsSpec: gpuv1.DaemonsetsSpec{
				UpdateStrategy: "RollingUpdate",
				RollingUpdate: &gpuv1.RollingUpdateSpec{
					MaxUnavailable: "10%abc",
				}},
			errorExpected: true,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.description, func(t *testing.T) {
			cpSpec := &gpuv1.ClusterPolicySpec{
				Daemonsets: tc.dsSpec,
			}
			err := applyCommonDaemonsetConfig(tc.ds.DaemonSet, cpSpec)
			if tc.errorExpected {
				require.Error(t, err)
				return
			}
			require.NoError(t, err)
			require.EqualValues(t, tc.expectedDs, tc.ds)
		})
	}
}

func TestApplyCommonDaemonsetMetadata(t *testing.T) {
	testCases := []struct {
		description string
		ds          Daemonset
		dsSpec      gpuv1.DaemonsetsSpec
		expectedDs  Daemonset
	}{
		{
			description: "empty daemonset spec configuration",
			ds:          NewDaemonset(),
			dsSpec:      gpuv1.DaemonsetsSpec{},
			expectedDs:  NewDaemonset(),
		},
		{
			description: "common daemonset labels configured",
			ds:          NewDaemonset(),
			dsSpec: gpuv1.DaemonsetsSpec{Labels: map[string]string{
				"key":                       "value",
				"app":                       "value",
				"app.kubernetes.io/part-of": "value",
			}},
			expectedDs: NewDaemonset().WithPodLabels(map[string]string{
				"key": "value",
			}),
		},
		{
			description: "common daemonset annotations configured",
			ds:          NewDaemonset(),
			dsSpec: gpuv1.DaemonsetsSpec{Annotations: map[string]string{
				"key":                       "value",
				"app":                       "value",
				"app.kubernetes.io/part-of": "value",
			}},
			expectedDs: NewDaemonset().WithPodAnnotations(map[string]string{
				"key":                       "value",
				"app":                       "value",
				"app.kubernetes.io/part-of": "value",
			}),
		},
	}

	for _, tc := range testCases {
		t.Run(tc.description, func(t *testing.T) {
			applyCommonDaemonsetMetadata(tc.ds.DaemonSet, &tc.dsSpec)
			require.EqualValues(t, tc.expectedDs, tc.ds)
		})
	}
}
