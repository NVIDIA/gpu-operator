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

package k8s

const (
	// GPUNodeSelector selects nodes with NVIDIA GPUs present.
	GPUNodeSelector = "nvidia.com/gpu.present=true"

	// DriverLabelSelector selects all driver pods/daemonsets in both ClusterPolicy
	// and NVIDIADriver modes via the standard Kubernetes component label.
	DriverLabelSelector = "app.kubernetes.io/component=nvidia-driver"

	// DriverContainerName is the container name for the NVIDIA driver in driver pods.
	DriverContainerName = "nvidia-driver-ctr"
)
