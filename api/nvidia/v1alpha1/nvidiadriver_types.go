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

package v1alpha1

import (
	"fmt"
	"strings"

	"github.com/regclient/regclient/types/ref"
	"golang.org/x/mod/semver"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/NVIDIA/gpu-operator/internal/consts"
	"github.com/NVIDIA/gpu-operator/internal/image"
)

const (
	NVIDIADriverCRDName = "NVIDIADriver"
)

// EDIT THIS FILE!  THIS IS SCAFFOLDING FOR YOU TO OWN!
// NOTE: json tags are required.  Any new fields you add must have json tags for the fields to be serialized.

// NVIDIADriverSpec defines the desired state of NVIDIADriver
type NVIDIADriverSpec struct {
	// INSERT ADDITIONAL SPEC FIELDS - desired state of cluster
	// Important: Run "make" to regenerate code after modifying this file

	// +kubebuilder:validation:Enum=gpu;vgpu;vgpu-host-manager
	// +kubebuilder:default=gpu
	// +kubebuilder:validation:XValidation:rule="self == oldSelf",message="driverType is an immutable field. Please create a new NvidiaDriver resource instead when you want to change this setting."
	DriverType DriverType `json:"driverType"`

	// UsePrecompiled indicates if deployment of NVIDIA Driver using pre-compiled modules is enabled
	// +operator-sdk:gen-csv:customresourcedefinitions.specDescriptors=true
	// +operator-sdk:gen-csv:customresourcedefinitions.specDescriptors.displayName="Enable NVIDIA Driver deployment using pre-compiled modules"
	// +operator-sdk:gen-csv:customresourcedefinitions.specDescriptors.x-descriptors="urn:alm:descriptor:com.tectonic.ui:booleanSwitch"
	// +kubebuilder:validation:XValidation:rule="self == oldSelf",message="usePrecompiled is an immutable field. Please create a new NvidiaDriver resource instead when you want to change this setting."
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

	// NVIDIA Driver container startup probe settings
	StartupProbe *ContainerProbeSpec `json:"startupProbe,omitempty"`

	// NVIDIA Driver container liveness probe settings
	LivenessProbe *ContainerProbeSpec `json:"livenessProbe,omitempty"`

	// NVIDIA Driver container readiness probe settings
	ReadinessProbe *ContainerProbeSpec `json:"readinessProbe,omitempty"`

	// GPUDirectRDMA defines the spec for NVIDIA Peer Memory driver
	GPUDirectRDMA *GPUDirectRDMASpec `json:"rdma,omitempty"`

	// GPUDirectStorage defines the spec for GDS driver
	GPUDirectStorage *GPUDirectStorageSpec `json:"gds,omitempty"`

	// GDRCopy defines the spec for GDRCopy driver
	GDRCopy *GDRCopySpec `json:"gdrcopy,omitempty"`

	// NVIDIA Driver repository
	// +kubebuilder:validation:Optional
	Repository string `json:"repository,omitempty"`

	// NVIDIA Driver container image name
	// +kubebuilder:default=nvcr.io/nvidia/driver
	Image string `json:"image"`

	// NVIDIA Driver version (or just branch for precompiled drivers)
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
	VirtualTopologyConfig *VirtualTopologyConfigSpec `json:"virtualTopologyConfig,omitempty"`

	// Optional: Kernel module configuration parameters for the NVIDIA Driver
	// +operator-sdk:gen-csv:customresourcedefinitions.specDescriptors=true
	// +operator-sdk:gen-csv:customresourcedefinitions.specDescriptors.displayName="Kernel module configuration parameters for the NVIDIA driver"
	KernelModuleConfig *KernelModuleConfigSpec `json:"kernelModuleConfig,omitempty"`

	// +kubebuilder:validation:Optional
	// NodeSelector specifies a selector for installation of NVIDIA driver
	NodeSelector map[string]string `json:"nodeSelector,omitempty"`

	// +kubebuilder:validation:Optional
	// Affinity specifies node affinity rules for driver pods
	NodeAffinity *corev1.NodeAffinity `json:"nodeAffinity,omitempty"`

	// +kubebuilder:validation:Optional
	// Optional: Map of string keys and values that can be used to organize and categorize
	// (scope and select) objects. May match selectors of replication controllers
	// and services.
	Labels map[string]string `json:"labels,omitempty"`

	// +kubebuilder:validation:Optional
	// Optional: Annotations is an unstructured key value map stored with a resource that may be
	// set by external tools to store and retrieve arbitrary metadata. They are not
	// queryable and should be preserved when modifying objects.
	Annotations map[string]string `json:"annotations,omitempty"`

	// +kubebuilder:validation:Optional
	// Optional: Set tolerations
	// +operator-sdk:gen-csv:customresourcedefinitions.specDescriptors=true
	// +operator-sdk:gen-csv:customresourcedefinitions.specDescriptors.displayName="Tolerations"
	// +operator-sdk:gen-csv:customresourcedefinitions.specDescriptors.x-descriptors="urn:alm:descriptor:com.tectonic.ui:advanced,urn:alm:descriptor:io.kubernetes:Tolerations"
	Tolerations []corev1.Toleration `json:"tolerations,omitempty"`

	// +kubebuilder:validation:Optional
	// Optional: Set priorityClassName
	// +operator-sdk:gen-csv:customresourcedefinitions.specDescriptors=true
	// +operator-sdk:gen-csv:customresourcedefinitions.specDescriptors.displayName="PriorityClassName"
	PriorityClassName string `json:"priorityClassName,omitempty"`
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

// EnvVar represents an environment variable present in a Container.
type EnvVar struct {
	// Name of the environment variable.
	Name string `json:"name"`

	// Value of the environment variable.
	Value string `json:"value,omitempty"`
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

// GDRCopySpec defines the properties for NVIDIA GDRCopy driver deployment
type GDRCopySpec struct {
	// Enabled indicates if GDRCopy is enabled through GPU operator
	// +operator-sdk:gen-csv:customresourcedefinitions.specDescriptors=true
	// +operator-sdk:gen-csv:customresourcedefinitions.specDescriptors.displayName="Enable GDRCopy through GPU operator"
	// +operator-sdk:gen-csv:customresourcedefinitions.specDescriptors.x-descriptors="urn:alm:descriptor:com.tectonic.ui:booleanSwitch"
	Enabled *bool `json:"enabled,omitempty"`

	// GDRCopy diver image repository
	// +kubebuilder:validation:Optional
	Repository string `json:"repository,omitempty"`

	// GDRCopy driver image name
	// +kubebuilder:validation:Pattern=[a-zA-Z0-9\-]+
	Image string `json:"image,omitempty"`

	// GDRCopy driver image tag
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

// KernelModuleConfigSpec defines custom configuration parameters for the NVIDIA Driver
type KernelModuleConfigSpec struct {
	// +kubebuilder:validation:Optional
	// +operator-sdk:gen-csv:customresourcedefinitions.specDescriptors=true
	// +operator-sdk:gen-csv:customresourcedefinitions.specDescriptors.displayName="ConfigMap Name"
	// +operator-sdk:gen-csv:customresourcedefinitions.specDescriptors.x-descriptors="urn:alm:descriptor:com.tectonic.ui:text"
	Name string `json:"name,omitempty"`
}

// VirtualTopologyConfigSpec defines virtual topology daemon configuration with NVIDIA vGPU
type VirtualTopologyConfigSpec struct {
	// Optional: Config name representing virtual topology daemon configuration file nvidia-topologyd.conf
	// +kubebuilder:validation:Optional
	// +operator-sdk:gen-csv:customresourcedefinitions.specDescriptors=true
	// +operator-sdk:gen-csv:customresourcedefinitions.specDescriptors.displayName="ConfigMap Name"
	// +operator-sdk:gen-csv:customresourcedefinitions.specDescriptors.x-descriptors="urn:alm:descriptor:com.tectonic.ui:text"
	Name string `json:"name,omitempty"`
}

// DriverCertConfigSpec defines custom certificates configuration for NVIDIA Driver container
type DriverCertConfigSpec struct {
	// +kubebuilder:validation:Optional
	// +operator-sdk:gen-csv:customresourcedefinitions.specDescriptors=true
	// +operator-sdk:gen-csv:customresourcedefinitions.specDescriptors.displayName="ConfigMap Name"
	// +operator-sdk:gen-csv:customresourcedefinitions.specDescriptors.x-descriptors="urn:alm:descriptor:com.tectonic.ui:text"
	Name string `json:"name,omitempty"`
}

// DriverRepoConfigSpec defines custom repo configuration for NVIDIA Driver container
type DriverRepoConfigSpec struct {
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
	Name string `json:"name,omitempty"`

	// NLSEnabled indicates if NVIDIA Licensing System is used for licensing.
	// +operator-sdk:gen-csv:customresourcedefinitions.specDescriptors=true
	// +operator-sdk:gen-csv:customresourcedefinitions.specDescriptors.displayName="Enable NVIDIA Licensing System licensing"
	// +operator-sdk:gen-csv:customresourcedefinitions.specDescriptors.x-descriptors="urn:alm:descriptor:com.tectonic.ui:booleanSwitch"
	NLSEnabled *bool `json:"nlsEnabled,omitempty"`
}

// DriverType defines NVIDIA driver type
type DriverType string

const (
	// GPU driver type
	GPU DriverType = "gpu"
	// VGPU guest driver type
	VGPU DriverType = "vgpu"
	// VGPUHostManager specifies vgpu host manager type
	VGPUHostManager DriverType = "vgpu-host-manager"
)

// State indicates state of the NVIDIA driver managed by this instance
type State string

const (
	// Ready indicates that the NVIDIA driver managed by this instance is ready
	Ready State = "ready"
	// NotReady indicates that the NVIDIA driver managed by this instance is not ready
	NotReady State = "notReady"
	// Disabled indicates if the state is disabled in ClusterPolicy
	Disabled State = "disabled"
)

// NVIDIADriverStatus defines the observed state of NVIDIADriver
type NVIDIADriverStatus struct {
	// INSERT ADDITIONAL STATUS FIELD - define observed state of cluster
	// Important: Run "make" to regenerate code after modifying this file
	// +kubebuilder:validation:Enum=ignored;ready;notReady
	// State indicates status of NVIDIADriver instance
	State State `json:"state"`
	// Namespace indicates a namespace in which the operator and driver are installed
	Namespace string `json:"namespace,omitempty"`
	// Conditions is a list of conditions representing the NVIDIADriver's current state.
	Conditions []metav1.Condition `json:"conditions,omitempty"`
}

// +genclient
// +genclient:nonNamespaced
//+kubebuilder:object:root=true
//+kubebuilder:subresource:status
//+kubebuilder:resource:scope=Cluster,shortName={"nvd","nvdriver","nvdrivers"}
//+kubebuilder:printcolumn:name="Status",type=string,JSONPath=`.status.state`,priority=0
//+kubebuilder:printcolumn:name="Age",type=string,JSONPath=`.metadata.creationTimestamp`,priority=0

// NVIDIADriver is the Schema for the nvidiadrivers API
type NVIDIADriver struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   NVIDIADriverSpec   `json:"spec,omitempty"`
	Status NVIDIADriverStatus `json:"status,omitempty"`
}

//+kubebuilder:object:root=true

// NVIDIADriverList contains a list of NVIDIADriver
type NVIDIADriverList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []NVIDIADriver `json:"items"`
}

func init() {
	SchemeBuilder.Register(&NVIDIADriver{}, &NVIDIADriverList{})
}

// UsePrecompiledDrivers returns true if usePrecompiled option is enabled in spec
func (d *NVIDIADriverSpec) UsePrecompiledDrivers() bool {
	if d.UsePrecompiled == nil {
		return false
	}
	return *d.UsePrecompiled
}

// GetNodeSelector returns node selector labels for NVIDIA driver installation
func (d *NVIDIADriver) GetNodeSelector() map[string]string {
	ns := d.Spec.NodeSelector
	if ns == nil {
		ns = make(map[string]string)
		// If no node selector is specified then the driver is deployed
		// on all GPU nodes by default
		// nolint
		ns["nvidia.com/gpu.present"] = "true"
	}
	return ns
}

// GetImagePath returns the driver image path given the information
// provided in NVIDIADriverSpec and the osVersion passed as an argument.
// The driver image path will be in the following format unless the spec
// contains a digest.
// <repository>/<image>:<driver-ver>-<os-ver>
func (d *NVIDIADriverSpec) GetImagePath(osVersion string) (string, error) {
	// We pass an empty string for the last arg, the imagePathEnvName, since
	// we do not want any environment variable in the operator container
	// to be used as the default driver image. This means that the driver
	// image must be specified in the NVIDIADriver CR spec.
	image, err := image.ImagePath(d.Repository, d.Image, d.Version, "")
	if err != nil {
		return "", fmt.Errorf("failed to get image path from crd: %w", err)
	}

	// if image digest is specified, use it directly
	if !strings.Contains(image, "sha256:") {
		// append '-<osVersion>' to the driver tag
		image = fmt.Sprintf("%s-%s", image, osVersion)
	}

	_, err = ref.New(image)
	if err != nil {
		return "", fmt.Errorf("failed to parse driver image path: %w", err)
	}

	return image, nil
}

// GetImagePath returns the gds driver image path given the information
// provided in GPUDirectStorageSpec and the osVersion passed as an argument.
// The driver image path will be in the following format unless the spec
// contains a digest.
// <repository>/<image>:<driver-ver>-<os-ver>
func (d *GPUDirectStorageSpec) GetImagePath(osVersion string) (string, error) {
	image, err := image.ImagePath(d.Repository, d.Image, d.Version, "")
	if err != nil {
		return "", fmt.Errorf("failed to get image path from crd: %w", err)
	}

	// if image digest is specified, use it directly
	if !strings.Contains(image, "sha256:") {
		// append '-<osVersion>' to the driver tag
		image = fmt.Sprintf("%s-%s", image, osVersion)
	}

	_, err = ref.New(image)
	if err != nil {
		return "", fmt.Errorf("failed to parse driver image path: %w", err)
	}

	return image, nil
}

// GetImagePath returns the gdrcopy driver image path given the information
// provided in GDRCopySpec and the osVersion passed as an argument.
// The driver image path will be in the following format unless the spec
// contains a digest.
// <repository>/<image>:<driver-ver>-<os-ver>
func (d *GDRCopySpec) GetImagePath(osVersion string) (string, error) {
	image, err := image.ImagePath(d.Repository, d.Image, d.Version, "")
	if err != nil {
		return "", fmt.Errorf("failed to get image path from crd: %w", err)
	}

	// if image digest is specified, use it directly
	if !strings.Contains(image, "sha256:") {
		// append '-<osVersion>' to the driver tag
		image = fmt.Sprintf("%s-%s", image, osVersion)
	}

	_, err = ref.New(image)
	if err != nil {
		return "", fmt.Errorf("failed to parse driver image path: %w", err)
	}

	return image, nil
}

// GetPrecompiledImagePath returns the precompiled driver image path for a
// given os version and kernel version. Precompiled driver images follow
// the following format:
// <repository>/<image>:<driver-ver>-<kernel-ver>-<os-ver>
func (d *NVIDIADriverSpec) GetPrecompiledImagePath(osVersion string, kernelVersion string) (string, error) {
	// We pass an empty string for the last arg, the imagePathEnvName, since
	// we do not want any environment variable in the operator container
	// to be used as the default driver image. This means that the driver
	// image must be specified in the NVIDIADriver CR spec.
	image, err := image.ImagePath(d.Repository, d.Image, d.Version, "")
	if err != nil {
		return "", fmt.Errorf("failed to get image path from crd: %w", err)
	}

	// specifying a digest in the spec is not supported when using precompiled
	if strings.Contains(image, "sha256:") {
		return "", fmt.Errorf("specifying image digest is not supported when precompiled is enabled")
	}

	// append '-<kernelVersion>-<osVersion>' to the driver tag
	image = fmt.Sprintf("%s-%s-%s", image, kernelVersion, osVersion)

	_, err = ref.New(image)
	if err != nil {
		return "", fmt.Errorf("failed to parse driver image path: %w", err)
	}

	return image, nil
}

// IsGDSEnabled returns true if GPUDirectStorage is enabled through gpu-operator
func (d *NVIDIADriverSpec) IsGDSEnabled() bool {
	if d.GPUDirectStorage == nil || d.GPUDirectStorage.Enabled == nil {
		// default is false if not specified by user
		return false
	}
	return *d.GPUDirectStorage.Enabled
}

// IsGDRCopyEnabled returns true if GDRCopy is enabled through gpu-operator
func (d *NVIDIADriverSpec) IsGDRCopyEnabled() bool {
	if d.GDRCopy == nil || d.GDRCopy.Enabled == nil {
		// default is false if not specified by user
		return false
	}
	return *d.GDRCopy.Enabled
}

// IsOpenKernelModulesEnabled returns true if NVIDIA OpenRM drivers are enabled
func (d *NVIDIADriverSpec) IsOpenKernelModulesEnabled() bool {
	return d.KernelModuleType == "open"
}

// IsOpenKernelModulesRequired returns true if NVIDIA OpenRM drivers required in this configuration
func (d *NVIDIADriverSpec) IsOpenKernelModulesRequired() bool {
	// Add constraints here which require OpenRM drivers
	if !d.IsGDSEnabled() {
		return false
	}

	// If image digest is provided instead of the version, assume that OpenRM driver is required
	if strings.HasPrefix(d.GPUDirectStorage.Version, "sha256") {
		return true
	}

	gdsVersion := d.GPUDirectStorage.Version
	if !strings.HasPrefix(gdsVersion, "v") {
		gdsVersion = fmt.Sprintf("v%s", gdsVersion)
	}
	if semver.Compare(gdsVersion, consts.MinimumGDSVersionForOpenRM) >= 0 {
		return true
	}
	return false
}

// IsVGPULicensingEnabled returns true if the vgpu driver license config is provided
func (d *NVIDIADriverSpec) IsVGPULicensingEnabled() bool {
	if d.LicensingConfig == nil {
		return false
	}
	return d.LicensingConfig.Name != ""
}

// IsKernelModuleConfigEnabled returns true if kernel module config is provided
func (d *NVIDIADriverSpec) IsKernelModuleConfigEnabled() bool {
	if d.KernelModuleConfig == nil {
		return false
	}
	return d.KernelModuleConfig.Name != ""
}

// IsVirtualTopologyConfigEnabled returns true if the virtual topology daemon config is provided
func (d *NVIDIADriverSpec) IsVirtualTopologyConfigEnabled() bool {
	if d.VirtualTopologyConfig == nil {
		return false
	}
	return d.VirtualTopologyConfig.Name != ""
}

// IsRepoConfigEnabled returns true if additional repo config is provided
func (d *NVIDIADriverSpec) IsRepoConfigEnabled() bool {
	if d.RepoConfig == nil {
		return false
	}
	return d.RepoConfig.Name != ""
}

// IsCertConfigEnabled returns true if additional certificate config is provided
func (d *NVIDIADriverSpec) IsCertConfigEnabled() bool {
	if d.CertConfig == nil {
		return false
	}
	return d.CertConfig.Name != ""
}

// IsNLSEnabled returns true if NLS should be used for licensing the driver
func (l *DriverLicensingConfigSpec) IsNLSEnabled() bool {
	if l.NLSEnabled == nil {
		// NLS is enabled by default
		return true
	}
	return *l.NLSEnabled
}
