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
	"github.com/stretchr/testify/require"
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

func TestValidateImages_InvalidRelatedImage(t *testing.T) {
	csv := &v1alpha1.ClusterServiceVersion{}
	csv.Spec.RelatedImages = []v1alpha1.RelatedImage{
		{Name: "bad-image", Image: "@@bad::ref"},
	}

	err := validateImages(context.Background(), csv)
	require.ErrorContains(t, err, "failed to validate image bad-image")
	require.ErrorContains(t, err, "failed to construct an image reference")
}
