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

package controllers

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"os"
	"path"
	"regexp"
	"sort"
	"strconv"
	"strings"

	"path/filepath"

	apiconfigv1 "github.com/openshift/api/config/v1"
	apiimagev1 "github.com/openshift/api/image/v1"
	secv1 "github.com/openshift/api/security/v1"
	promv1 "github.com/prometheus-operator/prometheus-operator/pkg/apis/monitoring/v1"
	"golang.org/x/mod/semver"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	nodev1 "k8s.io/api/node/v1"
	nodev1beta1 "k8s.io/api/node/v1beta1"
	apiextensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/intstr"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/yaml"

	gpuv1 "github.com/NVIDIA/gpu-operator/api/nvidia/v1"
	"github.com/NVIDIA/gpu-operator/internal/consts"
	"github.com/NVIDIA/gpu-operator/internal/utils"
)

const (
	// DefaultContainerdConfigFile indicates default config file path for containerd
	DefaultContainerdConfigFile = "/etc/containerd/config.toml"
	// DefaultContainerdDropInConfigFile indicates default drop-in config file path for containerd
	DefaultContainerdDropInConfigFile = "/etc/containerd/conf.d/99-nvidia.toml"
	// DefaultContainerdSocketFile indicates default containerd socket file
	DefaultContainerdSocketFile = "/run/containerd/containerd.sock"
	// DefaultDockerConfigFile indicates default config file path for docker
	DefaultDockerConfigFile = "/etc/docker/daemon.json"
	// DefaultDockerSocketFile indicates default docker socket file
	DefaultDockerSocketFile = "/var/run/docker.sock"
	// DefaultRuntimeNRISocketFile indicates the default container runtime NRI socket file
	DefaultRuntimeNRISocketFile = "/var/run/nri/nri.sock"
	// DefaultCRIOConfigFile indicates default config file path for cri-o. .
	DefaultCRIOConfigFile = "/etc/crio/config.toml"
	// DefaultCRIODropInConfigFile indicates the default path to the drop-in config file for cri-o
	DefaultCRIODropInConfigFile = "/etc/crio/crio.conf.d/99-nvidia.conf"
	// TrustedCAConfigMapName indicates configmap with custom user CA injected
	TrustedCAConfigMapName = "gpu-operator-trusted-ca"
	// TrustedCABundleFileName indicates custom user ca certificate filename
	TrustedCABundleFileName = "ca-bundle.crt"
	// TrustedCABundleMountDir indicates target mount directory of user ca bundle
	TrustedCABundleMountDir = "/etc/pki/ca-trust/extracted/pem"
	// TrustedCACertificate indicates injected CA certificate name
	TrustedCACertificate = "tls-ca-bundle.pem"
	// DefaultRuntimeClass represents "nvidia" RuntimeClass
	DefaultRuntimeClass = "nvidia"
	// DriverInstallPathVolName represents volume name for driver install path provided to toolkit
	DriverInstallPathVolName = "driver-install-path"
	// DefaultRuntimeNRISocketTargetDir represents target directory where runtime NRI socket directory will be mounted
	DefaultRuntimeNRISocketTargetDir = "/runtime/nri-sock-dir/"
	// DefaultRuntimeSocketTargetDir represents target directory where runtime socket directory will be mounted
	DefaultRuntimeSocketTargetDir = "/runtime/sock-dir/"
	// DefaultRuntimeConfigTargetDir represents target directory where runtime socket directory will be mounted
	DefaultRuntimeConfigTargetDir = "/runtime/config-dir/"
	// DefaultRuntimeDropInConfigTargetDir represents target directory where drop-in config directory will be mounted
	DefaultRuntimeDropInConfigTargetDir = "/runtime/config-dir.d/"
	// ValidatorImageEnvName indicates env name for validator image passed
	ValidatorImageEnvName = "VALIDATOR_IMAGE"
	// ValidatorImagePullPolicyEnvName indicates env name for validator image pull policy passed
	ValidatorImagePullPolicyEnvName = "VALIDATOR_IMAGE_PULL_POLICY"
	// ValidatorImagePullSecretsEnvName indicates env name for validator image pull secrets passed
	ValidatorImagePullSecretsEnvName = "VALIDATOR_IMAGE_PULL_SECRETS"
	// ValidatorRuntimeClassEnvName indicates env name of runtime class to be applied to validator pods
	ValidatorRuntimeClassEnvName = "VALIDATOR_RUNTIME_CLASS"
	// MigStrategyEnvName indicates env name for passing MIG strategy
	MigStrategyEnvName = "MIG_STRATEGY"
	// MigPartedDefaultConfigMapName indicates name of ConfigMap containing default mig-parted config
	MigPartedDefaultConfigMapName = "default-mig-parted-config"
	// MigDefaultGPUClientsConfigMapName indicates name of ConfigMap containing default gpu-clients
	MigDefaultGPUClientsConfigMapName = "default-gpu-clients"
	// DCGMRemoteEngineEnvName indicates env name to specify remote DCGM host engine ip:port
	DCGMRemoteEngineEnvName = "DCGM_REMOTE_HOSTENGINE_INFO"
	// DCGMDefaultPort indicates default port bound to DCGM host engine
	DCGMDefaultPort = 5555
	// GPUDirectRDMAEnabledEnvName indicates if GPU direct RDMA is enabled through GPU operator
	GPUDirectRDMAEnabledEnvName = "GPU_DIRECT_RDMA_ENABLED"
	// UseHostMOFEDEnvName indicates if MOFED driver is pre-installed on the host
	UseHostMOFEDEnvName = "USE_HOST_MOFED"
	// MetricsConfigMountPath indicates mount path for custom dcgm metrics file
	MetricsConfigMountPath = "/etc/dcgm-exporter/" + MetricsConfigFileName
	// MetricsConfigFileName indicates custom dcgm metrics file name
	MetricsConfigFileName = "dcgm-metrics.csv"
	// NvidiaAnnotationHashKey indicates annotation name for last applied hash by gpu-operator
	NvidiaAnnotationHashKey = "nvidia.com/last-applied-hash"
	// NvidiaDisableRequireEnvName is the env name to disable default cuda constraints
	NvidiaDisableRequireEnvName = "NVIDIA_DISABLE_REQUIRE"
	// GDSEnabledEnvName is the env name to enable GDS support with device-plugin
	GDSEnabledEnvName = "GDS_ENABLED"
	// MOFEDEnabledEnvName is the env name to enable MOFED devices injection with device-plugin
	MOFEDEnabledEnvName = "MOFED_ENABLED"
	// GDRCopyEnabledEnvName is the envvar that enables injection of the GDRCopy device node with the device-plugin
	GDRCopyEnabledEnvName = "GDRCOPY_ENABLED"
	// ServiceMonitorCRDName is the name of the CRD defining the ServiceMonitor kind
	ServiceMonitorCRDName = "servicemonitors.monitoring.coreos.com"
	// DefaultToolkitInstallDir is the default toolkit installation directory on the host
	DefaultToolkitInstallDir = "/usr/local/nvidia"
	// ToolkitInstallDirEnvName is the name of the toolkit container env for configuring where NVIDIA Container Toolkit is installed
	ToolkitInstallDirEnvName = "ROOT"
	// VgpuDMDefaultConfigMapName indicates name of ConfigMap containing default vGPU devices configuration
	VgpuDMDefaultConfigMapName = "default-vgpu-devices-config"
	// VgpuDMDefaultConfigName indicates name of default configuration in the vGPU devices config file
	VgpuDMDefaultConfigName = "default"
	// NvidiaCtrRuntimeModeEnvName is the name of the toolkit container env for configuring the NVIDIA Container Runtime mode
	NvidiaCtrRuntimeModeEnvName = "NVIDIA_CONTAINER_RUNTIME_MODE"
	// NvidiaCtrRuntimeCDIPrefixesEnvName is the name of toolkit container env for configuring the CDI annotation prefixes
	NvidiaCtrRuntimeCDIPrefixesEnvName = "NVIDIA_CONTAINER_RUNTIME_MODES_CDI_ANNOTATION_PREFIXES"
	// CDIEnabledEnvName is the name of the envvar used to enable CDI in the operands
	CDIEnabledEnvName = "CDI_ENABLED"
	// NvidiaCDIHookPathEnvName is the name of the envvar specifying the path to the 'nvidia-cdi-hook' binary
	NvidiaCDIHookPathEnvName = "NVIDIA_CDI_HOOK_PATH"
	// CRIOConfigModeEnvName is the name of the envvar controlling how the toolkit container updates the cri-o configuration
	CRIOConfigModeEnvName = "CRIO_CONFIG_MODE"
	// CDIEnableNRIPlugin is the name of the env var for enabling NRI Plugin in the toolkit
	CDIEnableNRIPlugin = "ENABLE_NRI_PLUGIN"
	// DeviceListStrategyEnvName is the name of the envvar for configuring the device-list-strategy in the device-plugin
	DeviceListStrategyEnvName = "DEVICE_LIST_STRATEGY"
	// CDIAnnotationPrefixEnvName is the name of the device-plugin envvar for configuring the CDI annotation prefix
	CDIAnnotationPrefixEnvName = "CDI_ANNOTATION_PREFIX"
	// KataManagerAnnotationHashKey is the annotation indicating the hash of the kata-manager configuration
	KataManagerAnnotationHashKey = "nvidia.com/kata-manager.last-applied-hash"
	// DefaultKataArtifactsDir is the default directory to store kata artifacts on the host
	DefaultKataArtifactsDir = "/opt/nvidia-gpu-operator/artifacts/runtimeclasses/"
	// PodControllerRevisionHashLabelKey is the annotation key for pod controller revision hash value
	PodControllerRevisionHashLabelKey = "controller-revision-hash"
	// DefaultCCModeEnvName is the name of the envvar for configuring default CC mode on all compatible GPUs on the node
	DefaultCCModeEnvName = "DEFAULT_CC_MODE"
	// OpenKernelModulesEnabledEnvName is the name of the driver-container envvar for enabling open GPU kernel module support
	OpenKernelModulesEnabledEnvName = "OPEN_KERNEL_MODULES_ENABLED"
	// KernelModuleTypeEnvName is the name of the driver-container envvar to set the desired kernel module type
	KernelModuleTypeEnvName = "KERNEL_MODULE_TYPE"
	// MPSRootEnvName is the name of the envvar for configuring the MPS root
	MPSRootEnvName = "MPS_ROOT"
	// DefaultMPSRoot is the default MPS root path on the host
	DefaultMPSRoot = "/run/nvidia/mps"
	// HostRootEnvName is the name of the envvar representing the root path of the underlying host
	HostRootEnvName = "HOST_ROOT"
	// DefaultDriverInstallDir represents the default path of a driver container installation
	DefaultDriverInstallDir = "/run/nvidia/driver"
	// DriverInstallDirEnvName is the name of the envvar used by the driver-validator to represent the driver install dir
	DriverInstallDirEnvName = "DRIVER_INSTALL_DIR"
	// DriverInstallDirCtrPathEnvName is the name of the envvar used by the driver-validator to represent the path
	// of the driver install dir mounted in the container
	DriverInstallDirCtrPathEnvName = "DRIVER_INSTALL_DIR_CTR_PATH"
	// NvidiaRuntimeSetAsDefaultEnvName is the name of the toolkit container env for configuring NVIDIA Container Runtime as the default runtime
	NvidiaRuntimeSetAsDefaultEnvName = "NVIDIA_RUNTIME_SET_AS_DEFAULT"
	// NRIAnnotationDomain represents the domain name used for NRI annotations used for CDI device injections
	NRIAnnotationDomain = "nvidia.cdi.k8s.io"
)

// ContainerProbe defines container probe types
type ContainerProbe string

const (
	// Startup probe
	Startup ContainerProbe = "startup"
	// Liveness probe
	Liveness ContainerProbe = "liveness"
	// Readiness probe
	Readiness ContainerProbe = "readiness"
)

// rootUID represents user 0
var rootUID = ptr.To(int64(0))

// RepoConfigPathMap indicates standard OS specific paths for repository configuration files
var RepoConfigPathMap = map[string]string{
	"centos": "/etc/yum.repos.d",
	"debian": "/etc/apt/sources.list.d",
	"ubuntu": "/etc/apt/sources.list.d",
	"rhcos":  "/etc/yum.repos.d",
	"rhel":   "/etc/yum.repos.d",
}

// CertConfigPathMap indicates standard OS specific paths for ssl keys/certificates.
// Where Go looks for certs: https://golang.org/src/crypto/x509/root_linux.go
// Where OCP mounts proxy certs on RHCOS nodes:
// https://access.redhat.com/documentation/en-us/openshift_container_platform/4.3/html/authentication/ocp-certificates#proxy-certificates_ocp-certificates
var CertConfigPathMap = map[string]string{
	"centos":   "/etc/pki/ca-trust/extracted/pem",
	"debian":   "/usr/local/share/ca-certificates",
	"ubuntu":   "/usr/local/share/ca-certificates",
	"rhcos":    "/etc/pki/ca-trust/extracted/pem",
	"rhel":     "/etc/pki/ca-trust/extracted/pem",
	"sles":     "/etc/pki/trust/anchors",
	"sl-micro": "/etc/pki/trust/anchors",
}

// MountPathToVolumeSource maps a container mount path to a VolumeSource
type MountPathToVolumeSource map[string]corev1.VolumeSource

// SubscriptionPathMap contains information on OS-specific paths
// that provide entitlements/subscription details on the host.
// These are used to enable Driver Container's access to packages controlled by
// the distro through their subscription and support program.
var SubscriptionPathMap = map[string](MountPathToVolumeSource){
	"rhel": {
		"/run/secrets/etc-pki-entitlement": corev1.VolumeSource{
			HostPath: &corev1.HostPathVolumeSource{
				Path: "/etc/pki/entitlement",
				Type: ptr.To(corev1.HostPathDirectory),
			},
		},
		"/run/secrets/redhat.repo": corev1.VolumeSource{
			HostPath: &corev1.HostPathVolumeSource{
				Path: "/etc/yum.repos.d/redhat.repo",
				Type: ptr.To(corev1.HostPathFile),
			},
		},
		"/run/secrets/rhsm": corev1.VolumeSource{
			HostPath: &corev1.HostPathVolumeSource{
				Path: "/etc/rhsm",
				Type: ptr.To(corev1.HostPathDirectory),
			},
		},
	},
	"rhcos": {
		"/run/secrets/etc-pki-entitlement": corev1.VolumeSource{
			HostPath: &corev1.HostPathVolumeSource{
				Path: "/etc/pki/entitlement",
				Type: ptr.To(corev1.HostPathDirectory),
			},
		},
		"/run/secrets/redhat.repo": corev1.VolumeSource{
			HostPath: &corev1.HostPathVolumeSource{
				Path: "/etc/yum.repos.d/redhat.repo",
				Type: ptr.To(corev1.HostPathFile),
			},
		},
		"/run/secrets/rhsm": corev1.VolumeSource{
			HostPath: &corev1.HostPathVolumeSource{
				Path: "/etc/rhsm",
				Type: ptr.To(corev1.HostPathDirectory),
			},
		},
	},
	"sles": {
		"/etc/zypp/credentials.d": corev1.VolumeSource{
			HostPath: &corev1.HostPathVolumeSource{
				Path: "/etc/zypp/credentials.d",
				Type: ptr.To(corev1.HostPathDirectory),
			},
		},
		"/etc/SUSEConnect": corev1.VolumeSource{
			HostPath: &corev1.HostPathVolumeSource{
				Path: "/etc/SUSEConnect",
				Type: ptr.To(corev1.HostPathFileOrCreate),
			},
		},
	},
	"sl-micro": {
		"/etc/zypp/credentials.d": corev1.VolumeSource{
			HostPath: &corev1.HostPathVolumeSource{
				Path: "/etc/zypp/credentials.d",
				Type: ptr.To(corev1.HostPathDirectory),
			},
		},
		"/etc/SUSEConnect": corev1.VolumeSource{
			HostPath: &corev1.HostPathVolumeSource{
				Path: "/etc/SUSEConnect",
				Type: ptr.To(corev1.HostPathFileOrCreate),
			},
		},
	},
}

type controlFunc []func(n ClusterPolicyController) (gpuv1.State, error)

// ServiceAccount creates ServiceAccount resource
func ServiceAccount(n ClusterPolicyController) (gpuv1.State, error) {
	ctx := n.ctx
	state := n.idx
	obj := n.resources[state].ServiceAccount.DeepCopy()
	obj.Namespace = n.operatorNamespace

	logger := n.logger.WithValues("ServiceAccount", obj.Name, "Namespace", obj.Namespace)

	// Check if state is disabled and cleanup resource if exists
	if !n.isStateEnabled(n.stateNames[n.idx]) {
		err := n.client.Delete(ctx, obj)
		if err != nil && !apierrors.IsNotFound(err) {
			logger.Info("Couldn't delete", "Error", err)
			return gpuv1.NotReady, err
		}
		return gpuv1.Disabled, nil
	}

	if err := controllerutil.SetControllerReference(n.singleton, obj, n.scheme); err != nil {
		return gpuv1.NotReady, err
	}

	if err := n.client.Create(ctx, obj); err != nil {
		if apierrors.IsAlreadyExists(err) {
			logger.Info("Found Resource, skipping update")
			return gpuv1.Ready, nil
		}

		logger.Info("Couldn't create", "Error", err)
		return gpuv1.NotReady, err
	}
	return gpuv1.Ready, nil
}

// Role creates Role resource
func Role(n ClusterPolicyController) (gpuv1.State, error) {
	ctx := n.ctx
	state := n.idx
	obj := n.resources[state].Role.DeepCopy()
	obj.Namespace = n.operatorNamespace

	logger := n.logger.WithValues("Role", obj.Name, "Namespace", obj.Namespace)

	// Check if state is disabled and cleanup resource if exists
	if !n.isStateEnabled(n.stateNames[n.idx]) {
		err := n.client.Delete(ctx, obj)
		if err != nil && !apierrors.IsNotFound(err) {
			logger.Info("Couldn't delete", "Error", err)
			return gpuv1.NotReady, err
		}
		return gpuv1.Disabled, nil
	}

	if err := controllerutil.SetControllerReference(n.singleton, obj, n.scheme); err != nil {
		return gpuv1.NotReady, err
	}

	if err := n.client.Create(ctx, obj); err != nil {
		if apierrors.IsAlreadyExists(err) {
			logger.Info("Found Resource, updating...")
			err = n.client.Update(ctx, obj)
			if err != nil {
				logger.Info("Couldn't update", "Error", err)
				return gpuv1.NotReady, err
			}
			return gpuv1.Ready, nil
		}

		logger.Info("Couldn't create", "Error", err)
		return gpuv1.NotReady, err
	}

	return gpuv1.Ready, nil
}

// RoleBinding creates RoleBinding resource
func RoleBinding(n ClusterPolicyController) (gpuv1.State, error) {
	ctx := n.ctx
	state := n.idx
	obj := n.resources[state].RoleBinding.DeepCopy()
	obj.Namespace = n.operatorNamespace

	logger := n.logger.WithValues("RoleBinding", obj.Name, "Namespace", obj.Namespace)

	// Check if state is disabled and cleanup resource if exists
	if !n.isStateEnabled(n.stateNames[n.idx]) {
		err := n.client.Delete(ctx, obj)
		if err != nil && !apierrors.IsNotFound(err) {
			logger.Info("Couldn't delete", "Error", err)
			return gpuv1.NotReady, err
		}
		return gpuv1.Disabled, nil
	}

	for idx := range obj.Subjects {
		// we don't want to update ALL the Subjects[].Namespace, eg we need to keep 'openshift-monitoring'
		// for allowing PrometheusOperator to scrape our metrics resources:
		// see in assets/state-dcgm-exporter, 0500_prom_rolebinding_openshift.yaml vs 0300_rolebinding.yaml
		if obj.Subjects[idx].Namespace != "FILLED BY THE OPERATOR" {
			continue
		}
		obj.Subjects[idx].Namespace = n.operatorNamespace
	}

	if err := controllerutil.SetControllerReference(n.singleton, obj, n.scheme); err != nil {
		return gpuv1.NotReady, err
	}

	if err := n.client.Create(ctx, obj); err != nil {
		if apierrors.IsAlreadyExists(err) {
			logger.Info("Found Resource, updating...")
			err = n.client.Update(ctx, obj)
			if err != nil {
				logger.Info("Couldn't update", "Error", err)
				return gpuv1.NotReady, err
			}
			return gpuv1.Ready, nil
		}

		logger.Info("Couldn't create", "Error", err)
		return gpuv1.NotReady, err
	}

	return gpuv1.Ready, nil
}

// ClusterRole creates ClusterRole resource
func ClusterRole(n ClusterPolicyController) (gpuv1.State, error) {
	ctx := n.ctx
	state := n.idx
	obj := n.resources[state].ClusterRole.DeepCopy()
	obj.Namespace = n.operatorNamespace

	logger := n.logger.WithValues("ClusterRole", obj.Name, "Namespace", obj.Namespace)

	// Check if state is disabled and cleanup resource if exists
	if !n.isStateEnabled(n.stateNames[n.idx]) {
		err := n.client.Delete(ctx, obj)
		if err != nil && !apierrors.IsNotFound(err) {
			logger.Info("Couldn't delete", "Error", err)
			return gpuv1.NotReady, err
		}
		return gpuv1.Disabled, nil
	}

	if err := controllerutil.SetControllerReference(n.singleton, obj, n.scheme); err != nil {
		return gpuv1.NotReady, err
	}

	if err := n.client.Create(ctx, obj); err != nil {
		if apierrors.IsAlreadyExists(err) {
			logger.Info("Found Resource, updating...")
			err = n.client.Update(ctx, obj)
			if err != nil {
				logger.Info("Couldn't update", "Error", err)
				return gpuv1.NotReady, err
			}
			return gpuv1.Ready, nil
		}

		logger.Info("Couldn't create", "Error", err)
		return gpuv1.NotReady, err
	}

	return gpuv1.Ready, nil
}

// ClusterRoleBinding creates ClusterRoleBinding resource
func ClusterRoleBinding(n ClusterPolicyController) (gpuv1.State, error) {
	ctx := n.ctx
	state := n.idx
	obj := n.resources[state].ClusterRoleBinding.DeepCopy()
	obj.Namespace = n.operatorNamespace

	logger := n.logger.WithValues("ClusterRoleBinding", obj.Name, "Namespace", obj.Namespace)

	// Check if state is disabled and cleanup resource if exists
	if !n.isStateEnabled(n.stateNames[n.idx]) {
		err := n.client.Delete(ctx, obj)
		if err != nil && !apierrors.IsNotFound(err) {
			logger.Info("Couldn't delete", "Error", err)
			return gpuv1.NotReady, err
		}
		return gpuv1.Disabled, nil
	}

	for idx := range obj.Subjects {
		obj.Subjects[idx].Namespace = n.operatorNamespace
	}

	if err := controllerutil.SetControllerReference(n.singleton, obj, n.scheme); err != nil {
		return gpuv1.NotReady, err
	}

	if err := n.client.Create(ctx, obj); err != nil {
		if apierrors.IsAlreadyExists(err) {
			logger.Info("Found Resource, updating...")
			err = n.client.Update(ctx, obj)
			if err != nil {
				logger.Info("Couldn't update", "Error", err)
				return gpuv1.NotReady, err
			}
			return gpuv1.Ready, nil
		}

		logger.Info("Couldn't create", "Error", err)
		return gpuv1.NotReady, err
	}

	return gpuv1.Ready, nil
}

// createConfigMap creates a ConfigMap resource
func createConfigMap(n ClusterPolicyController, configMapIdx int) (gpuv1.State, error) {
	ctx := n.ctx
	state := n.idx
	config := n.singleton.Spec
	obj := n.resources[state].ConfigMaps[configMapIdx].DeepCopy()
	obj.Namespace = n.operatorNamespace

	logger := n.logger.WithValues("ConfigMap", obj.Name, "Namespace", obj.Namespace)

	// Check if state is disabled and cleanup resource if exists
	if !n.isStateEnabled(n.stateNames[n.idx]) {
		err := n.client.Delete(ctx, obj)
		if err != nil && !apierrors.IsNotFound(err) {
			logger.Info("Couldn't delete", "Error", err)
			return gpuv1.NotReady, err
		}
		return gpuv1.Disabled, nil
	}

	// avoid creating default 'mig-parted-config' ConfigMap if custom one is provided
	if obj.Name == MigPartedDefaultConfigMapName {
		if name, isCustom := gpuv1.GetConfigMapName(config.MIGManager.Config, MigPartedDefaultConfigMapName); isCustom {
			logger.Info("Not creating resource, custom ConfigMap provided", "Name", name)
			return gpuv1.Ready, nil
		}
	}

	// avoid creating default 'gpu-clients' ConfigMap if custom one is provided
	if obj.Name == MigDefaultGPUClientsConfigMapName {
		if name, isCustom := gpuv1.GetConfigMapName(config.MIGManager.GPUClientsConfig, MigDefaultGPUClientsConfigMapName); isCustom {
			logger.Info("Not creating resource, custom ConfigMap provided", "Name", name)
			return gpuv1.Ready, nil
		}
	}

	// avoid creating default vGPU device manager ConfigMap if custom one provided
	if obj.Name == VgpuDMDefaultConfigMapName {
		if name, isCustom := gpuv1.GetConfigMapName(config.VGPUDeviceManager.Config, VgpuDMDefaultConfigMapName); isCustom {
			logger.Info("Not creating resource, custom ConfigMap provided", "Name", name)
			return gpuv1.Ready, nil
		}
	}

	if obj.Name == "nvidia-kata-manager-config" {
		data, err := yaml.Marshal(config.KataManager.Config)
		if err != nil {
			return gpuv1.NotReady, fmt.Errorf("failed to marshal kata manager config: %v", err)
		}
		obj.Data = map[string]string{
			"config.yaml": string(data),
		}
	}

	if err := controllerutil.SetControllerReference(n.singleton, obj, n.scheme); err != nil {
		return gpuv1.NotReady, err
	}

	if err := n.client.Create(ctx, obj); err != nil {
		if !apierrors.IsAlreadyExists(err) {
			logger.Info("Couldn't create", "Error", err)
			return gpuv1.NotReady, err
		}

		logger.Info("Found Resource, updating...")
		err = n.client.Update(ctx, obj)
		if err != nil {
			logger.Info("Couldn't update", "Error", err)
			return gpuv1.NotReady, err
		}
	}

	return gpuv1.Ready, nil
}

// ConfigMaps creates ConfigMap resource(s)
func ConfigMaps(n ClusterPolicyController) (gpuv1.State, error) {
	status := gpuv1.Ready
	state := n.idx
	for i := range n.resources[state].ConfigMaps {
		stat, err := createConfigMap(n, i)
		if err != nil {
			return stat, err
		}
		if stat != gpuv1.Ready {
			status = gpuv1.NotReady
		}
	}
	return status, nil
}

