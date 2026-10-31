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
	"time"

	"github.com/onsi/ginkgo/v2"
	"github.com/onsi/gomega"

	appsv1 "k8s.io/api/apps/v1"

	"github.com/NVIDIA/gpu-operator/tests/e2e/framework"
	e2eoperator "github.com/NVIDIA/gpu-operator/tests/e2e/framework/operator"
)

const (
	DriverUpgradeTimeout = 45 * time.Minute

	driverContainerName = "nvidia-driver-ctr"
)

func WaitForDriverPodsUpgraded(ctx context.Context, clients e2eoperator.Clients) {
	ginkgo.GinkgoHelper()
	framework.ExpectNoError(clients.Drivers.WaitForPodsReady(ctx, e2eoperator.TestContext.OperatorNamespace, OperandReadyTimeout), "failed to wait for the driver pods in %s to be ready", e2eoperator.TestContext.OperatorNamespace)
	framework.ExpectNoError(clients.Drivers.WaitForPodsUpgradeDone(ctx, DriverUpgradeTimeout), "failed to wait for the driver upgrade to complete on every GPU node")
}

func ExpectDriverImageVersion(g gomega.Gomega, daemonSets []appsv1.DaemonSet, version string) {
	g.Expect(daemonSets).NotTo(gomega.BeEmpty(), "no driver daemonsets found")
	for _, daemonSet := range daemonSets {
		g.Expect(daemonSet.Spec.Template.Spec.Containers).To(gomega.ContainElement(gomega.And(
			gomega.HaveField("Name", driverContainerName),
			gomega.HaveField("Image", gomega.ContainSubstring(version)),
		)), "daemonset %s", daemonSet.Name)
	}
}
