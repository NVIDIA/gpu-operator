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

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	corev1 "k8s.io/api/core/v1"
)

// EDIT THIS FILE!  THIS IS SCAFFOLDING FOR YOU TO OWN!
// NOTE: json tags are required.  Any new fields you add must have json tags for the fields to be serialized.

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
	// PSP defines spec for handling PodSecurityPolicies
	PSP PSPSpec `json:"psp,omitempty"`
	// Validator defines the spec for operator-validator daemonset
	Validator ValidatorSpec `json:"validator,omitempty"`
	// GPUDirectStorage defines the spec for GDS components(Experimental)
	GPUDirectStorage *GPUDirectStorageSpec `json:"gds,omitempty"`
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

	// UseOpenShiftDriverToolkit indicates if DriverToolkit image should be used on OpenShift to build and install driver modules
	// +operator-sdk:gen-csv:customresourcedefinitions.specDescriptors=true
	// +operator-sdk:gen-csv:customresourcedefinitions.specDescriptors.displayName="On OpenShift, enable DriverToolkit image to build and install driver modules"
	// +operator-sdk:gen-csv:customresourcedefinitions.specDescriptors.x-descriptors="urn:alm:descriptor:com.tectonic.ui:booleanSwitch"
	UseOpenShiftDriverToolkit *bool `json:"use_ocp_driver_toolkit,omitempty"`
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

// DaemonsetsSpec indicates common configuration for all Daemonsets managed by GPU Operator
type DaemonsetsSpec struct {
	// Optional: Set tolerations
	// +operator-sdk:gen-csv:customresourcedefinitions.specDescriptors=true
	// +operator-sdk:gen-csv:customresourcedefinitions.specDescriptors.displayName="Tolerations"
	// +operator-sdk:gen-csv:customresourcedefinitions.specDescriptors.x-descriptors="urn:alm:descriptor:com.tectonic.ui:advanced,urn:alm:descriptor:io.kubernetes:Tolerations"
	Tolerations []corev1.Toleration `json:"tolerations,omitempty"`

	// +kubebuilder:validation:Optional
	// +operator-sdk:gen-csv:customresourcedefinitions.specDescriptors=true
	// +operator-sdk:gen-csv:customresourcedefinitions.specDescriptors.displayName="PriorityClassName"
	PriorityClassName string `json:"priorityClassName,omitempty"`
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
	Resources *corev1.ResourceRequirements `json:"resources,omitempty"`

	// Optional: List of arguments
	// +operator-sdk:gen-csv:customresourcedefinitions.specDescriptors=true
	// +operator-sdk:gen-csv:customresourcedefinitions.specDescriptors.displayName="Arguments"
	// +operator-sdk:gen-csv:customresourcedefinitions.specDescriptors.x-descriptors="urn:alm:descriptor:com.tectonic.ui:advanced,urn:alm:descriptor:com.tectonic.ui:text"
	Args []string `json:"args,omitempty"`

	// Optional: List of environment variables
	// +operator-sdk:gen-csv:customresourcedefinitions.specDescriptors=true
	// +operator-sdk:gen-csv:customresourcedefinitions.specDescriptors.displayName="Environment Variables"
	// +operator-sdk:gen-csv:customresourcedefinitions.specDescriptors.x-descriptors="urn:alm:descriptor:com.tectonic.ui:advanced,urn:alm:descriptor:com.tectonic.ui:text"
	Env []corev1.EnvVar `json:"env,omitempty"`
}

// PluginValidatorSpec defines validator spec for NVIDIA Device Plugin
type PluginValidatorSpec struct {
	// Optional: List of environment variables
	// +operator-sdk:gen-csv:customresourcedefinitions.specDescriptors=true
	// +operator-sdk:gen-csv:customresourcedefinitions.specDescriptors.displayName="Environment Variables"
	// +operator-sdk:gen-csv:customresourcedefinitions.specDescriptors.x-descriptors="urn:alm:descriptor:com.tectonic.ui:advanced,urn:alm:descriptor:com.tectonic.ui:text"
	Env []corev1.EnvVar `json:"env,omitempty"`
}