// getKernelVersionsMap returns a map of kernel versions to their corresponding OS from all GPU nodes in the cluster
func (n ClusterPolicyController) getKernelVersionsMap() (map[string]string, error) {
	kernelVersionMap := make(map[string]string)
	ctx := n.ctx
	logger := n.logger.WithValues("Request.Namespace", "default", "Request.Name", "Node")

	// Filter only GPU nodes
	opts := []client.ListOption{
		client.MatchingLabels{"nvidia.com/gpu.present": "true"},
	}

	list := &corev1.NodeList{}
	err := n.client.List(ctx, list, opts...)
	if err != nil {
		logger.Info("Could not get NodeList", "ERROR", err)
		return nil, err
	}

	if len(list.Items) == 0 {
		// none of the nodes matched nvidia GPU label
		// either the nodes do not have GPUs, or NFD is not running
		logger.Info("Could not get any nodes to match nvidia.com/gpu.present label")
		return nil, nil
	}

	for _, node := range list.Items {
		labels := node.GetLabels()
		if kernelVersion, ok := labels[nfdKernelLabelKey]; ok {
			logger.Info("Found kernel version label", "version", kernelVersion)
			// get OS version for this kernel
			osType := labels[nfdOSReleaseIDLabelKey]
			osVersion := labels[nfdOSVersionIDLabelKey]
			nodeOS := fmt.Sprintf("%s%s", osType, osVersion)
			if os, ok := kernelVersionMap[kernelVersion]; ok {
				if os != nodeOS {
					return nil, fmt.Errorf("different OS versions found for the same kernel version %s, unsupported configuration", kernelVersion)
				}
			}
			// add mapping for "kernelVersion" --> "OS"
			kernelVersionMap[kernelVersion] = nodeOS
		} else {
			err := apierrors.NewNotFound(schema.GroupResource{Group: "Node", Resource: "Label"}, nfdKernelLabelKey)
			logger.Error(err, "Failed to get kernel version of GPU node using Node Feature Discovery (NFD) labels. Is NFD installed in the cluster?")
			return nil, err
		}
	}

	return kernelVersionMap, nil
}

func kernelFullVersion(n ClusterPolicyController) (string, string, string) {
	ctx := n.ctx
	logger := n.logger.WithValues("Request.Namespace", "default", "Request.Name", "Node")
	// We need the node labels to fetch the correct container
	opts := []client.ListOption{
		client.MatchingLabels{"nvidia.com/gpu.present": "true"},
	}

	list := &corev1.NodeList{}
	err := n.client.List(ctx, list, opts...)
	if err != nil {
		logger.Info("Could not get NodeList", "ERROR", err)
		return "", "", ""
	}

	if len(list.Items) == 0 {
		// none of the nodes matched nvidia GPU label
		// either the nodes do not have GPUs, or NFD is not running
		logger.Info("Could not get any nodes to match nvidia.com/gpu.present label", "ERROR", "")
		return "", "", ""
	}

	// Assuming all nodes are running the same kernel version,
	// One could easily add driver-kernel-versions for each node.
	node := list.Items[0]
	labels := node.GetLabels()

	var ok bool
	kFVersion, ok := labels[nfdKernelLabelKey]
	if ok {
		logger.Info(kFVersion)
	} else {
		err := apierrors.NewNotFound(schema.GroupResource{Group: "Node", Resource: "Label"}, nfdKernelLabelKey)
		logger.Info("Couldn't get kernelVersion, did you run the node feature discovery?", "Error", err)
		return "", "", ""
	}

	osName, ok := labels[nfdOSReleaseIDLabelKey]
	if !ok {
		return kFVersion, "", ""
	}
	osVersion, ok := labels[nfdOSVersionIDLabelKey]
	if !ok {
		return kFVersion, "", ""
	}
	osTag := fmt.Sprintf("%s%s", osName, osVersion)

	return kFVersion, osTag, osVersion
}

func preprocessService(obj *corev1.Service, n ClusterPolicyController) error {
	logger := n.logger.WithValues("Service", obj.Name)
	transformations := map[string]func(*corev1.Service, *gpuv1.ClusterPolicySpec) error{
		"nvidia-dcgm-exporter": TransformDCGMExporterService,
	}

	t, ok := transformations[obj.Name]
	if !ok {
		logger.V(2).Info(fmt.Sprintf("No transformation for Service '%s'", obj.Name))
		return nil
	}

	err := t(obj, &n.singleton.Spec)
	if err != nil {
		logger.Error(err, "Failed to apply transformation", "Service", obj.Name)
		return err
	}

	return nil
}

func preProcessDaemonSet(obj *appsv1.DaemonSet, n ClusterPolicyController) error {
	logger := n.logger.WithValues("Daemonset", obj.Name)
	transformations := map[string]func(*appsv1.DaemonSet, *gpuv1.ClusterPolicySpec, ClusterPolicyController) error{
		"nvidia-driver-daemonset":                 TransformDriver,
		"nvidia-vgpu-manager-daemonset":           TransformVGPUManager,
		"nvidia-vgpu-device-manager":              TransformVGPUDeviceManager,
		"nvidia-vfio-manager":                     TransformVFIOManager,
		"nvidia-container-toolkit-daemonset":      TransformToolkit,
		"nvidia-device-plugin-daemonset":          TransformDevicePlugin,
		"nvidia-device-plugin-mps-control-daemon": TransformMPSControlDaemon,
		"nvidia-sandbox-device-plugin-daemonset":  TransformSandboxDevicePlugin,
		"nvidia-dcgm":                             TransformDCGM,
		"nvidia-dcgm-exporter":                    TransformDCGMExporter,
		"nvidia-node-status-exporter":             TransformNodeStatusExporter,
		"gpu-feature-discovery":                   TransformGPUDiscoveryPlugin,
		"nvidia-mig-manager":                      TransformMIGManager,
		"nvidia-operator-validator":               TransformValidator,
		"nvidia-sandbox-validator":                TransformSandboxValidator,
		"nvidia-kata-manager":                     TransformKataManager,
		"nvidia-cc-manager":                       TransformCCManager,
	}

	t, ok := transformations[obj.Name]
	if !ok {
		logger.Info(fmt.Sprintf("No transformation for Daemonset '%s'", obj.Name))
		return nil
	}

	// apply common Daemonset configuration that is applicable to all
	err := applyCommonDaemonsetConfig(obj, &n.singleton.Spec)
	if err != nil {
		logger.Error(err, "Failed to apply common Daemonset transformation", "resource", obj.Name)
		return err
	}

	// transform the host-root and host-dev-char volumes if a custom host root is configured with the operator
	transformForHostRoot(obj, n.singleton.Spec.HostPaths.RootFS)

	// transform the driver-root volume if a custom driver install dir is configured with the operator
	transformForDriverInstallDir(obj, n.singleton.Spec.HostPaths.DriverInstallDir)

	// apply per operand Daemonset config
	err = t(obj, &n.singleton.Spec, n)
	if err != nil {
		logger.Error(err, "Failed to apply transformation", "resource", obj.Name)
		return err
	}

	// apply custom Labels and Annotations to the podSpec if any
	applyCommonDaemonsetMetadata(obj, &n.singleton.Spec.Daemonsets)

	return nil
}

// applyCommonDaemonsetMetadata adds additional labels and annotations to the daemonset podSpec if there are any specified
// by the user in the podSpec.
func applyCommonDaemonsetMetadata(obj *appsv1.DaemonSet, dsSpec *gpuv1.DaemonsetsSpec) {
	if len(dsSpec.Labels) > 0 {
		if obj.Spec.Template.Labels == nil {
			obj.Spec.Template.Labels = make(map[string]string)
		}
		for labelKey, labelValue := range dsSpec.Labels {
			// if the user specifies an override of the "app" or the "app.kubernetes.io/part-of" key, we skip it.
			// DaemonSet pod selectors are immutable, so we still want the pods to be selectable as before and working
			// with the existing daemon set selectors.
			if labelKey == "app" || labelKey == "app.kubernetes.io/part-of" {
				continue
			}
			obj.Spec.Template.Labels[labelKey] = labelValue
		}
	}

	if len(dsSpec.Annotations) > 0 {
		if obj.Spec.Template.Annotations == nil {
			obj.Spec.Template.Annotations = make(map[string]string)
		}
		for annoKey, annoVal := range dsSpec.Annotations {
			obj.Spec.Template.Annotations[annoKey] = annoVal
		}
	}
}

// Apply common config that is applicable for all Daemonsets
func applyCommonDaemonsetConfig(obj *appsv1.DaemonSet, config *gpuv1.ClusterPolicySpec) error {
	// apply daemonset update strategy
	err := applyUpdateStrategyConfig(obj, config)
	if err != nil {
		return err
	}

	// update PriorityClass
	if config.Daemonsets.PriorityClassName != "" {
		obj.Spec.Template.Spec.PriorityClassName = config.Daemonsets.PriorityClassName
	}

	// set tolerations if specified
	if len(config.Daemonsets.Tolerations) > 0 {
		obj.Spec.Template.Spec.Tolerations = config.Daemonsets.Tolerations
	}

	// set pod-level security context if specified (applies as defaults to all containers in the pod)
	if config.Daemonsets.PodSecurityContext != nil {
		obj.Spec.Template.Spec.SecurityContext = config.Daemonsets.PodSecurityContext
	}
	return nil
}

// apply necessary transforms if a custom host root path is configured
func transformForHostRoot(obj *appsv1.DaemonSet, hostRoot string) {
	if hostRoot == "" || hostRoot == "/" {
		return
	}

	transformHostRootVolume(obj, hostRoot)
	transformHostDevCharVolume(obj, hostRoot)
}

func transformHostRootVolume(obj *appsv1.DaemonSet, hostRoot string) {
	containsHostRootVolume := false
	for _, volume := range obj.Spec.Template.Spec.Volumes {
		if volume.Name == "host-root" {
			volume.HostPath.Path = hostRoot
			containsHostRootVolume = true
			break
		}
	}

	if !containsHostRootVolume {
		return
	}

	for index := range obj.Spec.Template.Spec.InitContainers {
		setContainerEnv(&(obj.Spec.Template.Spec.InitContainers[index]), HostRootEnvName, hostRoot)
	}

	for index := range obj.Spec.Template.Spec.Containers {
		setContainerEnv(&(obj.Spec.Template.Spec.Containers[index]), HostRootEnvName, hostRoot)
	}
}

func transformHostDevCharVolume(obj *appsv1.DaemonSet, hostRoot string) {
	for _, volume := range obj.Spec.Template.Spec.Volumes {
		if volume.Name == "host-dev-char" {
			volume.HostPath.Path = filepath.Join(hostRoot, "/dev/char")
			break
		}
	}
}

// apply necessary transforms if a custom driver install directory is configured
func transformForDriverInstallDir(obj *appsv1.DaemonSet, driverInstallDir string) {
	if driverInstallDir == "" || driverInstallDir == DefaultDriverInstallDir {
		return
	}

	containsDriverInstallDirVolume := false
	podSpec := obj.Spec.Template.Spec
	for _, volume := range podSpec.Volumes {
		if volume.Name == "driver-install-dir" {
			volume.HostPath.Path = driverInstallDir
			containsDriverInstallDirVolume = true
			break
		}
	}

	if !containsDriverInstallDirVolume {
		return
	}

	if ctr := findContainerByName(podSpec.InitContainers, "driver-validation"); ctr != nil {
		setContainerEnv(ctr, DriverInstallDirEnvName, driverInstallDir)
		setContainerEnv(ctr, DriverInstallDirCtrPathEnvName, driverInstallDir)
		for i, volumeMount := range ctr.VolumeMounts {
			if volumeMount.Name == "driver-install-dir" {
				ctr.VolumeMounts[i].MountPath = driverInstallDir
			}
		}
	}
}

// TransformGPUDiscoveryPlugin transforms GPU discovery daemonset with required config as per ClusterPolicy
func TransformGPUDiscoveryPlugin(obj *appsv1.DaemonSet, config *gpuv1.ClusterPolicySpec, n ClusterPolicyController) error {
	// update validation container
	err := transformValidationInitContainer(obj, config)
	if err != nil {
		return err
	}

	// update image
	img, err := gpuv1.ImagePath(&config.GPUFeatureDiscovery)
	if err != nil {
		return err
	}
	obj.Spec.Template.Spec.Containers[0].Image = img

	// update image pull policy
	obj.Spec.Template.Spec.Containers[0].ImagePullPolicy = gpuv1.ImagePullPolicy(config.GPUFeatureDiscovery.ImagePullPolicy)

	// set image pull secrets
	if len(config.GPUFeatureDiscovery.ImagePullSecrets) > 0 {
		addPullSecrets(&obj.Spec.Template.Spec, config.GPUFeatureDiscovery.ImagePullSecrets)
	}

	// set resource limits
	if config.GPUFeatureDiscovery.Resources != nil {
		// apply resource limits to all containers
		for i := range obj.Spec.Template.Spec.Containers {
			obj.Spec.Template.Spec.Containers[i].Resources.Requests = config.GPUFeatureDiscovery.Resources.Requests
			obj.Spec.Template.Spec.Containers[i].Resources.Limits = config.GPUFeatureDiscovery.Resources.Limits
		}
	}

	// set arguments if specified for driver container
	if len(config.GPUFeatureDiscovery.Args) > 0 {
		obj.Spec.Template.Spec.Containers[0].Args = config.GPUFeatureDiscovery.Args
	}

	// set/append environment variables for exporter container
	if len(config.GPUFeatureDiscovery.Env) > 0 {
		for _, env := range config.GPUFeatureDiscovery.Env {
			setContainerEnv(&(obj.Spec.Template.Spec.Containers[0]), env.Name, env.Value)
		}
	}

	// apply plugin configuration through ConfigMap if one is provided
	err = handleDevicePluginConfig(obj, config)
	if err != nil {
		return err
	}

	setRuntimeClassName(&obj.Spec.Template.Spec, config, n.runtime)
	setNRIPluginAnnotation(&obj.Spec.Template.ObjectMeta, &config.CDI, obj.Spec.Template.Spec.Containers[0].Name)

	// update env required for MIG support
	applyMIGConfiguration(&(obj.Spec.Template.Spec.Containers[0]), config.MIG.Strategy)

	return nil
}

func setNRIPluginAnnotation(o *metav1.ObjectMeta, cdiConfig *gpuv1.CDIConfigSpec, containerName string) {
	const (
		managementCDIDevice = "management.nvidia.com/gpu=all"
	)

	if !cdiConfig.IsNRIPluginEnabled() {
		return
	}
	annotations := o.Annotations
	if len(annotations) == 0 {
		annotations = make(map[string]string)
	}
	annotationKey := fmt.Sprintf("%s/container.%s", NRIAnnotationDomain, containerName)
	annotations[annotationKey] = managementCDIDevice
	o.Annotations = annotations
}

// parseOSRelease can be overridden in tests for mocking filesystem access.
// In production, it reads and parses /host-etc/os-release.
var parseOSRelease = parseOSReleaseFromFile

// osReleaseFilePath is the path to the os-release file, configurable for testing.
var osReleaseFilePath = "/host-etc/os-release"

// parseOSReleaseFromFile reads and parses the os-release file from the host filesystem.
func parseOSReleaseFromFile() (map[string]string, error) {
	release := map[string]string{}

	f, err := os.Open(osReleaseFilePath)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	re := regexp.MustCompile(`^(?P<key>\w+)=(?P<value>.+)`)

	// Read line-by-line
	s := bufio.NewScanner(f)
	for s.Scan() {
		line := s.Text()
		if m := re.FindStringSubmatch(line); m != nil {
			release[m[1]] = strings.Trim(m[2], `"`)
		}
	}
	return release, nil
}

func TransformDCGMExporterService(obj *corev1.Service, config *gpuv1.ClusterPolicySpec) error {
	serviceConfig := config.DCGMExporter.ServiceSpec
	if serviceConfig != nil {
		if len(serviceConfig.Type) > 0 {
			obj.Spec.Type = serviceConfig.Type
		}

		if serviceConfig.InternalTrafficPolicy != nil {
			obj.Spec.InternalTrafficPolicy = serviceConfig.InternalTrafficPolicy
		}
	}
	return nil
}

// TransformDriver transforms Nvidia driver daemonset with required config as per ClusterPolicy
func TransformDriver(obj *appsv1.DaemonSet, config *gpuv1.ClusterPolicySpec, n ClusterPolicyController) error {
	// update driver-manager initContainer
	err := transformDriverManagerInitContainer(obj, &config.Driver.Manager, config.Driver.GPUDirectRDMA)
	if err != nil {
		return err
	}

	// update nvidia-driver container
	err = transformDriverContainer(obj, config, n)
	if err != nil {
		return err
	}

	// update nvidia-peermem sidecar container
	err = transformPeerMemoryContainer(obj, config, n)
	if err != nil {
		return err
	}

	// update nvidia-fs sidecar container
	err = transformGDSContainer(obj, config, n)
	if err != nil {
		return err
	}

	// updated nvidia-gdrcopy sidecar container
	err = transformGDRCopyContainer(obj, config, n)
	if err != nil {
		return err
	}

	// update/remove OpenShift Driver Toolkit sidecar container
	err = transformOpenShiftDriverToolkitContainer(obj, config, n, "nvidia-driver-ctr")
	if err != nil {
		return fmt.Errorf("ERROR: failed to transform the Driver Toolkit Container: %s", err)
	}

	// updates for per kernel version pods using pre-compiled drivers
	if config.Driver.UsePrecompiledDrivers() {
		err = transformPrecompiledDriverDaemonset(obj, n)
		if err != nil {
			return fmt.Errorf("ERROR: failed to transform the pre-compiled Driver Daemonset: %s", err)
		}
	}

	// Compute driver configuration digest after all transformations are complete.
	// This digest enables fast-path driver installation by detecting when configuration
	// hasn't changed, avoiding unnecessary driver reinstalls and pod evictions.
	// Used by k8s-driver-manager to decide if driver cleanup is needed and by
	// nvidia-driver container to skip full reinstall for matching configurations.
	configDigest := utils.GetObjectHash(obj.Spec)

	// Set the computed digest in driver-manager initContainer
	driverManagerContainer := findContainerByName(obj.Spec.Template.Spec.InitContainers, "k8s-driver-manager")
	if driverManagerContainer != nil {
		setContainerEnv(driverManagerContainer, "DRIVER_CONFIG_DIGEST", configDigest)
	}

	// Set the computed digest in nvidia-driver container
	driverContainer := findContainerByName(obj.Spec.Template.Spec.Containers, "nvidia-driver-ctr")
	if driverContainer != nil {
		setContainerEnv(driverContainer, "DRIVER_CONFIG_DIGEST", configDigest)
	}

	// Used by dtk-build-driver to determine if fast path should be used (skip rebuild)
	driverToolkitContainer := findContainerByName(obj.Spec.Template.Spec.Containers, "openshift-driver-toolkit-ctr")
	if driverToolkitContainer != nil {
		setContainerEnv(driverToolkitContainer, "DRIVER_CONFIG_DIGEST", configDigest)
	}

	return nil
}

// TransformVGPUManager transforms NVIDIA vGPU Manager daemonset with required config as per ClusterPolicy
func TransformVGPUManager(obj *appsv1.DaemonSet, config *gpuv1.ClusterPolicySpec, n ClusterPolicyController) error {
	// update k8s-driver-manager initContainer
	err := transformDriverManagerInitContainer(obj, &config.VGPUManager.DriverManager, nil)
	if err != nil {
		return fmt.Errorf("failed to transform k8s-driver-manager initContainer for vGPU Manager: %v", err)
	}

	// update nvidia-vgpu-manager container
	err = transformVGPUManagerContainer(obj, config, n)
	if err != nil {
		return fmt.Errorf("failed to transform vGPU Manager container: %v", err)
	}

	// update OpenShift Driver Toolkit sidecar container
	err = transformOpenShiftDriverToolkitContainer(obj, config, n, "nvidia-vgpu-manager-ctr")
	if err != nil {
		return fmt.Errorf("failed to transform the Driver Toolkit container: %s", err)
	}

	return nil
}

// applyOCPProxySpec applies proxy settings to podSpec
func applyOCPProxySpec(n ClusterPolicyController, podSpec *corev1.PodSpec) error {
	// Pass HTTPS_PROXY, HTTP_PROXY and NO_PROXY env if set in clusterwide proxy for OCP
	proxy, err := GetClusterWideProxy(n.ctx)
	if err != nil {
		return fmt.Errorf("ERROR: failed to get clusterwide proxy object: %s", err)
	}

	if proxy == nil {
		// no clusterwide proxy configured
		return nil
	}

	for i, container := range podSpec.Containers {
		// skip if not nvidia-driver container
		if !strings.Contains(container.Name, "nvidia-driver") {
			continue
		}

		proxyEnv := getProxyEnv(proxy)
		if len(proxyEnv) != 0 {
			podSpec.Containers[i].Env = append(podSpec.Containers[i].Env, proxyEnv...)
		}

		// if user-ca-bundle is setup in proxy,  create a trusted-ca configmap and add volume mount
		if proxy.Spec.TrustedCA.Name == "" {
			return nil
		}

		// create trusted-ca configmap to inject custom user ca bundle into it
		_, err = getOrCreateTrustedCAConfigMap(n, TrustedCAConfigMapName)
		if err != nil {
			return err
		}

		// mount trusted-ca configmap
		podSpec.Containers[i].VolumeMounts = append(podSpec.Containers[i].VolumeMounts,
			corev1.VolumeMount{
				Name:      TrustedCAConfigMapName,
				ReadOnly:  true,
				MountPath: TrustedCABundleMountDir,
			})
		podSpec.Volumes = append(podSpec.Volumes,
			corev1.Volume{
				Name: TrustedCAConfigMapName,
				VolumeSource: corev1.VolumeSource{
					ConfigMap: &corev1.ConfigMapVolumeSource{
						LocalObjectReference: corev1.LocalObjectReference{
							Name: TrustedCAConfigMapName,
						},
						Items: []corev1.KeyToPath{
							{
								Key:  TrustedCABundleFileName,
								Path: TrustedCACertificate,
							},
						},
					},
				},
			})
	}
	return nil
}

// getOrCreateTrustedCAConfigMap creates or returns an existing Trusted CA Bundle ConfigMap.
func getOrCreateTrustedCAConfigMap(n ClusterPolicyController, name string) (*corev1.ConfigMap, error) {
	ctx := n.ctx
	configMap := &corev1.ConfigMap{
		TypeMeta: metav1.TypeMeta{
			Kind:       "ConfigMap",
			APIVersion: corev1.SchemeGroupVersion.String(),
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: n.operatorNamespace,
		},
		Data: map[string]string{
			TrustedCABundleFileName: "",
		},
	}

	// apply label "config.openshift.io/inject-trusted-cabundle: true", so that cert is automatically filled/updated.
	configMap.Labels = make(map[string]string)
	configMap.Labels["config.openshift.io/inject-trusted-cabundle"] = "true"

	logger := n.logger.WithValues("ConfigMap", configMap.Name, "Namespace", configMap.Namespace)

	if err := controllerutil.SetControllerReference(n.singleton, configMap, n.scheme); err != nil {
		return nil, err
	}

	found := &corev1.ConfigMap{}
	err := n.client.Get(ctx, types.NamespacedName{Namespace: configMap.Namespace, Name: configMap.Name}, found)
	if err != nil && apierrors.IsNotFound(err) {
		logger.Info("Not found, creating")
		err = n.client.Create(ctx, configMap)
		if err != nil {
			logger.Info("Couldn't create")
			return nil, fmt.Errorf("failed to create trusted CA bundle config map %q: %s", name, err)
		}
		return configMap, nil
	} else if err != nil {
		return nil, fmt.Errorf("failed to get trusted CA bundle config map %q: %s", name, err)
	}

	return found, nil
}

// get proxy env variables from cluster wide proxy in OCP
func getProxyEnv(proxyConfig *apiconfigv1.Proxy) []corev1.EnvVar {
	envVars := []corev1.EnvVar{}
	if proxyConfig == nil {
		return envVars
	}
	proxies := map[string]string{
		"HTTPS_PROXY": proxyConfig.Spec.HTTPSProxy,
		"HTTP_PROXY":  proxyConfig.Spec.HTTPProxy,
		"NO_PROXY":    proxyConfig.Spec.NoProxy,
	}
	var envs []string
	for k := range proxies {
		envs = append(envs, k)
	}
	// ensure ordering is preserved when we add these env to pod spec
	sort.Strings(envs)

	for _, e := range envs {
		v := proxies[e]
		if len(v) == 0 {
			continue
		}
		upperCaseEnvvar := corev1.EnvVar{
			Name:  strings.ToUpper(e),
			Value: v,
		}
		lowerCaseEnvvar := corev1.EnvVar{
			Name:  strings.ToLower(e),
			Value: v,
		}
		envVars = append(envVars, upperCaseEnvvar, lowerCaseEnvvar)
	}

	return envVars
}

func transformToolkitCtrForCDI(container *corev1.Container, nriPluginEnabled bool) {
	// When CDI is enabled in GPU Operator, we leverage native CDI support in containerd / cri-o
	// to inject GPUs into workloads. We do not configure 'nvidia' as the default runtime. The
	// 'nvidia' runtime will be set as the runtime class for our management containers so that
	// they get access to all GPUs.
	//
	// Note: one could override this and continue to configure 'nvidia' as the default runtime
	// by directly setting the 'NVIDIA_RUNTIME_SET_AS_DEFAULT' environment variable to 'true' in
	// the toolkit container. One can leverage the 'toolkit.env' field in ClusterPolicy to
	// directly configure environment variables for the toolkit container.
	setContainerEnv(container, CDIEnabledEnvName, "true")
	setContainerEnv(container, NvidiaRuntimeSetAsDefaultEnvName, "false")
	setContainerEnv(container, NvidiaCtrRuntimeModeEnvName, "cdi")
	setContainerEnv(container, CRIOConfigModeEnvName, "config")

	if nriPluginEnabled {
		setContainerEnv(container, CDIEnableNRIPlugin, "true")
	}
}

