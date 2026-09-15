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

func TestImagePath_ValidSpec(t *testing.T) {
	const envName = "TEST_IMAGE_PATH_VALID"

	t.Run("repository, image and tag version resolve to repo/image:tag", func(t *testing.T) {
		path, err := ImagePath("nvcr.io/nvidia", "gpu-operator", "v1.0.0", envName)
		require.NoError(t, err)
		assert.Equal(t, "nvcr.io/nvidia/gpu-operator:v1.0.0", path)
	})

	t.Run("sha256 version resolves to a digest reference", func(t *testing.T) {
		path, err := ImagePath("nvcr.io/nvidia", "gpu-operator", "sha256:cafe", envName)
		require.NoError(t, err)
		assert.Equal(t, "nvcr.io/nvidia/gpu-operator@sha256:cafe", path)
	})
}

func TestImagePath_InvalidSpec(t *testing.T) {
	const envName = "TEST_IMAGE_PATH_ENV_INVALID"

	testCases := []struct {
		description string
		repository  string
		image       string
		version     string
	}{
		{
			description: "empty repository with tag version",
			repository:  "",
			image:       "gpu-operator",
			version:     "v1.0.0",
		},
		{
			description: "empty repository with digest version",
			repository:  "",
			image:       "gpu-operator",
			version:     "sha256:cafe",
		},
		{
			description: "empty image with repository and version set",
			repository:  "nvcr.io/nvidia",
			image:       "",
			version:     "v1.0.0",
		},
		{
			description: "empty repository and image with tag version",
			repository:  "",
			image:       "",
			version:     "v1.0.0",
		},
		{
			description: "empty repository and image with digest version",
			repository:  "",
			image:       "",
			version:     "sha256:abc",
		},
		{
			description: "empty version with repository and image set",
			repository:  "nvcr.io/nvidia",
			image:       "gpu-operator",
			version:     "",
		},
	}

	for _, tc := range testCases {
		t.Run(tc.description, func(t *testing.T) {
			// An env value must not rescue an incomplete spec.
			t.Setenv(envName, "from-env/op:v9")

			path, err := ImagePath(tc.repository, tc.image, tc.version, envName)

			require.Error(t, err)
			assert.Empty(t, path)
			assert.ErrorContains(t, err, "invalid image specification")
		})
	}
}

func TestImagePath_KbldPassthrough(t *testing.T) {
	const envName = "TEST_IMAGE_PATH_KBLD"

	path, err := ImagePath("", "nvcr.io/nvidia/driver@sha256:cafe", "", envName)
	require.NoError(t, err)
	assert.Equal(t, "nvcr.io/nvidia/driver@sha256:cafe", path)
}

func TestImagePath_EnvFallback(t *testing.T) {
	const envName = "TEST_IMAGE_PATH_ENV_FALLBACK"
	t.Setenv(envName, "from-env/op:v9")

	path, err := ImagePath("", "", "", envName)
	require.NoError(t, err)
	assert.Equal(t, "from-env/op:v9", path)
}

func TestImagePath_EmptyError(t *testing.T) {
	const envName = "TEST_IMAGE_PATH_EMPTY"

	path, err := ImagePath("", "", "", envName)
	require.Error(t, err)
	assert.Empty(t, path)
	assert.ErrorContains(t, err, "empty image path provided through both CR and ENV "+envName)
}
