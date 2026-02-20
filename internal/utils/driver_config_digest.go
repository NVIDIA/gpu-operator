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
	"sort"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"

	nvidiav1alpha1 "github.com/NVIDIA/gpu-operator/api/nvidia/v1alpha1"
)

// DriverInstallConfig lists all fields that affect driver installation.
// Changes to these fields trigger a driver reinstall.
//
// This struct is shared by two code paths:
//   - ClusterPolicy (ExtractDriverInstallConfig): extracts fields from a
//     fully-transformed DaemonSet. Fields like KernelModuleType and proxy
//     settings are captured implicitly through env vars and volumes rather than
//     as top-level struct fields.
//   - NVIDIADriver CR (buildDriverInstallConfig): populates fields
//     directly from the CR spec before the DaemonSet is rendered.
//
// Fields that are only relevant to one path are left zero-valued by the other.
type DriverInstallConfig struct {
	// Container images
	DriverImage        string
	DriverManagerImage string
	PeermemImage       string
	GDSImage           string
	GDRCopyImage       string
	DTKImage           string

	// Driver type and kernel module variant (e.g. "open" vs "proprietary")
	DriverType       string
	KernelModuleType string

	// Entrypoint override for the nvidia-driver-ctr container
	DriverCommand []string
	DriverArgs    []string

	// Per-container environment variables (direct values only, not ValueFrom)
	DriverEnv  []nvidiav1alpha1.EnvVar
	ManagerEnv []nvidiav1alpha1.EnvVar
	GDSEnv     []nvidiav1alpha1.EnvVar
	GDRCopyEnv []nvidiav1alpha1.EnvVar

	// Name of the Secret used as an EnvFrom source on the driver container
	SecretEnvSource string

	// Feature toggles
	GPUDirectRDMAEnabled bool
	UseHostMOFED         bool
	GDSEnabled           bool
	GDRCopyEnabled       bool

	// Names of ConfigMaps/Secrets that supply licensing, topology, repo, and cert config
	LicensingConfigName   string
	VirtualTopologyConfig string
	KernelModuleConfig    string
	RepoConfig            string
	CertConfig            string

	// Pre-compiled driver settings
	UsePrecompiled bool
	KernelVersion  string

	// OpenShift-specific fields
	OpenshiftVersion string
	DTKEnabled       bool
	RHCOSVersion     string

	// Proxy configuration injected into the driver pod
	HTTPProxy              string
	HTTPSProxy             string
	NoProxy                string
	TrustedCAConfigMapName string

	// User-supplied volumes and mounts (licensing, certs, repo config, etc.).
	// In the DaemonSet extraction path these capture all volumes/mounts,
	// not only the "additional" ones.
	AdditionalVolumes      []VolumeConfig
	AdditionalVolumeMounts []VolumeMountConfig

	// Root filesystem path of the host, mounted into the driver container
	HostRoot string
}

// VolumeConfig and VolumeMountConfig are purposefully not corev1.Volume /
// corev1.VolumeMount. Including them would make the digest change whenever Kubernetes
// adds a new field to the struct, even if the operator's configuration is
// identical.
type VolumeConfig struct {
	Name          string
	ConfigMapName string
	SecretName    string
	HostPath      string
}

type VolumeMountConfig struct {
	Name      string
	MountPath string
	SubPath   string
	ReadOnly  bool
}

