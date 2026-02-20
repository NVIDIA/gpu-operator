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

package utils

import (
	"testing"

	"github.com/stretchr/testify/assert"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	nvidiav1alpha1 "github.com/NVIDIA/gpu-operator/api/nvidia/v1alpha1"
)

// baseDriverDaemonSetSpec returns a minimal DaemonSetSpec representative of
// the post-transformation driver DaemonSet in the ClusterPolicy path.
// Only fields relevant to ExtractDriverInstallConfig extraction are
// populated; non-digest fields are omitted for brevity.
func baseDriverDaemonSetSpec() *appsv1.DaemonSetSpec {
	return &appsv1.DaemonSetSpec{
		Template: corev1.PodTemplateSpec{
			ObjectMeta: metav1.ObjectMeta{
				Labels: map[string]string{"app": "nvidia-gpu-driver"},
			},
			Spec: corev1.PodSpec{
				NodeSelector:       map[string]string{"nvidia.com/gpu.deploy.driver": "true"},
				PriorityClassName:  "system-node-critical",
				ServiceAccountName: "nvidia-gpu-driver",
				HostPID:            true,
				InitContainers: []corev1.Container{{
					Name:  "k8s-driver-manager",
					Image: "nvcr.io/nvidia/cloud-native/k8s-driver-manager:v0.6.2",
					Env: []corev1.EnvVar{
						{Name: "NODE_NAME", ValueFrom: &corev1.EnvVarSource{
							FieldRef: &corev1.ObjectFieldSelector{FieldPath: "spec.nodeName"},
						}},
						{Name: "NVIDIA_VISIBLE_DEVICES", Value: "void"},
					},
				}},
				Containers: []corev1.Container{{
					Name:    "nvidia-driver-ctr",
					Image:   "nvcr.io/nvidia/driver:525.85.03-ubuntu22.04",
					Command: []string{"nvidia-driver"},
					Args:    []string{"init"},
					Env: []corev1.EnvVar{
						{Name: "NVIDIA_VISIBLE_DEVICES", Value: "void"},
						{Name: "NODE_NAME", ValueFrom: &corev1.EnvVarSource{
							FieldRef: &corev1.ObjectFieldSelector{FieldPath: "spec.nodeName"},
						}},
					},
					VolumeMounts: []corev1.VolumeMount{
						{Name: "run-nvidia", MountPath: "/run/nvidia"},
					},
				}},
				Volumes: []corev1.Volume{
					{Name: "run-nvidia", VolumeSource: corev1.VolumeSource{
						HostPath: &corev1.HostPathVolumeSource{Path: "/run/nvidia"},
					}},
					{Name: "host-root", VolumeSource: corev1.VolumeSource{
						HostPath: &corev1.HostPathVolumeSource{Path: "/"},
					}},
				},
			},
		},
	}
}

