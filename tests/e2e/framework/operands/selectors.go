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

package operands

import (
	"github.com/NVIDIA/gpu-operator/tests/e2e/helpers"
)

var DefaultOperandSelectors = []string{
	helpers.DriverComponentSelector,
	AppSelector("nvidia-container-toolkit-daemonset"),
	AppSelector("nvidia-device-plugin-daemonset"),
	AppSelector("nvidia-dcgm-exporter"),
	AppSelector("gpu-feature-discovery"),
	AppSelector("nvidia-operator-validator"),
}

func AppSelector(app string) string {
	return "app=" + app
}