// ToolkitValidatorSpec defines validator spec for NVIDIA Container Toolkit
type ToolkitValidatorSpec struct {
	// Optional: List of environment variables
	// +operator-sdk:gen-csv:customresourcedefinitions.specDescriptors=true
	// +operator-sdk:gen-csv:customresourcedefinitions.specDescriptors.displayName="Environment Variables"
	// +operator-sdk:gen-csv:customresourcedefinitions.specDescriptors.x-descriptors="urn:alm:descriptor:com.tectonic.ui:advanced,urn:alm:descriptor:com.tectonic.ui:text"
	Env []corev1.EnvVar `json:"env,omitempty"`
}

// DriverValidatorSpec defines validator spec for NVIDIA Driver validation
type DriverValidatorSpec struct {
	// Optional: List of environment variables
	// +operator-sdk:gen-csv:customresourcedefinitions.specDescriptors=true
	// +operator-sdk:gen-csv:customresourcedefinitions.specDescriptors.displayName="Environment Variables"
	// +operator-sdk:gen-csv:customresourcedefinitions.specDescriptors.x-descriptors="urn:alm:descriptor:com.tectonic.ui:advanced,urn:alm:descriptor:com.tectonic.ui:text"
	Env []corev1.EnvVar `json:"env,omitempty"`
}

// CUDAValidatorSpec defines validator spec for CUDA validation workload pod
type CUDAValidatorSpec struct {
	// Optional: List of environment variables
	// +operator-sdk:gen-csv:customresourcedefinitions.specDescriptors=true
	// +operator-sdk:gen-csv:customresourcedefinitions.specDescriptors.displayName="Environment Variables"
	// +operator-sdk:gen-csv:customresourcedefinitions.specDescriptors.x-descriptors="urn:alm:descriptor:com.tectonic.ui:advanced,urn:alm:descriptor:com.tectonic.ui:text"
	Env []corev1.EnvVar `json:"env,omitempty"`
}

// VFIOPCIValidatorSpec defines validator spec for NVIDIA VFIO-PCI device validation
type VFIOPCIValidatorSpec struct {
	// Optional: List of environment variables
	// +operator-sdk:gen-csv:customresourcedefinitions.specDescriptors=true
	// +operator-sdk:gen-csv:customresourcedefinitions.specDescriptors.displayName="Environment Variables"
	// +operator-sdk:gen-csv:customresourcedefinitions.specDescriptors.x-descriptors="urn:alm:descriptor:com.tectonic.ui:advanced,urn:alm:descriptor:com.tectonic.ui:text"
	Env []corev1.EnvVar `json:"env,omitempty"`
}

// VGPUManagerValidatorSpec defines validator spec for NVIDIA vGPU Manager
type VGPUManagerValidatorSpec struct {
	// Optional: List of environment variables
	// +operator-sdk:gen-csv:customresourcedefinitions.specDescriptors=true
	// +operator-sdk:gen-csv:customresourcedefinitions.specDescriptors.displayName="Environment Variables"
	// +operator-sdk:gen-csv:customresourcedefinitions.specDescriptors.x-descriptors="urn:alm:descriptor:com.tectonic.ui:advanced,urn:alm:descriptor:com.tectonic.ui:text"
	Env []corev1.EnvVar `json:"env,omitempty"`
}

// VGPUDevicesValidatorSpec defines validator spec for NVIDIA vGPU device validator
type VGPUDevicesValidatorSpec struct {
	// Optional: List of environment variables
	// +operator-sdk:gen-csv:customresourcedefinitions.specDescriptors=true
	// +operator-sdk:gen-csv:customresourcedefinitions.specDescriptors.displayName="Environment Variables"
	// +operator-sdk:gen-csv:customresourcedefinitions.specDescriptors.x-descriptors="urn:alm:descriptor:com.tectonic.ui:advanced,urn:alm:descriptor:com.tectonic.ui:text"
	Env []corev1.EnvVar `json:"env,omitempty"`
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
	Env []corev1.EnvVar `json:"env,omitempty"`
}

