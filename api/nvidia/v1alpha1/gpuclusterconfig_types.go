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
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	nvidiav1 "github.com/NVIDIA/gpu-operator/api/nvidia/v1"
)

const (
	GPUClusterConfigCRDName = "GPUClusterConfig"
)

const (
	// Ignored marks a duplicate GPUClusterConfig that the singleton controller does not reconcile.
	Ignored State = "ignored"
)

// NOTE: json tags are required.  Any new fields you add must have json tags for the fields to be serialized.

// GPUClusterConfigSpec defines the desired state of GPUClusterConfig, the DRA-based
// software-enablement stack. Unlike ClusterPolicy, it does not manage the NVIDIA driver
// or the device-plugin; the driver is installed separately (host-installed or via an
// NVIDIADriver CR) and GPUClusterConfig waits for driver readiness before proceeding.
type GPUClusterConfigSpec struct {
	// DRADriver defines the spec for the NVIDIA DRA driver stack (gpus + computeDomains).
	DRADriver DRADriverSpec `json:"draDriver"`

	// DCGM defines the spec for the standalone NVIDIA DCGM hostengine. Disabled by default;
	// when disabled, dcgm-exporter uses its embedded nv-hostengine. NOTE: the reused enabled
	// field carries no server-side default and its IsEnabled() treats nil as enabled, so the
	// controller must default nil enabled to disabled here.
	DCGM *nvidiav1.DCGMSpec `json:"dcgm,omitempty"`

	// DCGMExporter defines the spec for NVIDIA DCGM Exporter. Enabled by default, but the
	// reused enabled field carries no server-side default; the controller defaults nil enabled.
	DCGMExporter *nvidiav1.DCGMExporterSpec `json:"dcgmExporter,omitempty"`

	// GFD defines the spec for the standalone GPU Feature Discovery operand. Enabled by default,
	// but the reused enabled field carries no server-side default; the controller defaults nil enabled.
	GFD *nvidiav1.GPUFeatureDiscoverySpec `json:"gfd,omitempty"`

	// HostPaths defines the host paths used in host-path volumes for various components.
	HostPaths nvidiav1.HostPathsSpec `json:"hostPaths,omitempty"`

	// Daemonsets defines the common configuration applied to all DaemonSets deployed
	// by the GPUClusterConfig controller.
	Daemonsets nvidiav1.DaemonsetsSpec `json:"daemonsets,omitempty"`
}

// DRADriverSpec defines the spec for the NVIDIA DRA driver stack. There is no top-level
// enabled toggle; enablement is per capability (gpus / computeDomains).
type DRADriverSpec struct {
	// NVIDIA DRA driver image repository
	// +kubebuilder:validation:Optional
	Repository string `json:"repository,omitempty"`

	// NVIDIA DRA driver image name
	// +kubebuilder:validation:Pattern=[a-zA-Z0-9\-]+
	Image string `json:"image,omitempty"`

	// NVIDIA DRA driver image tag
	// +kubebuilder:validation:Optional
	Version string `json:"version,omitempty"`

	// Image pull policy
	// +kubebuilder:validation:Optional
	ImagePullPolicy string `json:"imagePullPolicy,omitempty"`

	// Image pull secrets
	// +kubebuilder:validation:Optional
	ImagePullSecrets []string `json:"imagePullSecrets,omitempty"`

	// FeatureGates is a map of feature gate names to a boolean enabling or disabling each.
	// It is rendered as the FEATURE_GATES environment variable on the DRA driver containers.
	// +kubebuilder:validation:Optional
	FeatureGates map[string]bool `json:"featureGates,omitempty"`

	// GPUs configures the gpu.nvidia.com capability of the DRA driver.
	GPUs DRADriverGPUsSpec `json:"gpus,omitempty"`

	// ComputeDomains configures the compute-domain capability of the DRA driver.
	ComputeDomains DRADriverComputeDomainsSpec `json:"computeDomains,omitempty"`
}

// IsGPUsEnabled returns true if the gpus capability of the DRA driver is enabled.
func (d *DRADriverSpec) IsGPUsEnabled() bool {
	return d.GPUs.Enabled != nil && *d.GPUs.Enabled
}

// IsComputeDomainsEnabled returns true if the computeDomains capability of the DRA driver is enabled.
func (d *DRADriverSpec) IsComputeDomainsEnabled() bool {
	return d.ComputeDomains.Enabled != nil && *d.ComputeDomains.Enabled
}

// DRADriverGPUsSpec configures the gpus capability of the DRA driver. It maps onto the
// gpus container of the upstream kubelet-plugin DaemonSet.
type DRADriverGPUsSpec struct {
	// Enabled indicates if the gpus capability of the DRA driver is enabled.
	// +kubebuilder:default=true
	Enabled *bool `json:"enabled,omitempty"`

	// KubeletPlugin configures the kubelet-plugin workload for the gpus capability.
	KubeletPlugin DRADriverKubeletPluginSpec `json:"kubeletPlugin,omitempty"`
}

