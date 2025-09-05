/*
Copyright 2021.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package v1

import (
	"fmt"
	"os"
	"strings"

	kata_v1alpha1 "github.com/NVIDIA/k8s-kata-manager/api/v1alpha1/config"
	upgrade_v1alpha1 "github.com/NVIDIA/k8s-operator-libs/api/upgrade/v1alpha1"
	promv1 "github.com/prometheus-operator/prometheus-operator/pkg/apis/monitoring/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// EDIT THIS FILE!  THIS IS SCAFFOLDING FOR YOU TO OWN!
// NOTE: json tags are required.  Any new fields you add must have json tags for the fields to be serialized.

const (
	ClusterPolicyCRDName = "ClusterPolicy"
)

// ClusterPolicySpec defines the desired state of ClusterPolicy
type ClusterPolicySpec struct {
	// INSERT ADDITIONAL SPEC FIELDS - desired state of cluster
	// Important: Run "make" to regenerate code after modifying this file

	// Operator component spec
	Operator OperatorSpec `json:"operator"`
	// Daemonset defines common configuration for all Daemonsets
	Daemonsets DaemonsetsSpec `json:"daemonsets"`
	// Driver component spec
	Driver DriverSpec `json:"driver"`
	// Toolkit component spec
	Toolkit ToolkitSpec `json:"toolkit"`
	// DevicePlugin component spec
	DevicePlugin DevicePluginSpec `json:"devicePlugin"`
	// DCGMExporter spec
	DCGMExporter DCGMExporterSpec `json:"dcgmExporter"`
	// DCGM component spec
	DCGM DCGMSpec `json:"dcgm"`
	// NodeStatusExporter spec
	NodeStatusExporter NodeStatusExporterSpec `json:"nodeStatusExporter"`
	// GPUFeatureDiscovery spec
	GPUFeatureDiscovery GPUFeatureDiscoverySpec `json:"gfd"`
	// MIG spec
	MIG MIGSpec `json:"mig,omitempty"`
	// MIGManager for configuration to deploy MIG Manager
	MIGManager MIGManagerSpec `json:"migManager,omitempty"`
	// Deprecated: Pod Security Policies are no longer supported. Please use PodSecurityAdmission instead
	// PSP defines spec for handling PodSecurityPolicies
	PSP PSPSpec `json:"psp,omitempty"`
	// PSA defines spec for PodSecurityAdmission configuration
	PSA PSASpec `json:"psa,omitempty"`
	// Validator defines the spec for operator-validator daemonset
	Validator ValidatorSpec `json:"validator,omitempty"`
	// GPUDirectStorage defines the spec for GDS components(Experimental)
	GPUDirectStorage *GPUDirectStorageSpec `json:"gds,omitempty"`
	// GDRCopy component spec
	GDRCopy *GDRCopySpec `json:"gdrcopy,omitempty"`
	// SandboxWorkloads defines the spec for handling sandbox workloads (i.e. Virtual Machines)
	SandboxWorkloads SandboxWorkloadsSpec `json:"sandboxWorkloads,omitempty"`
	// VFIOManager for configuration to deploy VFIO-PCI Manager
	VFIOManager VFIOManagerSpec `json:"vfioManager,omitempty"`
	// SandboxDevicePlugin component spec
	SandboxDevicePlugin SandboxDevicePluginSpec `json:"sandboxDevicePlugin,omitempty"`
	// VGPUManager component spec
	VGPUManager VGPUManagerSpec `json:"vgpuManager,omitempty"`
	// VGPUDeviceManager spec
	VGPUDeviceManager VGPUDeviceManagerSpec `json:"vgpuDeviceManager,omitempty"`
	// CDI configures how the Container Device Interface is used in the cluster
	CDI CDIConfigSpec `json:"cdi,omitempty"`
	// KataManager component spec
	KataManager KataManagerSpec `json:"kataManager,omitempty"`
	// CCManager component spec
	CCManager CCManagerSpec `json:"ccManager,omitempty"`
	// HostPaths defines various paths on the host needed by GPU Operator components
	HostPaths HostPathsSpec `json:"hostPaths,omitempty"`
}

// Runtime defines container runtime type
type Runtime string

// RuntimeClass defines the runtime class to use for GPU-enabled pods
type RuntimeClass string

const (
	// Docker runtime
	Docker Runtime = "docker"
	// CRIO runtime
	CRIO Runtime = "crio"
	// Containerd runtime
	Containerd Runtime = "containerd"
)

func (r Runtime) String() string {
	switch r {
	case Docker:
		return "docker"
	case CRIO:
		return "crio"
	case Containerd:
		return "containerd"
	default:
		return ""
	}
}

// OperatorSpec describes configuration options for the operator
type OperatorSpec struct {
	// +kubebuilder:validation:Enum=docker;crio;containerd
	// +kubebuilder:default=docker
	DefaultRuntime Runtime `json:"defaultRuntime"`
	// +kubebuilder:default=nvidia
	RuntimeClass  string            `json:"runtimeClass,omitempty"`
	InitContainer InitContainerSpec `json:"initContainer,omitempty"`

	// Optional: Map of string keys and values that can be used to organize and categorize
	// (scope and select) objects. May match selectors of replication controllers
	// and services.
	Labels map[string]string `json:"labels,omitempty"`

	// Optional: Annotations is an unstructured key value map stored with a resource that may be
	// set by external tools to store and retrieve arbitrary metadata. They are not
	// queryable and should be preserved when modifying objects.
	Annotations map[string]string `json:"annotations,omitempty"`

	// UseOpenShiftDriverToolkit indicates if DriverToolkit image should be used on OpenShift to build and install driver modules
	// +operator-sdk:gen-csv:customresourcedefinitions.specDescriptors=true
	// +operator-sdk:gen-csv:customresourcedefinitions.specDescriptors.displayName="On OpenShift, enable DriverToolkit image to build and install driver modules"
	// +operator-sdk:gen-csv:customresourcedefinitions.specDescriptors.x-descriptors="urn:alm:descriptor:com.tectonic.ui:booleanSwitch"
	UseOpenShiftDriverToolkit *bool `json:"use_ocp_driver_toolkit,omitempty"`
}

// HostPathsSpec defines various paths on the host needed by GPU Operator components
type HostPathsSpec struct {
	// RootFS represents the path to the root filesystem of the host.
	// This is used by components that need to interact with the host filesystem
	// and as such this must be a chroot-able filesystem.
	// Examples include the MIG Manager and Toolkit Container which may need to
	// stop, start, or restart systemd services.
	RootFS string `json:"rootFS,omitempty"`

	// DriverInstallDir represents the root at which driver files including libraries,
	// config files, and executables can be found.
	DriverInstallDir string `json:"driverInstallDir,omitempty"`
}

// EnvVar represents an environment variable present in a Container.
type EnvVar struct {
	// Name of the environment variable.
	Name string `json:"name"`

	// Value of the environment variable.
	Value string `json:"value,omitempty"`
}

// ResourceRequirements describes the compute resource requirements.
type ResourceRequirements struct {
	// Limits describes the maximum amount of compute resources allowed.
	// More info: https://kubernetes.io/docs/concepts/configuration/manage-resources-containers/
	// +optional
	Limits corev1.ResourceList `json:"limits,omitempty"`
	// Requests describes the minimum amount of compute resources required.
	// If Requests is omitted for a container, it defaults to Limits if that is explicitly specified,
	// otherwise to an implementation-defined value. Requests cannot exceed Limits.
	// More info: https://kubernetes.io/docs/concepts/configuration/manage-resources-containers/
	// +optional
	Requests corev1.ResourceList `json:"requests,omitempty"`
}

// SandboxWorkloadsSpec describes configuration for handling sandbox workloads (i.e. Virtual Machines)
type SandboxWorkloadsSpec struct {
	// Enabled indicates if the GPU Operator should manage additional operands required
	// for sandbox workloads (i.e. VFIO Manager, vGPU Manager, and additional device plugins)
	Enabled *bool `json:"enabled,omitempty"`
	// DefaultWorkload indicates the default GPU workload type to configure
	// worker nodes in the cluster for
	// +kubebuilder:validation:Enum=container;vm-passthrough;vm-vgpu
	// +kubebuilder:default=container
	DefaultWorkload string `json:"defaultWorkload,omitempty"`
}

// PSPSpec describes configuration for PodSecurityPolicies to apply for all Pods
type PSPSpec struct {
	// Enabled indicates if PodSecurityPolicies needs to be enabled for all Pods
	Enabled *bool `json:"enabled,omitempty"`
}

// PSASpec describes configuration for PodSecurityAdmission to apply for all Pods
type PSASpec struct {
	// Enabled indicates if PodSecurityAdmission configuration needs to be enabled for all Pods
	Enabled *bool `json:"enabled,omitempty"`
}

// DaemonsetsSpec indicates common configuration for all Daemonsets managed by GPU Operator
type DaemonsetsSpec struct {
	// Optional: Map of string keys and values that can be used to organize and categorize
	// (scope and select) objects. May match selectors of replication controllers
	// and services.
	Labels map[string]string `json:"labels,omitempty"`

	// Optional: Annotations is an unstructured key value map stored with a resource that may be
	// set by external tools to store and retrieve arbitrary metadata. They are not
	// queryable and should be preserved when modifying objects.
	Annotations map[string]string `json:"annotations,omitempty"`

	// Optional: Set tolerations
	// +operator-sdk:gen-csv:customresourcedefinitions.specDescriptors=true
	// +operator-sdk:gen-csv:customresourcedefinitions.specDescriptors.displayName="Tolerations"
	// +operator-sdk:gen-csv:customresourcedefinitions.specDescriptors.x-descriptors="urn:alm:descriptor:com.tectonic.ui:advanced,urn:alm:descriptor:io.kubernetes:Tolerations"
	Tolerations []corev1.Toleration `json:"tolerations,omitempty"`

	// +kubebuilder:validation:Optional
	// +operator-sdk:gen-csv:customresourcedefinitions.specDescriptors=true
	// +operator-sdk:gen-csv:customresourcedefinitions.specDescriptors.displayName="PriorityClassName"
	PriorityClassName string `json:"priorityClassName,omitempty"`

	// +kubebuilder:validation:Optional
	// +kubebuilder:default=RollingUpdate
	// +kubebuilder:validation:Enum=RollingUpdate;OnDelete
	// +operator-sdk:gen-csv:customresourcedefinitions.specDescriptors=true
	// +operator-sdk:gen-csv:customresourcedefinitions.specDescriptors.displayName="UpdateStrategy for all Daemonsets"
	UpdateStrategy string `json:"updateStrategy,omitempty"`

	// Optional: Configuration for rolling update of all DaemonSet pods
	// +operator-sdk:gen-csv:customresourcedefinitions.specDescriptors=true
	// +operator-sdk:gen-csv:customresourcedefinitions.specDescriptors.displayName="Rolling update configuration for all DaemonSet pods"
	RollingUpdate *RollingUpdateSpec `json:"rollingUpdate,omitempty"`
}

// InitContainerSpec describes configuration for initContainer image used with all components
type InitContainerSpec struct {
	// Repository represents image repository path
	Repository string `json:"repository,omitempty"`

	// Image represents image name
	// +kubebuilder:validation:Pattern=[a-zA-Z0-9\-]+
	Image string `json:"image,omitempty"`

	// Version represents image tag(version)
	Version string `json:"version,omitempty"`

	// Image pull policy
	// +kubebuilder:validation:Optional
	ImagePullPolicy string `json:"imagePullPolicy,omitempty"`

	// Image pull secrets
	// +kubebuilder:validation:Optional
	// +operator-sdk:gen-csv:customresourcedefinitions.specDescriptors=true
	// +operator-sdk:gen-csv:customresourcedefinitions.specDescriptors.displayName="Image pull secrets"
	// +operator-sdk:gen-csv:customresourcedefinitions.specDescriptors.x-descriptors="urn:alm:descriptor:io.kubernetes:Secret"
	ImagePullSecrets []string `json:"imagePullSecrets,omitempty"`
}

// ValidatorSpec describes configuration options for validation pod
type ValidatorSpec struct {
	// Plugin validator spec
	Plugin PluginValidatorSpec `json:"plugin,omitempty"`

	// Toolkit validator spec
	Toolkit ToolkitValidatorSpec `json:"toolkit,omitempty"`

	// Toolkit validator spec
	Driver DriverValidatorSpec `json:"driver,omitempty"`

	// CUDA validator spec
	CUDA CUDAValidatorSpec `json:"cuda,omitempty"`

	// VfioPCI validator spec
	VFIOPCI VFIOPCIValidatorSpec `json:"vfioPCI,omitempty"`

	// VGPUManager validator spec
	VGPUManager VGPUManagerValidatorSpec `json:"vgpuManager,omitempty"`

	// VGPUDevices validator spec
	VGPUDevices VGPUDevicesValidatorSpec `json:"vgpuDevices,omitempty"`

	// Validator image repository
	// +kubebuilder:validation:Optional
	Repository string `json:"repository,omitempty"`

	// Validator image name
	// +kubebuilder:validation:Pattern=[a-zA-Z0-9\-]+
	Image string `json:"image,omitempty"`

	// Validator image tag
	// +kubebuilder:validation:Optional
	Version string `json:"version,omitempty"`

	// Image pull policy
	// +kubebuilder:validation:Optional
	// +operator-sdk:gen-csv:customresourcedefinitions.specDescriptors=true
	// +operator-sdk:gen-csv:customresourcedefinitions.specDescriptors.displayName="Image Pull Policy"
	// +operator-sdk:gen-csv:customresourcedefinitions.specDescriptors.x-descriptors="urn:alm:descriptor:com.tectonic.ui:imagePullPolicy"
	ImagePullPolicy string `json:"imagePullPolicy,omitempty"`

	// Image pull secrets
	// +kubebuilder:validation:Optional
	// +operator-sdk:gen-csv:customresourcedefinitions.specDescriptors=true
	// +operator-sdk:gen-csv:customresourcedefinitions.specDescriptors.displayName="Image pull secrets"
	// +operator-sdk:gen-csv:customresourcedefinitions.specDescriptors.x-descriptors="urn:alm:descriptor:io.kubernetes:Secret"
	ImagePullSecrets []string `json:"imagePullSecrets,omitempty"`

	// Optional: Define resources requests and limits for each pod
	// +operator-sdk:gen-csv:customresourcedefinitions.specDescriptors=true
	// +operator-sdk:gen-csv:customresourcedefinitions.specDescriptors.displayName="Resource Requirements"
	// +operator-sdk:gen-csv:customresourcedefinitions.specDescriptors.x-descriptors="urn:alm:descriptor:com.tectonic.ui:advanced,urn:alm:descriptor:com.tectonic.ui:resourceRequirements"
	Resources *ResourceRequirements `json:"resources,omitempty"`

	// Optional: List of arguments
	// +operator-sdk:gen-csv:customresourcedefinitions.specDescriptors=true
	// +operator-sdk:gen-csv:customresourcedefinitions.specDescriptors.displayName="Arguments"
	// +operator-sdk:gen-csv:customresourcedefinitions.specDescriptors.x-descriptors="urn:alm:descriptor:com.tectonic.ui:advanced,urn:alm:descriptor:com.tectonic.ui:text"
	Args []string `json:"args,omitempty"`

	// Optional: List of environment variables
	// +operator-sdk:gen-csv:customresourcedefinitions.specDescriptors=true
	// +operator-sdk:gen-csv:customresourcedefinitions.specDescriptors.displayName="Environment Variables"
	// +operator-sdk:gen-csv:customresourcedefinitions.specDescriptors.x-descriptors="urn:alm:descriptor:com.tectonic.ui:advanced,urn:alm:descriptor:com.tectonic.ui:text"
	Env []EnvVar `json:"env,omitempty"`
}

// PluginValidatorSpec defines validator spec for NVIDIA Device Plugin
type PluginValidatorSpec struct {
	// Optional: List of environment variables
	// +operator-sdk:gen-csv:customresourcedefinitions.specDescriptors=true
	// +operator-sdk:gen-csv:customresourcedefinitions.specDescriptors.displayName="Environment Variables"
	// +operator-sdk:gen-csv:customresourcedefinitions.specDescriptors.x-descriptors="urn:alm:descriptor:com.tectonic.ui:advanced,urn:alm:descriptor:com.tectonic.ui:text"
	Env []EnvVar `json:"env,omitempty"`
}

// ToolkitValidatorSpec defines validator spec for NVIDIA Container Toolkit
type ToolkitValidatorSpec struct {
	// Optional: List of environment variables
	// +operator-sdk:gen-csv:customresourcedefinitions.specDescriptors=true
	// +operator-sdk:gen-csv:customresourcedefinitions.specDescriptors.displayName="Environment Variables"
	// +operator-sdk:gen-csv:customresourcedefinitions.specDescriptors.x-descriptors="urn:alm:descriptor:com.tectonic.ui:advanced,urn:alm:descriptor:com.tectonic.ui:text"
	Env []EnvVar `json:"env,omitempty"`
}

// DriverValidatorSpec defines validator spec for NVIDIA Driver validation
type DriverValidatorSpec struct {
	// Optional: List of environment variables
	// +operator-sdk:gen-csv:customresourcedefinitions.specDescriptors=true
	// +operator-sdk:gen-csv:customresourcedefinitions.specDescriptors.displayName="Environment Variables"
	// +operator-sdk:gen-csv:customresourcedefinitions.specDescriptors.x-descriptors="urn:alm:descriptor:com.tectonic.ui:advanced,urn:alm:descriptor:com.tectonic.ui:text"
	Env []EnvVar `json:"env,omitempty"`
}

// CUDAValidatorSpec defines validator spec for CUDA validation workload pod
type CUDAValidatorSpec struct {
	// Optional: List of environment variables
	// +operator-sdk:gen-csv:customresourcedefinitions.specDescriptors=true
	// +operator-sdk:gen-csv:customresourcedefinitions.specDescriptors.displayName="Environment Variables"
	// +operator-sdk:gen-csv:customresourcedefinitions.specDescriptors.x-descriptors="urn:alm:descriptor:com.tectonic.ui:advanced,urn:alm:descriptor:com.tectonic.ui:text"
	Env []EnvVar `json:"env,omitempty"`
}

// VFIOPCIValidatorSpec defines validator spec for NVIDIA VFIO-PCI device validation
type VFIOPCIValidatorSpec struct {
	// Optional: List of environment variables
	// +operator-sdk:gen-csv:customresourcedefinitions.specDescriptors=true
	// +operator-sdk:gen-csv:customresourcedefinitions.specDescriptors.displayName="Environment Variables"
	// +operator-sdk:gen-csv:customresourcedefinitions.specDescriptors.x-descriptors="urn:alm:descriptor:com.tectonic.ui:advanced,urn:alm:descriptor:com.tectonic.ui:text"
	Env []EnvVar `json:"env,omitempty"`
}

// VGPUManagerValidatorSpec defines validator spec for NVIDIA vGPU Manager
type VGPUManagerValidatorSpec struct {
	// Optional: List of environment variables
	// +operator-sdk:gen-csv:customresourcedefinitions.specDescriptors=true
	// +operator-sdk:gen-csv:customresourcedefinitions.specDescriptors.displayName="Environment Variables"
	// +operator-sdk:gen-csv:customresourcedefinitions.specDescriptors.x-descriptors="urn:alm:descriptor:com.tectonic.ui:advanced,urn:alm:descriptor:com.tectonic.ui:text"
	Env []EnvVar `json:"env,omitempty"`
}

// VGPUDevicesValidatorSpec defines validator spec for NVIDIA vGPU device validator
type VGPUDevicesValidatorSpec struct {
	// Optional: List of environment variables
	// +operator-sdk:gen-csv:customresourcedefinitions.specDescriptors=true
	// +operator-sdk:gen-csv:customresourcedefinitions.specDescriptors.displayName="Environment Variables"
	// +operator-sdk:gen-csv:customresourcedefinitions.specDescriptors.x-descriptors="urn:alm:descriptor:com.tectonic.ui:advanced,urn:alm:descriptor:com.tectonic.ui:text"
	Env []EnvVar `json:"env,omitempty"`
}

// MIGSpec defines the configuration for MIG support
type MIGSpec struct {
	// Optional: MIGStrategy to apply for GFD and NVIDIA Device Plugin
	// +kubebuilder:validation:Enum=none;single;mixed
	Strategy MIGStrategy `json:"strategy,omitempty"`
}

// DriverManagerSpec describes configuration for NVIDIA Driver Manager(initContainer)
type DriverManagerSpec struct {
	// Repository represents Driver Managerrepository path
	Repository string `json:"repository,omitempty"`

	// Image represents NVIDIA Driver Manager image name
	// +kubebuilder:validation:Pattern=[a-zA-Z0-9\-]+
	Image string `json:"image,omitempty"`

	// Version represents NVIDIA Driver Manager image tag(version)
	Version string `json:"version,omitempty"`

	// Image pull policy
	// +kubebuilder:validation:Optional
	ImagePullPolicy string `json:"imagePullPolicy,omitempty"`

	// Image pull secrets
	// +kubebuilder:validation:Optional
	// +operator-sdk:gen-csv:customresourcedefinitions.specDescriptors=true
	// +operator-sdk:gen-csv:customresourcedefinitions.specDescriptors.displayName="Image pull secrets"
	// +operator-sdk:gen-csv:customresourcedefinitions.specDescriptors.x-descriptors="urn:alm:descriptor:io.kubernetes:Secret"
	ImagePullSecrets []string `json:"imagePullSecrets,omitempty"`

	// Optional: List of environment variables
	// +operator-sdk:gen-csv:customresourcedefinitions.specDescriptors=true
	// +operator-sdk:gen-csv:customresourcedefinitions.specDescriptors.displayName="Environment Variables"
	// +operator-sdk:gen-csv:customresourcedefinitions.specDescriptors.x-descriptors="urn:alm:descriptor:com.tectonic.ui:advanced,urn:alm:descriptor:com.tectonic.ui:text"
	Env []EnvVar `json:"env,omitempty"`
}

// ContainerProbeSpec defines the properties for configuring container probes
type ContainerProbeSpec struct {
	// Number of seconds after the container has started before liveness probes are initiated.
	// More info: https://kubernetes.io/docs/concepts/workloads/pods/pod-lifecycle#container-probes
	// +kubebuilder:validation:Optional
	InitialDelaySeconds int32 `json:"initialDelaySeconds,omitempty"`
	// Number of seconds after which the probe times out.
	// Defaults to 1 second. Minimum value is 1.
	// More info: https://kubernetes.io/docs/concepts/workloads/pods/pod-lifecycle#container-probes
	// +kubebuilder:validation:Optional
	// +kubebuilder:validation:Minimum=1
	TimeoutSeconds int32 `json:"timeoutSeconds,omitempty"`
	// How often (in seconds) to perform the probe.
	// Default to 10 seconds. Minimum value is 1.
	// +kubebuilder:validation:Optional
	// +kubebuilder:validation:Minimum=1
	PeriodSeconds int32 `json:"periodSeconds,omitempty"`
	// Minimum consecutive successes for the probe to be considered successful after having failed.
	// Defaults to 1. Must be 1 for liveness and startup. Minimum value is 1.
	// +kubebuilder:validation:Optional
	// +kubebuilder:validation:Minimum=1
	SuccessThreshold int32 `json:"successThreshold,omitempty"`
	// Minimum consecutive failures for the probe to be considered failed after having succeeded.
	// Defaults to 3. Minimum value is 1.
	// +kubebuilder:validation:Optional
	// +kubebuilder:validation:Minimum=1
	FailureThreshold int32 `json:"failureThreshold,omitempty"`
}

// DriverSpec defines the properties for NVIDIA Driver deployment
type DriverSpec struct {
	// UseNvidiaDriverCRD indicates if the deployment of NVIDIA Driver is managed by the NVIDIADriver CRD type
	// +operator-sdk:gen-csv:customresourcedefinitions.specDescriptors=true
	// +operator-sdk:gen-csv:customresourcedefinitions.specDescriptors.displayName="Enable NVIDIA Driver deployment through NVIDIADriver CRD type"
	// +operator-sdk:gen-csv:customresourcedefinitions.specDescriptors.x-descriptors="urn:alm:descriptor:com.tectonic.ui:booleanSwitch"
	UseNvidiaDriverCRD *bool `json:"useNvidiaDriverCRD,omitempty"`

	// UsePrecompiled indicates if deployment of NVIDIA Driver using pre-compiled modules is enabled
	// +operator-sdk:gen-csv:customresourcedefinitions.specDescriptors=true
	// +operator-sdk:gen-csv:customresourcedefinitions.specDescriptors.displayName="Enable NVIDIA Driver deployment using pre-compiled modules"
	// +operator-sdk:gen-csv:customresourcedefinitions.specDescriptors.x-descriptors="urn:alm:descriptor:com.tectonic.ui:booleanSwitch"
	UsePrecompiled *bool `json:"usePrecompiled,omitempty"`

	// Deprecated: This field is no longer honored by the gpu-operator. Please use KernelModuleType instead.
	// UseOpenKernelModules indicates if the open GPU kernel modules should be used
	// +operator-sdk:gen-csv:customresourcedefinitions.specDescriptors=true
	// +operator-sdk:gen-csv:customresourcedefinitions.specDescriptors.displayName="Enable use of open GPU kernel modules"
	// +operator-sdk:gen-csv:customresourcedefinitions.specDescriptors.x-descriptors="urn:alm:descriptor:com.tectonic.ui:booleanSwitch,urn:alm:descriptor:com.tectonic.ui:hidden"
	UseOpenKernelModules *bool `json:"useOpenKernelModules,omitempty"`

	// KernelModuleType represents the type of driver kernel modules to be used when installing the GPU driver.
	// Accepted values are auto, proprietary and open. NOTE: If auto is chosen, it means that the recommended kernel module
	// type is chosen based on the GPU devices on the host and the driver branch used
	// +kubebuilder:validation:Enum=auto;open;proprietary
	// +kubebuilder:default=auto
	// +operator-sdk:gen-csv:customresourcedefinitions.specDescriptors.displayName="Kernel Module Type"
	// +operator-sdk:gen-csv:customresourcedefinitions.specDescriptors.description="Kernel Module Type"
	// +operator-sdk:gen-csv:customresourcedefinitions.specDescriptors.x-descriptors="urn:alm:descriptor:com.tectonic.ui:select:auto,urn:alm:descriptor:com.tectonic.ui:select:open,urn:alm:descriptor:com.tectonic.ui:select:proprietary"
	KernelModuleType string `json:"kernelModuleType,omitempty"`

	// Enabled indicates if deployment of NVIDIA Driver through operator is enabled
	// +operator-sdk:gen-csv:customresourcedefinitions.specDescriptors=true
	// +operator-sdk:gen-csv:customresourcedefinitions.specDescriptors.displayName="Enable NVIDIA Driver deployment through GPU Operator"
	// +operator-sdk:gen-csv:customresourcedefinitions.specDescriptors.x-descriptors="urn:alm:descriptor:com.tectonic.ui:booleanSwitch"
	Enabled *bool `json:"enabled,omitempty"`

	// NVIDIA Driver container startup probe settings
	StartupProbe *ContainerProbeSpec `json:"startupProbe,omitempty"`

	// NVIDIA Driver container liveness probe settings
	LivenessProbe *ContainerProbeSpec `json:"livenessProbe,omitempty"`

	// NVIDIA Driver container readiness probe settings
	ReadinessProbe *ContainerProbeSpec `json:"readinessProbe,omitempty"`

	GPUDirectRDMA *GPUDirectRDMASpec `json:"rdma,omitempty"`

	// Driver auto-upgrade settings
	UpgradePolicy *upgrade_v1alpha1.DriverUpgradePolicySpec `json:"upgradePolicy,omitempty"`

	// NVIDIA Driver image repository
	// +kubebuilder:validation:Optional
	Repository string `json:"repository,omitempty"`

	// NVIDIA Driver image name
	// +kubebuilder:validation:Pattern=[a-zA-Z0-9\-]+
	Image string `json:"image,omitempty"`

	// NVIDIA Driver image tag
	// +kubebuilder:validation:Optional
	Version string `json:"version,omitempty"`

	// Image pull policy
	// +kubebuilder:validation:Optional
	// +operator-sdk:gen-csv:customresourcedefinitions.specDescriptors=true
	// +operator-sdk:gen-csv:customresourcedefinitions.specDescriptors.displayName="Image Pull Policy"
	// +operator-sdk:gen-csv:customresourcedefinitions.specDescriptors.x-descriptors="urn:alm:descriptor:com.tectonic.ui:imagePullPolicy"
	ImagePullPolicy string `json:"imagePullPolicy,omitempty"`

	// Image pull secrets
	// +kubebuilder:validation:Optional
	// +operator-sdk:gen-csv:customresourcedefinitions.specDescriptors=true
	// +operator-sdk:gen-csv:customresourcedefinitions.specDescriptors.displayName="Image pull secrets"
	// +operator-sdk:gen-csv:customresourcedefinitions.specDescriptors.x-descriptors="urn:alm:descriptor:io.kubernetes:Secret"
	ImagePullSecrets []string `json:"imagePullSecrets,omitempty"`

	// Manager represents configuration for NVIDIA Driver Manager initContainer
	Manager DriverManagerSpec `json:"manager,omitempty"`

	// Optional: Define resources requests and limits for each pod
	// +operator-sdk:gen-csv:customresourcedefinitions.specDescriptors=true
	// +operator-sdk:gen-csv:customresourcedefinitions.specDescriptors.displayName="Resource Requirements"
	// +operator-sdk:gen-csv:customresourcedefinitions.specDescriptors.x-descriptors="urn:alm:descriptor:com.tectonic.ui:advanced,urn:alm:descriptor:com.tectonic.ui:resourceRequirements"
	Resources *ResourceRequirements `json:"resources,omitempty"`

	// Optional: List of arguments
	// +operator-sdk:gen-csv:customresourcedefinitions.specDescriptors=true
	// +operator-sdk:gen-csv:customresourcedefinitions.specDescriptors.displayName="Arguments"
	// +operator-sdk:gen-csv:customresourcedefinitions.specDescriptors.x-descriptors="urn:alm:descriptor:com.tectonic.ui:advanced,urn:alm:descriptor:com.tectonic.ui:text"
	Args []string `json:"args,omitempty"`

	// Optional: List of environment variables
	// +operator-sdk:gen-csv:customresourcedefinitions.specDescriptors=true
	// +operator-sdk:gen-csv:customresourcedefinitions.specDescriptors.displayName="Environment Variables"
	// +operator-sdk:gen-csv:customresourcedefinitions.specDescriptors.x-descriptors="urn:alm:descriptor:com.tectonic.ui:advanced,urn:alm:descriptor:com.tectonic.ui:text"
	Env []EnvVar `json:"env,omitempty"`

	// Optional: Custom repo configuration for NVIDIA Driver container
	// +operator-sdk:gen-csv:customresourcedefinitions.specDescriptors=true
	// +operator-sdk:gen-csv:customresourcedefinitions.specDescriptors.displayName="Custom Repo Configuration For NVIDIA Driver Container"
	RepoConfig *DriverRepoConfigSpec `json:"repoConfig,omitempty"`

	// Optional: Custom certificates configuration for NVIDIA Driver container
	// +operator-sdk:gen-csv:customresourcedefinitions.specDescriptors=true
	// +operator-sdk:gen-csv:customresourcedefinitions.specDescriptors.displayName="Custom Certificates Configuration For NVIDIA Driver Container"
	CertConfig *DriverCertConfigSpec `json:"certConfig,omitempty"`

	// Optional: Licensing configuration for NVIDIA vGPU licensing
	// +operator-sdk:gen-csv:customresourcedefinitions.specDescriptors=true
	// +operator-sdk:gen-csv:customresourcedefinitions.specDescriptors.displayName="Licensing Configuration For NVIDIA vGPU Driver Container"
	LicensingConfig *DriverLicensingConfigSpec `json:"licensingConfig,omitempty"`

	// Optional: Virtual Topology Daemon configuration for NVIDIA vGPU drivers
	// +operator-sdk:gen-csv:customresourcedefinitions.specDescriptors=true
	// +operator-sdk:gen-csv:customresourcedefinitions.specDescriptors.displayName="Custom Virtual Topology Daemon Configuration For vGPU Driver Container"
	VirtualTopology *VirtualTopologyConfigSpec `json:"virtualTopology,omitempty"`

	// Optional: Kernel module configuration parameters for the NVIDIA Driver
	// +operator-sdk:gen-csv:customresourcedefinitions.specDescriptors=true
	// +operator-sdk:gen-csv:customresourcedefinitions.specDescriptors.displayName="Kernel module configuration parameters for the NVIDIA driver"
	KernelModuleConfig *KernelModuleConfigSpec `json:"kernelModuleConfig,omitempty"`
}

// VGPUManagerSpec defines the properties for the NVIDIA vGPU Manager deployment
type VGPUManagerSpec struct {
	// Enabled indicates if deployment of NVIDIA vGPU Manager through operator is enabled
	// +operator-sdk:gen-csv:customresourcedefinitions.specDescriptors=true
	// +operator-sdk:gen-csv:customresourcedefinitions.specDescriptors.displayName="Enable vgpu host driver deployment through GPU Operator"
	// +operator-sdk:gen-csv:customresourcedefinitions.specDescriptors.x-descriptors="urn:alm:descriptor:com.tectonic.ui:booleanSwitch"
	Enabled *bool `json:"enabled,omitempty"`

	// NVIDIA vGPU Manager image repository
	// +kubebuilder:validation:Optional
	Repository string `json:"repository,omitempty"`

	// NVIDIA vGPU Manager  image name
	// +kubebuilder:validation:Pattern=[a-zA-Z0-9\-]+
	Image string `json:"image,omitempty"`

	// NVIDIA vGPU Manager image tag
	// +kubebuilder:validation:Optional
	Version string `json:"version,omitempty"`

	// Image pull policy
	// +kubebuilder:validation:Optional
	// +operator-sdk:gen-csv:customresourcedefinitions.specDescriptors=true
	// +operator-sdk:gen-csv:customresourcedefinitions.specDescriptors.displayName="Image Pull Policy"
	// +operator-sdk:gen-csv:customresourcedefinitions.specDescriptors.x-descriptors="urn:alm:descriptor:com.tectonic.ui:imagePullPolicy"
	ImagePullPolicy string `json:"imagePullPolicy,omitempty"`

	// Image pull secrets
	// +kubebuilder:validation:Optional
	// +operator-sdk:gen-csv:customresourcedefinitions.specDescriptors=true
	// +operator-sdk:gen-csv:customresourcedefinitions.specDescriptors.displayName="Image pull secrets"
	// +operator-sdk:gen-csv:customresourcedefinitions.specDescriptors.x-descriptors="urn:alm:descriptor:io.kubernetes:Secret"
	ImagePullSecrets []string `json:"imagePullSecrets,omitempty"`

	// Optional: Define resources requests and limits for each pod
	// +operator-sdk:gen-csv:customresourcedefinitions.specDescriptors=true
	// +operator-sdk:gen-csv:customresourcedefinitions.specDescriptors.displayName="Resource Requirements"
	// +operator-sdk:gen-csv:customresourcedefinitions.specDescriptors.x-descriptors="urn:alm:descriptor:com.tectonic.ui:advanced,urn:alm:descriptor:com.tectonic.ui:resourceRequirements"
	Resources *ResourceRequirements `json:"resources,omitempty"`

	// Optional: List of arguments
	// +operator-sdk:gen-csv:customresourcedefinitions.specDescriptors=true
	// +operator-sdk:gen-csv:customresourcedefinitions.specDescriptors.displayName="Arguments"
	// +operator-sdk:gen-csv:customresourcedefinitions.specDescriptors.x-descriptors="urn:alm:descriptor:com.tectonic.ui:advanced,urn:alm:descriptor:com.tectonic.ui:text"
	Args []string `json:"args,omitempty"`

	// Optional: List of environment variables
	// +operator-sdk:gen-csv:customresourcedefinitions.specDescriptors=true
	// +operator-sdk:gen-csv:customresourcedefinitions.specDescriptors.displayName="Environment Variables"
	// +operator-sdk:gen-csv:customresourcedefinitions.specDescriptors.x-descriptors="urn:alm:descriptor:com.tectonic.ui:advanced,urn:alm:descriptor:com.tectonic.ui:text"
	Env []EnvVar `json:"env,omitempty"`

	// DriverManager represents configuration for NVIDIA Driver Manager initContainer
	DriverManager DriverManagerSpec `json:"driverManager,omitempty"`
}

// ToolkitSpec defines the properties for NVIDIA Container Toolkit deployment
type ToolkitSpec struct {
	// Enabled indicates if deployment of NVIDIA Container Toolkit through operator is enabled
	// +operator-sdk:gen-csv:customresourcedefinitions.specDescriptors=true
	// +operator-sdk:gen-csv:customresourcedefinitions.specDescriptors.displayName="Enable NVIDIA Container Toolkit deployment through GPU Operator"
	// +operator-sdk:gen-csv:customresourcedefinitions.specDescriptors.x-descriptors="urn:alm:descriptor:com.tectonic.ui:booleanSwitch"
	Enabled *bool `json:"enabled,omitempty"`

	// NVIDIA Container Toolkit image repository
	// +kubebuilder:validation:Optional
	Repository string `json:"repository,omitempty"`

	// NVIDIA Container Toolkit image name
	// +kubebuilder:validation:Pattern=[a-zA-Z0-9\-]+
	Image string `json:"image,omitempty"`

	// NVIDIA Container Toolkit image tag
	// +kubebuilder:validation:Optional
	Version string `json:"version,omitempty"`

	// Image pull policy
	// +kubebuilder:validation:Optional
	// +operator-sdk:gen-csv:customresourcedefinitions.specDescriptors=true
	// +operator-sdk:gen-csv:customresourcedefinitions.specDescriptors.displayName="Image Pull Policy"
	// +operator-sdk:gen-csv:customresourcedefinitions.specDescriptors.x-descriptors="urn:alm:descriptor:com.tectonic.ui:imagePullPolicy"
	ImagePullPolicy string `json:"imagePullPolicy,omitempty"`

	// Image pull secrets
	// +kubebuilder:validation:Optional
	// +operator-sdk:gen-csv:customresourcedefinitions.specDescriptors=true
	// +operator-sdk:gen-csv:customresourcedefinitions.specDescriptors.displayName="Image pull secrets"
	// +operator-sdk:gen-csv:customresourcedefinitions.specDescriptors.x-descriptors="urn:alm:descriptor:io.kubernetes:Secret"
	ImagePullSecrets []string `json:"imagePullSecrets,omitempty"`

	// Optional: Define resources requests and limits for each pod
	// +operator-sdk:gen-csv:customresourcedefinitions.specDescriptors=true
	// +operator-sdk:gen-csv:customresourcedefinitions.specDescriptors.displayName="Resource Requirements"
	// +operator-sdk:gen-csv:customresourcedefinitions.specDescriptors.x-descriptors="urn:alm:descriptor:com.tectonic.ui:advanced,urn:alm:descriptor:com.tectonic.ui:resourceRequirements"
	Resources *ResourceRequirements `json:"resources,omitempty"`

	// Optional: List of arguments
	// +operator-sdk:gen-csv:customresourcedefinitions.specDescriptors=true
	// +operator-sdk:gen-csv:customresourcedefinitions.specDescriptors.displayName="Arguments"
	// +operator-sdk:gen-csv:customresourcedefinitions.specDescriptors.x-descriptors="urn:alm:descriptor:com.tectonic.ui:advanced,urn:alm:descriptor:com.tectonic.ui:text"
	Args []string `json:"args,omitempty"`

	// Optional: List of environment variables
	// +operator-sdk:gen-csv:customresourcedefinitions.specDescriptors=true
	// +operator-sdk:gen-csv:customresourcedefinitions.specDescriptors.displayName="Environment Variables"
	// +operator-sdk:gen-csv:customresourcedefinitions.specDescriptors.x-descriptors="urn:alm:descriptor:com.tectonic.ui:advanced,urn:alm:descriptor:com.tectonic.ui:text"
	Env []EnvVar `json:"env,omitempty"`

	// Toolkit install directory on the host
	// +kubebuilder:validation:Optional
	// +kubebuilder:default=/usr/local/nvidia
	// +operator-sdk:gen-csv:customresourcedefinitions.specDescriptors=true
	// +operator-sdk:gen-csv:customresourcedefinitions.specDescriptors.displayName="Toolkit install directory on the host"
	// +operator-sdk:gen-csv:customresourcedefinitions.specDescriptors.x-descriptors="urn:alm:descriptor:com.tectonic.ui:text"
	InstallDir string `json:"installDir,omitempty"`
}

// DevicePluginSpec defines the properties for NVIDIA Device Plugin deployment
type DevicePluginSpec struct {
	// Enabled indicates if deployment of NVIDIA Device Plugin through operator is enabled
	// +operator-sdk:gen-csv:customresourcedefinitions.specDescriptors=true
	// +operator-sdk:gen-csv:customresourcedefinitions.specDescriptors.displayName="Enable NVIDIA Device Plugin deployment through GPU Operator"
	// +operator-sdk:gen-csv:customresourcedefinitions.specDescriptors.x-descriptors="urn:alm:descriptor:com.tectonic.ui:booleanSwitch"
	Enabled *bool `json:"enabled,omitempty"`

	// NVIDIA Device Plugin image repository
	// +kubebuilder:validation:Optional
	Repository string `json:"repository,omitempty"`

	// NVIDIA Device Plugin image name
	// +kubebuilder:validation:Pattern=[a-zA-Z0-9\-]+
	Image string `json:"image,omitempty"`

	// NVIDIA Device Plugin image tag
	// +kubebuilder:validation:Optional
	Version string `json:"version,omitempty"`

	// Image pull policy
	// +kubebuilder:validation:Optional
	// +operator-sdk:gen-csv:customresourcedefinitions.specDescriptors=true
	// +operator-sdk:gen-csv:customresourcedefinitions.specDescriptors.displayName="Image Pull Policy"
	// +operator-sdk:gen-csv:customresourcedefinitions.specDescriptors.x-descriptors="urn:alm:descriptor:com.tectonic.ui:imagePullPolicy"
	ImagePullPolicy string `json:"imagePullPolicy,omitempty"`

	// Image pull secrets
	// +kubebuilder:validation:Optional
	// +operator-sdk:gen-csv:customresourcedefinitions.specDescriptors=true
	// +operator-sdk:gen-csv:customresourcedefinitions.specDescriptors.displayName="Image pull secrets"
	// +operator-sdk:gen-csv:customresourcedefinitions.specDescriptors.x-descriptors="urn:alm:descriptor:io.kubernetes:Secret"
	ImagePullSecrets []string `json:"imagePullSecrets,omitempty"`

	// Optional: Define resources requests and limits for each pod
	// +operator-sdk:gen-csv:customresourcedefinitions.specDescriptors=true
	// +operator-sdk:gen-csv:customresourcedefinitions.specDescriptors.displayName="Resource Requirements"
	// +operator-sdk:gen-csv:customresourcedefinitions.specDescriptors.x-descriptors="urn:alm:descriptor:com.tectonic.ui:advanced,urn:alm:descriptor:com.tectonic.ui:resourceRequirements"
	Resources *ResourceRequirements `json:"resources,omitempty"`

	// Optional: List of arguments
	// +operator-sdk:gen-csv:customresourcedefinitions.specDescriptors=true
	// +operator-sdk:gen-csv:customresourcedefinitions.specDescriptors.displayName="Arguments"
	// +operator-sdk:gen-csv:customresourcedefinitions.specDescriptors.x-descriptors="urn:alm:descriptor:com.tectonic.ui:advanced,urn:alm:descriptor:com.tectonic.ui:text"
	Args []string `json:"args,omitempty"`

	// Optional: List of environment variables
	// +operator-sdk:gen-csv:customresourcedefinitions.specDescriptors=true
	// +operator-sdk:gen-csv:customresourcedefinitions.specDescriptors.displayName="Environment Variables"
	// +operator-sdk:gen-csv:customresourcedefinitions.specDescriptors.x-descriptors="urn:alm:descriptor:com.tectonic.ui:advanced,urn:alm:descriptor:com.tectonic.ui:text"
	Env []EnvVar `json:"env,omitempty"`

	// Optional: Configuration for the NVIDIA Device Plugin via the ConfigMap
	// +operator-sdk:gen-csv:customresourcedefinitions.specDescriptors=true
	// +operator-sdk:gen-csv:customresourcedefinitions.specDescriptors.displayName="Configuration for the NVIDIA Device Plugin via the ConfigMap"
	Config *DevicePluginConfig `json:"config,omitempty"`

	// Optional: MPS related configuration for the NVIDIA Device Plugin
	// +operator-sdk:gen-csv:customresourcedefinitions.specDescriptors=true
	// +operator-sdk:gen-csv:customresourcedefinitions.specDescriptors.displayName="MPS related configuration for the NVIDIA Device Plugin"
	MPS *MPSConfig `json:"mps,omitempty"`
}

// DevicePluginConfig defines ConfigMap name for NVIDIA Device Plugin config
type DevicePluginConfig struct {
	// ConfigMap name for NVIDIA Device Plugin config including shared config between plugin and GFD
	// +kubebuilder:validation:Optional
	// +operator-sdk:gen-csv:customresourcedefinitions.specDescriptors=true
	// +operator-sdk:gen-csv:customresourcedefinitions.specDescriptors.displayName="ConfigMap name for NVIDIA Device Plugin including shared config between plugin and GFD"
	// +operator-sdk:gen-csv:customresourcedefinitions.specDescriptors.x-descriptors="urn:alm:descriptor:com.tectonic.ui:text"
	Name string `json:"name,omitempty"`
	// Default config name within the ConfigMap for the NVIDIA Device Plugin  config
	// +kubebuilder:validation:Optional
	// +operator-sdk:gen-csv:customresourcedefinitions.specDescriptors=true
	// +operator-sdk:gen-csv:customresourcedefinitions.specDescriptors.displayName="Default config name within the ConfigMap for the NVIDIA Device Plugin config"
	// +operator-sdk:gen-csv:customresourcedefinitions.specDescriptors.x-descriptors="urn:alm:descriptor:com.tectonic.ui:text"
	Default string `json:"default,omitempty"`
}

// MPSConfig defines MPS related configuration for the NVIDIA Device Plugin
type MPSConfig struct {
	// Root defines the MPS root path on the host
	// +kubebuilder:validation:Optional
	// +kubebuilder:default=/run/nvidia/mps
	// +operator-sdk:gen-csv:customresourcedefinitions.specDescriptors=true
	// +operator-sdk:gen-csv:customresourcedefinitions.specDescriptors.displayName="MPS root path on the host"
	// +operator-sdk:gen-csv:customresourcedefinitions.specDescriptors.x-descriptors="urn:alm:descriptor:com.tectonic.ui:text"
	Root string `json:"root,omitempty"`
}

// SandboxDevicePluginSpec defines the properties for the NVIDIA Sandbox Device Plugin deployment
type SandboxDevicePluginSpec struct {
	// Enabled indicates if deployment of NVIDIA Sandbox Device Plugin through operator is enabled
	// +operator-sdk:gen-csv:customresourcedefinitions.specDescriptors=true
	// +operator-sdk:gen-csv:customresourcedefinitions.specDescriptors.displayName="Enable NVIDIA Sandbox Device Plugin deployment through GPU Operator"
	// +operator-sdk:gen-csv:customresourcedefinitions.specDescriptors.x-descriptors="urn:alm:descriptor:com.tectonic.ui:booleanSwitch"
	Enabled *bool `json:"enabled,omitempty"`

	// NVIDIA Sandbox Device Plugin image repository
	// +kubebuilder:validation:Optional
	Repository string `json:"repository,omitempty"`

	// NVIDIA Sandbox Device Plugin image name
	// +kubebuilder:validation:Pattern=[a-zA-Z0-9\-]+
	Image string `json:"image,omitempty"`

	// NVIDIA Sandbox Device Plugin image tag
	// +kubebuilder:validation:Optional
	Version string `json:"version,omitempty"`

	// Image pull policy
	// +kubebuilder:validation:Optional
	// +operator-sdk:gen-csv:customresourcedefinitions.specDescriptors=true
	// +operator-sdk:gen-csv:customresourcedefinitions.specDescriptors.displayName="Image Pull Policy"
	// +operator-sdk:gen-csv:customresourcedefinitions.specDescriptors.x-descriptors="urn:alm:descriptor:com.tectonic.ui:imagePullPolicy"
	ImagePullPolicy string `json:"imagePullPolicy,omitempty"`

	// Image pull secrets
	// +kubebuilder:validation:Optional
	// +operator-sdk:gen-csv:customresourcedefinitions.specDescriptors=true
	// +operator-sdk:gen-csv:customresourcedefinitions.specDescriptors.displayName="Image pull secrets"
	// +operator-sdk:gen-csv:customresourcedefinitions.specDescriptors.x-descriptors="urn:alm:descriptor:io.kubernetes:Secret"
	ImagePullSecrets []string `json:"imagePullSecrets,omitempty"`

	// Optional: Define resources requests and limits for each pod
	// +operator-sdk:gen-csv:customresourcedefinitions.specDescriptors=true
	// +operator-sdk:gen-csv:customresourcedefinitions.specDescriptors.displayName="Resource Requirements"
	// +operator-sdk:gen-csv:customresourcedefinitions.specDescriptors.x-descriptors="urn:alm:descriptor:com.tectonic.ui:advanced,urn:alm:descriptor:com.tectonic.ui:resourceRequirements"
	Resources *ResourceRequirements `json:"resources,omitempty"`

	// Optional: List of arguments
	// +operator-sdk:gen-csv:customresourcedefinitions.specDescriptors=true
	// +operator-sdk:gen-csv:customresourcedefinitions.specDescriptors.displayName="Arguments"
	// +operator-sdk:gen-csv:customresourcedefinitions.specDescriptors.x-descriptors="urn:alm:descriptor:com.tectonic.ui:advanced,urn:alm:descriptor:com.tectonic.ui:text"
	Args []string `json:"args,omitempty"`

	// Optional: List of environment variables
	// +operator-sdk:gen-csv:customresourcedefinitions.specDescriptors=true
	// +operator-sdk:gen-csv:customresourcedefinitions.specDescriptors.displayName="Environment Variables"
	// +operator-sdk:gen-csv:customresourcedefinitions.specDescriptors.x-descriptors="urn:alm:descriptor:com.tectonic.ui:advanced,urn:alm:descriptor:com.tectonic.ui:text"
	Env []EnvVar `json:"env,omitempty"`
}

// DCGMExporterSpec defines the properties for NVIDIA DCGM Exporter deployment
type DCGMExporterSpec struct {
	// Enabled indicates if deployment of NVIDIA DCGM Exporter through operator is enabled
	// +operator-sdk:gen-csv:customresourcedefinitions.specDescriptors=true
	// +operator-sdk:gen-csv:customresourcedefinitions.specDescriptors.displayName="Enable NVIDIA DCGM Exporter deployment through GPU Operator"
	// +operator-sdk:gen-csv:customresourcedefinitions.specDescriptors.x-descriptors="urn:alm:descriptor:com.tectonic.ui:booleanSwitch"
	Enabled *bool `json:"enabled,omitempty"`

	// NVIDIA DCGM Exporter image repository
	// +kubebuilder:validation:Optional
	Repository string `json:"repository,omitempty"`

	// NVIDIA DCGM Exporter image name
	// +kubebuilder:validation:Pattern=[a-zA-Z0-9\-]+
	Image string `json:"image,omitempty"`

	// NVIDIA DCGM Exporter image tag
	// +kubebuilder:validation:Optional
	Version string `json:"version,omitempty"`

	// Image pull policy
	// +kubebuilder:validation:Optional
	// +operator-sdk:gen-csv:customresourcedefinitions.specDescriptors=true
	// +operator-sdk:gen-csv:customresourcedefinitions.specDescriptors.displayName="Image Pull Policy"
	// +operator-sdk:gen-csv:customresourcedefinitions.specDescriptors.x-descriptors="urn:alm:descriptor:com.tectonic.ui:imagePullPolicy"
	ImagePullPolicy string `json:"imagePullPolicy,omitempty"`

	// Image pull secrets
	// +kubebuilder:validation:Optional
	// +operator-sdk:gen-csv:customresourcedefinitions.specDescriptors=true
	// +operator-sdk:gen-csv:customresourcedefinitions.specDescriptors.displayName="Image pull secrets"
	// +operator-sdk:gen-csv:customresourcedefinitions.specDescriptors.x-descriptors="urn:alm:descriptor:io.kubernetes:Secret"
	ImagePullSecrets []string `json:"imagePullSecrets,omitempty"`

	// Optional: Define resources requests and limits for each pod
	// +operator-sdk:gen-csv:customresourcedefinitions.specDescriptors=true
	// +operator-sdk:gen-csv:customresourcedefinitions.specDescriptors.displayName="Resource Requirements"
	// +operator-sdk:gen-csv:customresourcedefinitions.specDescriptors.x-descriptors="urn:alm:descriptor:com.tectonic.ui:advanced,urn:alm:descriptor:com.tectonic.ui:resourceRequirements"
	Resources *ResourceRequirements `json:"resources,omitempty"`

	// Optional: List of arguments
	// +operator-sdk:gen-csv:customresourcedefinitions.specDescriptors=true
	// +operator-sdk:gen-csv:customresourcedefinitions.specDescriptors.displayName="Arguments"
	// +operator-sdk:gen-csv:customresourcedefinitions.specDescriptors.x-descriptors="urn:alm:descriptor:com.tectonic.ui:advanced,urn:alm:descriptor:com.tectonic.ui:text"
	Args []string `json:"args,omitempty"`

	// Optional: List of environment variables
	// +operator-sdk:gen-csv:customresourcedefinitions.specDescriptors=true
	// +operator-sdk:gen-csv:customresourcedefinitions.specDescriptors.displayName="Environment Variables"
	// +operator-sdk:gen-csv:customresourcedefinitions.specDescriptors.x-descriptors="urn:alm:descriptor:com.tectonic.ui:advanced,urn:alm:descriptor:com.tectonic.ui:text"
	Env []EnvVar `json:"env,omitempty"`

	// Optional: Custom metrics configuration for NVIDIA DCGM Exporter
	// +operator-sdk:gen-csv:customresourcedefinitions.specDescriptors=true
	// +operator-sdk:gen-csv:customresourcedefinitions.specDescriptors.displayName="Custom Metrics Configuration For DCGM Exporter"
	MetricsConfig *DCGMExporterMetricsConfig `json:"config,omitempty"`

	// Optional: ServiceMonitor configuration for NVIDIA DCGM Exporter
	// +operator-sdk:gen-csv:customresourcedefinitions.specDescriptors=true
	// +operator-sdk:gen-csv:customresourcedefinitions.specDescriptors.displayName="ServiceMonitor configuration for NVIDIA DCGM Exporter"
	ServiceMonitor *DCGMExporterServiceMonitorConfig `json:"serviceMonitor,omitempty"`

	// Optional: Service configuration for NVIDIA DCGM Exporter
	// +operator-sdk:gen-csv:customresourcedefinitions.specDescriptors=true
	// +operator-sdk:gen-csv:customresourcedefinitions.specDescriptors.displayName="Service configuration for NVIDIA DCGM Exporter"
	ServiceSpec *DCGMExporterServiceConfig `json:"service,omitempty"`
}

// DCGMExporterMetricsConfig defines metrics to be collected by NVIDIA DCGM Exporter
type DCGMExporterMetricsConfig struct {
	// ConfigMap name with file dcgm-metrics.csv for metrics to be collected by NVIDIA DCGM Exporter
	// +kubebuilder:validation:Optional
	// +operator-sdk:gen-csv:customresourcedefinitions.specDescriptors=true
	// +operator-sdk:gen-csv:customresourcedefinitions.specDescriptors.displayName="ConfigMap name with file dcgm-metrics.csv"
	// +operator-sdk:gen-csv:customresourcedefinitions.specDescriptors.x-descriptors="urn:alm:descriptor:com.tectonic.ui:text"
	Name string `json:"name,omitempty"`
}

// DCGMExporterServiceConfig defines the configuration options for the Kubernetes Service deployed for DCGM Exporter
type DCGMExporterServiceConfig struct {
	// Type represents the ServiceType which describes ingress methods for a service
	// +operator-sdk:gen-csv:customresourcedefinitions.specDescriptors=true
	// +operator-sdk:gen-csv:customresourcedefinitions.specDescriptors.displayName="ServiceType for the DCGM Exporter K8s Service"
	// +operator-sdk:gen-csv:customresourcedefinitions.specDescriptors.x-descriptors="urn:alm:descriptor:com.tectonic.ui:text"
	Type corev1.ServiceType `json:"type,omitempty"`

	// InternalTrafficPolicy describes how nodes distribute service traffic they receive on the ClusterIP.
	// +operator-sdk:gen-csv:customresourcedefinitions.specDescriptors=true
	// +operator-sdk:gen-csv:customresourcedefinitions.specDescriptors.displayName="Internal Traffic Policy for the DCGM Exporter K8s Service"
	// +operator-sdk:gen-csv:customresourcedefinitions.specDescriptors.x-descriptors="urn:alm:descriptor:com.tectonic.ui:text"
	InternalTrafficPolicy *corev1.ServiceInternalTrafficPolicy `json:"internalTrafficPolicy,omitempty"`
}

// DCGMExporterServiceMonitorConfig defines configuration options for the ServiceMonitor
// deployed for DCGM Exporter
type DCGMExporterServiceMonitorConfig struct {
	// Enabled indicates if ServiceMonitor is deployed for NVIDIA DCGM Exporter
	// +operator-sdk:gen-csv:customresourcedefinitions.specDescriptors=true
	// +operator-sdk:gen-csv:customresourcedefinitions.specDescriptors.displayName="Enable deployment of NVIDIA DCGM Exporter ServiceMonitor"
	// +operator-sdk:gen-csv:customresourcedefinitions.specDescriptors.x-descriptors="urn:alm:descriptor:com.tectonic.ui:booleanSwitch"
	Enabled *bool `json:"enabled,omitempty"`

	// Interval which metrics should be scraped from NVIDIA DCGM Exporter. If not specified Prometheus’ global scrape interval is used.
	// Supported units: y, w, d, h, m, s, ms
	// +operator-sdk:gen-csv:customresourcedefinitions.specDescriptors=true
	// +operator-sdk:gen-csv:customresourcedefinitions.specDescriptors.displayName="Interval which metrics should be scraped from NVDIA DCGM Exporter"
	// +operator-sdk:gen-csv:customresourcedefinitions.specDescriptors.x-descriptors="urn:alm:descriptor:com.tectonic.ui:text"
	Interval promv1.Duration `json:"interval,omitempty"`

	// HonorLabels chooses the metric’s labels on collisions with target labels.
	// +operator-sdk:gen-csv:customresourcedefinitions.specDescriptors=true
	// +operator-sdk:gen-csv:customresourcedefinitions.specDescriptors.displayName="Choose the metric's label on collisions with target labels"
	// +operator-sdk:gen-csv:customresourcedefinitions.specDescriptors.x-descriptors="urn:alm:descriptor:com.tectonic.ui:booleanSwitch"
	HonorLabels *bool `json:"honorLabels,omitempty"`

	// AdditionalLabels to add to ServiceMonitor instance for NVIDIA DCGM Exporter
	// +operator-sdk:gen-csv:customresourcedefinitions.specDescriptors=true
	// +operator-sdk:gen-csv:customresourcedefinitions.specDescriptors.displayName="Additional labels to add to ServiceMonitor instance for NVIDIA DCGM Exporter"
	// +operator-sdk:gen-csv:customresourcedefinitions.specDescriptors.x-descriptors="urn:alm:descriptor:com.tectonic.ui:text"
	AdditionalLabels map[string]string `json:"additionalLabels,omitempty"`

	// Relabelings allows to rewrite labels on metric sets for NVIDIA DCGM Exporter
	// +operator-sdk:gen-csv:customresourcedefinitions.specDescriptors=true
	// +operator-sdk:gen-csv:customresourcedefinitions.specDescriptors.displayName="Relabelings allows to rewrite labels on metric sets for NVIDIA DCGM Exporter"
	Relabelings []*promv1.RelabelConfig `json:"relabelings,omitempty"`
}

// DCGMSpec defines the properties for NVIDIA DCGM deployment
type DCGMSpec struct {
	// Enabled indicates if deployment of NVIDIA DCGM Hostengine as a separate pod is enabled.
	// +operator-sdk:gen-csv:customresourcedefinitions.specDescriptors=true
	// +operator-sdk:gen-csv:customresourcedefinitions.specDescriptors.displayName="Enable NVIDIA DCGM hostengine as a separate Pod"
	// +operator-sdk:gen-csv:customresourcedefinitions.specDescriptors.x-descriptors="urn:alm:descriptor:com.tectonic.ui:booleanSwitch"
	Enabled *bool `json:"enabled,omitempty"`

	// NVIDIA DCGM image repository
	// +kubebuilder:validation:Optional
	Repository string `json:"repository,omitempty"`

	// NVIDIA DCGM image name
	// +kubebuilder:validation:Pattern=[a-zA-Z0-9\-]+
	Image string `json:"image,omitempty"`

	// NVIDIA DCGM image tag
	// +kubebuilder:validation:Optional
	Version string `json:"version,omitempty"`

	// Image pull policy
	// +kubebuilder:validation:Optional
	// +operator-sdk:gen-csv:customresourcedefinitions.specDescriptors=true
	// +operator-sdk:gen-csv:customresourcedefinitions.specDescriptors.displayName="Image Pull Policy"
	// +operator-sdk:gen-csv:customresourcedefinitions.specDescriptors.x-descriptors="urn:alm:descriptor:com.tectonic.ui:imagePullPolicy"
	ImagePullPolicy string `json:"imagePullPolicy,omitempty"`

	// Image pull secrets
	// +kubebuilder:validation:Optional
	// +operator-sdk:gen-csv:customresourcedefinitions.specDescriptors=true
	// +operator-sdk:gen-csv:customresourcedefinitions.specDescriptors.displayName="Image pull secrets"
	// +operator-sdk:gen-csv:customresourcedefinitions.specDescriptors.x-descriptors="urn:alm:descriptor:io.kubernetes:Secret"
	ImagePullSecrets []string `json:"imagePullSecrets,omitempty"`

	// Optional: Define resources requests and limits for each pod
	// +operator-sdk:gen-csv:customresourcedefinitions.specDescriptors=true
	// +operator-sdk:gen-csv:customresourcedefinitions.specDescriptors.displayName="Resource Requirements"
	// +operator-sdk:gen-csv:customresourcedefinitions.specDescriptors.x-descriptors="urn:alm:descriptor:com.tectonic.ui:advanced,urn:alm:descriptor:com.tectonic.ui:resourceRequirements"
	Resources *ResourceRequirements `json:"resources,omitempty"`

	// Optional: List of arguments
	// +operator-sdk:gen-csv:customresourcedefinitions.specDescriptors=true
	// +operator-sdk:gen-csv:customresourcedefinitions.specDescriptors.displayName="Arguments"
	// +operator-sdk:gen-csv:customresourcedefinitions.specDescriptors.x-descriptors="urn:alm:descriptor:com.tectonic.ui:advanced,urn:alm:descriptor:com.tectonic.ui:text"
	Args []string `json:"args,omitempty"`

	// Optional: List of environment variables
	// +operator-sdk:gen-csv:customresourcedefinitions.specDescriptors=true
	// +operator-sdk:gen-csv:customresourcedefinitions.specDescriptors.displayName="Environment Variables"
	// +operator-sdk:gen-csv:customresourcedefinitions.specDescriptors.x-descriptors="urn:alm:descriptor:com.tectonic.ui:advanced,urn:alm:descriptor:com.tectonic.ui:text"
	Env []EnvVar `json:"env,omitempty"`

	// Deprecated: HostPort represents host port that needs to be bound for DCGM engine (Default: 5555)
	// +operator-sdk:gen-csv:customresourcedefinitions.specDescriptors.displayName="Host port to bind for DCGM engine"
	// +operator-sdk:gen-csv:customresourcedefinitions.specDescriptors.x-descriptors="urn:alm:descriptor:com.tectonic.ui:number"
	HostPort int32 `json:"hostPort,omitempty"`
}

// NodeStatusExporterSpec defines the properties for node-status-exporter state
type NodeStatusExporterSpec struct {
	// Enabled indicates if deployment of Node Status Exporter is enabled.
	// +operator-sdk:gen-csv:customresourcedefinitions.specDescriptors=true
	// +operator-sdk:gen-csv:customresourcedefinitions.specDescriptors.displayName="Enable Node Status Exporter deployment through GPU Operator"
	// +operator-sdk:gen-csv:customresourcedefinitions.specDescriptors.x-descriptors="urn:alm:descriptor:com.tectonic.ui:booleanSwitch"
	Enabled *bool `json:"enabled,omitempty"`

	// Node Status Exporterimage repository
	// +kubebuilder:validation:Optional
	Repository string `json:"repository,omitempty"`

	// Node Status Exporter image name
	// +kubebuilder:validation:Pattern=[a-zA-Z0-9\-]+
	Image string `json:"image,omitempty"`

	// Node Status Exporterimage tag
	// +kubebuilder:validation:Optional
	Version string `json:"version,omitempty"`

	// Image pull policy
	// +kubebuilder:validation:Optional
	// +operator-sdk:gen-csv:customresourcedefinitions.specDescriptors=true
	// +operator-sdk:gen-csv:customresourcedefinitions.specDescriptors.displayName="Image Pull Policy"
	// +operator-sdk:gen-csv:customresourcedefinitions.specDescriptors.x-descriptors="urn:alm:descriptor:com.tectonic.ui:imagePullPolicy"
	ImagePullPolicy string `json:"imagePullPolicy,omitempty"`

	// Image pull secrets
	// +kubebuilder:validation:Optional
	// +operator-sdk:gen-csv:customresourcedefinitions.specDescriptors=true
	// +operator-sdk:gen-csv:customresourcedefinitions.specDescriptors.displayName="Image pull secrets"
	// +operator-sdk:gen-csv:customresourcedefinitions.specDescriptors.x-descriptors="urn:alm:descriptor:io.kubernetes:Secret"
	ImagePullSecrets []string `json:"imagePullSecrets,omitempty"`

	// Optional: Define resources requests and limits for each pod
	// +operator-sdk:gen-csv:customresourcedefinitions.specDescriptors=true
	// +operator-sdk:gen-csv:customresourcedefinitions.specDescriptors.displayName="Resource Requirements"
	// +operator-sdk:gen-csv:customresourcedefinitions.specDescriptors.x-descriptors="urn:alm:descriptor:com.tectonic.ui:advanced,urn:alm:descriptor:com.tectonic.ui:resourceRequirements"
	Resources *ResourceRequirements `json:"resources,omitempty"`

	// Optional: List of arguments
	// +operator-sdk:gen-csv:customresourcedefinitions.specDescriptors=true
	// +operator-sdk:gen-csv:customresourcedefinitions.specDescriptors.displayName="Arguments"
	// +operator-sdk:gen-csv:customresourcedefinitions.specDescriptors.x-descriptors="urn:alm:descriptor:com.tectonic.ui:advanced,urn:alm:descriptor:com.tectonic.ui:text"
	Args []string `json:"args,omitempty"`

	// Optional: List of environment variables
	// +operator-sdk:gen-csv:customresourcedefinitions.specDescriptors=true
	// +operator-sdk:gen-csv:customresourcedefinitions.specDescriptors.displayName="Environment Variables"
	// +operator-sdk:gen-csv:customresourcedefinitions.specDescriptors.x-descriptors="urn:alm:descriptor:com.tectonic.ui:advanced,urn:alm:descriptor:com.tectonic.ui:text"
	Env []EnvVar `json:"env,omitempty"`
}

// DriverRepoConfigSpec defines custom repo configuration for NVIDIA Driver container
type DriverRepoConfigSpec struct {
	// +kubebuilder:validation:Optional
	// +operator-sdk:gen-csv:customresourcedefinitions.specDescriptors=true
	// +operator-sdk:gen-csv:customresourcedefinitions.specDescriptors.displayName="ConfigMap Name"
	// +operator-sdk:gen-csv:customresourcedefinitions.specDescriptors.x-descriptors="urn:alm:descriptor:com.tectonic.ui:text"
	ConfigMapName string `json:"configMapName,omitempty"`
}

// DriverCertConfigSpec defines custom certificates configuration for NVIDIA Driver container
type DriverCertConfigSpec struct {
	// +kubebuilder:validation:Optional
	// +operator-sdk:gen-csv:customresourcedefinitions.specDescriptors=true
	// +operator-sdk:gen-csv:customresourcedefinitions.specDescriptors.displayName="ConfigMap Name"
	// +operator-sdk:gen-csv:customresourcedefinitions.specDescriptors.x-descriptors="urn:alm:descriptor:com.tectonic.ui:text"
	Name string `json:"name,omitempty"`
}

// DriverLicensingConfigSpec defines licensing server configuration for NVIDIA Driver container
type DriverLicensingConfigSpec struct {
	// +kubebuilder:validation:Optional
	// +operator-sdk:gen-csv:customresourcedefinitions.specDescriptors=true
	// +operator-sdk:gen-csv:customresourcedefinitions.specDescriptors.displayName="ConfigMap Name"
	// +operator-sdk:gen-csv:customresourcedefinitions.specDescriptors.x-descriptors="urn:alm:descriptor:com.tectonic.ui:text"
	ConfigMapName string `json:"configMapName,omitempty"`

	// NLSEnabled indicates if NVIDIA Licensing System is used for licensing.
	// +operator-sdk:gen-csv:customresourcedefinitions.specDescriptors=true
	// +operator-sdk:gen-csv:customresourcedefinitions.specDescriptors.displayName="Enable NVIDIA Licensing System licensing"
	// +operator-sdk:gen-csv:customresourcedefinitions.specDescriptors.x-descriptors="urn:alm:descriptor:com.tectonic.ui:booleanSwitch"
	NLSEnabled *bool `json:"nlsEnabled,omitempty"`
}

// VirtualTopologyConfigSpec defines virtual topology daemon configuration with NVIDIA vGPU
type VirtualTopologyConfigSpec struct {
	// Optional: Config name representing virtual topology daemon configuration file nvidia-topologyd.conf
	// +kubebuilder:validation:Optional
	// +operator-sdk:gen-csv:customresourcedefinitions.specDescriptors=true
	// +operator-sdk:gen-csv:customresourcedefinitions.specDescriptors.displayName="ConfigMap Name"
	// +operator-sdk:gen-csv:customresourcedefinitions.specDescriptors.x-descriptors="urn:alm:descriptor:com.tectonic.ui:text"
	Config string `json:"config,omitempty"`
}

// KernelModuleConfigSpec defines custom configuration parameters for the NVIDIA Driver
type KernelModuleConfigSpec struct {
	// +kubebuilder:validation:Optional
	// +operator-sdk:gen-csv:customresourcedefinitions.specDescriptors=true
	// +operator-sdk:gen-csv:customresourcedefinitions.specDescriptors.displayName="ConfigMap Name"
	// +operator-sdk:gen-csv:customresourcedefinitions.specDescriptors.x-descriptors="urn:alm:descriptor:com.tectonic.ui:text"
	Name string `json:"name,omitempty"`
}

// RollingUpdateSpec defines configuration for the rolling update of all DaemonSet pods
type RollingUpdateSpec struct {
	// +kubebuilder:validation:Optional
	// +operator-sdk:gen-csv:customresourcedefinitions.specDescriptors=true
	// +operator-sdk:gen-csv:customresourcedefinitions.specDescriptors.displayName="Maximum number of nodes to simultaneously apply Daemonset pod updates on. Default 1"
	// +operator-sdk:gen-csv:customresourcedefinitions.specDescriptors.x-descriptors="urn:alm:descriptor:com.tectonic.ui:text"
	MaxUnavailable string `json:"maxUnavailable,omitempty"`
}

// GPUFeatureDiscoverySpec defines the properties for GPU Feature Discovery Plugin
type GPUFeatureDiscoverySpec struct {
	// Enabled indicates if deployment of GPU Feature Discovery Plugin is enabled.
	// +operator-sdk:gen-csv:customresourcedefinitions.specDescriptors=true
	// +operator-sdk:gen-csv:customresourcedefinitions.specDescriptors.displayName="Enable GPU Feature Discovery Plugin deployment through GPU Operator"
	// +operator-sdk:gen-csv:customresourcedefinitions.specDescriptors.x-descriptors="urn:alm:descriptor:com.tectonic.ui:booleanSwitch"
	Enabled *bool `json:"enabled,omitempty"`

	// GFD image repository
	// +kubebuilder:validation:Optional
	Repository string `json:"repository,omitempty"`

	// GFD image name
	// +kubebuilder:validation:Pattern=[a-zA-Z0-9\-]+
	Image string `json:"image,omitempty"`

	// GFD image tag
	// +kubebuilder:validation:Optional
	Version string `json:"version,omitempty"`

	// Image pull policy
	// +kubebuilder:validation:Optional
	// +operator-sdk:gen-csv:customresourcedefinitions.specDescriptors=true
	// +operator-sdk:gen-csv:customresourcedefinitions.specDescriptors.displayName="Image Pull Policy"
	// +operator-sdk:gen-csv:customresourcedefinitions.specDescriptors.x-descriptors="urn:alm:descriptor:com.tectonic.ui:imagePullPolicy"
	ImagePullPolicy string `json:"imagePullPolicy,omitempty"`

	// Image pull secrets
	// +kubebuilder:validation:Optional
	// +operator-sdk:gen-csv:customresourcedefinitions.specDescriptors=true
	// +operator-sdk:gen-csv:customresourcedefinitions.specDescriptors.displayName="Image pull secrets"
	// +operator-sdk:gen-csv:customresourcedefinitions.specDescriptors.x-descriptors="urn:alm:descriptor:io.kubernetes:Secret"
	ImagePullSecrets []string `json:"imagePullSecrets,omitempty"`

	// Optional: Define resources requests and limits for each pod
	// +operator-sdk:gen-csv:customresourcedefinitions.specDescriptors=true
	// +operator-sdk:gen-csv:customresourcedefinitions.specDescriptors.displayName="Resource Requirements"
	// +operator-sdk:gen-csv:customresourcedefinitions.specDescriptors.x-descriptors="urn:alm:descriptor:com.tectonic.ui:advanced,urn:alm:descriptor:com.tectonic.ui:resourceRequirements"
	Resources *ResourceRequirements `json:"resources,omitempty"`

	// Optional: List of arguments
	// +operator-sdk:gen-csv:customresourcedefinitions.specDescriptors=true
	// +operator-sdk:gen-csv:customresourcedefinitions.specDescriptors.displayName="Arguments"
	// +operator-sdk:gen-csv:customresourcedefinitions.specDescriptors.x-descriptors="urn:alm:descriptor:com.tectonic.ui:advanced,urn:alm:descriptor:com.tectonic.ui:text"
	Args []string `json:"args,omitempty"`

	// Optional: List of environment variables
	// +operator-sdk:gen-csv:customresourcedefinitions.specDescriptors=true
	// +operator-sdk:gen-csv:customresourcedefinitions.specDescriptors.displayName="Environment Variables"
	// +operator-sdk:gen-csv:customresourcedefinitions.specDescriptors.x-descriptors="urn:alm:descriptor:com.tectonic.ui:advanced,urn:alm:descriptor:com.tectonic.ui:text"
	Env []EnvVar `json:"env,omitempty"`
}

// MIGManagerSpec defines the properties for deploying NVIDIA MIG Manager
type MIGManagerSpec struct {
	// Enabled indicates if deployment of NVIDIA MIG Manager is enabled
	// +operator-sdk:gen-csv:customresourcedefinitions.specDescriptors=true
	// +operator-sdk:gen-csv:customresourcedefinitions.specDescriptors.displayName="Enable NVIDIA MIG Manager deployment through GPU Operator"
	// +operator-sdk:gen-csv:customresourcedefinitions.specDescriptors.x-descriptors="urn:alm:descriptor:com.tectonic.ui:booleanSwitch"
	Enabled *bool `json:"enabled,omitempty"`

	// NVIDIA MIG Manager image repository
	// +kubebuilder:validation:Optional
	Repository string `json:"repository,omitempty"`

	// NVIDIA MIG Manager image name
	// +kubebuilder:validation:Pattern=[a-zA-Z0-9\-]+
	Image string `json:"image,omitempty"`

	// NVIDIA MIG Manager image tag
	// +kubebuilder:validation:Optional
	Version string `json:"version,omitempty"`

	// Image pull policy
	// +kubebuilder:validation:Optional
	// +operator-sdk:gen-csv:customresourcedefinitions.specDescriptors=true
	// +operator-sdk:gen-csv:customresourcedefinitions.specDescriptors.displayName="Image Pull Policy"
	// +operator-sdk:gen-csv:customresourcedefinitions.specDescriptors.x-descriptors="urn:alm:descriptor:com.tectonic.ui:imagePullPolicy"
	ImagePullPolicy string `json:"imagePullPolicy,omitempty"`

	// Image pull secrets
	// +kubebuilder:validation:Optional
	// +operator-sdk:gen-csv:customresourcedefinitions.specDescriptors=true
	// +operator-sdk:gen-csv:customresourcedefinitions.specDescriptors.displayName="Image pull secrets"
	// +operator-sdk:gen-csv:customresourcedefinitions.specDescriptors.x-descriptors="urn:alm:descriptor:io.kubernetes:Secret"
	ImagePullSecrets []string `json:"imagePullSecrets,omitempty"`

	// Optional: Define resources requests and limits for each pod
	// +operator-sdk:gen-csv:customresourcedefinitions.specDescriptors=true
	// +operator-sdk:gen-csv:customresourcedefinitions.specDescriptors.displayName="Resource Requirements"
	// +operator-sdk:gen-csv:customresourcedefinitions.specDescriptors.x-descriptors="urn:alm:descriptor:com.tectonic.ui:advanced,urn:alm:descriptor:com.tectonic.ui:resourceRequirements"
	Resources *ResourceRequirements `json:"resources,omitempty"`

	// Optional: List of arguments
	// +operator-sdk:gen-csv:customresourcedefinitions.specDescriptors=true
	// +operator-sdk:gen-csv:customresourcedefinitions.specDescriptors.displayName="Arguments"
	// +operator-sdk:gen-csv:customresourcedefinitions.specDescriptors.x-descriptors="urn:alm:descriptor:com.tectonic.ui:advanced,urn:alm:descriptor:com.tectonic.ui:text"
	Args []string `json:"args,omitempty"`

	// Optional: List of environment variables
	// +operator-sdk:gen-csv:customresourcedefinitions.specDescriptors=true
	// +operator-sdk:gen-csv:customresourcedefinitions.specDescriptors.displayName="Environment Variables"
	// +operator-sdk:gen-csv:customresourcedefinitions.specDescriptors.x-descriptors="urn:alm:descriptor:com.tectonic.ui:advanced,urn:alm:descriptor:com.tectonic.ui:text"
	Env []EnvVar `json:"env,omitempty"`

	// Optional: Custom mig-parted configuration for NVIDIA MIG Manager container
	// +operator-sdk:gen-csv:customresourcedefinitions.specDescriptors=true
	// +operator-sdk:gen-csv:customresourcedefinitions.specDescriptors.displayName="Custom mig-parted configuration for NVIDIA MIG Manager container"
	Config *MIGPartedConfigSpec `json:"config,omitempty"`

	// Optional: Custom gpu-clients configuration for NVIDIA MIG Manager container
	// +operator-sdk:gen-csv:customresourcedefinitions.specDescriptors=true
	// +operator-sdk:gen-csv:customresourcedefinitions.specDescriptors.displayName="Custom gpu-clients configuration for NVIDIA MIG Manager container"
	GPUClientsConfig *MIGGPUClientsConfigSpec `json:"gpuClientsConfig,omitempty"`
}

// GPUDirectRDMASpec defines the properties for nvidia-peermem deployment
type GPUDirectRDMASpec struct {
	// Enabled indicates if GPUDirect RDMA is enabled through GPU operator
	// +operator-sdk:gen-csv:customresourcedefinitions.specDescriptors=true
	// +operator-sdk:gen-csv:customresourcedefinitions.specDescriptors.displayName="Enable GPUDirect RDMA through GPU operator"
	// +operator-sdk:gen-csv:customresourcedefinitions.specDescriptors.x-descriptors="urn:alm:descriptor:com.tectonic.ui:booleanSwitch"
	Enabled *bool `json:"enabled,omitempty"`
	// UseHostMOFED indicates to use MOFED drivers directly installed on the host to enable GPUDirect RDMA
	// +operator-sdk:gen-csv:customresourcedefinitions.specDescriptors=true
	// +operator-sdk:gen-csv:customresourcedefinitions.specDescriptors.displayName="Use MOFED drivers directly installed on the host to enable GPUDirect RDMA"
	// +operator-sdk:gen-csv:customresourcedefinitions.specDescriptors.x-descriptors="urn:alm:descriptor:com.tectonic.ui:booleanSwitch"
	UseHostMOFED *bool `json:"useHostMofed,omitempty"`
}

// GPUDirectStorageSpec defines the properties for NVIDIA GPUDirect Storage Driver deployment(Experimental)
type GPUDirectStorageSpec struct {
	// Enabled indicates if GPUDirect Storage is enabled through GPU operator
	// +operator-sdk:gen-csv:customresourcedefinitions.specDescriptors=true
	// +operator-sdk:gen-csv:customresourcedefinitions.specDescriptors.displayName="Enable GPUDirect Storage through GPU operator"
	// +operator-sdk:gen-csv:customresourcedefinitions.specDescriptors.x-descriptors="urn:alm:descriptor:com.tectonic.ui:booleanSwitch"
	Enabled *bool `json:"enabled,omitempty"`

	// NVIDIA GPUDirect Storage Driver image repository
	// +kubebuilder:validation:Optional
	Repository string `json:"repository,omitempty"`

	// NVIDIA GPUDirect Storage Driver image name
	// +kubebuilder:validation:Pattern=[a-zA-Z0-9\-]+
	Image string `json:"image,omitempty"`

	// NVIDIA GPUDirect Storage Driver image tag
	// +kubebuilder:validation:Optional
	Version string `json:"version,omitempty"`

	// Image pull policy
	// +kubebuilder:validation:Optional
	// +operator-sdk:gen-csv:customresourcedefinitions.specDescriptors=true
	// +operator-sdk:gen-csv:customresourcedefinitions.specDescriptors.displayName="Image Pull Policy"
	// +operator-sdk:gen-csv:customresourcedefinitions.specDescriptors.x-descriptors="urn:alm:descriptor:com.tectonic.ui:imagePullPolicy"
	ImagePullPolicy string `json:"imagePullPolicy,omitempty"`

	// Image pull secrets
	// +kubebuilder:validation:Optional
	// +operator-sdk:gen-csv:customresourcedefinitions.specDescriptors=true
	// +operator-sdk:gen-csv:customresourcedefinitions.specDescriptors.displayName="Image pull secrets"
	// +operator-sdk:gen-csv:customresourcedefinitions.specDescriptors.x-descriptors="urn:alm:descriptor:io.kubernetes:Secret"
	ImagePullSecrets []string `json:"imagePullSecrets,omitempty"`

	// Optional: List of arguments
	// +operator-sdk:gen-csv:customresourcedefinitions.specDescriptors=true
	// +operator-sdk:gen-csv:customresourcedefinitions.specDescriptors.displayName="Arguments"
	// +operator-sdk:gen-csv:customresourcedefinitions.specDescriptors.x-descriptors="urn:alm:descriptor:com.tectonic.ui:advanced,urn:alm:descriptor:com.tectonic.ui:text"
	Args []string `json:"args,omitempty"`

	// Optional: List of environment variables
	// +operator-sdk:gen-csv:customresourcedefinitions.specDescriptors=true
	// +operator-sdk:gen-csv:customresourcedefinitions.specDescriptors.displayName="Environment Variables"
	// +operator-sdk:gen-csv:customresourcedefinitions.specDescriptors.x-descriptors="urn:alm:descriptor:com.tectonic.ui:advanced,urn:alm:descriptor:com.tectonic.ui:text"
	Env []EnvVar `json:"env,omitempty"`
}

// GDRCopySpec defines the properties for NVIDIA GDRCopy driver (gdrdrv) deployment
type GDRCopySpec struct {
	// Enabled indicates if GDRCopy is enabled through GPU Operator
	// +operator-sdk:gen-csv:customresourcedefinitions.specDescriptors=true
	// +operator-sdk:gen-csv:customresourcedefinitions.specDescriptors.displayName="Enable GDRCopy through GPU operator"
	// +operator-sdk:gen-csv:customresourcedefinitions.specDescriptors.x-descriptors="urn:alm:descriptor:com.tectonic.ui:booleanSwitch"
	Enabled *bool `json:"enabled,omitempty"`

	// NVIDIA GDRCopy driver image repository
	// +kubebuilder:validation:Optional
	Repository string `json:"repository,omitempty"`

	// NVIDIA GDRCopy driver image name
	// +kubebuilder:validation:Pattern=[a-zA-Z0-9\-]+
	Image string `json:"image,omitempty"`

	// NVIDIA GDRCopy driver image tag
	// +kubebuilder:validation:Optional
	Version string `json:"version,omitempty"`

	// Image pull policy
	// +kubebuilder:validation:Optional
	// +operator-sdk:gen-csv:customresourcedefinitions.specDescriptors=true
	// +operator-sdk:gen-csv:customresourcedefinitions.specDescriptors.displayName="Image Pull Policy"
	// +operator-sdk:gen-csv:customresourcedefinitions.specDescriptors.x-descriptors="urn:alm:descriptor:com.tectonic.ui:imagePullPolicy"
	ImagePullPolicy string `json:"imagePullPolicy,omitempty"`

	// Image pull secrets
	// +kubebuilder:validation:Optional
	// +operator-sdk:gen-csv:customresourcedefinitions.specDescriptors=true
	// +operator-sdk:gen-csv:customresourcedefinitions.specDescriptors.displayName="Image pull secrets"
	// +operator-sdk:gen-csv:customresourcedefinitions.specDescriptors.x-descriptors="urn:alm:descriptor:io.kubernetes:Secret"
	ImagePullSecrets []string `json:"imagePullSecrets,omitempty"`

	// Optional: List of arguments
	// +operator-sdk:gen-csv:customresourcedefinitions.specDescriptors=true
	// +operator-sdk:gen-csv:customresourcedefinitions.specDescriptors.displayName="Arguments"
	// +operator-sdk:gen-csv:customresourcedefinitions.specDescriptors.x-descriptors="urn:alm:descriptor:com.tectonic.ui:advanced,urn:alm:descriptor:com.tectonic.ui:text"
	Args []string `json:"args,omitempty"`

	// Optional: List of environment variables
	// +operator-sdk:gen-csv:customresourcedefinitions.specDescriptors=true
	// +operator-sdk:gen-csv:customresourcedefinitions.specDescriptors.displayName="Environment Variables"
	// +operator-sdk:gen-csv:customresourcedefinitions.specDescriptors.x-descriptors="urn:alm:descriptor:com.tectonic.ui:advanced,urn:alm:descriptor:com.tectonic.ui:text"
	Env []EnvVar `json:"env,omitempty"`
}

// MIGPartedConfigSpec defines custom mig-parted config for NVIDIA MIG Manager container
type MIGPartedConfigSpec struct {
	// ConfigMap name
	// +kubebuilder:validation:Optional
	// +kubebuilder:default=default-mig-parted-config
	// +operator-sdk:gen-csv:customresourcedefinitions.specDescriptors=true
	// +operator-sdk:gen-csv:customresourcedefinitions.specDescriptors.displayName="ConfigMap Name"
	// +operator-sdk:gen-csv:customresourcedefinitions.specDescriptors.x-descriptors="urn:alm:descriptor:com.tectonic.ui:text"
	Name string `json:"name,omitempty"`
	// Default MIG config to be applied on the node, when there is no config specified with the node label nvidia.com/mig.config
	// +kubebuilder:validation:Optional
	// +kubebuilder:default=all-disabled
	// +kubebuilder:validation:Enum=all-disabled;""
	// +operator-sdk:gen-csv:customresourcedefinitions.specDescriptors=true
	// +operator-sdk:gen-csv:customresourcedefinitions.specDescriptors.displayName="Default MIG config"
	// +operator-sdk:gen-csv:customresourcedefinitions.specDescriptors.x-descriptors="urn:alm:descriptor:com.tectonic.ui:text"
	Default string `json:"default,omitempty"`
}

// MIGGPUClientsConfigSpec defines custom gpu-clients config for NVIDIA MIG Manager container
type MIGGPUClientsConfigSpec struct {
	// ConfigMap name
	// +kubebuilder:validation:Optional
	// +operator-sdk:gen-csv:customresourcedefinitions.specDescriptors=true
	// +operator-sdk:gen-csv:customresourcedefinitions.specDescriptors.displayName="ConfigMap Name"
	// +operator-sdk:gen-csv:customresourcedefinitions.specDescriptors.x-descriptors="urn:alm:descriptor:com.tectonic.ui:text"
	Name string `json:"name,omitempty"`
}

// KataManagerSpec defines the configuration for the kata-manager which prepares NVIDIA-specific kata runtimes
type KataManagerSpec struct {
	// Enabled indicates if deployment of Kata Manager is enabled
	// +operator-sdk:gen-csv:customresourcedefinitions.specDescriptors=true
	// +operator-sdk:gen-csv:customresourcedefinitions.specDescriptors.displayName="Enable Kata Manager deployment through GPU Operator"
	// +operator-sdk:gen-csv:customresourcedefinitions.specDescriptors.x-descriptors="urn:alm:descriptor:com.tectonic.ui:booleanSwitch"
	Enabled *bool `json:"enabled,omitempty"`

	// Kata Manager config
	// +operator-sdk:gen-csv:customresourcedefinitions.specDescriptors=true
	// +operator-sdk:gen-csv:customresourcedefinitions.specDescriptors.displayName="Kata Manager configuration"
	Config *kata_v1alpha1.Config `json:"config,omitempty"`

	// Kata Manager image repository
	// +kubebuilder:validation:Optional
	Repository string `json:"repository,omitempty"`

	// Kata Manager image name
	// +kubebuilder:validation:Pattern=[a-zA-Z0-9\-]+
	Image string `json:"image,omitempty"`

	// Kata Manager image tag
	// +kubebuilder:validation:Optional
	Version string `json:"version,omitempty"`

	// Image pull policy
	// +kubebuilder:validation:Optional
	// +operator-sdk:gen-csv:customresourcedefinitions.specDescriptors=true
	// +operator-sdk:gen-csv:customresourcedefinitions.specDescriptors.displayName="Image Pull Policy"
	// +operator-sdk:gen-csv:customresourcedefinitions.specDescriptors.x-descriptors="urn:alm:descriptor:com.tectonic.ui:imagePullPolicy"
	ImagePullPolicy string `json:"imagePullPolicy,omitempty"`

	// Image pull secrets
	// +kubebuilder:validation:Optional
	// +operator-sdk:gen-csv:customresourcedefinitions.specDescriptors=true
	// +operator-sdk:gen-csv:customresourcedefinitions.specDescriptors.displayName="Image pull secrets"
	// +operator-sdk:gen-csv:customresourcedefinitions.specDescriptors.x-descriptors="urn:alm:descriptor:io.kubernetes:Secret"
	ImagePullSecrets []string `json:"imagePullSecrets,omitempty"`

	// Optional: Define resources requests and limits for each pod
	// +operator-sdk:gen-csv:customresourcedefinitions.specDescriptors=true
	// +operator-sdk:gen-csv:customresourcedefinitions.specDescriptors.displayName="Resource Requirements"
	// +operator-sdk:gen-csv:customresourcedefinitions.specDescriptors.x-descriptors="urn:alm:descriptor:com.tectonic.ui:advanced,urn:alm:descriptor:com.tectonic.ui:resourceRequirements"
	Resources *ResourceRequirements `json:"resources,omitempty"`

	// Optional: List of arguments
	// +operator-sdk:gen-csv:customresourcedefinitions.specDescriptors=true
	// +operator-sdk:gen-csv:customresourcedefinitions.specDescriptors.displayName="Arguments"
	// +operator-sdk:gen-csv:customresourcedefinitions.specDescriptors.x-descriptors="urn:alm:descriptor:com.tectonic.ui:advanced,urn:alm:descriptor:com.tectonic.ui:text"
	Args []string `json:"args,omitempty"`

	// Optional: List of environment variables
	// +operator-sdk:gen-csv:customresourcedefinitions.specDescriptors=true
	// +operator-sdk:gen-csv:customresourcedefinitions.specDescriptors.displayName="Environment Variables"
	// +operator-sdk:gen-csv:customresourcedefinitions.specDescriptors.x-descriptors="urn:alm:descriptor:com.tectonic.ui:advanced,urn:alm:descriptor:com.tectonic.ui:text"
	Env []EnvVar `json:"env,omitempty"`
}

// CCManagerSpec defines the properties for deploying Confidential Containers (CC) manager
type CCManagerSpec struct {
	// Enabled indicates if deployment of CC Manager is enabled
	// +operator-sdk:gen-csv:customresourcedefinitions.specDescriptors=true
	// +operator-sdk:gen-csv:customresourcedefinitions.specDescriptors.displayName="Enable CC Manager deployment through GPU Operator"
	// +operator-sdk:gen-csv:customresourcedefinitions.specDescriptors.x-descriptors="urn:alm:descriptor:com.tectonic.ui:booleanSwitch"
	Enabled *bool `json:"enabled,omitempty"`

	// Default CC mode setting for compatible GPUs on the node
	// +operator-sdk:gen-csv:customresourcedefinitions.specDescriptors=true
	// +operator-sdk:gen-csv:customresourcedefinitions.specDescriptors.displayName="Default CC mode setting for all CC-capable GPUs"
	// +operator-sdk:gen-csv:customresourcedefinitions.specDescriptors.x-descriptors="urn:alm:descriptor:com.tectonic.ui:text"
	// +kubebuilder:validation:Enum=on;off;devtools
	DefaultMode string `json:"defaultMode,omitempty"`

	// CC Manager image repository
	// +kubebuilder:validation:Optional
	Repository string `json:"repository,omitempty"`

	// CC Manager image name
	// +kubebuilder:validation:Pattern=[a-zA-Z0-9\-]+
	Image string `json:"image,omitempty"`

	// CC Manager image tag
	// +kubebuilder:validation:Optional
	Version string `json:"version,omitempty"`

	// Image pull policy
	// +kubebuilder:validation:Optional
	// +operator-sdk:gen-csv:customresourcedefinitions.specDescriptors=true
	// +operator-sdk:gen-csv:customresourcedefinitions.specDescriptors.displayName="Image Pull Policy"
	// +operator-sdk:gen-csv:customresourcedefinitions.specDescriptors.x-descriptors="urn:alm:descriptor:com.tectonic.ui:imagePullPolicy"
	ImagePullPolicy string `json:"imagePullPolicy,omitempty"`

	// Image pull secrets
	// +kubebuilder:validation:Optional
	// +operator-sdk:gen-csv:customresourcedefinitions.specDescriptors=true
	// +operator-sdk:gen-csv:customresourcedefinitions.specDescriptors.displayName="Image pull secrets"
	// +operator-sdk:gen-csv:customresourcedefinitions.specDescriptors.x-descriptors="urn:alm:descriptor:io.kubernetes:Secret"
	ImagePullSecrets []string `json:"imagePullSecrets,omitempty"`

	// Optional: Define resources requests and limits for each pod
	// +operator-sdk:gen-csv:customresourcedefinitions.specDescriptors=true
	// +operator-sdk:gen-csv:customresourcedefinitions.specDescriptors.displayName="Resource Requirements"
	// +operator-sdk:gen-csv:customresourcedefinitions.specDescriptors.x-descriptors="urn:alm:descriptor:com.tectonic.ui:advanced,urn:alm:descriptor:com.tectonic.ui:resourceRequirements"
	Resources *ResourceRequirements `json:"resources,omitempty"`

	// Optional: List of arguments
	// +operator-sdk:gen-csv:customresourcedefinitions.specDescriptors=true
	// +operator-sdk:gen-csv:customresourcedefinitions.specDescriptors.displayName="Arguments"
	// +operator-sdk:gen-csv:customresourcedefinitions.specDescriptors.x-descriptors="urn:alm:descriptor:com.tectonic.ui:advanced,urn:alm:descriptor:com.tectonic.ui:text"
	Args []string `json:"args,omitempty"`

	// Optional: List of environment variables
	// +operator-sdk:gen-csv:customresourcedefinitions.specDescriptors=true
	// +operator-sdk:gen-csv:customresourcedefinitions.specDescriptors.displayName="Environment Variables"
	// +operator-sdk:gen-csv:customresourcedefinitions.specDescriptors.x-descriptors="urn:alm:descriptor:com.tectonic.ui:advanced,urn:alm:descriptor:com.tectonic.ui:text"
	Env []EnvVar `json:"env,omitempty"`
}

// VFIOManagerSpec defines the properties for deploying VFIO-PCI manager
type VFIOManagerSpec struct {
	// Enabled indicates if deployment of VFIO Manager is enabled
	// +operator-sdk:gen-csv:customresourcedefinitions.specDescriptors=true
	// +operator-sdk:gen-csv:customresourcedefinitions.specDescriptors.displayName="Enable VFIO Manager deployment through GPU Operator"
	// +operator-sdk:gen-csv:customresourcedefinitions.specDescriptors.x-descriptors="urn:alm:descriptor:com.tectonic.ui:booleanSwitch"
	Enabled *bool `json:"enabled,omitempty"`

	// VFIO Manager image repository
	// +kubebuilder:validation:Optional
	Repository string `json:"repository,omitempty"`

	// VFIO Manager image name
	// +kubebuilder:validation:Pattern=[a-zA-Z0-9\-]+
	Image string `json:"image,omitempty"`

	// VFIO Manager image tag
	// +kubebuilder:validation:Optional
	Version string `json:"version,omitempty"`

	// Image pull policy
	// +kubebuilder:validation:Optional
	// +operator-sdk:gen-csv:customresourcedefinitions.specDescriptors=true
	// +operator-sdk:gen-csv:customresourcedefinitions.specDescriptors.displayName="Image Pull Policy"
	// +operator-sdk:gen-csv:customresourcedefinitions.specDescriptors.x-descriptors="urn:alm:descriptor:com.tectonic.ui:imagePullPolicy"
	ImagePullPolicy string `json:"imagePullPolicy,omitempty"`

	// Image pull secrets
	// +kubebuilder:validation:Optional
	// +operator-sdk:gen-csv:customresourcedefinitions.specDescriptors=true
	// +operator-sdk:gen-csv:customresourcedefinitions.specDescriptors.displayName="Image pull secrets"
	// +operator-sdk:gen-csv:customresourcedefinitions.specDescriptors.x-descriptors="urn:alm:descriptor:io.kubernetes:Secret"
	ImagePullSecrets []string `json:"imagePullSecrets,omitempty"`

	// Optional: Define resources requests and limits for each pod
	// +operator-sdk:gen-csv:customresourcedefinitions.specDescriptors=true
	// +operator-sdk:gen-csv:customresourcedefinitions.specDescriptors.displayName="Resource Requirements"
	// +operator-sdk:gen-csv:customresourcedefinitions.specDescriptors.x-descriptors="urn:alm:descriptor:com.tectonic.ui:advanced,urn:alm:descriptor:com.tectonic.ui:resourceRequirements"
	Resources *ResourceRequirements `json:"resources,omitempty"`

	// Optional: List of arguments
	// +operator-sdk:gen-csv:customresourcedefinitions.specDescriptors=true
	// +operator-sdk:gen-csv:customresourcedefinitions.specDescriptors.displayName="Arguments"
	// +operator-sdk:gen-csv:customresourcedefinitions.specDescriptors.x-descriptors="urn:alm:descriptor:com.tectonic.ui:advanced,urn:alm:descriptor:com.tectonic.ui:text"
	Args []string `json:"args,omitempty"`

	// Optional: List of environment variables
	// +operator-sdk:gen-csv:customresourcedefinitions.specDescriptors=true
	// +operator-sdk:gen-csv:customresourcedefinitions.specDescriptors.displayName="Environment Variables"
	// +operator-sdk:gen-csv:customresourcedefinitions.specDescriptors.x-descriptors="urn:alm:descriptor:com.tectonic.ui:advanced,urn:alm:descriptor:com.tectonic.ui:text"
	Env []EnvVar `json:"env,omitempty"`

	// DriverManager represents configuration for NVIDIA Driver Manager
	DriverManager DriverManagerSpec `json:"driverManager,omitempty"`
}

// VGPUDeviceManagerSpec defines the properties for deploying NVIDIA vGPU Device Manager
type VGPUDeviceManagerSpec struct {
	// Enabled indicates if deployment of NVIDIA vGPU Device Manager is enabled
	// +operator-sdk:gen-csv:customresourcedefinitions.specDescriptors=true
	// +operator-sdk:gen-csv:customresourcedefinitions.specDescriptors.displayName="Enable NVIDIA vGPU Device Manager deployment through GPU Operator"
	// +operator-sdk:gen-csv:customresourcedefinitions.specDescriptors.x-descriptors="urn:alm:descriptor:com.tectonic.ui:booleanSwitch"
	Enabled *bool `json:"enabled,omitempty"`

	// NVIDIA vGPU Device Manager image repository
	// +kubebuilder:validation:Optional
	Repository string `json:"repository,omitempty"`

	// NVIDIA vGPU Device Manager image name
	// +kubebuilder:validation:Pattern=[a-zA-Z0-9\-]+
	Image string `json:"image,omitempty"`

	// NVIDIA vGPU Device Manager image tag
	// +kubebuilder:validation:Optional
	Version string `json:"version,omitempty"`

	// Image pull policy
	// +kubebuilder:validation:Optional
	// +operator-sdk:gen-csv:customresourcedefinitions.specDescriptors=true
	// +operator-sdk:gen-csv:customresourcedefinitions.specDescriptors.displayName="Image Pull Policy"
	// +operator-sdk:gen-csv:customresourcedefinitions.specDescriptors.x-descriptors="urn:alm:descriptor:com.tectonic.ui:imagePullPolicy"
	ImagePullPolicy string `json:"imagePullPolicy,omitempty"`

	// Image pull secrets
	// +kubebuilder:validation:Optional
	// +operator-sdk:gen-csv:customresourcedefinitions.specDescriptors=true
	// +operator-sdk:gen-csv:customresourcedefinitions.specDescriptors.displayName="Image pull secrets"
	// +operator-sdk:gen-csv:customresourcedefinitions.specDescriptors.x-descriptors="urn:alm:descriptor:io.kubernetes:Secret"
	ImagePullSecrets []string `json:"imagePullSecrets,omitempty"`

	// Optional: Define resources requests and limits for each pod
	// +operator-sdk:gen-csv:customresourcedefinitions.specDescriptors=true
	// +operator-sdk:gen-csv:customresourcedefinitions.specDescriptors.displayName="Resource Requirements"
	// +operator-sdk:gen-csv:customresourcedefinitions.specDescriptors.x-descriptors="urn:alm:descriptor:com.tectonic.ui:advanced,urn:alm:descriptor:com.tectonic.ui:resourceRequirements"
	Resources *ResourceRequirements `json:"resources,omitempty"`

	// Optional: List of arguments
	// +operator-sdk:gen-csv:customresourcedefinitions.specDescriptors=true
	// +operator-sdk:gen-csv:customresourcedefinitions.specDescriptors.displayName="Arguments"
	// +operator-sdk:gen-csv:customresourcedefinitions.specDescriptors.x-descriptors="urn:alm:descriptor:com.tectonic.ui:advanced,urn:alm:descriptor:com.tectonic.ui:text"
	Args []string `json:"args,omitempty"`

	// Optional: List of environment variables
	// +operator-sdk:gen-csv:customresourcedefinitions.specDescriptors=true
	// +operator-sdk:gen-csv:customresourcedefinitions.specDescriptors.displayName="Environment Variables"
	// +operator-sdk:gen-csv:customresourcedefinitions.specDescriptors.x-descriptors="urn:alm:descriptor:com.tectonic.ui:advanced,urn:alm:descriptor:com.tectonic.ui:text"
	Env []EnvVar `json:"env,omitempty"`

	// NVIDIA vGPU devices configuration for NVIDIA vGPU Device Manager container
	// +operator-sdk:gen-csv:customresourcedefinitions.specDescriptors=true
	// +operator-sdk:gen-csv:customresourcedefinitions.specDescriptors.displayName="NVIDIA vGPU devices configuration for NVIDIA vGPU Device Manager container"
	Config *VGPUDevicesConfigSpec `json:"config,omitempty"`
}

// VGPUDevicesConfigSpec defines vGPU devices configuration for NVIDIA vGPU Device Manager container
type VGPUDevicesConfigSpec struct {
	// ConfigMap name
	// +kubebuilder:validation:Optional
	// +operator-sdk:gen-csv:customresourcedefinitions.specDescriptors=true
	// +operator-sdk:gen-csv:customresourcedefinitions.specDescriptors.displayName="ConfigMap Name"
	// +operator-sdk:gen-csv:customresourcedefinitions.specDescriptors.x-descriptors="urn:alm:descriptor:com.tectonic.ui:text"
	Name string `json:"name,omitempty"`
	// Default config name within the ConfigMap
	// +kubebuilder:validation:Optional
	// +kubebuilder:default=default
	// +operator-sdk:gen-csv:customresourcedefinitions.specDescriptors=true
	// +operator-sdk:gen-csv:customresourcedefinitions.specDescriptors.displayName="Default config name within the ConfigMap for the NVIDIA vGPU devices config"
	// +operator-sdk:gen-csv:customresourcedefinitions.specDescriptors.x-descriptors="urn:alm:descriptor:com.tectonic.ui:text"
	Default string `json:"default,omitempty"`
}

// CDIConfigSpec defines how the Container Device Interface is used in the cluster.
type CDIConfigSpec struct {
	// Enabled indicates whether CDI can be used to make GPUs accessible to containers.
	// +kubebuilder:validation:Optional
	// +kubebuilder:default=false
	// +operator-sdk:gen-csv:customresourcedefinitions.specDescriptors=true
	// +operator-sdk:gen-csv:customresourcedefinitions.specDescriptors.displayName="Enable CDI as a mechanism for making GPUs accessible to containers"
	// +operator-sdk:gen-csv:customresourcedefinitions.specDescriptors.x-descriptors="urn:alm:descriptor:com.tectonic.ui:booleanSwitch"
	Enabled *bool `json:"enabled,omitempty"`

	// Default indicates whether to use CDI as the default mechanism for providing GPU access to containers.
	// +kubebuilder:validation:Optional
	// +kubebuilder:default=false
	// +operator-sdk:gen-csv:customresourcedefinitions.specDescriptors=true
	// +operator-sdk:gen-csv:customresourcedefinitions.specDescriptors.displayName="Configure CDI as the default mechanism for making GPUs accessible to containers"
	// +operator-sdk:gen-csv:customresourcedefinitions.specDescriptors.x-descriptors="urn:alm:descriptor:com.tectonic.ui:booleanSwitch"
	Default *bool `json:"default,omitempty"`
}

// MIGStrategy indicates MIG mode
type MIGStrategy string

// Constants representing different MIG strategies.
const (
	// MIGStrategyNone indicates MIG mode disabled.
	MIGStrategyNone MIGStrategy = "none"
	// MIGStrategySingle indicates Single MIG mode
	MIGStrategySingle MIGStrategy = "single"
	// MIGStrategyMixed indicates Mixed MIG mode
	MIGStrategyMixed MIGStrategy = "mixed"
)

// State indicates state of GPU operator components
type State string

const (
	// Ignored indicates duplicate ClusterPolicy instances and rest are ignored.
	Ignored State = "ignored"
	// Ready indicates all components of ClusterPolicy are ready
	Ready State = "ready"
	// NotReady indicates some/all components of ClusterPolicy are not ready
	NotReady State = "notReady"
	// Disabled indicates if the state is disabled
	Disabled State = "disabled"
)

// ClusterPolicyStatus defines the observed state of ClusterPolicy
type ClusterPolicyStatus struct {
	// +kubebuilder:validation:Enum=ignored;ready;notReady
	// State indicates status of ClusterPolicy
	State State `json:"state"`
	// Namespace indicates a namespace in which the operator is installed
	Namespace string `json:"namespace,omitempty"`
	// Conditions is a list of conditions representing the ClusterPolicy's current state.
	Conditions []metav1.Condition `json:"conditions,omitempty"`
}

// +genclient
// +genclient:nonNamespaced
// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:resource:scope=Cluster
// +kubebuilder:printcolumn:name="Status",type=string,JSONPath=`.status.state`,priority=0
// +kubebuilder:printcolumn:name="Age",type=string,JSONPath=`.metadata.creationTimestamp`,priority=0

// ClusterPolicy is the Schema for the clusterpolicies API
type ClusterPolicy struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   ClusterPolicySpec   `json:"spec,omitempty"`
	Status ClusterPolicyStatus `json:"status,omitempty"`
}

// +kubebuilder:object:root=true

// ClusterPolicyList contains a list of ClusterPolicy
type ClusterPolicyList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []ClusterPolicy `json:"items"`
}

func init() {
	SchemeBuilder.Register(&ClusterPolicy{}, &ClusterPolicyList{})
}

// SetStatus sets state and namespace of ClusterPolicy instance
func (p *ClusterPolicy) SetStatus(s State, ns string) {
	p.Status.State = s
	p.Status.Namespace = ns
}

func imagePath(repository string, image string, version string, imagePathEnvName string) (string, error) {
	// ImagePath is obtained using following priority
	// 1. ClusterPolicy (i.e through repository/image/path variables in CRD)
	var crdImagePath string
	if repository == "" && version == "" {
		if image != "" {
			// this is useful for tools like kbld(carvel) which transform templates into image as path@digest
			crdImagePath = image
		}
	} else {
		// use @ if image digest is specified instead of tag
		if strings.HasPrefix(version, "sha256:") {
			crdImagePath = repository + "/" + image + "@" + version
		} else {
			crdImagePath = repository + "/" + image + ":" + version
		}
	}
	if crdImagePath != "" {
		return crdImagePath, nil
	}

	// 2. Env passed to GPU Operator Pod (eg OLM)
	envImagePath := os.Getenv(imagePathEnvName)
	if envImagePath != "" {
		return envImagePath, nil
	}

	// 3. If both are not set, error out
	return "", fmt.Errorf("empty image path provided through both ClusterPolicy CR and ENV %s", imagePathEnvName)
}

// ImagePath sets image path for given component type
func ImagePath(spec interface{}) (string, error) {
	switch v := spec.(type) {
	case *DriverSpec:
		config := spec.(*DriverSpec)
		return imagePath(config.Repository, config.Image, config.Version, "DRIVER_IMAGE")
	case *VGPUManagerSpec:
		config := spec.(*VGPUManagerSpec)
		return imagePath(config.Repository, config.Image, config.Version, "VGPU_MANAGER_IMAGE")
	case *ToolkitSpec:
		config := spec.(*ToolkitSpec)
		return imagePath(config.Repository, config.Image, config.Version, "CONTAINER_TOOLKIT_IMAGE")
	case *DevicePluginSpec:
		config := spec.(*DevicePluginSpec)
		return imagePath(config.Repository, config.Image, config.Version, "DEVICE_PLUGIN_IMAGE")
	case *SandboxDevicePluginSpec:
		config := spec.(*SandboxDevicePluginSpec)
		return imagePath(config.Repository, config.Image, config.Version, "SANDBOX_DEVICE_PLUGIN_IMAGE")
	case *DCGMExporterSpec:
		config := spec.(*DCGMExporterSpec)
		return imagePath(config.Repository, config.Image, config.Version, "DCGM_EXPORTER_IMAGE")
	case *DCGMSpec:
		config := spec.(*DCGMSpec)
		return imagePath(config.Repository, config.Image, config.Version, "DCGM_IMAGE")
	case *NodeStatusExporterSpec:
		config := spec.(*NodeStatusExporterSpec)
		return imagePath(config.Repository, config.Image, config.Version, "VALIDATOR_IMAGE")
	case *GPUFeatureDiscoverySpec:
		config := spec.(*GPUFeatureDiscoverySpec)
		return imagePath(config.Repository, config.Image, config.Version, "GFD_IMAGE")
	case *ValidatorSpec:
		config := spec.(*ValidatorSpec)
		return imagePath(config.Repository, config.Image, config.Version, "VALIDATOR_IMAGE")
	case *InitContainerSpec:
		config := spec.(*InitContainerSpec)
		return imagePath(config.Repository, config.Image, config.Version, "CUDA_BASE_IMAGE")
	case *MIGManagerSpec:
		config := spec.(*MIGManagerSpec)
		return imagePath(config.Repository, config.Image, config.Version, "MIG_MANAGER_IMAGE")
	case *DriverManagerSpec:
		config := spec.(*DriverManagerSpec)
		return imagePath(config.Repository, config.Image, config.Version, "DRIVER_MANAGER_IMAGE")
	case *GPUDirectStorageSpec:
		config := spec.(*GPUDirectStorageSpec)
		return imagePath(config.Repository, config.Image, config.Version, "GDS_IMAGE")
	case *GDRCopySpec:
		config := spec.(*GDRCopySpec)
		return imagePath(config.Repository, config.Image, config.Version, "GDRCOPY_IMAGE")
	case *VFIOManagerSpec:
		config := spec.(*VFIOManagerSpec)
		return imagePath(config.Repository, config.Image, config.Version, "VFIO_MANAGER_IMAGE")
	case *VGPUDeviceManagerSpec:
		config := spec.(*VGPUDeviceManagerSpec)
		return imagePath(config.Repository, config.Image, config.Version, "VGPU_DEVICE_MANAGER_IMAGE")
	case *KataManagerSpec:
		config := spec.(*KataManagerSpec)
		return imagePath(config.Repository, config.Image, config.Version, "KATA_MANAGER_IMAGE")
	case *CCManagerSpec:
		config := spec.(*CCManagerSpec)
		return imagePath(config.Repository, config.Image, config.Version, "CC_MANAGER_IMAGE")
	default:
		return "", fmt.Errorf("invalid type to construct image path: %v", v)
	}
}

// ImagePullPolicy sets image pull policy
func ImagePullPolicy(pullPolicy string) corev1.PullPolicy {
	var imagePullPolicy corev1.PullPolicy
	switch pullPolicy {
	case "Always":
		imagePullPolicy = corev1.PullAlways
	case "Never":
		imagePullPolicy = corev1.PullNever
	case "IfNotPresent":
		imagePullPolicy = corev1.PullIfNotPresent
	default:
		imagePullPolicy = corev1.PullIfNotPresent
	}
	return imagePullPolicy
}

// IsEnabled returns true if driver install is enabled(default) through gpu-operator
func (d *DriverSpec) IsEnabled() bool {
	if d.Enabled == nil {
		// default is true if not specified by user
		return true
	}
	return *d.Enabled
}

// UseNvdiaDriverCRDType returns true if the driver installation is managed by NVIDIADriver CRD type
func (d *DriverSpec) UseNvdiaDriverCRDType() bool {
	if d.UseNvidiaDriverCRD == nil {
		// default is false if not specified by user
		return false
	}
	return *d.UseNvidiaDriverCRD
}

// UsePrecompiledDrivers returns true if driver install is enabled using pre-compiled modules
func (d *DriverSpec) UsePrecompiledDrivers() bool {
	if d.UsePrecompiled == nil {
		// default is false if not specified by user
		return false
	}
	return *d.UsePrecompiled
}

// OpenKernelModulesEnabled returns true if driver install is enabled using open GPU kernel modules
func (d *DriverSpec) OpenKernelModulesEnabled() bool {
	return d.KernelModuleType == "open"
}

// IsEnabled returns true if device-plugin is enabled(default) through gpu-operator
func (p *DevicePluginSpec) IsEnabled() bool {
	if p.Enabled == nil {
		// default is true if not specified by user
		return true
	}
	return *p.Enabled
}

// IsEnabled returns true if dcgm-exporter is enabled(default) through gpu-operator
func (e *DCGMExporterSpec) IsEnabled() bool {
	if e.Enabled == nil {
		// default is true if not specified by user
		return true
	}
	return *e.Enabled
}

// IsEnabled returns true if gpu-feature-discovery is enabled(default) through gpu-operator
func (g *GPUFeatureDiscoverySpec) IsEnabled() bool {
	if g.Enabled == nil {
		// default is true if not specified by user
		return true
	}
	return *g.Enabled
}

// IsEnabled returns true if VFIO-PCI Manager install is enabled through gpu-operator
func (v *VFIOManagerSpec) IsEnabled() bool {
	if v.Enabled == nil {
		// default is false if not specified by user
		return false
	}
	return *v.Enabled
}

// IsEnabled returns true if vGPU Manager install is enabled through gpu-operator
func (d *VGPUManagerSpec) IsEnabled() bool {
	if d.Enabled == nil {
		// default is false if not specified by user
		return false
	}
	return *d.Enabled
}

// IsEnabled returns true if vGPU Device Manager is enabled through gpu-operator
func (v *VGPUDeviceManagerSpec) IsEnabled() bool {
	if v.Enabled == nil {
		// default is false if not specified by user
		return false
	}
	return *v.Enabled
}

// IsEnabled returns true if container-toolkit install is enabled(default) through gpu-operator
func (t *ToolkitSpec) IsEnabled() bool {
	if t.Enabled == nil {
		// default is true if not specified by user
		return true
	}
	return *t.Enabled
}

// IsEnabled returns true if the cluster intends to run GPU accelerated
// workloads in sandboxed environments (VMs).
func (s *SandboxWorkloadsSpec) IsEnabled() bool {
	if s.Enabled == nil {
		// Sandbox workloads are disabled by default
		return false
	}
	return *s.Enabled
}

// IsEnabled returns true if the sandbox device plugin is enabled through gpu-operator
func (s *SandboxDevicePluginSpec) IsEnabled() bool {
	if s.Enabled == nil {
		// default is false if not specified by user
		return false
	}
	return *s.Enabled
}

// IsEnabled returns true if PodSecurityAdmission configuration is enabled for all gpu-operator pods
func (p *PSASpec) IsEnabled() bool {
	if p.Enabled == nil {
		// PSA is disabled by default
		return false
	}
	return *p.Enabled
}

// IsEnabled returns true if mig-manager is enabled(default) through gpu-operator
func (m *MIGManagerSpec) IsEnabled() bool {
	if m.Enabled == nil {
		// default is true if not specified by user
		return true
	}
	return *m.Enabled
}

// IsEnabled returns true if node-status-exporter is
// enabled through gpu-operator
func (m *NodeStatusExporterSpec) IsEnabled() bool {
	if m.Enabled == nil {
		// default is false if not specified by user
		return false
	}
	return *m.Enabled
}

// IsEnabled returns true if GPUDirect RDMA are enabled through gpu-operator
func (g *GPUDirectRDMASpec) IsEnabled() bool {
	if g.Enabled == nil {
		// GPUDirectRDMA is disabled by default
		return false
	}
	return *g.Enabled
}

// IsHostMOFED returns true if GPUDirect RDMA is enabled through MOFED installed on the host
func (g *GPUDirectRDMASpec) IsHostMOFED() bool {
	if g.UseHostMOFED == nil {
		// GPUDirectRDMA is disabled by default
		return false
	}
	return g.IsEnabled() && *g.UseHostMOFED
}

// IsEnabled returns true if GPUDirect Storage are enabled through gpu-operator
func (gds *GPUDirectStorageSpec) IsEnabled() bool {
	if gds.Enabled == nil {
		// GPUDirectStorage is disabled by default
		return false
	}
	return *gds.Enabled
}

// IsEnabled returns true if GDRCopy is enabled through gpu-operator
func (gdrcopy *GDRCopySpec) IsEnabled() bool {
	if gdrcopy.Enabled == nil {
		// GDRCopy is disabled by default
		return false
	}
	return *gdrcopy.Enabled
}

// IsEnabled returns true if DCGM hostengine as a separate Pod is enabled through gpu-perator
func (dcgm *DCGMSpec) IsEnabled() bool {
	if dcgm.Enabled == nil {
		// DCGM is enabled by default
		return true
	}
	return *dcgm.Enabled
}

// IsEnabled returns true if ServiceMonitor for DCGM Exporter is enabled through gpu-operator
func (sm *DCGMExporterServiceMonitorConfig) IsEnabled() bool {
	if sm.Enabled == nil {
		// ServiceMonitor for DCGM Exporter is disabled by default
		return false
	}
	return *sm.Enabled
}

// IsNLSEnabled returns true if NLS should be used for licensing the driver
func (l *DriverLicensingConfigSpec) IsNLSEnabled() bool {
	if l.NLSEnabled == nil {
		// NLS is not enabled by default
		return false
	}
	return *l.NLSEnabled
}

// IsEnabled returns true if CDI is enabled as a mechanism for
// providing GPU access to containers
func (c *CDIConfigSpec) IsEnabled() bool {
	if c.Enabled == nil {
		return false
	}
	return *c.Enabled
}

// IsDefault returns true if CDI is enabled as the default
// mechanism for providing GPU access to containers
func (c *CDIConfigSpec) IsDefault() bool {
	if c.Default == nil {
		return false
	}
	return *c.Default
}

// IsEnabled returns true if Kata Manager is enabled
func (k *KataManagerSpec) IsEnabled() bool {
	if k.Enabled == nil {
		return false
	}
	return *k.Enabled
}

// IsEnabled returns true if CC Manager is enabled for configuring
// CC mode on compatible GPUs on the node
func (c *CCManagerSpec) IsEnabled() bool {
	if c.Enabled == nil {
		return false
	}
	return *c.Enabled
}