// DriverSpec defines the properties for NVIDIA Driver deployment
type DriverSpec struct {
	// Enabled indicates if deployment of NVIDIA Driver through operator is enabled
	// +operator-sdk:gen-csv:customresourcedefinitions.specDescriptors=true
	// +operator-sdk:gen-csv:customresourcedefinitions.specDescriptors.displayName="Enable NVIDIA Driver deployment through GPU Operator"
	// +operator-sdk:gen-csv:customresourcedefinitions.specDescriptors.x-descriptors="urn:alm:descriptor:com.tectonic.ui:booleanSwitch"
	Enabled *bool `json:"enabled,omitempty"`

	GPUDirectRDMA *GPUDirectRDMASpec `json:"rdma,omitempty"`

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
	Resources *corev1.ResourceRequirements `json:"resources,omitempty"`

	// Optional: List of arguments
	// +operator-sdk:gen-csv:customresourcedefinitions.specDescriptors=true
	// +operator-sdk:gen-csv:customresourcedefinitions.specDescriptors.displayName="Arguments"
	// +operator-sdk:gen-csv:customresourcedefinitions.specDescriptors.x-descriptors="urn:alm:descriptor:com.tectonic.ui:advanced,urn:alm:descriptor:com.tectonic.ui:text"
	Args []string `json:"args,omitempty"`

	// Optional: List of environment variables
	// +operator-sdk:gen-csv:customresourcedefinitions.specDescriptors=true
	// +operator-sdk:gen-csv:customresourcedefinitions.specDescriptors.displayName="Environment Variables"
	// +operator-sdk:gen-csv:customresourcedefinitions.specDescriptors.x-descriptors="urn:alm:descriptor:com.tectonic.ui:advanced,urn:alm:descriptor:com.tectonic.ui:text"
	Env []corev1.EnvVar `json:"env,omitempty"`

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

	// Optional: Configuration for rolling update of NVIDIA Driver DaemonSet pods
	// +operator-sdk:gen-csv:customresourcedefinitions.specDescriptors=true
	// +operator-sdk:gen-csv:customresourcedefinitions.specDescriptors.displayName="Rolling update configuration for NVIDIA Driver DaemonSet pods"
	RollingUpdate *RollingUpdateSpec `json:"rollingUpdate,omitempty"`
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
	Resources *corev1.ResourceRequirements `json:"resources,omitempty"`

	// Optional: List of arguments
	// +operator-sdk:gen-csv:customresourcedefinitions.specDescriptors=true
	// +operator-sdk:gen-csv:customresourcedefinitions.specDescriptors.displayName="Arguments"
	// +operator-sdk:gen-csv:customresourcedefinitions.specDescriptors.x-descriptors="urn:alm:descriptor:com.tectonic.ui:advanced,urn:alm:descriptor:com.tectonic.ui:text"
	Args []string `json:"args,omitempty"`

	// Optional: List of environment variables
	// +operator-sdk:gen-csv:customresourcedefinitions.specDescriptors=true
	// +operator-sdk:gen-csv:customresourcedefinitions.specDescriptors.displayName="Environment Variables"
	// +operator-sdk:gen-csv:customresourcedefinitions.specDescriptors.x-descriptors="urn:alm:descriptor:com.tectonic.ui:advanced,urn:alm:descriptor:com.tectonic.ui:text"
	Env []corev1.EnvVar `json:"env,omitempty"`

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
	Resources *corev1.ResourceRequirements `json:"resources,omitempty"`

	// Optional: List of arguments
	// +operator-sdk:gen-csv:customresourcedefinitions.specDescriptors=true
	// +operator-sdk:gen-csv:customresourcedefinitions.specDescriptors.displayName="Arguments"
	// +operator-sdk:gen-csv:customresourcedefinitions.specDescriptors.x-descriptors="urn:alm:descriptor:com.tectonic.ui:advanced,urn:alm:descriptor:com.tectonic.ui:text"
	Args []string `json:"args,omitempty"`

	// Optional: List of environment variables
	// +operator-sdk:gen-csv:customresourcedefinitions.specDescriptors=true
	// +operator-sdk:gen-csv:customresourcedefinitions.specDescriptors.displayName="Environment Variables"
	// +operator-sdk:gen-csv:customresourcedefinitions.specDescriptors.x-descriptors="urn:alm:descriptor:com.tectonic.ui:advanced,urn:alm:descriptor:com.tectonic.ui:text"
	Env []corev1.EnvVar `json:"env,omitempty"`
}

