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

// TestImagePath exercises the value-producing paths of ImagePath.
//
// ImagePath has two structural branches:
//   - Branch A (repository=="" && version==""): returns `image` verbatim when
//     set (the kbld/carvel case), otherwise falls back to the env var, then errors.
//   - Branch B (repository OR version non-empty): builds a "repo/image:tag" or
//     "repo/image@digest" string when repository and image are both set (never
//     consulting the env); an incomplete spec (empty repository or image) is
//     rejected with an error (see TestImagePath_InvalidSpec). Digest is selected
//     only when version has the exact, case-sensitive "sha256:" prefix.
func TestImagePath(t *testing.T) {
	const envName = "TEST_IMAGE_PATH_ENV"

	testCases := []struct {
		description string
		repository  string
		image       string
		version     string
		setEnv      bool // when true, envName is set to envValue (possibly "")
		envValue    string
		expected    string
	}{
		// ---- Branch B: tag paths (positive) ----
		{
			description: "repository, image and tag version resolve to repo/image:tag",
			repository:  "nvcr.io/nvidia",
			image:       "gpu-operator",
			version:     "v1.0.0",
			expected:    "nvcr.io/nvidia/gpu-operator:v1.0.0",
		},
		{
			description: "multi-segment repository is preserved",
			repository:  "nvcr.io/nvidia/cloud-native",
			image:       "k8s-driver-manager",
			version:     "v0.7.0",
			expected:    "nvcr.io/nvidia/cloud-native/k8s-driver-manager:v0.7.0",
		},
		// ---- Branch B: digest paths (positive) ----
		{
			description: "sha256 version is joined with @ instead of :",
			repository:  "nvcr.io/nvidia",
			image:       "gpu-operator",
			version:     "sha256:abcdef0123456789",
			expected:    "nvcr.io/nvidia/gpu-operator@sha256:abcdef0123456789",
		},
		{
			description: "sha256 prefix with empty digest still uses @",
			repository:  "nvcr.io/nvidia",
			image:       "gpu-operator",
			version:     "sha256:",
			expected:    "nvcr.io/nvidia/gpu-operator@sha256:",
		},
		{
			description: "version 'sha256' without a colon is treated as a tag, not a digest",
			repository:  "nvcr.io/nvidia",
			image:       "gpu-operator",
			version:     "sha256",
			expected:    "nvcr.io/nvidia/gpu-operator:sha256",
		},
		{
			description: "uppercase SHA256 prefix is case-sensitive and treated as a tag",
			repository:  "nvcr.io/nvidia",
			image:       "gpu-operator",
			version:     "SHA256:abc",
			expected:    "nvcr.io/nvidia/gpu-operator:SHA256:abc",
		},
		// ---- Branch A: kbld image passthrough (positive) ----
		{
			description: "empty repository and version with digest image returns image as-is (kbld case)",
			repository:  "",
			image:       "nvcr.io/nvidia/gpu-operator@sha256:deadbeef",
			version:     "",
			expected:    "nvcr.io/nvidia/gpu-operator@sha256:deadbeef",
		},
		{
			description: "empty repository and version with tag image returns image as-is (kbld case)",
			repository:  "",
			image:       "busybox:1.36",
			version:     "",
			expected:    "busybox:1.36",
		},
		// ---- Priority: CR (branch B) always beats env (positive) ----
		{
			description: "CR tag path takes priority over env",
			repository:  "nvcr.io/nvidia",
			image:       "gpu-operator",
			version:     "v2.0.0",
			setEnv:      true,
			envValue:    "from-env/gpu-operator:v9.9.9",
			expected:    "nvcr.io/nvidia/gpu-operator:v2.0.0",
		},
		{
			description: "partial CR (repository only, empty version) still beats a valid env",
			repository:  "nvcr.io/nvidia",
			image:       "gpu-operator",
			version:     "",
			setEnv:      true,
			envValue:    "from-env/gpu-operator:v9.9.9",
			expected:    "nvcr.io/nvidia/gpu-operator:", // trailing colon, NOT the env value
		},
		// ---- Env fallback (positive): only when repo, version AND image are empty ----
		{
			description: "falls back to env when CR is fully empty",
			repository:  "",
			image:       "",
			version:     "",
			setEnv:      true,
			envValue:    "registry.example.com/op:v3",
			expected:    "registry.example.com/op:v3",
		},
	}

	for _, tc := range testCases {
		t.Run(tc.description, func(t *testing.T) {
			if tc.setEnv {
				t.Setenv(envName, tc.envValue)
			}

			path, err := ImagePath(tc.repository, tc.image, tc.version, envName)

			require.NoError(t, err)
			assert.Equal(t, tc.expected, path)
		})
	}
}

// TestImagePath_Errors covers the only error path: repository, version and image
// are all empty AND the env var provides nothing.
func TestImagePath_Errors(t *testing.T) {
	const envName = "TEST_IMAGE_PATH_ENV_ERR"

	testCases := []struct {
		description string
		setEnv      bool
		envValue    string
	}{
		{
			description: "errors when CR is empty and env is unset",
			setEnv:      false,
		},
		{
			description: "errors when CR is empty and env is explicitly the empty string",
			setEnv:      true,
			envValue:    "",
		},
	}

	for _, tc := range testCases {
		t.Run(tc.description, func(t *testing.T) {
			if tc.setEnv {
				t.Setenv(envName, tc.envValue)
			}

			path, err := ImagePath("", "", "", envName)

			require.Error(t, err)
			assert.Empty(t, path)
			// The error should name both sources and the specific env var checked.
			assert.ErrorContains(t, err, "empty image path provided through both CR and ENV")
			assert.ErrorContains(t, err, envName)
		})
	}
}

// TestImagePath_InvalidSpec covers the validation of an incomplete CR spec: when
// repository or version is set (branch B) but repository or image is empty, the
// only reference that could be built is malformed (leading slash or empty path
// segment), so ImagePath returns an error instead of an invalid image path.
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
	}

	for _, tc := range testCases {
		t.Run(tc.description, func(t *testing.T) {
			// An env value must NOT rescue an incomplete CR spec: the partial
			// spec is a misconfiguration and should fail fast regardless.
			t.Setenv(envName, "from-env/op:v9")

			path, err := ImagePath(tc.repository, tc.image, tc.version, envName)

			require.Error(t, err)
			assert.Empty(t, path)
			assert.ErrorContains(t, err, "invalid image specification")
		})
	}
}

// TestImagePath_ErrorNamesProvidedEnvVar verifies the error message reflects the
// exact env var name passed in, not a hardcoded one.
func TestImagePath_ErrorNamesProvidedEnvVar(t *testing.T) {
	_, err := ImagePath("", "", "", "SOME_CUSTOM_IMAGE_ENV")
	require.Error(t, err)
	assert.ErrorContains(t, err, "SOME_CUSTOM_IMAGE_ENV")
}
