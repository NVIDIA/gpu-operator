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
	"github.com/NVIDIA/gpu-operator/cmd/gpuop-cfg/internal/images"
)

func validateImages(ctx context.Context, spec *v1.ClusterPolicySpec) error {
	operandImages, err := images.FromClusterPolicy(spec)
	if err != nil {
		return err
	}

	for _, op := range operandImages {
		path := op.Image
		// For Driver and GPUDirectStorage, we must append the os-tag
		if op.Name == "Driver" || op.Name == "GPUDirectStorage" {
			path += "-ubuntu22.04"
		}
		err = validateImage(ctx, path)
		if err != nil {
			return fmt.Errorf("failed to validate image %s: %v", path, err)
		}
	}

	return nil
}

func validateImage(ctx context.Context, path string) error {
	var client = regclient.New()
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
