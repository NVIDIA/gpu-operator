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

package consts

/*
  This package contains constants used throughout the projects and does not fall into a particular package
*/

const (
	// Note: if a different logger is used than zap (operator-sdk default), these values would probably need to change.
	LogLevelError = iota - 2
	LogLevelWarning
	LogLevelInfo
	LogLevelDebug
)

const (
	StateLabel      = "nvidia.com/gpu-operator.state"
	GPUPresentLabel = "nvidia.com/gpu.present"

	// Docker runtime
	Docker = "docker"
	// CRIO runtime
	CRIO = "crio"
	// Containerd runtime
	Containerd = "containerd"

	// OpenshiftNamespace indicates the main namespace of an  Openshift cluster
	OpenshiftNamespace = "openshift"

	OcpDriverToolkitVersionLabel        = "openshift.driver-toolkit.rhcos"
	OcpDriverToolkitIdentificationLabel = "openshift.driver-toolkit"
	NfdOSTreeVersionLabelKey            = "feature.node.kubernetes.io/system-os_release.OSTREE_VERSION"

	// NvidiaAnnotationHashKey indicates annotation name for last applied hash by gpu-operator
	NvidiaAnnotationHashKey = "nvidia.com/last-applied-hash"

	// VGPULicensingConfigMountPath indicates target mount path for vGPU licensing configuration file
	VGPULicensingConfigMountPath = "/drivers/gridd.conf"
	// VGPULicensingFileName is the vGPU licensing configuration filename
	VGPULicensingFileName = "gridd.conf"
	// NLSClientTokenMountPath indicates the target mount path for NLS client config token file (.tok)
	NLSClientTokenMountPath = "/drivers/ClientConfigToken/client_configuration_token.tok"
	// NLSClientTokenFileName is the NLS client config token filename
	NLSClientTokenFileName = "client_configuration_token.tok"
	// VGPUTopologyConfigMountPath indicates target mount path for vGPU topology daemon configuration file
	VGPUTopologyConfigMountPath = "/etc/nvidia/nvidia-topologyd.conf"
	// VGPUTopologyConfigFileName is the vGPU topology daemon configuration filename
	VGPUTopologyConfigFileName = "nvidia-topologyd.conf"

	// NVIDIADriverControllerIndexKey provides quick lookups for DaemonSets owned by an NVIDIADriver instance
	NVIDIADriverControllerIndexKey = "metadata.nvidiadriver.controller"

	// MinimumGDSVersionForOpenRM indicates the minimum GDS version that is supported only with OpenRM driver
	MinimumGDSVersionForOpenRM = "v2.17.5"
)