// TransformToolkit transforms Nvidia container-toolkit daemonset with required config as per ClusterPolicy
func TransformToolkit(obj *appsv1.DaemonSet, config *gpuv1.ClusterPolicySpec, n ClusterPolicyController) error {
	toolkitContainerName := "nvidia-container-toolkit-ctr"
	toolkitMainContainer := findContainerByName(obj.Spec.Template.Spec.Containers, toolkitContainerName)
	if toolkitMainContainer == nil {
		return fmt.Errorf("failed to find toolkit container %q", toolkitContainerName)
	}

	// update validation container
	err := transformValidationInitContainer(obj, config)
	if err != nil {
		return err
	}
	// update image
	image, err := gpuv1.ImagePath(&config.Toolkit)
	if err != nil {
		return err
	}
	toolkitMainContainer.Image = image

	// update image pull policy
	toolkitMainContainer.ImagePullPolicy = gpuv1.ImagePullPolicy(config.Toolkit.ImagePullPolicy)

	// set image pull secrets
	if len(config.Toolkit.ImagePullSecrets) > 0 {
		addPullSecrets(&obj.Spec.Template.Spec, config.Toolkit.ImagePullSecrets)
	}

	// set resource limits
	if config.Toolkit.Resources != nil {
		// apply resource limits to all containers
		for i := range obj.Spec.Template.Spec.Containers {
			obj.Spec.Template.Spec.Containers[i].Resources.Requests = config.Toolkit.Resources.Requests
			obj.Spec.Template.Spec.Containers[i].Resources.Limits = config.Toolkit.Resources.Limits
		}
	}

	// update env required for CDI support
	if config.CDI.IsEnabled() {
		transformToolkitCtrForCDI(toolkitMainContainer, config.CDI.IsNRIPluginEnabled())
	} else if n.runtime == gpuv1.CRIO {
		// (cdesiniotis) When CDI is not enabled and cri-o is the container runtime,
		// we continue to install the OCI prestart hook as opposed to adding nvidia
		// runtime handlers to the cri-o configuration. Users can override this behavior
		// and have nvidia runtime handlers added to the cri-o configuration by setting
		// the 'CRIO_CONFIG_MODE' environment variable to 'config' in the toolkit container.
		// However, one should note setting 'CRIO_CONFIG_MODE' to 'config' in this case
		// (when CDI is not enabled) would result in the 'nvidia' runtime being set as
		// the default runtime. While this should work in theory, it is a significant
		// change -- which was the primary motivation to continue using the OCI prestart
		// hook by default in this case.
		setContainerEnv(toolkitMainContainer, CRIOConfigModeEnvName, "hook")
	}

	// set install directory for the toolkit
	if config.Toolkit.InstallDir != "" && config.Toolkit.InstallDir != DefaultToolkitInstallDir {
		setContainerEnv(toolkitMainContainer, ToolkitInstallDirEnvName, config.Toolkit.InstallDir)

		for i, volume := range obj.Spec.Template.Spec.Volumes {
			if volume.Name == "toolkit-install-dir" {
				obj.Spec.Template.Spec.Volumes[i].HostPath.Path = config.Toolkit.InstallDir
				break
			}
		}

		for i, volumeMount := range toolkitMainContainer.VolumeMounts {
			if volumeMount.Name == "toolkit-install-dir" {
				toolkitMainContainer.VolumeMounts[i].MountPath = config.Toolkit.InstallDir
				break
			}
		}
	}

	// Update CRI-O hooks path to use default path for non OCP cases
	if n.openshift == "" && n.runtime == gpuv1.CRIO {
		for index, volume := range obj.Spec.Template.Spec.Volumes {
			if volume.Name == "crio-hooks" {
				obj.Spec.Template.Spec.Volumes[index].HostPath.Path = "/usr/share/containers/oci/hooks.d"
			}
		}
	}

	if len(config.Toolkit.Env) > 0 {
		for _, env := range config.Toolkit.Env {
			setContainerEnv(toolkitMainContainer, env.Name, env.Value)
		}
	}

	// configure runtime
	runtime := n.runtime.String()
	err = transformForRuntime(obj, config, runtime, toolkitMainContainer)
	if err != nil {
		return fmt.Errorf("error transforming toolkit daemonset : %w", err)
	}

	return nil
}

func transformForRuntime(obj *appsv1.DaemonSet, config *gpuv1.ClusterPolicySpec, runtime string, container *corev1.Container) error {
	setContainerEnv(container, "RUNTIME", runtime)

	if runtime == gpuv1.Containerd.String() {
		// Set the runtime class name that is to be configured for containerd
		setContainerEnv(container, "CONTAINERD_RUNTIME_CLASS", getRuntimeClassName(config))
	}

	// For runtime config files we have top-level configs and drop-in files.
	// These are supported as follows:
	//   * Docker only supports top-level config files.
	//   * Containerd supports drop-in files, but required modification to the top-level config
	//   * Crio supports drop-in files at a predefined location. The top-level config may be read
	//     but should not be updated.

	// setup mounts for runtime config file
	topLevelConfigFile, dropInConfigFile, err := getRuntimeConfigFiles(container, runtime)
	if err != nil {
		return fmt.Errorf("error getting path to runtime config file: %w", err)
	}

	var configEnvvarName string
	switch runtime {
	case gpuv1.Containerd.String():
		configEnvvarName = "CONTAINERD_CONFIG"
	case gpuv1.Docker.String():
		configEnvvarName = "DOCKER_CONFIG"
	case gpuv1.CRIO.String():
		configEnvvarName = "CRIO_CONFIG"
	}

	// Handle the top-level configs
	if topLevelConfigFile != "" {
		sourceConfigFileName := path.Base(topLevelConfigFile)
		sourceConfigDir := path.Dir(topLevelConfigFile)
		containerConfigDir := DefaultRuntimeConfigTargetDir
		setContainerEnv(container, "RUNTIME_CONFIG", containerConfigDir+sourceConfigFileName)
		setContainerEnv(container, configEnvvarName, containerConfigDir+sourceConfigFileName)

		volMountConfigName := fmt.Sprintf("%s-config", runtime)
		volMountConfig := corev1.VolumeMount{Name: volMountConfigName, MountPath: containerConfigDir}
		container.VolumeMounts = append(container.VolumeMounts, volMountConfig)

		configVol := corev1.Volume{Name: volMountConfigName, VolumeSource: corev1.VolumeSource{HostPath: &corev1.HostPathVolumeSource{Path: sourceConfigDir, Type: ptr.To(corev1.HostPathDirectoryOrCreate)}}}
		obj.Spec.Template.Spec.Volumes = append(obj.Spec.Template.Spec.Volumes, configVol)
	}

	// Handle the drop-in configs
	// TODO: It's a bit of a hack to skip the `nvidia-kata-manager` container here.
	// Ideally if the two projects are using the SAME API then this should be
	// captured more rigorously.
	// Note that we probably want to implement drop-in file support in the
	// kata manager in any case -- in which case it will be good to use a
	// similar implementation.
	if dropInConfigFile != "" && container.Name != "nvidia-kata-manager" {
		sourceConfigFileName := path.Base(dropInConfigFile)
		sourceConfigDir := path.Dir(dropInConfigFile)
		containerConfigDir := DefaultRuntimeDropInConfigTargetDir
		setContainerEnv(container, "RUNTIME_DROP_IN_CONFIG", containerConfigDir+sourceConfigFileName)
		setContainerEnv(container, "RUNTIME_DROP_IN_CONFIG_HOST_PATH", dropInConfigFile)

		volMountConfigName := fmt.Sprintf("%s-drop-in-config", runtime)
		volMountConfig := corev1.VolumeMount{Name: volMountConfigName, MountPath: containerConfigDir}
		container.VolumeMounts = append(container.VolumeMounts, volMountConfig)

		configVol := corev1.Volume{Name: volMountConfigName, VolumeSource: corev1.VolumeSource{HostPath: &corev1.HostPathVolumeSource{Path: sourceConfigDir, Type: ptr.To(corev1.HostPathDirectoryOrCreate)}}}
		obj.Spec.Template.Spec.Volumes = append(obj.Spec.Template.Spec.Volumes, configVol)
	}

	// Handle any additional runtime config sources
	const runtimeConfigSourceFile = "file"
	if runtimeConfigSources := getContainerEnv(container, "RUNTIME_CONFIG_SOURCE"); runtimeConfigSources != "" {
		var sources []string
		for _, runtimeConfigSource := range strings.Split(runtimeConfigSources, ",") {
			parts := strings.SplitN(runtimeConfigSource, "=", 2)
			if len(parts) == 1 || parts[0] != runtimeConfigSourceFile {
				sources = append(sources, runtimeConfigSource)
				continue
			}
			// We transform the host path to a container path by prepending "/host" to the file
			// path. This works because the toolkit container has the host's root filesystem
			// mounted as read-only at "/host"
			sourceConfigFile := filepath.Join("/host", parts[1])
			sources = append(sources, runtimeConfigSourceFile+"="+sourceConfigFile)
		}
		setContainerEnv(container, "RUNTIME_CONFIG_SOURCE", strings.Join(sources, ","))
	}

	// setup mounts for runtime socket file
	runtimeSocketFile, err := getRuntimeSocketFile(container, runtime)
	if err != nil {
		return fmt.Errorf("error getting path to runtime socket: %w", err)
	}
	if runtimeSocketFile != "" {
		sourceSocketFileName := path.Base(runtimeSocketFile)
		// set envvar for runtime socket
		var socketEnvvarName string
		if runtime == gpuv1.Containerd.String() {
			socketEnvvarName = "CONTAINERD_SOCKET"
		} else if runtime == gpuv1.Docker.String() {
			socketEnvvarName = "DOCKER_SOCKET"
		}
		setContainerEnv(container, "RUNTIME_SOCKET", DefaultRuntimeSocketTargetDir+sourceSocketFileName)
		setContainerEnv(container, socketEnvvarName, DefaultRuntimeSocketTargetDir+sourceSocketFileName)

		volMountSocketName := fmt.Sprintf("%s-socket", runtime)
		volMountSocket := corev1.VolumeMount{Name: volMountSocketName, MountPath: DefaultRuntimeSocketTargetDir}
		container.VolumeMounts = append(container.VolumeMounts, volMountSocket)

		socketVol := corev1.Volume{Name: volMountSocketName, VolumeSource: corev1.VolumeSource{HostPath: &corev1.HostPathVolumeSource{Path: path.Dir(runtimeSocketFile)}}}
		obj.Spec.Template.Spec.Volumes = append(obj.Spec.Template.Spec.Volumes, socketVol)
	}

	if config.CDI.IsNRIPluginEnabled() {
		// setup mounts for the runtime NRI socket file
		nriSocketFile := getContainerEnv(container, "NRI_SOCKET")
		if nriSocketFile == "" {
			nriSocketFile = DefaultRuntimeNRISocketFile
		}
		setContainerEnv(container, "NRI_SOCKET", DefaultRuntimeNRISocketTargetDir+path.Base(nriSocketFile))

		nriVolMountSocketName := "nri-socket"
		nriVolMountSocket := corev1.VolumeMount{Name: nriVolMountSocketName, MountPath: DefaultRuntimeNRISocketTargetDir}
		container.VolumeMounts = append(container.VolumeMounts, nriVolMountSocket)

		nriSocketVol := corev1.Volume{Name: nriVolMountSocketName, VolumeSource: corev1.VolumeSource{HostPath: &corev1.HostPathVolumeSource{Path: path.Dir(nriSocketFile), Type: ptr.To(corev1.HostPathDirectoryOrCreate)}}}
		obj.Spec.Template.Spec.Volumes = append(obj.Spec.Template.Spec.Volumes, nriSocketVol)
	}

	return nil
}

func transformDevicePluginCtrForCDI(container *corev1.Container, config *gpuv1.ClusterPolicySpec) {
	setContainerEnv(container, CDIEnabledEnvName, "true")
	setContainerEnv(container, DeviceListStrategyEnvName, "cdi-annotations,cdi-cri")
	setContainerEnv(container, CDIAnnotationPrefixEnvName, "cdi.k8s.io/")

	if config.Toolkit.IsEnabled() {
		setContainerEnv(container, NvidiaCDIHookPathEnvName, filepath.Join(config.Toolkit.InstallDir, "toolkit/nvidia-cdi-hook"))
	}
}

// TransformDevicePlugin transforms k8s-device-plugin daemonset with required config as per ClusterPolicy
func TransformDevicePlugin(obj *appsv1.DaemonSet, config *gpuv1.ClusterPolicySpec, n ClusterPolicyController) error {
	devicePluginContainerName := "nvidia-device-plugin"
	devicePluginMainContainer := findContainerByName(obj.Spec.Template.Spec.Containers, devicePluginContainerName)
	if devicePluginMainContainer == nil {
		return fmt.Errorf("failed to find device plugin container %q", devicePluginContainerName)
	}

	// update validation container
	err := transformValidationInitContainer(obj, config)
	if err != nil {
		return err
	}

	// update image
	image, err := gpuv1.ImagePath(&config.DevicePlugin)
	if err != nil {
		return err
	}
	devicePluginMainContainer.Image = image

	// update image pull policy
	devicePluginMainContainer.ImagePullPolicy = gpuv1.ImagePullPolicy(config.DevicePlugin.ImagePullPolicy)

	// set image pull secrets
	if len(config.DevicePlugin.ImagePullSecrets) > 0 {
		addPullSecrets(&obj.Spec.Template.Spec, config.DevicePlugin.ImagePullSecrets)
	}

	// set resource limits
	if config.DevicePlugin.Resources != nil {
		// apply resource limits to all containers
		for i := range obj.Spec.Template.Spec.Containers {
			obj.Spec.Template.Spec.Containers[i].Resources.Requests = config.DevicePlugin.Resources.Requests
			obj.Spec.Template.Spec.Containers[i].Resources.Limits = config.DevicePlugin.Resources.Limits
		}
	}
	// set arguments if specified for device-plugin container
	if len(config.DevicePlugin.Args) > 0 {
		devicePluginMainContainer.Args = config.DevicePlugin.Args
	}

	// add env to allow injection of /dev/nvidia-fs and /dev/infiniband devices for GDS
	if config.GPUDirectStorage != nil && config.GPUDirectStorage.IsEnabled() {
		setContainerEnv(devicePluginMainContainer, GDSEnabledEnvName, "true")
		setContainerEnv(devicePluginMainContainer, MOFEDEnabledEnvName, "true")
	}

	if config.GDRCopy != nil && config.GDRCopy.IsEnabled() {
		setContainerEnv(devicePluginMainContainer, GDRCopyEnabledEnvName, "true")
	}

	// apply plugin configuration through ConfigMap if one is provided
	err = handleDevicePluginConfig(obj, config)
	if err != nil {
		return err
	}

	setRuntimeClassName(&obj.Spec.Template.Spec, config, n.runtime)
	setNRIPluginAnnotation(&obj.Spec.Template.ObjectMeta, &config.CDI, devicePluginContainerName)

	// update env required for MIG support
	applyMIGConfiguration(devicePluginMainContainer, config.MIG.Strategy)

	// update env required for CDI support
	if config.CDI.IsEnabled() {
		transformDevicePluginCtrForCDI(devicePluginMainContainer, config)
	}

	// update MPS volumes and set MPS_ROOT env var if a custom MPS root is configured
	if config.DevicePlugin.MPS != nil && config.DevicePlugin.MPS.Root != "" &&
		config.DevicePlugin.MPS.Root != DefaultMPSRoot {
		for i, volume := range obj.Spec.Template.Spec.Volumes {
			switch volume.Name {
			case "mps-root":
				obj.Spec.Template.Spec.Volumes[i].HostPath.Path = config.DevicePlugin.MPS.Root
			case "mps-shm":
				obj.Spec.Template.Spec.Volumes[i].HostPath.Path = filepath.Join(config.DevicePlugin.MPS.Root, "shm")
			}
		}
		setContainerEnv(devicePluginMainContainer, MPSRootEnvName, config.DevicePlugin.MPS.Root)
	}

	if len(config.DevicePlugin.Env) > 0 {
		for _, env := range config.DevicePlugin.Env {
			setContainerEnv(devicePluginMainContainer, env.Name, env.Value)
		}
	}

	return nil
}

func TransformMPSControlDaemon(obj *appsv1.DaemonSet, config *gpuv1.ClusterPolicySpec, n ClusterPolicyController) error {
	// update validation container
	err := transformValidationInitContainer(obj, config)
	if err != nil {
		return err
	}

	image, err := gpuv1.ImagePath(&config.DevicePlugin)
	if err != nil {
		return err
	}
	imagePullPolicy := gpuv1.ImagePullPolicy(config.DevicePlugin.ImagePullPolicy)

	// update image path and imagePullPolicy for 'mps-control-daemon-mounts' initContainer
	if initCtr := findContainerByName(obj.Spec.Template.Spec.InitContainers, "mps-control-daemon-mounts"); initCtr != nil {
		initCtr.Image = image
		initCtr.ImagePullPolicy = imagePullPolicy
	}

	// update image path and imagePullPolicy for main container
	mpsControlMainContainer := findContainerByName(obj.Spec.Template.Spec.Containers, "mps-control-daemon-ctr")
	if mpsControlMainContainer == nil {
		return fmt.Errorf("failed to find main container 'mps-control-daemon-ctr'")
	}
	mpsControlMainContainer.Image = image
	mpsControlMainContainer.ImagePullPolicy = imagePullPolicy

	// set image pull secrets
	if len(config.DevicePlugin.ImagePullSecrets) > 0 {
		addPullSecrets(&obj.Spec.Template.Spec, config.DevicePlugin.ImagePullSecrets)
	}

	// set resource limits
	if config.DevicePlugin.Resources != nil {
		// apply resource limits to all containers
		for i := range obj.Spec.Template.Spec.Containers {
			obj.Spec.Template.Spec.Containers[i].Resources.Requests = config.DevicePlugin.Resources.Requests
			obj.Spec.Template.Spec.Containers[i].Resources.Limits = config.DevicePlugin.Resources.Limits
		}
	}

	// apply plugin configuration through ConfigMap if one is provided
	err = handleDevicePluginConfig(obj, config)
	if err != nil {
		return err
	}

	setRuntimeClassName(&obj.Spec.Template.Spec, config, n.runtime)
	setNRIPluginAnnotation(&obj.Spec.Template.ObjectMeta, &config.CDI, mpsControlMainContainer.Name)

	// update env required for MIG support
	applyMIGConfiguration(mpsControlMainContainer, config.MIG.Strategy)

	// update MPS volumes if a custom MPS root is configured
	if config.DevicePlugin.MPS != nil && config.DevicePlugin.MPS.Root != "" &&
		config.DevicePlugin.MPS.Root != DefaultMPSRoot {
		for i, volume := range obj.Spec.Template.Spec.Volumes {
			switch volume.Name {
			case "mps-root":
				obj.Spec.Template.Spec.Volumes[i].HostPath.Path = config.DevicePlugin.MPS.Root
			case "mps-shm":
				obj.Spec.Template.Spec.Volumes[i].HostPath.Path = filepath.Join(config.DevicePlugin.MPS.Root, "shm")
			}
		}
	}

	return nil
}

// TransformSandboxDevicePlugin transforms sandbox-device-plugin daemonset with required config as per ClusterPolicy
func TransformSandboxDevicePlugin(obj *appsv1.DaemonSet, config *gpuv1.ClusterPolicySpec, n ClusterPolicyController) error {
	// update validation container
	err := transformValidationInitContainer(obj, config)
	if err != nil {
		return err
	}
	// update image
	image, err := gpuv1.ImagePath(&config.SandboxDevicePlugin)
	if err != nil {
		return err
	}
	obj.Spec.Template.Spec.Containers[0].Image = image

	// update image pull policy
	obj.Spec.Template.Spec.Containers[0].ImagePullPolicy = gpuv1.ImagePullPolicy(config.SandboxDevicePlugin.ImagePullPolicy)
	// set image pull secrets
	if len(config.SandboxDevicePlugin.ImagePullSecrets) > 0 {
		addPullSecrets(&obj.Spec.Template.Spec, config.SandboxDevicePlugin.ImagePullSecrets)
	}
	// set resource limits
	if config.SandboxDevicePlugin.Resources != nil {
		// apply resource limits to all containers
		for i := range obj.Spec.Template.Spec.Containers {
			obj.Spec.Template.Spec.Containers[i].Resources.Requests = config.SandboxDevicePlugin.Resources.Requests
			obj.Spec.Template.Spec.Containers[i].Resources.Limits = config.SandboxDevicePlugin.Resources.Limits
		}
	}
	// set arguments if specified for device-plugin container
	if len(config.SandboxDevicePlugin.Args) > 0 {
		obj.Spec.Template.Spec.Containers[0].Args = config.SandboxDevicePlugin.Args
	}
	// set/append environment variables for device-plugin container
	if len(config.SandboxDevicePlugin.Env) > 0 {
		for _, env := range config.SandboxDevicePlugin.Env {
			setContainerEnv(&(obj.Spec.Template.Spec.Containers[0]), env.Name, env.Value)
		}
	}
	return nil
}

// TransformDCGMExporter transforms dcgm exporter daemonset with required config as per ClusterPolicy
func TransformDCGMExporter(obj *appsv1.DaemonSet, config *gpuv1.ClusterPolicySpec, n ClusterPolicyController) error {
	// update validation container
	err := transformValidationInitContainer(obj, config)
	if err != nil {
		return err
	}

	// update image
	image, err := gpuv1.ImagePath(&config.DCGMExporter)
	if err != nil {
		return err
	}
	obj.Spec.Template.Spec.Containers[0].Image = image

	// update image pull policy
	obj.Spec.Template.Spec.Containers[0].ImagePullPolicy = gpuv1.ImagePullPolicy(config.DCGMExporter.ImagePullPolicy)
	// set image pull secrets
	if len(config.DCGMExporter.ImagePullSecrets) > 0 {
		addPullSecrets(&obj.Spec.Template.Spec, config.DCGMExporter.ImagePullSecrets)
	}
	// set resource limits
	if config.DCGMExporter.Resources != nil {
		// apply resource limits to all containers
		for i := range obj.Spec.Template.Spec.Containers {
			obj.Spec.Template.Spec.Containers[i].Resources.Requests = config.DCGMExporter.Resources.Requests
			obj.Spec.Template.Spec.Containers[i].Resources.Limits = config.DCGMExporter.Resources.Limits
		}
	}
	// set arguments if specified for exporter container
	if len(config.DCGMExporter.Args) > 0 {
		obj.Spec.Template.Spec.Containers[0].Args = config.DCGMExporter.Args
	}

	// check if DCGM hostengine is enabled as a separate Pod and setup env accordingly
	if config.DCGM.IsEnabled() {
		setContainerEnv(&(obj.Spec.Template.Spec.Containers[0]), DCGMRemoteEngineEnvName, fmt.Sprintf("nvidia-dcgm:%d", DCGMDefaultPort))
	} else {
		// case for DCGM running on the host itself(DGX BaseOS)
		remoteEngine := getContainerEnv(&(obj.Spec.Template.Spec.Containers[0]), DCGMRemoteEngineEnvName)
		if remoteEngine != "" && strings.HasPrefix(remoteEngine, "localhost") {
			// enable hostNetwork for communication with external DCGM using localhost
			obj.Spec.Template.Spec.HostNetwork = true
			obj.Spec.Template.Spec.DNSPolicy = corev1.DNSClusterFirstWithHostNet
		}
	}
	// set hostNetwork if specified for DCGM Exporter (if it is already enabled above,
	// do not touch the value)
	if config.DCGMExporter.IsHostNetworkEnabled() {
		obj.Spec.Template.Spec.HostNetwork = true
		obj.Spec.Template.Spec.DNSPolicy = corev1.DNSClusterFirstWithHostNet
	}

	setNRIPluginAnnotation(&obj.Spec.Template.ObjectMeta, &config.CDI, obj.Spec.Template.Spec.Containers[0].Name)
	setRuntimeClassName(&obj.Spec.Template.Spec, config, n.runtime)

	// set hostPID if specified for DCGM Exporter
	if config.DCGMExporter.IsHostPIDEnabled() {
		obj.Spec.Template.Spec.HostPID = true
	}

	// configure HPC job mapping if enabled
	if config.DCGMExporter.IsHPCJobMappingEnabled() {
		jobMappingDir := config.DCGMExporter.GetHPCJobMappingDirectory()
		if jobMappingDir == "" {
			jobMappingDir = gpuv1.DefaultDCGMJobMappingDir
		}

		// set environment variable for DCGM Exporter
		setContainerEnv(&(obj.Spec.Template.Spec.Containers[0]), "DCGM_HPC_JOB_MAPPING_DIR", jobMappingDir)

		// add volumeMount to main container
		jobMappingVolMount := corev1.VolumeMount{Name: "hpc-job-mapping", ReadOnly: true, MountPath: jobMappingDir}
		obj.Spec.Template.Spec.Containers[0].VolumeMounts = append(obj.Spec.Template.Spec.Containers[0].VolumeMounts, jobMappingVolMount)

		// add volume
		jobMappingVolumeSource := corev1.VolumeSource{
			HostPath: &corev1.HostPathVolumeSource{
				Path: jobMappingDir,
				Type: ptr.To(corev1.HostPathDirectoryOrCreate),
			},
		}
		jobMappingVol := corev1.Volume{Name: "hpc-job-mapping", VolumeSource: jobMappingVolumeSource}
		obj.Spec.Template.Spec.Volumes = append(obj.Spec.Template.Spec.Volumes, jobMappingVol)
	}

	// mount configmap for custom metrics if provided by user
	if config.DCGMExporter.MetricsConfig != nil && config.DCGMExporter.MetricsConfig.Name != "" {
		metricsConfigVolMount := corev1.VolumeMount{Name: "metrics-config", ReadOnly: true, MountPath: MetricsConfigMountPath, SubPath: MetricsConfigFileName}
		obj.Spec.Template.Spec.Containers[0].VolumeMounts = append(obj.Spec.Template.Spec.Containers[0].VolumeMounts, metricsConfigVolMount)

		metricsConfigVolumeSource := corev1.VolumeSource{
			ConfigMap: &corev1.ConfigMapVolumeSource{
				LocalObjectReference: corev1.LocalObjectReference{
					Name: config.DCGMExporter.MetricsConfig.Name,
				},
				Items: []corev1.KeyToPath{
					{
						Key:  MetricsConfigFileName,
						Path: MetricsConfigFileName,
					},
				},
			},
		}
		metricsConfigVol := corev1.Volume{Name: "metrics-config", VolumeSource: metricsConfigVolumeSource}
		obj.Spec.Template.Spec.Volumes = append(obj.Spec.Template.Spec.Volumes, metricsConfigVol)

		setContainerEnv(&(obj.Spec.Template.Spec.Containers[0]), "DCGM_EXPORTER_COLLECTORS", MetricsConfigMountPath)
	}

	if n.openshift != "" {
		if err = transformDCGMExporterForOpenShift(obj, config); err != nil {
			return fmt.Errorf("failed to transform dcgm-exporter for openshift: %w", err)
		}
	}

	for _, env := range config.DCGMExporter.Env {
		setContainerEnv(&(obj.Spec.Template.Spec.Containers[0]), env.Name, env.Value)
	}

	return nil
}

