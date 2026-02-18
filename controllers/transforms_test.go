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

	kata_v1alpha1 "github.com/NVIDIA/k8s-kata-manager/api/v1alpha1/config"
	"github.com/stretchr/testify/require"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/intstr"
	"k8s.io/utils/ptr"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	gpuv1 "github.com/NVIDIA/gpu-operator/api/nvidia/v1"
	"github.com/NVIDIA/gpu-operator/internal/consts"
)

var mockClientMap map[string]client.Client

func initMockK8sClients() {
	envSecret := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-env-secret",
			Namespace: "test-ns",
		},
	}

	node := &corev1.Node{
		ObjectMeta: metav1.ObjectMeta{
			Name: "test-node",
			Labels: map[string]string{
				nfdKernelLabelKey: "6.8.0-60-generic",
				commonGPULabelKey: "true",
			},
		},
	}

	secretEnvMockClient := fake.NewFakeClient(envSecret, node)

	mockClientMap = map[string]client.Client{
		"secret-env-client": secretEnvMockClient,
	}
}

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

func (d Daemonset) WithConfigMapVolume(name string, configMapName string, defaultMode int32) Daemonset {
	volume := corev1.Volume{
		Name: name,
		VolumeSource: corev1.VolumeSource{
			ConfigMap: &corev1.ConfigMapVolumeSource{
				LocalObjectReference: corev1.LocalObjectReference{
					Name: configMapName,
				},
				DefaultMode: &defaultMode,
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

func (d Daemonset) WithPodSecurityContext(psc *corev1.PodSecurityContext) Daemonset {
	d.Spec.Template.Spec.SecurityContext = psc
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

func (d Daemonset) WithHostNetwork(enabled bool) Daemonset {
	d.Spec.Template.Spec.HostNetwork = enabled
	return d
}

func (d Daemonset) WithDNSPolicy(policy corev1.DNSPolicy) Daemonset {
	d.Spec.Template.Spec.DNSPolicy = policy
	return d
}

func (d Daemonset) WithHostPID(enabled bool) Daemonset {
	d.Spec.Template.Spec.HostPID = enabled
	return d
}

func (d Daemonset) WithVolume(volume corev1.Volume) Daemonset {
	d.Spec.Template.Spec.Volumes = append(d.Spec.Template.Spec.Volumes, volume)
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

func TestFindContainerByName(t *testing.T) {
	containers := []corev1.Container{
		{Name: "config"},
		{Name: "target", Image: "initial"},
	}

	t.Run("found", func(t *testing.T) {
		result := findContainerByName(containers, "target")
		require.NotNil(t, result)
		require.Equal(t, "target", result.Name)
		result.Image = "updated"
		require.Equal(t, "updated", containers[1].Image)
	})

	t.Run("not found", func(t *testing.T) {
		result := findContainerByName(containers, "missing")
		require.Nil(t, result)
	})
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
				WithHostPathVolume("containerd-config", filepath.Dir(DefaultContainerdConfigFile), ptr.To(corev1.HostPathDirectoryOrCreate)).
				WithHostPathVolume("containerd-drop-in-config", "/etc/containerd/conf.d", ptr.To(corev1.HostPathDirectoryOrCreate)).
				WithHostPathVolume("containerd-socket", filepath.Dir(DefaultContainerdSocketFile), nil).
				WithContainer(corev1.Container{
					Name: "test-ctr",
					Env: []corev1.EnvVar{
						{Name: "RUNTIME", Value: gpuv1.Containerd.String()},
						{Name: "CONTAINERD_RUNTIME_CLASS", Value: DefaultRuntimeClass},
						{Name: "RUNTIME_CONFIG", Value: filepath.Join(DefaultRuntimeConfigTargetDir, filepath.Base(DefaultContainerdConfigFile))},
						{Name: "CONTAINERD_CONFIG", Value: filepath.Join(DefaultRuntimeConfigTargetDir, filepath.Base(DefaultContainerdConfigFile))},
						{Name: "RUNTIME_DROP_IN_CONFIG", Value: "/runtime/config-dir.d/99-nvidia.toml"},
						{Name: "RUNTIME_DROP_IN_CONFIG_HOST_PATH", Value: "/etc/containerd/conf.d/99-nvidia.toml"},
						{Name: "RUNTIME_SOCKET", Value: filepath.Join(DefaultRuntimeSocketTargetDir, filepath.Base(DefaultContainerdSocketFile))},
						{Name: "CONTAINERD_SOCKET", Value: filepath.Join(DefaultRuntimeSocketTargetDir, filepath.Base(DefaultContainerdSocketFile))},
					},
					VolumeMounts: []corev1.VolumeMount{
						{Name: "containerd-config", MountPath: DefaultRuntimeConfigTargetDir},
						{Name: "containerd-drop-in-config", MountPath: "/runtime/config-dir.d/"},
						{Name: "containerd-socket", MountPath: DefaultRuntimeSocketTargetDir},
					},
				}),
		},
		{
			description: "containerd, file config source preferred over command source",
			runtime:     gpuv1.Containerd,
			input: NewDaemonset().
				WithContainer(corev1.Container{
					Name: "test-ctr",
					Env: []corev1.EnvVar{
						{Name: "RUNTIME_CONFIG_SOURCE", Value: "file,command"},
					},
				}),
			expectedOutput: NewDaemonset().
				WithHostPathVolume("containerd-config", filepath.Dir(DefaultContainerdConfigFile), ptr.To(corev1.HostPathDirectoryOrCreate)).
				WithHostPathVolume("containerd-drop-in-config", "/etc/containerd/conf.d", ptr.To(corev1.HostPathDirectoryOrCreate)).
				WithHostPathVolume("containerd-socket", filepath.Dir(DefaultContainerdSocketFile), nil).
				WithContainer(corev1.Container{
					Name: "test-ctr",
					Env: []corev1.EnvVar{
						{Name: "RUNTIME_CONFIG_SOURCE", Value: "file,command"},
						{Name: "RUNTIME", Value: gpuv1.Containerd.String()},
						{Name: "CONTAINERD_RUNTIME_CLASS", Value: DefaultRuntimeClass},
						{Name: "RUNTIME_CONFIG", Value: filepath.Join(DefaultRuntimeConfigTargetDir, filepath.Base(DefaultContainerdConfigFile))},
						{Name: "CONTAINERD_CONFIG", Value: filepath.Join(DefaultRuntimeConfigTargetDir, filepath.Base(DefaultContainerdConfigFile))},
						{Name: "RUNTIME_DROP_IN_CONFIG", Value: "/runtime/config-dir.d/99-nvidia.toml"},
						{Name: "RUNTIME_DROP_IN_CONFIG_HOST_PATH", Value: "/etc/containerd/conf.d/99-nvidia.toml"},
						{Name: "RUNTIME_SOCKET", Value: filepath.Join(DefaultRuntimeSocketTargetDir, filepath.Base(DefaultContainerdSocketFile))},
						{Name: "CONTAINERD_SOCKET", Value: filepath.Join(DefaultRuntimeSocketTargetDir, filepath.Base(DefaultContainerdSocketFile))},
					},
					VolumeMounts: []corev1.VolumeMount{
						{Name: "containerd-config", MountPath: DefaultRuntimeConfigTargetDir},
						{Name: "containerd-drop-in-config", MountPath: "/runtime/config-dir.d/"},
						{Name: "containerd-socket", MountPath: DefaultRuntimeSocketTargetDir},
					},
				}),
		},
		{
			description: "containerd, custom config source configured",
			runtime:     gpuv1.Containerd,
			input: NewDaemonset().
				WithContainer(corev1.Container{
					Name: "test-ctr",
					Env: []corev1.EnvVar{
						{Name: "RUNTIME_CONFIG_SOURCE", Value: "file=/path/to/custom/source/config.toml,command"},
					},
				}),
			expectedOutput: NewDaemonset().
				WithHostPathVolume("containerd-config", filepath.Dir(DefaultContainerdConfigFile), ptr.To(corev1.HostPathDirectoryOrCreate)).
				WithHostPathVolume("containerd-drop-in-config", "/etc/containerd/conf.d", ptr.To(corev1.HostPathDirectoryOrCreate)).
				WithHostPathVolume("containerd-socket", filepath.Dir(DefaultContainerdSocketFile), nil).
				WithContainer(corev1.Container{
					Name: "test-ctr",
					Env: []corev1.EnvVar{
						{Name: "RUNTIME_CONFIG_SOURCE", Value: "file=/host/path/to/custom/source/config.toml,command"},
						{Name: "RUNTIME", Value: gpuv1.Containerd.String()},
						{Name: "CONTAINERD_RUNTIME_CLASS", Value: DefaultRuntimeClass},
						{Name: "RUNTIME_CONFIG", Value: filepath.Join(DefaultRuntimeConfigTargetDir, filepath.Base(DefaultContainerdConfigFile))},
						{Name: "CONTAINERD_CONFIG", Value: filepath.Join(DefaultRuntimeConfigTargetDir, filepath.Base(DefaultContainerdConfigFile))},
						{Name: "RUNTIME_DROP_IN_CONFIG", Value: "/runtime/config-dir.d/99-nvidia.toml"},
						{Name: "RUNTIME_DROP_IN_CONFIG_HOST_PATH", Value: "/etc/containerd/conf.d/99-nvidia.toml"},
						{Name: "RUNTIME_SOCKET", Value: filepath.Join(DefaultRuntimeSocketTargetDir, filepath.Base(DefaultContainerdSocketFile))},
						{Name: "CONTAINERD_SOCKET", Value: filepath.Join(DefaultRuntimeSocketTargetDir, filepath.Base(DefaultContainerdSocketFile))},
					},
					VolumeMounts: []corev1.VolumeMount{
						{Name: "containerd-config", MountPath: DefaultRuntimeConfigTargetDir},
						{Name: "containerd-drop-in-config", MountPath: "/runtime/config-dir.d/"},
						{Name: "containerd-socket", MountPath: DefaultRuntimeSocketTargetDir},
					},
				}),
		},
		{
			description: "crio",
			runtime:     gpuv1.CRIO,
			input:       NewDaemonset().WithContainer(corev1.Container{Name: "test-ctr"}),
			expectedOutput: NewDaemonset().
				WithHostPathVolume("crio-config", "/etc/crio", ptr.To(corev1.HostPathDirectoryOrCreate)).
				WithHostPathVolume("crio-drop-in-config", "/etc/crio/crio.conf.d", ptr.To(corev1.HostPathDirectoryOrCreate)).
				WithContainer(corev1.Container{
					Name: "test-ctr",
					Env: []corev1.EnvVar{
						{Name: "RUNTIME", Value: gpuv1.CRIO.String()},
						{Name: "RUNTIME_CONFIG", Value: "/runtime/config-dir/config.toml"},
						{Name: "CRIO_CONFIG", Value: "/runtime/config-dir/config.toml"},
						{Name: "RUNTIME_DROP_IN_CONFIG", Value: "/runtime/config-dir.d/99-nvidia.conf"},
						{Name: "RUNTIME_DROP_IN_CONFIG_HOST_PATH", Value: "/etc/crio/crio.conf.d/99-nvidia.conf"},
					},
					VolumeMounts: []corev1.VolumeMount{
						{Name: "crio-config", MountPath: DefaultRuntimeConfigTargetDir},
						{Name: "crio-drop-in-config", MountPath: "/runtime/config-dir.d/"},
					},
				}),
		},
		// Cover the kata-manager naming case
		{
			description: "containerd skips drop-in for kata manager",
			runtime:     gpuv1.Containerd,
			input: NewDaemonset().
				WithContainer(corev1.Container{Name: "nvidia-kata-manager"}),
			expectedOutput: NewDaemonset().
				WithHostPathVolume("containerd-config", filepath.Dir(DefaultContainerdConfigFile), ptr.To(corev1.HostPathDirectoryOrCreate)).
				WithHostPathVolume("containerd-socket", filepath.Dir(DefaultContainerdSocketFile), nil).
				WithContainer(corev1.Container{
					Name: "nvidia-kata-manager",
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
			description: "docker",
			runtime:     gpuv1.Docker,
			input:       NewDaemonset().WithContainer(corev1.Container{Name: "test-ctr"}),
			expectedOutput: NewDaemonset().
				WithHostPathVolume("docker-config", filepath.Dir(DefaultDockerConfigFile), ptr.To(corev1.HostPathDirectoryOrCreate)).
				WithHostPathVolume("docker-socket", filepath.Dir(DefaultDockerSocketFile), nil).
				WithContainer(corev1.Container{
					Name: "test-ctr",
					Env: []corev1.EnvVar{
						{Name: "RUNTIME", Value: gpuv1.Docker.String()},
						{Name: "RUNTIME_CONFIG", Value: filepath.Join(DefaultRuntimeConfigTargetDir, filepath.Base(DefaultDockerConfigFile))},
						{Name: "DOCKER_CONFIG", Value: filepath.Join(DefaultRuntimeConfigTargetDir, filepath.Base(DefaultDockerConfigFile))},
						{Name: "RUNTIME_SOCKET", Value: filepath.Join(DefaultRuntimeSocketTargetDir, filepath.Base(DefaultDockerSocketFile))},
						{Name: "DOCKER_SOCKET", Value: filepath.Join(DefaultRuntimeSocketTargetDir, filepath.Base(DefaultDockerSocketFile))},
					},
					VolumeMounts: []corev1.VolumeMount{
						{Name: "docker-config", MountPath: DefaultRuntimeConfigTargetDir},
						{Name: "docker-socket", MountPath: DefaultRuntimeSocketTargetDir},
					},
				}),
		},
	}

	cp := &gpuv1.ClusterPolicySpec{Operator: gpuv1.OperatorSpec{RuntimeClass: DefaultRuntimeClass}}
	for _, tc := range testCases {
		t.Run(tc.description, func(t *testing.T) {
			// pass pointer to the target container
			err := transformForRuntime(tc.input.DaemonSet, cp, tc.runtime.String(), &tc.input.Spec.Template.Spec.Containers[0])
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
		{
			description: "podSecurityContext configured",
			ds:          NewDaemonset(),
			dsSpec: gpuv1.DaemonsetsSpec{
				PodSecurityContext: &corev1.PodSecurityContext{
					RunAsUser:    ptr.To(int64(1000)),
					RunAsGroup:   ptr.To(int64(3000)),
					FSGroup:      ptr.To(int64(2000)),
					RunAsNonRoot: ptr.To(true),
				},
			},
			expectedDs: NewDaemonset().WithPodSecurityContext(&corev1.PodSecurityContext{
				RunAsUser:    ptr.To(int64(1000)),
				RunAsGroup:   ptr.To(int64(3000)),
				FSGroup:      ptr.To(int64(2000)),
				RunAsNonRoot: ptr.To(true),
			}),
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

func TestTransformToolkit(t *testing.T) {
	testCases := []struct {
		description string
		ds          Daemonset                // Input DaemonSet
		cpSpec      *gpuv1.ClusterPolicySpec // Input configuration
		runtime     gpuv1.Runtime
		expectedDs  Daemonset // Expected output DaemonSet
	}{
		{
			description: "transform nvidia-container-toolkit-ctr container",
			ds: NewDaemonset().
				WithContainer(corev1.Container{Name: "nvidia-container-toolkit-ctr"}),
			runtime: gpuv1.Containerd,
			cpSpec: &gpuv1.ClusterPolicySpec{
				Toolkit: gpuv1.ToolkitSpec{
					Repository:       "nvcr.io/nvidia/cloud-native",
					Image:            "nvidia-container-toolkit",
					Version:          "v1.0.0",
					ImagePullPolicy:  "IfNotPresent",
					ImagePullSecrets: []string{"pull-secret"},
					Resources: &gpuv1.ResourceRequirements{
						Limits: corev1.ResourceList{
							corev1.ResourceCPU:    resource.MustParse("100m"),
							corev1.ResourceMemory: resource.MustParse("100Mi"),
						},
						Requests: corev1.ResourceList{
							corev1.ResourceCPU:    resource.MustParse("50m"),
							corev1.ResourceMemory: resource.MustParse("50Mi"),
						},
					},
					Env: []gpuv1.EnvVar{
						{Name: "foo", Value: "bar"},
					},
				},
			},
			expectedDs: NewDaemonset().
				WithContainer(corev1.Container{
					Name:            "nvidia-container-toolkit-ctr",
					Image:           "nvcr.io/nvidia/cloud-native/nvidia-container-toolkit:v1.0.0",
					ImagePullPolicy: corev1.PullIfNotPresent,
					Resources: corev1.ResourceRequirements{
						Limits: corev1.ResourceList{
							corev1.ResourceCPU:    resource.MustParse("100m"),
							corev1.ResourceMemory: resource.MustParse("100Mi"),
						},
						Requests: corev1.ResourceList{
							corev1.ResourceCPU:    resource.MustParse("50m"),
							corev1.ResourceMemory: resource.MustParse("50Mi"),
						},
					},
					Env: []corev1.EnvVar{
						{Name: CDIEnabledEnvName, Value: "true"},
						{Name: NvidiaRuntimeSetAsDefaultEnvName, Value: "false"},
						{Name: NvidiaCtrRuntimeModeEnvName, Value: "cdi"},
						{Name: CRIOConfigModeEnvName, Value: "config"},
						{Name: "foo", Value: "bar"},
						{Name: "RUNTIME", Value: "containerd"},
						{Name: "CONTAINERD_RUNTIME_CLASS", Value: "nvidia"},
						{Name: "RUNTIME_CONFIG", Value: "/runtime/config-dir/config.toml"},
						{Name: "CONTAINERD_CONFIG", Value: "/runtime/config-dir/config.toml"},
						{Name: "RUNTIME_DROP_IN_CONFIG", Value: "/runtime/config-dir.d/99-nvidia.toml"},
						{Name: "RUNTIME_DROP_IN_CONFIG_HOST_PATH", Value: "/etc/containerd/conf.d/99-nvidia.toml"},
						{Name: "RUNTIME_SOCKET", Value: "/runtime/sock-dir/containerd.sock"},
						{Name: "CONTAINERD_SOCKET", Value: "/runtime/sock-dir/containerd.sock"},
					},
					VolumeMounts: []corev1.VolumeMount{
						{Name: "containerd-config", MountPath: "/runtime/config-dir/"},
						{Name: "containerd-drop-in-config", MountPath: "/runtime/config-dir.d/"},
						{Name: "containerd-socket", MountPath: "/runtime/sock-dir/"},
					},
				}).
				WithHostPathVolume("containerd-config", "/etc/containerd", ptr.To(corev1.HostPathDirectoryOrCreate)).
				WithHostPathVolume("containerd-drop-in-config", "/etc/containerd/conf.d", ptr.To(corev1.HostPathDirectoryOrCreate)).
				WithHostPathVolume("containerd-socket", "/run/containerd", nil).
				WithPullSecret("pull-secret"),
		},
		{
			description: "transform nvidia-container-toolkit-ctr container with custom ctr runtime socket",
			ds: NewDaemonset().
				WithContainer(corev1.Container{Name: "nvidia-container-toolkit-ctr"}),
			runtime: gpuv1.Containerd,
			cpSpec: &gpuv1.ClusterPolicySpec{
				Toolkit: gpuv1.ToolkitSpec{
					Repository:       "nvcr.io/nvidia/cloud-native",
					Image:            "nvidia-container-toolkit",
					Version:          "v1.17.0",
					ImagePullPolicy:  "IfNotPresent",
					ImagePullSecrets: []string{"pull-secret"},
					Resources: &gpuv1.ResourceRequirements{
						Limits: corev1.ResourceList{
							corev1.ResourceCPU:    resource.MustParse("100m"),
							corev1.ResourceMemory: resource.MustParse("100Mi"),
						},
						Requests: corev1.ResourceList{
							corev1.ResourceCPU:    resource.MustParse("50m"),
							corev1.ResourceMemory: resource.MustParse("50Mi"),
						},
					},
					Env: []gpuv1.EnvVar{
						{
							Name: "CONTAINERD_CONFIG", Value: "/var/lib/rancher/k3s/agent/etc/containerd/config.toml",
						},
						{
							Name: "CONTAINERD_SOCKET", Value: "/run/k3s/containerd/containerd.sock",
						},
						{
							Name: "CONTAINERD_RUNTIME_CLASS", Value: "nvidia",
						},
						{
							Name: "CONTAINERD_SET_AS_DEFAULT", Value: "true",
						},
					},
				},
			},
			expectedDs: NewDaemonset().
				WithContainer(corev1.Container{
					Name:            "nvidia-container-toolkit-ctr",
					Image:           "nvcr.io/nvidia/cloud-native/nvidia-container-toolkit:v1.17.0",
					ImagePullPolicy: corev1.PullIfNotPresent,
					Resources: corev1.ResourceRequirements{
						Limits: corev1.ResourceList{
							corev1.ResourceCPU:    resource.MustParse("100m"),
							corev1.ResourceMemory: resource.MustParse("100Mi"),
						},
						Requests: corev1.ResourceList{
							corev1.ResourceCPU:    resource.MustParse("50m"),
							corev1.ResourceMemory: resource.MustParse("50Mi"),
						},
					},
					Env: []corev1.EnvVar{
						{Name: CDIEnabledEnvName, Value: "true"},
						{Name: NvidiaRuntimeSetAsDefaultEnvName, Value: "false"},
						{Name: NvidiaCtrRuntimeModeEnvName, Value: "cdi"},
						{Name: CRIOConfigModeEnvName, Value: "config"},
						{Name: "CONTAINERD_CONFIG", Value: "/runtime/config-dir/config.toml"},
						{Name: "CONTAINERD_SOCKET", Value: "/runtime/sock-dir/containerd.sock"},
						{Name: "CONTAINERD_RUNTIME_CLASS", Value: "nvidia"},
						{Name: "CONTAINERD_SET_AS_DEFAULT", Value: "true"},
						{Name: "RUNTIME", Value: "containerd"},
						{Name: "RUNTIME_CONFIG", Value: "/runtime/config-dir/config.toml"},
						{Name: "RUNTIME_DROP_IN_CONFIG", Value: "/runtime/config-dir.d/99-nvidia.toml"},
						{Name: "RUNTIME_DROP_IN_CONFIG_HOST_PATH", Value: "/etc/containerd/conf.d/99-nvidia.toml"},
						{Name: "RUNTIME_SOCKET", Value: "/runtime/sock-dir/containerd.sock"},
					},
					VolumeMounts: []corev1.VolumeMount{
						{Name: "containerd-config", MountPath: "/runtime/config-dir/"},
						{Name: "containerd-drop-in-config", MountPath: "/runtime/config-dir.d/"},
						{Name: "containerd-socket", MountPath: "/runtime/sock-dir/"},
					},
				}).
				WithHostPathVolume("containerd-config", "/var/lib/rancher/k3s/agent/etc/containerd", ptr.To(corev1.HostPathDirectoryOrCreate)).
				WithHostPathVolume("containerd-drop-in-config", "/etc/containerd/conf.d", ptr.To(corev1.HostPathDirectoryOrCreate)).
				WithHostPathVolume("containerd-socket", "/run/k3s/containerd", nil).
				WithPullSecret("pull-secret"),
		},
		{
			description: "transform nvidia-container-toolkit-ctr container, cri-o runtime, cdi enabled",
			ds: NewDaemonset().
				WithContainer(corev1.Container{Name: "nvidia-container-toolkit-ctr"}),
			runtime: gpuv1.CRIO,
			cpSpec: &gpuv1.ClusterPolicySpec{
				Toolkit: gpuv1.ToolkitSpec{
					Repository: "nvcr.io/nvidia/cloud-native",
					Image:      "nvidia-container-toolkit",
					Version:    "v1.0.0",
				},
			},
			expectedDs: NewDaemonset().
				WithContainer(corev1.Container{
					Name:            "nvidia-container-toolkit-ctr",
					Image:           "nvcr.io/nvidia/cloud-native/nvidia-container-toolkit:v1.0.0",
					ImagePullPolicy: corev1.PullIfNotPresent,
					Env: []corev1.EnvVar{
						{Name: CDIEnabledEnvName, Value: "true"},
						{Name: NvidiaRuntimeSetAsDefaultEnvName, Value: "false"},
						{Name: NvidiaCtrRuntimeModeEnvName, Value: "cdi"},
						{Name: CRIOConfigModeEnvName, Value: "config"},
						{Name: "RUNTIME", Value: gpuv1.CRIO.String()},
						{Name: "RUNTIME_CONFIG", Value: "/runtime/config-dir/config.toml"},
						{Name: "CRIO_CONFIG", Value: "/runtime/config-dir/config.toml"},
						{Name: "RUNTIME_DROP_IN_CONFIG", Value: "/runtime/config-dir.d/99-nvidia.conf"},
						{Name: "RUNTIME_DROP_IN_CONFIG_HOST_PATH", Value: "/etc/crio/crio.conf.d/99-nvidia.conf"},
					},
					VolumeMounts: []corev1.VolumeMount{
						{Name: "crio-config", MountPath: DefaultRuntimeConfigTargetDir},
						{Name: "crio-drop-in-config", MountPath: "/runtime/config-dir.d/"},
					},
				}).
				WithHostPathVolume("crio-config", "/etc/crio", ptr.To(corev1.HostPathDirectoryOrCreate)).
				WithHostPathVolume("crio-drop-in-config", "/etc/crio/crio.conf.d", ptr.To(corev1.HostPathDirectoryOrCreate)),
		},
		{
			description: "transform nvidia-container-toolkit-ctr container, cri-o runtime, cdi disabled",
			ds: NewDaemonset().
				WithContainer(corev1.Container{Name: "nvidia-container-toolkit-ctr"}),
			runtime: gpuv1.CRIO,
			cpSpec: &gpuv1.ClusterPolicySpec{
				Toolkit: gpuv1.ToolkitSpec{
					Repository: "nvcr.io/nvidia/cloud-native",
					Image:      "nvidia-container-toolkit",
					Version:    "v1.0.0",
				},
				CDI: gpuv1.CDIConfigSpec{
					Enabled: newBoolPtr(false),
				},
			},
			expectedDs: NewDaemonset().
				WithContainer(corev1.Container{
					Name:            "nvidia-container-toolkit-ctr",
					Image:           "nvcr.io/nvidia/cloud-native/nvidia-container-toolkit:v1.0.0",
					ImagePullPolicy: corev1.PullIfNotPresent,
					Env: []corev1.EnvVar{
						{Name: CRIOConfigModeEnvName, Value: "hook"},
						{Name: "RUNTIME", Value: gpuv1.CRIO.String()},
						{Name: "RUNTIME_CONFIG", Value: "/runtime/config-dir/config.toml"},
						{Name: "CRIO_CONFIG", Value: "/runtime/config-dir/config.toml"},
						{Name: "RUNTIME_DROP_IN_CONFIG", Value: "/runtime/config-dir.d/99-nvidia.conf"},
						{Name: "RUNTIME_DROP_IN_CONFIG_HOST_PATH", Value: "/etc/crio/crio.conf.d/99-nvidia.conf"},
					},
					VolumeMounts: []corev1.VolumeMount{
						{Name: "crio-config", MountPath: DefaultRuntimeConfigTargetDir},
						{Name: "crio-drop-in-config", MountPath: "/runtime/config-dir.d/"},
					},
				}).
				WithHostPathVolume("crio-config", "/etc/crio", ptr.To(corev1.HostPathDirectoryOrCreate)).
				WithHostPathVolume("crio-drop-in-config", "/etc/crio/crio.conf.d", ptr.To(corev1.HostPathDirectoryOrCreate)),
		},
	}

	for _, tc := range testCases {
		t.Run(tc.description, func(t *testing.T) {
			controller := ClusterPolicyController{
				runtime: tc.runtime,
				logger:  ctrl.Log.WithName("test"),
			}

			err := TransformToolkit(tc.ds.DaemonSet, tc.cpSpec, controller)
			require.NoError(t, err)
			require.EqualValues(t, tc.expectedDs, tc.ds)
		})
	}
}

func TestTransformDevicePlugin(t *testing.T) {
	testCases := []struct {
		description string
		ds          Daemonset                // Input DaemonSet
		cpSpec      *gpuv1.ClusterPolicySpec // Input configuration
		expectedDs  Daemonset                // Expected output DaemonSet
	}{
		{
			description: "transform device plugin",
			ds: NewDaemonset().
				WithContainer(corev1.Container{Name: "nvidia-device-plugin"}).
				WithContainer(corev1.Container{Name: "dummy"}),
			cpSpec: &gpuv1.ClusterPolicySpec{
				DevicePlugin: gpuv1.DevicePluginSpec{
					Repository:       "nvcr.io/nvidia/cloud-native",
					Image:            "nvidia-device-plugin",
					Version:          "v1.0.0",
					ImagePullPolicy:  "IfNotPresent",
					ImagePullSecrets: []string{"pull-secret"},
					Args:             []string{"--fail-on-init-error=false"},
					Env: []gpuv1.EnvVar{
						{Name: "foo", Value: "bar"},
					},
				},
				Toolkit: gpuv1.ToolkitSpec{
					Enabled:    newBoolPtr(true),
					InstallDir: "/path/to/install",
				},
			},
			expectedDs: NewDaemonset().WithContainer(corev1.Container{
				Name:            "nvidia-device-plugin",
				Image:           "nvcr.io/nvidia/cloud-native/nvidia-device-plugin:v1.0.0",
				ImagePullPolicy: corev1.PullIfNotPresent,
				Args:            []string{"--fail-on-init-error=false"},
				Env: []corev1.EnvVar{
					{Name: "NVIDIA_MIG_MONITOR_DEVICES", Value: "all"},
					{Name: CDIEnabledEnvName, Value: "true"},
					{Name: DeviceListStrategyEnvName, Value: "cdi-annotations,cdi-cri"},
					{Name: CDIAnnotationPrefixEnvName, Value: "cdi.k8s.io/"},
					{Name: NvidiaCDIHookPathEnvName, Value: "/path/to/install/toolkit/nvidia-cdi-hook"},
					{Name: "foo", Value: "bar"},
				},
			}).WithContainer(corev1.Container{Name: "dummy"}).WithPullSecret("pull-secret").WithRuntimeClassName("nvidia"),
		},
		{
			description: "transform device plugin, gds and gdrcopy enabled",
			ds: NewDaemonset().
				WithContainer(corev1.Container{Name: "nvidia-device-plugin"}),
			cpSpec: &gpuv1.ClusterPolicySpec{
				DevicePlugin: gpuv1.DevicePluginSpec{
					Repository:      "nvcr.io/nvidia/cloud-native",
					Image:           "nvidia-device-plugin",
					Version:         "v1.0.0",
					ImagePullPolicy: "IfNotPresent",
				},
				Toolkit: gpuv1.ToolkitSpec{
					Enabled:    newBoolPtr(true),
					InstallDir: "/path/to/install",
				},
				GDRCopy: &gpuv1.GDRCopySpec{
					Enabled: newBoolPtr(true),
				},
				GPUDirectStorage: &gpuv1.GPUDirectStorageSpec{
					Enabled: newBoolPtr(true),
				},
			},
			expectedDs: NewDaemonset().WithContainer(corev1.Container{
				Name:            "nvidia-device-plugin",
				Image:           "nvcr.io/nvidia/cloud-native/nvidia-device-plugin:v1.0.0",
				ImagePullPolicy: corev1.PullIfNotPresent,
				Env: []corev1.EnvVar{
					{Name: GDSEnabledEnvName, Value: "true"},
					{Name: MOFEDEnabledEnvName, Value: "true"},
					{Name: GDRCopyEnabledEnvName, Value: "true"},
					{Name: "NVIDIA_MIG_MONITOR_DEVICES", Value: "all"},
					{Name: CDIEnabledEnvName, Value: "true"},
					{Name: DeviceListStrategyEnvName, Value: "cdi-annotations,cdi-cri"},
					{Name: CDIAnnotationPrefixEnvName, Value: "cdi.k8s.io/"},
					{Name: NvidiaCDIHookPathEnvName, Value: "/path/to/install/toolkit/nvidia-cdi-hook"},
				},
			}).WithRuntimeClassName("nvidia"),
		},
	}

	for _, tc := range testCases {
		t.Run(tc.description, func(t *testing.T) {
			err := TransformDevicePlugin(tc.ds.DaemonSet, tc.cpSpec, ClusterPolicyController{
				runtime: gpuv1.Containerd,
				logger:  ctrl.Log.WithName("test"),
			})
			require.NoError(t, err)
			require.EqualValues(t, tc.expectedDs, tc.ds)
		})
	}
}

func TestTransformMPSControlDaemon(t *testing.T) {
	testCases := []struct {
		description       string
		daemonset         Daemonset
		clusterPolicySpec *gpuv1.ClusterPolicySpec
		expectedDaemonset Daemonset
	}{
		{
			description: "transform mps control daemon",
			daemonset: NewDaemonset().
				WithInitContainer(corev1.Container{Name: "mps-control-daemon-mounts"}).
				WithContainer(corev1.Container{Name: "mps-control-daemon-ctr"}).
				WithHostPathVolume("mps-root", "/run/nvidia/mps", ptr.To(corev1.HostPathDirectoryOrCreate)).
				WithHostPathVolume("mps-shm", "/run/nvidia/mps/shm", ptr.To(corev1.HostPathDirectoryOrCreate)),
			clusterPolicySpec: &gpuv1.ClusterPolicySpec{
				DevicePlugin: gpuv1.DevicePluginSpec{
					Repository:       "nvcr.io",
					Image:            "mps",
					Version:          "latest",
					ImagePullPolicy:  string(corev1.PullAlways),
					ImagePullSecrets: []string{"secret"},
					MPS:              &gpuv1.MPSConfig{Root: "/var/mps"},
				},
			},
			expectedDaemonset: NewDaemonset().
				WithInitContainer(corev1.Container{
					Name:            "mps-control-daemon-mounts",
					Image:           "nvcr.io/mps:latest",
					ImagePullPolicy: corev1.PullAlways,
				}).
				WithContainer(corev1.Container{
					Name:            "mps-control-daemon-ctr",
					Image:           "nvcr.io/mps:latest",
					ImagePullPolicy: corev1.PullAlways,
					Env: []corev1.EnvVar{
						{Name: "NVIDIA_MIG_MONITOR_DEVICES", Value: "all"},
					},
				}).
				WithHostPathVolume("mps-root", "/var/mps", ptr.To(corev1.HostPathDirectoryOrCreate)).
				WithHostPathVolume("mps-shm", "/var/mps/shm", ptr.To(corev1.HostPathDirectoryOrCreate)).
				WithPullSecret("secret").
				WithRuntimeClassName("nvidia"),
		},
	}

	for _, tc := range testCases {
		t.Run(tc.description, func(t *testing.T) {
			err := TransformMPSControlDaemon(tc.daemonset.DaemonSet, tc.clusterPolicySpec, ClusterPolicyController{
				runtime: gpuv1.Containerd,
				logger:  ctrl.Log.WithName("test"),
			})
			require.NoError(t, err)
			require.EqualValues(t, tc.expectedDaemonset.DaemonSet, tc.daemonset.DaemonSet)
		})
	}
}

func TestTransformDCGMExporter(t *testing.T) {
	testCases := []struct {
		description      string
		ds               Daemonset                // Input DaemonSet
		cpSpec           *gpuv1.ClusterPolicySpec // Input configuration
		expectedDs       Daemonset                // Expected output DaemonSet
		openshiftVersion string
	}{
		{
			description: "transform dcgm exporter",
			ds: NewDaemonset().
				WithContainer(corev1.Container{Name: "dcgm-exporter"}).
				WithContainer(corev1.Container{Name: "dummy"}),
			cpSpec: &gpuv1.ClusterPolicySpec{
				DCGMExporter: gpuv1.DCGMExporterSpec{
					Repository:       "nvcr.io/nvidia/cloud-native",
					Image:            "dcgm-exporter",
					Version:          "v1.0.0",
					ImagePullPolicy:  "IfNotPresent",
					ImagePullSecrets: []string{"pull-secret"},
					Args:             []string{"--fail-on-init-error=false"},
					Env: []gpuv1.EnvVar{
						{Name: "foo", Value: "bar"},
					},
				},
				DCGM: gpuv1.DCGMSpec{
					Enabled: newBoolPtr(true),
				},
			},
			expectedDs: NewDaemonset().WithContainer(corev1.Container{
				Name:            "dcgm-exporter",
				Image:           "nvcr.io/nvidia/cloud-native/dcgm-exporter:v1.0.0",
				ImagePullPolicy: corev1.PullIfNotPresent,
				Args:            []string{"--fail-on-init-error=false"},
				Env: []corev1.EnvVar{
					{Name: "DCGM_REMOTE_HOSTENGINE_INFO", Value: "nvidia-dcgm:5555"},
					{Name: "foo", Value: "bar"},
				},
			}).WithContainer(corev1.Container{Name: "dummy"}).WithPullSecret("pull-secret").WithRuntimeClassName("nvidia"),
		},
		{
			description: "transform dcgm exporter with hostPID enabled",
			ds: NewDaemonset().
				WithContainer(corev1.Container{Name: "dcgm-exporter"}).
				WithContainer(corev1.Container{Name: "dummy"}),
			cpSpec: &gpuv1.ClusterPolicySpec{
				DCGMExporter: gpuv1.DCGMExporterSpec{
					Repository:       "nvcr.io/nvidia/cloud-native",
					Image:            "dcgm-exporter",
					Version:          "v1.0.0",
					ImagePullPolicy:  "IfNotPresent",
					ImagePullSecrets: []string{"pull-secret"},
					Args:             []string{"--fail-on-init-error=false"},
					HostPID:          newBoolPtr(true),
					Env: []gpuv1.EnvVar{
						{Name: "foo", Value: "bar"},
					},
				},
				DCGM: gpuv1.DCGMSpec{
					Enabled: newBoolPtr(true),
				},
			},
			expectedDs: NewDaemonset().WithContainer(corev1.Container{
				Name:            "dcgm-exporter",
				Image:           "nvcr.io/nvidia/cloud-native/dcgm-exporter:v1.0.0",
				ImagePullPolicy: corev1.PullIfNotPresent,
				Args:            []string{"--fail-on-init-error=false"},
				Env: []corev1.EnvVar{
					{Name: "DCGM_REMOTE_HOSTENGINE_INFO", Value: "nvidia-dcgm:5555"},
					{Name: "foo", Value: "bar"},
				},
			}).WithContainer(corev1.Container{Name: "dummy"}).WithPullSecret("pull-secret").WithRuntimeClassName("nvidia").WithHostPID(true),
		},
		{
			description: "transform dcgm exporter with hostPID disabled",
			ds: NewDaemonset().
				WithContainer(corev1.Container{Name: "dcgm-exporter"}).
				WithContainer(corev1.Container{Name: "dummy"}),
			cpSpec: &gpuv1.ClusterPolicySpec{
				DCGMExporter: gpuv1.DCGMExporterSpec{
					Repository:       "nvcr.io/nvidia/cloud-native",
					Image:            "dcgm-exporter",
					Version:          "v1.0.0",
					ImagePullPolicy:  "IfNotPresent",
					ImagePullSecrets: []string{"pull-secret"},
					Args:             []string{"--fail-on-init-error=false"},
					HostPID:          newBoolPtr(false),
					Env: []gpuv1.EnvVar{
						{Name: "foo", Value: "bar"},
					},
				},
				DCGM: gpuv1.DCGMSpec{
					Enabled: newBoolPtr(true),
				},
			},
			expectedDs: NewDaemonset().WithContainer(corev1.Container{
				Name:            "dcgm-exporter",
				Image:           "nvcr.io/nvidia/cloud-native/dcgm-exporter:v1.0.0",
				ImagePullPolicy: corev1.PullIfNotPresent,
				Args:            []string{"--fail-on-init-error=false"},
				Env: []corev1.EnvVar{
					{Name: "DCGM_REMOTE_HOSTENGINE_INFO", Value: "nvidia-dcgm:5555"},
					{Name: "foo", Value: "bar"},
				},
			}).WithContainer(corev1.Container{Name: "dummy"}).WithPullSecret("pull-secret").WithRuntimeClassName("nvidia").WithHostPID(false),
		},
		{
			description: "transform dcgm exporter with hostNetwork enabled",
			ds: NewDaemonset().
				WithContainer(corev1.Container{Name: "dcgm-exporter"}).
				WithContainer(corev1.Container{Name: "dummy"}),
			cpSpec: &gpuv1.ClusterPolicySpec{
				DCGMExporter: gpuv1.DCGMExporterSpec{
					Repository:       "nvcr.io/nvidia/cloud-native",
					Image:            "dcgm-exporter",
					Version:          "v1.0.0",
					ImagePullPolicy:  "IfNotPresent",
					ImagePullSecrets: []string{"pull-secret"},
					Args:             []string{"--fail-on-init-error=false"},
					HostNetwork:      newBoolPtr(true),
					Env: []gpuv1.EnvVar{
						{Name: "foo", Value: "bar"},
						{Name: "DCGM_REMOTE_HOSTENGINE_INFO", Value: "nvidia-dcgm:5555"},
					},
				},
			},
			expectedDs: NewDaemonset().WithContainer(corev1.Container{
				Name:            "dcgm-exporter",
				Image:           "nvcr.io/nvidia/cloud-native/dcgm-exporter:v1.0.0",
				ImagePullPolicy: corev1.PullIfNotPresent,
				Args:            []string{"--fail-on-init-error=false"},
				Env: []corev1.EnvVar{
					{Name: "DCGM_REMOTE_HOSTENGINE_INFO", Value: "nvidia-dcgm:5555"},
					{Name: "foo", Value: "bar"},
				},
			}).WithContainer(corev1.Container{Name: "dummy"}).WithPullSecret("pull-secret").WithRuntimeClassName("nvidia").WithHostNetwork(true).WithDNSPolicy(corev1.DNSClusterFirstWithHostNet),
		},
		{
			description: "transform dcgm exporter with hostNetwork disabled",
			ds: NewDaemonset().
				WithContainer(corev1.Container{Name: "dcgm-exporter"}).
				WithContainer(corev1.Container{Name: "dummy"}),
			cpSpec: &gpuv1.ClusterPolicySpec{
				DCGMExporter: gpuv1.DCGMExporterSpec{
					Repository:       "nvcr.io/nvidia/cloud-native",
					Image:            "dcgm-exporter",
					Version:          "v1.0.0",
					ImagePullPolicy:  "IfNotPresent",
					ImagePullSecrets: []string{"pull-secret"},
					Args:             []string{"--fail-on-init-error=false"},
					HostNetwork:      newBoolPtr(false),
					Env: []gpuv1.EnvVar{
						{Name: "foo", Value: "bar"},
					},
				},
			},
			expectedDs: NewDaemonset().WithContainer(corev1.Container{
				Name:            "dcgm-exporter",
				Image:           "nvcr.io/nvidia/cloud-native/dcgm-exporter:v1.0.0",
				ImagePullPolicy: corev1.PullIfNotPresent,
				Args:            []string{"--fail-on-init-error=false"},
				Env: []corev1.EnvVar{
					{Name: "DCGM_REMOTE_HOSTENGINE_INFO", Value: "nvidia-dcgm:5555"},
					{Name: "foo", Value: "bar"},
				},
			}).WithContainer(corev1.Container{Name: "dummy"}).WithPullSecret("pull-secret").WithRuntimeClassName("nvidia").WithHostNetwork(false),
		},
		{
			description: "transform dcgm exporter with hostNetwork unspecified",
			ds: NewDaemonset().
				WithContainer(corev1.Container{Name: "dcgm-exporter"}).
				WithContainer(corev1.Container{Name: "dummy"}),
			cpSpec: &gpuv1.ClusterPolicySpec{
				DCGMExporter: gpuv1.DCGMExporterSpec{
					Repository:       "nvcr.io/nvidia/cloud-native",
					Image:            "dcgm-exporter",
					Version:          "v1.0.0",
					ImagePullPolicy:  "IfNotPresent",
					ImagePullSecrets: []string{"pull-secret"},
					Args:             []string{"--fail-on-init-error=false"},
					Env: []gpuv1.EnvVar{
						{Name: "DCGM_REMOTE_HOSTENGINE_INFO", Value: "localhost:5555"},
						{Name: "foo", Value: "bar"},
					},
				},
			},
			expectedDs: NewDaemonset().WithContainer(corev1.Container{
				Name:            "dcgm-exporter",
				Image:           "nvcr.io/nvidia/cloud-native/dcgm-exporter:v1.0.0",
				ImagePullPolicy: corev1.PullIfNotPresent,
				Args:            []string{"--fail-on-init-error=false"},
				Env: []corev1.EnvVar{
					{Name: "DCGM_REMOTE_HOSTENGINE_INFO", Value: "localhost:5555"},
					{Name: "foo", Value: "bar"},
				},
			}).WithContainer(corev1.Container{Name: "dummy"}).WithPullSecret("pull-secret").WithRuntimeClassName("nvidia").WithHostNetwork(false),
		},
		{
			description: "transform dcgm exporter with dcgm running on the host itself(DGX BaseOS)",
			ds: NewDaemonset().
				WithContainer(corev1.Container{
					Name: "dcgm-exporter",
					Env:  []corev1.EnvVar{{Name: "DCGM_REMOTE_HOSTENGINE_INFO", Value: "localhost:5555"}},
				}).
				WithContainer(corev1.Container{Name: "dummy"}),
			cpSpec: &gpuv1.ClusterPolicySpec{
				DCGM: gpuv1.DCGMSpec{
					Enabled: newBoolPtr(false),
				},
				DCGMExporter: gpuv1.DCGMExporterSpec{
					Repository:       "nvcr.io/nvidia/cloud-native",
					Image:            "dcgm-exporter",
					Version:          "v1.0.0",
					ImagePullPolicy:  "IfNotPresent",
					ImagePullSecrets: []string{"pull-secret"},
					Args:             []string{"--fail-on-init-error=false"},
					Env: []gpuv1.EnvVar{
						{Name: "DCGM_REMOTE_HOSTENGINE_INFO", Value: "localhost:5555"},
						{Name: "foo", Value: "bar"},
					},
				},
			},
			expectedDs: NewDaemonset().WithContainer(corev1.Container{
				Name:            "dcgm-exporter",
				Image:           "nvcr.io/nvidia/cloud-native/dcgm-exporter:v1.0.0",
				ImagePullPolicy: corev1.PullIfNotPresent,
				Args:            []string{"--fail-on-init-error=false"},
				Env: []corev1.EnvVar{
					{Name: "DCGM_REMOTE_HOSTENGINE_INFO", Value: "localhost:5555"},
					{Name: "foo", Value: "bar"},
				},
			}).WithContainer(corev1.Container{Name: "dummy"}).WithPullSecret("pull-secret").WithRuntimeClassName("nvidia").WithHostNetwork(true).WithDNSPolicy(corev1.DNSClusterFirstWithHostNet),
		},
		{
			description:      "transform dcgm exporter, openshift",
			openshiftVersion: "1.0.0",
			ds: NewDaemonset().
				WithContainer(corev1.Container{Name: "dcgm-exporter"}).
				WithContainer(corev1.Container{Name: "dummy"}),
			cpSpec: &gpuv1.ClusterPolicySpec{
				Operator: gpuv1.OperatorSpec{
					InitContainer: gpuv1.InitContainerSpec{
						Repository: "nvcr.io/nvidia",
						Image:      "init-container",
						Version:    "devel",
					},
				},
				DCGMExporter: gpuv1.DCGMExporterSpec{
					Repository:       "nvcr.io/nvidia/cloud-native",
					Image:            "dcgm-exporter",
					Version:          "v1.0.0",
					ImagePullPolicy:  "IfNotPresent",
					ImagePullSecrets: []string{"pull-secret"},
					Args:             []string{"--fail-on-init-error=false"},
					Env: []gpuv1.EnvVar{
						{Name: "foo", Value: "bar"},
					},
				},
				DCGM: gpuv1.DCGMSpec{
					Enabled: newBoolPtr(true),
				},
			},
			expectedDs: NewDaemonset().WithInitContainer(corev1.Container{
				Name:            "init-pod-nvidia-node-status-exporter",
				Command:         []string{"/bin/entrypoint.sh"},
				Image:           "nvcr.io/nvidia/init-container:devel",
				ImagePullPolicy: corev1.PullIfNotPresent,
				SecurityContext: &corev1.SecurityContext{
					Privileged: newBoolPtr(true),
				},
				Env: []corev1.EnvVar{{Name: NvidiaDisableRequireEnvName, Value: "true"}},
				VolumeMounts: []corev1.VolumeMount{
					{Name: "pod-gpu-resources", MountPath: "/var/lib/kubelet/pod-resources"},
					{Name: "init-config", ReadOnly: true, MountPath: "/bin/entrypoint.sh", SubPath: "entrypoint.sh"},
				},
			}).WithContainer(corev1.Container{
				Name:            "dcgm-exporter",
				Image:           "nvcr.io/nvidia/cloud-native/dcgm-exporter:v1.0.0",
				ImagePullPolicy: corev1.PullIfNotPresent,
				Args:            []string{"--fail-on-init-error=false"},
				Env: []corev1.EnvVar{
					{Name: "DCGM_REMOTE_HOSTENGINE_INFO", Value: "nvidia-dcgm:5555"},
					{Name: "foo", Value: "bar"},
				},
			}).WithContainer(corev1.Container{Name: "dummy"}).WithPullSecret("pull-secret").WithRuntimeClassName("nvidia").
				WithConfigMapVolume("init-config", "nvidia-dcgm-exporter", int32(0700)),
		},
		{
			description: "transform dcgm exporter with HPC job mapping enabled",
			ds: NewDaemonset().
				WithContainer(corev1.Container{Name: "dcgm-exporter"}).
				WithContainer(corev1.Container{Name: "dummy"}),
			cpSpec: &gpuv1.ClusterPolicySpec{
				DCGMExporter: gpuv1.DCGMExporterSpec{
					Repository:       "nvcr.io/nvidia/cloud-native",
					Image:            "dcgm-exporter",
					Version:          "v1.0.0",
					ImagePullPolicy:  "IfNotPresent",
					ImagePullSecrets: []string{"pull-secret"},
					Args:             []string{"--fail-on-init-error=false"},
					Env: []gpuv1.EnvVar{
						{Name: "foo", Value: "bar"},
					},
					HPCJobMapping: &gpuv1.DCGMExporterHPCJobMappingConfig{
						Enabled:   newBoolPtr(true),
						Directory: "/run/nvidia/dcgm-job-mapping",
					},
				},
				DCGM: gpuv1.DCGMSpec{
					Enabled: newBoolPtr(true),
				},
			},
			expectedDs: NewDaemonset().WithContainer(corev1.Container{
				Name:            "dcgm-exporter",
				Image:           "nvcr.io/nvidia/cloud-native/dcgm-exporter:v1.0.0",
				ImagePullPolicy: corev1.PullIfNotPresent,
				Args:            []string{"--fail-on-init-error=false"},
				Env: []corev1.EnvVar{
					{Name: "DCGM_REMOTE_HOSTENGINE_INFO", Value: "nvidia-dcgm:5555"},
					{Name: "DCGM_HPC_JOB_MAPPING_DIR", Value: "/run/nvidia/dcgm-job-mapping"},
					{Name: "foo", Value: "bar"},
				},
				VolumeMounts: []corev1.VolumeMount{
					{Name: "hpc-job-mapping", ReadOnly: true, MountPath: "/run/nvidia/dcgm-job-mapping"},
				},
			}).WithContainer(corev1.Container{Name: "dummy"}).
				WithPullSecret("pull-secret").
				WithRuntimeClassName("nvidia").
				WithHostPathVolume("hpc-job-mapping", "/run/nvidia/dcgm-job-mapping", ptr.To(corev1.HostPathDirectoryOrCreate)),
		},
	}

	for _, tc := range testCases {
		t.Run(tc.description, func(t *testing.T) {
			err := TransformDCGMExporter(tc.ds.DaemonSet, tc.cpSpec, ClusterPolicyController{runtime: gpuv1.Containerd, logger: ctrl.Log.WithName("test"), openshift: tc.openshiftVersion})
			require.NoError(t, err)
			require.EqualValues(t, tc.expectedDs.DaemonSet, tc.ds.DaemonSet)
		})
	}
}

func TestTransformDCGM(t *testing.T) {
	limits := corev1.ResourceList{
		corev1.ResourceCPU:    resource.MustParse("100m"),
		corev1.ResourceMemory: resource.MustParse("128Mi"),
	}
	requests := corev1.ResourceList{
		corev1.ResourceCPU:    resource.MustParse("50m"),
		corev1.ResourceMemory: resource.MustParse("64Mi"),
	}

	testCases := []struct {
		description       string
		daemonset         Daemonset
		clusterPolicySpec *gpuv1.ClusterPolicySpec
		expectedDaemonset Daemonset
	}{
		{
			description: "transform dcgm fully configured",
			daemonset: NewDaemonset().
				WithContainer(corev1.Container{Name: "dcgm"}).
				WithContainer(corev1.Container{Name: "sidecar"}),
			clusterPolicySpec: &gpuv1.ClusterPolicySpec{
				DCGM: gpuv1.DCGMSpec{
					Repository:       "nvcr.io/nvidia/cloud-native",
					Image:            "dcgm",
					Version:          "v1.0.0",
					ImagePullPolicy:  "IfNotPresent",
					ImagePullSecrets: []string{"pull-secret"},
					Resources:        &gpuv1.ResourceRequirements{Limits: limits, Requests: requests},
					Args:             []string{"--foo"},
					Env:              []gpuv1.EnvVar{{Name: "FOO", Value: "bar"}},
				},
			},
			expectedDaemonset: NewDaemonset().
				WithContainer(corev1.Container{
					Name:            "dcgm",
					Image:           "nvcr.io/nvidia/cloud-native/dcgm:v1.0.0",
					ImagePullPolicy: corev1.PullIfNotPresent,
					Args:            []string{"--foo"},
					Env:             []corev1.EnvVar{{Name: "FOO", Value: "bar"}},
					Resources:       corev1.ResourceRequirements{Limits: limits, Requests: requests},
				}).
				WithContainer(corev1.Container{
					Name:      "sidecar",
					Resources: corev1.ResourceRequirements{Limits: limits, Requests: requests},
				}).
				WithPullSecret("pull-secret").
				WithRuntimeClassName("nvidia"),
		},
		{
			description: "transform dcgm sets runtime class only when spec empty",
			daemonset:   NewDaemonset().WithContainer(corev1.Container{Name: "dcgm"}),
			clusterPolicySpec: &gpuv1.ClusterPolicySpec{
				Operator: gpuv1.OperatorSpec{RuntimeClass: "nvidia"},
				DCGM:     gpuv1.DCGMSpec{Repository: "nvcr.io/nvidia/cloud-native", Image: "dcgm", Version: "v1.0.0"},
			},
			expectedDaemonset: NewDaemonset().
				WithContainer(corev1.Container{
					Name:            "dcgm",
					Image:           "nvcr.io/nvidia/cloud-native/dcgm:v1.0.0",
					ImagePullPolicy: corev1.PullIfNotPresent,
				}).
				WithRuntimeClassName("nvidia"),
		},
		{
			description: "dcgm enabled does not set remote engine env",
			daemonset:   NewDaemonset().WithContainer(corev1.Container{Name: "dcgm"}),
			clusterPolicySpec: &gpuv1.ClusterPolicySpec{
				DCGM: gpuv1.DCGMSpec{Enabled: ptr.To(true), Repository: "nvcr.io/nvidia/cloud-native", Image: "dcgm", Version: "v1.0.0"},
			},
			expectedDaemonset: NewDaemonset().WithContainer(corev1.Container{
				Name:            "dcgm",
				Image:           "nvcr.io/nvidia/cloud-native/dcgm:v1.0.0",
				ImagePullPolicy: corev1.PullIfNotPresent,
			}).WithRuntimeClassName("nvidia"),
		},
		{
			description: "dcgm disabled with localhost env does not change hostNetwork",
			daemonset: NewDaemonset().WithContainer(corev1.Container{
				Name: "dcgm",
				Env:  []corev1.EnvVar{{Name: "DCGM_REMOTE_HOSTENGINE_INFO", Value: "localhost:5555"}},
			}),
			clusterPolicySpec: &gpuv1.ClusterPolicySpec{
				DCGM: gpuv1.DCGMSpec{Enabled: ptr.To(false), Repository: "nvcr.io/nvidia/cloud-native", Image: "dcgm", Version: "v1.0.0"},
			},
			expectedDaemonset: NewDaemonset().
				WithContainer(corev1.Container{
					Name:            "dcgm",
					Image:           "nvcr.io/nvidia/cloud-native/dcgm:v1.0.0",
					ImagePullPolicy: corev1.PullIfNotPresent,
					Env:             []corev1.EnvVar{{Name: "DCGM_REMOTE_HOSTENGINE_INFO", Value: "localhost:5555"}},
				}).
				WithRuntimeClassName("nvidia"),
		},
	}

	for _, tc := range testCases {
		t.Run(tc.description, func(t *testing.T) {
			err := TransformDCGM(tc.daemonset.DaemonSet, tc.clusterPolicySpec, ClusterPolicyController{runtime: gpuv1.Containerd, logger: ctrl.Log.WithName("test")})
			require.NoError(t, err)
			require.EqualValues(t, tc.expectedDaemonset.DaemonSet, tc.daemonset.DaemonSet)
		})
	}
}

func TestTransformMigManager(t *testing.T) {
	testCases := []struct {
		description string
		ds          Daemonset
		cpSpec      *gpuv1.ClusterPolicySpec
		expectedDs  Daemonset
	}{
		{
			description: "transform mig manager",
			ds:          NewDaemonset().WithContainer(corev1.Container{Name: "mig-manager"}),
			cpSpec: &gpuv1.ClusterPolicySpec{
				MIGManager: gpuv1.MIGManagerSpec{
					Repository:       "nvcr.io/nvidia/cloud-native",
					Image:            "mig-manager",
					Version:          "v1.0.0",
					ImagePullPolicy:  "IfNotPresent",
					ImagePullSecrets: []string{"pull-secret"},
					Args:             []string{"--test-flag"},
					Env: []gpuv1.EnvVar{
						{Name: "foo", Value: "bar"},
					},
				},
				Toolkit: gpuv1.ToolkitSpec{
					Enabled:    newBoolPtr(true),
					InstallDir: "/path/to/install",
				},
			},
			expectedDs: NewDaemonset().WithContainer(corev1.Container{
				Name:            "mig-manager",
				Image:           "nvcr.io/nvidia/cloud-native/mig-manager:v1.0.0",
				ImagePullPolicy: corev1.PullIfNotPresent,
				Args:            []string{"--test-flag"},
				Env: []corev1.EnvVar{
					{Name: CDIEnabledEnvName, Value: "true"},
					{Name: NvidiaCDIHookPathEnvName, Value: "/path/to/install/toolkit/nvidia-cdi-hook"},
					{Name: "foo", Value: "bar"},
				},
			}).WithPullSecret("pull-secret").WithRuntimeClassName("nvidia"),
		},
	}

	for _, tc := range testCases {
		t.Run(tc.description, func(t *testing.T) {
			err := TransformMIGManager(tc.ds.DaemonSet, tc.cpSpec, ClusterPolicyController{
				runtime: gpuv1.Containerd,
				logger:  ctrl.Log.WithName("test"),
			})
			require.NoError(t, err)
			require.EqualValues(t, tc.expectedDs, tc.ds)
		})
	}
}

func TestTransformKataManager(t *testing.T) {
	testCases := []struct {
		description string
		ds          Daemonset
		cpSpec      *gpuv1.ClusterPolicySpec
		expectedDs  Daemonset
	}{
		{
			description: "transform kata manager",
			ds:          NewDaemonset().WithContainer(corev1.Container{Name: "nvidia-kata-manager"}),
			cpSpec: &gpuv1.ClusterPolicySpec{
				KataManager: gpuv1.KataManagerSpec{
					Repository:       "nvcr.io/nvidia/cloud-native",
					Image:            "kata-manager",
					Version:          "v1.0.0",
					ImagePullPolicy:  "IfNotPresent",
					ImagePullSecrets: []string{"pull-secret"},
					Args:             []string{"--test-flag"},
					Config: &kata_v1alpha1.Config{
						ArtifactsDir: "/var/lib/kata",
					},
					Env: []gpuv1.EnvVar{
						{Name: "foo", Value: "bar"},
					},
				},
			},
			expectedDs: NewDaemonset().WithContainer(corev1.Container{
				Name:            "nvidia-kata-manager",
				Image:           "nvcr.io/nvidia/cloud-native/kata-manager:v1.0.0",
				ImagePullPolicy: corev1.PullIfNotPresent,
				Args:            []string{"--test-flag"},
				Env: []corev1.EnvVar{
					{Name: "KATA_ARTIFACTS_DIR", Value: "/var/lib/kata"},
					{Name: "foo", Value: "bar"},
					{Name: "RUNTIME", Value: "containerd"},
					{Name: "CONTAINERD_RUNTIME_CLASS", Value: "nvidia"},
					{Name: "RUNTIME_CONFIG", Value: "/runtime/config-dir/config.toml"},
					{Name: "CONTAINERD_CONFIG", Value: "/runtime/config-dir/config.toml"},
					{Name: "RUNTIME_SOCKET", Value: "/runtime/sock-dir/containerd.sock"},
					{Name: "CONTAINERD_SOCKET", Value: "/runtime/sock-dir/containerd.sock"},
				},
				VolumeMounts: []corev1.VolumeMount{
					{Name: "kata-artifacts", MountPath: "/var/lib/kata"},
					{Name: "containerd-config", MountPath: "/runtime/config-dir/"},
					{Name: "containerd-socket", MountPath: "/runtime/sock-dir/"},
				},
			}).WithPullSecret("pull-secret").WithPodAnnotations(map[string]string{"nvidia.com/kata-manager.last-applied-hash": "1929911998"}).WithHostPathVolume("kata-artifacts", "/var/lib/kata", ptr.To(corev1.HostPathDirectoryOrCreate)).WithHostPathVolume("containerd-config", "/etc/containerd", ptr.To(corev1.HostPathDirectoryOrCreate)).WithHostPathVolume("containerd-socket", "/run/containerd", nil),
		},
		{
			description: "transform kata manager with custom container runtime socket",
			ds:          NewDaemonset().WithContainer(corev1.Container{Name: "nvidia-kata-manager"}),
			cpSpec: &gpuv1.ClusterPolicySpec{
				KataManager: gpuv1.KataManagerSpec{
					Repository:       "nvcr.io/nvidia/cloud-native",
					Image:            "kata-manager",
					Version:          "v1.0.0",
					ImagePullPolicy:  "IfNotPresent",
					ImagePullSecrets: []string{"pull-secret"},
					Args:             []string{"--test-flag"},
					Config: &kata_v1alpha1.Config{
						ArtifactsDir: "/var/lib/kata",
					},
					Env: []gpuv1.EnvVar{
						{
							Name: "CONTAINERD_CONFIG", Value: "/var/lib/rancher/k3s/agent/etc/containerd/config.toml",
						},
						{
							Name: "CONTAINERD_SOCKET", Value: "/run/k3s/containerd/containerd.sock",
						},
						{
							Name: "CONTAINERD_RUNTIME_CLASS", Value: "nvidia",
						},
						{
							Name: "CONTAINERD_SET_AS_DEFAULT", Value: "true",
						},
					},
				},
			},
			expectedDs: NewDaemonset().WithContainer(corev1.Container{
				Name:            "nvidia-kata-manager",
				Image:           "nvcr.io/nvidia/cloud-native/kata-manager:v1.0.0",
				ImagePullPolicy: corev1.PullIfNotPresent,
				Args:            []string{"--test-flag"},
				Env: []corev1.EnvVar{
					{Name: "KATA_ARTIFACTS_DIR", Value: "/var/lib/kata"},
					{Name: "CONTAINERD_CONFIG", Value: "/runtime/config-dir/config.toml"},
					{Name: "CONTAINERD_SOCKET", Value: "/runtime/sock-dir/containerd.sock"},
					{Name: "CONTAINERD_RUNTIME_CLASS", Value: "nvidia"},
					{Name: "CONTAINERD_SET_AS_DEFAULT", Value: "true"},
					{Name: "RUNTIME", Value: "containerd"},
					{Name: "RUNTIME_CONFIG", Value: "/runtime/config-dir/config.toml"},
					{Name: "RUNTIME_SOCKET", Value: "/runtime/sock-dir/containerd.sock"},
				},
				VolumeMounts: []corev1.VolumeMount{
					{Name: "kata-artifacts", MountPath: "/var/lib/kata"},
					{Name: "containerd-config", MountPath: "/runtime/config-dir/"},
					{Name: "containerd-socket", MountPath: "/runtime/sock-dir/"},
				},
			}).WithPullSecret("pull-secret").
				WithPodAnnotations(map[string]string{"nvidia.com/kata-manager.last-applied-hash": "1929911998"}).
				WithHostPathVolume("kata-artifacts", "/var/lib/kata", ptr.To(corev1.HostPathDirectoryOrCreate)).
				WithHostPathVolume("containerd-config", "/var/lib/rancher/k3s/agent/etc/containerd", ptr.To(corev1.HostPathDirectoryOrCreate)).
				WithHostPathVolume("containerd-socket", "/run/k3s/containerd", nil),
		},
	}

	for _, tc := range testCases {
		t.Run(tc.description, func(t *testing.T) {
			err := TransformKataManager(tc.ds.DaemonSet, tc.cpSpec, ClusterPolicyController{
				runtime: gpuv1.Containerd,
				logger:  ctrl.Log.WithName("test"),
			})
			require.NoError(t, err)
			require.EqualValues(t, tc.expectedDs, tc.ds)
		})
	}
}

func TestTransformVFIOManager(t *testing.T) {
	resources := corev1.ResourceRequirements{
		Limits: corev1.ResourceList{
			corev1.ResourceCPU:    resource.MustParse("100m"),
			corev1.ResourceMemory: resource.MustParse("128Mi"),
		},
		Requests: corev1.ResourceList{
			corev1.ResourceCPU:    resource.MustParse("50m"),
			corev1.ResourceMemory: resource.MustParse("64Mi"),
		},
	}
	secret := "pull-secret"
	mockEnv := []gpuv1.EnvVar{{Name: "foo", Value: "bar"}}
	mockEnvCore := []corev1.EnvVar{{Name: "foo", Value: "bar"}}

	testCases := []struct {
		description       string
		daemonset         Daemonset
		clusterPolicySpec *gpuv1.ClusterPolicySpec
		expectedDaemonset Daemonset
	}{
		{
			description: "transform vfio manager",
			daemonset: NewDaemonset().
				WithContainer(corev1.Container{Name: "nvidia-vfio-manager"}).
				WithContainer(corev1.Container{Name: "sidecar"}).
				WithInitContainer(corev1.Container{Name: "k8s-driver-manager"}),
			clusterPolicySpec: &gpuv1.ClusterPolicySpec{
				VFIOManager: gpuv1.VFIOManagerSpec{
					Repository:       "nvcr.io/nvidia/cloud-native",
					Image:            "vfio-pci-manager",
					Version:          "v1.0.0",
					ImagePullPolicy:  "IfNotPresent",
					ImagePullSecrets: []string{secret},
					Resources:        &gpuv1.ResourceRequirements{Limits: resources.Limits, Requests: resources.Requests},
					Args:             []string{"--test-flag"},
					Env:              mockEnv,
					DriverManager: gpuv1.DriverManagerSpec{
						Repository:      "nvcr.io/nvidia/cloud-native",
						Image:           "k8s-driver-manager",
						Version:         "v1.0.0",
						ImagePullPolicy: "IfNotPresent",
						Env:             mockEnv,
					},
				},
			},
			expectedDaemonset: NewDaemonset().
				WithContainer(corev1.Container{
					Name:            "nvidia-vfio-manager",
					Image:           "nvcr.io/nvidia/cloud-native/vfio-pci-manager:v1.0.0",
					ImagePullPolicy: corev1.PullIfNotPresent,
					Args:            []string{"--test-flag"},
					Env:             mockEnvCore,
					Resources:       resources,
				}).
				WithContainer(corev1.Container{
					Name:      "sidecar",
					Resources: resources,
				}).
				WithInitContainer(corev1.Container{
					Name:            "k8s-driver-manager",
					Image:           "nvcr.io/nvidia/cloud-native/k8s-driver-manager:v1.0.0",
					ImagePullPolicy: corev1.PullIfNotPresent,
					Env:             mockEnvCore,
				}).
				WithPullSecret(secret),
		},
	}

	for _, tc := range testCases {
		t.Run(tc.description, func(t *testing.T) {
			err := TransformVFIOManager(tc.daemonset.DaemonSet, tc.clusterPolicySpec, ClusterPolicyController{logger: ctrl.Log.WithName("test")})
			require.NoError(t, err)
			require.EqualValues(t, tc.expectedDaemonset, tc.daemonset)
		})
	}
}

func TestTransformCCManager(t *testing.T) {
	resources := corev1.ResourceRequirements{
		Limits: corev1.ResourceList{
			corev1.ResourceCPU:    resource.MustParse("100m"),
			corev1.ResourceMemory: resource.MustParse("128Mi"),
		},
		Requests: corev1.ResourceList{
			corev1.ResourceCPU:    resource.MustParse("50m"),
			corev1.ResourceMemory: resource.MustParse("64Mi"),
		},
	}
	secret := "pull-secret"
	mockEnv := []gpuv1.EnvVar{{Name: "foo", Value: "bar"}}
	defaultMode := "devtools"

	testCases := []struct {
		description       string
		daemonset         Daemonset
		clusterPolicySpec *gpuv1.ClusterPolicySpec
		expectedDaemonset Daemonset
	}{
		{
			description: "transform cc manager",
			daemonset: NewDaemonset().
				WithContainer(corev1.Container{Name: "nvidia-cc-manager"}).
				WithContainer(corev1.Container{Name: "sidecar"}),
			clusterPolicySpec: &gpuv1.ClusterPolicySpec{
				CCManager: gpuv1.CCManagerSpec{
					Repository:       "nvcr.io/nvidia/cloud-native",
					Image:            "k8s-cc-manager",
					Version:          "v1.0.0",
					ImagePullPolicy:  "IfNotPresent",
					ImagePullSecrets: []string{secret},
					Resources:        &gpuv1.ResourceRequirements{Limits: resources.Limits, Requests: resources.Requests},
					Args:             []string{"--test-flag"},
					DefaultMode:      defaultMode,
					Env:              mockEnv,
				},
			},
			expectedDaemonset: NewDaemonset().
				WithContainer(corev1.Container{
					Name:            "nvidia-cc-manager",
					Image:           "nvcr.io/nvidia/cloud-native/k8s-cc-manager:v1.0.0",
					ImagePullPolicy: corev1.PullIfNotPresent,
					Args:            []string{"--test-flag"},
					Env: []corev1.EnvVar{
						{Name: "DEFAULT_CC_MODE", Value: defaultMode},
						{Name: "foo", Value: "bar"},
					},
					Resources: resources,
				}).
				WithContainer(corev1.Container{
					Name:      "sidecar",
					Resources: resources,
				}).
				WithPullSecret(secret),
		},
	}

	for _, tc := range testCases {
		t.Run(tc.description, func(t *testing.T) {
			err := TransformCCManager(tc.daemonset.DaemonSet, tc.clusterPolicySpec, ClusterPolicyController{logger: ctrl.Log.WithName("test")})
			require.NoError(t, err)
			require.EqualValues(t, tc.expectedDaemonset, tc.daemonset)
		})
	}
}

func TestTransformVGPUDeviceManager(t *testing.T) {
	resources := corev1.ResourceRequirements{
		Limits: corev1.ResourceList{
			corev1.ResourceCPU:    resource.MustParse("100m"),
			corev1.ResourceMemory: resource.MustParse("128Mi"),
		},
		Requests: corev1.ResourceList{
			corev1.ResourceCPU:    resource.MustParse("50m"),
			corev1.ResourceMemory: resource.MustParse("64Mi"),
		},
	}
	secret := "pull-secret"
	mockEnv := []gpuv1.EnvVar{{Name: "foo", Value: "bar"}}

	testCases := []struct {
		description       string
		daemonset         Daemonset
		clusterPolicySpec *gpuv1.ClusterPolicySpec
		expectedDaemonset Daemonset
	}{
		{
			description: "transform vgpu device manager",
			daemonset: NewDaemonset().
				WithContainer(corev1.Container{Name: "nvidia-vgpu-device-manager"}).
				WithContainer(corev1.Container{Name: "sidecar"}),
			clusterPolicySpec: &gpuv1.ClusterPolicySpec{
				VGPUDeviceManager: gpuv1.VGPUDeviceManagerSpec{
					Repository:       "nvcr.io/nvidia/cloud-native",
					Image:            "vgpu-device-manager",
					Version:          "v1.0.0",
					ImagePullPolicy:  "IfNotPresent",
					ImagePullSecrets: []string{secret},
					Resources:        &gpuv1.ResourceRequirements{Limits: resources.Limits, Requests: resources.Requests},
					Args:             []string{"--test-flag"},
					Env:              mockEnv,
					Config: &gpuv1.VGPUDevicesConfigSpec{
						Name:    "custom-vgpu-config",
						Default: "perf",
					},
				},
			},
			expectedDaemonset: NewDaemonset().
				WithContainer(corev1.Container{
					Name:            "nvidia-vgpu-device-manager",
					Image:           "nvcr.io/nvidia/cloud-native/vgpu-device-manager:v1.0.0",
					ImagePullPolicy: corev1.PullIfNotPresent,
					Args:            []string{"--test-flag"},
					Env: []corev1.EnvVar{
						{Name: "foo", Value: "bar"},
						{Name: "DEFAULT_VGPU_CONFIG", Value: "perf"},
					},
					Resources: resources,
				}).
				WithContainer(corev1.Container{
					Name:      "sidecar",
					Resources: resources,
				}).
				WithPullSecret(secret),
		},
	}

	for _, tc := range testCases {
		t.Run(tc.description, func(t *testing.T) {
			err := TransformVGPUDeviceManager(tc.daemonset.DaemonSet, tc.clusterPolicySpec, ClusterPolicyController{logger: ctrl.Log.WithName("test")})
			require.NoError(t, err)
			require.EqualValues(t, tc.expectedDaemonset, tc.daemonset)
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

// removeDigestFromDaemonSet removes DRIVER_CONFIG_DIGEST env var from a DaemonSet
func removeDigestFromDaemonSet(ds *appsv1.DaemonSet) {
	removeDigestFromContainers := func(containers []corev1.Container) {
		for i := range containers {
			var filtered []corev1.EnvVar
			for _, env := range containers[i].Env {
				if env.Name != "DRIVER_CONFIG_DIGEST" {
					filtered = append(filtered, env)
				}
			}
			containers[i].Env = filtered
		}
	}
	removeDigestFromContainers(ds.Spec.Template.Spec.Containers)
	removeDigestFromContainers(ds.Spec.Template.Spec.InitContainers)
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
					{Name: ValidatorImageEnvName, Value: "nvcr.io/nvidia/cloud-native/gpu-operator-validator:v1.0.0"},
					{Name: ValidatorImagePullPolicyEnvName, Value: "IfNotPresent"},
					{Name: ValidatorImagePullSecretsEnvName, Value: "pull-secret1,pull-secret2"},
					{Name: ValidatorRuntimeClassEnvName, Value: "nvidia"},
					{Name: "foo", Value: "bar"},
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
					{Name: ValidatorImageEnvName, Value: "nvcr.io/nvidia/cloud-native/gpu-operator-validator:v1.0.0"},
					{Name: ValidatorImagePullPolicyEnvName, Value: "IfNotPresent"},
					{Name: ValidatorImagePullSecretsEnvName, Value: "pull-secret1,pull-secret2"},
					{Name: ValidatorRuntimeClassEnvName, Value: "nvidia"},
					{Name: MigStrategyEnvName, Value: string(gpuv1.MIGStrategySingle)},
					{Name: "foo", Value: "bar"},
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
		{
			description: "nri plugin enabled",
			ds: NewDaemonset().
				WithInitContainer(corev1.Container{Name: "toolkit-validation"}).
				WithContainer(corev1.Container{
					Name:            "dummy",
					Image:           "nvcr.io/nvidia/cloud-native/gpu-operator-validator:v1.0.0",
					ImagePullPolicy: corev1.PullIfNotPresent,
					SecurityContext: &corev1.SecurityContext{
						RunAsUser: rootUID,
					},
				}).
				WithPullSecret("pull-secret"),
			cpSpec: &gpuv1.ClusterPolicySpec{
				Validator: gpuv1.ValidatorSpec{
					Repository:       "nvcr.io/nvidia/cloud-native",
					Image:            "gpu-operator-validator",
					Version:          "v1.0.0",
					ImagePullPolicy:  "IfNotPresent",
					ImagePullSecrets: []string{"pull-secret"},
				},
				CDI: gpuv1.CDIConfigSpec{
					Enabled:          newBoolPtr(true),
					NRIPluginEnabled: newBoolPtr(true),
				},
			},
			expectedDs: NewDaemonset().
				WithPodAnnotations(map[string]string{
					"nvidia.cdi.k8s.io/container.toolkit-validation": "management.nvidia.com/gpu=all",
				}).
				WithInitContainer(corev1.Container{
					Name:            "toolkit-validation",
					Image:           "nvcr.io/nvidia/cloud-native/gpu-operator-validator:v1.0.0",
					ImagePullPolicy: corev1.PullIfNotPresent,
					SecurityContext: &corev1.SecurityContext{
						RunAsUser: rootUID,
					},
				},
				).
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

func TestTransformDriver(t *testing.T) {
	initMockK8sClients()
	testCases := []struct {
		description   string
		ds            Daemonset
		cpSpec        *gpuv1.ClusterPolicySpec
		client        client.Client
		expectedDs    Daemonset
		errorExpected bool
	}{
		{
			description: "driver spec with secret env",
			ds: NewDaemonset().WithContainer(corev1.Container{Name: "nvidia-driver-ctr"}).
				WithContainer(corev1.Container{Name: "nvidia-fs"}).
				WithContainer(corev1.Container{Name: "nvidia-gdrcopy"}).
				WithInitContainer(corev1.Container{Name: "k8s-driver-manager"}),
			cpSpec: &gpuv1.ClusterPolicySpec{
				Driver: gpuv1.DriverSpec{
					Repository: "nvcr.io/nvidia",
					Image:      "driver",
					Version:    "570.172.08",
					Manager: gpuv1.DriverManagerSpec{
						Repository: "nvcr.io/nvidia/cloud-native",
						Image:      "k8s-driver-manager",
						Version:    "v0.8.0",
					},
					SecretEnv: "test-env-secret",
				},
				GPUDirectStorage: &gpuv1.GPUDirectStorageSpec{
					Enabled:    newBoolPtr(true),
					Repository: "nvcr.io/nvidia/cloud-native",
					Image:      "nvidia-fs",
					Version:    "2.20.5",
				},
				GDRCopy: &gpuv1.GDRCopySpec{
					Enabled:    newBoolPtr(true),
					Repository: "nvcr.io/nvidia/cloud-native",
					Image:      "gdrdrv",
					Version:    "v2.5",
				},
			},
			client: mockClientMap["secret-env-client"],
			expectedDs: NewDaemonset().WithContainer(corev1.Container{
				Name:            "nvidia-driver-ctr",
				Image:           "nvcr.io/nvidia/driver:570.172.08-",
				ImagePullPolicy: corev1.PullIfNotPresent,
				EnvFrom: []corev1.EnvFromSource{{
					SecretRef: &corev1.SecretEnvSource{
						LocalObjectReference: corev1.LocalObjectReference{
							Name: "test-env-secret",
						},
					},
				}},
				Env: []corev1.EnvVar{
					{
						Name:  "GDRCOPY_ENABLED",
						Value: "true",
					},
					{
						Name:  "GDS_ENABLED",
						Value: "true",
					},
				},
			}).WithContainer(corev1.Container{
				Name:  "nvidia-fs",
				Image: "nvcr.io/nvidia/cloud-native/nvidia-fs:2.20.5-",
				EnvFrom: []corev1.EnvFromSource{{
					SecretRef: &corev1.SecretEnvSource{
						LocalObjectReference: corev1.LocalObjectReference{
							Name: "test-env-secret",
						},
					},
				}},
			}).WithContainer(corev1.Container{
				Name:  "nvidia-gdrcopy",
				Image: "nvcr.io/nvidia/cloud-native/gdrdrv:v2.5-",
				EnvFrom: []corev1.EnvFromSource{{
					SecretRef: &corev1.SecretEnvSource{
						LocalObjectReference: corev1.LocalObjectReference{
							Name: "test-env-secret",
						},
					},
				}},
			}).WithInitContainer(corev1.Container{
				Name:  "k8s-driver-manager",
				Image: "nvcr.io/nvidia/cloud-native/k8s-driver-manager:v0.8.0",
			}),
			errorExpected: false,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.description, func(t *testing.T) {
			err := TransformDriver(tc.ds.DaemonSet, tc.cpSpec,
				ClusterPolicyController{client: tc.client, runtime: gpuv1.Containerd,
					operatorNamespace: "test-ns", logger: ctrl.Log.WithName("test")})
			if tc.errorExpected {
				require.Error(t, err)
				return
			}
			require.NoError(t, err)

			// Remove dynamically generated digest before comparison
			removeDigestFromDaemonSet(tc.ds.DaemonSet)
			require.EqualValues(t, tc.expectedDs, tc.ds)
		})
	}
}

func TestTransformToolkitCtrForCDI(t *testing.T) {
	testCases := []struct {
		description string
		ds          Daemonset
		cpSpec      *gpuv1.ClusterPolicySpec
		expectedDs  Daemonset
	}{
		{
			description: "cdi enabled",
			ds:          NewDaemonset().WithContainer(corev1.Container{Name: "main-ctr"}),
			cpSpec: &gpuv1.ClusterPolicySpec{
				CDI: gpuv1.CDIConfigSpec{
					Enabled: newBoolPtr(true),
				},
			},
			expectedDs: NewDaemonset().WithContainer(
				corev1.Container{
					Name: "main-ctr",
					Env: []corev1.EnvVar{
						{Name: CDIEnabledEnvName, Value: "true"},
						{Name: NvidiaRuntimeSetAsDefaultEnvName, Value: "false"},
						{Name: NvidiaCtrRuntimeModeEnvName, Value: "cdi"},
						{Name: CRIOConfigModeEnvName, Value: "config"},
					},
				}),
		},
		{
			description: "cdi and nri plugin enabled",
			ds:          NewDaemonset().WithContainer(corev1.Container{Name: "main-ctr"}),
			cpSpec: &gpuv1.ClusterPolicySpec{
				CDI: gpuv1.CDIConfigSpec{
					Enabled:          newBoolPtr(true),
					NRIPluginEnabled: newBoolPtr(true),
				},
			},
			expectedDs: NewDaemonset().WithContainer(
				corev1.Container{
					Name: "main-ctr",
					Env: []corev1.EnvVar{
						{Name: CDIEnabledEnvName, Value: "true"},
						{Name: NvidiaRuntimeSetAsDefaultEnvName, Value: "false"},
						{Name: NvidiaCtrRuntimeModeEnvName, Value: "cdi"},
						{Name: CRIOConfigModeEnvName, Value: "config"},
						{Name: CDIEnableNRIPlugin, Value: "true"},
					},
				}),
		},
	}

	for _, tc := range testCases {
		t.Run(tc.description, func(t *testing.T) {
			mainContainer := &tc.ds.Spec.Template.Spec.Containers[0]
			transformToolkitCtrForCDI(mainContainer, tc.cpSpec.CDI.IsNRIPluginEnabled())
			require.EqualValues(t, tc.expectedDs, tc.ds)
		})
	}
}

func TestTransformDevicePluginCtrForCDI(t *testing.T) {
	testCases := []struct {
		description string
		ds          Daemonset
		cpSpec      *gpuv1.ClusterPolicySpec
		expectedDs  Daemonset
	}{
		{
			description: "toolkit disabled",
			ds:          NewDaemonset().WithContainer(corev1.Container{Name: "main-ctr"}),
			cpSpec: &gpuv1.ClusterPolicySpec{
				Toolkit: gpuv1.ToolkitSpec{
					Enabled: newBoolPtr(false),
				},
			},
			expectedDs: NewDaemonset().WithContainer(
				corev1.Container{
					Name: "main-ctr",
					Env: []corev1.EnvVar{
						{Name: CDIEnabledEnvName, Value: "true"},
						{Name: DeviceListStrategyEnvName, Value: "cdi-annotations,cdi-cri"},
						{Name: CDIAnnotationPrefixEnvName, Value: "cdi.k8s.io/"},
					},
				}),
		},
		{
			description: "toolkit enabled",
			ds:          NewDaemonset().WithContainer(corev1.Container{Name: "main-ctr"}),
			cpSpec: &gpuv1.ClusterPolicySpec{
				Toolkit: gpuv1.ToolkitSpec{
					Enabled:    newBoolPtr(true),
					InstallDir: "/path/to/install",
				},
			},
			expectedDs: NewDaemonset().WithContainer(
				corev1.Container{
					Name: "main-ctr",
					Env: []corev1.EnvVar{
						{Name: CDIEnabledEnvName, Value: "true"},
						{Name: DeviceListStrategyEnvName, Value: "cdi-annotations,cdi-cri"},
						{Name: CDIAnnotationPrefixEnvName, Value: "cdi.k8s.io/"},
						{Name: NvidiaCDIHookPathEnvName, Value: "/path/to/install/toolkit/nvidia-cdi-hook"},
					},
				}),
		},
	}

	for _, tc := range testCases {
		t.Run(tc.description, func(t *testing.T) {
			mainContainer := &tc.ds.Spec.Template.Spec.Containers[0]
			transformDevicePluginCtrForCDI(mainContainer, tc.cpSpec)
			require.EqualValues(t, tc.expectedDs, tc.ds)
		})
	}
}

func TestGetRuntimeConfigFiles(t *testing.T) {
	testCases := []struct {
		description                string
		container                  corev1.Container
		runtime                    string
		expectedTopLevelConfigFile string
		expectedDropInConfigFile   string
		errorExpected              bool
	}{
		{
			description:   "invalid runtime",
			container:     corev1.Container{},
			runtime:       "foo",
			errorExpected: true,
		},
		{
			description:                "docker",
			container:                  corev1.Container{},
			runtime:                    gpuv1.Docker.String(),
			expectedTopLevelConfigFile: DefaultDockerConfigFile,
			expectedDropInConfigFile:   "",
		},
		{
			description: "docker, config path overridden",
			container: corev1.Container{
				Env: []corev1.EnvVar{
					{Name: "RUNTIME_CONFIG", Value: "/path/to/docker/daemon.json"},
				},
			},
			runtime:                    gpuv1.Docker.String(),
			expectedTopLevelConfigFile: "/path/to/docker/daemon.json",
			expectedDropInConfigFile:   "",
		},
		{
			description: "docker, config path overridden, DOCKER_CONFIG envvar has highest precedence",
			container: corev1.Container{
				Env: []corev1.EnvVar{
					{Name: "RUNTIME_CONFIG", Value: "/path/to/docker/daemon.json"},
					{Name: "DOCKER_CONFIG", Value: "/another/path/to/docker/daemon.json"},
				},
			},
			runtime:                    gpuv1.Docker.String(),
			expectedTopLevelConfigFile: "/another/path/to/docker/daemon.json",
			expectedDropInConfigFile:   "",
		},
		{
			description:                "containerd",
			container:                  corev1.Container{},
			runtime:                    gpuv1.Containerd.String(),
			expectedTopLevelConfigFile: DefaultContainerdConfigFile,
			expectedDropInConfigFile:   DefaultContainerdDropInConfigFile,
		},
		{
			description: "containerd, config path overridden",
			container: corev1.Container{
				Env: []corev1.EnvVar{
					{Name: "RUNTIME_CONFIG", Value: "/path/to/containerd/config.toml"},
					{Name: "RUNTIME_DROP_IN_CONFIG", Value: "/path/to/containerd/drop-in/config.toml"},
				},
			},
			runtime:                    gpuv1.Containerd.String(),
			expectedTopLevelConfigFile: "/path/to/containerd/config.toml",
			expectedDropInConfigFile:   "/path/to/containerd/drop-in/config.toml",
		},
		{
			description: "containerd, config path overridden, CONTAINERD_CONFIG envvar has highest precedence",
			container: corev1.Container{
				Env: []corev1.EnvVar{
					{Name: "RUNTIME_CONFIG", Value: "/path/to/containerd/config.toml"},
					{Name: "CONTAINERD_CONFIG", Value: "/another/path/to/containerd/config.toml"},
					{Name: "RUNTIME_DROP_IN_CONFIG", Value: "/path/to/containerd/drop-in/config.toml"},
				},
			},
			runtime:                    gpuv1.Containerd.String(),
			expectedTopLevelConfigFile: "/another/path/to/containerd/config.toml",
			expectedDropInConfigFile:   "/path/to/containerd/drop-in/config.toml",
		},
		{
			description:                "crio",
			container:                  corev1.Container{},
			runtime:                    gpuv1.CRIO.String(),
			expectedTopLevelConfigFile: DefaultCRIOConfigFile,
			expectedDropInConfigFile:   DefaultCRIODropInConfigFile,
		},
		{
			description: "crio, config path overridden",
			container: corev1.Container{
				Env: []corev1.EnvVar{
					{Name: "RUNTIME_CONFIG", Value: "/path/to/crio/config.toml"},
					{Name: "RUNTIME_DROP_IN_CONFIG", Value: "/path/to/crio/drop-in/config.toml"},
				},
			},
			runtime:                    gpuv1.CRIO.String(),
			expectedTopLevelConfigFile: "/path/to/crio/config.toml",
			expectedDropInConfigFile:   "/path/to/crio/drop-in/config.toml",
		},
		{
			description: "crio, config path overridden, CRIO_CONFIG envvar has highest precedence",
			container: corev1.Container{
				Env: []corev1.EnvVar{
					{Name: "RUNTIME_CONFIG", Value: "/path/to/crio/config.toml"},
					{Name: "CRIO_CONFIG", Value: "/another/path/to/crio/config.toml"},
					{Name: "RUNTIME_DROP_IN_CONFIG", Value: "/path/to/crio/drop-in/config.toml"},
				},
			},
			runtime:                    gpuv1.CRIO.String(),
			expectedTopLevelConfigFile: "/another/path/to/crio/config.toml",
			expectedDropInConfigFile:   "/path/to/crio/drop-in/config.toml",
		},
	}

	for _, tc := range testCases {
		t.Run(tc.description, func(t *testing.T) {
			topLevelConfigFile, dropInConfigFile, err := getRuntimeConfigFiles(&tc.container, tc.runtime)
			if tc.errorExpected {
				require.Error(t, err)
				return
			}
			require.NoError(t, err)
			require.EqualValues(t, tc.expectedTopLevelConfigFile, topLevelConfigFile)
			require.EqualValues(t, tc.expectedDropInConfigFile, dropInConfigFile)
		})
	}

}

func TestTransformDriverWithLicensingConfig(t *testing.T) {
	node := &corev1.Node{
		ObjectMeta: metav1.ObjectMeta{
			Name: "test-node",
			Labels: map[string]string{
				nfdKernelLabelKey: "6.8.0-60-generic",
				commonGPULabelKey: "true",
			},
		},
	}
	mockClient := fake.NewFakeClient(node)

	testCases := []struct {
		description   string
		ds            Daemonset
		cpSpec        *gpuv1.ClusterPolicySpec
		client        client.Client
		expectedDs    Daemonset
		errorExpected bool
	}{
		{
			description: "transform driver dependent containers with secretName",
			ds: NewDaemonset().WithContainer(corev1.Container{Name: "nvidia-driver-ctr"}).
				WithInitContainer(corev1.Container{Name: "k8s-driver-manager"}),
			cpSpec: &gpuv1.ClusterPolicySpec{
				Driver: gpuv1.DriverSpec{
					Repository:      "nvcr.io/nvidia",
					Image:           "driver",
					ImagePullPolicy: "IfNotPresent",
					Version:         "570.172.08",
					Manager: gpuv1.DriverManagerSpec{
						Repository:      "nvcr.io/nvidia/cloud-native",
						Image:           "k8s-driver-manager",
						ImagePullPolicy: "IfNotPresent",
						Version:         "v0.8.0",
					},
					LicensingConfig: &gpuv1.DriverLicensingConfigSpec{
						SecretName: "test-secret",
					},
				},
			},
			client: mockClient,
			expectedDs: NewDaemonset().WithContainer(corev1.Container{
				Name:            "nvidia-driver-ctr",
				Image:           "nvcr.io/nvidia/driver:570.172.08-",
				ImagePullPolicy: corev1.PullIfNotPresent,
				VolumeMounts: []corev1.VolumeMount{
					{
						Name:      "licensing-config",
						ReadOnly:  true,
						MountPath: consts.VGPULicensingConfigMountPath,
						SubPath:   consts.VGPULicensingFileName,
					},
				},
			}).WithInitContainer(corev1.Container{
				Name:            "k8s-driver-manager",
				Image:           "nvcr.io/nvidia/cloud-native/k8s-driver-manager:v0.8.0",
				ImagePullPolicy: corev1.PullIfNotPresent,
			}).WithVolume(corev1.Volume{
				Name: "licensing-config",
				VolumeSource: corev1.VolumeSource{
					Secret: &corev1.SecretVolumeSource{
						SecretName: "test-secret",
						Items: []corev1.KeyToPath{
							{
								Key:  consts.VGPULicensingFileName,
								Path: consts.VGPULicensingFileName,
							},
						},
					},
				},
			}),
			errorExpected: false,
		},
		{
			description: "transform driver dependent containers with configMapName",
			ds: NewDaemonset().WithContainer(corev1.Container{Name: "nvidia-driver-ctr"}).
				WithInitContainer(corev1.Container{Name: "k8s-driver-manager"}),
			cpSpec: &gpuv1.ClusterPolicySpec{
				Driver: gpuv1.DriverSpec{
					Repository:      "nvcr.io/nvidia",
					Image:           "driver",
					ImagePullPolicy: "IfNotPresent",
					Version:         "570.172.08",
					Manager: gpuv1.DriverManagerSpec{
						Repository:      "nvcr.io/nvidia/cloud-native",
						Image:           "k8s-driver-manager",
						ImagePullPolicy: "IfNotPresent",
						Version:         "v0.8.0",
					},
					LicensingConfig: &gpuv1.DriverLicensingConfigSpec{
						ConfigMapName: "test-configmap",
					},
				},
			},
			client: mockClient,
			expectedDs: NewDaemonset().WithContainer(corev1.Container{
				Name:            "nvidia-driver-ctr",
				Image:           "nvcr.io/nvidia/driver:570.172.08-",
				ImagePullPolicy: corev1.PullIfNotPresent,
				VolumeMounts: []corev1.VolumeMount{
					{
						Name:      "licensing-config",
						ReadOnly:  true,
						MountPath: consts.VGPULicensingConfigMountPath,
						SubPath:   consts.VGPULicensingFileName,
					},
				},
			}).WithInitContainer(corev1.Container{
				Name:            "k8s-driver-manager",
				Image:           "nvcr.io/nvidia/cloud-native/k8s-driver-manager:v0.8.0",
				ImagePullPolicy: corev1.PullIfNotPresent,
			}).WithVolume(corev1.Volume{
				Name: "licensing-config",
				VolumeSource: corev1.VolumeSource{
					ConfigMap: &corev1.ConfigMapVolumeSource{
						LocalObjectReference: corev1.LocalObjectReference{
							Name: "test-configmap",
						},
						Items: []corev1.KeyToPath{
							{
								Key:  consts.VGPULicensingFileName,
								Path: consts.VGPULicensingFileName,
							},
						},
					},
				},
			}),
			errorExpected: false,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.description, func(t *testing.T) {
			err := TransformDriver(tc.ds.DaemonSet, tc.cpSpec,
				ClusterPolicyController{client: tc.client, runtime: gpuv1.Containerd,
					operatorNamespace: "test-ns", logger: ctrl.Log.WithName("test")})
			if tc.errorExpected {
				require.Error(t, err)
				return
			}
			require.NoError(t, err)

			// Remove dynamically generated digest before comparison
			removeDigestFromDaemonSet(tc.ds.DaemonSet)
			require.EqualValues(t, tc.expectedDs, tc.ds)
		})
	}
}

func TestTransformDriverWithResources(t *testing.T) {
	node := &corev1.Node{
		ObjectMeta: metav1.ObjectMeta{
			Name: "test-node",
			Labels: map[string]string{
				nfdKernelLabelKey: "6.8.0-60-generic",
				commonGPULabelKey: "true",
			},
		},
	}
	mockClient := fake.NewFakeClient(node)

	resources := gpuv1.ResourceRequirements{
		Requests: corev1.ResourceList{
			corev1.ResourceCPU:    resource.MustParse("100m"),
			corev1.ResourceMemory: resource.MustParse("200Mi"),
		},
		Limits: corev1.ResourceList{
			corev1.ResourceCPU:    resource.MustParse("500m"),
			corev1.ResourceMemory: resource.MustParse("400Mi"),
		},
	}

	testCases := []struct {
		description   string
		ds            Daemonset
		cpSpec        *gpuv1.ClusterPolicySpec
		client        client.Client
		expectedDs    Daemonset
		errorExpected bool
	}{
		{
			description: "transform driver dependent containers with resources",
			ds: NewDaemonset().WithContainer(corev1.Container{Name: "nvidia-driver-ctr"}).
				WithContainer(corev1.Container{Name: "nvidia-fs"}).
				WithContainer(corev1.Container{Name: "nvidia-gdrcopy"}).
				WithContainer(corev1.Container{Name: "nvidia-peermem"}).
				WithInitContainer(corev1.Container{Name: "k8s-driver-manager"}),
			cpSpec: &gpuv1.ClusterPolicySpec{
				Driver: gpuv1.DriverSpec{
					Repository: "nvcr.io/nvidia",
					Image:      "driver",
					Version:    "570.172.08",
					Manager: gpuv1.DriverManagerSpec{
						Repository: "nvcr.io/nvidia/cloud-native",
						Image:      "k8s-driver-manager",
						Version:    "v0.8.0",
					},
					Resources: &resources,
				},
				GPUDirectStorage: &gpuv1.GPUDirectStorageSpec{
					Enabled:    newBoolPtr(true),
					Repository: "nvcr.io/nvidia/cloud-native",
					Image:      "nvidia-fs",
					Version:    "2.20.5",
				},
				GDRCopy: &gpuv1.GDRCopySpec{
					Enabled:    newBoolPtr(true),
					Repository: "nvcr.io/nvidia/cloud-native",
					Image:      "gdrdrv",
					Version:    "v2.5",
				},
			},
			client: mockClient,
			expectedDs: NewDaemonset().WithContainer(corev1.Container{
				Name:            "nvidia-driver-ctr",
				Image:           "nvcr.io/nvidia/driver:570.172.08-",
				ImagePullPolicy: corev1.PullIfNotPresent,
				Resources: corev1.ResourceRequirements{
					Requests: resources.Requests,
					Limits:   resources.Limits,
				},
				Env: []corev1.EnvVar{
					{
						Name:  "GDRCOPY_ENABLED",
						Value: "true",
					},
					{
						Name:  "GDS_ENABLED",
						Value: "true",
					},
				},
			}).WithContainer(corev1.Container{
				Name:  "nvidia-fs",
				Image: "nvcr.io/nvidia/cloud-native/nvidia-fs:2.20.5-",
				Resources: corev1.ResourceRequirements{
					Requests: resources.Requests,
					Limits:   resources.Limits,
				},
			}).WithContainer(corev1.Container{
				Name:  "nvidia-gdrcopy",
				Image: "nvcr.io/nvidia/cloud-native/gdrdrv:v2.5-",
				Resources: corev1.ResourceRequirements{
					Requests: resources.Requests,
					Limits:   resources.Limits,
				},
			}).WithInitContainer(corev1.Container{
				Name:  "k8s-driver-manager",
				Image: "nvcr.io/nvidia/cloud-native/k8s-driver-manager:v0.8.0",
			}),
			errorExpected: false,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.description, func(t *testing.T) {
			err := TransformDriver(tc.ds.DaemonSet, tc.cpSpec,
				ClusterPolicyController{client: tc.client, runtime: gpuv1.Containerd,
					operatorNamespace: "test-ns", logger: ctrl.Log.WithName("test")})
			if tc.errorExpected {
				require.Error(t, err)
				return
			}
			require.NoError(t, err)

			// Remove dynamically generated digest before comparison
			removeDigestFromDaemonSet(tc.ds.DaemonSet)
			require.EqualValues(t, tc.expectedDs, tc.ds)
		})
	}
}

func TestTransformDriverRDMA(t *testing.T) {
	node := &corev1.Node{
		ObjectMeta: metav1.ObjectMeta{
			Name: "test-node",
			Labels: map[string]string{
				nfdKernelLabelKey: "6.8.0-60-generic",
				commonGPULabelKey: "true",
			},
		},
	}
	mockClient := fake.NewFakeClient(node)
	ds := NewDaemonset().WithContainer(corev1.Container{Name: "nvidia-driver-ctr"}).
		WithContainer(corev1.Container{Name: "nvidia-fs"}).
		WithContainer(corev1.Container{Name: "nvidia-gdrcopy"}).
		WithContainer(corev1.Container{Name: "nvidia-peermem"}).
		WithInitContainer(corev1.Container{Name: "k8s-driver-manager"})
	cpSpec := &gpuv1.ClusterPolicySpec{
		Driver: gpuv1.DriverSpec{
			Repository: "nvcr.io/nvidia",
			Image:      "driver",
			Version:    "570.172.08",
			Manager: gpuv1.DriverManagerSpec{
				Repository: "nvcr.io/nvidia/cloud-native",
				Image:      "k8s-driver-manager",
				Version:    "v0.8.0",
			},
			GPUDirectRDMA: &gpuv1.GPUDirectRDMASpec{
				Enabled:      newBoolPtr(true),
				UseHostMOFED: newBoolPtr(true),
			},
		},
	}

	expectedDs := NewDaemonset().WithContainer(corev1.Container{
		Name:            "nvidia-driver-ctr",
		Image:           "nvcr.io/nvidia/driver:570.172.08-",
		ImagePullPolicy: corev1.PullIfNotPresent,
		Env: []corev1.EnvVar{
			{
				Name:  "GPU_DIRECT_RDMA_ENABLED",
				Value: "true",
			},
			{
				Name:  "USE_HOST_MOFED",
				Value: "true",
			},
		},
	}).WithInitContainer(corev1.Container{
		Name:  "k8s-driver-manager",
		Image: "nvcr.io/nvidia/cloud-native/k8s-driver-manager:v0.8.0",
		Env: []corev1.EnvVar{
			{
				Name:  "GPU_DIRECT_RDMA_ENABLED",
				Value: "true",
			},
			{
				Name:  "USE_HOST_MOFED",
				Value: "true",
			},
		},
	}).WithContainer(corev1.Container{
		Name:  "nvidia-peermem",
		Image: "nvcr.io/nvidia/driver:570.172.08-",
		Env: []corev1.EnvVar{
			{
				Name:  "USE_HOST_MOFED",
				Value: "true",
			},
		},
	})

	err := TransformDriver(ds.DaemonSet, cpSpec,
		ClusterPolicyController{client: mockClient, runtime: gpuv1.Containerd,
			operatorNamespace: "test-ns", logger: ctrl.Log.WithName("test")})
	require.NoError(t, err)

	// Remove dynamically generated digest before comparison
	removeDigestFromDaemonSet(ds.DaemonSet)
	require.EqualValues(t, expectedDs, ds)
}

func TestTransformDriverVGPUTopologyConfig(t *testing.T) {
	node := &corev1.Node{
		ObjectMeta: metav1.ObjectMeta{
			Name: "test-node",
			Labels: map[string]string{
				nfdKernelLabelKey: "6.8.0-60-generic",
				commonGPULabelKey: "true",
			},
		},
	}
	mockClient := fake.NewFakeClient(node)
	ds := NewDaemonset().WithContainer(corev1.Container{Name: "nvidia-driver-ctr"}).
		WithInitContainer(corev1.Container{Name: "k8s-driver-manager"})
	cpSpec := &gpuv1.ClusterPolicySpec{
		Driver: gpuv1.DriverSpec{
			Repository: "nvcr.io/nvidia",
			Image:      "driver",
			Version:    "570.172.08",
			Manager: gpuv1.DriverManagerSpec{
				Repository: "nvcr.io/nvidia/cloud-native",
				Image:      "k8s-driver-manager",
				Version:    "v0.8.0",
			},
			VirtualTopology: &gpuv1.VirtualTopologyConfigSpec{
				Config: "sample-topology-config",
			},
		},
	}

	expectedDs := NewDaemonset().WithContainer(corev1.Container{
		Name:            "nvidia-driver-ctr",
		Image:           "nvcr.io/nvidia/driver:570.172.08-",
		ImagePullPolicy: corev1.PullIfNotPresent,
		VolumeMounts: []corev1.VolumeMount{
			{
				Name:      "topology-config",
				ReadOnly:  true,
				MountPath: consts.VGPUTopologyConfigMountPath,
				SubPath:   consts.VGPUTopologyConfigFileName,
			},
		},
	}).WithInitContainer(corev1.Container{
		Name:  "k8s-driver-manager",
		Image: "nvcr.io/nvidia/cloud-native/k8s-driver-manager:v0.8.0",
	}).WithVolume(corev1.Volume{
		Name: "topology-config",
		VolumeSource: corev1.VolumeSource{
			ConfigMap: &corev1.ConfigMapVolumeSource{
				LocalObjectReference: corev1.LocalObjectReference{
					Name: "sample-topology-config",
				},
				Items: []corev1.KeyToPath{
					{
						Key:  consts.VGPUTopologyConfigFileName,
						Path: consts.VGPUTopologyConfigFileName,
					},
				},
			},
		},
	})

	err := TransformDriver(ds.DaemonSet, cpSpec,
		ClusterPolicyController{client: mockClient, runtime: gpuv1.Containerd,
			operatorNamespace: "test-ns", logger: ctrl.Log.WithName("test")})
	require.NoError(t, err)
	removeDigestFromDaemonSet(ds.DaemonSet)
	require.EqualValues(t, expectedDs, ds)
}
