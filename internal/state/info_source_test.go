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

package state

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestInfoCatalogAddAndGet(t *testing.T) {
	clusterInfo := struct{ KubernetesVersion string }{KubernetesVersion: "v1.31.0"}
	hostRoot := "/host"

	t.Run("get before add returns nil", func(t *testing.T) {
		catalog := NewInfoCatalog()

		assert.Nil(t, catalog.Get(InfoTypeClusterInfo))
		assert.Nil(t, catalog.Get(InfoTypeHostRoot))
	})

	t.Run("each info type keeps its own source", func(t *testing.T) {
		catalog := NewInfoCatalog()
		catalog.Add(InfoTypeClusterInfo, clusterInfo)
		catalog.Add(InfoTypeHostRoot, hostRoot)

		assert.Equal(t, clusterInfo, catalog.Get(InfoTypeClusterInfo))
		assert.Equal(t, hostRoot, catalog.Get(InfoTypeHostRoot))
	})

	t.Run("adding the same info type again replaces only that source", func(t *testing.T) {
		catalog := NewInfoCatalog()
		catalog.Add(InfoTypeClusterInfo, clusterInfo)
		catalog.Add(InfoTypeHostRoot, hostRoot)

		catalog.Add(InfoTypeClusterInfo, "replacement")

		assert.Equal(t, "replacement", catalog.Get(InfoTypeClusterInfo))
		assert.Equal(t, hostRoot, catalog.Get(InfoTypeHostRoot))
	})
}
