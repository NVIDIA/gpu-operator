/*
 * Copyright (c) NVIDIA CORPORATION.  All rights reserved.
 *
 * Licensed under the Apache License, Version 2.0 (the "License");
 * you may not use this file except in compliance with the License.
 * You may obtain a copy of the License at
 *
 *     http://www.apache.org/licenses/LICENSE-2.0
 *
 * Unless required by applicable law or agreed to in writing, software
 * distributed under the License is distributed on an "AS IS" BASIS,
 * WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
 * See the License for the specific language governing permissions and
 * limitations under the License.
 */

package v1

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestDriverSpecIsNVIDIADriverCRDEnabled(t *testing.T) {
	driverEnabled := true
	driverDisabled := false
	crdEnabled := true
	crdDisabled := false

	tests := []struct {
		name          string
		driverEnabled *bool
		crdEnabled    *bool
		expected      bool
	}{
		{
			name:       "driver enabled by default and CRD enabled",
			crdEnabled: &crdEnabled,
			expected:   true,
		},
		{
			name:       "CRD disabled",
			crdEnabled: &crdDisabled,
			expected:   false,
		},
		{
			name:          "driver disabled",
			driverEnabled: &driverDisabled,
			crdEnabled:    &crdEnabled,
			expected:      false,
		},
		{
			name:          "driver explicitly enabled and CRD enabled",
			driverEnabled: &driverEnabled,
			crdEnabled:    &crdEnabled,
			expected:      true,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			driverSpec := DriverSpec{
				Enabled:            tc.driverEnabled,
				UseNvidiaDriverCRD: tc.crdEnabled,
			}

			require.Equal(t, tc.expected, driverSpec.IsNVIDIADriverCRDEnabled())
		})
	}

	t.Run("nil driver spec", func(t *testing.T) {
		var driverSpec *DriverSpec
		require.False(t, driverSpec.IsNVIDIADriverCRDEnabled())
	})
}
