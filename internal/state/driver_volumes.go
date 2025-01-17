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

package state

import (
	"context"
	"fmt"
	"os"
	"sort"

	corev1 "k8s.io/api/core/v1"
	"sigs.k8s.io/controller-runtime/pkg/log"

	"github.com/NVIDIA/gpu-operator/api/nvidia/v1alpha1"
	"github.com/NVIDIA/gpu-operator/controllers/clusterinfo"
	"github.com/NVIDIA/gpu-operator/internal/consts"
)

// RepoConfigPathMap indicates standard OS specific paths for repository configuration files
var RepoConfigPathMap = map[string]string{
	"centos": "/etc/yum.repos.d",
	"ubuntu": "/etc/apt/sources.list.d",
	"rhcos":  "/etc/yum.repos.d",
	"rhel":   "/etc/yum.repos.d",
}

// CertConfigPathMap indicates standard OS specific paths for ssl keys/certificates.
// Where Go looks for certs: https://golang.org/src/crypto/x509/root_linux.go
// Where OCP mounts proxy certs on RHCOS nodes:
// https://access.redhat.com/documentation/en-us/openshift_container_platform/4.3/html/authentication/ocp-certificates#proxy-certificates_ocp-certificates
var CertConfigPathMap = map[string]string{
	"centos": "/etc/pki/ca-trust/extracted/pem",
	"ubuntu": "/usr/local/share/ca-certificates",
	"rhcos":  "/etc/pki/ca-trust/extracted/pem",
	"rhel":   "/etc/pki/ca-trust/extracted/pem",
}

// MountPathToVolumeSource maps a container mount path to a VolumeSource
type MountPathToVolumeSource map[string]corev1.VolumeSource

// SubscriptionPathMap contains information on OS-specific paths
// that provide entitlements/subscription details on the host.
// These are used to enable Driver Container's access to packages controlled by
// the distro through their subscription and support program.
var SubscriptionPathMap = map[string]MountPathToVolumeSource{
	"rhel": {
		"/run/secrets/etc-pki-entitlement": corev1.VolumeSource{
			HostPath: &corev1.HostPathVolumeSource{
				Path: "/etc/pki/entitlement",
				Type: newHostPathType(corev1.HostPathDirectory),
			},
		},
		"/run/secrets/redhat.repo": corev1.VolumeSource{
			HostPath: &corev1.HostPathVolumeSource{
				Path: "/etc/yum.repos.d/redhat.repo",
				Type: newHostPathType(corev1.HostPathFile),
			},
		},
		"/run/secrets/rhsm": corev1.VolumeSource{
			HostPath: &corev1.HostPathVolumeSource{
				Path: "/etc/rhsm",
				Type: newHostPathType(corev1.HostPathDirectory),
			},
		},
	},
	"rhcos": {
		"/run/secrets/etc-pki-entitlement": corev1.VolumeSource{
			HostPath: &corev1.HostPathVolumeSource{
				Path: "/etc/pki/entitlement",
				Type: newHostPathType(corev1.HostPathDirectory),
			},
		},
		"/run/secrets/redhat.repo": corev1.VolumeSource{
			HostPath: &corev1.HostPathVolumeSource{
				Path: "/etc/yum.repos.d/redhat.repo",
				Type: newHostPathType(corev1.HostPathFile),
			},
		},
		"/run/secrets/rhsm": corev1.VolumeSource{
			HostPath: &corev1.HostPathVolumeSource{
				Path: "/etc/rhsm",
				Type: newHostPathType(corev1.HostPathDirectory),
			},
		},
	},
	"sles": {
		"/etc/zypp/credentials.d": corev1.VolumeSource{
			HostPath: &corev1.HostPathVolumeSource{
				Path: "/etc/zypp/credentials.d",
				Type: newHostPathType(corev1.HostPathDirectory),
			},
		},
		"/etc/SUSEConnect": corev1.VolumeSource{
			HostPath: &corev1.HostPathVolumeSource{
				Path: "/etc/SUSEConnect",
				Type: newHostPathType(corev1.HostPathFileOrCreate),
			},
		},
	},
	"sl-micro": {
		"/etc/zypp/credentials.d": corev1.VolumeSource{
			HostPath: &corev1.HostPathVolumeSource{
				Path: "/etc/zypp/credentials.d",
				Type: newHostPathType(corev1.HostPathDirectory),
			},
		},
		"/etc/SUSEConnect": corev1.VolumeSource{
			HostPath: &corev1.HostPathVolumeSource{
				Path: "/etc/SUSEConnect",
				Type: newHostPathType(corev1.HostPathFileOrCreate),
			},
		},
	},
}

