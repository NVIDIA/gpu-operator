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
	"cmp"
	"flag"
	"fmt"
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
	helmChartVersion    string
	valuesFile          string
	namespace           string
	operatorRepository  string
	operatorImage       string
	operatorVersion     string
	validatorRepository string
	validatorImage      string
	validatorVersion    string
	containerRuntime    string
	targetDriverVersion string
	logDir              string
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
	flag.StringVar(&tcfg.helmChartVersion, "helm-chart-version", "", "Helm chart version to use, required when helm-chart is an OCI reference without a tag")
	flag.StringVar(&tcfg.valuesFile, "values-file", "", "Helm values file to apply before the operator and validator image settings")
	flag.StringVar(&tcfg.namespace, "namespace", "gpu-operator", "Namespace name to use for the gpu-operator helm deploys")
	flag.StringVar(&tcfg.operatorRepository, "operator-repository", "", "GPU Operator image repository to use")
	flag.StringVar(&tcfg.operatorImage, "operator-image", "", "GPU Operator image name to use")
	flag.StringVar(&tcfg.operatorVersion, "operator-version", "", "GPU Operator image tag to use")
	flag.StringVar(&tcfg.validatorRepository, "validator-repository", "", "GPU Operator Validator image repository to use, defaults to -operator-repository")
	flag.StringVar(&tcfg.validatorImage, "validator-image", "", "GPU Operator Validator image name to use, defaults to -operator-image")
	flag.StringVar(&tcfg.validatorVersion, "validator-version", "", "GPU Operator Validator image tag to use, defaults to -operator-version")
	flag.StringVar(&tcfg.containerRuntime, "container-runtime", "containerd", "Container runtime of the cluster nodes, set as operator.defaultRuntime when migrating to NVIDIADriver")
	flag.StringVar(&tcfg.targetDriverVersion, "target-driver-version", "", "Driver version to upgrade to in the driver update specs")
	flag.StringVar(&tcfg.logDir, "log-dir", "", "Directory to write pod logs to when a spec fails")

	framework.RegisterClusterFlags(flag.CommandLine)

	flag.Parse()

	tcfg.validatorRepository = cmp.Or(tcfg.validatorRepository, tcfg.operatorRepository)
	tcfg.validatorImage = cmp.Or(tcfg.validatorImage, tcfg.operatorImage)
	tcfg.validatorVersion = cmp.Or(tcfg.validatorVersion, tcfg.operatorVersion)

	os.Exit(m.Run())
}

func requireFlag(value, flagName string) {
	ginkgo.GinkgoHelper()
	if value == "" {
		ginkgo.Fail(fmt.Sprintf("-%s must be set", flagName))
	}
}

var _ = ginkgo.BeforeSuite(func() {
	requireFlag(tcfg.helmChart, "helm-chart")
	requireFlag(tcfg.targetDriverVersion, "target-driver-version")
})

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
