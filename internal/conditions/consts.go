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

package conditions

const (
	// Reconciled is the generic reason for successful reconciliation of all states
	Reconciled = "Reconciled"
	// ReconcileFailed is the generic reason for reconciliation failures
	ReconcileFailed = "ReconcileFailed"
	// NFDLabelsMissing indicates that NFD labels for GPU nodes are missing
	NFDLabelsMissing = "NFDLabelsMissing"
	// NoGPUNodes indicates that there are no GPU nodes in the cluster
	NoGPUNodes = "NoGPUNodes"
	// OperandNotReady is the generic reason for any operand pod failures
	OperandNotReady = "OperandNotReady"
	// OperatorMetricsNotReady is the reason for operator metrics state failures
	OperatorMetricsNotReady = "OperatorMetricsNotReady"
	// DriverNotReady indicates that the driver daemonset pods are not ready
	DriverNotReady = "ContainerToolkitNotReady"
	// ContainerToolkitNotReady indicates that the container-toolkit daemonset pods are not ready
	ContainerToolkitNotReady = "ContainerToolkitNotReady"
	// DevicePluginNotReady indicates that the device-plugin daemonset pods are not ready
	DevicePluginNotReady = "DevicePluginNotReady"
	// GPUFeatureDiscoveryNotReady indicates that the gfd daemonset pods are not ready
	GPUFeatureDiscoveryNotReady = "GPUFeatureDiscoveryNotReady"
	// MIGManagerNotReady indicates that the mig manager daemonset pods are not ready
	MIGManagerNotReady = "MIGManagerNotReady"
	// NodeFeatureDiscoveryNotReady indicates that the nfd daemonset pods are not ready
	NodeFeatureDiscoveryNotReady = "NodeFeatureDiscoveryNotReady"
	// VGPUManagerNotReady indicates that the driver daemonset pods are not ready
	VGPUManagerNotReady = "VGPUManagerNotReady"
	// VGPUDeviceManagerNotReady indicates that the vgpu-device-manager daemonset pods are not ready
	VGPUDeviceManagerNotReady = "VGPUDeviceManagerNotReady"
	// KataManagerNotReady indicates that the kata manager daemonset pods are not ready
	KataManagerNotReady = "KataManagerNotReady"
	// VFIOManagerNotReady indicates that the vfio manager daemonset pods are not ready
	VFIOManagerNotReady = "VFIOManagerNotReady"
	// CCManagerNotReady indicates that the cc manager daemonset pods are not ready
	CCManagerNotReady = "CCManagerNotReady"
	// SandboxDevicePluginNotReady indicates that the sandbox device plugin daemonset pod are not ready
	SandboxDevicePluginNotReady = "SandboxDevicePluginNotReady"
	// OperatorValidatorNotReady indicates that the operator validator daemonset pod are not ready
	OperatorValidatorNotReady = "OperatorValidatorNotReady"
	// DCGMExporterNotReady indicates that the dcgm exporter daemonset pods are not ready
	DCGMExporterNotReady = "DCGMExporterNotReady"
	// DCGMNotReady indicates that the dcgm daemonset pods are not ready
	DCGMNotReady = "DCGMNotReady"
	// NodeStatusExporterNotReady indicates that the node-status-exporter daemonset pods are not ready
	NodeStatusExporterNotReady = "NodeStatusExporterNotReady"
)