// TODO: make this a public utils method
func newHostPathType(pathType corev1.HostPathType) *corev1.HostPathType {
	hostPathType := new(corev1.HostPathType)
	*hostPathType = pathType
	return hostPathType
}

func (s *stateDriver) getDriverAdditionalConfigs(ctx context.Context, cr *v1alpha1.NVIDIADriver, info clusterinfo.Interface, pool nodePool) (*additionalConfigs, error) {
	logger := log.FromContext(ctx, "method", "getDriverAdditionalConfigs")

	additionalCfgs := &additionalConfigs{}

	operatorNamespace := os.Getenv("OPERATOR_NAMESPACE")
	if operatorNamespace == "" {
		return nil, fmt.Errorf("OPERATOR_NAMESPACE environment variable not set")
	}

	if !cr.Spec.UsePrecompiledDrivers() {
		if cr.Spec.IsRepoConfigEnabled() {
			destinationDir, err := getRepoConfigPath(pool.osRelease)
			if err != nil {
				return nil, fmt.Errorf("ERROR: failed to get destination directory for custom repo config: %w", err)
			}
			volumeMounts, itemsToInclude, err := s.createConfigMapVolumeMounts(ctx, operatorNamespace,
				cr.Spec.RepoConfig.Name, destinationDir)
			if err != nil {
				return nil, fmt.Errorf("ERROR: failed to create ConfigMap VolumeMounts for custom repo config: %w", err)
			}
			additionalCfgs.VolumeMounts = append(additionalCfgs.VolumeMounts, volumeMounts...)
			additionalCfgs.Volumes = append(additionalCfgs.Volumes, createConfigMapVolume(cr.Spec.RepoConfig.Name, itemsToInclude))
		}

		// set any custom ssl key/certificate configuration provided
		if cr.Spec.IsCertConfigEnabled() {
			destinationDir, err := getCertConfigPath(pool.osRelease)
			if err != nil {
				return nil, fmt.Errorf("ERROR: failed to get destination directory for custom repo config: %w", err)
			}
			volumeMounts, itemsToInclude, err := s.createConfigMapVolumeMounts(ctx, operatorNamespace,
				cr.Spec.CertConfig.Name, destinationDir)
			if err != nil {
				return nil, fmt.Errorf("ERROR: failed to create ConfigMap VolumeMounts for custom certs: %w", err)
			}
			additionalCfgs.VolumeMounts = append(additionalCfgs.VolumeMounts, volumeMounts...)
			additionalCfgs.Volumes = append(additionalCfgs.Volumes, createConfigMapVolume(cr.Spec.CertConfig.Name, itemsToInclude))
		}

		runtime, err := info.GetContainerRuntime()
		if err != nil {
			return nil, fmt.Errorf("unexpected error when trying to retrieve container runtime info from cluster: %w", err)
		}

		openshiftVersion, err := info.GetOpenshiftVersion()
		if err != nil {
			return nil, fmt.Errorf("unexpected error when introspecting cluster: %w", err)
		}

		// set up subscription entitlements for RHEL(using K8s with a non-CRIO runtime) and SLES
		if (pool.osRelease == "rhel" && openshiftVersion == "" && runtime != consts.CRIO) || pool.osRelease == "sles" || pool.osRelease == "sl-micro" {
			logger.Info("Mounting subscriptions into the driver container", "OS", pool.osVersion)
			pathToVolumeSource, err := getSubscriptionPathsToVolumeSources(pool.osRelease)
			if err != nil {
				return nil, fmt.Errorf("ERROR: failed to get path items for subscription entitlements: %v", err)
			}

			// sort host path volumes to ensure ordering is preserved when adding to pod spec
			mountPaths := make([]string, 0, len(pathToVolumeSource))
			for k := range pathToVolumeSource {
				mountPaths = append(mountPaths, k)
			}
			sort.Strings(mountPaths)

			for num, mountPath := range mountPaths {
				volMountSubscriptionName := fmt.Sprintf("subscription-config-%d", num)

				volMountSubscription := corev1.VolumeMount{
					Name:      volMountSubscriptionName,
					MountPath: mountPath,
					ReadOnly:  true,
				}
				additionalCfgs.VolumeMounts = append(additionalCfgs.VolumeMounts, volMountSubscription)

				subscriptionVol := corev1.Volume{Name: volMountSubscriptionName, VolumeSource: pathToVolumeSource[mountPath]}
				additionalCfgs.Volumes = append(additionalCfgs.Volumes, subscriptionVol)
			}
		}
	}

	// mount any custom kernel module configuration parameters at /drivers
	if cr.Spec.IsKernelModuleConfigEnabled() {
		destinationDir := "/drivers"
		volumeMounts, itemsToInclude, err := s.createConfigMapVolumeMounts(ctx, operatorNamespace,
			cr.Spec.KernelModuleConfig.Name, destinationDir)
		if err != nil {
			return nil, fmt.Errorf("ERROR: failed to create ConfigMap VolumeMounts for kernel module configuration: %w", err)
		}
		additionalCfgs.VolumeMounts = append(additionalCfgs.VolumeMounts, volumeMounts...)
		additionalCfgs.Volumes = append(additionalCfgs.Volumes, createConfigMapVolume(cr.Spec.KernelModuleConfig.Name, itemsToInclude))
	}

	// set any licensing configuration required
	if cr.Spec.IsVGPULicensingEnabled() {
		licensingConfigVolMount := corev1.VolumeMount{Name: "licensing-config", ReadOnly: true,
			MountPath: consts.VGPULicensingConfigMountPath, SubPath: consts.VGPULicensingFileName}
		additionalCfgs.VolumeMounts = append(additionalCfgs.VolumeMounts, licensingConfigVolMount)

		// gridd.conf always mounted
		licenseItemsToInclude := []corev1.KeyToPath{
			{
				Key:  consts.VGPULicensingFileName,
				Path: consts.VGPULicensingFileName,
			},
		}
		// client config token only mounted when NLS is enabled
		if cr.Spec.LicensingConfig.IsNLSEnabled() {
			licenseItemsToInclude = append(licenseItemsToInclude, corev1.KeyToPath{
				Key:  consts.NLSClientTokenFileName,
				Path: consts.NLSClientTokenFileName,
			})
			nlsTokenVolMount := corev1.VolumeMount{Name: "licensing-config", ReadOnly: true,
				MountPath: consts.NLSClientTokenMountPath, SubPath: consts.NLSClientTokenFileName}
			additionalCfgs.VolumeMounts = append(additionalCfgs.VolumeMounts, nlsTokenVolMount)
		}

		licensingConfigVolumeSource := corev1.VolumeSource{
			ConfigMap: &corev1.ConfigMapVolumeSource{
				LocalObjectReference: corev1.LocalObjectReference{
					Name: cr.Spec.LicensingConfig.Name,
				},
				Items: licenseItemsToInclude,
			},
		}
		licensingConfigVol := corev1.Volume{Name: "licensing-config", VolumeSource: licensingConfigVolumeSource}
		additionalCfgs.Volumes = append(additionalCfgs.Volumes, licensingConfigVol)
	}

	// set virtual topology daemon configuration if specified for vGPU driver
	if cr.Spec.IsVirtualTopologyConfigEnabled() {
		topologyConfigVolMount := corev1.VolumeMount{Name: "topology-config", ReadOnly: true, MountPath: consts.VGPUTopologyConfigMountPath, SubPath: consts.VGPUTopologyConfigFileName}
		additionalCfgs.VolumeMounts = append(additionalCfgs.VolumeMounts, topologyConfigVolMount)

		topologyConfigVolumeSource := corev1.VolumeSource{
			ConfigMap: &corev1.ConfigMapVolumeSource{
				LocalObjectReference: corev1.LocalObjectReference{
					Name: cr.Spec.VirtualTopologyConfig.Name,
				},
				Items: []corev1.KeyToPath{
					{
						Key:  consts.VGPUTopologyConfigFileName,
						Path: consts.VGPUTopologyConfigFileName,
					},
				},
			},
		}
		topologyConfigVol := corev1.Volume{Name: "topology-config", VolumeSource: topologyConfigVolumeSource}
		additionalCfgs.Volumes = append(additionalCfgs.Volumes, topologyConfigVol)
	}

	return additionalCfgs, nil
}

// getRepoConfigPath returns the standard OS specific path for repository configuration files
func getRepoConfigPath(os string) (string, error) {
	if path, ok := RepoConfigPathMap[os]; ok {
		return path, nil
	}
	return "", fmt.Errorf("distribution %s not supported", os)
}

// getCertConfigPath returns the standard OS specific path for ssl keys/certificates
func getCertConfigPath(os string) (string, error) {
	if path, ok := CertConfigPathMap[os]; ok {
		return path, nil
	}
	return "", fmt.Errorf("distribution %s not supported", os)
}

// getSubscriptionPathsToVolumeSources returns the MountPathToVolumeSource map containing all
// OS-specific subscription/entitlement paths that need to be mounted in the container.
func getSubscriptionPathsToVolumeSources(os string) (map[string]corev1.VolumeSource, error) {
	if pathToVolumeSource, ok := SubscriptionPathMap[os]; ok {
		return pathToVolumeSource, nil
	}
	return nil, fmt.Errorf("distribution %s not supported", os)
}
