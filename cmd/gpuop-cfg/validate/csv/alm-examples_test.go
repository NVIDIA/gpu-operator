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
	"testing"

	"github.com/operator-framework/api/pkg/operators/v1alpha1"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestValidateALMExample(t *testing.T) {
	testCases := []struct {
		description string
		annotations map[string]string
		expectError bool
		errContains string
	}{
		{
			description: "first item with Kind ClusterPolicy returns nil",
			annotations: map[string]string{
				"alm-examples": `[{"kind":"ClusterPolicy","apiVersion":"nvidia.com/v1","spec":{}}]`,
			},
			expectError: false,
		},
		{
			description: "Kind ClusterPolicy with wrong apiVersion returns nil (apiVersion is not checked)",
			annotations: map[string]string{
				"alm-examples": `[{"kind":"ClusterPolicy","apiVersion":"example.com/v99","spec":{}}]`,
			},
			expectError: false,
		},
		{
			description: "multi-entry with ClusterPolicy first ignores later entries and returns nil",
			annotations: map[string]string{
				"alm-examples": `[{"kind":"ClusterPolicy","apiVersion":"nvidia.com/v1","spec":{}},{"kind":"SomethingElse","apiVersion":"nvidia.com/v1"}]`,
			},
			expectError: false,
		},
		{
			description: "malformed JSON returns an unmarshal error",
			annotations: map[string]string{
				"alm-examples": `{not valid json`,
			},
			expectError: true,
			errContains: "invalid character",
		},
		{
			description: "missing alm-examples annotation returns an unmarshal error on empty string",
			annotations: map[string]string{},
			expectError: true,
			errContains: "unexpected end of JSON input",
		},
		{
			description: "nil annotations returns an unmarshal error on empty string",
			annotations: nil,
			expectError: true,
			errContains: "unexpected end of JSON input",
		},
		{
			description: "empty alm-examples annotation returns an unmarshal error on empty string",
			annotations: map[string]string{
				"alm-examples": "",
			},
			expectError: true,
			errContains: "unexpected end of JSON input",
		},
		{
			description: "empty list returns 'no example clusterpolicy found'",
			annotations: map[string]string{
				"alm-examples": `[]`,
			},
			expectError: true,
			errContains: "no example clusterpolicy found",
		},
		{
			description: "JSON null returns 'no example clusterpolicy found'",
			annotations: map[string]string{
				"alm-examples": "null",
			},
			expectError: true,
			errContains: "no example clusterpolicy found",
		},
		{
			description: "JSON object instead of array returns an unmarshal error",
			annotations: map[string]string{
				"alm-examples": `{"kind":"ClusterPolicy","apiVersion":"nvidia.com/v1"}`,
			},
			expectError: true,
			errContains: "cannot unmarshal object",
		},
		{
			description: "first item with Kind != ClusterPolicy returns 'invalid example clusterpolicy'",
			annotations: map[string]string{
				"alm-examples": `[{"kind":"NotAClusterPolicy","apiVersion":"nvidia.com/v1"}]`,
			},
			expectError: true,
			errContains: "invalid example clusterpolicy",
		},
		{
			description: "multi-entry with ClusterPolicy not first returns 'invalid example clusterpolicy'",
			annotations: map[string]string{
				"alm-examples": `[{"kind":"SomethingElse","apiVersion":"nvidia.com/v1"},{"kind":"ClusterPolicy","apiVersion":"nvidia.com/v1","spec":{}}]`,
			},
			expectError: true,
			errContains: "invalid example clusterpolicy",
		},
	}

	for _, tc := range testCases {
		t.Run(tc.description, func(t *testing.T) {
			csv := &v1alpha1.ClusterServiceVersion{}
			csv.Annotations = tc.annotations

			err := validateALMExample(csv)

			if !tc.expectError {
				require.NoError(t, err)
				return
			}

			assert.ErrorContains(t, err, tc.errContains)
		})
	}
}
