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
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/intstr"
	ctrl "sigs.k8s.io/controller-runtime"

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
				Spec: corev1.PodSpec{},
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

func (d Daemonset) WithInitContainer(container corev1.Container) Daemonset {
	d.Spec.Template.Spec.InitContainers = append(d.Spec.Template.Spec.InitContainers, container)
	return d
}

func (d Daemonset) WithContainer(container corev1.Container) Daemonset {
	d.Spec.Template.Spec.Containers = append(d.Spec.Template.Spec.Containers, container)
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

func (d Daemonset) WithPullSecret(secret string) Daemonset {
	d.Spec.Template.Spec.ImagePullSecrets = []corev1.LocalObjectReference{{Name: secret}}
	return d
}

func (d Daemonset) WithRuntimeClassName(name string) Daemonset {
	d.Spec.Template.Spec.RuntimeClassName = &name
	return d
}

// Pod is a Pod wrapper used for testing
type Pod struct {
	*corev1.Pod
}

func NewPod() Pod {
	pod := &corev1.Pod{
		Spec: corev1.PodSpec{},
	}
	return Pod{pod}
}

func (p Pod) WithInitContainer(container corev1.Container) Pod {
	p.Spec.InitContainers = append(p.Spec.InitContainers, container)
	return p
}

func (p Pod) WithRuntimeClassName(name string) Pod {
	p.Spec.RuntimeClassName = &name
	return p
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
				WithHostPathVolume(hostDevCharVolumeName, "/", nil).
				WithContainer(corev1.Container{Name: "test-ctr"}),
			expectedOutput: NewDaemonset().
				WithHostPathVolume(hostRootVolumeName, "/custom-root", nil).
				WithHostPathVolume(hostDevCharVolumeName, "/custom-root/dev/char", nil).
				WithContainer(corev1.Container{Name: "test-ctr", Env: []corev1.EnvVar{{Name: HostRootEnvName, Value: "/custom-root"}}}),
		},
		{
			description: "custom host root with host-root volume",
			hostRoot:    "/custom-root",
			input: NewDaemonset().
				WithHostPathVolume(hostRootVolumeName, "/", nil).
				WithContainer(corev1.Container{Name: "test-ctr"}),
			expectedOutput: NewDaemonset().
				WithHostPathVolume(hostRootVolumeName, "/custom-root", nil).
				WithContainer(corev1.Container{Name: "test-ctr", Env: []corev1.EnvVar{{Name: HostRootEnvName, Value: "/custom-root"}}}),
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
			input: NewDaemonset().
				WithContainer(corev1.Container{Name: "test-ctr"}),
			expectedOutput: NewDaemonset().
				WithHostPathVolume("containerd-config", filepath.Dir(DefaultContainerdConfigFile), newHostPathType(corev1.HostPathDirectoryOrCreate)).
				WithHostPathVolume("containerd-socket", filepath.Dir(DefaultContainerdSocketFile), nil).
				WithContainer(corev1.Container{
					Name: "test-ctr",
					Env: []corev1.EnvVar{
						{Name: "RUNTIME", Value: gpuv1.Containerd.String()},
						{Name: "CONTAINERD_RUNTIME_CLASS", Value: DefaultRuntimeClass},
						{Name: "RUNTIME_CONFIG", Value: filepath.Join(DefaultRuntimeConfigTargetDir, filepath.Base(DefaultContainerdConfigFile))},
						{Name: "CONTAINERD_CONFIG", Value: filepath.Join(DefaultRuntimeConfigTargetDir, filepath.Base(DefaultContainerdConfigFile))},
						{Name: "RUNTIME_SOCKET", Value: filepath.Join(DefaultRuntimeSocketTargetDir, filepath.Base(DefaultContainerdSocketFile))},
						{Name: "CONTAINERD_SOCKET", Value: filepath.Join(DefaultRuntimeSocketTargetDir, filepath.Base(DefaultContainerdSocketFile))},
					},
					VolumeMounts: []corev1.VolumeMount{
						{Name: "containerd-config", MountPath: DefaultRuntimeConfigTargetDir},
						{Name: "containerd-socket", MountPath: DefaultRuntimeSocketTargetDir},
					},
				}),
		},
		{
			description: "crio",
			runtime:     gpuv1.CRIO,
			input:       NewDaemonset().WithContainer(corev1.Container{Name: "test-ctr"}),
			expectedOutput: NewDaemonset().
				WithHostPathVolume("crio-config", filepath.Dir(DefaultCRIOConfigFile), newHostPathType(corev1.HostPathDirectoryOrCreate)).
				WithContainer(corev1.Container{
					Name: "test-ctr",
					Env: []corev1.EnvVar{
						{Name: "RUNTIME", Value: gpuv1.CRIO.String()},
						{Name: "RUNTIME_CONFIG", Value: filepath.Join(DefaultRuntimeConfigTargetDir, filepath.Base(DefaultCRIOConfigFile))},
						{Name: "CRIO_CONFIG", Value: filepath.Join(DefaultRuntimeConfigTargetDir, filepath.Base(DefaultCRIOConfigFile))},
					},
					VolumeMounts: []corev1.VolumeMount{
						{Name: "crio-config", MountPath: DefaultRuntimeConfigTargetDir},
					},
				}),
		},
	}

	cp := &gpuv1.ClusterPolicySpec{Operator: gpuv1.OperatorSpec{RuntimeClass: DefaultRuntimeClass}}
	for _, tc := range testCases {
		t.Run(tc.description, func(t *testing.T) {
			err := transformForRuntime(tc.input.DaemonSet, cp, tc.runtime.String(), "test-ctr")
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

func TestTransformValidationInitContainer(t *testing.T) {
	testCases := []struct {
		description string
		ds          Daemonset
		cpSpec      *gpuv1.ClusterPolicySpec
		expectedDs  Daemonset
	}{
		{
			description: "transform both driver and toolkit validation initContainers",
			ds: NewDaemonset().
				WithInitContainer(corev1.Container{Name: "driver-validation"}).
				WithInitContainer(corev1.Container{Name: "toolkit-validation"}).
				WithInitContainer(corev1.Container{Name: "dummy"}),
			cpSpec: &gpuv1.ClusterPolicySpec{
				Validator: gpuv1.ValidatorSpec{
					Repository:       "nvcr.io/nvidia/cloud-native",
					Image:            "gpu-operator-validator",
					Version:          "v1.0.0",
					ImagePullPolicy:  "IfNotPresent",
					ImagePullSecrets: []string{"pull-secret"},
					Driver: gpuv1.DriverValidatorSpec{
						Env: []gpuv1.EnvVar{{Name: "foo", Value: "bar"}},
					},
					Toolkit: gpuv1.ToolkitValidatorSpec{
						Env: []gpuv1.EnvVar{{Name: "foo", Value: "bar"}},
					},
				},
			},
			expectedDs: NewDaemonset().WithInitContainer(corev1.Container{
				Name:            "driver-validation",
				Image:           "nvcr.io/nvidia/cloud-native/gpu-operator-validator:v1.0.0",
				ImagePullPolicy: corev1.PullIfNotPresent,
				Env:             []corev1.EnvVar{{Name: "foo", Value: "bar"}},
				SecurityContext: &corev1.SecurityContext{
					RunAsUser: rootUID,
				},
			}).WithInitContainer(corev1.Container{
				Name:            "toolkit-validation",
				Image:           "nvcr.io/nvidia/cloud-native/gpu-operator-validator:v1.0.0",
				ImagePullPolicy: corev1.PullIfNotPresent,
				Env:             []corev1.EnvVar{{Name: "foo", Value: "bar"}},
				SecurityContext: &corev1.SecurityContext{
					RunAsUser: rootUID,
				},
			}).WithInitContainer(corev1.Container{Name: "dummy"}).WithPullSecret("pull-secret"),
		},
	}

	for _, tc := range testCases {
		t.Run(tc.description, func(t *testing.T) {
			err := transformValidationInitContainer(tc.ds.DaemonSet, tc.cpSpec)
			require.NoError(t, err)
			require.EqualValues(t, tc.expectedDs, tc.ds)
		})
	}
}

func newBoolPtr(b bool) *bool {
	boolPtr := new(bool)
	*boolPtr = b
	return boolPtr
}

func TestTransformDriverManagerInitContainer(t *testing.T) {
	testCases := []struct {
		description string
		ds          Daemonset
		cpSpec      *gpuv1.ClusterPolicySpec
		expectedDs  Daemonset
	}{
		{
			description: "transform k8s-driver-manager initContainer",
			ds: NewDaemonset().
				WithInitContainer(corev1.Container{Name: "k8s-driver-manager"}).
				WithInitContainer(corev1.Container{Name: "dummy"}),
			cpSpec: &gpuv1.ClusterPolicySpec{
				Driver: gpuv1.DriverSpec{
					Manager: gpuv1.DriverManagerSpec{
						Repository:       "nvcr.io/nvidia/cloud-native",
						Image:            "k8s-driver-manager",
						Version:          "v1.0.0",
						ImagePullPolicy:  "IfNotPresent",
						ImagePullSecrets: []string{"pull-secret"},
						Env:              []gpuv1.EnvVar{{Name: "foo", Value: "bar"}},
					},
					GPUDirectRDMA: &gpuv1.GPUDirectRDMASpec{
						Enabled:      newBoolPtr(true),
						UseHostMOFED: newBoolPtr(true),
					},
				},
			},
			expectedDs: NewDaemonset().WithInitContainer(corev1.Container{
				Name:            "k8s-driver-manager",
				Image:           "nvcr.io/nvidia/cloud-native/k8s-driver-manager:v1.0.0",
				ImagePullPolicy: corev1.PullIfNotPresent,
				Env: []corev1.EnvVar{
					{Name: GPUDirectRDMAEnabledEnvName, Value: "true"},
					{Name: UseHostMOFEDEnvName, Value: "true"},
					{Name: "foo", Value: "bar"},
				},
			}).WithInitContainer(corev1.Container{Name: "dummy"}).WithPullSecret("pull-secret"),
		},
	}

	for _, tc := range testCases {
		t.Run(tc.description, func(t *testing.T) {
			err := transformDriverManagerInitContainer(tc.ds.DaemonSet, &tc.cpSpec.Driver.Manager, tc.cpSpec.Driver.GPUDirectRDMA)
			require.NoError(t, err)
			require.EqualValues(t, tc.expectedDs, tc.ds)
		})
	}
}

func TestTransformValidatorShared(t *testing.T) {
	testCases := []struct {
		description string
		ds          Daemonset
		cpSpec      *gpuv1.ClusterPolicySpec
		expectedDs  Daemonset
	}{
		{
			description: "transform validator daemonset's main container",
			ds:          NewDaemonset().WithContainer(corev1.Container{Name: "test-ctr"}),
			cpSpec: &gpuv1.ClusterPolicySpec{
				Validator: gpuv1.ValidatorSpec{
					Repository:       "nvcr.io/nvidia/cloud-native",
					Image:            "gpu-operator-validator",
					Version:          "v1.0.0",
					ImagePullPolicy:  "IfNotPresent",
					ImagePullSecrets: []string{"pull-secret"},
					Resources: &gpuv1.ResourceRequirements{
						Limits: corev1.ResourceList{
							corev1.ResourceCPU: resource.MustParse("500m"),
							"memory":           resource.MustParse("200Mi"),
						},
						Requests: corev1.ResourceList{
							corev1.ResourceCPU: resource.MustParse("500m"),
							"memory":           resource.MustParse("200Mi"),
						},
					},
					Args: []string{"--test-flag"},
					Env:  []gpuv1.EnvVar{{Name: "foo", Value: "bar"}},
				},
			},
			expectedDs: NewDaemonset().WithContainer(corev1.Container{
				Name:            "test-ctr",
				Image:           "nvcr.io/nvidia/cloud-native/gpu-operator-validator:v1.0.0",
				ImagePullPolicy: corev1.PullIfNotPresent,
				Resources: corev1.ResourceRequirements{
					Limits: corev1.ResourceList{
						corev1.ResourceCPU: resource.MustParse("500m"),
						"memory":           resource.MustParse("200Mi"),
					},
					Requests: corev1.ResourceList{
						corev1.ResourceCPU: resource.MustParse("500m"),
						"memory":           resource.MustParse("200Mi"),
					},
				},
				Args: []string{"--test-flag"},
				Env:  []corev1.EnvVar{{Name: "foo", Value: "bar"}},
				SecurityContext: &corev1.SecurityContext{
					RunAsUser: rootUID,
				},
			}).WithPullSecret("pull-secret"),
		},
	}

	for _, tc := range testCases {
		t.Run(tc.description, func(t *testing.T) {
			err := TransformValidatorShared(tc.ds.DaemonSet, tc.cpSpec)
			require.NoError(t, err)
			require.EqualValues(t, tc.expectedDs, tc.ds)
		})
	}
}

func TestTransformValidatorComponent(t *testing.T) {
	testCases := []struct {
		description   string
		pod           Pod
		cpSpec        *gpuv1.ClusterPolicySpec
		component     string
		expectedPod   Pod
		errorExpected bool
	}{
		{
			description: "no validation init container is a no-op",
			pod:         NewPod(),
			cpSpec:      nil,
			component:   "driver",
			expectedPod: NewPod(),
		},
		{
			description: "invalid component",
			pod:         NewPod().WithInitContainer(corev1.Container{Name: "invalid-validation"}),
			cpSpec: &gpuv1.ClusterPolicySpec{
				Validator: gpuv1.ValidatorSpec{},
			},
			component:     "invalid",
			expectedPod:   NewPod(),
			errorExpected: true,
		},
		{
			description: "cuda validation",
			pod: NewPod().
				WithInitContainer(corev1.Container{Name: "cuda-validation"}).
				WithRuntimeClassName("nvidia"),
			cpSpec: &gpuv1.ClusterPolicySpec{
				Validator: gpuv1.ValidatorSpec{
					Repository:       "nvcr.io/nvidia/cloud-native",
					Image:            "gpu-operator-validator",
					Version:          "v1.0.0",
					ImagePullPolicy:  "IfNotPresent",
					ImagePullSecrets: []string{"pull-secret1", "pull-secret2"},
					CUDA: gpuv1.CUDAValidatorSpec{
						Env: []gpuv1.EnvVar{{Name: "foo", Value: "bar"}},
					},
				},
			},
			component: "cuda",
			expectedPod: NewPod().WithInitContainer(corev1.Container{
				Name:            "cuda-validation",
				Image:           "nvcr.io/nvidia/cloud-native/gpu-operator-validator:v1.0.0",
				ImagePullPolicy: corev1.PullIfNotPresent,
				Env: []corev1.EnvVar{
					{Name: "foo", Value: "bar"},
					{Name: ValidatorImageEnvName, Value: "nvcr.io/nvidia/cloud-native/gpu-operator-validator:v1.0.0"},
					{Name: ValidatorImagePullPolicyEnvName, Value: "IfNotPresent"},
					{Name: ValidatorImagePullSecretsEnvName, Value: "pull-secret1,pull-secret2"},
					{Name: ValidatorRuntimeClassEnvName, Value: "nvidia"},
				},
				SecurityContext: &corev1.SecurityContext{
					RunAsUser: rootUID,
				},
			}).WithRuntimeClassName("nvidia"),
		},
		{
			description: "plugin validation",
			pod: NewPod().
				WithInitContainer(corev1.Container{Name: "plugin-validation"}).
				WithRuntimeClassName("nvidia"),
			cpSpec: &gpuv1.ClusterPolicySpec{
				Validator: gpuv1.ValidatorSpec{
					Repository:       "nvcr.io/nvidia/cloud-native",
					Image:            "gpu-operator-validator",
					Version:          "v1.0.0",
					ImagePullPolicy:  "IfNotPresent",
					ImagePullSecrets: []string{"pull-secret1", "pull-secret2"},
					Plugin: gpuv1.PluginValidatorSpec{
						Env: []gpuv1.EnvVar{{Name: "foo", Value: "bar"}},
					},
				},
				MIG: gpuv1.MIGSpec{
					Strategy: gpuv1.MIGStrategySingle,
				},
			},
			component: "plugin",
			expectedPod: NewPod().WithInitContainer(corev1.Container{
				Name:            "plugin-validation",
				Image:           "nvcr.io/nvidia/cloud-native/gpu-operator-validator:v1.0.0",
				ImagePullPolicy: corev1.PullIfNotPresent,
				Env: []corev1.EnvVar{
					{Name: "foo", Value: "bar"},
					{Name: ValidatorImageEnvName, Value: "nvcr.io/nvidia/cloud-native/gpu-operator-validator:v1.0.0"},
					{Name: ValidatorImagePullPolicyEnvName, Value: "IfNotPresent"},
					{Name: ValidatorImagePullSecretsEnvName, Value: "pull-secret1,pull-secret2"},
					{Name: ValidatorRuntimeClassEnvName, Value: "nvidia"},
					{Name: MigStrategyEnvName, Value: string(gpuv1.MIGStrategySingle)},
				},
				SecurityContext: &corev1.SecurityContext{
					RunAsUser: rootUID,
				},
			}).WithRuntimeClassName("nvidia"),
		},
		{
			description: "plugin validation removed when plugin is disabled",
			pod: NewPod().
				WithInitContainer(corev1.Container{Name: "plugin-validation"}).
				WithInitContainer(corev1.Container{Name: "dummy"}),
			cpSpec: &gpuv1.ClusterPolicySpec{
				Validator: gpuv1.ValidatorSpec{
					Repository:      "nvcr.io/nvidia/cloud-native",
					Image:           "gpu-operator-validator",
					Version:         "v1.0.0",
					ImagePullPolicy: "IfNotPresent",
				},
				DevicePlugin: gpuv1.DevicePluginSpec{Enabled: newBoolPtr(false)},
			},
			component:   "plugin",
			expectedPod: NewPod().WithInitContainer(corev1.Container{Name: "dummy"}),
		},
		{
			description: "driver validation",
			pod:         NewPod().WithInitContainer(corev1.Container{Name: "driver-validation"}),
			cpSpec: &gpuv1.ClusterPolicySpec{
				Validator: gpuv1.ValidatorSpec{
					Repository:      "nvcr.io/nvidia/cloud-native",
					Image:           "gpu-operator-validator",
					Version:         "v1.0.0",
					ImagePullPolicy: "IfNotPresent",
					Driver: gpuv1.DriverValidatorSpec{
						Env: []gpuv1.EnvVar{{Name: "foo", Value: "bar"}},
					},
				},
			},
			component: "driver",
			expectedPod: NewPod().WithInitContainer(corev1.Container{
				Name:            "driver-validation",
				Image:           "nvcr.io/nvidia/cloud-native/gpu-operator-validator:v1.0.0",
				ImagePullPolicy: corev1.PullIfNotPresent,
				Env: []corev1.EnvVar{
					{Name: "foo", Value: "bar"},
				},
				SecurityContext: &corev1.SecurityContext{
					RunAsUser: rootUID,
				},
			}),
		},
		{
			description: "nvidia-fs validation",
			pod:         NewPod().WithInitContainer(corev1.Container{Name: "nvidia-fs-validation"}),
			cpSpec: &gpuv1.ClusterPolicySpec{
				Validator: gpuv1.ValidatorSpec{
					Repository:      "nvcr.io/nvidia/cloud-native",
					Image:           "gpu-operator-validator",
					Version:         "v1.0.0",
					ImagePullPolicy: "IfNotPresent",
				},
				GPUDirectStorage: &gpuv1.GPUDirectStorageSpec{Enabled: newBoolPtr(true)},
			},
			component: "nvidia-fs",
			expectedPod: NewPod().WithInitContainer(corev1.Container{
				Name:            "nvidia-fs-validation",
				Image:           "nvcr.io/nvidia/cloud-native/gpu-operator-validator:v1.0.0",
				ImagePullPolicy: corev1.PullIfNotPresent,
				SecurityContext: &corev1.SecurityContext{
					RunAsUser: rootUID,
				},
			}),
		},
		{
			description: "nvidia-fs validation is removed when gds is disabled",
			pod: NewPod().
				WithInitContainer(corev1.Container{Name: "nvidia-fs-validation"}).
				WithInitContainer(corev1.Container{Name: "dummy"}),
			cpSpec: &gpuv1.ClusterPolicySpec{
				Validator: gpuv1.ValidatorSpec{
					Repository:      "nvcr.io/nvidia/cloud-native",
					Image:           "gpu-operator-validator",
					Version:         "v1.0.0",
					ImagePullPolicy: "IfNotPresent",
				},
				GPUDirectStorage: &gpuv1.GPUDirectStorageSpec{Enabled: newBoolPtr(false)},
			},
			component:   "nvidia-fs",
			expectedPod: NewPod().WithInitContainer(corev1.Container{Name: "dummy"}),
		},
		{
			description: "cc-manager validation",
			pod:         NewPod().WithInitContainer(corev1.Container{Name: "cc-manager-validation"}),
			cpSpec: &gpuv1.ClusterPolicySpec{
				Validator: gpuv1.ValidatorSpec{
					Repository:      "nvcr.io/nvidia/cloud-native",
					Image:           "gpu-operator-validator",
					Version:         "v1.0.0",
					ImagePullPolicy: "IfNotPresent",
				},
				CCManager: gpuv1.CCManagerSpec{Enabled: newBoolPtr(true)},
			},
			component: "cc-manager",
			expectedPod: NewPod().WithInitContainer(corev1.Container{
				Name:            "cc-manager-validation",
				Image:           "nvcr.io/nvidia/cloud-native/gpu-operator-validator:v1.0.0",
				ImagePullPolicy: corev1.PullIfNotPresent,
				SecurityContext: &corev1.SecurityContext{
					RunAsUser: rootUID,
				},
			}),
		},
		{
			description: "cc-manager validation is removed when cc-manager is disabled",
			pod: NewPod().
				WithInitContainer(corev1.Container{Name: "cc-manager-validation"}).
				WithInitContainer(corev1.Container{Name: "dummy"}),
			cpSpec: &gpuv1.ClusterPolicySpec{
				Validator: gpuv1.ValidatorSpec{
					Repository:      "nvcr.io/nvidia/cloud-native",
					Image:           "gpu-operator-validator",
					Version:         "v1.0.0",
					ImagePullPolicy: "IfNotPresent",
				},
				CCManager: gpuv1.CCManagerSpec{Enabled: newBoolPtr(false)},
			},
			component:   "cc-manager",
			expectedPod: NewPod().WithInitContainer(corev1.Container{Name: "dummy"}),
		},
		{
			description: "toolkit validation",
			pod:         NewPod().WithInitContainer(corev1.Container{Name: "toolkit-validation"}),
			cpSpec: &gpuv1.ClusterPolicySpec{
				Validator: gpuv1.ValidatorSpec{
					Repository:      "nvcr.io/nvidia/cloud-native",
					Image:           "gpu-operator-validator",
					Version:         "v1.0.0",
					ImagePullPolicy: "IfNotPresent",
					Toolkit: gpuv1.ToolkitValidatorSpec{
						Env: []gpuv1.EnvVar{{Name: "foo", Value: "bar"}},
					},
				},
			},
			component: "toolkit",
			expectedPod: NewPod().WithInitContainer(corev1.Container{
				Name:            "toolkit-validation",
				Image:           "nvcr.io/nvidia/cloud-native/gpu-operator-validator:v1.0.0",
				ImagePullPolicy: corev1.PullIfNotPresent,
				Env: []corev1.EnvVar{
					{Name: "foo", Value: "bar"},
				},
				SecurityContext: &corev1.SecurityContext{
					RunAsUser: rootUID,
				},
			}),
		},
		{
			description: "vfio-pci validation",
			pod:         NewPod().WithInitContainer(corev1.Container{Name: "vfio-pci-validation"}),
			cpSpec: &gpuv1.ClusterPolicySpec{
				Validator: gpuv1.ValidatorSpec{
					Repository:      "nvcr.io/nvidia/cloud-native",
					Image:           "gpu-operator-validator",
					Version:         "v1.0.0",
					ImagePullPolicy: "IfNotPresent",
					VFIOPCI: gpuv1.VFIOPCIValidatorSpec{
						Env: []gpuv1.EnvVar{{Name: "foo", Value: "bar"}},
					},
				},
			},
			component: "vfio-pci",
			expectedPod: NewPod().WithInitContainer(corev1.Container{
				Name:            "vfio-pci-validation",
				Image:           "nvcr.io/nvidia/cloud-native/gpu-operator-validator:v1.0.0",
				ImagePullPolicy: corev1.PullIfNotPresent,
				Env: []corev1.EnvVar{
					{Name: "DEFAULT_GPU_WORKLOAD_CONFIG", Value: defaultGPUWorkloadConfig},
					{Name: "foo", Value: "bar"},
				},
				SecurityContext: &corev1.SecurityContext{
					RunAsUser: rootUID,
				},
			}),
		},
		{
			description: "vgpu-manager validation",
			pod:         NewPod().WithInitContainer(corev1.Container{Name: "vgpu-manager-validation"}),
			cpSpec: &gpuv1.ClusterPolicySpec{
				Validator: gpuv1.ValidatorSpec{
					Repository:      "nvcr.io/nvidia/cloud-native",
					Image:           "gpu-operator-validator",
					Version:         "v1.0.0",
					ImagePullPolicy: "IfNotPresent",
					VGPUManager: gpuv1.VGPUManagerValidatorSpec{
						Env: []gpuv1.EnvVar{{Name: "foo", Value: "bar"}},
					},
				},
			},
			component: "vgpu-manager",
			expectedPod: NewPod().WithInitContainer(corev1.Container{
				Name:            "vgpu-manager-validation",
				Image:           "nvcr.io/nvidia/cloud-native/gpu-operator-validator:v1.0.0",
				ImagePullPolicy: corev1.PullIfNotPresent,
				Env: []corev1.EnvVar{
					{Name: "DEFAULT_GPU_WORKLOAD_CONFIG", Value: defaultGPUWorkloadConfig},
					{Name: "foo", Value: "bar"},
				},
				SecurityContext: &corev1.SecurityContext{
					RunAsUser: rootUID,
				},
			}),
		},
		{
			description: "vgpu-devices validation",
			pod:         NewPod().WithInitContainer(corev1.Container{Name: "vgpu-devices-validation"}),
			cpSpec: &gpuv1.ClusterPolicySpec{
				Validator: gpuv1.ValidatorSpec{
					Repository:      "nvcr.io/nvidia/cloud-native",
					Image:           "gpu-operator-validator",
					Version:         "v1.0.0",
					ImagePullPolicy: "IfNotPresent",
					VGPUDevices: gpuv1.VGPUDevicesValidatorSpec{
						Env: []gpuv1.EnvVar{{Name: "foo", Value: "bar"}},
					},
				},
			},
			component: "vgpu-devices",
			expectedPod: NewPod().WithInitContainer(corev1.Container{
				Name:            "vgpu-devices-validation",
				Image:           "nvcr.io/nvidia/cloud-native/gpu-operator-validator:v1.0.0",
				ImagePullPolicy: corev1.PullIfNotPresent,
				Env: []corev1.EnvVar{
					{Name: "DEFAULT_GPU_WORKLOAD_CONFIG", Value: defaultGPUWorkloadConfig},
					{Name: "foo", Value: "bar"},
				},
				SecurityContext: &corev1.SecurityContext{
					RunAsUser: rootUID,
				},
			}),
		},
	}

	for _, tc := range testCases {
		t.Run(tc.description, func(t *testing.T) {
			err := TransformValidatorComponent(tc.cpSpec, &tc.pod.Spec, tc.component)
			if tc.errorExpected {
				require.Error(t, err)
				return
			}
			require.NoError(t, err)
			require.EqualValues(t, tc.expectedPod, tc.pod)
		})
	}
}

func TestTransformValidator(t *testing.T) {
	testCases := []struct {
		description   string
		ds            Daemonset
		cpSpec        *gpuv1.ClusterPolicySpec
		expectedDs    Daemonset
		errorExpected bool
	}{
		{
			description: "empty validator spec",
			ds: NewDaemonset().
				WithInitContainer(corev1.Container{Name: "dummy"}).
				WithContainer(corev1.Container{Name: "dummy"}),
			cpSpec: &gpuv1.ClusterPolicySpec{
				Validator: gpuv1.ValidatorSpec{},
			},
			expectedDs:    NewDaemonset(),
			errorExpected: true,
		},
		{
			description: "valid validator spec",
			ds: NewDaemonset().
				WithInitContainer(corev1.Container{Name: "dummy"}).
				WithContainer(corev1.Container{Name: "dummy"}),
			cpSpec: &gpuv1.ClusterPolicySpec{
				Validator: gpuv1.ValidatorSpec{
					Repository:       "nvcr.io/nvidia/cloud-native",
					Image:            "gpu-operator-validator",
					Version:          "v1.0.0",
					ImagePullPolicy:  "IfNotPresent",
					ImagePullSecrets: []string{"pull-secret"},
				},
			},
			expectedDs: NewDaemonset().
				WithInitContainer(corev1.Container{Name: "dummy"}).
				WithContainer(corev1.Container{
					Name:            "dummy",
					Image:           "nvcr.io/nvidia/cloud-native/gpu-operator-validator:v1.0.0",
					ImagePullPolicy: corev1.PullIfNotPresent,
					SecurityContext: &corev1.SecurityContext{
						RunAsUser: rootUID,
					},
				}).
				WithPullSecret("pull-secret").
				WithRuntimeClassName("nvidia"),
		},
	}

	for _, tc := range testCases {
		t.Run(tc.description, func(t *testing.T) {
			err := TransformValidator(tc.ds.DaemonSet, tc.cpSpec, ClusterPolicyController{runtime: gpuv1.Containerd, logger: ctrl.Log.WithName("test")})
			if tc.errorExpected {
				require.Error(t, err)
				return
			}
			require.NoError(t, err)
			require.EqualValues(t, tc.expectedDs, tc.ds)
		})
	}
}

func TestTransformSandboxValidator(t *testing.T) {
	testCases := []struct {
		description   string
		ds            Daemonset
		cpSpec        *gpuv1.ClusterPolicySpec
		expectedDs    Daemonset
		errorExpected bool
	}{
		{
			description: "empty validator spec",
			ds: NewDaemonset().
				WithInitContainer(corev1.Container{Name: "dummy"}).
				WithContainer(corev1.Container{Name: "dummy"}),
			cpSpec: &gpuv1.ClusterPolicySpec{
				Validator: gpuv1.ValidatorSpec{},
			},
			expectedDs:    NewDaemonset(),
			errorExpected: true,
		},
		{
			description: "valid validator spec",
			ds: NewDaemonset().
				WithInitContainer(corev1.Container{Name: "dummy"}).
				WithContainer(corev1.Container{Name: "dummy"}),
			cpSpec: &gpuv1.ClusterPolicySpec{
				Validator: gpuv1.ValidatorSpec{
					Repository:       "nvcr.io/nvidia/cloud-native",
					Image:            "gpu-operator-validator",
					Version:          "v1.0.0",
					ImagePullPolicy:  "IfNotPresent",
					ImagePullSecrets: []string{"pull-secret"},
				},
			},
			expectedDs: NewDaemonset().
				WithInitContainer(corev1.Container{Name: "dummy"}).
				WithContainer(corev1.Container{
					Name:            "dummy",
					Image:           "nvcr.io/nvidia/cloud-native/gpu-operator-validator:v1.0.0",
					ImagePullPolicy: corev1.PullIfNotPresent,
					SecurityContext: &corev1.SecurityContext{
						RunAsUser: rootUID,
					},
				}).
				WithPullSecret("pull-secret"),
		},
	}

	for _, tc := range testCases {
		t.Run(tc.description, func(t *testing.T) {
			err := TransformSandboxValidator(tc.ds.DaemonSet, tc.cpSpec, ClusterPolicyController{runtime: gpuv1.Containerd, logger: ctrl.Log.WithName("test")})
			if tc.errorExpected {
				require.Error(t, err)
				return
			}
			require.NoError(t, err)
			require.EqualValues(t, tc.expectedDs, tc.ds)
		})
	}
}

func TestTransformNodeStatusExporter(t *testing.T) {
	testCases := []struct {
		description   string
		ds            Daemonset
		cpSpec        *gpuv1.ClusterPolicySpec
		expectedDs    Daemonset
		errorExpected bool
	}{
		{
			description: "empty node status exporter spec",
			ds: NewDaemonset().
				WithContainer(corev1.Container{Name: "dummy"}),
			cpSpec: &gpuv1.ClusterPolicySpec{
				NodeStatusExporter: gpuv1.NodeStatusExporterSpec{},
			},
			expectedDs:    NewDaemonset(),
			errorExpected: true,
		},
		{
			description: "valid node status exporter spec",
			ds: NewDaemonset().
				WithContainer(corev1.Container{Name: "dummy"}),
			cpSpec: &gpuv1.ClusterPolicySpec{
				NodeStatusExporter: gpuv1.NodeStatusExporterSpec{
					Repository:      "nvcr.io/nvidia/cloud-native",
					Image:           "node-status-exporter",
					Version:         "v1.0.0",
					ImagePullPolicy: "IfNotPresent",
				},
			},
			expectedDs: NewDaemonset().
				WithContainer(corev1.Container{
					Name:            "dummy",
					Image:           "nvcr.io/nvidia/cloud-native/node-status-exporter:v1.0.0",
					ImagePullPolicy: corev1.PullIfNotPresent,
					SecurityContext: &corev1.SecurityContext{
						RunAsUser: rootUID,
					},
				}),
		},
	}

	for _, tc := range testCases {
		t.Run(tc.description, func(t *testing.T) {
			err := TransformNodeStatusExporter(tc.ds.DaemonSet, tc.cpSpec, ClusterPolicyController{runtime: gpuv1.Containerd, logger: ctrl.Log.WithName("test")})
			if tc.errorExpected {
				require.Error(t, err)
				return
			}
			require.NoError(t, err)
			require.EqualValues(t, tc.expectedDs, tc.ds)
		})
	}
}