// DevicePluginSpec defines the properties for NVIDIA Device Plugin deployment
type DevicePluginSpec struct {
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
	Resources *corev1.ResourceRequirements `json:"resources,omitempty"`

	// Optional: List of arguments
	// +operator-sdk:gen-csv:customresourcedefinitions.specDescriptors=true
	// +operator-sdk:gen-csv:customresourcedefinitions.specDescriptors.displayName="Arguments"
	// +operator-sdk:gen-csv:customresourcedefinitions.specDescriptors.x-descriptors="urn:alm:descriptor:com.tectonic.ui:advanced,urn:alm:descriptor:com.tectonic.ui:text"
	Args []string `json:"args,omitempty"`

	// Optional: List of environment variables
	// +operator-sdk:gen-csv:customresourcedefinitions.specDescriptors=true
	// +operator-sdk:gen-csv:customresourcedefinitions.specDescriptors.displayName="Environment Variables"
	// +operator-sdk:gen-csv:customresourcedefinitions.specDescriptors.x-descriptors="urn:alm:descriptor:com.tectonic.ui:advanced,urn:alm:descriptor:com.tectonic.ui:text"
	Env []corev1.EnvVar `json:"env,omitempty"`

	// Optional: Configuration for the NVIDIA Device Plugin via the ConfigMap
	// +operator-sdk:gen-csv:customresourcedefinitions.specDescriptors=true
	// +operator-sdk:gen-csv:customresourcedefinitions.specDescriptors.displayName="Configuration for the NVIDIA Device Plugin via the ConfigMap"
	Config *DevicePluginConfig `json:"config,omitempty"`
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
	Resources *corev1.ResourceRequirements `json:"resources,omitempty"`

	// Optional: List of arguments
	// +operator-sdk:gen-csv:customresourcedefinitions.specDescriptors=true
	// +operator-sdk:gen-csv:customresourcedefinitions.specDescriptors.displayName="Arguments"
	// +operator-sdk:gen-csv:customresourcedefinitions.specDescriptors.x-descriptors="urn:alm:descriptor:com.tectonic.ui:advanced,urn:alm:descriptor:com.tectonic.ui:text"
	Args []string `json:"args,omitempty"`

	// Optional: List of environment variables
	// +operator-sdk:gen-csv:customresourcedefinitions.specDescriptors=true
	// +operator-sdk:gen-csv:customresourcedefinitions.specDescriptors.displayName="Environment Variables"
	// +operator-sdk:gen-csv:customresourcedefinitions.specDescriptors.x-descriptors="urn:alm:descriptor:com.tectonic.ui:advanced,urn:alm:descriptor:com.tectonic.ui:text"
	Env []corev1.EnvVar `json:"env,omitempty"`
}

