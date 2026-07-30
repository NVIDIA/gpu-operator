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

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"

	v1 "github.com/NVIDIA/gpu-operator/api/nvidia/v1"
)

func TestValidateImage_InvalidReference(t *testing.T) {
	testCases := []struct {
		description string
		path        string
	}{
		{
			description: "empty reference",
			path:        "",
		},
		{
			description: "malformed reference",
			path:        "@@bad::ref",
		},
	}

	for _, tc := range testCases {
		t.Run(tc.description, func(t *testing.T) {
			err := validateImage(context.Background(), tc.path)
			require.ErrorContains(t, err, "failed to construct an image reference")
		})
	}
}

func TestValidateImages_EmptyDriverSpecImagePathError(t *testing.T) {
	t.Setenv("DRIVER_IMAGE", "")

	spec := &v1.ClusterPolicySpec{}

	err := validateImages(context.Background(), spec)
	require.ErrorContains(t, err, "failed to construct the image path")
}

func TestValidateImages_InvalidDriverImageRefError(t *testing.T) {
	spec := &v1.ClusterPolicySpec{}
	spec.Driver.Image = "@@bad::ref"

	err := validateImages(context.Background(), spec)
	require.ErrorContains(t, err, "failed to validate image")
	require.ErrorContains(t, err, "failed to construct an image reference")
}
