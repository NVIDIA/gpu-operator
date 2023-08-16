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

package v1alpha1

import (
	gpuv1 "github.com/NVIDIA/gpu-operator/api/v1"
	upgrade_v1alpha1 "github.com/NVIDIA/k8s-operator-libs/api/upgrade/v1alpha1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// EDIT THIS FILE!  THIS IS SCAFFOLDING FOR YOU TO OWN!
// NOTE: json tags are required.  Any new fields you add must have json tags for the fields to be serialized.

// NVIDIADriverSpec defines the desired state of NVIDIADriver
type NVIDIADriverSpec struct {
	// INSERT ADDITIONAL SPEC FIELDS - desired state of cluster
	// Important: Run "make" to regenerate code after modifying this file

	// UsePrecompiled indicates if deployment of NVIDIA Driver using pre-compiled modules is enabled
	// +operator-sdk:gen-csv:customresourcedefinitions.specDescriptors=true
	// +operator-sdk:gen-csv:customresourcedefinitions.specDescriptors.displayName="Enable NVIDIA Driver deployment using pre-compiled modules"
	// +operator-sdk:gen-csv:customresourcedefinitions.specDescriptors.x-descriptors="urn:alm:descriptor:com.tectonic.ui:booleanSwitch"
	UsePrecompiled *bool `json:"usePrecompiled,omitempty"`

	// NVIDIA Driver container startup probe settings
	StartupProbe *gpuv1.ContainerProbeSpec `json:"startupProbe,omitempty"`

	// NVIDIA Driver container liveness probe settings
	LivenessProbe *gpuv1.ContainerProbeSpec `json:"livenessProbe,omitempty"`

	// NVIDIA Driver container readiness probe settings
	ReadinessProbe *gpuv1.ContainerProbeSpec `json:"readinessProbe,omitempty"`

	// GPUDirectRDMA defines the spec for NVIDIA Peer Memory driver
	GPUDirectRDMA *gpuv1.GPUDirectRDMASpec `json:"rdma,omitempty"`

	// GPUDirectStorage defines the spec for GDS driver
	GPUDirectStorage *gpuv1.GPUDirectStorageSpec `json:"gds,omitempty"`

	// Driver auto-upgrade settings
	UpgradePolicy *upgrade_v1alpha1.DriverUpgradePolicySpec `json:"upgradePolicy,omitempty"`

	// NVIDIA Driver image name
	// +kubebuilder:validation:Pattern=[a-zA-Z0-9\-]+
	Image string `json:"image,omitempty"`

	// NVIDIA Driver image tag/digest (or just branch for precompiled drivers)
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
	Manager gpuv1.DriverManagerSpec `json:"manager,omitempty"`

	// Optional: Define resources requests and limits for each pod
	// +operator-sdk:gen-csv:customresourcedefinitions.specDescriptors=true
	// +operator-sdk:gen-csv:customresourcedefinitions.specDescriptors.displayName="Resource Requirements"
	// +operator-sdk:gen-csv:customresourcedefinitions.specDescriptors.x-descriptors="urn:alm:descriptor:com.tectonic.ui:advanced,urn:alm:descriptor:com.tectonic.ui:resourceRequirements"
	Resources *gpuv1.ResourceRequirements `json:"resources,omitempty"`

	// Optional: List of arguments
	// +operator-sdk:gen-csv:customresourcedefinitions.specDescriptors=true
	// +operator-sdk:gen-csv:customresourcedefinitions.specDescriptors.displayName="Arguments"
	// +operator-sdk:gen-csv:customresourcedefinitions.specDescriptors.x-descriptors="urn:alm:descriptor:com.tectonic.ui:advanced,urn:alm:descriptor:com.tectonic.ui:text"
	Args []string `json:"args,omitempty"`

	// Optional: List of environment variables
	// +operator-sdk:gen-csv:customresourcedefinitions.specDescriptors=true
	// +operator-sdk:gen-csv:customresourcedefinitions.specDescriptors.displayName="Environment Variables"
	// +operator-sdk:gen-csv:customresourcedefinitions.specDescriptors.x-descriptors="urn:alm:descriptor:com.tectonic.ui:advanced,urn:alm:descriptor:com.tectonic.ui:text"
	Env []gpuv1.EnvVar `json:"env,omitempty"`

	// Optional: Custom repo configuration for NVIDIA Driver container
	// +operator-sdk:gen-csv:customresourcedefinitions.specDescriptors=true
	// +operator-sdk:gen-csv:customresourcedefinitions.specDescriptors.displayName="Custom Repo Configuration For NVIDIA Driver Container"
	RepoConfig *gpuv1.DriverRepoConfigSpec `json:"repoConfig,omitempty"`

	// Optional: Custom certificates configuration for NVIDIA Driver container
	// +operator-sdk:gen-csv:customresourcedefinitions.specDescriptors=true
	// +operator-sdk:gen-csv:customresourcedefinitions.specDescriptors.displayName="Custom Certificates Configuration For NVIDIA Driver Container"
	CertConfig *gpuv1.DriverCertConfigSpec `json:"certConfig,omitempty"`

	// Optional: Licensing configuration for NVIDIA vGPU licensing
	// +operator-sdk:gen-csv:customresourcedefinitions.specDescriptors=true
	// +operator-sdk:gen-csv:customresourcedefinitions.specDescriptors.displayName="Licensing Configuration For NVIDIA vGPU Driver Container"
	LicensingConfig *gpuv1.DriverLicensingConfigSpec `json:"licensingConfig,omitempty"`

	// Optional: Virtual Topology Daemon configuration for NVIDIA vGPU drivers
	// +operator-sdk:gen-csv:customresourcedefinitions.specDescriptors=true
	// +operator-sdk:gen-csv:customresourcedefinitions.specDescriptors.displayName="Custom Virtual Topology Daemon Configuration For vGPU Driver Container"
	VirtualTopology *gpuv1.VirtualTopologyConfigSpec `json:"virtualTopology,omitempty"`

	// Optional: Kernel module configuration parameters for the NVIDIA Driver
	// +operator-sdk:gen-csv:customresourcedefinitions.specDescriptors=true
	// +operator-sdk:gen-csv:customresourcedefinitions.specDescriptors.displayName="Kernel module configuration parameters for the NVIDIA driver"
	KernelModuleConfig *gpuv1.KernelModuleConfigSpec `json:"kernelModuleConfig,omitempty"`
	//+kubebuilder:validation:Optional
	// NodeSelector specifies a selector for installation of NVIDIA driver
	NodeSelector map[string]string `json:"nodeSelector,omitempty"`
}

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
	// State indicates status of ClusterPolicy
	State State `json:"state"`
	// Namespace indicates a namespace in which the operator and driver are installed
	Namespace string `json:"namespace,omitempty"`
	// Conditions is a list of conditions representing the NVIDIADriver's current state.
	Conditions []metav1.Condition `json:"conditions"`
}

//+kubebuilder:object:root=true
//+kubebuilder:subresource:status

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

// GetNodeSelector returns node selector labels for NVIDIA driver installation
func (d *NVIDIADriver) GetNodeSelector() map[string]string {
	ns := d.Spec.NodeSelector
	if ns == nil {
		ns = make(map[string]string, 0)
		// If no node selector is specified then the driver is deployed
		// on all GPU nodes by default
		ns["nvidia.com/gpu.present"] = "true"
	}
	return ns
}
