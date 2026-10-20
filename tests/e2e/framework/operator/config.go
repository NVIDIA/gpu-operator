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

package operator

import (
	"cmp"
	"errors"
	"flag"
	"fmt"
)

const (
	TestNamePrefix = "gpu-operator-e2e"
)

type TestContextType struct {
	HelmChart           string
	HelmChartVersion    string
	ValuesFile          string
	OperatorNamespace   string
	OperatorRepository  string
	OperatorImage       string
	OperatorVersion     string
	ValidatorRepository string
	ValidatorImage      string
	ValidatorVersion    string
	ContainerRuntime    string
	TargetDriverVersion string
	LogDir              string
}

var TestContext TestContextType

func RegisterFlags(flags *flag.FlagSet) {
	flags.StringVar(&TestContext.HelmChart, "helm-chart", "", "Helm chart to use")
	flags.StringVar(&TestContext.HelmChartVersion, "helm-chart-version", "", "Helm chart version to use, required when helm-chart is an OCI reference without a tag")
	flags.StringVar(&TestContext.ValuesFile, "values-file", "", "Helm values file to apply before the operator and validator image settings")
	flags.StringVar(&TestContext.OperatorNamespace, "namespace", "gpu-operator", "Namespace name to use for the gpu-operator helm deploys")
	flags.StringVar(&TestContext.OperatorRepository, "operator-repository", "", "GPU Operator image repository to use")
	flags.StringVar(&TestContext.OperatorImage, "operator-image", "", "GPU Operator image name to use")
	flags.StringVar(&TestContext.OperatorVersion, "operator-version", "", "GPU Operator image tag to use")
	flags.StringVar(&TestContext.ValidatorRepository, "validator-repository", "", "GPU Operator Validator image repository to use, defaults to -operator-repository")
	flags.StringVar(&TestContext.ValidatorImage, "validator-image", "", "GPU Operator Validator image name to use, defaults to -operator-image")
	flags.StringVar(&TestContext.ValidatorVersion, "validator-version", "", "GPU Operator Validator image tag to use, defaults to -operator-version")
	flags.StringVar(&TestContext.ContainerRuntime, "container-runtime", "containerd", "Container runtime of the cluster nodes, set as operator.defaultRuntime when migrating to NVIDIADriver")
	flags.StringVar(&TestContext.TargetDriverVersion, "target-driver-version", "", "Driver version to upgrade to in the driver update specs")
	flags.StringVar(&TestContext.LogDir, "log-dir", "", "Directory to write pod logs to when a spec fails, empty disables log capture")
}

func AfterReadingAllFlags(testContext *TestContextType) {
	testContext.ValidatorRepository = cmp.Or(testContext.ValidatorRepository, testContext.OperatorRepository)
	testContext.ValidatorImage = cmp.Or(testContext.ValidatorImage, testContext.OperatorImage)
	testContext.ValidatorVersion = cmp.Or(testContext.ValidatorVersion, testContext.OperatorVersion)
}

func (c *TestContextType) Validate() error {
	requiredFlags := []struct {
		name  string
		value string
	}{
		{"helm-chart", c.HelmChart},
		{"operator-repository", c.OperatorRepository},
		{"operator-image", c.OperatorImage},
		{"operator-version", c.OperatorVersion},
		{"target-driver-version", c.TargetDriverVersion},
	}

	var missingFlagErrs []error
	for _, requiredFlag := range requiredFlags {
		if requiredFlag.value == "" {
			missingFlagErrs = append(missingFlagErrs, fmt.Errorf("-%s must be set", requiredFlag.name))
		}
	}
	return errors.Join(missingFlagErrs...)
}