func transformDCGMExporterForOpenShift(obj *appsv1.DaemonSet, config *gpuv1.ClusterPolicySpec) error {
	// Add initContainer for OCP to set proper SELinux context on /var/lib/kubelet/pod-resources
	initImage, err := gpuv1.ImagePath(&config.Operator.InitContainer)
	if err != nil {
		return err
	}

	initContainer := corev1.Container{}
	if initImage != "" {
		initContainer.Image = initImage
	}
	initContainer.Name = "init-pod-nvidia-node-status-exporter"
	initContainer.ImagePullPolicy = gpuv1.ImagePullPolicy(config.Operator.InitContainer.ImagePullPolicy)
	initContainer.Command = []string{"/bin/entrypoint.sh"}

	// need CAP_SYS_ADMIN privileges for collecting pod specific resources
	privileged := true
	securityContext := &corev1.SecurityContext{
		Privileged: &privileged,
	}

	initContainer.SecurityContext = securityContext

	// Disable all constraints on the configurations required by NVIDIA container toolkit
	setContainerEnv(&initContainer, NvidiaDisableRequireEnvName, "true")

	volMountSockName, volMountSockPath := "pod-gpu-resources", "/var/lib/kubelet/pod-resources"
	volMountSock := corev1.VolumeMount{Name: volMountSockName, MountPath: volMountSockPath}
	initContainer.VolumeMounts = append(initContainer.VolumeMounts, volMountSock)

	volMountConfigName, volMountConfigPath, volMountConfigSubPath := "init-config", "/bin/entrypoint.sh", "entrypoint.sh"
	volMountConfig := corev1.VolumeMount{Name: volMountConfigName, ReadOnly: true, MountPath: volMountConfigPath, SubPath: volMountConfigSubPath}
	initContainer.VolumeMounts = append(initContainer.VolumeMounts, volMountConfig)

	obj.Spec.Template.Spec.InitContainers = append(obj.Spec.Template.Spec.InitContainers, initContainer)

	volMountConfigKey, volMountConfigDefaultMode := "nvidia-dcgm-exporter", int32(0700)
	initVol := corev1.Volume{Name: volMountConfigName, VolumeSource: corev1.VolumeSource{ConfigMap: &corev1.ConfigMapVolumeSource{LocalObjectReference: corev1.LocalObjectReference{Name: volMountConfigKey}, DefaultMode: &volMountConfigDefaultMode}}}
	obj.Spec.Template.Spec.Volumes = append(obj.Spec.Template.Spec.Volumes, initVol)

	return nil
}

// TransformDCGM transforms dcgm daemonset with required config as per ClusterPolicy
func TransformDCGM(obj *appsv1.DaemonSet, config *gpuv1.ClusterPolicySpec, n ClusterPolicyController) error {
	// update validation container
	err := transformValidationInitContainer(obj, config)
	if err != nil {
		return err
	}
	// update image
	image, err := gpuv1.ImagePath(&config.DCGM)
	if err != nil {
		return err
	}
	obj.Spec.Template.Spec.Containers[0].Image = image
	// update image pull policy
	obj.Spec.Template.Spec.Containers[0].ImagePullPolicy = gpuv1.ImagePullPolicy(config.DCGM.ImagePullPolicy)
	// set image pull secrets
	if len(config.DCGM.ImagePullSecrets) > 0 {
		addPullSecrets(&obj.Spec.Template.Spec, config.DCGM.ImagePullSecrets)
	}
	// set resource limits
	if config.DCGM.Resources != nil {
		// apply resource limits to all containers
		for i := range obj.Spec.Template.Spec.Containers {
			obj.Spec.Template.Spec.Containers[i].Resources.Requests = config.DCGM.Resources.Requests
			obj.Spec.Template.Spec.Containers[i].Resources.Limits = config.DCGM.Resources.Limits
		}
	}
	// set arguments if specified for exporter container
	if len(config.DCGM.Args) > 0 {
		obj.Spec.Template.Spec.Containers[0].Args = config.DCGM.Args
	}
	// set/append environment variables for exporter container
	if len(config.DCGM.Env) > 0 {
		for _, env := range config.DCGM.Env {
			setContainerEnv(&(obj.Spec.Template.Spec.Containers[0]), env.Name, env.Value)
		}
	}

	setNRIPluginAnnotation(&obj.Spec.Template.ObjectMeta, &config.CDI, obj.Spec.Template.Spec.Containers[0].Name)
	setRuntimeClassName(&obj.Spec.Template.Spec, config, n.runtime)

	return nil
}

// TransformMIGManager transforms MIG Manager daemonset with required config as per ClusterPolicy
func TransformMIGManager(obj *appsv1.DaemonSet, config *gpuv1.ClusterPolicySpec, n ClusterPolicyController) error {
	// update validation container
	err := transformValidationInitContainer(obj, config)
	if err != nil {
		return err
	}

	// update image
	image, err := gpuv1.ImagePath(&config.MIGManager)
	if err != nil {
		return err
	}
	obj.Spec.Template.Spec.Containers[0].Image = image

	// update image pull policy
	obj.Spec.Template.Spec.Containers[0].ImagePullPolicy = gpuv1.ImagePullPolicy(config.MIGManager.ImagePullPolicy)

	// set image pull secrets
	if len(config.MIGManager.ImagePullSecrets) > 0 {
		addPullSecrets(&obj.Spec.Template.Spec, config.MIGManager.ImagePullSecrets)
	}

	// set resource limits
	if config.MIGManager.Resources != nil {
		// apply resource limits to all containers
		for i := range obj.Spec.Template.Spec.Containers {
			obj.Spec.Template.Spec.Containers[i].Resources.Requests = config.MIGManager.Resources.Requests
			obj.Spec.Template.Spec.Containers[i].Resources.Limits = config.MIGManager.Resources.Limits
		}
	}

	// set arguments if specified for mig-manager container
	if len(config.MIGManager.Args) > 0 {
		obj.Spec.Template.Spec.Containers[0].Args = config.MIGManager.Args
	}

	setRuntimeClassName(&obj.Spec.Template.Spec, config, n.runtime)
	setNRIPluginAnnotation(&obj.Spec.Template.ObjectMeta, &config.CDI, obj.Spec.Template.Spec.Containers[0].Name)

	// set ConfigMap name for "mig-parted-config" Volume
	for i, vol := range obj.Spec.Template.Spec.Volumes {
		if !strings.Contains(vol.Name, "mig-parted-config") {
			continue
		}

		name, _ := gpuv1.GetConfigMapName(config.MIGManager.Config, MigPartedDefaultConfigMapName)
		obj.Spec.Template.Spec.Volumes[i].ConfigMap.Name = name
		break
	}

	// set ConfigMap name for "gpu-clients" Volume
	for i, vol := range obj.Spec.Template.Spec.Volumes {
		if !strings.Contains(vol.Name, "gpu-clients") {
			continue
		}

		name, _ := gpuv1.GetConfigMapName(config.MIGManager.GPUClientsConfig, MigDefaultGPUClientsConfigMapName)
		obj.Spec.Template.Spec.Volumes[i].ConfigMap.Name = name
		break
	}

	// update env required for CDI support
	if config.CDI.IsEnabled() {
		setContainerEnv(&(obj.Spec.Template.Spec.Containers[0]), CDIEnabledEnvName, "true")
		if config.Toolkit.IsEnabled() {
			setContainerEnv(&(obj.Spec.Template.Spec.Containers[0]), NvidiaCDIHookPathEnvName, filepath.Join(config.Toolkit.InstallDir, "toolkit/nvidia-cdi-hook"))
		}
	}

	if len(config.MIGManager.Env) > 0 {
		for _, env := range config.MIGManager.Env {
			setContainerEnv(&(obj.Spec.Template.Spec.Containers[0]), env.Name, env.Value)
		}
	}

	return nil
}

// TransformKataManager transforms Kata Manager daemonset with required config as per ClusterPolicy
func TransformKataManager(obj *appsv1.DaemonSet, config *gpuv1.ClusterPolicySpec, n ClusterPolicyController) error {
	// update image
	image, err := gpuv1.ImagePath(&config.KataManager)
	if err != nil {
		return err
	}
	obj.Spec.Template.Spec.Containers[0].Image = image

	// update image pull policy
	obj.Spec.Template.Spec.Containers[0].ImagePullPolicy = gpuv1.ImagePullPolicy(config.KataManager.ImagePullPolicy)

	// set image pull secrets
	if len(config.KataManager.ImagePullSecrets) > 0 {
		addPullSecrets(&obj.Spec.Template.Spec, config.KataManager.ImagePullSecrets)
	}

	// set resource limits
	if config.KataManager.Resources != nil {
		// apply resource limits to all containers
		for i := range obj.Spec.Template.Spec.Containers {
			obj.Spec.Template.Spec.Containers[i].Resources.Requests = config.KataManager.Resources.Requests
			obj.Spec.Template.Spec.Containers[i].Resources.Limits = config.KataManager.Resources.Limits
		}
	}

	// set arguments if specified for mig-manager container
	if len(config.KataManager.Args) > 0 {
		obj.Spec.Template.Spec.Containers[0].Args = config.KataManager.Args
	}

	// mount artifactsDir
	artifactsDir := DefaultKataArtifactsDir
	if config.KataManager.Config.ArtifactsDir != "" {
		artifactsDir = config.KataManager.Config.ArtifactsDir
	}

	// set env used by readinessProbe to determine path to kata-manager pid file.
	setContainerEnv(&(obj.Spec.Template.Spec.Containers[0]), "KATA_ARTIFACTS_DIR", artifactsDir)

	artifactsVolMount := corev1.VolumeMount{Name: "kata-artifacts", MountPath: artifactsDir}
	obj.Spec.Template.Spec.Containers[0].VolumeMounts = append(obj.Spec.Template.Spec.Containers[0].VolumeMounts, artifactsVolMount)

	artifactsVol := corev1.Volume{Name: "kata-artifacts", VolumeSource: corev1.VolumeSource{HostPath: &corev1.HostPathVolumeSource{Path: artifactsDir, Type: ptr.To(corev1.HostPathDirectoryOrCreate)}}}
	obj.Spec.Template.Spec.Volumes = append(obj.Spec.Template.Spec.Volumes, artifactsVol)

	// Compute hash of kata manager config and add an annotation with the value.
	// If the kata config changes, a new revision of the daemonset will be
	// created and thus the kata-manager pods will restart with the updated config.
	hash := utils.GetObjectHash(config.KataManager.Config)

	if obj.Spec.Template.Annotations == nil {
		obj.Spec.Template.Annotations = make(map[string]string)
	}
	obj.Spec.Template.Annotations[KataManagerAnnotationHashKey] = hash

	if len(config.KataManager.Env) > 0 {
		for _, env := range config.KataManager.Env {
			setContainerEnv(&(obj.Spec.Template.Spec.Containers[0]), env.Name, env.Value)
		}
	}

	// mount containerd config and socket
	// setup mounts for runtime config file
	runtime := n.runtime.String()
	// kata manager is the only container in this daemonset
	err = transformForRuntime(obj, config, runtime, &obj.Spec.Template.Spec.Containers[0])
	if err != nil {
		return fmt.Errorf("error transforming kata-manager daemonset : %w", err)
	}

	return nil
}

// TransformVFIOManager transforms VFIO-PCI Manager daemonset with required config as per ClusterPolicy
func TransformVFIOManager(obj *appsv1.DaemonSet, config *gpuv1.ClusterPolicySpec, n ClusterPolicyController) error {
	// update k8s-driver-manager initContainer
	err := transformDriverManagerInitContainer(obj, &config.VFIOManager.DriverManager, nil)
	if err != nil {
		return fmt.Errorf("failed to transform k8s-driver-manager initContainer for VFIO Manager: %v", err)
	}

	// update image
	image, err := gpuv1.ImagePath(&config.VFIOManager)
	if err != nil {
		return err
	}
	obj.Spec.Template.Spec.Containers[0].Image = image

	// update image pull policy
	obj.Spec.Template.Spec.Containers[0].ImagePullPolicy = gpuv1.ImagePullPolicy(config.VFIOManager.ImagePullPolicy)

	// set image pull secrets
	if len(config.VFIOManager.ImagePullSecrets) > 0 {
		addPullSecrets(&obj.Spec.Template.Spec, config.VFIOManager.ImagePullSecrets)
	}

	// set resource limits
	if config.VFIOManager.Resources != nil {
		// apply resource limits to all containers
		for i := range obj.Spec.Template.Spec.Containers {
			obj.Spec.Template.Spec.Containers[i].Resources.Requests = config.VFIOManager.Resources.Requests
			obj.Spec.Template.Spec.Containers[i].Resources.Limits = config.VFIOManager.Resources.Limits
		}
	}

	// set arguments if specified for mig-manager container
	if len(config.VFIOManager.Args) > 0 {
		obj.Spec.Template.Spec.Containers[0].Args = config.VFIOManager.Args
	}

	// set/append environment variables for mig-manager container
	if len(config.VFIOManager.Env) > 0 {
		for _, env := range config.VFIOManager.Env {
			setContainerEnv(&(obj.Spec.Template.Spec.Containers[0]), env.Name, env.Value)
		}
	}

	return nil
}

// TransformCCManager transforms CC Manager daemonset with required config as per ClusterPolicy
func TransformCCManager(obj *appsv1.DaemonSet, config *gpuv1.ClusterPolicySpec, n ClusterPolicyController) error {
	// update image
	image, err := gpuv1.ImagePath(&config.CCManager)
	if err != nil {
		return err
	}
	obj.Spec.Template.Spec.Containers[0].Image = image

	// update image pull policy
	obj.Spec.Template.Spec.Containers[0].ImagePullPolicy = gpuv1.ImagePullPolicy(config.CCManager.ImagePullPolicy)

	// set image pull secrets
	if len(config.CCManager.ImagePullSecrets) > 0 {
		addPullSecrets(&obj.Spec.Template.Spec, config.CCManager.ImagePullSecrets)
	}

	// set resource limits
	if config.CCManager.Resources != nil {
		// apply resource limits to all containers
		for i := range obj.Spec.Template.Spec.Containers {
			obj.Spec.Template.Spec.Containers[i].Resources.Requests = config.CCManager.Resources.Requests
			obj.Spec.Template.Spec.Containers[i].Resources.Limits = config.CCManager.Resources.Limits
		}
	}

	// set arguments if specified for cc-manager container
	if len(config.CCManager.Args) > 0 {
		obj.Spec.Template.Spec.Containers[0].Args = config.CCManager.Args
	}

	// set default cc mode env
	if config.CCManager.DefaultMode != "" {
		setContainerEnv(&(obj.Spec.Template.Spec.Containers[0]), DefaultCCModeEnvName, config.CCManager.DefaultMode)
	}

	// set/append environment variables for cc-manager container
	if len(config.CCManager.Env) > 0 {
		for _, env := range config.CCManager.Env {
			setContainerEnv(&(obj.Spec.Template.Spec.Containers[0]), env.Name, env.Value)
		}
	}

	return nil
}

// TransformVGPUDeviceManager transforms VGPU Device Manager daemonset with required config as per ClusterPolicy
func TransformVGPUDeviceManager(obj *appsv1.DaemonSet, config *gpuv1.ClusterPolicySpec, n ClusterPolicyController) error {
	// update validation container
	err := transformValidationInitContainer(obj, config)
	if err != nil {
		return err
	}

	// update image
	image, err := gpuv1.ImagePath(&config.VGPUDeviceManager)
	if err != nil {
		return err
	}
	obj.Spec.Template.Spec.Containers[0].Image = image

	// update image pull policy
	obj.Spec.Template.Spec.Containers[0].ImagePullPolicy = gpuv1.ImagePullPolicy(config.VGPUDeviceManager.ImagePullPolicy)

	// set image pull secrets
	if len(config.VGPUDeviceManager.ImagePullSecrets) > 0 {
		addPullSecrets(&obj.Spec.Template.Spec, config.VGPUDeviceManager.ImagePullSecrets)
	}

	// set resource limits
	if config.VGPUDeviceManager.Resources != nil {
		// apply resource limits to all containers
		for i := range obj.Spec.Template.Spec.Containers {
			obj.Spec.Template.Spec.Containers[i].Resources.Requests = config.VGPUDeviceManager.Resources.Requests
			obj.Spec.Template.Spec.Containers[i].Resources.Limits = config.VGPUDeviceManager.Resources.Limits
		}
	}

	// set arguments if specified for mig-manager container
	if len(config.VGPUDeviceManager.Args) > 0 {
		obj.Spec.Template.Spec.Containers[0].Args = config.VGPUDeviceManager.Args
	}

	// set/append environment variables for mig-manager container
	if len(config.VGPUDeviceManager.Env) > 0 {
		for _, env := range config.VGPUDeviceManager.Env {
			setContainerEnv(&(obj.Spec.Template.Spec.Containers[0]), env.Name, env.Value)
		}
	}

	// set ConfigMap name for "vgpu-config" Volume
	for i, vol := range obj.Spec.Template.Spec.Volumes {
		if !strings.Contains(vol.Name, "vgpu-config") {
			continue
		}

		name, _ := gpuv1.GetConfigMapName(config.VGPUDeviceManager.Config, VgpuDMDefaultConfigMapName)
		obj.Spec.Template.Spec.Volumes[i].ConfigMap.Name = name
		break
	}

	// set name of default vGPU device configuration. The default configuration is applied if the node
	// is not labelled with a specific configuration
	defaultConfig := VgpuDMDefaultConfigName
	if config.VGPUDeviceManager.Config != nil && config.VGPUDeviceManager.Config.Default != "" {
		defaultConfig = config.VGPUDeviceManager.Config.Default
	}
	setContainerEnv(&(obj.Spec.Template.Spec.Containers[0]), "DEFAULT_VGPU_CONFIG", defaultConfig)

	return nil
}

// transformValidatorSecurityContext updates the security context for a validator
// container so that it runs as uid 0. Some of the validations run commands
// that require root privileges (e.g. chroot). In addition, all validations
// create / delete status files in the '/run/nvidia/validations' host path
// volume. This directory is initially created by the kubelet and thus has
// the same group and ownership as the kubelet.
func transformValidatorSecurityContext(ctr *corev1.Container) {
	if ctr.SecurityContext == nil {
		ctr.SecurityContext = &corev1.SecurityContext{}
	}
	ctr.SecurityContext.RunAsUser = rootUID
}

// TransformValidator transforms nvidia-operator-validator daemonset with required config as per ClusterPolicy
func TransformValidator(obj *appsv1.DaemonSet, config *gpuv1.ClusterPolicySpec, n ClusterPolicyController) error {
	err := TransformValidatorShared(obj, config)
	if err != nil {
		return fmt.Errorf("%v", err)
	}

	setRuntimeClassName(&obj.Spec.Template.Spec, config, n.runtime)

	toolkitValidationCtr := findContainerByName(obj.Spec.Template.Spec.InitContainers, "toolkit-validation")
	if toolkitValidationCtr != nil && len(toolkitValidationCtr.Name) > 0 {
		setNRIPluginAnnotation(&obj.Spec.Template.ObjectMeta, &config.CDI, toolkitValidationCtr.Name)
	}

	var validatorErr error
	// apply changes for individual component validators(initContainers)
	components := []string{
		"driver",
		"nvidia-fs",
		"gdrcopy",
		"toolkit",
		"cuda",
		"plugin",
	}

	for _, component := range components {
		if err := TransformValidatorComponent(config, &obj.Spec.Template.Spec, component); err != nil {
			validatorErr = errors.Join(validatorErr, err)
		}
	}

	if validatorErr != nil {
		n.logger.Info("WARN: errors transforming the validator containers: %v", validatorErr)
	}

	return nil
}

// TransformSandboxValidator transforms nvidia-sandbox-validator daemonset with required config as per ClusterPolicy
func TransformSandboxValidator(obj *appsv1.DaemonSet, config *gpuv1.ClusterPolicySpec, n ClusterPolicyController) error {
	err := TransformValidatorShared(obj, config)
	if err != nil {
		return fmt.Errorf("%v", err)
	}

	var validatorErr error
	// apply changes for individual component validators(initContainers)
	components := []string{
		"cc-manager",
		"vfio-pci",
		"vgpu-manager",
		"vgpu-devices",
	}

	for _, component := range components {
		if err := TransformValidatorComponent(config, &obj.Spec.Template.Spec, component); err != nil {
			validatorErr = errors.Join(validatorErr, err)
		}
	}

	if validatorErr != nil {
		n.logger.Info("WARN: errors transforming the validator containers: %v", validatorErr)
	}

	return nil
}

// TransformValidatorShared applies general transformations to the validator daemonset with required config as per ClusterPolicy
func TransformValidatorShared(obj *appsv1.DaemonSet, config *gpuv1.ClusterPolicySpec) error {
	// update image
	image, err := gpuv1.ImagePath(&config.Validator)
	if err != nil {
		return err
	}
	obj.Spec.Template.Spec.Containers[0].Image = image
	// update image pull policy
	obj.Spec.Template.Spec.Containers[0].ImagePullPolicy = gpuv1.ImagePullPolicy(config.Validator.ImagePullPolicy)
	// set image pull secrets
	if len(config.Validator.ImagePullSecrets) > 0 {
		addPullSecrets(&obj.Spec.Template.Spec, config.Validator.ImagePullSecrets)
	}
	// set resource limits
	if config.Validator.Resources != nil {
		// apply resource limits to all containers
		for i := range obj.Spec.Template.Spec.Containers {
			obj.Spec.Template.Spec.Containers[i].Resources.Requests = config.Validator.Resources.Requests
			obj.Spec.Template.Spec.Containers[i].Resources.Limits = config.Validator.Resources.Limits
		}
	}
	// set arguments if specified for validator container
	if len(config.Validator.Args) > 0 {
		obj.Spec.Template.Spec.Containers[0].Args = config.Validator.Args
	}
	// set/append environment variables for validator container
	if len(config.Validator.Env) > 0 {
		for _, env := range config.Validator.Env {
			setContainerEnv(&(obj.Spec.Template.Spec.Containers[0]), env.Name, env.Value)
		}
	}
	// update the security context for the validator container
	transformValidatorSecurityContext(&obj.Spec.Template.Spec.Containers[0])

	return nil
}

// TransformValidatorComponent applies changes to given validator component
func TransformValidatorComponent(config *gpuv1.ClusterPolicySpec, podSpec *corev1.PodSpec, component string) error {
	for i, initContainer := range podSpec.InitContainers {
		// skip if not component validation initContainer
		if !strings.Contains(initContainer.Name, fmt.Sprintf("%s-validation", component)) {
			continue
		}
		// update validation image
		image, err := gpuv1.ImagePath(&config.Validator)
		if err != nil {
			return err
		}
		podSpec.InitContainers[i].Image = image
		// update validation image pull policy
		if config.Validator.ImagePullPolicy != "" {
			podSpec.InitContainers[i].ImagePullPolicy = gpuv1.ImagePullPolicy(config.Validator.ImagePullPolicy)
		}
		// update the security context for the validator container
		transformValidatorSecurityContext(&podSpec.InitContainers[i])

		switch component {
		case "cuda":
			// set additional env to indicate image, pullSecrets to spin-off cuda validation workload pod.
			setContainerEnv(&(podSpec.InitContainers[i]), ValidatorImageEnvName, image)
			setContainerEnv(&(podSpec.InitContainers[i]), ValidatorImagePullPolicyEnvName, config.Validator.ImagePullPolicy)
			var pullSecrets string
			if len(config.Validator.ImagePullSecrets) > 0 {
				pullSecrets = strings.Join(config.Validator.ImagePullSecrets, ",")
				setContainerEnv(&(podSpec.InitContainers[i]), ValidatorImagePullSecretsEnvName, pullSecrets)
			}
			if podSpec.RuntimeClassName != nil {
				setContainerEnv(&(podSpec.InitContainers[i]), ValidatorRuntimeClassEnvName, *podSpec.RuntimeClassName)
			}
			// set/append environment variables for cuda-validation container
			if len(config.Validator.CUDA.Env) > 0 {
				for _, env := range config.Validator.CUDA.Env {
					setContainerEnv(&(podSpec.InitContainers[i]), env.Name, env.Value)
				}
			}
		case "plugin":
			// remove plugin init container from validator Daemonset if it is not enabled
			if !config.DevicePlugin.IsEnabled() {
				podSpec.InitContainers = append(podSpec.InitContainers[:i], podSpec.InitContainers[i+1:]...)
				return nil
			}
			// set additional env to indicate image, pullSecrets to spin-off plugin validation workload pod.
			setContainerEnv(&(podSpec.InitContainers[i]), ValidatorImageEnvName, image)
			setContainerEnv(&(podSpec.InitContainers[i]), ValidatorImagePullPolicyEnvName, config.Validator.ImagePullPolicy)
			var pullSecrets string
			if len(config.Validator.ImagePullSecrets) > 0 {
				pullSecrets = strings.Join(config.Validator.ImagePullSecrets, ",")
				setContainerEnv(&(podSpec.InitContainers[i]), ValidatorImagePullSecretsEnvName, pullSecrets)
			}
			if podSpec.RuntimeClassName != nil {
				setContainerEnv(&(podSpec.InitContainers[i]), ValidatorRuntimeClassEnvName, *podSpec.RuntimeClassName)
			}
			// apply mig-strategy env to spin off plugin-validation workload pod
			setContainerEnv(&(podSpec.InitContainers[i]), MigStrategyEnvName, string(config.MIG.Strategy))
			// set/append environment variables for plugin-validation container
			if len(config.Validator.Plugin.Env) > 0 {
				for _, env := range config.Validator.Plugin.Env {
					setContainerEnv(&(podSpec.InitContainers[i]), env.Name, env.Value)
				}
			}
		case "driver":
			// set/append environment variables for driver-validation container
			if len(config.Validator.Driver.Env) > 0 {
				for _, env := range config.Validator.Driver.Env {
					setContainerEnv(&(podSpec.InitContainers[i]), env.Name, env.Value)
				}
			}
		case "cc-manager":
			if !config.CCManager.IsEnabled() {
				// remove  cc-manager init container from validator Daemonset if it is not enabled
				podSpec.InitContainers = append(podSpec.InitContainers[:i], podSpec.InitContainers[i+1:]...)
				return nil
			}
		case "toolkit":
			// set/append environment variables for toolkit-validation container
			if len(config.Validator.Toolkit.Env) > 0 {
				for _, env := range config.Validator.Toolkit.Env {
					setContainerEnv(&(podSpec.InitContainers[i]), env.Name, env.Value)
				}
			}
		case "vfio-pci":
			// set/append environment variables for vfio-pci-validation container
			setContainerEnv(&(podSpec.InitContainers[i]), "DEFAULT_GPU_WORKLOAD_CONFIG", defaultGPUWorkloadConfig)
			if len(config.Validator.VFIOPCI.Env) > 0 {
				for _, env := range config.Validator.VFIOPCI.Env {
					setContainerEnv(&(podSpec.InitContainers[i]), env.Name, env.Value)
				}
			}
		case "vgpu-manager":
			// set/append environment variables for vgpu-manager-validation container
			setContainerEnv(&(podSpec.InitContainers[i]), "DEFAULT_GPU_WORKLOAD_CONFIG", defaultGPUWorkloadConfig)
			if len(config.Validator.VGPUManager.Env) > 0 {
				for _, env := range config.Validator.VGPUManager.Env {
					setContainerEnv(&(podSpec.InitContainers[i]), env.Name, env.Value)
				}
			}
		case "vgpu-devices":
			// set/append environment variables for vgpu-devices-validation container
			setContainerEnv(&(podSpec.InitContainers[i]), "DEFAULT_GPU_WORKLOAD_CONFIG", defaultGPUWorkloadConfig)
			if len(config.Validator.VGPUDevices.Env) > 0 {
				for _, env := range config.Validator.VGPUDevices.Env {
					setContainerEnv(&(podSpec.InitContainers[i]), env.Name, env.Value)
				}
			}
		default:
			return fmt.Errorf("invalid component provided to apply validator changes")
		}
	}
	return nil
}

