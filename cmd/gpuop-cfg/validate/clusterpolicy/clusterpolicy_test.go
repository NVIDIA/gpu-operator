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
