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

package v1

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestImagePath(t *testing.T) {
	t.Run("valid spec builds repo/image:tag", func(t *testing.T) {
		path, err := ImagePath(&DriverSpec{Repository: "nvcr.io/nvidia", Image: "driver", Version: "535"})
		require.NoError(t, err)
		assert.Equal(t, "nvcr.io/nvidia/driver:535", path)
	})

	t.Run("sha256 version builds a digest reference", func(t *testing.T) {
		path, err := ImagePath(&DriverSpec{Repository: "nvcr.io/nvidia", Image: "driver", Version: "sha256:abc"})
		require.NoError(t, err)
		assert.Equal(t, "nvcr.io/nvidia/driver@sha256:abc", path)
	})

	t.Run("kbld passthrough when repository and version are empty", func(t *testing.T) {
		path, err := ImagePath(&DriverSpec{Image: "nvcr.io/nvidia/driver@sha256:abc"})
		require.NoError(t, err)
		assert.Equal(t, "nvcr.io/nvidia/driver@sha256:abc", path)
	})

	t.Run("incomplete spec (empty repository) is rejected", func(t *testing.T) {
		// A partial CR spec must fail fast; an env value must not rescue it.
		t.Setenv("DRIVER_IMAGE", "from-env/driver:v9")
		path, err := ImagePath(&DriverSpec{Image: "driver", Version: "535"})
		require.Error(t, err)
		assert.Empty(t, path)
		assert.ErrorContains(t, err, "invalid image specification")
	})

	t.Run("incomplete spec (empty image) is rejected", func(t *testing.T) {
		path, err := ImagePath(&DriverSpec{Repository: "nvcr.io/nvidia", Version: "535"})
		require.Error(t, err)
		assert.Empty(t, path)
		assert.ErrorContains(t, err, "invalid image specification")
	})

	t.Run("unsupported spec type errors", func(t *testing.T) {
		path, err := ImagePath("not-a-spec")
		require.Error(t, err)
		assert.Empty(t, path)
		assert.ErrorContains(t, err, "invalid type to construct image path")
	})
}
