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

package main

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/NVIDIA/gpu-operator/internal/gpuconfig/decision"
	"github.com/NVIDIA/gpu-operator/internal/gpuconfig/loadid"
	"github.com/NVIDIA/gpu-operator/internal/gpuconfig/statefile"
)

// internal/gpuconfig keeps its own copies of these names, because the
// validator's are unexported in package main; this pins them together.
func TestGPUConfigNamesMatchValidator(t *testing.T) {
	require.Equal(t, defaultStatusPath, statefile.DefaultDir)
	require.Equal(t, driverStatusFile, loadid.DriverReadyFile)
	require.Equal(t, defaultStatusPath+"/"+loadid.DriverCtrReadyFile, driverContainerStatusFilePath)
	require.Equal(t, appComponentLabelKey+"=nvidia-driver", loadid.DriverDaemonSetSelector)
	require.Equal(t, toolkitStatusFile, decision.ToolkitReadyFile)
	require.Equal(t, cudaValidatorLabelValue, decision.CUDAValidatorApp)
	require.Equal(t, pluginValidatorLabelValue, decision.PluginValidatorApp)
}
