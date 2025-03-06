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

package e2e

import (
	"flag"
	"os"
	"testing"

	"github.com/onsi/ginkgo/v2"
	"github.com/onsi/ginkgo/v2/types"
	"github.com/onsi/gomega"
	"k8s.io/apimachinery/pkg/util/uuid"
	"k8s.io/klog/v2"

	"github.com/NVIDIA/gpu-operator/tests/e2e/framework"
	e2elog "github.com/NVIDIA/gpu-operator/tests/e2e/framework/logs"
)

type testConfig struct {
	helmChart           string
	namespace           string
	operatorRepository  string
	operatorImage       string
	operatorVersion     string
	validatorRepository string
	validatorImage      string
	validatorVersion    string
}

// runID is a unique identifier of the e2e run.
// Beware that this ID is not the same for all tests in the e2e run, because each Ginkgo node creates it separately.
var runID = uuid.NewUUID()

var tcfg = testConfig{}

func createGinkgoConfig() (types.SuiteConfig, types.ReporterConfig) {
	// fetch the current config
	suiteConfig, reporterConfig := ginkgo.GinkgoConfiguration()
	// Randomize specs as well as suites
	suiteConfig.RandomizeAllSpecs = true
	return suiteConfig, reporterConfig
}

func TestMain(m *testing.M) {
	flag.StringVar(&tcfg.helmChart, "helm-chart", "", "Helm chart to use")
	flag.StringVar(&tcfg.namespace, "namespace", "gpu-operator", "Namespace name to use for the gpu-operator helm deploys")
	flag.StringVar(&tcfg.operatorRepository, "operator-repository", "", "GPU Operator image repository to use")
	flag.StringVar(&tcfg.operatorImage, "operator-image", "", "GPU Operator image name to use")
	flag.StringVar(&tcfg.operatorVersion, "operator-version", "", "GPU Operator image tag to use")
	flag.StringVar(&tcfg.validatorRepository, "validator-repository", "", "GPU Operator Validator image repository to use")
	flag.StringVar(&tcfg.validatorImage, "validator-image", "", "GPU Operator Validator image name to use")
	flag.StringVar(&tcfg.validatorVersion, "validator-version", "", "GPU Operator Validator image tag to use")

	framework.RegisterClusterFlags(flag.CommandLine)

	flag.Parse()

	os.Exit(m.Run())
}

func TestE2E(t *testing.T) {
	e2elog.InitLogs()
	defer e2elog.FlushLogs()
	klog.EnableContextualLogging(true)

	gomega.RegisterFailHandler(ginkgo.Fail)

	// Run tests through the Ginkgo runner with output to console + JUnit for Jenkins
	suiteConfig, reporterConfig := createGinkgoConfig()
	klog.Infof("Starting e2e run %q on Ginkgo node %d", runID, suiteConfig.ParallelProcess)
	ginkgo.RunSpecs(t, "GPU Operator e2e suite", suiteConfig, reporterConfig)
}
