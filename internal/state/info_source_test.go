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
	"github.com/stretchr/testify/require"
)

func TestInfoCatalogAddAndGet(t *testing.T) {
	catalog := NewInfoCatalog()
	require.NotNil(t, catalog)

	// Getting an entry that has not been added returns nil.
	assert.Nil(t, catalog.Get(InfoTypeClusterInfo))

	clusterInfo := "some-cluster-info"
	policy := struct{ Name string }{Name: "policy"}

	catalog.Add(InfoTypeClusterInfo, clusterInfo)
	catalog.Add(InfoTypeClusterPolicyCR, policy)

	assert.Equal(t, clusterInfo, catalog.Get(InfoTypeClusterInfo))
	assert.Equal(t, policy, catalog.Get(InfoTypeClusterPolicyCR))

	// Overwriting an existing entry replaces the value.
	catalog.Add(InfoTypeClusterInfo, "updated")
	assert.Equal(t, "updated", catalog.Get(InfoTypeClusterInfo))
}
