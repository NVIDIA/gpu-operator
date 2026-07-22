/**
# Copyright (c), NVIDIA CORPORATION.  All rights reserved.
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

package images

import (
	"fmt"

	v1 "github.com/NVIDIA/gpu-operator/api/nvidia/v1"
)

type OperandImage struct {
	Name  string
	Image string
}

func FromClusterPolicy(spec *v1.ClusterPolicySpec) ([]OperandImage, error) {
	type operand struct {
		name string
		spec interface{}
	}

	operands := []operand{
		{"Driver", &spec.Driver},
		{"Toolkit", &spec.Toolkit},
		{"DevicePlugin", &spec.DevicePlugin},
		{"DCGMExporter", &spec.DCGMExporter},
		{"DCGM", &spec.DCGM},
		{"GPUFeatureDiscovery", &spec.GPUFeatureDiscovery},
		{"MIGManager", &spec.MIGManager},
		{"GPUDirectStorage", spec.GPUDirectStorage},
		{"VFIOManager", &spec.VFIOManager},
		{"SandboxDevicePlugin", &spec.SandboxDevicePlugin},
		{"VGPUDeviceManager", &spec.VGPUDeviceManager},
	}

	var images []OperandImage
	for _, op := range operands {
		path, err := v1.ImagePath(op.spec)
		if err != nil {
			return nil, fmt.Errorf("failed to construct image path for %s: %v", op.name, err)
		}
		images = append(images, OperandImage{Name: op.name, Image: path})
	}

	return images, nil
}
