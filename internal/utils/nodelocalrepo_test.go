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

package utils

import (
	"testing"

	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
)

func TestValidateNodeLocalRepoPaths(t *testing.T) {
	testCases := []struct {
		description string
		input       []string
		expected    []string
		errContains string
	}{
		{
			description: "nil input",
			input:       nil,
			expected:    []string{},
		},
		{
			description: "empty input",
			input:       []string{},
			expected:    []string{},
		},
		{
			description: "single valid path",
			input:       []string{"/opt/local-packages"},
			expected:    []string{"/opt/local-packages"},
		},
		{
			description: "duplicates removed and result sorted",
			input:       []string{"/b", "/a", "/b"},
			expected:    []string{"/a", "/b"},
		},
		{
			description: "relative path rejected",
			input:       []string{"opt/local-packages"},
			errContains: "not an absolute path",
		},
		{
			description: "empty string rejected",
			input:       []string{""},
			errContains: "not an absolute path",
		},
		{
			description: "trailing slash rejected with suggestion",
			input:       []string{"/opt/local-packages/"},
			errContains: `not a clean path (did you mean "/opt/local-packages"?)`,
		},
		{
			description: "double slash rejected",
			input:       []string{"//opt/pkgs"},
			errContains: "not a clean path",
		},
		{
			description: "parent traversal rejected",
			input:       []string{"/opt/../etc"},
			errContains: "not a clean path",
		},
		{
			description: "host root rejected",
			input:       []string{"/"},
			errContains: "host root filesystem",
		},
	}

	for _, tc := range testCases {
		t.Run(tc.description, func(t *testing.T) {
			actual, err := ValidateNodeLocalRepoPaths(tc.input)
			if tc.errContains != "" {
				require.Error(t, err)
				require.Contains(t, err.Error(), tc.errContains)
				require.Nil(t, actual)
				return
			}
			require.NoError(t, err)
			require.Equal(t, tc.expected, actual)
		})
	}
}

func TestNodeLocalRepoVolumes(t *testing.T) {
	volumes, mounts, err := NodeLocalRepoVolumes([]string{"/srv/pkgs", "/opt/local-packages"})
	require.NoError(t, err)
	require.Len(t, volumes, 2)
	require.Len(t, mounts, 2)

	// names are assigned over the sorted list
	require.Equal(t, "node-local-repo-0", volumes[0].Name)
	require.Equal(t, "/opt/local-packages", volumes[0].HostPath.Path)
	require.Equal(t, "node-local-repo-1", volumes[1].Name)
	require.Equal(t, "/srv/pkgs", volumes[1].HostPath.Path)

	for i, v := range volumes {
		require.NotNil(t, v.HostPath, "volume %d must be a hostPath volume", i)
		require.NotNil(t, v.HostPath.Type)
		require.Equal(t, corev1.HostPathDirectory, *v.HostPath.Type)
	}

	for i, m := range mounts {
		require.Equal(t, volumes[i].Name, m.Name)
		require.True(t, m.ReadOnly, "mount %d must be read-only", i)
		// the container path must equal the host path so that file:// URIs resolve identically
		require.Equal(t, volumes[i].HostPath.Path, m.MountPath)
	}
}

func TestNodeLocalRepoVolumesIsDeterministic(t *testing.T) {
	volumesA, mountsA, err := NodeLocalRepoVolumes([]string{"/b", "/a"})
	require.NoError(t, err)
	volumesB, mountsB, err := NodeLocalRepoVolumes([]string{"/a", "/b"})
	require.NoError(t, err)

	require.Equal(t, volumesA, volumesB)
	require.Equal(t, mountsA, mountsB)
}

func TestNodeLocalRepoVolumesEmpty(t *testing.T) {
	volumes, mounts, err := NodeLocalRepoVolumes(nil)
	require.NoError(t, err)
	require.Empty(t, volumes)
	require.Empty(t, mounts)
}

func TestNodeLocalRepoVolumesPropagatesValidationError(t *testing.T) {
	volumes, mounts, err := NodeLocalRepoVolumes([]string{"relative/path"})
	require.Error(t, err)
	require.Contains(t, err.Error(), "not an absolute path")
	require.Nil(t, volumes)
	require.Nil(t, mounts)
}
