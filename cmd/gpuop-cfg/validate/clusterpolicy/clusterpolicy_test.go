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

package clusterpolicy

import (
	"context"
	"io"
	"os"
	"path/filepath"
	"slices"
	"testing"

	"github.com/sirupsen/logrus"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/urfave/cli/v3"
	"sigs.k8s.io/yaml"

	v1 "github.com/NVIDIA/gpu-operator/api/nvidia/v1"
)

func TestNewCommand(t *testing.T) {
	cmd := NewCommand(logrus.New())

	require.NotNil(t, cmd)
	assert.Equal(t, "clusterpolicy", cmd.Name)
	assert.NotEmpty(t, cmd.Usage)

	idx := slices.IndexFunc(cmd.Flags, func(f cli.Flag) bool {
		return slices.Contains(f.Names(), "input")
	})
	require.GreaterOrEqual(t, idx, 0)

	inputFlag, ok := cmd.Flags[idx].(*cli.StringFlag)
	require.True(t, ok)
	assert.Contains(t, inputFlag.Names(), "input")
	assert.NotEmpty(t, inputFlag.Usage)
	assert.Equal(t, "-", inputFlag.Value)
}

func TestCommandRun(t *testing.T) {
	clearImagePathEnvVars(t)

	clusterPolicyYAMLWithResolvableImages, err := yaml.Marshal(&v1.ClusterPolicy{Spec: *newClusterPolicySpecWithResolvableImages(t)})
	require.NoError(t, err)

	testCases := []struct {
		description           string
		inputFileContents     string
		expectedErrorMessages []string
	}{
		{
			description:           "malformed clusterpolicy yaml",
			inputFileContents:     "\tnot: : valid: yaml",
			expectedErrorMessages: []string{"failed to load clusterpolicy spec"},
		},
		{
			description:           "no images in the clusterpolicy or the environment",
			inputFileContents:     "",
			expectedErrorMessages: []string{"failed to validate images", "DRIVER_IMAGE"},
		},
		{
			description:       "every validated image resolves",
			inputFileContents: string(clusterPolicyYAMLWithResolvableImages),
		},
	}

	for _, tc := range testCases {
		t.Run(tc.description, func(t *testing.T) {
			inputPath := filepath.Join(t.TempDir(), "clusterpolicy.yaml")
			require.NoError(t, os.WriteFile(inputPath, []byte(tc.inputFileContents), 0o600))

			cmd := NewCommand(logrus.New())
			cmd.Writer = io.Discard
			cmd.ErrWriter = io.Discard

			err := cmd.Run(context.Background(), []string{"clusterpolicy", "--input", inputPath})
			if len(tc.expectedErrorMessages) == 0 {
				require.NoError(t, err)
				return
			}
			for _, expectedErrorMessage := range tc.expectedErrorMessages {
				require.ErrorContains(t, err, expectedErrorMessage)
			}
		})
	}
}