// TestDriverConfigDigest verifies that non-driver-relevant field changes
// (wantChange=false) do NOT alter the digest, while driver-relevant changes
// (wantChange=true) DO alter it.
func TestDriverConfigDigest(t *testing.T) {
	baseDigest := GetObjectHash(ExtractDriverInstallConfig(baseDriverDaemonSetSpec()))

	tests := []struct {
		name       string
		wantChange bool
		modify     func(*appsv1.DaemonSetSpec)
	}{
		// Non-driver-relevant fields: digest must NOT change.
		{"resource limits", false, func(s *appsv1.DaemonSetSpec) {
			s.Template.Spec.Containers[0].Resources.Limits = corev1.ResourceList{
				corev1.ResourceCPU: resource.MustParse("4"),
			}
		}},
		{"startup probe", false, func(s *appsv1.DaemonSetSpec) {
			s.Template.Spec.Containers[0].StartupProbe = &corev1.Probe{InitialDelaySeconds: 5}
		}},
		{"tolerations", false, func(s *appsv1.DaemonSetSpec) {
			s.Template.Spec.Tolerations = []corev1.Toleration{{Key: "custom", Effect: "NoExecute"}}
		}},
		{"node selector", false, func(s *appsv1.DaemonSetSpec) {
			s.Template.Spec.NodeSelector["custom-label"] = "v"
		}},
		{"container security context", false, func(s *appsv1.DaemonSetSpec) {
			s.Template.Spec.Containers[0].SecurityContext = &corev1.SecurityContext{}
		}},
		{"pod security context", false, func(s *appsv1.DaemonSetSpec) {
			runAsUser := int64(1000)
			s.Template.Spec.SecurityContext = &corev1.PodSecurityContext{
				RunAsUser: &runAsUser,
			}
		}},
		{"image pull policy", false, func(s *appsv1.DaemonSetSpec) {
			s.Template.Spec.Containers[0].ImagePullPolicy = corev1.PullAlways
		}},
		{"update strategy", false, func(s *appsv1.DaemonSetSpec) {
			s.UpdateStrategy.Type = appsv1.RollingUpdateDaemonSetStrategyType
		}},
		{"priority class", false, func(s *appsv1.DaemonSetSpec) {
			s.Template.Spec.PriorityClassName = "custom-priority"
		}},
		{"service account", false, func(s *appsv1.DaemonSetSpec) {
			s.Template.Spec.ServiceAccountName = "different-sa"
		}},
		{"hostPID", false, func(s *appsv1.DaemonSetSpec) {
			s.Template.Spec.HostPID = false
		}},
		{"pod labels", false, func(s *appsv1.DaemonSetSpec) {
			s.Template.Labels["extra"] = "v"
		}},
		{"init container resources", false, func(s *appsv1.DaemonSetSpec) {
			s.Template.Spec.InitContainers[0].Resources.Limits = corev1.ResourceList{
				corev1.ResourceCPU: resource.MustParse("2"),
			}
		}},
		{"FieldRef env var added", false, func(s *appsv1.DaemonSetSpec) {
			s.Template.Spec.Containers[0].Env = append(s.Template.Spec.Containers[0].Env,
				corev1.EnvVar{Name: "NODE_IP", ValueFrom: &corev1.EnvVarSource{
					FieldRef: &corev1.ObjectFieldSelector{FieldPath: "status.hostIP"},
				}})
		}},
		{"unrecognized container", false, func(s *appsv1.DaemonSetSpec) {
			s.Template.Spec.Containers = append(s.Template.Spec.Containers,
				corev1.Container{Name: "sidecar", Image: "example.com/sidecar:v1"})
		}},
		{"unrecognized init container", false, func(s *appsv1.DaemonSetSpec) {
			s.Template.Spec.InitContainers = append(s.Template.Spec.InitContainers,
				corev1.Container{Name: "extra-init", Image: "example.com/init:v1"})
		}},
		{"pod annotation", false, func(s *appsv1.DaemonSetSpec) {
			s.Template.Annotations = map[string]string{"k": "v"}
		}},

		// Driver-relevant fields: digest MUST change.
		{"driver image", true, func(s *appsv1.DaemonSetSpec) {
			s.Template.Spec.Containers[0].Image = "nvcr.io/nvidia/driver:535.104.05-ubuntu22.04"
		}},
		{"driver manager image", true, func(s *appsv1.DaemonSetSpec) {
			s.Template.Spec.InitContainers[0].Image = "nvcr.io/nvidia/cloud-native/k8s-driver-manager:v0.7.0"
		}},
		{"env var added", true, func(s *appsv1.DaemonSetSpec) {
			s.Template.Spec.Containers[0].Env = append(s.Template.Spec.Containers[0].Env,
				corev1.EnvVar{Name: "KERNEL_MODULE_TYPE", Value: "open"})
		}},
		{"env var modified", true, func(s *appsv1.DaemonSetSpec) {
			s.Template.Spec.Containers[0].Env[0].Value = "modified"
		}},
		{"args change", true, func(s *appsv1.DaemonSetSpec) {
			s.Template.Spec.Containers[0].Args = []string{"init", "--extra-arg"}
		}},
		{"peermem container (RDMA)", true, func(s *appsv1.DaemonSetSpec) {
			s.Template.Spec.Containers = append(s.Template.Spec.Containers,
				corev1.Container{Name: "nvidia-peermem-ctr", Image: "nvcr.io/nvidia/driver:525.85.03-ubuntu22.04"})
		}},
		{"GDS container", true, func(s *appsv1.DaemonSetSpec) {
			s.Template.Spec.Containers = append(s.Template.Spec.Containers,
				corev1.Container{Name: "nvidia-fs-ctr", Image: "nvcr.io/nvidia/cloud-native/nvidia-fs:2.16.1"})
		}},
		{"GDRCopy container", true, func(s *appsv1.DaemonSetSpec) {
			s.Template.Spec.Containers = append(s.Template.Spec.Containers,
				corev1.Container{Name: "nvidia-gdrcopy-ctr", Image: "nvcr.io/nvidia/cloud-native/gdrdrv:v2.4.1"})
		}},
		{"config volume (licensing)", true, func(s *appsv1.DaemonSetSpec) {
			s.Template.Spec.Volumes = append(s.Template.Spec.Volumes, corev1.Volume{
				Name: "licensing-config", VolumeSource: corev1.VolumeSource{
					ConfigMap: &corev1.ConfigMapVolumeSource{
						LocalObjectReference: corev1.LocalObjectReference{Name: "licensing-configmap"},
					},
				},
			})
		}},
		{"volume source changed", true, func(s *appsv1.DaemonSetSpec) {
			s.Template.Spec.Volumes = append(s.Template.Spec.Volumes, corev1.Volume{
				Name: "licensing-config", VolumeSource: corev1.VolumeSource{
					Secret: &corev1.SecretVolumeSource{SecretName: "licensing-secret"},
				},
			})
		}},
		{"volume mount added", true, func(s *appsv1.DaemonSetSpec) {
			s.Template.Spec.Containers[0].VolumeMounts = append(s.Template.Spec.Containers[0].VolumeMounts,
				corev1.VolumeMount{Name: "kernel-module-config", MountPath: "/drivers"})
		}},
		{"host root changed", true, func(s *appsv1.DaemonSetSpec) {
			for i := range s.Template.Spec.Volumes {
				if s.Template.Spec.Volumes[i].Name == "host-root" {
					s.Template.Spec.Volumes[i].HostPath.Path = "/custom-root"
				}
			}
		}},
		{"driver command", true, func(s *appsv1.DaemonSetSpec) {
			s.Template.Spec.Containers[0].Command = []string{"ocp_dtk_entrypoint"}
		}},
		{"secret env source", true, func(s *appsv1.DaemonSetSpec) {
			s.Template.Spec.Containers[0].EnvFrom = []corev1.EnvFromSource{
				{SecretRef: &corev1.SecretEnvSource{
					LocalObjectReference: corev1.LocalObjectReference{Name: "driver-secret"},
				}},
			}
		}},
		{"manager env var added", true, func(s *appsv1.DaemonSetSpec) {
			s.Template.Spec.InitContainers[0].Env = append(s.Template.Spec.InitContainers[0].Env,
				corev1.EnvVar{Name: "GPU_DIRECT_RDMA_ENABLED", Value: "true"})
		}},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			spec := baseDriverDaemonSetSpec()
			tc.modify(spec)
			digest := GetObjectHash(ExtractDriverInstallConfig(spec))
			if tc.wantChange {
				assert.NotEqual(t, baseDigest, digest, "digest SHOULD change")
			} else {
				assert.Equal(t, baseDigest, digest, "digest should NOT change")
			}
		})
	}
}