// ExtractDriverInstallConfig extracts driver-relevant fields from a
// post-transformation DaemonSetSpec (ClusterPolicy path). Fields like
// KernelModuleType and proxy settings are captured implicitly via the
// per-container env var maps rather than as top-level struct fields.
func ExtractDriverInstallConfig(spec *appsv1.DaemonSetSpec) *DriverInstallConfig {
	config := &DriverInstallConfig{}
	podSpec := spec.Template.Spec

	for i := range podSpec.InitContainers {
		c := &podSpec.InitContainers[i]
		if c.Name == "k8s-driver-manager" {
			config.DriverManagerImage = c.Image
			config.ManagerEnv = extractEnvVars(c.Env)
		}
	}

	for i := range podSpec.Containers {
		c := &podSpec.Containers[i]
		switch c.Name {
		case "nvidia-driver-ctr":
			config.DriverImage = c.Image
			config.DriverCommand = c.Command
			config.DriverArgs = c.Args
			config.DriverEnv = extractEnvVars(c.Env)
			for _, ef := range c.EnvFrom {
				if ef.SecretRef != nil {
					config.SecretEnvSource = ef.SecretRef.Name
				}
			}
			config.AdditionalVolumeMounts = ExtractVolumeMounts(c.VolumeMounts)
		case "nvidia-peermem-ctr":
			config.PeermemImage = c.Image
			config.GPUDirectRDMAEnabled = true
		case "nvidia-fs-ctr":
			config.GDSImage = c.Image
			config.GDSEnabled = true
			config.GDSEnv = extractEnvVars(c.Env)
		case "nvidia-gdrcopy-ctr":
			config.GDRCopyImage = c.Image
			config.GDRCopyEnabled = true
			config.GDRCopyEnv = extractEnvVars(c.Env)
		case "openshift-driver-toolkit-ctr":
			config.DTKImage = c.Image
			config.DTKEnabled = true
		}
	}

	config.AdditionalVolumes = ExtractVolumes(podSpec.Volumes)
	for _, v := range podSpec.Volumes {
		if v.Name == "host-root" && v.HostPath != nil {
			config.HostRoot = v.HostPath.Path
		}
	}

	return config
}

// SortEnvVars returns a sorted copy of the given env vars for deterministic hashing.
func SortEnvVars(envs []nvidiav1alpha1.EnvVar) []nvidiav1alpha1.EnvVar {
	if len(envs) == 0 {
		return envs
	}
	sorted := make([]nvidiav1alpha1.EnvVar, len(envs))
	copy(sorted, envs)
	sort.Slice(sorted, func(i, j int) bool { return sorted[i].Name < sorted[j].Name })
	return sorted
}

// extractEnvVars extracts env vars with direct values, skipping ValueFrom
// (FieldRef, etc.) since those are not part of the driver install configuration.
// Results are sorted by name for deterministic hashing.
func extractEnvVars(envs []corev1.EnvVar) []nvidiav1alpha1.EnvVar {
	var result []nvidiav1alpha1.EnvVar
	for _, e := range envs {
		if e.ValueFrom != nil {
			continue
		}
		result = append(result, nvidiav1alpha1.EnvVar{Name: e.Name, Value: e.Value})
	}
	return SortEnvVars(result)
}

// ExtractVolumeMounts converts corev1.VolumeMounts to a digest-stable
// representation, sorted by name then mount path.
func ExtractVolumeMounts(mounts []corev1.VolumeMount) []VolumeMountConfig {
	var result []VolumeMountConfig
	for _, vm := range mounts {
		result = append(result, VolumeMountConfig{
			Name: vm.Name, MountPath: vm.MountPath,
			SubPath: vm.SubPath, ReadOnly: vm.ReadOnly,
		})
	}
	sort.Slice(result, func(i, j int) bool {
		if result[i].Name != result[j].Name {
			return result[i].Name < result[j].Name
		}
		return result[i].MountPath < result[j].MountPath
	})
	return result
}

// ExtractVolumes builds a digest-stable representation of each volume.
// Only ConfigMap, Secret, and HostPath source details are captured;
// other volume types (e.g. emptyDir) still contribute their Name to the digest.
func ExtractVolumes(volumes []corev1.Volume) []VolumeConfig {
	var result []VolumeConfig
	for _, v := range volumes {
		vc := VolumeConfig{Name: v.Name}
		if v.ConfigMap != nil {
			vc.ConfigMapName = v.ConfigMap.Name
		}
		if v.Secret != nil {
			vc.SecretName = v.Secret.SecretName
		}
		if v.HostPath != nil {
			vc.HostPath = v.HostPath.Path
		}
		result = append(result, vc)
	}
	sort.Slice(result, func(i, j int) bool { return result[i].Name < result[j].Name })
	return result
}
