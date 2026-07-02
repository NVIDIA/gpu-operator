/*
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
*/

package driverroot

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestFindNvidiaBinaries(t *testing.T) {
	// Returned paths are symlink-resolved, so canonicalize the temp dir
	// (on macOS /tmp is a symlink to /private/tmp).
	driverRoot, err := filepath.EvalSymlinks(t.TempDir())
	require.NoError(t, err)
	writeFile(t, filepath.Join(driverRoot, "usr/bin/nvidia-smi"), "fake nvidia-smi")
	writeFile(t, filepath.Join(driverRoot, "usr/lib64/libnvidia-ml.so.1"), "fake libnvidia-ml")

	r := Root(driverRoot)

	smiPath, err := r.GetNvidiaSMIPath()
	require.NoError(t, err)
	require.Equal(t, filepath.Join(driverRoot, "usr/bin/nvidia-smi"), smiPath)

	libPath, err := r.GetDriverLibraryPath()
	require.NoError(t, err)
	require.Equal(t, filepath.Join(driverRoot, "usr/lib64/libnvidia-ml.so.1"), libPath)

	// An empty root finds neither.
	empty := Root(t.TempDir())
	_, err = empty.GetNvidiaSMIPath()
	require.Error(t, err)
	_, err = empty.GetDriverLibraryPath()
	require.Error(t, err)
}

func writeFile(t *testing.T, path, content string) {
	t.Helper()
	require.NoError(t, os.MkdirAll(filepath.Dir(path), 0750))
	require.NoError(t, os.WriteFile(path, []byte(content), 0600))
}
