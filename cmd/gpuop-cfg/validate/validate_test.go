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

package validate

import (
	"testing"

	"github.com/sirupsen/logrus"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNewCommand(t *testing.T) {
	cmd := NewCommand(logrus.New())

	require.NotNil(t, cmd)
	assert.Equal(t, "validate", cmd.Name)
	assert.NotEmpty(t, cmd.Usage)

	// the 'validate' command wires the 'csv' and 'clusterpolicy' subcommands
	require.Len(t, cmd.Commands, 2)

	names := []string{}
	for _, sub := range cmd.Commands {
		names = append(names, sub.Name)
	}
	assert.Contains(t, names, "csv")
	assert.Contains(t, names, "clusterpolicy")
}