// TransformNodeStatusExporter transforms the node-status-exporter daemonset with required config as per ClusterPolicy
func TransformNodeStatusExporter(obj *appsv1.DaemonSet, config *gpuv1.ClusterPolicySpec, n ClusterPolicyController) error {
	// update image
	image, err := gpuv1.ImagePath(&config.NodeStatusExporter)
	if err != nil {
		return err
	}
	obj.Spec.Template.Spec.Containers[0].Image = image

	// update image pull policy
	obj.Spec.Template.Spec.Containers[0].ImagePullPolicy = gpuv1.ImagePullPolicy(config.NodeStatusExporter.ImagePullPolicy)

	// set image pull secrets
	if len(config.NodeStatusExporter.ImagePullSecrets) > 0 {
		addPullSecrets(&obj.Spec.Template.Spec, config.NodeStatusExporter.ImagePullSecrets)
	}

	// set resource limits
	if config.NodeStatusExporter.Resources != nil {
		// apply resource limits to all containers
		for i := range obj.Spec.Template.Spec.Containers {
			obj.Spec.Template.Spec.Containers[i].Resources.Requests = config.NodeStatusExporter.Resources.Requests
			obj.Spec.Template.Spec.Containers[i].Resources.Limits = config.NodeStatusExporter.Resources.Limits
		}
	}

	// set arguments if specified for driver container
	if len(config.NodeStatusExporter.Args) > 0 {
		obj.Spec.Template.Spec.Containers[0].Args = config.NodeStatusExporter.Args
	}

	// set/append environment variables for exporter container
	if len(config.NodeStatusExporter.Env) > 0 {
		for _, env := range config.NodeStatusExporter.Env {
			setContainerEnv(&(obj.Spec.Template.Spec.Containers[0]), env.Name, env.Value)
		}
	}

	// update the security context for the node status exporter container.
	transformValidatorSecurityContext(&obj.Spec.Template.Spec.Containers[0])

	return nil
}

// getRuntimeConfigFiles returns the path to the top-level and drop-in config files that
// should be used when configuring the specified container runtime.
func getRuntimeConfigFiles(c *corev1.Container, runtime string) (string, string, error) {
	switch runtime {
	case gpuv1.Docker.String():
		topLevelConfigFile := DefaultDockerConfigFile
		if value := getContainerEnv(c, "DOCKER_CONFIG"); value != "" {
			topLevelConfigFile = value
		} else if value := getContainerEnv(c, "RUNTIME_CONFIG"); value != "" {
			topLevelConfigFile = value
		}
		// Docker does not support drop-in files.
		return topLevelConfigFile, "", nil
	case gpuv1.Containerd.String():
		topLevelConfigFile := DefaultContainerdConfigFile
		if value := getContainerEnv(c, "CONTAINERD_CONFIG"); value != "" {
			topLevelConfigFile = value
		} else if value := getContainerEnv(c, "RUNTIME_CONFIG"); value != "" {
			topLevelConfigFile = value
		}
		dropInConfigFile := DefaultContainerdDropInConfigFile
		if value := getContainerEnv(c, "RUNTIME_DROP_IN_CONFIG"); value != "" {
			dropInConfigFile = value
		}
		return topLevelConfigFile, dropInConfigFile, nil
	case gpuv1.CRIO.String():
		// TODO: We should still allow the top-level config to be specified
		topLevelConfigFile := DefaultCRIOConfigFile
		if value := getContainerEnv(c, "CRIO_CONFIG"); value != "" {
			topLevelConfigFile = value
		} else if value := getContainerEnv(c, "RUNTIME_CONFIG"); value != "" {
			topLevelConfigFile = value
		}
		dropInConfigFile := DefaultCRIODropInConfigFile
		if value := getContainerEnv(c, "RUNTIME_DROP_IN_CONFIG"); value != "" {
			dropInConfigFile = value
		}
		return topLevelConfigFile, dropInConfigFile, nil
	default:
		return "", "", fmt.Errorf("invalid runtime: %s", runtime)
	}
}

// get runtime(docker, containerd) socket file path based on toolkit container env or default
func getRuntimeSocketFile(c *corev1.Container, runtime string) (string, error) {
	var runtimeSocketFile string
	switch runtime {
	case gpuv1.Docker.String():
		runtimeSocketFile = DefaultDockerSocketFile
		if getContainerEnv(c, "DOCKER_SOCKET") != "" {
			runtimeSocketFile = getContainerEnv(c, "DOCKER_SOCKET")
		}
	case gpuv1.Containerd.String():
		runtimeSocketFile = DefaultContainerdSocketFile
		if getContainerEnv(c, "CONTAINERD_SOCKET") != "" {
			runtimeSocketFile = getContainerEnv(c, "CONTAINERD_SOCKET")
		}
	case gpuv1.CRIO.String():
		runtimeSocketFile = ""
	default:
		return "", fmt.Errorf("invalid runtime: %s", runtime)
	}

	return runtimeSocketFile, nil
}

func getContainerEnv(c *corev1.Container, key string) string {
	for _, val := range c.Env {
		if val.Name == key {
			return val.Value
		}
	}
	return ""
}

func setContainerEnv(c *corev1.Container, key, value string) {
	for i, val := range c.Env {
		if val.Name != key {
			continue
		}

		c.Env[i].Value = value
		return
	}
	c.Env = append(c.Env, corev1.EnvVar{Name: key, Value: value})
}

// findContainerByName returns a pointer to the container with the given name, or nil if not found.
func findContainerByName(containers []corev1.Container, name string) *corev1.Container {
	for i := range containers {
		if containers[i].Name == name {
			return &containers[i]
		}
	}
	return nil
}

func getRuntimeClassName(config *gpuv1.ClusterPolicySpec) string {
	if config.Operator.RuntimeClass != "" {
		return config.Operator.RuntimeClass
	}
	return DefaultRuntimeClass
}

func setRuntimeClassName(podSpec *corev1.PodSpec, config *gpuv1.ClusterPolicySpec, runtime gpuv1.Runtime) {
	if !config.CDI.IsEnabled() && runtime == gpuv1.CRIO || config.CDI.IsNRIPluginEnabled() {
		return
	}
	runtimeClassName := getRuntimeClassName(config)
	podSpec.RuntimeClassName = &runtimeClassName
}

func setContainerProbe(container *corev1.Container, probe *gpuv1.ContainerProbeSpec, probeType ContainerProbe) {
	var containerProbe *corev1.Probe

	// determine probe type to update
	switch probeType {
	case Startup:
		containerProbe = container.StartupProbe
	case Liveness:
		containerProbe = container.LivenessProbe
	case Readiness:
		containerProbe = container.ReadinessProbe
	}

	// set probe parameters if specified
	if probe.InitialDelaySeconds != 0 {
		containerProbe.InitialDelaySeconds = probe.InitialDelaySeconds
	}
	if probe.TimeoutSeconds != 0 {
		containerProbe.TimeoutSeconds = probe.TimeoutSeconds
	}
	if probe.FailureThreshold != 0 {
		containerProbe.FailureThreshold = probe.FailureThreshold
	}
	if probe.SuccessThreshold != 0 {
		containerProbe.SuccessThreshold = probe.SuccessThreshold
	}
	if probe.PeriodSeconds != 0 {
		containerProbe.PeriodSeconds = probe.PeriodSeconds
	}
}

// applies MIG related configuration env to container spec
func applyMIGConfiguration(c *corev1.Container, strategy gpuv1.MIGStrategy) {
	// if not set then let plugin decide this per node(default: none)
	if strategy == "" {
		setContainerEnv(c, "NVIDIA_MIG_MONITOR_DEVICES", "all")
		return
	}

	setContainerEnv(c, "MIG_STRATEGY", string(strategy))
	if strategy != gpuv1.MIGStrategyNone {
		setContainerEnv(c, "NVIDIA_MIG_MONITOR_DEVICES", "all")
	}
}

// checks if custom plugin config is provided through a ConfigMap
func isCustomPluginConfigSet(pluginConfig *gpuv1.DevicePluginConfig) bool {
	if pluginConfig != nil && pluginConfig.Name != "" {
		return true
	}
	return false
}

// adds shared volume mounts required for custom plugin config provided via a ConfigMap
func addSharedMountsForPluginConfig(container *corev1.Container, config *gpuv1.DevicePluginConfig) {
	emptyDirMount := corev1.VolumeMount{Name: "config", MountPath: "/config"}
	configVolMount := corev1.VolumeMount{Name: config.Name, MountPath: "/available-configs"}

	container.VolumeMounts = append(container.VolumeMounts, emptyDirMount)
	container.VolumeMounts = append(container.VolumeMounts, configVolMount)
}

// apply spec changes to make custom configurations provided via a ConfigMap available to all containers
func handleDevicePluginConfig(obj *appsv1.DaemonSet, config *gpuv1.ClusterPolicySpec) error {
	if !isCustomPluginConfigSet(config.DevicePlugin.Config) {
		// remove config-manager-init container
		for i, initContainer := range obj.Spec.Template.Spec.InitContainers {
			if initContainer.Name != "config-manager-init" {
				continue
			}
			obj.Spec.Template.Spec.InitContainers = append(obj.Spec.Template.Spec.InitContainers[:i], obj.Spec.Template.Spec.InitContainers[i+1:]...)
		}
		// remove config-manager sidecar container
		for i, container := range obj.Spec.Template.Spec.Containers {
			if container.Name != "config-manager" {
				continue
			}
			obj.Spec.Template.Spec.Containers = append(obj.Spec.Template.Spec.Containers[:i], obj.Spec.Template.Spec.Containers[i+1:]...)
		}
		return nil
	}

	// Apply custom configuration provided through ConfigMap
	// setup env for main container
	for i, container := range obj.Spec.Template.Spec.Containers {
		switch container.Name {
		case "nvidia-device-plugin":
		case "gpu-feature-discovery":
		case "mps-control-daemon-ctr":
		default:
			// skip if not the main container
			continue
		}
		setContainerEnv(&obj.Spec.Template.Spec.Containers[i], "CONFIG_FILE", "/config/config.yaml")
		// setup sharedvolume(emptydir) for main container
		addSharedMountsForPluginConfig(&obj.Spec.Template.Spec.Containers[i], config.DevicePlugin.Config)
	}

	// if hostPID is already set, we skip setting the shareProcessNamespace field
	// for context, go to https://github.com/kubernetes-client/go/blob/master/kubernetes/docs/V1PodSpec.md
	if !obj.Spec.Template.Spec.HostPID {
		// Enable process ns sharing for PID access
		shareProcessNamespace := true
		obj.Spec.Template.Spec.ShareProcessNamespace = &shareProcessNamespace
	}
	// setup volumes from configmap and shared emptyDir
	obj.Spec.Template.Spec.Volumes = append(obj.Spec.Template.Spec.Volumes, createConfigMapVolume(config.DevicePlugin.Config.Name, nil))
	obj.Spec.Template.Spec.Volumes = append(obj.Spec.Template.Spec.Volumes, createEmptyDirVolume("config"))

	// apply env/volume changes to initContainer
	err := transformConfigManagerInitContainer(obj, config)
	if err != nil {
		return err
	}
	// apply env/volume changes to sidecarContainer
	err = transformConfigManagerSidecarContainer(obj, config)
	if err != nil {
		return err
	}
	return nil
}

func transformConfigManagerInitContainer(obj *appsv1.DaemonSet, config *gpuv1.ClusterPolicySpec) error {
	initContainer := findContainerByName(obj.Spec.Template.Spec.InitContainers, "config-manager-init")
	if initContainer == nil {
		// config-manager-init container is not added to the spec, this is a no-op
		return nil
	}
	configManagerImage, err := gpuv1.ImagePath(&config.DevicePlugin)
	if err != nil {
		return err
	}
	initContainer.Image = configManagerImage
	if config.DevicePlugin.ImagePullPolicy != "" {
		initContainer.ImagePullPolicy = gpuv1.ImagePullPolicy(config.DevicePlugin.ImagePullPolicy)
	}
	// setup env
	setContainerEnv(initContainer, "DEFAULT_CONFIG", config.DevicePlugin.Config.Default)
	setContainerEnv(initContainer, "FALLBACK_STRATEGIES", "empty")

	// setup volume mounts
	addSharedMountsForPluginConfig(initContainer, config.DevicePlugin.Config)
	return nil
}

func transformConfigManagerSidecarContainer(obj *appsv1.DaemonSet, config *gpuv1.ClusterPolicySpec) error {
	var container *corev1.Container
	for i := range obj.Spec.Template.Spec.Containers {
		if obj.Spec.Template.Spec.Containers[i].Name != "config-manager" {
			continue
		}
		container = &obj.Spec.Template.Spec.Containers[i]
	}
	if container == nil {
		// config-manager-init container is not added to the spec, this is a no-op
		return nil
	}
	configManagerImage, err := gpuv1.ImagePath(&config.DevicePlugin)
	if err != nil {
		return err
	}
	container.Image = configManagerImage
	if config.DevicePlugin.ImagePullPolicy != "" {
		container.ImagePullPolicy = gpuv1.ImagePullPolicy(config.DevicePlugin.ImagePullPolicy)
	}
	// setup env
	setContainerEnv(container, "DEFAULT_CONFIG", config.DevicePlugin.Config.Default)
	setContainerEnv(container, "FALLBACK_STRATEGIES", "empty")

	// setup volume mounts
	addSharedMountsForPluginConfig(container, config.DevicePlugin.Config)
	return nil
}

func transformDriverManagerInitContainer(obj *appsv1.DaemonSet, driverManagerSpec *gpuv1.DriverManagerSpec, rdmaSpec *gpuv1.GPUDirectRDMASpec) error {
	container := findContainerByName(obj.Spec.Template.Spec.InitContainers, "k8s-driver-manager")

	if container == nil {
		return fmt.Errorf("failed to find k8s-driver-manager initContainer in spec")
	}

	managerImage, err := gpuv1.ImagePath(driverManagerSpec)
	if err != nil {
		return err
	}
	container.Image = managerImage

	if driverManagerSpec.ImagePullPolicy != "" {
		container.ImagePullPolicy = gpuv1.ImagePullPolicy(driverManagerSpec.ImagePullPolicy)
	}

	if rdmaSpec != nil && rdmaSpec.IsEnabled() {
		setContainerEnv(container, GPUDirectRDMAEnabledEnvName, "true")
		if rdmaSpec.IsHostMOFED() {
			setContainerEnv(container, UseHostMOFEDEnvName, "true")
		}
	}

	// set/append environment variables for driver-manager initContainer
	if len(driverManagerSpec.Env) > 0 {
		for _, env := range driverManagerSpec.Env {
			setContainerEnv(container, env.Name, env.Value)
		}
	}

	// add any pull secrets needed for driver-manager image
	if len(driverManagerSpec.ImagePullSecrets) > 0 {
		addPullSecrets(&obj.Spec.Template.Spec, driverManagerSpec.ImagePullSecrets)
	}

	return nil
}

func transformPeerMemoryContainer(obj *appsv1.DaemonSet, config *gpuv1.ClusterPolicySpec, n ClusterPolicyController) error {
	for i, container := range obj.Spec.Template.Spec.Containers {
		// skip if not nvidia-peermem
		if !strings.Contains(container.Name, "nvidia-peermem") {
			continue
		}
		if config.Driver.GPUDirectRDMA == nil || !config.Driver.GPUDirectRDMA.IsEnabled() {
			// remove nvidia-peermem sidecar container from driver Daemonset if RDMA is not enabled
			obj.Spec.Template.Spec.Containers = append(obj.Spec.Template.Spec.Containers[:i], obj.Spec.Template.Spec.Containers[i+1:]...)
			return nil
		}
		// update nvidia-peermem driver image and pull policy to be same as gpu-driver image
		// as its installed as part of gpu-driver image
		driverImage, err := resolveDriverTag(n, &config.Driver)
		if err != nil {
			return err
		}
		if driverImage != "" {
			obj.Spec.Template.Spec.Containers[i].Image = driverImage
		}
		if config.Driver.ImagePullPolicy != "" {
			obj.Spec.Template.Spec.Containers[i].ImagePullPolicy = gpuv1.ImagePullPolicy(config.Driver.ImagePullPolicy)
		}
		if config.Driver.GPUDirectRDMA.UseHostMOFED != nil && *config.Driver.GPUDirectRDMA.UseHostMOFED {
			// set env indicating host-mofed is enabled
			setContainerEnv(&(obj.Spec.Template.Spec.Containers[i]), UseHostMOFEDEnvName, "true")
		}
		// mount any custom kernel module configuration parameters at /drivers
		if config.Driver.KernelModuleConfig != nil && config.Driver.KernelModuleConfig.Name != "" {
			// note: transformDriverContainer() will have already created a Volume backed by the ConfigMap.
			// Only add a VolumeMount for nvidia-peermem-ctr.
			destinationDir := "/drivers"
			volumeMounts, _, err := createConfigMapVolumeMounts(n, config.Driver.KernelModuleConfig.Name, destinationDir)
			if err != nil {
				return fmt.Errorf("ERROR: failed to create ConfigMap VolumeMounts for kernel module configuration: %v", err)
			}
			obj.Spec.Template.Spec.Containers[i].VolumeMounts = append(obj.Spec.Template.Spec.Containers[i].VolumeMounts, volumeMounts...)
		}
		if config.Driver.Resources != nil {
			obj.Spec.Template.Spec.Containers[i].Resources = corev1.ResourceRequirements{
				Requests: config.Driver.Resources.Requests,
				Limits:   config.Driver.Resources.Limits,
			}
		}
	}
	return nil
}

// check if running with openshift and add an ENV VAR to the OCP DTK CTR
func transformGDSContainer(obj *appsv1.DaemonSet, config *gpuv1.ClusterPolicySpec, n ClusterPolicyController) error {
	for i, container := range obj.Spec.Template.Spec.Containers {
		// skip if not nvidia-fs
		if !strings.Contains(container.Name, "nvidia-fs") {
			continue
		}
		if config.GPUDirectStorage == nil || !config.GPUDirectStorage.IsEnabled() {
			n.logger.Info("GPUDirect Storage is disabled")
			// remove nvidia-fs sidecar container from driver Daemonset if GDS is not enabled
			obj.Spec.Template.Spec.Containers = append(obj.Spec.Template.Spec.Containers[:i], obj.Spec.Template.Spec.Containers[i+1:]...)
			return nil
		}
		if config.Driver.UsePrecompiledDrivers() {
			return fmt.Errorf("GPUDirect Storage driver (nvidia-fs) is not supported along with pre-compiled NVIDIA drivers")
		}

		gdsContainer := &obj.Spec.Template.Spec.Containers[i]

		// update nvidia-fs(sidecar) image and pull policy
		gdsImage, err := resolveDriverTag(n, config.GPUDirectStorage)
		if err != nil {
			return err
		}
		if gdsImage != "" {
			gdsContainer.Image = gdsImage
		}
		if config.GPUDirectStorage.ImagePullPolicy != "" {
			gdsContainer.ImagePullPolicy = gpuv1.ImagePullPolicy(config.GPUDirectStorage.ImagePullPolicy)
		}

		// set image pull secrets
		if len(config.GPUDirectStorage.ImagePullSecrets) > 0 {
			addPullSecrets(&obj.Spec.Template.Spec, config.GPUDirectStorage.ImagePullSecrets)
		}

		// set/append environment variables for GDS container
		if len(config.GPUDirectStorage.Env) > 0 {
			for _, env := range config.GPUDirectStorage.Env {
				setContainerEnv(gdsContainer, env.Name, env.Value)
			}
		}

		if config.Driver.RepoConfig != nil && config.Driver.RepoConfig.ConfigMapName != "" {
			// note: transformDriverContainer() will have already created a Volume backed by the ConfigMap.
			// Only add a VolumeMount for nvidia-fs-ctr.
			destinationDir, err := getRepoConfigPath()
			if err != nil {
				return fmt.Errorf("ERROR: failed to get destination directory for custom repo config: %w", err)
			}
			volumeMounts, _, err := createConfigMapVolumeMounts(n, config.Driver.RepoConfig.ConfigMapName, destinationDir)
			if err != nil {
				return fmt.Errorf("ERROR: failed to create ConfigMap VolumeMounts for custom package repo config: %w", err)
			}
			gdsContainer.VolumeMounts = append(gdsContainer.VolumeMounts, volumeMounts...)
		}

		// set any custom ssl key/certificate configuration provided
		if config.Driver.CertConfig != nil && config.Driver.CertConfig.Name != "" {
			destinationDir, err := getCertConfigPath()
			if err != nil {
				return fmt.Errorf("ERROR: failed to get destination directory for ssl key/cert config: %w", err)
			}
			volumeMounts, _, err := createConfigMapVolumeMounts(n, config.Driver.CertConfig.Name, destinationDir)
			if err != nil {
				return fmt.Errorf("ERROR: failed to create ConfigMap VolumeMounts for custom certs: %w", err)
			}
			gdsContainer.VolumeMounts = append(gdsContainer.VolumeMounts, volumeMounts...)
		}

		secretName := config.Driver.SecretEnv
		if len(secretName) > 0 {
			err := createSecretEnvReference(n.ctx, n.client, secretName, n.operatorNamespace, gdsContainer)
			if err != nil {
				return fmt.Errorf("ERROR: failed to attach secret %s to the driver container: %w", secretName, err)
			}
		}

		// transform the nvidia-fs-ctr to use the openshift driver toolkit
		// notify openshift driver toolkit container GDS is enabled
		err = transformOpenShiftDriverToolkitContainer(obj, config, n, "nvidia-fs-ctr")
		if err != nil {
			return fmt.Errorf("ERROR: failed to transform the Driver Toolkit Container: %s", err)
		}
		if config.Driver.Resources != nil {
			gdsContainer.Resources = corev1.ResourceRequirements{
				Requests: config.Driver.Resources.Requests,
				Limits:   config.Driver.Resources.Limits,
			}
		}
	}
	return nil
}

func transformGDRCopyContainer(obj *appsv1.DaemonSet, config *gpuv1.ClusterPolicySpec, n ClusterPolicyController) error {
	for i, container := range obj.Spec.Template.Spec.Containers {
		// skip if not nvidia-gdrcopy
		if !strings.HasPrefix(container.Name, "nvidia-gdrcopy") {
			continue
		}
		if config.GDRCopy == nil || !config.GDRCopy.IsEnabled() {
			n.logger.Info("GDRCopy is disabled")
			// remove nvidia-gdrcopy sidecar container from driver Daemonset if gdrcopy is not enabled
			obj.Spec.Template.Spec.Containers = append(obj.Spec.Template.Spec.Containers[:i], obj.Spec.Template.Spec.Containers[i+1:]...)
			return nil
		}
		if config.Driver.UsePrecompiledDrivers() {
			return fmt.Errorf("GDRCopy is not supported along with pre-compiled NVIDIA drivers")
		}

		gdrcopyContainer := &obj.Spec.Template.Spec.Containers[i]

		// update nvidia-gdrcopy image and pull policy
		gdrcopyImage, err := resolveDriverTag(n, config.GDRCopy)
		if err != nil {
			return err
		}
		if gdrcopyImage != "" {
			gdrcopyContainer.Image = gdrcopyImage
		}
		if config.GDRCopy.ImagePullPolicy != "" {
			gdrcopyContainer.ImagePullPolicy = gpuv1.ImagePullPolicy(config.GDRCopy.ImagePullPolicy)
		}

		// set image pull secrets
		if len(config.GDRCopy.ImagePullSecrets) > 0 {
			addPullSecrets(&obj.Spec.Template.Spec, config.GDRCopy.ImagePullSecrets)
		}

		// set/append environment variables for gdrcopy container
		if len(config.GDRCopy.Env) > 0 {
			for _, env := range config.GDRCopy.Env {
				setContainerEnv(gdrcopyContainer, env.Name, env.Value)
			}
		}

		if config.Driver.RepoConfig != nil && config.Driver.RepoConfig.ConfigMapName != "" {
			// note: transformDriverContainer() will have already created a Volume backed by the ConfigMap.
			// Only add a VolumeMount for nvidia-gdrcopy-ctr.
			destinationDir, err := getRepoConfigPath()
			if err != nil {
				return fmt.Errorf("ERROR: failed to get destination directory for custom repo config: %w", err)
			}
			volumeMounts, _, err := createConfigMapVolumeMounts(n, config.Driver.RepoConfig.ConfigMapName, destinationDir)
			if err != nil {
				return fmt.Errorf("ERROR: failed to create ConfigMap VolumeMounts for custom package repo config: %w", err)
			}
			gdrcopyContainer.VolumeMounts = append(gdrcopyContainer.VolumeMounts, volumeMounts...)
		}

		// set any custom ssl key/certificate configuration provided
		if config.Driver.CertConfig != nil && config.Driver.CertConfig.Name != "" {
			destinationDir, err := getCertConfigPath()
			if err != nil {
				return fmt.Errorf("ERROR: failed to get destination directory for ssl key/cert config: %w", err)
			}
			volumeMounts, _, err := createConfigMapVolumeMounts(n, config.Driver.CertConfig.Name, destinationDir)
			if err != nil {
				return fmt.Errorf("ERROR: failed to create ConfigMap VolumeMounts for custom certs: %w", err)
			}
			gdrcopyContainer.VolumeMounts = append(gdrcopyContainer.VolumeMounts, volumeMounts...)
		}

		secretName := config.Driver.SecretEnv
		if len(secretName) > 0 {
			err := createSecretEnvReference(n.ctx, n.client, secretName, n.operatorNamespace, gdrcopyContainer)
			if err != nil {
				return fmt.Errorf("ERROR: failed to attach secret %s to the driver container: %w", secretName, err)
			}
		}

		// transform the nvidia-gdrcopy-ctr to use the openshift driver toolkit
		// notify openshift driver toolkit container that gdrcopy is enabled
		err = transformOpenShiftDriverToolkitContainer(obj, config, n, "nvidia-gdrcopy-ctr")
		if err != nil {
			return fmt.Errorf("ERROR: failed to transform the Driver Toolkit Container: %w", err)
		}
		if config.Driver.Resources != nil {
			gdrcopyContainer.Resources = corev1.ResourceRequirements{
				Requests: config.Driver.Resources.Requests,
				Limits:   config.Driver.Resources.Limits,
			}
		}
	}
	return nil
}

// getSanitizedKernelVersion returns kernelVersion with following changes
// 1. Remove arch suffix (as we use multi-arch images) and
// 2. ensure to meet k8s constraints for metadata.name, i.e it
// must consist of lower case alphanumeric characters, '-' or '.', and must start and end with an alphanumeric character
func getSanitizedKernelVersion(kernelVersion string) string {
	archRegex := regexp.MustCompile("x86_64(?:_64k)?|aarch64(?:_64k)?")
	// remove arch strings, "_" and any trailing "." from the kernel version
	sanitizedVersion := strings.TrimSuffix(strings.ReplaceAll(archRegex.ReplaceAllString(kernelVersion, ""), "_", "."), ".")
	return strings.ToLower(sanitizedVersion)
}

func transformPrecompiledDriverDaemonset(obj *appsv1.DaemonSet, n ClusterPolicyController) (err error) {
	sanitizedVersion := getSanitizedKernelVersion(n.currentKernelVersion)
	// prepare the DaemonSet to be kernel-version specific
	obj.Name += "-" + sanitizedVersion + "-" + n.kernelVersionMap[n.currentKernelVersion]

	// add unique labels for each kernel-version specific Daemonset
	obj.Labels[precompiledIdentificationLabelKey] = precompiledIdentificationLabelValue
	obj.Spec.Template.Labels[precompiledIdentificationLabelKey] = precompiledIdentificationLabelValue

	// append kernel-version specific node-selector
	obj.Spec.Template.Spec.NodeSelector[nfdKernelLabelKey] = n.currentKernelVersion
	return nil
}

