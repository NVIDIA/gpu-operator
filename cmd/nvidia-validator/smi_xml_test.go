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

package main

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

func writeTestFile(t *testing.T, root, name, content string) string {
	t.Helper()
	path := filepath.Join(root, name)
	require.NoError(t, os.MkdirAll(filepath.Dir(path), 0755))
	require.NoError(t, os.WriteFile(path, []byte(content), 0700)) // #nosec G306 -- Executable test fixtures live in a private temporary directory.
	return path
}

// The subtests are cumulative: each adds a file to driverRoot.
func TestNvidiaSMICommand(t *testing.T) {
	driverRoot := t.TempDir()
	t.Run("missing installation", func(t *testing.T) {
		_, err := nvidiaSMICommand(t.Context(), driverRoot, "-q", "-x")
		require.Error(t, err)
	})
	binary := writeTestFile(t, driverRoot, "usr/bin/nvidia-smi", "executable")
	t.Run("missing library", func(t *testing.T) {
		_, err := nvidiaSMICommand(t.Context(), driverRoot, "-q", "-x")
		require.Error(t, err)
	})
	library := writeTestFile(t, driverRoot, "usr/lib64/libnvidia-ml.so.1", "library")
	t.Run("custom container driver root", func(t *testing.T) {
		t.Setenv("LD_PRELOAD", "existing.so")
		cmd, err := nvidiaSMICommand(t.Context(), driverRoot, "-q", "-x")
		require.NoError(t, err)
		require.Equal(t, []string{binary, "-q", "-x"}, cmd.Args)
		require.Contains(t, cmd.Env, "LD_PRELOAD="+library+":existing.so")
	})
}

func TestSMIXMLReaderIsHostDriver(t *testing.T) {
	dir := t.TempDir()
	for name, tc := range map[string]struct {
		content   string
		want      bool
		wantError string
	}{
		"host driver":      {content: "IS_HOST_DRIVER=true\nNVIDIA_DRIVER_ROOT=/\n", want: true},
		"container driver": {content: "NVIDIA_DRIVER_ROOT=/run/nvidia/driver\nIS_HOST_DRIVER=false\n"},
		"key missing":      {content: "NVIDIA_DRIVER_ROOT=/\n", wantError: "IS_HOST_DRIVER not found"},
		"invalid value":    {content: "IS_HOST_DRIVER=maybe\n", wantError: "parse IS_HOST_DRIVER"},
		"empty":            {wantError: "IS_HOST_DRIVER not found"},
	} {
		t.Run(name, func(t *testing.T) {
			reader := &smiXMLReader{statusFile: writeTestFile(t, dir, name, tc.content)}
			got, err := reader.isHostDriver()
			if tc.wantError != "" {
				require.ErrorContains(t, err, tc.wantError)
				return
			}
			require.NoError(t, err)
			require.Equal(t, tc.want, got)
		})
	}
	t.Run("absent file", func(t *testing.T) {
		_, err := (&smiXMLReader{statusFile: filepath.Join(dir, "absent")}).isHostDriver()
		require.ErrorIs(t, err, errDriverNotReady)
	})
}

// The reader reads the status file that driver validation writes.
func TestSMIXMLReaderReadsDriverValidationStatusFile(t *testing.T) {
	original := outputDirFlag
	t.Cleanup(func() { outputDirFlag = original })
	outputDirFlag = t.TempDir()
	reader := &smiXMLReader{statusFile: filepath.Join(outputDirFlag, driverStatusFile)}
	for _, isHostDriver := range []bool{true, false} {
		require.NoError(t, (&Driver{}).createStatusFile(driverInfo{isHostDriver: isHostDriver, driverRoot: "/run/nvidia/driver"}))
		got, err := reader.isHostDriver()
		require.NoError(t, err)
		require.Equal(t, isHostDriver, got)
	}
}
