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

package loadid

import (
	"os"
	"path/filepath"
	"syscall"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestRead(t *testing.T) {
	dir := t.TempDir()
	module := filepath.Join(dir, "nvidia")

	_, ok, err := Read(module)
	require.NoError(t, err)
	require.False(t, ok)

	require.NoError(t, os.Mkdir(module, 0o700))
	l, ok, err := Read(module)
	require.NoError(t, err)
	require.True(t, ok)

	fi, err := os.Stat(module)
	require.NoError(t, err)
	require.Equal(t, fi.Sys().(*syscall.Stat_t).Ino, l)
	require.NotZero(t, l)
}