// DCGMExporterSpec defines the properties for NVIDIA DCGM Exporter deployment
type DCGMExporterSpec struct {
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
	Resources *corev1.ResourceRequirements `json:"resources,omitempty"`

	// Optional: List of arguments
	// +operator-sdk:gen-csv:customresourcedefinitions.specDescriptors=true
	// +operator-sdk:gen-csv:customresourcedefinitions.specDescriptors.displayName="Arguments"
	// +operator-sdk:gen-csv:customresourcedefinitions.specDescriptors.x-descriptors="urn:alm:descriptor:com.tectonic.ui:advanced,urn:alm:descriptor:com.tectonic.ui:text"
	Args []string `json:"args,omitempty"`

	// Optional: List of environment variables
	// +operator-sdk:gen-csv:customresourcedefinitions.specDescriptors=true
	// +operator-sdk:gen-csv:customresourcedefinitions.specDescriptors.displayName="Environment Variables"
	// +operator-sdk:gen-csv:customresourcedefinitions.specDescriptors.x-descriptors="urn:alm:descriptor:com.tectonic.ui:advanced,urn:alm:descriptor:com.tectonic.ui:text"
	Env []corev1.EnvVar `json:"env,omitempty"`

	// Optional: Custom metrics configuration for NVIDIA DCGM Exporter
	// +operator-sdk:gen-csv:customresourcedefinitions.specDescriptors=true
	// +operator-sdk:gen-csv:customresourcedefinitions.specDescriptors.displayName="Custom Metrics Configuration For DCGM Exporter"
	MetricsConfig *DCGMExporterMetricsConfig `json:"config,omitempty"`
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
	Resources *corev1.ResourceRequirements `json:"resources,omitempty"`

	// Optional: List of arguments
	// +operator-sdk:gen-csv:customresourcedefinitions.specDescriptors=true
	// +operator-sdk:gen-csv:customresourcedefinitions.specDescriptors.displayName="Arguments"
	// +operator-sdk:gen-csv:customresourcedefinitions.specDescriptors.x-descriptors="urn:alm:descriptor:com.tectonic.ui:advanced,urn:alm:descriptor:com.tectonic.ui:text"
	Args []string `json:"args,omitempty"`

	// Optional: List of environment variables
	// +operator-sdk:gen-csv:customresourcedefinitions.specDescriptors=true
	// +operator-sdk:gen-csv:customresourcedefinitions.specDescriptors.displayName="Environment Variables"
	// +operator-sdk:gen-csv:customresourcedefinitions.specDescriptors.x-descriptors="urn:alm:descriptor:com.tectonic.ui:advanced,urn:alm:descriptor:com.tectonic.ui:text"
	Env []corev1.EnvVar `json:"env,omitempty"`

	// HostPort represents host port that needs to be bound for DCGM engine (Default: 5555)
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
	Resources *corev1.ResourceRequirements `json:"resources,omitempty"`

	// Optional: List of arguments
	// +operator-sdk:gen-csv:customresourcedefinitions.specDescriptors=true
	// +operator-sdk:gen-csv:customresourcedefinitions.specDescriptors.displayName="Arguments"
	// +operator-sdk:gen-csv:customresourcedefinitions.specDescriptors.x-descriptors="urn:alm:descriptor:com.tectonic.ui:advanced,urn:alm:descriptor:com.tectonic.ui:text"
	Args []string `json:"args,omitempty"`

	// Optional: List of environment variables
	// +operator-sdk:gen-csv:customresourcedefinitions.specDescriptors=true
	// +operator-sdk:gen-csv:customresourcedefinitions.specDescriptors.displayName="Environment Variables"
	// +operator-sdk:gen-csv:customresourcedefinitions.specDescriptors.x-descriptors="urn:alm:descriptor:com.tectonic.ui:advanced,urn:alm:descriptor:com.tectonic.ui:text"
	Env []corev1.EnvVar `json:"env,omitempty"`
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

// RollingUpdateSpec defines configuration for the rolling update of NVIDIA Driver DaemonSet pods
type RollingUpdateSpec struct {
	// +kubebuilder:validation:Optional
	// +operator-sdk:gen-csv:customresourcedefinitions.specDescriptors=true
	// +operator-sdk:gen-csv:customresourcedefinitions.specDescriptors.displayName="Maximum number of nodes to simultaneously apply pod updates on. Default 1"
	// +operator-sdk:gen-csv:customresourcedefinitions.specDescriptors.x-descriptors="urn:alm:descriptor:com.tectonic.ui:text"
	MaxUnavailable string `json:"maxUnavailable,omitempty"`
}

// GPUFeatureDiscoverySpec defines the properties for GPU Feature Discovery Plugin
type GPUFeatureDiscoverySpec struct {
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
	Resources *corev1.ResourceRequirements `json:"resources,omitempty"`

	// Optional: List of arguments
	// +operator-sdk:gen-csv:customresourcedefinitions.specDescriptors=true
	// +operator-sdk:gen-csv:customresourcedefinitions.specDescriptors.displayName="Arguments"
	// +operator-sdk:gen-csv:customresourcedefinitions.specDescriptors.x-descriptors="urn:alm:descriptor:com.tectonic.ui:advanced,urn:alm:descriptor:com.tectonic.ui:text"
	Args []string `json:"args,omitempty"`

	// Optional: List of environment variables
	// +operator-sdk:gen-csv:customresourcedefinitions.specDescriptors=true
	// +operator-sdk:gen-csv:customresourcedefinitions.specDescriptors.displayName="Environment Variables"
	// +operator-sdk:gen-csv:customresourcedefinitions.specDescriptors.x-descriptors="urn:alm:descriptor:com.tectonic.ui:advanced,urn:alm:descriptor:com.tectonic.ui:text"
	Env []corev1.EnvVar `json:"env,omitempty"`
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
	Resources *corev1.ResourceRequirements `json:"resources,omitempty"`

	// Optional: List of arguments
	// +operator-sdk:gen-csv:customresourcedefinitions.specDescriptors=true
	// +operator-sdk:gen-csv:customresourcedefinitions.specDescriptors.displayName="Arguments"
	// +operator-sdk:gen-csv:customresourcedefinitions.specDescriptors.x-descriptors="urn:alm:descriptor:com.tectonic.ui:advanced,urn:alm:descriptor:com.tectonic.ui:text"
	Args []string `json:"args,omitempty"`

	// Optional: List of environment variables
	// +operator-sdk:gen-csv:customresourcedefinitions.specDescriptors=true
	// +operator-sdk:gen-csv:customresourcedefinitions.specDescriptors.displayName="Environment Variables"
	// +operator-sdk:gen-csv:customresourcedefinitions.specDescriptors.x-descriptors="urn:alm:descriptor:com.tectonic.ui:advanced,urn:alm:descriptor:com.tectonic.ui:text"
	Env []corev1.EnvVar `json:"env,omitempty"`

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
	Env []corev1.EnvVar `json:"env,omitempty"`
}

// MIGPartedConfigSpec defines custom mig-parted config for NVIDIA MIG Manager container
type MIGPartedConfigSpec struct {
	// ConfigMap name
	// +kubebuilder:validation:Optional
	// +operator-sdk:gen-csv:customresourcedefinitions.specDescriptors=true
	// +operator-sdk:gen-csv:customresourcedefinitions.specDescriptors.displayName="ConfigMap Name"
	// +operator-sdk:gen-csv:customresourcedefinitions.specDescriptors.x-descriptors="urn:alm:descriptor:com.tectonic.ui:text"
	Name string `json:"name,omitempty"`
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
	Resources *corev1.ResourceRequirements `json:"resources,omitempty"`

	// Optional: List of arguments
	// +operator-sdk:gen-csv:customresourcedefinitions.specDescriptors=true
	// +operator-sdk:gen-csv:customresourcedefinitions.specDescriptors.displayName="Arguments"
	// +operator-sdk:gen-csv:customresourcedefinitions.specDescriptors.x-descriptors="urn:alm:descriptor:com.tectonic.ui:advanced,urn:alm:descriptor:com.tectonic.ui:text"
	Args []string `json:"args,omitempty"`

	// Optional: List of environment variables
	// +operator-sdk:gen-csv:customresourcedefinitions.specDescriptors=true
	// +operator-sdk:gen-csv:customresourcedefinitions.specDescriptors.displayName="Environment Variables"
	// +operator-sdk:gen-csv:customresourcedefinitions.specDescriptors.x-descriptors="urn:alm:descriptor:com.tectonic.ui:advanced,urn:alm:descriptor:com.tectonic.ui:text"
	Env []corev1.EnvVar `json:"env,omitempty"`

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
	Resources *corev1.ResourceRequirements `json:"resources,omitempty"`

	// Optional: List of arguments
	// +operator-sdk:gen-csv:customresourcedefinitions.specDescriptors=true
	// +operator-sdk:gen-csv:customresourcedefinitions.specDescriptors.displayName="Arguments"
	// +operator-sdk:gen-csv:customresourcedefinitions.specDescriptors.x-descriptors="urn:alm:descriptor:com.tectonic.ui:advanced,urn:alm:descriptor:com.tectonic.ui:text"
	Args []string `json:"args,omitempty"`

	// Optional: List of environment variables
	// +operator-sdk:gen-csv:customresourcedefinitions.specDescriptors=true
	// +operator-sdk:gen-csv:customresourcedefinitions.specDescriptors.displayName="Environment Variables"
	// +operator-sdk:gen-csv:customresourcedefinitions.specDescriptors.x-descriptors="urn:alm:descriptor:com.tectonic.ui:advanced,urn:alm:descriptor:com.tectonic.ui:text"
	Env []corev1.EnvVar `json:"env,omitempty"`

	// NVIDIA vGPU devices configuration for NVIDIA vGPU Device Manager container
	// +operator-sdk:gen-csv:customresourcedefinitions.specDescriptors=true
	// +operator-sdk:gen-csv:customresourcedefinitions.specDescriptors.displayName="NVIDIA vGPU devices configuration for NVIDIA vGPU Device Manager container"
	Config *VGPUDevicesConfigSpec `json:"config,omitempty"`
}

// VGPUDevicesConfigSpec defines vGPU devices configuration for NVIDIA vGPU Device Manager container
type VGPUDevicesConfigSpec struct {
	// ConfigMap name
	// +kubebuilder:validation:Optional
	// +kubebuilder:default=vgpu-devices-config
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
)

// ClusterPolicyStatus defines the observed state of ClusterPolicy
type ClusterPolicyStatus struct {
	// +kubebuilder:validation:Enum=ignored;ready;notReady
	// State indicates status of ClusterPolicy
	State State `json:"state"`
	// Namespace indicates a namespace in which the operator is installed
	Namespace string `json:"namespace,omitempty"`
}

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status

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
	return "", fmt.Errorf("Empty image path provided through both ClusterPolicy CR and ENV %s", imagePathEnvName)
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
	case *VFIOManagerSpec:
		config := spec.(*VFIOManagerSpec)
		return imagePath(config.Repository, config.Image, config.Version, "VFIO_MANAGER_IMAGE")
	case *VGPUDeviceManagerSpec:
		config := spec.(*VGPUDeviceManagerSpec)
		return imagePath(config.Repository, config.Image, config.Version, "VGPU_DEVICE_MANAGER_IMAGE")
	default:
		return "", fmt.Errorf("Invalid type to construct image path: %v", v)
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

// IsDriverEnabled returns true if driver install is enabled(default) through gpu-operator
func (d *DriverSpec) IsDriverEnabled() bool {
	if d.Enabled == nil {
		// default is true if not specified by user
		return true
	}
	return *d.Enabled
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

// IsToolkitEnabled returns true if container-toolkit install is enabled(default) through gpu-operator
func (t *ToolkitSpec) IsToolkitEnabled() bool {
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

// IsEnabled returns true if PodSecurityPolicies are enabled for all Pods
func (p *PSPSpec) IsEnabled() bool {
	if p.Enabled == nil {
		// PSP is disabled by default
		return false
	}
	return *p.Enabled
}

// IsMIGManagerEnabled returns true if mig-manager is enabled(default) through gpu-operator
func (m *MIGManagerSpec) IsMIGManagerEnabled() bool {
	if m.Enabled == nil {
		// default is true if not specified by user
		return true
	}
	return *m.Enabled
}

// IsNodeStatusExporterEnabled returns true if node-status-exporter is
// enabled through gpu-operator
func (m *NodeStatusExporterSpec) IsNodeStatusExporterEnabled() bool {
	if m.Enabled == nil {
		// default is false if not specified by user
		return false
	}
	return *m.Enabled
}

// IsEnabled returns true if GPUDirect RDMA are enabled through gpu-perator
func (g *GPUDirectRDMASpec) IsEnabled() bool {
	if g.Enabled == nil {
		// GPUDirectRDMA is disabled by default
		return false
	}
	return *g.Enabled
}

// IsEnabled returns true if GPUDirect Storage are enabled through gpu-perator
func (gds *GPUDirectStorageSpec) IsEnabled() bool {
	if gds.Enabled == nil {
		// GPUDirectStorage is disabled by default
		return false
	}
	return *gds.Enabled
}

// IsEnabled returns true if DCGM hostengine as a separate Pod is enabled through gpu-perator
func (dcgm *DCGMSpec) IsEnabled() bool {
	if dcgm.Enabled == nil {
		// DCGM is enabled by default
		return true
	}
	return *dcgm.Enabled
}

// IsNLSEnabled returns true if NLS should be used for licensing the driver
func (l *DriverLicensingConfigSpec) IsNLSEnabled() bool {
	if l.NLSEnabled == nil {
		// NLS is not enabled by default
		return false
	}
	return *l.NLSEnabled
}
