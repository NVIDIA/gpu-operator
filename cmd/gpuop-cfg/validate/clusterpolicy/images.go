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

package clusterpolicy

import (
	"context"
	"fmt"

	"github.com/regclient/regclient"
	"github.com/regclient/regclient/types/ref"

	v1 "github.com/NVIDIA/gpu-operator/api/nvidia/v1"
)

var client = regclient.New()

func validateImages(ctx context.Context, spec *v1.ClusterPolicySpec) error {
	// Driver
	path, err := v1.ImagePath(&spec.Driver)
	if err != nil {
		return fmt.Errorf("failed to construct the image path: %v", err)
	}
	// For driver, we must append the os-tag
	path += "-ubuntu22.04"

	err = validateImage(ctx, path)
	if err != nil {
		return fmt.Errorf("failed to validate image %s: %v", path, err)
	}

	// Toolkit
	path, err = v1.ImagePath(&spec.Toolkit)
	if err != nil {
		return fmt.Errorf("failed to construct the image path: %v", err)
	}

	err = validateImage(ctx, path)
	if err != nil {
		return fmt.Errorf("failed to validate image %s: %v", path, err)
	}

	// Device Plugin
	path, err = v1.ImagePath(&spec.DevicePlugin)
	if err != nil {
		return fmt.Errorf("failed to construct the image path: %v", err)
	}

	err = validateImage(ctx, path)
	if err != nil {
		return fmt.Errorf("failed to validate image %s: %v", path, err)
	}

	// DCGMExporter
	path, err = v1.ImagePath(&spec.DCGMExporter)
	if err != nil {
		return fmt.Errorf("failed to construct the image path: %v", err)
	}

	err = validateImage(ctx, path)
	if err != nil {
		return fmt.Errorf("failed to validate image %s: %v", path, err)
	}

	// DCGM
	path, err = v1.ImagePath(&spec.DCGM)
	if err != nil {
		return fmt.Errorf("failed to construct the image path: %v", err)
	}

	err = validateImage(ctx, path)
	if err != nil {
		return fmt.Errorf("failed to validate image %s: %v", path, err)
	}

	// GPUFeatureDiscovery
	path, err = v1.ImagePath(&spec.GPUFeatureDiscovery)
	if err != nil {
		return fmt.Errorf("failed to construct the image path: %v", err)
	}

	err = validateImage(ctx, path)
	if err != nil {
		return fmt.Errorf("failed to validate image %s: %v", path, err)
	}

	// MIGManager
	path, err = v1.ImagePath(&spec.MIGManager)
	if err != nil {
		return fmt.Errorf("failed to construct the image path: %v", err)
	}

	err = validateImage(ctx, path)
	if err != nil {
		return fmt.Errorf("failed to validate image %s: %v", path, err)
	}

	// GPUDirectStorage
	path, err = v1.ImagePath(spec.GPUDirectStorage)
	if err != nil {
		return fmt.Errorf("failed to construct the image path: %v", err)
	}
	// For GDS driver, we must append the os-tag
	path += "-ubuntu22.04"

	err = validateImage(ctx, path)
	if err != nil {
		return fmt.Errorf("failed to validate image %s: %v", path, err)
	}

	// VFIOManager
	path, err = v1.ImagePath(&spec.VFIOManager)
	if err != nil {
		return fmt.Errorf("failed to construct the image path: %v", err)
	}

	err = validateImage(ctx, path)
	if err != nil {
		return fmt.Errorf("failed to validate image %s: %v", path, err)
	}

	// SandboxDevicePlugin
	path, err = v1.ImagePath(&spec.SandboxDevicePlugin)
	if err != nil {
		return fmt.Errorf("failed to construct the image path: %v", err)
	}

	err = validateImage(ctx, path)
	if err != nil {
		return fmt.Errorf("failed to validate image %s: %v", path, err)
	}

	// VGPUDeviceManager
	path, err = v1.ImagePath(&spec.VGPUDeviceManager)
	if err != nil {
		return fmt.Errorf("failed to construct the image path: %v", err)
	}

	err = validateImage(ctx, path)
	if err != nil {
		return fmt.Errorf("failed to validate image %s: %v", path, err)
	}

	return nil
}

func validateImage(ctx context.Context, path string) error {
	ref, err := ref.New(path)
	if err != nil {
		return fmt.Errorf("failed to construct an image reference: %v", err)
	}

	_, err = client.ManifestGet(ctx, ref)
	if err != nil {
		return fmt.Errorf("failed to get image manifest: %v", err)
	}

	return nil
}