// DRADriverComputeDomainsSpec configures the computeDomains capability of the DRA driver.
// The kubeletPlugin maps onto the computeDomains container of the upstream kubelet-plugin
// DaemonSet; the controller is a separate Deployment.
type DRADriverComputeDomainsSpec struct {
	// Enabled indicates if the computeDomains capability of the DRA driver is enabled.
	// +kubebuilder:default=true
	Enabled *bool `json:"enabled,omitempty"`

	// Controller configures the compute-domain controller Deployment.
	Controller DRADriverControllerSpec `json:"controller,omitempty"`

	// KubeletPlugin configures the kubelet-plugin workload for the computeDomains capability.
	KubeletPlugin DRADriverKubeletPluginSpec `json:"kubeletPlugin,omitempty"`
}

// DRADriverKubeletPluginSpec defines configuration for a DRA driver kubelet-plugin container.
// Per-component scheduling fields augment/override the shared daemonsets defaults for this
// workload. The gpus and computeDomains kubelet-plugin blocks map onto the two containers of
// a single kubelet-plugin DaemonSet, so the renderer reconciles pod-level scheduling when
// both blocks set it.
type DRADriverKubeletPluginSpec struct {
	// Optional: List of environment variables
	// +kubebuilder:validation:Optional
	Env []nvidiav1.EnvVar `json:"env,omitempty"`

	// Optional: Define resources requests and limits for the kubelet-plugin container
	// +kubebuilder:validation:Optional
	Resources *nvidiav1.ResourceRequirements `json:"resources,omitempty"`

	// HealthcheckPort is the port running a gRPC health service checked by a livenessProbe.
	// Set to a negative value to disable the service and the probe.
	// +kubebuilder:validation:Optional
	HealthcheckPort *int32 `json:"healthcheckPort,omitempty"`

	// +kubebuilder:validation:Optional
	// PriorityClassName for the kubelet-plugin DaemonSet pods
	PriorityClassName string `json:"priorityClassName,omitempty"`

	// +kubebuilder:validation:Optional
	// NodeSelector for the kubelet-plugin DaemonSet pods
	NodeSelector map[string]string `json:"nodeSelector,omitempty"`

	// +kubebuilder:validation:Optional
	// Tolerations for the kubelet-plugin DaemonSet pods
	Tolerations []corev1.Toleration `json:"tolerations,omitempty"`

	// +kubebuilder:validation:Optional
	// Affinity rules for the kubelet-plugin DaemonSet pods
	Affinity *corev1.Affinity `json:"affinity,omitempty"`
}

// DRADriverControllerSpec defines configuration for the compute-domain controller Deployment.
// As a Deployment (not a DaemonSet) it carries its own scheduling configuration rather than
// inheriting the shared daemonsets defaults.
type DRADriverControllerSpec struct {
	// Optional: List of environment variables
	// +kubebuilder:validation:Optional
	Env []nvidiav1.EnvVar `json:"env,omitempty"`

	// Optional: Define resources requests and limits for the controller container
	// +kubebuilder:validation:Optional
	Resources *nvidiav1.ResourceRequirements `json:"resources,omitempty"`

	// +kubebuilder:validation:Optional
	// PriorityClassName for the controller Deployment pods
	PriorityClassName string `json:"priorityClassName,omitempty"`

	// +kubebuilder:validation:Optional
	// NodeSelector for the controller Deployment pods
	NodeSelector map[string]string `json:"nodeSelector,omitempty"`

	// +kubebuilder:validation:Optional
	// Tolerations for the controller Deployment pods
	Tolerations []corev1.Toleration `json:"tolerations,omitempty"`

	// +kubebuilder:validation:Optional
	// Affinity rules for the controller Deployment pods
	Affinity *corev1.Affinity `json:"affinity,omitempty"`
}

// GPUClusterConfigStatus defines the observed state of GPUClusterConfig
type GPUClusterConfigStatus struct {
	// +kubebuilder:validation:Enum=ignored;ready;notReady;disabled
	// State indicates the status of the GPUClusterConfig instance
	State State `json:"state"`
	// Namespace indicates the namespace in which the operator and operands are installed
	Namespace string `json:"namespace,omitempty"`
	// Conditions is a list of conditions representing the GPUClusterConfig's current state.
	Conditions []metav1.Condition `json:"conditions,omitempty"`
}

// +genclient
// +genclient:nonNamespaced
//+kubebuilder:object:root=true
//+kubebuilder:subresource:status
//+kubebuilder:resource:scope=Cluster,shortName={"gcc"}
//+kubebuilder:printcolumn:name="Status",type=string,JSONPath=`.status.state`,priority=0
//+kubebuilder:printcolumn:name="Age",type=string,JSONPath=`.metadata.creationTimestamp`,priority=0

// GPUClusterConfig is the Schema for the gpuclusterconfigs API
type GPUClusterConfig struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   GPUClusterConfigSpec   `json:"spec,omitempty"`
	Status GPUClusterConfigStatus `json:"status,omitempty"`
}

//+kubebuilder:object:root=true

// GPUClusterConfigList contains a list of GPUClusterConfig
type GPUClusterConfigList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []GPUClusterConfig `json:"items"`
}