func transformOpenShiftDriverToolkitContainer(obj *appsv1.DaemonSet, config *gpuv1.ClusterPolicySpec, n ClusterPolicyController, operandContainerName string) error {
	var err error

	getContainer := func(name string, remove bool) (*corev1.Container, error) {
		for i, container := range obj.Spec.Template.Spec.Containers {
			if container.Name != name {
				continue
			}
			if !remove {
				return &obj.Spec.Template.Spec.Containers[i], nil
			}

			obj.Spec.Template.Spec.Containers = append(obj.Spec.Template.Spec.Containers[:i],
				obj.Spec.Template.Spec.Containers[i+1:]...)
			return nil, nil
		}

		// if a container is not found, then it must have been removed already, return success
		if remove {
			return nil, nil
		}

		return nil, fmt.Errorf("could not find the '%s' container", name)
	}

	if !n.ocpDriverToolkit.enabled {
		if n.ocpDriverToolkit.requested {
			n.logger.Info("OpenShift DriverToolkit was requested but could not be enabled (dependencies missing)")
		}

		/* remove OpenShift Driver Toolkit side-car container from the Driver DaemonSet */
		_, err = getContainer("openshift-driver-toolkit-ctr", true)
		return err
	}

	/* find the main container and driver-toolkit sidecar container */
	var operandMainContainer, driverToolkitContainer *corev1.Container
	if operandMainContainer, err = getContainer(operandContainerName, false); err != nil {
		return err
	}

	if driverToolkitContainer, err = getContainer("openshift-driver-toolkit-ctr", false); err != nil {
		return err
	}

	/* prepare the DaemonSet to be RHCOS-version specific */
	rhcosVersion := n.ocpDriverToolkit.currentRhcosVersion

	if !strings.Contains(obj.Name, rhcosVersion) {
		obj.Name += "-" + rhcosVersion
	}
	obj.Labels["app"] = obj.Name
	obj.Spec.Selector.MatchLabels["app"] = obj.Name
	obj.Spec.Template.Labels["app"] = obj.Name

	obj.Labels[ocpDriverToolkitVersionLabel] = rhcosVersion
	obj.Spec.Template.Spec.NodeSelector[nfdOSTreeVersionLabelKey] = rhcosVersion

	/* prepare the DaemonSet to be searchable */
	obj.Labels[ocpDriverToolkitIdentificationLabel] = ocpDriverToolkitIdentificationValue
	obj.Spec.Template.Labels[ocpDriverToolkitIdentificationLabel] = ocpDriverToolkitIdentificationValue

	/* prepare the DriverToolkit container */
	setContainerEnv(driverToolkitContainer, "RHCOS_VERSION", rhcosVersion)

	if config.GPUDirectStorage != nil && config.GPUDirectStorage.IsEnabled() {
		setContainerEnv(driverToolkitContainer, "GDS_ENABLED", "true")
		n.logger.V(2).Info("transformOpenShiftDriverToolkitContainer", "GDS_ENABLED", config.GPUDirectStorage.IsEnabled())
	}

	if config.GDRCopy != nil && config.GDRCopy.IsEnabled() {
		setContainerEnv(driverToolkitContainer, "GDRCOPY_ENABLED", "true")
		n.logger.V(2).Info("transformOpenShiftDriverToolkitContainer", "GDRCOPY_ENABLED", "true")
	}

	image := n.ocpDriverToolkit.rhcosDriverToolkitImages[n.ocpDriverToolkit.currentRhcosVersion]
	if image != "" {
		driverToolkitContainer.Image = image
		n.logger.Info("DriverToolkit", "image", driverToolkitContainer.Image)
	} else {
		/* RHCOS tag missing in the Driver-Toolkit imagestream, setup fallback */
		obj.Labels["openshift.driver-toolkit.rhcos-image-missing"] = "true"
		obj.Spec.Template.Labels["openshift.driver-toolkit.rhcos-image-missing"] = "true"

		driverToolkitContainer.Image = operandMainContainer.Image
		setContainerEnv(operandMainContainer, "RHCOS_IMAGE_MISSING", "true")
		setContainerEnv(operandMainContainer, "RHCOS_VERSION", rhcosVersion)
		setContainerEnv(driverToolkitContainer, "RHCOS_IMAGE_MISSING", "true")

		n.logger.Info("WARNING: DriverToolkit image tag missing. Version-specific fallback mode enabled.", "rhcosVersion", rhcosVersion)
	}

	/* prepare the main container to start from the DriverToolkit entrypoint */
	switch operandContainerName {
	case "nvidia-fs-ctr":
		operandMainContainer.Command = []string{"ocp_dtk_entrypoint"}
		operandMainContainer.Args = []string{"nv-fs-ctr-run-with-dtk"}
	case "nvidia-gdrcopy-ctr":
		operandMainContainer.Command = []string{"ocp_dtk_entrypoint"}
		operandMainContainer.Args = []string{"gdrcopy-ctr-run-with-dtk"}
	default:
		operandMainContainer.Command = []string{"ocp_dtk_entrypoint"}
		operandMainContainer.Args = []string{"nv-ctr-run-with-dtk"}
	}

	/* prepare the shared volumes */
	// shared directory
	volSharedDirName, volSharedDirPath := "shared-nvidia-driver-toolkit", "/mnt/shared-nvidia-driver-toolkit"

	volMountSharedDir := corev1.VolumeMount{Name: volSharedDirName, MountPath: volSharedDirPath}
	operandMainContainer.VolumeMounts = append(operandMainContainer.VolumeMounts, volMountSharedDir)

	volSharedDir := corev1.Volume{
		Name: volSharedDirName,
		VolumeSource: corev1.VolumeSource{
			EmptyDir: &corev1.EmptyDirVolumeSource{},
		},
	}

	// Check if the volume already exists, if not add it
	for i := range obj.Spec.Template.Spec.Volumes {
		if obj.Spec.Template.Spec.Volumes[i].Name == volSharedDirName {
			// already exists, avoid duplicated volume
			return nil
		}
	}
	obj.Spec.Template.Spec.Volumes = append(obj.Spec.Template.Spec.Volumes, volSharedDir)

	// set resource limits
	if config.Driver.Resources != nil {
		driverToolkitContainer.Resources = corev1.ResourceRequirements{
			Requests: config.Driver.Resources.Requests,
			Limits:   config.Driver.Resources.Limits,
		}
	}
	return nil
}

// resolveDriverTag resolves image tag based on the OS of the worker node
func resolveDriverTag(n ClusterPolicyController, driverSpec interface{}) (string, error) {
	// obtain os version
	kvers, osTag, _ := kernelFullVersion(n)
	if kvers == "" {
		return "", fmt.Errorf("ERROR: Could not find kernel full version: ('%s', '%s')", kvers, osTag)
	}

	// obtain image path
	var image string
	var err error
	switch v := driverSpec.(type) {
	case *gpuv1.DriverSpec:
		spec := driverSpec.(*gpuv1.DriverSpec)
		// check if this is pre-compiled driver deployment.
		if spec.UsePrecompiledDrivers() {
			if spec.Repository == "" && spec.Version == "" {
				if spec.Image != "" {
					// this is useful for tools like kbld(carvel) which will just specify driver.image param as path:version
					image = spec.Image + "-" + n.currentKernelVersion
				} else {
					return "", fmt.Errorf("unable to resolve driver image path for pre-compiled drivers, driver.repository, driver.image and driver.version have to be specified in the ClusterPolicy")
				}
			} else {
				// use per kernel version tag
				image = spec.Repository + "/" + spec.Image + ":" + spec.Version + "-" + n.currentKernelVersion
			}
		} else {
			image, err = gpuv1.ImagePath(spec)
			if err != nil {
				return "", err
			}
		}
	case *gpuv1.GPUDirectStorageSpec:
		spec := driverSpec.(*gpuv1.GPUDirectStorageSpec)
		image, err = gpuv1.ImagePath(spec)
		if err != nil {
			return "", err
		}
	case *gpuv1.VGPUManagerSpec:
		spec := driverSpec.(*gpuv1.VGPUManagerSpec)
		image, err = gpuv1.ImagePath(spec)
		if err != nil {
			return "", err
		}
	case *gpuv1.GDRCopySpec:
		spec := driverSpec.(*gpuv1.GDRCopySpec)
		image, err = gpuv1.ImagePath(spec)
		if err != nil {
			return "", err
		}
	default:
		return "", fmt.Errorf("invalid type to construct image path: %v", v)
	}

	// if image digest is specified, use it directly
	if !strings.Contains(image, "sha256:") {
		// append os-tag to the provided driver version
		image = fmt.Sprintf("%s-%s", image, osTag)
	}
	return image, nil
}

// getRepoConfigPath returns the standard OS specific path for repository configuration files
func getRepoConfigPath() (string, error) {
	release, err := parseOSRelease()
	if err != nil {
		return "", err
	}

	os := release["ID"]
	if path, ok := RepoConfigPathMap[os]; ok {
		return path, nil
	}
	return "", fmt.Errorf("distribution not supported")
}

// getCertConfigPath returns the standard OS specific path for ssl keys/certificates
func getCertConfigPath() (string, error) {
	release, err := parseOSRelease()
	if err != nil {
		return "", err
	}

	os := release["ID"]
	if path, ok := CertConfigPathMap[os]; ok {
		return path, nil
	}
	return "", fmt.Errorf("distribution not supported")
}

// getSubscriptionPathsToVolumeSources returns the MountPathToVolumeSource map containing all
// OS-specific subscription/entitlement paths that need to be mounted in the container.
func getSubscriptionPathsToVolumeSources() (MountPathToVolumeSource, error) {
	release, err := parseOSRelease()
	if err != nil {
		return nil, err
	}

	os := release["ID"]
	if pathToVolumeSource, ok := SubscriptionPathMap[os]; ok {
		return pathToVolumeSource, nil
	}
	return nil, fmt.Errorf("distribution not supported")
}

// createConfigMapVolumeMounts creates a VolumeMount for each key
// in the ConfigMap. Use subPath to ensure original contents
// at destinationDir are not overwritten.
func createConfigMapVolumeMounts(n ClusterPolicyController, configMapName string, destinationDir string) ([]corev1.VolumeMount, []corev1.KeyToPath, error) {
	ctx := n.ctx
	// get the ConfigMap
	cm := &corev1.ConfigMap{}
	opts := client.ObjectKey{Namespace: n.operatorNamespace, Name: configMapName}
	err := n.client.Get(ctx, opts, cm)
	if err != nil {
		return nil, nil, fmt.Errorf("ERROR: could not get ConfigMap %s from client: %v", configMapName, err)
	}

	// create one volume mount per file in the ConfigMap and use subPath
	var filenames []string
	for filename := range cm.Data {
		filenames = append(filenames, filename)
	}
	// sort so volume mounts are added to spec in deterministic order
	sort.Strings(filenames)
	var itemsToInclude []corev1.KeyToPath
	var volumeMounts []corev1.VolumeMount
	for _, filename := range filenames {
		volumeMounts = append(volumeMounts,
			corev1.VolumeMount{Name: configMapName, ReadOnly: true, MountPath: filepath.Join(destinationDir, filename), SubPath: filename})
		itemsToInclude = append(itemsToInclude, corev1.KeyToPath{
			Key:  filename,
			Path: filename,
		})
	}
	return volumeMounts, itemsToInclude, nil
}

func createConfigMapVolume(configMapName string, itemsToInclude []corev1.KeyToPath) corev1.Volume {
	volumeSource := corev1.VolumeSource{
		ConfigMap: &corev1.ConfigMapVolumeSource{
			LocalObjectReference: corev1.LocalObjectReference{
				Name: configMapName,
			},
			Items: itemsToInclude,
		},
	}
	return corev1.Volume{Name: configMapName, VolumeSource: volumeSource}
}

func createEmptyDirVolume(volumeName string) corev1.Volume {
	return corev1.Volume{
		Name: volumeName,
		VolumeSource: corev1.VolumeSource{
			EmptyDir: &corev1.EmptyDirVolumeSource{},
		},
	}
}

func applyLicensingConfig(obj *appsv1.DaemonSet, config *gpuv1.ClusterPolicySpec, driverContainer *corev1.Container) {
	podSpec := &obj.Spec.Template.Spec

	// add new volume mount
	licensingConfigVolMount := corev1.VolumeMount{Name: "licensing-config", ReadOnly: true, MountPath: consts.VGPULicensingConfigMountPath, SubPath: consts.VGPULicensingFileName}
	driverContainer.VolumeMounts = append(driverContainer.VolumeMounts, licensingConfigVolMount)

	// gridd.conf always mounted
	licenseItemsToInclude := []corev1.KeyToPath{
		{
			Key:  consts.VGPULicensingFileName,
			Path: consts.VGPULicensingFileName,
		},
	}
	// client config token only mounted when NLS is enabled
	if config.Driver.LicensingConfig.IsNLSEnabled() {
		licenseItemsToInclude = append(licenseItemsToInclude, corev1.KeyToPath{
			Key:  consts.NLSClientTokenFileName,
			Path: consts.NLSClientTokenFileName,
		})
		nlsTokenVolMount := corev1.VolumeMount{Name: "licensing-config", ReadOnly: true, MountPath: consts.NLSClientTokenMountPath, SubPath: consts.NLSClientTokenFileName}
		driverContainer.VolumeMounts = append(driverContainer.VolumeMounts, nlsTokenVolMount)
	}

	var licensingConfigVolumeSource corev1.VolumeSource
	if config.Driver.LicensingConfig.SecretName != "" {
		licensingConfigVolumeSource = corev1.VolumeSource{
			Secret: &corev1.SecretVolumeSource{
				SecretName: config.Driver.LicensingConfig.SecretName,
				Items:      licenseItemsToInclude,
			},
		}
	} else if config.Driver.LicensingConfig.ConfigMapName != "" {
		licensingConfigVolumeSource = corev1.VolumeSource{
			ConfigMap: &corev1.ConfigMapVolumeSource{
				LocalObjectReference: corev1.LocalObjectReference{
					Name: config.Driver.LicensingConfig.ConfigMapName,
				},
				Items: licenseItemsToInclude,
			},
		}
	}
	licensingConfigVol := corev1.Volume{Name: "licensing-config", VolumeSource: licensingConfigVolumeSource}
	podSpec.Volumes = append(podSpec.Volumes, licensingConfigVol)
}

