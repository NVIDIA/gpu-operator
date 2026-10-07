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

package clusterpolicy

// Common app labels for GPU operator operands
const (
	DriverAppLabel       = "nvidia-driver-daemonset"
	ToolkitAppLabel      = "nvidia-container-toolkit-daemonset"
	DevicePluginAppLabel = "nvidia-device-plugin-daemonset"
	DCGMAppLabel         = "nvidia-dcgm"
	DCGMExporterAppLabel = "nvidia-dcgm-exporter"
	GFDAppLabel          = "gpu-feature-discovery"
	ValidatorAppLabel    = "nvidia-operator-validator"
	MIGManagerAppLabel   = "nvidia-mig-manager"
)

// Common service names
const (
	DCGMServiceName = "nvidia-dcgm"
)

// MIG-related constants
const (
	MIGCapableLabel    = "nvidia.com/mig.capable"
	MIGConfigLabelPath = "/metadata/labels/nvidia.com~1mig.config"

	// DefaultMIGConfigSingle is the fallback uniform profile when auto-detection
	// cannot determine the correct single-strategy profile.
	DefaultMIGConfigSingle = "all-1g.10gb"

	// DefaultMIGConfigMixed is the fallback balanced profile when auto-detection
	// cannot determine the correct mixed-strategy profile.
	DefaultMIGConfigMixed = "all-balanced"
)
