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

package environment

import (
	"encoding/json"
	"time"
)

// EnvironmentSnapshot captures complete cluster state at certification time.
type EnvironmentSnapshot struct {
	CapturedAt time.Time `json:"capturedAt"`

	Cluster       ClusterInfo    `json:"cluster"`
	Operator      OperatorInfo   `json:"operator"`
	Policy        PolicyInfo     `json:"policy"`
	Drivers       []DriverInfo   `json:"drivers,omitempty"`
	SharingConfig *SharingConfig `json:"sharingConfig,omitempty"`
	Operands      []OperandInfo  `json:"operands"`
	Nodes         []NodeInfo     `json:"nodes"`

	Virtualization *VirtualizationInfo `json:"virtualization,omitempty"`
	OpenShift      *OpenShiftInfo      `json:"openshift,omitempty"`

	Warnings []string `json:"warnings,omitempty"`
}

// DriverInfo contains NVIDIADriver CR information.
type DriverInfo struct {
	Name                 string            `json:"name"`
	State                string            `json:"state"`
	DriverVersion        string            `json:"driverVersion,omitempty"`
	Repository           string            `json:"repository,omitempty"`
	Image                string            `json:"image,omitempty"`
	UsePrecompiled       bool              `json:"usePrecompiled"`
	UseOpenKernelModules bool              `json:"useOpenKernelModules"`
	NodeSelector         map[string]string `json:"nodeSelector,omitempty"`
	Conditions           []PolicyCondition `json:"conditions,omitempty"`
	RawSpec              json.RawMessage   `json:"rawSpec,omitempty"`
}

// ClusterInfo contains Kubernetes cluster metadata.
type ClusterInfo struct {
	KubernetesVersion    string   `json:"kubernetesVersion"`
	KubernetesGitCommit  string   `json:"kubernetesGitCommit"`
	Platform             string   `json:"platform"`
	PlatformVersion      string   `json:"platformVersion,omitempty"`
	TotalNodes           int      `json:"totalNodes"`
	GPUNodeCount         int      `json:"gpuNodeCount"`
	GPUPCINodeCount      int      `json:"gpuPciNodeCount"`
	GPUPCINodes          []string `json:"gpuPciNodes,omitempty"`
	PodSecurityAdmission string   `json:"podSecurityAdmission,omitempty"`
	NvidiaSmiOutput      []string `json:"nvidiaSmiOutput,omitempty"`
}

// OperatorInfo contains GPU Operator deployment information.
type OperatorInfo struct {
	Namespace     string            `json:"namespace"`
	Version       string            `json:"version"`
	Image         string            `json:"image"`
	PodName       string            `json:"podName"`
	PodPhase      string            `json:"podPhase"`
	PodReady      bool              `json:"podReady"`
	HelmRelease   string            `json:"helmRelease,omitempty"`
	Replicas      int32             `json:"replicas"`
	ReadyReplicas int32             `json:"readyReplicas"`
	Args          []string          `json:"args,omitempty"`
	Env           map[string]string `json:"env,omitempty"`
}

// PolicyInfo contains ClusterPolicy information.
type PolicyInfo struct {
	Name       string            `json:"name"`
	State      string            `json:"state"`
	SpecHash   string            `json:"specHash"`
	Config     PolicyConfig      `json:"config"`
	Conditions []PolicyCondition `json:"conditions,omitempty"`
	RawSpec    json.RawMessage   `json:"rawSpec,omitempty"`
}

// PolicyCondition represents a ClusterPolicy status condition.
type PolicyCondition struct {
	Type    string `json:"type"`
	Status  string `json:"status"`
	Reason  string `json:"reason,omitempty"`
	Message string `json:"message,omitempty"`
}