// TestExtractDriverInstallConfigExtraction verifies that the extractor
// correctly populates fields from the DaemonSet spec.
func TestExtractDriverInstallConfigExtraction(t *testing.T) {
	spec := baseDriverDaemonSetSpec()
	spec.Template.Spec.Containers = append(spec.Template.Spec.Containers,
		corev1.Container{
			Name:  "openshift-driver-toolkit-ctr",
			Image: "quay.io/openshift-release-dev/ocp-v4.0-art-dev@sha256:abc123",
		})
	spec.Template.Spec.Containers[0].Env = append(spec.Template.Spec.Containers[0].Env,
		corev1.EnvVar{Name: "KERNEL_MODULE_TYPE", Value: "open"},
		corev1.EnvVar{Name: "OPENSHIFT_VERSION", Value: "4.13"},
	)
	spec.Template.Spec.Containers[0].EnvFrom = []corev1.EnvFromSource{
		{SecretRef: &corev1.SecretEnvSource{
			LocalObjectReference: corev1.LocalObjectReference{Name: "test-secret"},
		}},
	}

	config := ExtractDriverInstallConfig(spec)

	assert.Equal(t, "nvcr.io/nvidia/driver:525.85.03-ubuntu22.04", config.DriverImage)
	assert.Equal(t, "nvcr.io/nvidia/cloud-native/k8s-driver-manager:v0.6.2", config.DriverManagerImage)
	assert.Equal(t, []string{"nvidia-driver"}, config.DriverCommand)
	assert.Equal(t, []string{"init"}, config.DriverArgs)
	// KernelModuleType and OpenshiftVersion are captured in DriverEnv (not
	// as top-level fields) via the DaemonSet extraction path.
	assert.Contains(t, config.DriverEnv, nvidiav1alpha1.EnvVar{Name: "KERNEL_MODULE_TYPE", Value: "open"})
	assert.Contains(t, config.DriverEnv, nvidiav1alpha1.EnvVar{Name: "OPENSHIFT_VERSION", Value: "4.13"})
	assert.Equal(t, "test-secret", config.SecretEnvSource)
	assert.True(t, config.DTKEnabled)
	assert.Equal(t, "quay.io/openshift-release-dev/ocp-v4.0-art-dev@sha256:abc123", config.DTKImage)
	assert.Equal(t, "/", config.HostRoot)

	// FieldRef env vars should be excluded.
	for _, ev := range config.DriverEnv {
		assert.NotEqual(t, "NODE_NAME", ev.Name, "FieldRef env vars should be excluded")
	}
}
