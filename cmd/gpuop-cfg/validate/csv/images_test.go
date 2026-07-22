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

package csv

import (
	"context"
	"testing"

	"github.com/operator-framework/api/pkg/operators/v1alpha1"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
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

// TestValidateImages_InvalidRelatedImage verifies that validateImages returns an
// error from the very first RelatedImage (which is iterated before any
// deployment/registry access), so no network call is made and no nil-index
// panic occurs on the empty DeploymentSpecs slice.
func TestValidateImages_InvalidRelatedImage(t *testing.T) {
	csv := &v1alpha1.ClusterServiceVersion{}
	csv.Spec.RelatedImages = []v1alpha1.RelatedImage{
		{Name: "bad-image", Image: "@@bad::ref"},
	}

	err := validateImages(context.Background(), csv)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "failed to validate image bad-image")
	assert.Contains(t, err.Error(), "failed to construct an image reference")
}