// PolicyConfig contains typed ClusterPolicy configuration.
type PolicyConfig struct {
	// Driver configuration
	DriverEnabled         bool   `json:"driverEnabled"`
	UseNvidiaDriverCRD    bool   `json:"useNvidiaDriverCRD"`
	DriverVersion         string `json:"driverVersion,omitempty"`
	DriverRepository      string `json:"driverRepository,omitempty"`
	DriverImage           string `json:"driverImage,omitempty"`
	UsePrecompiledDrivers bool   `json:"usePrecompiledDrivers"`
	UseOpenKernelModules  bool   `json:"useOpenKernelModules"`
	DriverInstallDir      string `json:"driverInstallDir,omitempty"`
	LicensingConfigured   bool   `json:"licensingConfigured"`

	// Toolkit configuration
	ToolkitEnabled    bool   `json:"toolkitEnabled"`
	ToolkitInstallDir string `json:"toolkitInstallDir,omitempty"`

	// Core operands
	DevicePluginEnabled bool `json:"devicePluginEnabled"`
	DCGMExporterEnabled bool `json:"dcgmExporterEnabled"`
	DCGMEnabled         bool `json:"dcgmEnabled"`
	GFDEnabled          bool `json:"gfdEnabled"`
	MIGManagerEnabled   bool `json:"migManagerEnabled"`
	ValidatorEnabled    bool `json:"validatorEnabled"`

	// Optional features
	CDIEnabled           bool `json:"cdiEnabled"`
	GDSEnabled           bool `json:"gdsEnabled"`
	GDRCopyEnabled       bool `json:"gdrcopyEnabled"`
	FabricManagerEnabled bool `json:"fabricManagerEnabled"`
	VFIOManagerEnabled   bool `json:"vfioManagerEnabled"`
	VGPUManagerEnabled   bool `json:"vgpuManagerEnabled"`
	CCManagerEnabled     bool `json:"ccManagerEnabled"`

	// Sandbox workloads
	SandboxWorkloadsEnabled bool   `json:"sandboxWorkloadsEnabled"`
	SandboxDefaultWorkload  string `json:"sandboxDefaultWorkload,omitempty"`

	// Sharing configuration
	TimeSlicingConfigName string `json:"timeSlicingConfigName,omitempty"`
	MIGConfigName         string `json:"migConfigName,omitempty"`
}

// SharingConfig contains time-slicing and MIG configuration.
type SharingConfig struct {
	TimeSlicing *TimeSlicingConfig `json:"timeSlicing,omitempty"`
	MIG         *MIGConfig         `json:"mig,omitempty"`
}

// TimeSlicingConfig contains time-slicing ConfigMap data.
type TimeSlicingConfig struct {
	ConfigMapName string                        `json:"configMapName"`
	Profiles      map[string]TimeSlicingProfile `json:"profiles,omitempty"`
}

// TimeSlicingProfile defines a time-slicing profile.
type TimeSlicingProfile struct {
	Replicas        int  `json:"replicas"`
	RenameResources bool `json:"renameResources,omitempty"`
}

// MIGConfig contains MIG ConfigMap data.
type MIGConfig struct {
	ConfigMapName string                 `json:"configMapName"`
	Profiles      map[string][]MIGDevice `json:"profiles,omitempty"`
}

// MIGDevice defines a MIG device configuration.
type MIGDevice struct {
	Profile string `json:"profile"`
	Count   int    `json:"count"`
}

// Operand app label constants.
const (
	OperandDriver              = "nvidia-driver-daemonset"
	OperandToolkit             = "nvidia-container-toolkit-daemonset"
	OperandDevicePlugin        = "nvidia-device-plugin-daemonset"
	OperandDCGMExporter        = "nvidia-dcgm-exporter"
	OperandDCGM                = "nvidia-dcgm"
	OperandGFD                 = "gpu-feature-discovery"
	OperandMIGManager          = "nvidia-mig-manager"
	OperandVFIOManager         = "nvidia-vfio-manager"
	OperandSandboxDevicePlugin = "nvidia-sandbox-device-plugin-daemonset"
	OperandVGPUManager         = "nvidia-vgpu-manager-daemonset"
	OperandCCManager           = "nvidia-cc-manager"
	OperandValidator           = "nvidia-operator-validator"
	OperandFabricManager       = "nvidia-fabric-manager"
	OperandGDS                 = "nvidia-fs"
	OperandGDRCopy             = "nvidia-gdrcopy"
	OperandKataManager         = "nvidia-kata-manager"
	OperandNFDWorker           = "gpu-operator-node-feature-discovery-worker"
	OperandMPSControlDaemon    = "nvidia-device-plugin-mps-control-daemon"
)

// allOperandLabels contains all known operand app labels.
var allOperandLabels = []string{
	OperandDriver,
	OperandToolkit,
	OperandDevicePlugin,
	OperandDCGMExporter,
	OperandDCGM,
	OperandGFD,
	OperandMIGManager,
	OperandVFIOManager,
	OperandSandboxDevicePlugin,
	OperandVGPUManager,
	OperandCCManager,
	OperandValidator,
	OperandFabricManager,
	OperandGDS,
	OperandGDRCopy,
	OperandKataManager,
	OperandNFDWorker,
	OperandMPSControlDaemon,
}

// AllOperandLabels returns all known operand app labels.
func AllOperandLabels() []string {
	return allOperandLabels
}

// OperandInfo contains information about a GPU Operator operand.
type OperandInfo struct {
	Name           string           `json:"name"`
	AppLabel       string           `json:"appLabel"`
	Enabled        bool             `json:"enabled"`
	Image          string           `json:"image,omitempty"`
	ImageTag       string           `json:"imageTag,omitempty"`
	DesiredCount   int32            `json:"desiredCount"`
	ReadyCount     int32            `json:"readyCount"`
	AvailableCount int32            `json:"availableCount"`
	UpdatedCount   int32            `json:"updatedCount"`
	InitContainers []ContainerInfo  `json:"initContainers,omitempty"`
	Pods           []OperandPodInfo `json:"pods,omitempty"`
}