func transformDriverContainer(obj *appsv1.DaemonSet, config *gpuv1.ClusterPolicySpec, n ClusterPolicyController) error {
	podSpec := &obj.Spec.Template.Spec
	driverContainer := findContainerByName(podSpec.Containers, "nvidia-driver-ctr")
	if driverContainer == nil {
		return fmt.Errorf("driver container (nvidia-driver-ctr) is missing from the driver daemonset manifest")
	}

	image, err := resolveDriverTag(n, &config.Driver)
	if err != nil {
		return err
	}
	if image != "" {
		driverContainer.Image = image
	}

	// update image pull policy
	driverContainer.ImagePullPolicy = gpuv1.ImagePullPolicy(config.Driver.ImagePullPolicy)

	// set image pull secrets
	if len(config.Driver.ImagePullSecrets) > 0 {
		addPullSecrets(&obj.Spec.Template.Spec, config.Driver.ImagePullSecrets)
	}
	// set resource limits
	if config.Driver.Resources != nil {
		driverContainer.Resources.Requests = config.Driver.Resources.Requests
		driverContainer.Resources.Limits = config.Driver.Resources.Limits
	}
	// set arguments if specified for driver container
	if len(config.Driver.Args) > 0 {
		driverContainer.Args = config.Driver.Args
	}

	if len(config.Driver.KernelModuleType) > 0 {
		setContainerEnv(driverContainer, KernelModuleTypeEnvName, config.Driver.KernelModuleType)
		// we set the "OPEN_KERNEL_MODULES_ENABLED" envar for backwards compatibility with older driver containers
		if config.Driver.OpenKernelModulesEnabled() {
			setContainerEnv(driverContainer, OpenKernelModulesEnabledEnvName, "true")
		}
	}

	// set container probe timeouts
	if config.Driver.StartupProbe != nil {
		setContainerProbe(driverContainer, config.Driver.StartupProbe, Startup)
	}
	if config.Driver.LivenessProbe != nil {
		setContainerProbe(driverContainer, config.Driver.LivenessProbe, Liveness)
	}
	if config.Driver.ReadinessProbe != nil {
		setContainerProbe(driverContainer, config.Driver.ReadinessProbe, Readiness)
	}

	if config.GDRCopy != nil && config.GDRCopy.IsEnabled() {
		// set env indicating gdrcopy is enabled
		setContainerEnv(driverContainer, GDRCopyEnabledEnvName, "true")
	}
	if config.GPUDirectStorage != nil && config.GPUDirectStorage.IsEnabled() {
		// set env indicating gds is enabled
		setContainerEnv(driverContainer, GDSEnabledEnvName, "true")
	}

	if config.Driver.GPUDirectRDMA != nil && config.Driver.GPUDirectRDMA.IsEnabled() {
		// set env indicating nvidia-peermem is enabled to compile module with required ib_* interfaces
		setContainerEnv(driverContainer, GPUDirectRDMAEnabledEnvName, "true")
		// check if MOFED drives are directly installed on host and update source path accordingly
		// to build nvidia-peermem module
		if config.Driver.GPUDirectRDMA.UseHostMOFED != nil && *config.Driver.GPUDirectRDMA.UseHostMOFED {
			// mount /usr/src/ofa_kernel path directly from host to build using MOFED drivers installed on host
			for index, volume := range podSpec.Volumes {
				if volume.Name == "mlnx-ofed-usr-src" {
					podSpec.Volumes[index].HostPath.Path = "/usr/src"
				}
			}
			// set env indicating host-mofed is enabled
			setContainerEnv(driverContainer, UseHostMOFEDEnvName, "true")
		}
	}

	// set any licensing configuration required
	if config.Driver.IsVGPULicensingEnabled() {
		applyLicensingConfig(obj, config, driverContainer)
	}

	// set virtual topology daemon configuration if specified for vGPU driver
	if config.Driver.VirtualTopology != nil && config.Driver.VirtualTopology.Config != "" {
		topologyConfigVolMount := corev1.VolumeMount{Name: "topology-config", ReadOnly: true, MountPath: consts.VGPUTopologyConfigMountPath, SubPath: consts.VGPUTopologyConfigFileName}
		driverContainer.VolumeMounts = append(driverContainer.VolumeMounts, topologyConfigVolMount)

		topologyConfigVolumeSource := corev1.VolumeSource{
			ConfigMap: &corev1.ConfigMapVolumeSource{
				LocalObjectReference: corev1.LocalObjectReference{
					Name: config.Driver.VirtualTopology.Config,
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
		podSpec.Volumes = append(podSpec.Volumes, topologyConfigVol)
	}

	// mount any custom kernel module configuration parameters at /drivers
	if config.Driver.KernelModuleConfig != nil && config.Driver.KernelModuleConfig.Name != "" {
		destinationDir := "/drivers"
		volumeMounts, itemsToInclude, err := createConfigMapVolumeMounts(n, config.Driver.KernelModuleConfig.Name, destinationDir)
		if err != nil {
			return fmt.Errorf("ERROR: failed to create ConfigMap VolumeMounts for kernel module configuration: %v", err)
		}
		driverContainer.VolumeMounts = append(driverContainer.VolumeMounts, volumeMounts...)
		podSpec.Volumes = append(podSpec.Volumes, createConfigMapVolume(config.Driver.KernelModuleConfig.Name, itemsToInclude))
	}

	if len(config.Driver.Env) > 0 {
		for _, env := range config.Driver.Env {
			setContainerEnv(&(obj.Spec.Template.Spec.Containers[0]), env.Name, env.Value)
		}
	}

	// no further repo configuration required when using pre-compiled drivers, return here.
	if config.Driver.UsePrecompiledDrivers() {
		return nil
	}

	// set any custom repo configuration provided when using runfile based driver installation
	if config.Driver.RepoConfig != nil && config.Driver.RepoConfig.ConfigMapName != "" {
		destinationDir, err := getRepoConfigPath()
		if err != nil {
			return fmt.Errorf("ERROR: failed to get destination directory for custom repo config: %v", err)
		}
		volumeMounts, itemsToInclude, err := createConfigMapVolumeMounts(n, config.Driver.RepoConfig.ConfigMapName, destinationDir)
		if err != nil {
			return fmt.Errorf("ERROR: failed to create ConfigMap VolumeMounts for custom repo config: %v", err)
		}
		driverContainer.VolumeMounts = append(driverContainer.VolumeMounts, volumeMounts...)
		podSpec.Volumes = append(podSpec.Volumes, createConfigMapVolume(config.Driver.RepoConfig.ConfigMapName, itemsToInclude))
	}

	// set any custom ssl key/certificate configuration provided
	if config.Driver.CertConfig != nil && config.Driver.CertConfig.Name != "" {
		destinationDir, err := getCertConfigPath()
		if err != nil {
			return fmt.Errorf("ERROR: failed to get destination directory for custom repo config: %v", err)
		}
		volumeMounts, itemsToInclude, err := createConfigMapVolumeMounts(n, config.Driver.CertConfig.Name, destinationDir)
		if err != nil {
			return fmt.Errorf("ERROR: failed to create ConfigMap VolumeMounts for custom certs: %w", err)
		}
		driverContainer.VolumeMounts = append(driverContainer.VolumeMounts, volumeMounts...)
		podSpec.Volumes = append(podSpec.Volumes, createConfigMapVolume(config.Driver.CertConfig.Name, itemsToInclude))
	}

	secretName := config.Driver.SecretEnv
	if len(secretName) > 0 {
		err := createSecretEnvReference(n.ctx, n.client, secretName, n.operatorNamespace, driverContainer)
		if err != nil {
			return fmt.Errorf("ERROR: failed to attach secret %s to the driver container: %w", secretName, err)
		}
	}

	release, err := parseOSRelease()
	if err != nil {
		return fmt.Errorf("ERROR: failed to get os-release: %s", err)
	}

	// set up subscription entitlements for RHEL(using K8s with a non-CRIO runtime) and SLES
	if (release["ID"] == "rhel" && n.openshift == "" && n.runtime != gpuv1.CRIO) || release["ID"] == "sles" || release["ID"] == "sl-micro" {
		n.logger.Info("Mounting subscriptions into the driver container", "OS", release["ID"])
		pathToVolumeSource, err := getSubscriptionPathsToVolumeSources()
		if err != nil {
			return fmt.Errorf("ERROR: failed to get path items for subscription entitlements: %v", err)
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
			driverContainer.VolumeMounts = append(driverContainer.VolumeMounts, volMountSubscription)

			subscriptionVol := corev1.Volume{Name: volMountSubscriptionName, VolumeSource: pathToVolumeSource[mountPath]}
			podSpec.Volumes = append(podSpec.Volumes, subscriptionVol)
		}
	}

	// apply proxy and env settings if this is an OpenShift cluster
	if _, ok := release["OPENSHIFT_VERSION"]; ok {
		setContainerEnv(driverContainer, "OPENSHIFT_VERSION", release["OPENSHIFT_VERSION"])

		// Automatically apply proxy settings for OCP and inject custom CA if configured by user
		// https://docs.openshift.com/container-platform/4.6/networking/configuring-a-custom-pki.html
		err = applyOCPProxySpec(n, podSpec)
		if err != nil {
			return err
		}
	}
	return nil
}

func createSecretEnvReference(ctx context.Context, ctrlClient client.Client, secretName string,
	namespace string, container *corev1.Container) error {
	envFrom := container.EnvFrom
	if len(envFrom) == 0 {
		envFrom = make([]corev1.EnvFromSource, 0)
	}

	// get the ConfigMap
	sec := &corev1.Secret{}
	opts := client.ObjectKey{Namespace: namespace, Name: secretName}
	err := ctrlClient.Get(ctx, opts, sec)
	if err != nil {
		return fmt.Errorf("ERROR: could not get Secret %s from client: %w", secretName, err)
	}

	secretEnvSource := corev1.EnvFromSource{
		SecretRef: &corev1.SecretEnvSource{
			LocalObjectReference: corev1.LocalObjectReference{
				Name: sec.Name,
			},
		}}
	envFrom = append(envFrom, secretEnvSource)
	container.EnvFrom = envFrom
	return nil
}

func transformVGPUManagerContainer(obj *appsv1.DaemonSet, config *gpuv1.ClusterPolicySpec, n ClusterPolicyController) error {
	container := findContainerByName(obj.Spec.Template.Spec.Containers, "nvidia-vgpu-manager-ctr")

	if container == nil {
		return fmt.Errorf("failed to find nvidia-vgpu-manager-ctr in spec")
	}

	image, err := resolveDriverTag(n, &config.VGPUManager)
	if err != nil {
		return err
	}
	if image != "" {
		container.Image = image
	}

	// update image pull policy
	container.ImagePullPolicy = gpuv1.ImagePullPolicy(config.VGPUManager.ImagePullPolicy)

	// set image pull secrets
	if len(config.VGPUManager.ImagePullSecrets) > 0 {
		addPullSecrets(&obj.Spec.Template.Spec, config.VGPUManager.ImagePullSecrets)
	}
	// set resource limits
	if config.VGPUManager.Resources != nil {
		container.Resources.Requests = config.VGPUManager.Resources.Requests
		container.Resources.Limits = config.VGPUManager.Resources.Limits
	}
	// set arguments if specified for driver container
	if len(config.VGPUManager.Args) > 0 {
		container.Args = config.VGPUManager.Args
	}

	release, err := parseOSRelease()
	if err != nil {
		return fmt.Errorf("ERROR: failed to get os-release: %s", err)
	}

	// add env for OCP
	if _, ok := release["OPENSHIFT_VERSION"]; ok {
		setContainerEnv(container, "OPENSHIFT_VERSION", release["OPENSHIFT_VERSION"])
	}

	if len(config.VGPUManager.Env) > 0 {
		for _, env := range config.VGPUManager.Env {
			setContainerEnv(&(obj.Spec.Template.Spec.Containers[0]), env.Name, env.Value)
		}
	}

	return nil
}

func applyUpdateStrategyConfig(obj *appsv1.DaemonSet, config *gpuv1.ClusterPolicySpec) error {
	switch config.Daemonsets.UpdateStrategy {
	case "OnDelete":
		obj.Spec.UpdateStrategy = appsv1.DaemonSetUpdateStrategy{Type: appsv1.OnDeleteDaemonSetStrategyType}
	case "RollingUpdate":
		fallthrough
	default:
		// update config for RollingUpdate strategy
		if config.Daemonsets.RollingUpdate == nil || config.Daemonsets.RollingUpdate.MaxUnavailable == "" {
			return nil
		}
		if strings.HasPrefix(obj.Name, commonDriverDaemonsetName) {
			// disallow setting RollingUpdate strategy with the driver container
			return nil
		}
		var intOrString intstr.IntOrString
		if strings.HasSuffix(config.Daemonsets.RollingUpdate.MaxUnavailable, "%") {
			intOrString = intstr.IntOrString{Type: intstr.String, StrVal: config.Daemonsets.RollingUpdate.MaxUnavailable}
		} else {
			int64Val, err := strconv.ParseInt(config.Daemonsets.RollingUpdate.MaxUnavailable, 10, 32)
			if err != nil {
				return fmt.Errorf("failed to apply rolling update config: %s", err)
			}
			intOrString = intstr.IntOrString{Type: intstr.Int, IntVal: int32(int64Val)}
		}
		rollingUpdateSpec := appsv1.RollingUpdateDaemonSet{MaxUnavailable: &intOrString}
		obj.Spec.UpdateStrategy = appsv1.DaemonSetUpdateStrategy{Type: appsv1.RollingUpdateDaemonSetStrategyType, RollingUpdate: &rollingUpdateSpec}
	}
	return nil
}

func transformValidationInitContainer(obj *appsv1.DaemonSet, config *gpuv1.ClusterPolicySpec) error {
	for i, initContainer := range obj.Spec.Template.Spec.InitContainers {
		// skip if not validation initContainer
		if !strings.Contains(initContainer.Name, "validation") {
			continue
		}

		// TODO: refactor the component-specific validation logic so that we are not duplicating TransformValidatorComponent()
		// Pass env for driver-validation init container
		if strings.HasPrefix(initContainer.Name, "driver") {
			if len(config.Validator.Driver.Env) > 0 {
				for _, env := range config.Validator.Driver.Env {
					setContainerEnv(&(obj.Spec.Template.Spec.InitContainers[i]), env.Name, env.Value)
				}
			}
		}

		// Pass env for toolkit-validation init container
		if strings.HasPrefix(initContainer.Name, "toolkit") {
			if len(config.Validator.Toolkit.Env) > 0 {
				for _, env := range config.Validator.Toolkit.Env {
					setContainerEnv(&(obj.Spec.Template.Spec.InitContainers[i]), env.Name, env.Value)
				}
			}
		}

		// update validation image
		image, err := gpuv1.ImagePath(&config.Validator)
		if err != nil {
			return err
		}
		obj.Spec.Template.Spec.InitContainers[i].Image = image
		// update validation image pull policy
		if config.Validator.ImagePullPolicy != "" {
			obj.Spec.Template.Spec.InitContainers[i].ImagePullPolicy = gpuv1.ImagePullPolicy(config.Validator.ImagePullPolicy)
		}
		// update the security context for the validator container
		transformValidatorSecurityContext(&obj.Spec.Template.Spec.InitContainers[i])
	}
	// add any pull secrets needed for validation image
	if len(config.Validator.ImagePullSecrets) > 0 {
		addPullSecrets(&obj.Spec.Template.Spec, config.Validator.ImagePullSecrets)
	}
	return nil
}

func addPullSecrets(podSpec *corev1.PodSpec, secrets []string) {
	for _, secret := range secrets {
		if !containsSecret(podSpec.ImagePullSecrets, secret) {
			podSpec.ImagePullSecrets = append(podSpec.ImagePullSecrets, corev1.LocalObjectReference{Name: secret})
		}
	}
}

func containsSecret(secrets []corev1.LocalObjectReference, secretName string) bool {
	for _, s := range secrets {
		if s.Name == secretName {
			return true
		}
	}
	return false
}

func isDeploymentReady(name string, n ClusterPolicyController) gpuv1.State {
	opts := []client.ListOption{
		client.MatchingLabels{"app": name},
	}
	n.logger.V(1).Info("Deployment", "LabelSelector", fmt.Sprintf("app=%s", name))
	list := &appsv1.DeploymentList{}
	err := n.client.List(n.ctx, list, opts...)
	if err != nil {
		n.logger.Info("Could not get DeploymentList", err)
	}
	n.logger.V(1).Info("Deployment", "NumberOfDeployment", len(list.Items))
	if len(list.Items) == 0 {
		return gpuv1.NotReady
	}

	ds := list.Items[0]
	n.logger.V(1).Info("Deployment", "NumberUnavailable", ds.Status.UnavailableReplicas)

	if ds.Status.UnavailableReplicas != 0 {
		return gpuv1.NotReady
	}

	return isPodReady(name, n, "Running")
}

func isDaemonSetReady(name string, n ClusterPolicyController) gpuv1.State {
	ctx := n.ctx
	ds := &appsv1.DaemonSet{}
	n.logger.V(2).Info("checking daemonset for readiness", "name", name)
	err := n.client.Get(ctx, types.NamespacedName{Namespace: n.operatorNamespace, Name: name}, ds)
	if err != nil {
		n.logger.Error(err, "could not get daemonset", "name", name)
	}

	if ds.Status.DesiredNumberScheduled == 0 {
		n.logger.V(2).Info("Daemonset has desired pods of 0", "name", name)
		return gpuv1.Ready
	}

	if ds.Status.NumberUnavailable != 0 {
		n.logger.Info("daemonset not ready", "name", name)
		return gpuv1.NotReady
	}

	// if ds is running with "OnDelete" strategy, check if the revision matches for all pods
	if ds.Spec.UpdateStrategy.Type != appsv1.OnDeleteDaemonSetStrategyType {
		return gpuv1.Ready
	}

	opts := []client.ListOption{client.MatchingLabels(ds.Spec.Template.Labels)}

	n.logger.V(2).Info("Pod", "LabelSelector", fmt.Sprintf("app=%s", name))
	list := &corev1.PodList{}
	err = n.client.List(ctx, list, opts...)
	if err != nil {
		n.logger.Info("Could not get PodList", err)
		return gpuv1.NotReady
	}
	n.logger.V(2).Info("Pod", "NumberOfPods", len(list.Items))
	if len(list.Items) == 0 {
		return gpuv1.NotReady
	}

	dsPods := getPodsOwnedbyDaemonset(ds, list.Items, n)
	daemonsetRevisionHash, err := getDaemonsetControllerRevisionHash(ctx, ds, n)
	if err != nil {
		n.logger.Error(
			err, "Failed to get daemonset template revision hash", "daemonset", ds)
		return gpuv1.NotReady
	}
	n.logger.V(2).Info("daemonset template revision hash", "hash", daemonsetRevisionHash)

	for _, pod := range dsPods {
		pod := pod
		podRevisionHash, err := getPodControllerRevisionHash(ctx, &pod)
		if err != nil {
			n.logger.Error(
				err, "Failed to get pod template revision hash", "pod", pod)
			return gpuv1.NotReady
		}
		n.logger.V(2).Info("pod template revision hash", "hash", podRevisionHash)

		// check if the revision hashes are matching and pod is in running state
		if podRevisionHash != daemonsetRevisionHash || pod.Status.Phase != "Running" {
			return gpuv1.NotReady
		}

		// If the pod generation matches the daemonset generation and the pod is running
		// and it has at least 1 container
		if len(pod.Status.ContainerStatuses) != 0 {
			for i := range pod.Status.ContainerStatuses {
				if !pod.Status.ContainerStatuses[i].Ready {
					// Return false if at least 1 container isn't ready
					return gpuv1.NotReady
				}
			}
		}
	}

	// All containers are ready
	return gpuv1.Ready
}

func getPodsOwnedbyDaemonset(ds *appsv1.DaemonSet, pods []corev1.Pod, n ClusterPolicyController) []corev1.Pod {
	dsPodList := []corev1.Pod{}
	for _, pod := range pods {
		if len(pod.OwnerReferences) < 1 {
			n.logger.Info("Driver Pod has no owner DaemonSet", "pod", pod.Name)
			continue
		}
		n.logger.V(2).Info("Pod", "pod", pod.Name, "owner", pod.OwnerReferences[0].Name)

		if ds.UID != pod.OwnerReferences[0].UID {
			n.logger.Info("Driver Pod is not owned by a Driver DaemonSet",
				"pod", pod, "actual owner", pod.OwnerReferences[0])
			continue
		}
		dsPodList = append(dsPodList, pod)
	}
	return dsPodList
}

func getPodControllerRevisionHash(ctx context.Context, pod *corev1.Pod) (string, error) {
	if hash, ok := pod.Labels[PodControllerRevisionHashLabelKey]; ok {
		return hash, nil
	}
	return "", fmt.Errorf("controller-revision-hash label not present for pod %s", pod.Name)
}

func getDaemonsetControllerRevisionHash(ctx context.Context, daemonset *appsv1.DaemonSet, n ClusterPolicyController) (string, error) {

	// get all revisions for the daemonset
	opts := []client.ListOption{
		client.MatchingLabels(daemonset.Spec.Selector.MatchLabels),
		client.InNamespace(n.operatorNamespace),
	}
	list := &appsv1.ControllerRevisionList{}
	err := n.client.List(ctx, list, opts...)
	if err != nil {
		return "", fmt.Errorf("error getting controller revision list for daemonset %s: %v", daemonset.Name, err)
	}

	n.logger.V(2).Info("obtained controller revisions", "Daemonset", daemonset.Name, "len", len(list.Items))

	var revisions []appsv1.ControllerRevision
	for _, controllerRevision := range list.Items {
		if strings.HasPrefix(controllerRevision.Name, daemonset.Name) {
			revisions = append(revisions, controllerRevision)
		}
	}

	if len(revisions) == 0 {
		return "", fmt.Errorf("no revision found for daemonset %s", daemonset.Name)
	}

	// sort the revision list to make sure we obtain latest revision always
	sort.Slice(revisions, func(i, j int) bool { return revisions[i].Revision < revisions[j].Revision })

	currentRevision := revisions[len(revisions)-1]
	hash := strings.TrimPrefix(currentRevision.Name, fmt.Sprintf("%s-", daemonset.Name))

	return hash, nil
}

// Deployment creates Deployment resource
func Deployment(n ClusterPolicyController) (gpuv1.State, error) {
	ctx := n.ctx
	state := n.idx
	obj := n.resources[state].Deployment.DeepCopy()
	obj.Namespace = n.operatorNamespace

	logger := n.logger.WithValues("Deployment", obj.Name, "Namespace", obj.Namespace)

	// Check if state is disabled and cleanup resource if exists
	if !n.isStateEnabled(n.stateNames[n.idx]) {
		err := n.client.Delete(ctx, obj)
		if err != nil && !apierrors.IsNotFound(err) {
			logger.Info("Couldn't delete", "Error", err)
			return gpuv1.NotReady, err
		}
		return gpuv1.Disabled, nil
	}

	if err := controllerutil.SetControllerReference(n.singleton, obj, n.scheme); err != nil {
		return gpuv1.NotReady, err
	}

	if err := n.client.Create(ctx, obj); err != nil {
		if apierrors.IsAlreadyExists(err) {
			logger.Info("Found Resource, updating...")
			err = n.client.Update(ctx, obj)
			if err != nil {
				logger.Info("Couldn't update", "Error", err)
				return gpuv1.NotReady, err
			}
			return isDeploymentReady(obj.Name, n), nil
		}

		logger.Info("Couldn't create", "Error", err)
		return gpuv1.NotReady, err
	}

	return isDeploymentReady(obj.Name, n), nil
}

func ocpHasDriverToolkitImageStream(n *ClusterPolicyController) (bool, error) {
	ctx := n.ctx
	found := &apiimagev1.ImageStream{}
	name := "driver-toolkit"
	namespace := consts.OpenshiftNamespace
	err := n.client.Get(ctx, types.NamespacedName{Namespace: namespace, Name: name}, found)
	if err != nil {
		if apierrors.IsNotFound(err) {
			n.logger.Info("ocpHasDriverToolkitImageStream: driver-toolkit imagestream not found",
				"Name", name,
				"Namespace", namespace)

			return false, nil
		}

		n.logger.Info("Couldn't get the driver-toolkit imagestream", "Error", err)

		return false, err
	}
	n.logger.V(1).Info("ocpHasDriverToolkitImageStream: driver-toolkit imagestream found")
	isBroken := false
	for _, tag := range found.Spec.Tags {
		if tag.Name == "" {
			isBroken = true
			continue
		}
		if tag.Name == "latest" || tag.From == nil {
			continue
		}
		n.logger.V(1).Info("ocpHasDriverToolkitImageStream: tag", tag.Name, tag.From.Name)
		n.ocpDriverToolkit.rhcosDriverToolkitImages[tag.Name] = tag.From.Name
	}
	if isBroken {
		n.logger.Info("WARNING: ocpHasDriverToolkitImageStream: driver-toolkit imagestream is broken, see RHBZ#2015024")

		n.operatorMetrics.openshiftDriverToolkitIsBroken.Set(1)
	} else {
		n.operatorMetrics.openshiftDriverToolkitIsBroken.Set(0)
	}

	return true, nil
}

func (n ClusterPolicyController) cleanupAllDriverDaemonSets(ctx context.Context) error {
	// Get all DaemonSets owned by ClusterPolicy
	//
	// (cdesiniotis) There is a limitation with the controller-runtime client where only a single field selector
	// is allowed when specifying ListOptions or DeleteOptions.
	// See GH issue: https://github.com/kubernetes-sigs/controller-runtime/issues/612
	list := &appsv1.DaemonSetList{}
	err := n.client.List(ctx, list, client.MatchingFields{clusterPolicyControllerIndexKey: n.singleton.Name})
	if err != nil {
		return fmt.Errorf("failed to list all NVIDIA driver daemonsets owned by ClusterPolicy: %w", err)
	}

	for _, ds := range list.Items {
		ds := ds
		// filter out DaemonSets which are not the NVIDIA driver/vgpu-manager
		if strings.HasPrefix(ds.Name, commonDriverDaemonsetName) || strings.HasPrefix(ds.Name, commonVGPUManagerDaemonsetName) {
			n.logger.Info("Deleting NVIDIA driver daemonset owned by ClusterPolicy", "Name", ds.Name)
			err = n.client.Delete(ctx, &ds)
			if err != nil {
				return fmt.Errorf("error deleting NVIDIA driver daemonset: %w", err)
			}
		}
	}

	return nil
}

// cleanupStalePrecompiledDaemonsets deletes stale driver daemonsets which can happen
// 1. If all nodes upgraded to the latest kernel
// 2. no GPU nodes are present
func (n ClusterPolicyController) cleanupStalePrecompiledDaemonsets(ctx context.Context) error {
	opts := []client.ListOption{
		client.MatchingLabels{
			precompiledIdentificationLabelKey: precompiledIdentificationLabelValue,
		},
	}
	list := &appsv1.DaemonSetList{}
	err := n.client.List(ctx, list, opts...)
	if err != nil {
		n.logger.Error(err, "could not get daemonset list")
		return err
	}

	for idx := range list.Items {
		ds := list.Items[idx]
		name := ds.Name
		desiredNumberScheduled := ds.Status.DesiredNumberScheduled
		numberMisscheduled := ds.Status.NumberMisscheduled

		n.logger.V(1).Info("Driver DaemonSet found",
			"Name", name,
			"Status.DesiredNumberScheduled", desiredNumberScheduled)

		// We consider a daemonset to be stale only if it has no desired number of pods and no pods currently mis-scheduled
		// As per the Kubernetes docs, a daemonset pod is mis-scheduled when an already scheduled pod no longer satisfies
		// node affinity constraints or has un-tolerated taints, for e.g. "node.kubernetes.io/unreachable:NoSchedule"
		if desiredNumberScheduled == 0 && numberMisscheduled == 0 {
			n.logger.Info("Delete Driver DaemonSet", "Name", name)

			err = n.client.Delete(ctx, &ds)
			if err != nil {
				n.logger.Error(err, "Could not get delete DaemonSet",
					"Name", name)
			}
		} else {
			n.logger.Info("Driver DaemonSet active, keep it.",
				"Name", name,
				"Status.DesiredNumberScheduled", desiredNumberScheduled)
		}
	}
	return nil
}

// precompiledDriverDaemonsets goes through all the kernel versions
// found in the cluster, sets `currentKernelVersion` and calls the
// original DaemonSet() function to create/update the kernel-specific
// DaemonSet.
func precompiledDriverDaemonsets(ctx context.Context, n ClusterPolicyController) (gpuv1.State, []error) {
	overallState := gpuv1.Ready
	var errs []error
	n.logger.Info("cleaning any stale precompiled driver daemonsets")
	err := n.cleanupStalePrecompiledDaemonsets(ctx)
	if err != nil {
		return gpuv1.NotReady, append(errs, err)
	}

	n.logger.V(1).Info("preparing pre-compiled driver daemonsets")
	for kernelVersion, os := range n.kernelVersionMap {
		// set current kernel version
		n.currentKernelVersion = kernelVersion

		n.logger.Info("preparing pre-compiled driver daemonset",
			"version", n.currentKernelVersion, "os", os)

		state, err := DaemonSet(n)
		if state != gpuv1.Ready {
			n.logger.Info("pre-compiled driver daemonset not ready",
				"version", n.currentKernelVersion, "state", state)
			overallState = state
		}
		if err != nil {
			errs = append(errs, fmt.Errorf("failed to handle Precompiled Driver Daemonset for version %s: %v", kernelVersion, err))
		}
	}

	// reset current kernel version
	n.currentKernelVersion = ""
	return overallState, errs
}

// ocpDriverToolkitDaemonSets goes through all the RHCOS versions
// found in the cluster, sets `currentRhcosVersion` and calls the
// original DaemonSet() function to create/update the RHCOS-specific
// DaemonSet.
func (n ClusterPolicyController) ocpDriverToolkitDaemonSets(ctx context.Context) (gpuv1.State, error) {
	err := n.ocpCleanupStaleDriverToolkitDaemonSets(ctx)
	if err != nil {
		return gpuv1.NotReady, err
	}

	n.logger.V(1).Info("preparing DriverToolkit DaemonSet",
		"rhcos", n.ocpDriverToolkit.rhcosVersions)

	overallState := gpuv1.Ready
	var errs error

	for rhcosVersion := range n.ocpDriverToolkit.rhcosVersions {
		n.ocpDriverToolkit.currentRhcosVersion = rhcosVersion

		n.logger.V(1).Info("preparing DriverToolkit DaemonSet",
			"rhcosVersion", n.ocpDriverToolkit.currentRhcosVersion)

		state, err := DaemonSet(n)

		n.logger.V(1).Info("preparing DriverToolkit DaemonSet",
			"rhcosVersion", n.ocpDriverToolkit.currentRhcosVersion, "state", state)
		if state != gpuv1.Ready {
			overallState = state
		}

		if err != nil {
			if errs == nil {
				errs = err
			}
			errs = fmt.Errorf("failed to handle OpenShift Driver Toolkit Daemonset for version %s: %v", rhcosVersion, errs)
		}
	}

	n.ocpDriverToolkit.currentRhcosVersion = ""

	tagsMissing := false
	for rhcosVersion, image := range n.ocpDriverToolkit.rhcosDriverToolkitImages {
		if image != "" {
			continue
		}
		n.logger.Info("WARNINGs: RHCOS driver-toolkit image missing. Version-specific fallback mode enabled.", "rhcosVersion", rhcosVersion)
		tagsMissing = true
	}
	if tagsMissing {
		n.operatorMetrics.openshiftDriverToolkitRhcosTagsMissing.Set(1)
	} else {
		n.operatorMetrics.openshiftDriverToolkitRhcosTagsMissing.Set(0)
	}

	return overallState, errs
}

// ocpCleanupStaleDriverToolkitDaemonSets scans the DriverToolkit
// RHCOS-version specific DaemonSets, and deletes the unused one:
// - RHCOS version wasn't found in the node labels (upgrade finished)
// - RHCOS version marked for deletion earlier in the Reconciliation loop (currently unexpected)
// - no RHCOS version label (unexpected)
// The DaemonSet set is kept if:
// - RHCOS version was found in the node labels (most likely case)
func (n ClusterPolicyController) ocpCleanupStaleDriverToolkitDaemonSets(ctx context.Context) error {
	opts := []client.ListOption{
		client.MatchingLabels{
			ocpDriverToolkitIdentificationLabel: ocpDriverToolkitIdentificationValue,
		},
	}

	list := &appsv1.DaemonSetList{}
	err := n.client.List(ctx, list, opts...)
	if err != nil {
		n.logger.Info("ERROR: Could not get DaemonSetList", "Error", err)
		return err
	}

	for idx := range list.Items {
		name := list.Items[idx].Name
		dsRhcosVersion, versionOk := list.Items[idx].Labels[ocpDriverToolkitVersionLabel]
		clusterHasRhcosVersion, clusterOk := n.ocpDriverToolkit.rhcosVersions[dsRhcosVersion]
		desiredNumberScheduled := list.Items[idx].Status.DesiredNumberScheduled

		n.logger.V(1).Info("Driver DaemonSet found",
			"Name", name,
			"dsRhcosVersion", dsRhcosVersion,
			"clusterHasRhcosVersion", clusterHasRhcosVersion,
			"desiredNumberScheduled", desiredNumberScheduled)

		if desiredNumberScheduled != 0 {
			n.logger.Info("Driver DaemonSet active, keep it.",
				"Name", name, "Status.DesiredNumberScheduled", desiredNumberScheduled)
			continue
		}

		if !versionOk {
			n.logger.Info("WARNING: Driver DaemonSet doesn't have DriverToolkit version label",
				"Name", name, "Label", ocpDriverToolkitVersionLabel,
			)
		} else {
			switch {
			case !clusterOk:
				n.logger.V(1).Info("Driver DaemonSet RHCOS version NOT part of the cluster",
					"Name", name, "RHCOS version", dsRhcosVersion,
				)
			case clusterHasRhcosVersion:
				n.logger.V(1).Info("Driver DaemonSet RHCOS version is part of the cluster, keep it.",
					"Name", name, "RHCOS version", dsRhcosVersion,
				)

				// the version of RHCOS targeted by this DS is part of the cluster
				// keep it alive

				continue
			default: /* clusterHasRhcosVersion == false */
				// currently unexpected
				n.logger.V(1).Info("Driver DaemonSet RHCOS version marked for deletion",
					"Name", name, "RHCOS version", dsRhcosVersion,
				)
			}
		}

		n.logger.Info("Delete Driver DaemonSet", "Name", name)
		err = n.client.Delete(ctx, &list.Items[idx])
		if err != nil {
			n.logger.Info("ERROR: Could not get delete DaemonSet",
				"Name", name, "Error", err)
			return err
		}
	}
	return nil
}

// cleanupUnusedVGPUManagerDaemonsets cleans up the vgpu-manager DaemonSet(s)
// according to the operator.useOCPDriverToolkit is enabled for ocp
// This allows switching toggling the flag after the initial deployment.  If no
// error happens, returns the number of Pods belonging to these
// DaemonSets.
func (n ClusterPolicyController) cleanupUnusedVGPUManagerDaemonsets(ctx context.Context) (int, error) {
	podCount := 0
	if n.openshift == "" {
		return podCount, nil
	}

	if !n.ocpDriverToolkit.enabled {
		// cleanup DTK daemonsets
		count, err := n.cleanupDriverDaemonsets(ctx,
			ocpDriverToolkitIdentificationLabel,
			ocpDriverToolkitIdentificationValue, commonVGPUManagerDaemonsetName)
		if err != nil {
			return 0, err
		}
		podCount = count
	} else {
		// cleanup legacy vgpu-manager daemonsets
		count, err := n.cleanupDriverDaemonsets(ctx,
			appLabelKey,
			commonVGPUManagerDaemonsetName, commonVGPUManagerDaemonsetName)
		if err != nil {
			return 0, err
		}
		podCount = count
	}
	return podCount, nil
}

// cleanupUnusedDriverDaemonSets cleans up the driver DaemonSet(s)
// according to following.
// 1. If driver.usePrecompiled is enabled
// 2. if operator.useOCPDriverToolkit is enabled for ocp
// This allows switching toggling the flag after the initial deployment.  If no
// error happens, returns the number of Pods belonging to these
// DaemonSets.
func (n ClusterPolicyController) cleanupUnusedDriverDaemonSets(ctx context.Context) (int, error) {
	podCount := 0
	if n.openshift != "" {
		switch {
		case n.singleton.Spec.Driver.UsePrecompiledDrivers():
			// cleanup DTK daemonsets
			count, err := n.cleanupDriverDaemonsets(ctx,
				ocpDriverToolkitIdentificationLabel,
				ocpDriverToolkitIdentificationValue, commonDriverDaemonsetName)
			if err != nil {
				return 0, err
			}
			podCount = count
			// cleanup legacy driver daemonsets that use run file
			count, err = n.cleanupDriverDaemonsets(ctx,
				precompiledIdentificationLabelKey,
				"false", commonDriverDaemonsetName)
			if err != nil {
				return 0, err
			}
			podCount += count

		case n.ocpDriverToolkit.enabled:
			// cleanup pre-compiled and legacy driver daemonsets
			count, err := n.cleanupDriverDaemonsets(ctx,
				appLabelKey,
				commonDriverDaemonsetName, commonDriverDaemonsetName)
			if err != nil {
				return 0, err
			}
			podCount = count
		default:
			// cleanup pre-compiled
			count, err := n.cleanupDriverDaemonsets(ctx,
				precompiledIdentificationLabelKey,
				precompiledIdentificationLabelValue, commonDriverDaemonsetName)
			if err != nil {
				return 0, err
			}
			podCount = count

			// cleanup DTK daemonsets
			count, err = n.cleanupDriverDaemonsets(ctx,
				ocpDriverToolkitIdentificationLabel,
				ocpDriverToolkitIdentificationValue, commonDriverDaemonsetName)
			if err != nil {
				return 0, err
			}
			podCount += count
		}
	} else {
		if n.singleton.Spec.Driver.UsePrecompiledDrivers() {
			// cleanup legacy driver daemonsets that use run file
			count, err := n.cleanupDriverDaemonsets(ctx,
				precompiledIdentificationLabelKey,
				"false", commonDriverDaemonsetName)
			if err != nil {
				return 0, err
			}
			podCount = count
		} else {
			// cleanup pre-compiled driver daemonsets
			count, err := n.cleanupDriverDaemonsets(ctx,
				precompiledIdentificationLabelKey,
				precompiledIdentificationLabelValue, commonDriverDaemonsetName)
			if err != nil {
				return 0, err
			}
			podCount = count
		}
	}
	return podCount, nil
}

// cleanupDriverDaemonSets deletes the DaemonSets matching a given key/value
// pairs If no error happens, returns the number of Pods belonging to
// the DaemonSet.
func (n ClusterPolicyController) cleanupDriverDaemonsets(ctx context.Context, searchKey string, searchValue string, namePrefix string) (int, error) {
	var opts = []client.ListOption{client.MatchingLabels{searchKey: searchValue}}

	dsList := &appsv1.DaemonSetList{}
	if err := n.client.List(ctx, dsList, opts...); err != nil {
		n.logger.Error(err, "Could not get DaemonSetList")
		return 0, err
	}

	var lastErr error
	for idx := range dsList.Items {
		n.logger.Info("Delete DaemonSet",
			"Name", dsList.Items[idx].Name,
		)
		// ignore daemonsets that doesn't match the required name
		if !strings.HasPrefix(dsList.Items[idx].Name, namePrefix) {
			continue
		}
		if err := n.client.Delete(ctx, &dsList.Items[idx]); err != nil {
			n.logger.Error(err, "Could not get delete DaemonSet",
				"Name", dsList.Items[idx].Name)
			lastErr = err
		}
	}

	// return the last error that occurred, if any
	if lastErr != nil {
		return 0, lastErr
	}

	podList := &corev1.PodList{}
	if err := n.client.List(ctx, podList, opts...); err != nil {
		n.logger.Info("ERROR: Could not get PodList", "Error", err)
		return 0, err
	}

	podCount := 0
	for idx := range podList.Items {
		// ignore pods that doesn't match the required name
		if !strings.HasPrefix(podList.Items[idx].Name, namePrefix) {
			continue
		}
		podCount++
	}
	return podCount, nil
}

// DaemonSet creates Daemonset resource
func DaemonSet(n ClusterPolicyController) (gpuv1.State, error) {
	ctx := n.ctx
	state := n.idx
	obj := n.resources[state].DaemonSet.DeepCopy()
	obj.Namespace = n.operatorNamespace

	logger := n.logger.WithValues("DaemonSet", obj.Name, "Namespace", obj.Namespace)

	// Check if state is disabled and cleanup resource if exists
	if !n.isStateEnabled(n.stateNames[n.idx]) {
		err := n.client.Delete(ctx, obj)
		if err != nil && !apierrors.IsNotFound(err) {
			logger.Info("Couldn't delete", "Error", err)
			return gpuv1.NotReady, err
		}
		return gpuv1.Disabled, nil
	}

	if !n.hasGPUNodes {
		// multiple DaemonSets (eg, driver, dgcm-exporter) cannot be
		// deployed without knowing the OS name, so skip their
		// deployment for now. The operator will be notified
		// (addWatchNewGPUNode) when new nodes will join the cluster.
		logger.Info("No GPU node in the cluster, do not create DaemonSets")
		return gpuv1.Ready, nil
	}

	if n.resources[state].DaemonSet.GetName() == commonDriverDaemonsetName {
		podCount, err := n.cleanupUnusedDriverDaemonSets(n.ctx)
		if err != nil {
			return gpuv1.NotReady, err
		}
		if podCount != 0 {
			logger.Info("Driver DaemonSet cleanup in progress", "podCount", podCount)
			return gpuv1.NotReady, nil
		}

		// Daemonsets using pre-compiled packages or using driver-toolkit (openshift) require creation of
		// one daemonset per kernel version (or rhcos version).
		// If currentKernelVersion or currentRhcosVersion (ocp) are not set, we intercept here
		// and call Daemonset() per specific version
		if n.singleton.Spec.Driver.UsePrecompiledDrivers() {
			if n.currentKernelVersion == "" {
				overallState, errs := precompiledDriverDaemonsets(ctx, n)
				if len(errs) != 0 {
					// log errors
					return overallState, fmt.Errorf("unable to deploy precompiled driver daemonsets %v", errs)
				}
				return overallState, nil
			}
		} else if n.openshift != "" && n.ocpDriverToolkit.enabled &&
			n.ocpDriverToolkit.currentRhcosVersion == "" {
			return n.ocpDriverToolkitDaemonSets(ctx)
		}
	} else if n.resources[state].DaemonSet.Name == commonVGPUManagerDaemonsetName {
		podCount, err := n.cleanupUnusedVGPUManagerDaemonsets(ctx)
		if err != nil {
			return gpuv1.NotReady, err
		}
		if podCount != 0 {
			logger.Info("Driver DaemonSet cleanup in progress", "podCount", podCount)
			return gpuv1.NotReady, nil
		}
		if n.openshift != "" && n.ocpDriverToolkit.enabled &&
			n.ocpDriverToolkit.currentRhcosVersion == "" {
			// OpenShift Driver Toolkit requires the creation of
			// one Driver DaemonSet per RHCOS version (stored in
			// n.ocpDriverToolkit.rhcosVersions).
			//
			// Here, we are at the top-most call of DaemonSet(),
			// as currentRhcosVersion is unset.
			//
			// Initiate the multi-DaemonSet OCP DriverToolkit
			// deployment.
			return n.ocpDriverToolkitDaemonSets(ctx)
		}
	}

	err := preProcessDaemonSet(obj, n)
	if err != nil {
		logger.Info("Could not pre-process", "Error", err)
		return gpuv1.NotReady, err
	}

	if err := controllerutil.SetControllerReference(n.singleton, obj, n.scheme); err != nil {
		logger.Info("SetControllerReference failed", "Error", err)
		return gpuv1.NotReady, err
	}

	if obj.Labels == nil {
		obj.Labels = make(map[string]string)
	}

	for labelKey, labelValue := range n.singleton.Spec.Daemonsets.Labels {
		obj.Labels[labelKey] = labelValue
	}

	// Daemonsets will always have at least one annotation applied, so allocate if necessary
	if obj.Annotations == nil {
		obj.Annotations = make(map[string]string)
	}

	for annoKey, annoValue := range n.singleton.Spec.Daemonsets.Annotations {
		obj.Annotations[annoKey] = annoValue
	}

	found := &appsv1.DaemonSet{}
	err = n.client.Get(ctx, types.NamespacedName{Namespace: obj.Namespace, Name: obj.Name}, found)
	if err != nil && apierrors.IsNotFound(err) {
		logger.Info("DaemonSet not found, creating",
			"Name", obj.Name,
		)
		// generate hash for the spec to create
		hashStr := utils.GetObjectHash(obj)
		// add annotation to the Daemonset with hash value during creation
		obj.Annotations[NvidiaAnnotationHashKey] = hashStr
		err = n.client.Create(ctx, obj)
		if err != nil {
			logger.Info("Couldn't create DaemonSet",
				"Name", obj.Name,
				"Error", err,
			)
			return gpuv1.NotReady, err
		}
		return isDaemonSetReady(obj.Name, n), nil
	} else if err != nil {
		logger.Info("Failed to get DaemonSet from client",
			"Name", obj.Name,
			"Error", err.Error())
		return gpuv1.NotReady, err
	}

	changed := isDaemonsetSpecChanged(found, obj)
	if changed {
		logger.Info("DaemonSet is different, updating", "name", obj.Name)
		err = n.client.Update(ctx, obj)
		if err != nil {
			return gpuv1.NotReady, err
		}
	} else {
		logger.Info("DaemonSet identical, skipping update", "name", obj.Name)
	}
	return isDaemonSetReady(obj.Name, n), nil
}

// isDaemonsetSpecChanged returns true if the spec has changed between existing one
// and new Daemonset spec compared by hash.
func isDaemonsetSpecChanged(current *appsv1.DaemonSet, new *appsv1.DaemonSet) bool {
	if current == nil && new != nil {
		return true
	}
	if current.Annotations == nil || new.Annotations == nil {
		panic("appsv1.DaemonSet.Annotations must be allocated prior to calling isDaemonsetSpecChanged()")
	}

	hashStr := utils.GetObjectHash(new)
	foundHashAnnotation := false

	for annotation, value := range current.Annotations {
		if annotation == NvidiaAnnotationHashKey {
			if value != hashStr {
				// update annotation to be added to Daemonset as per new spec and indicate spec update is required
				new.Annotations[NvidiaAnnotationHashKey] = hashStr
				return true
			}
			foundHashAnnotation = true
			break
		}
	}

	if !foundHashAnnotation {
		// update annotation to be added to Daemonset as per new spec and indicate spec update is required
		new.Annotations[NvidiaAnnotationHashKey] = hashStr
		return true
	}
	return false
}

// The operator starts two pods in different stages to validate
// the correct working of the DaemonSets (driver and dp). Therefore
// the operator waits until the Pod completes and checks the error status
// to advance to the next state.
func isPodReady(name string, n ClusterPolicyController, phase corev1.PodPhase) gpuv1.State {
	ctx := n.ctx
	opts := []client.ListOption{&client.MatchingLabels{"app": name}}

	n.logger.V(1).Info("Pod", "LabelSelector", fmt.Sprintf("app=%s", name))
	list := &corev1.PodList{}
	err := n.client.List(ctx, list, opts...)
	if err != nil {
		n.logger.Info("Could not get PodList", err)
	}
	n.logger.V(1).Info("Pod", "NumberOfPods", len(list.Items))
	if len(list.Items) == 0 {
		return gpuv1.NotReady
	}

	pd := list.Items[0]

	if pd.Status.Phase != phase {
		n.logger.V(1).Info("Pod", "Phase", pd.Status.Phase, "!=", phase)
		return gpuv1.NotReady
	}
	n.logger.V(1).Info("Pod", "Phase", pd.Status.Phase, "==", phase)
	return gpuv1.Ready
}

// SecurityContextConstraints creates SCC resources
func SecurityContextConstraints(n ClusterPolicyController) (gpuv1.State, error) {
	ctx := n.ctx
	state := n.idx
	obj := n.resources[state].SecurityContextConstraints.DeepCopy()
	obj.Namespace = n.operatorNamespace

	logger := n.logger.WithValues("SecurityContextConstraints", obj.Name, "Namespace", "default")

	// Check if state is disabled and cleanup resource if exists
	if !n.isStateEnabled(n.stateNames[n.idx]) {
		err := n.client.Delete(ctx, obj)
		if err != nil && !apierrors.IsNotFound(err) {
			logger.Info("Couldn't delete", "Error", err)
			return gpuv1.NotReady, err
		}
		return gpuv1.Disabled, nil
	}

	for idx := range obj.Users {
		if obj.Users[idx] != "FILLED BY THE OPERATOR" {
			continue
		}
		obj.Users[idx] = fmt.Sprintf("system:serviceaccount:%s:%s", obj.Namespace, obj.Name)
	}

	if err := controllerutil.SetControllerReference(n.singleton, obj, n.scheme); err != nil {
		return gpuv1.NotReady, err
	}

	found := &secv1.SecurityContextConstraints{}
	err := n.client.Get(ctx, types.NamespacedName{Namespace: "", Name: obj.Name}, found)
	if err != nil && apierrors.IsNotFound(err) {
		logger.Info("Not found, creating...")
		err = n.client.Create(ctx, obj)
		if err != nil {
			logger.Info("Couldn't create", "Error", err)
			return gpuv1.NotReady, err
		}
		return gpuv1.Ready, nil
	} else if err != nil {
		return gpuv1.NotReady, err
	}

	logger.Info("Found Resource, updating...")
	obj.ResourceVersion = found.ResourceVersion

	err = n.client.Update(ctx, obj)
	if err != nil {
		logger.Info("Couldn't update", "Error", err)
		return gpuv1.NotReady, err
	}
	return gpuv1.Ready, nil
}

// Service creates Service object
func Service(n ClusterPolicyController) (gpuv1.State, error) {
	ctx := n.ctx
	state := n.idx
	obj := n.resources[state].Service.DeepCopy()

	obj.Namespace = n.operatorNamespace

	logger := n.logger.WithValues("Service", obj.Name, "Namespace", obj.Namespace)

	// Check if state is disabled and cleanup resource if exists
	if !n.isStateEnabled(n.stateNames[n.idx]) {
		err := n.client.Delete(ctx, obj)
		if err != nil && !apierrors.IsNotFound(err) {
			logger.Info("Couldn't delete", "Error", err)
			return gpuv1.NotReady, err
		}
		return gpuv1.Disabled, nil
	}

	if err := controllerutil.SetControllerReference(n.singleton, obj, n.scheme); err != nil {
		return gpuv1.NotReady, err
	}

	err := preprocessService(obj, n)
	if err != nil {
		logger.Info("Couldn't preprocess Service", "Error", err)
		return gpuv1.NotReady, err
	}

	found := &corev1.Service{}
	err = n.client.Get(ctx, types.NamespacedName{Namespace: obj.Namespace, Name: obj.Name}, found)
	if err != nil && apierrors.IsNotFound(err) {
		logger.Info("Not found, creating...")
		err = n.client.Create(ctx, obj)
		if err != nil {
			logger.Info("Couldn't create", "Error", err)
			return gpuv1.NotReady, err
		}
		return gpuv1.Ready, nil
	} else if err != nil {
		return gpuv1.NotReady, err
	}

	logger.Info("Found Resource, updating...")
	obj.ResourceVersion = found.ResourceVersion
	obj.Spec.ClusterIP = found.Spec.ClusterIP

	err = n.client.Update(ctx, obj)
	if err != nil {
		logger.Info("Couldn't update", "Error", err)
		return gpuv1.NotReady, err
	}
	return gpuv1.Ready, nil
}

func crdExists(n ClusterPolicyController, name string) (bool, error) {
	crd := &apiextensionsv1.CustomResourceDefinition{}
	err := n.client.Get(n.ctx, client.ObjectKey{Name: name}, crd)
	if err != nil && apierrors.IsNotFound(err) {
		return false, nil
	} else if err != nil {
		return false, err
	}

	return true, nil
}

// ServiceMonitor creates ServiceMonitor object
func ServiceMonitor(n ClusterPolicyController) (gpuv1.State, error) {
	ctx := n.ctx
	state := n.idx
	obj := n.resources[state].ServiceMonitor.DeepCopy()
	obj.Namespace = n.operatorNamespace

	logger := n.logger.WithValues("ServiceMonitor", obj.Name, "Namespace", obj.Namespace)

	// Check if ServiceMonitor is a valid kind
	serviceMonitorCRDExists, err := crdExists(n, ServiceMonitorCRDName)
	if err != nil {
		return gpuv1.NotReady, err
	}

	// Check if state is disabled and cleanup resource if exists
	if !n.isStateEnabled(n.stateNames[state]) {
		if !serviceMonitorCRDExists {
			return gpuv1.Ready, nil
		}
		err := n.client.Delete(ctx, obj)
		if err != nil && !apierrors.IsNotFound(err) {
			logger.Info("Couldn't delete", "Error", err)
			return gpuv1.NotReady, err
		}
		return gpuv1.Disabled, nil
	}

	if n.stateNames[state] == "state-dcgm-exporter" {
		serviceMonitor := n.singleton.Spec.DCGMExporter.ServiceMonitor
		// Check if ServiceMonitor is disabled and cleanup resource if exists
		if serviceMonitor == nil || !serviceMonitor.IsEnabled() {
			if !serviceMonitorCRDExists {
				return gpuv1.Ready, nil
			}
			err := n.client.Delete(ctx, obj)
			if err != nil && !apierrors.IsNotFound(err) {
				logger.Info("Couldn't delete", "Error", err)
				return gpuv1.NotReady, err
			}
			return gpuv1.Disabled, nil
		}

		if !serviceMonitorCRDExists {
			logger.Error(fmt.Errorf("couldn't find ServiceMonitor CRD"), "Install Prometheus and necessary CRDs for gathering GPU metrics!")
			return gpuv1.NotReady, nil
		}

		// Apply custom edits for DCGM Exporter
		if serviceMonitor.Interval != "" {
			obj.Spec.Endpoints[0].Interval = serviceMonitor.Interval
		}

		if serviceMonitor.HonorLabels != nil {
			obj.Spec.Endpoints[0].HonorLabels = *serviceMonitor.HonorLabels
		}

		if serviceMonitor.AdditionalLabels != nil {
			for key, value := range serviceMonitor.AdditionalLabels {
				obj.Labels[key] = value
			}
		}
		if serviceMonitor.Relabelings != nil {
			relabelConfigs := make([]promv1.RelabelConfig, len(serviceMonitor.Relabelings))
			for i, relabel := range serviceMonitor.Relabelings {
				if relabel != nil {
					relabelConfigs[i] = *relabel
				}
			}
			obj.Spec.Endpoints[0].RelabelConfigs = relabelConfigs
		}
	}
	if n.stateNames[state] == "state-operator-metrics" || n.stateNames[state] == "state-node-status-exporter" {
		// if ServiceMonitor CRD is missing, assume prometheus is not setup and ignore CR creation
		if !serviceMonitorCRDExists {
			logger.V(1).Info("ServiceMonitor CRD is missing, ignoring creation of CR for operator-metrics")
			return gpuv1.Ready, nil
		}
		obj.Spec.NamespaceSelector.MatchNames = []string{obj.Namespace}
	}

	for idx := range obj.Spec.NamespaceSelector.MatchNames {
		if obj.Spec.NamespaceSelector.MatchNames[idx] != "FILLED BY THE OPERATOR" {
			continue
		}
		obj.Spec.NamespaceSelector.MatchNames[idx] = obj.Namespace
	}

	if err := controllerutil.SetControllerReference(n.singleton, obj, n.scheme); err != nil {
		return gpuv1.NotReady, err
	}

	found := &promv1.ServiceMonitor{}
	err = n.client.Get(ctx, types.NamespacedName{Namespace: obj.Namespace, Name: obj.Name}, found)
	if err != nil && apierrors.IsNotFound(err) {
		logger.Info("Not found, creating...")
		err = n.client.Create(ctx, obj)
		if err != nil {
			logger.Info("Couldn't create", "Error", err)
			return gpuv1.NotReady, err
		}
		return gpuv1.Ready, nil
	} else if err != nil {
		return gpuv1.NotReady, err
	}

	logger.Info("Found Resource, updating...")
	obj.ResourceVersion = found.ResourceVersion

	err = n.client.Update(ctx, obj)
	if err != nil {
		logger.Info("Couldn't update", "Error", err)
		return gpuv1.NotReady, err
	}
	return gpuv1.Ready, nil
}

func transformRuntimeClassLegacy(n ClusterPolicyController, spec nodev1.RuntimeClass) (gpuv1.State, error) {
	ctx := n.ctx
	obj := &nodev1beta1.RuntimeClass{}

	obj.Name = spec.Name
	obj.Handler = spec.Handler

	// apply runtime class name as per ClusterPolicy
	if obj.Name == "FILLED_BY_OPERATOR" {
		runtimeClassName := getRuntimeClassName(&n.singleton.Spec)
		obj.Name = runtimeClassName
		obj.Handler = runtimeClassName
	}

	obj.Labels = spec.Labels

	logger := n.logger.WithValues("RuntimeClass", obj.Name)

	if err := controllerutil.SetControllerReference(n.singleton, obj, n.scheme); err != nil {
		return gpuv1.NotReady, err
	}

	found := &nodev1beta1.RuntimeClass{}
	err := n.client.Get(ctx, types.NamespacedName{Namespace: "", Name: obj.Name}, found)
	if err != nil && apierrors.IsNotFound(err) {
		logger.Info("Not found, creating...")
		err = n.client.Create(ctx, obj)
		if err != nil {
			logger.Info("Couldn't create", "Error", err)
			return gpuv1.NotReady, err
		}
		return gpuv1.Ready, nil
	} else if err != nil {
		return gpuv1.NotReady, err
	}

	logger.Info("Found Resource, updating...")
	obj.ResourceVersion = found.ResourceVersion

	err = n.client.Update(ctx, obj)
	if err != nil {
		logger.Info("Couldn't update", "Error", err)
		return gpuv1.NotReady, err
	}
	return gpuv1.Ready, nil
}

func transformRuntimeClass(n ClusterPolicyController, spec nodev1.RuntimeClass) (gpuv1.State, error) {
	ctx := n.ctx
	obj := &nodev1.RuntimeClass{}

	obj.Name = spec.Name
	obj.Handler = spec.Handler

	// apply runtime class name as per ClusterPolicy
	if obj.Name == "FILLED_BY_OPERATOR" {
		runtimeClassName := getRuntimeClassName(&n.singleton.Spec)
		obj.Name = runtimeClassName
		obj.Handler = runtimeClassName
	}

	obj.Labels = spec.Labels

	logger := n.logger.WithValues("RuntimeClass", obj.Name)

	if err := controllerutil.SetControllerReference(n.singleton, obj, n.scheme); err != nil {
		return gpuv1.NotReady, err
	}

	found := &nodev1.RuntimeClass{}
	err := n.client.Get(ctx, types.NamespacedName{Namespace: "", Name: obj.Name}, found)
	if err != nil && apierrors.IsNotFound(err) {
		logger.Info("Not found, creating...")
		err = n.client.Create(ctx, obj)
		if err != nil {
			logger.Info("Couldn't create", "Error", err)
			return gpuv1.NotReady, err
		}
		return gpuv1.Ready, nil
	} else if err != nil {
		return gpuv1.NotReady, err
	}

	logger.Info("Found Resource, updating...")
	obj.ResourceVersion = found.ResourceVersion

	err = n.client.Update(ctx, obj)
	if err != nil {
		logger.Info("Couldn't update", "Error", err)
		return gpuv1.NotReady, err
	}
	return gpuv1.Ready, nil
}

func transformKataRuntimeClasses(n ClusterPolicyController) (gpuv1.State, error) {
	ctx := n.ctx
	state := n.idx
	config := n.singleton.Spec

	// Get all existing Kata RuntimeClasses
	opts := []client.ListOption{&client.MatchingLabels{"nvidia.com/kata-runtime-class": "true"}}
	list := &nodev1.RuntimeClassList{}
	err := n.client.List(ctx, list, opts...)
	if err != nil {
		n.logger.Info("Could not get Kata RuntimeClassList", err)
		return gpuv1.NotReady, fmt.Errorf("error getting kata RuntimeClassList: %v", err)
	}
	n.logger.V(1).Info("Kata RuntimeClasses", "Number", len(list.Items))

	if !config.KataManager.IsEnabled() {
		// Delete all Kata RuntimeClasses
		n.logger.Info("Kata Manager disabled, deleting all Kata RuntimeClasses")
		for _, rc := range list.Items {
			rc := rc
			n.logger.V(1).Info("Deleting Kata RuntimeClass", "Name", rc.Name)
			err := n.client.Delete(ctx, &rc)
			if err != nil {
				return gpuv1.NotReady, fmt.Errorf("error deleting kata RuntimeClass '%s': %v", rc.Name, err)
			}
		}
		return gpuv1.Ready, nil
	}

	// Get names of desired kata RuntimeClasses
	rcNames := make(map[string]struct{})
	for _, rc := range config.KataManager.Config.RuntimeClasses {
		rcNames[rc.Name] = struct{}{}
	}

	// Delete any existing Kata RuntimeClasses that are no longer specified in KataManager configuration
	for _, rc := range list.Items {
		if _, ok := rcNames[rc.Name]; !ok {
			rc := rc
			n.logger.Info("Deleting Kata RuntimeClass", "Name", rc.Name)
			err := n.client.Delete(ctx, &rc)
			if err != nil {
				return gpuv1.NotReady, fmt.Errorf("error deleting kata RuntimeClass '%s': %v", rc.Name, err)
			}
		}
	}

	// Using kata RuntimClass template, create / update RuntimeClass objects specified in KataManager configuration
	template := n.resources[state].RuntimeClasses[0]
	for _, rc := range config.KataManager.Config.RuntimeClasses {
		logger := n.logger.WithValues("RuntimeClass", rc.Name)

		if rc.Name == config.Operator.RuntimeClass {
			return gpuv1.NotReady, fmt.Errorf("error creating kata runtimeclass '%s' as it conflicts with the runtimeclass used for the gpu-operator operand pods itself", rc.Name)
		}

		obj := nodev1.RuntimeClass{}
		obj.Name = rc.Name
		obj.Handler = rc.Name
		obj.Labels = template.Labels
		obj.Scheduling = &nodev1.Scheduling{}
		nodeSelector := make(map[string]string)
		for k, v := range template.Scheduling.NodeSelector {
			nodeSelector[k] = v
		}
		if rc.NodeSelector != nil {
			// append user provided selectors to default nodeSelector
			for k, v := range rc.NodeSelector {
				nodeSelector[k] = v
			}
		}
		obj.Scheduling.NodeSelector = nodeSelector

		if err := controllerutil.SetControllerReference(n.singleton, &obj, n.scheme); err != nil {
			return gpuv1.NotReady, err
		}

		found := &nodev1.RuntimeClass{}
		err := n.client.Get(ctx, types.NamespacedName{Namespace: "", Name: obj.Name}, found)
		if err != nil && apierrors.IsNotFound(err) {
			logger.Info("Not found, creating...")
			err = n.client.Create(ctx, &obj)
			if err != nil {
				logger.Info("Couldn't create", "Error", err)
				return gpuv1.NotReady, err
			}
			continue
		} else if err != nil {
			return gpuv1.NotReady, err
		}

		logger.Info("Found Resource, updating...")
		obj.ResourceVersion = found.ResourceVersion

		err = n.client.Update(ctx, &obj)
		if err != nil {
			logger.Info("Couldn't update", "Error", err)
			return gpuv1.NotReady, err
		}
	}
	return gpuv1.Ready, nil
}

func RuntimeClasses(n ClusterPolicyController) (gpuv1.State, error) {
	status := gpuv1.Ready
	state := n.idx

	if n.stateNames[state] == "state-kata-manager" {
		return transformKataRuntimeClasses(n)
	}

	createRuntimeClassFunc := transformRuntimeClass
	if semver.Compare(n.k8sVersion, nodev1MinimumAPIVersion) <= 0 {
		createRuntimeClassFunc = transformRuntimeClassLegacy
	}

	for _, obj := range n.resources[state].RuntimeClasses {
		obj := obj
		// When CDI is disabled, do not create the additional 'nvidia-cdi' and
		// 'nvidia-legacy' runtime classes. Delete these objects if they were
		// previously created.
		if !n.singleton.Spec.CDI.IsEnabled() && (obj.Name == "nvidia-cdi" || obj.Name == "nvidia-legacy") {
			err := n.client.Delete(n.ctx, &obj)
			if err != nil && !apierrors.IsNotFound(err) {
				n.logger.Info("Couldn't delete", "RuntimeClass", obj.Name, "Error", err)
				return gpuv1.NotReady, err
			}
			continue
		}
		stat, err := createRuntimeClassFunc(n, obj)
		if err != nil {
			return stat, err
		}
		if stat != gpuv1.Ready {
			status = gpuv1.NotReady
		}
	}
	return status, nil
}

// PrometheusRule creates PrometheusRule object
func PrometheusRule(n ClusterPolicyController) (gpuv1.State, error) {
	ctx := n.ctx
	state := n.idx
	obj := n.resources[state].PrometheusRule.DeepCopy()
	obj.Namespace = n.operatorNamespace

	logger := n.logger.WithValues("PrometheusRule", obj.Name)

	if err := controllerutil.SetControllerReference(n.singleton, obj, n.scheme); err != nil {
		return gpuv1.NotReady, err
	}

	found := &promv1.PrometheusRule{}
	err := n.client.Get(ctx, types.NamespacedName{Namespace: obj.Namespace, Name: obj.Name}, found)
	if err != nil && apierrors.IsNotFound(err) {
		logger.Info("Not found, creating...")
		err = n.client.Create(ctx, obj)
		if err != nil {
			logger.Info("Couldn't create", "Error", err)
			return gpuv1.NotReady, err
		}
		return gpuv1.Ready, nil
	} else if err != nil {
		return gpuv1.NotReady, err
	}

	logger.Info("Found Resource, updating...")
	obj.ResourceVersion = found.ResourceVersion

	err = n.client.Update(ctx, obj)
	if err != nil {
		logger.Info("Couldn't update", "Error", err)
		return gpuv1.NotReady, err
	}
	return gpuv1.Ready, nil
}
