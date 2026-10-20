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

	_ "github.com/NVIDIA/gpu-operator/tests/e2e/clusterpolicy"
	"github.com/NVIDIA/gpu-operator/tests/e2e/framework"
	e2elog "github.com/NVIDIA/gpu-operator/tests/e2e/framework/logs"
	e2eoperator "github.com/NVIDIA/gpu-operator/tests/e2e/framework/operator"
)

// runID is a unique identifier of the e2e run.
// Beware that this ID is not the same for all tests in the e2e run, because each Ginkgo node creates it separately.
var runID = uuid.NewUUID()

func createGinkgoConfig() (types.SuiteConfig, types.ReporterConfig) {
	// fetch the current config
	suiteConfig, reporterConfig := ginkgo.GinkgoConfiguration()
	// Randomize specs as well as suites
	suiteConfig.RandomizeAllSpecs = true
	return suiteConfig, reporterConfig
}

func TestMain(m *testing.M) {
	e2eoperator.RegisterFlags(flag.CommandLine)
	framework.RegisterClusterFlags(flag.CommandLine)

	flag.Parse()
	e2eoperator.AfterReadingAllFlags(&e2eoperator.TestContext)

	os.Exit(m.Run())
}

var _ = ginkgo.BeforeSuite(func() {
	if err := e2eoperator.TestContext.Validate(); err != nil {
		ginkgo.Fail(err.Error())
	}
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