// ContainerInfo contains container metadata.
type ContainerInfo struct {
	Name  string `json:"name"`
	Image string `json:"image"`
}

// OperandPodInfo contains status of an operand pod.
type OperandPodInfo struct {
	Name   string `json:"name"`
	Node   string `json:"node"`
	Phase  string `json:"phase"`
	Ready  bool   `json:"ready"`
	Reason string `json:"reason,omitempty"`
}

// NodeInfo contains node information.
type NodeInfo struct {
	Name             string `json:"name"`
	Status           string `json:"status"`
	Roles            string `json:"roles"`
	Age              string `json:"age"`
	KubeletVersion   string `json:"kubeletVersion"`
	ContainerRuntime string `json:"containerRuntime"`
	OSImage          string `json:"osImage"`
	KernelVersion    string `json:"kernelVersion"`
	Architecture     string `json:"architecture"`
	OperatingSystem  string `json:"operatingSystem"`

	// Addresses
	InternalIP string `json:"internalIP,omitempty"`
	ExternalIP string `json:"externalIP,omitempty"`
	Hostname   string `json:"hostname,omitempty"`

	// GPU hardware
	GPUProduct             string `json:"gpuProduct"`
	GPUCount               int    `json:"gpuCount"`
	GPUMemoryMB            int    `json:"gpuMemoryMB"`
	GPUFamily              string `json:"gpuFamily,omitempty"`
	GPUMachine             string `json:"gpuMachine,omitempty"`
	ComputeCapabilityMajor int    `json:"computeCapabilityMajor"`
	ComputeCapabilityMinor int    `json:"computeCapabilityMinor"`

	// Driver and CUDA versions
	DriverVersion string `json:"driverVersion"`
	CUDAVersion   string `json:"cudaVersion"`

	// MIG
	MIGCapable  bool   `json:"migCapable"`
	MIGStrategy string `json:"migStrategy,omitempty"`

	// MPS
	MPSCapable bool `json:"mpsCapable"`

	// Sharing
	SharingStrategy string `json:"sharingStrategy,omitempty"`
	GPUReplicas     int    `json:"gpuReplicas,omitempty"`

	// vGPU
	VGPUPresent bool `json:"vgpuPresent"`

	// Resources
	AllocatableGPUs int `json:"allocatableGPUs"`
	CapacityGPUs    int `json:"capacityGPUs"`

	// Scheduling
	HasGPUTaint bool              `json:"hasGpuTaint"`
	Taints      []TaintInfo       `json:"taints,omitempty"`
	Conditions  map[string]string `json:"conditions,omitempty"`

	// Deploy states
	DeployStates map[string]string `json:"deployStates,omitempty"`

	// GFD metadata
	GFDTimestamp string `json:"gfdTimestamp,omitempty"`

	// Recent events
	Events []EventInfo `json:"events,omitempty"`
}

// EventInfo contains a Kubernetes event.
type EventInfo struct {
	Type     string `json:"type"`
	Reason   string `json:"reason"`
	Message  string `json:"message"`
	Count    int32  `json:"count,omitempty"`
	LastSeen string `json:"lastSeen,omitempty"`
}

// TaintInfo contains node taint information.
type TaintInfo struct {
	Key    string `json:"key"`
	Value  string `json:"value,omitempty"`
	Effect string `json:"effect"`
}

// VirtualizationInfo contains KubeVirt/virtualization information.
type VirtualizationInfo struct {
	KubeVirtPresent       bool   `json:"kubevirtPresent"`
	KubeVirtVersion       string `json:"kubevirtVersion,omitempty"`
	KubeVirtTargetVersion string `json:"kubevirtTargetVersion,omitempty"`
	HyperConvergedPresent bool   `json:"hyperconvergedPresent"`
	HyperConvergedVersion string `json:"hyperconvergedVersion,omitempty"`
}

// OpenShiftInfo contains OpenShift-specific information.
type OpenShiftInfo struct {
	Version                  string   `json:"version"`
	Channel                  string   `json:"channel,omitempty"`
	ClusterID                string   `json:"clusterId,omitempty"`
	DriverToolkitImage       string   `json:"driverToolkitImage,omitempty"`
	ProxyHTTP                string   `json:"proxyHttp,omitempty"`
	ProxyHTTPS               string   `json:"proxyHttps,omitempty"`
	ProxyNoProxy             string   `json:"proxyNoProxy,omitempty"`
	InternalRegistryHostname string   `json:"internalRegistryHostname,omitempty"`
	Machines                 []string `json:"machines,omitempty"`
}
