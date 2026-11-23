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
	"context"
	"slices"
	"strings"

	"github.com/onsi/ginkgo/v2"

	nvidiav1 "github.com/NVIDIA/gpu-operator/api/nvidia/v1"
	"github.com/NVIDIA/gpu-operator/tests/e2e/framework"
	e2eoperator "github.com/NVIDIA/gpu-operator/tests/e2e/framework/operator"
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

var DefaultRestartCheckedOperandSelectors = []string{
	helpers.DriverComponentSelector,
	AppSelector("nvidia-container-toolkit-daemonset"),
	AppSelector("nvidia-device-plugin-daemonset"),
	AppSelector("gpu-feature-discovery"),
}

// EnabledOperandSelectors drops the selectors of operands the ClusterPolicy disables, so
// clusters without some operands (for example Mokka, which has no driver or toolkit
// container) are not expected to run their pods.
func EnabledOperandSelectors(ctx context.Context, clients e2eoperator.Clients, operandSelectors []string) []string {
	ginkgo.GinkgoHelper()
	clusterPolicy, err := clients.ClusterPolicies.Get(ctx, e2eoperator.ClusterPolicyName)
	framework.ExpectNoError(err, "failed to get ClusterPolicy %s", e2eoperator.ClusterPolicyName)
	return slices.DeleteFunc(slices.Clone(operandSelectors), func(selector string) bool {
		return !isOperandEnabled(&clusterPolicy.Spec, selector)
	})
}

func isOperandEnabled(spec *nvidiav1.ClusterPolicySpec, selector string) bool {
	switch selector {
	case helpers.DriverComponentSelector:
		return spec.Driver.IsEnabled()
	case AppSelector("nvidia-container-toolkit-daemonset"):
		return spec.Toolkit.IsEnabled()
	case AppSelector("nvidia-device-plugin-daemonset"):
		return spec.DevicePlugin.IsEnabled()
	case AppSelector("nvidia-dcgm-exporter"):
		return spec.DCGMExporter.IsEnabled()
	case AppSelector("gpu-feature-discovery"):
		return spec.GPUFeatureDiscovery.IsEnabled()
	default:
		return true
	}
}

func AppSelector(app string) string {
	return "app=" + app
}

func operandNameFromSelector(selector string) string {
	_, labelValue, hasLabelValue := strings.Cut(selector, "=")
	if !hasLabelValue {
		return selector
	}
	return labelValue
}
