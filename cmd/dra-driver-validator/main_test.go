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

package main

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestWriteDriverReady(t *testing.T) {
	testCases := []struct {
		description string
		info        driverInfo
		expected    string
	}{
		{
			description: "host driver",
			info:        getDriverInfo(true, "/", "/run/nvidia/driver", "/run/nvidia/driver"),
			expected: "NVIDIA_DRIVER_ROOT=/\n" +
				"DRIVER_ROOT_CTR_PATH=/host\n",
		},
		{
			description: "containerized driver",
			info:        getDriverInfo(false, "/", "/run/nvidia/driver", "/run/nvidia/driver"),
			expected: "NVIDIA_DRIVER_ROOT=/run/nvidia/driver\n" +
				"DRIVER_ROOT_CTR_PATH=/run/nvidia/driver\n",
		},
	}

	for _, tc := range testCases {
		t.Run(tc.description, func(t *testing.T) {
			outputDirFlag = t.TempDir()
			require.NoError(t, writeDriverReady(tc.info))

			data, err := os.ReadFile(filepath.Join(outputDirFlag, driverStatusFile))
			require.NoError(t, err)
			require.Equal(t, tc.expected, string(data))
		})
	}
}

func TestGetDriverInfo(t *testing.T) {
	t.Run("host driver", func(t *testing.T) {
		// Host driver: NVIDIA_DRIVER_ROOT is the host root, read in-container via /host.
		info := getDriverInfo(true, "/", "/run/nvidia/driver", "/run/nvidia/driver")
		require.Equal(t, "/", info.driverRoot)
		require.Equal(t, "/host", info.driverRootCtrPath)
	})

	t.Run("containerized driver", func(t *testing.T) {
		// Containerized driver: emitted in-container path is the configured ctr path
		// (the DRA stack mounts the driver at the same path on host and in-container),
		// not nvidia-validator's fixed /driver-root.
		info := getDriverInfo(false, "/", "/run/nvidia/driver", "/run/nvidia/driver")
		require.Equal(t, "/run/nvidia/driver", info.driverRoot)
		require.Equal(t, "/run/nvidia/driver", info.driverRootCtrPath)
	})
}

func TestFindNvidiaBinaries(t *testing.T) {
	driverRoot := t.TempDir()
	writeFile(t, filepath.Join(driverRoot, "usr/bin/nvidia-smi"), "fake nvidia-smi")
	writeFile(t, filepath.Join(driverRoot, "usr/lib64/libnvidia-ml.so.1"), "fake libnvidia-ml")

	r := root(driverRoot)

	smiPath, err := r.getNvidiaSMIPath()
	require.NoError(t, err)
	require.Equal(t, filepath.Join(driverRoot, "usr/bin/nvidia-smi"), smiPath)

	libPath, err := r.getDriverLibraryPath()
	require.NoError(t, err)
	require.Equal(t, filepath.Join(driverRoot, "usr/lib64/libnvidia-ml.so.1"), libPath)

	// An empty root finds neither.
	empty := root(t.TempDir())
	_, err = empty.getNvidiaSMIPath()
	require.Error(t, err)
	_, err = empty.getDriverLibraryPath()
	require.Error(t, err)
}

// TestNvidiaSMIArgsAlwaysVersion guards the passthrough-safety invariant: the
// validator must only ever invoke `nvidia-smi --version`, never full nvidia-smi.
func TestNvidiaSMIArgsAlwaysVersion(t *testing.T) {
	t.Run("container probe", func(t *testing.T) {
		cmd := nvidiaSMIVersionCommand("/driver-root/usr/bin/nvidia-smi")
		require.Equal(t, []string{"/driver-root/usr/bin/nvidia-smi", "--version"}, cmd.Args)
	})

	t.Run("host probe via chroot", func(t *testing.T) {
		cmd := nvidiaSMIVersionCommand("chroot", "/host", "nvidia-smi")
		require.Equal(t, []string{"chroot", "/host", "nvidia-smi", "--version"}, cmd.Args)
		require.Equal(t, "--version", cmd.Args[len(cmd.Args)-1])
	})
}

func writeFile(t *testing.T, path, content string) {
	t.Helper()
	require.NoError(t, os.MkdirAll(filepath.Dir(path), 0750))
	require.NoError(t, os.WriteFile(path, []byte(content), 0600))
}
