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

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	v1 "github.com/NVIDIA/gpu-operator/api/nvidia/v1"
)

// TestValidateImage_InvalidReference only exercises the error path that occurs
// BEFORE any network call: an invalid image reference string causes ref.New to
// fail. The happy path is intentionally not tested since it performs a real
// registry manifest lookup (not hermetic).
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
			require.Error(t, err)
			assert.Contains(t, err.Error(), "failed to construct an image reference")
		})
	}
}

// TestValidateImages_EmptyDriverSpecImagePathError verifies that validateImages
// fails at the very first component (Driver) when no image path can be
// constructed. With a zero-value Driver spec and the DRIVER_IMAGE env var
// cleared, v1.ImagePath returns an error, so validateImages returns before any
// registry access.
func TestValidateImages_EmptyDriverSpecImagePathError(t *testing.T) {
	// Ensure the env-var fallback in imagePath() is not populated so that a
	// zero-value Driver spec deterministically fails to produce an image path.
	t.Setenv("DRIVER_IMAGE", "")

	spec := &v1.ClusterPolicySpec{}

	err := validateImages(context.Background(), spec)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "failed to construct the image path")
}

// TestValidateImages_InvalidDriverImageRefError verifies that validateImages
// fails while validating the Driver image when v1.ImagePath succeeds but the
// resulting reference is invalid. This exercises the validateImage error branch
// for the first component before any registry access.
func TestValidateImages_InvalidDriverImageRefError(t *testing.T) {
	spec := &v1.ClusterPolicySpec{}
	// With Repository and Version empty and Image set, imagePath returns the
	// image verbatim; validateImages then appends the os-tag before building
	// the reference, which is invalid and fails ref.New.
	spec.Driver.Image = "@@bad::ref"

	err := validateImages(context.Background(), spec)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "failed to validate image")
	assert.Contains(t, err.Error(), "failed to construct an image reference")
}
