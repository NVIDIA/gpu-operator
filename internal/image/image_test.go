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

package image

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestImagePath(t *testing.T) {
	const envName = "TEST_IMAGE_PATH_ENV"

	testCases := []struct {
		description string
		repository  string
		image       string
		version     string
		envValue    string // when non-empty, envName is set to this value
		expected    string
		expectError bool
	}{
		{
			description: "repository, image and tag version resolve to repo/image:tag",
			repository:  "nvcr.io/nvidia",
			image:       "gpu-operator",
			version:     "v1.0.0",
			expected:    "nvcr.io/nvidia/gpu-operator:v1.0.0",
		},
		{
			description: "sha256 version is joined with @ instead of :",
			repository:  "nvcr.io/nvidia",
			image:       "gpu-operator",
			version:     "sha256:abc123",
			expected:    "nvcr.io/nvidia/gpu-operator@sha256:abc123",
		},
		{
			description: "empty repository and version with image set returns image as-is (kbld case)",
			repository:  "",
			image:       "nvcr.io/nvidia/gpu-operator@sha256:abc123",
			version:     "",
			expected:    "nvcr.io/nvidia/gpu-operator@sha256:abc123",
		},
		{
			description: "CR image path takes priority over env",
			repository:  "nvcr.io/nvidia",
			image:       "gpu-operator",
			version:     "v1.0.0",
			envValue:    "from-env/gpu-operator:v9.9.9",
			expected:    "nvcr.io/nvidia/gpu-operator:v1.0.0",
		},
		{
			description: "falls back to env when CR values are empty",
			repository:  "",
			image:       "",
			version:     "",
			envValue:    "from-env/gpu-operator:v2.0.0",
			expected:    "from-env/gpu-operator:v2.0.0",
		},
		{
			description: "errors when neither CR nor env provide an image path",
			repository:  "",
			image:       "",
			version:     "",
			expectError: true,
		},
		{
			description: "repository and image with empty version still builds a CR path with trailing colon",
			repository:  "nvcr.io/nvidia",
			image:       "gpu-operator",
			version:     "",
			expected:    "nvcr.io/nvidia/gpu-operator:",
		},
	}

	for _, tc := range testCases {
		t.Run(tc.description, func(t *testing.T) {
			if tc.envValue != "" {
				t.Setenv(envName, tc.envValue)
			}

			path, err := ImagePath(tc.repository, tc.image, tc.version, envName)

			if tc.expectError {
				require.Error(t, err)
				assert.Empty(t, path)
				return
			}

			require.NoError(t, err)
			assert.Equal(t, tc.expected, path)
		})
	}
}
