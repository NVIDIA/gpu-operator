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
	"sort"

	corev1 "k8s.io/api/core/v1"
)

// DriverInstallState lists all fields that affect driver installation.
// Changes to these fields trigger a driver reinstall.
//
// The digest is computed by hashing non-zero fields in alphabetical order
// by field name (see GetObjectHashIgnoreEmptyKeys). Consequences for
// struct modifications:
//
//   - Add field (zero value):     no digest change → no reinstall
//   - Add field (non-zero value): digest change    → reinstall
//   - Remove field:               digest change    → reinstall
//   - Rename field:               digest change    → reinstall
//   - Reorder fields:             no digest change → no reinstall
//
// This struct is shared by two code paths:
//   - ClusterPolicy (extractDriverInstallConfig): extracts fields from a
//     fully-transformed DaemonSet's PodSpec. Fields like KernelModuleType and proxy
//     settings are captured implicitly through env vars and volumes rather than
//     as top-level struct fields.
//   - NVIDIADriver CR (buildDriverInstallConfig): populates fields
//     directly from the CR spec before the DaemonSet is rendered.
//
// Fields that are only relevant to one path are left zero-valued by the other.
type DriverInstallState struct {
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
	DriverEnv  []EnvVar
	ManagerEnv []EnvVar
	GDSEnv     []EnvVar
	GDRCopyEnv []EnvVar

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

// EnvVar mirrors the API EnvVar type to avoid a dependency on the API package.
type EnvVar struct {
	Name  string
	Value string
}

// SortEnvVars returns a sorted copy of the given env vars for deterministic hashing.
func SortEnvVars(envs []EnvVar) []EnvVar {
	if len(envs) == 0 {
		return envs
	}
	sorted := make([]EnvVar, len(envs))
	copy(sorted, envs)
	sort.Slice(sorted, func(i, j int) bool { return sorted[i].Name < sorted[j].Name })
	return sorted
}

// ExtractEnvVars extracts env vars with direct values, skipping ValueFrom
// (FieldRef, etc.) since those are not part of the driver install configuration.
// Results are sorted by name for deterministic hashing.
func ExtractEnvVars(envs []corev1.EnvVar) []EnvVar {
	var result []EnvVar
	for _, e := range envs {
		if e.ValueFrom != nil {
			continue
		}
		result = append(result, EnvVar{Name: e.Name, Value: e.Value})
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
