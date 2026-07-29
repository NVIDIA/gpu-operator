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
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestOptionsLoad(t *testing.T) {
	const validManifest = `apiVersion: operators.coreos.com/v1alpha1
kind: ClusterServiceVersion
metadata:
  name: gpu-operator
`

	t.Run("valid manifest", func(t *testing.T) {
		path := filepath.Join(t.TempDir(), "csv.yaml")
		require.NoError(t, os.WriteFile(path, []byte(validManifest), 0o600))

		spec, err := (options{input: path}).load()

		require.NoError(t, err)
		assert.Equal(t, "gpu-operator", spec.Name)
	})

	t.Run("missing file", func(t *testing.T) {
		path := filepath.Join(t.TempDir(), "does-not-exist.yaml")

		_, err := (options{input: path}).load()

		require.ErrorContains(t, err, "failed to read file")
	})

	t.Run("malformed yaml", func(t *testing.T) {
		path := filepath.Join(t.TempDir(), "malformed.yaml")
		require.NoError(t, os.WriteFile(path, []byte("\tnot: : valid: yaml"), 0o600))

		_, err := (options{input: path}).load()

		require.ErrorContains(t, err, "failed to unmarshal spec")
	})

	t.Run("empty file", func(t *testing.T) {
		path := filepath.Join(t.TempDir(), "empty.yaml")
		require.NoError(t, os.WriteFile(path, []byte(""), 0o600))

		spec, err := (options{input: path}).load()

		require.NoError(t, err)
		assert.Empty(t, spec.Name)
	})
}

func TestOptionsGetContentsStdin(t *testing.T) {
	r, w, err := os.Pipe()
	require.NoError(t, err)

	orig := os.Stdin
	os.Stdin = r
	t.Cleanup(func() { os.Stdin = orig })

	go func() {
		_, _ = w.Write([]byte("hello"))
		_ = w.Close()
	}()

	got, err := (options{input: "-"}).getContents()

	require.NoError(t, err)
	assert.Equal(t, "hello", string(got))
}
