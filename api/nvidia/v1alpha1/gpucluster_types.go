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
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	nvidiav1 "github.com/NVIDIA/gpu-operator/api/nvidia/v1"
)

const (
	GPUClusterCRDName = "GPUCluster"
)

const (
	// Ignored marks a duplicate GPUCluster that the singleton controller does not reconcile.
	Ignored State = "ignored"
)

// NOTE: json tags are required.  Any new fields you add must have json tags for the fields to be serialized.

// GPUClusterSpec defines the desired state of GPUCluster, the DRA-based
// software-enablement stack. Unlike ClusterPolicy, it does not manage the NVIDIA driver
// or the device-plugin; the driver is installed separately (host-installed or via an
// NVIDIADriver CR) and GPUCluster waits for driver readiness before proceeding.
type GPUClusterSpec struct {
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

	// HostPaths defines the host paths used in host-path volumes for various components.
	HostPaths nvidiav1.HostPathsSpec `json:"hostPaths,omitempty"`

	// Daemonsets defines the common configuration applied to all DaemonSets deployed
	// by the GPUCluster controller.
	Daemonsets nvidiav1.DaemonsetsSpec `json:"daemonsets,omitempty"`
}

// DRADriverSpec defines the spec for the NVIDIA DRA driver stack. There is no top-level
// enabled toggle; the gpus capability is always deployed and computeDomains has its own
// enabled field.
type DRADriverSpec struct {
	// NVIDIA DRA driver image repository
	Repository string `json:"repository,omitempty"`

	// NVIDIA DRA driver image name
	// +kubebuilder:validation:Pattern=^[a-zA-Z0-9\-]+$
	Image string `json:"image,omitempty"`

	// NVIDIA DRA driver image tag
	Version string `json:"version,omitempty"`

	// Image pull policy
	ImagePullPolicy string `json:"imagePullPolicy,omitempty"`

	// Image pull secrets
	ImagePullSecrets []string `json:"imagePullSecrets,omitempty"`

	// FeatureGates is a map of feature gate names to a boolean enabling or disabling each.
	// It is rendered as the FEATURE_GATES environment variable on the DRA driver containers.
	FeatureGates map[string]bool `json:"featureGates,omitempty"`

	// GPUs configures the gpu.nvidia.com capability of the DRA driver.
	GPUs DRADriverGPUsSpec `json:"gpus,omitempty"`

	// ComputeDomains configures the compute-domain capability of the DRA driver.
	ComputeDomains DRADriverComputeDomainsSpec `json:"computeDomains,omitempty"`
}

// IsComputeDomainsEnabled returns true if the computeDomains capability of the DRA driver is enabled.
func (d *DRADriverSpec) IsComputeDomainsEnabled() bool {
	return d.ComputeDomains.Enabled != nil && *d.ComputeDomains.Enabled
}

// DRADriverGPUsSpec configures the gpus capability of the DRA driver. It maps onto the
// gpus container of the upstream kubelet-plugin DaemonSet. The capability is always
// deployed; there is no enabled toggle.
type DRADriverGPUsSpec struct {
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

// DRADriverKubeletPluginSpec configures a DRA driver kubelet-plugin container. The gpus and
// computeDomains blocks map onto the two containers of a single kubelet-plugin DaemonSet.
// Scheduling is opinionated and not configurable here.
type DRADriverKubeletPluginSpec struct {
	// Optional: List of environment variables
	Env []nvidiav1.EnvVar `json:"env,omitempty"`

	// Optional: Define resources requests and limits for the kubelet-plugin container
	Resources *nvidiav1.ResourceRequirements `json:"resources,omitempty"`

	// Optional: Configure the container's gRPC health service and its probes
	Healthcheck *DRADriverHealthcheckSpec `json:"healthcheck,omitempty"`
}

// DRADriverHealthcheckSpec configures the gRPC health service of a kubelet-plugin
// container, checked by the startup and liveness probes.
type DRADriverHealthcheckSpec struct {
	// +kubebuilder:default=true
	Enabled *bool `json:"enabled,omitempty"`

	// Defaults to 51516 for the gpus container and 51515 for the computeDomains container.
	// +kubebuilder:validation:Minimum=1
	// +kubebuilder:validation:Maximum=65535
	Port *int32 `json:"port,omitempty"`
}

// DRADriverControllerSpec defines configuration for the compute-domain controller Deployment.
// Scheduling is opinionated and not configurable here.
type DRADriverControllerSpec struct {
	// Optional: List of environment variables
	Env []nvidiav1.EnvVar `json:"env,omitempty"`

	// Optional: Define resources requests and limits for the controller container
	Resources *nvidiav1.ResourceRequirements `json:"resources,omitempty"`
}

// GPUClusterStatus defines the observed state of GPUCluster
type GPUClusterStatus struct {
	// +kubebuilder:validation:Enum=ignored;ready;notReady;disabled
	// State indicates the status of the GPUCluster instance
	State State `json:"state"`
	// Namespace indicates the namespace in which the operator and operands are installed
	Namespace string `json:"namespace,omitempty"`
	// Conditions is a list of conditions representing the GPUCluster's current state.
	Conditions []metav1.Condition `json:"conditions,omitempty"`
}

// +genclient
// +genclient:nonNamespaced
//+kubebuilder:object:root=true
//+kubebuilder:subresource:status
//+kubebuilder:resource:scope=Cluster,shortName={"gc"}
//+kubebuilder:printcolumn:name="Status",type=string,JSONPath=`.status.state`,priority=0
//+kubebuilder:printcolumn:name="Age",type=date,JSONPath=`.metadata.creationTimestamp`,priority=0

// GPUCluster is the Schema for the gpuclusters API
type GPUCluster struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   GPUClusterSpec   `json:"spec,omitempty"`
	Status GPUClusterStatus `json:"status,omitempty"`
}

//+kubebuilder:object:root=true

// GPUClusterList contains a list of GPUCluster
type GPUClusterList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []GPUCluster `json:"items"`
}
