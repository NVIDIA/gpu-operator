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
	"fmt"
	"io"
	"os"
	"path/filepath"
	"slices"
	"testing"

	"github.com/sirupsen/logrus"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/urfave/cli/v3"
)

func TestNewCommand(t *testing.T) {
	cmd := NewCommand(logrus.New())

	require.NotNil(t, cmd)
	assert.Equal(t, "csv", cmd.Name)
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
	const csvYAMLTemplate = `apiVersion: operators.coreos.com/v1alpha1
kind: ClusterServiceVersion
metadata:
  annotations:
    alm-examples: '%[1]s'
spec:
  relatedImages:
    - name: gpu-operator-image
      image: "%[2]s"
  install:
    spec:
      deployments:
        - name: gpu-operator
          spec:
            template:
              spec:
                containers:
                  - name: gpu-operator
                    image: "%[3]s"
                    env:
                      - name: DRIVER_IMAGE
                        value: "%[4]s"
                      - name: DRIVER_VERSION
                        value: "` + malformedImageRef + `"
`

	const almExamplesWithClusterPolicy = `[{"apiVersion":"nvidia.com/v1","kind":"ClusterPolicy","metadata":{"name":"gpu-cluster-policy"}}]`

	relatedImage := newOCILayoutRef(t, "related") + ":related"
	operatorImage := newOCILayoutRef(t, "operator") + ":operator"
	operandImage := newOCILayoutRef(t, "operand") + ":operand"

	testCases := []struct {
		description          string
		inputFileContents    string
		expectedErrorMessage string
	}{
		{
			description:          "malformed csv yaml",
			inputFileContents:    "\tnot: : valid: yaml",
			expectedErrorMessage: "failed to load csv yaml",
		},
		{
			description:       "every validated image resolves",
			inputFileContents: fmt.Sprintf(csvYAMLTemplate, almExamplesWithClusterPolicy, relatedImage, operatorImage, operandImage),
		},
		{
			description:          "operator image does not resolve",
			inputFileContents:    fmt.Sprintf(csvYAMLTemplate, almExamplesWithClusterPolicy, relatedImage, malformedImageRef, operandImage),
			expectedErrorMessage: "failed to validate images",
		},
		{
			description:          "alm-examples annotation holds no clusterpolicy",
			inputFileContents:    fmt.Sprintf(csvYAMLTemplate, "[]", relatedImage, operatorImage, operandImage),
			expectedErrorMessage: "failed to validate alm-example",
		},
	}

	for _, tc := range testCases {
		t.Run(tc.description, func(t *testing.T) {
			inputPath := filepath.Join(t.TempDir(), "csv.yaml")
			require.NoError(t, os.WriteFile(inputPath, []byte(tc.inputFileContents), 0o600))

			cmd := NewCommand(logrus.New())
			cmd.Writer = io.Discard
			cmd.ErrWriter = io.Discard

			err := cmd.Run(context.Background(), []string{"csv", "--input", inputPath})
			if tc.expectedErrorMessage == "" {
				require.NoError(t, err)
				return
			}
			require.ErrorContains(t, err, tc.expectedErrorMessage)
		})
	}
}
