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
	"fmt"
	"time"

	"github.com/onsi/ginkgo/v2"
	"github.com/onsi/gomega"

	"github.com/NVIDIA/k8s-operator-libs/pkg/upgrade"
	appsv1 "k8s.io/api/apps/v1"

	"github.com/NVIDIA/gpu-operator/tests/e2e/framework"
	e2eoperator "github.com/NVIDIA/gpu-operator/tests/e2e/framework/operator"
	"github.com/NVIDIA/gpu-operator/tests/e2e/helpers"
)

const (
	driverUpgradeTimeout = 45 * time.Minute

	driverUpgradeStateLabel = "nvidia.com/gpu-driver-upgrade-state"
	driverContainerName     = "nvidia-driver-ctr"
)

func WaitForDriverUpgradeDone(ctx context.Context, clients e2eoperator.Clients) {
	ginkgo.GinkgoHelper()
	framework.WaitFor(ctx, driverUpgradeTimeout, "waiting for the driver upgrade to complete on every GPU node", func(g gomega.Gomega) {
		gpuNodes, err := clients.Nodes.ListGPUNodes(ctx)
		g.Expect(err).NotTo(gomega.HaveOccurred())
		g.Expect(gpuNodes).NotTo(gomega.BeEmpty(), "no GPU nodes found")
		for _, node := range gpuNodes {
			g.Expect(node.Labels).To(gomega.HaveKeyWithValue(driverUpgradeStateLabel, upgrade.UpgradeStateDone), "node %s", node.Name)
		}
	})
}

func RolloutDriverPods(ctx context.Context, clients e2eoperator.Clients) {
	ginkgo.GinkgoHelper()
	err := clients.Pods.DeleteCollection(ctx, e2eoperator.TestContext.OperatorNamespace, helpers.DriverComponentSelector)
	framework.ExpectNoError(err, "failed to delete pods %q in %s", helpers.DriverComponentSelector, e2eoperator.TestContext.OperatorNamespace)
	WaitForPodsReady(ctx, clients, helpers.DriverComponentSelector)
	WaitForDriverUpgradeDone(ctx, clients)
}

func WaitForDriverImageVersion(ctx context.Context, timeout time.Duration, listDriverDaemonSets func(ctx context.Context) ([]appsv1.DaemonSet, error), version string) {
	ginkgo.GinkgoHelper()
	framework.WaitFor(ctx, timeout, fmt.Sprintf("waiting for the driver daemonsets to run version %s", version), func(g gomega.Gomega) {
		daemonSets, err := listDriverDaemonSets(ctx)
		g.Expect(err).NotTo(gomega.HaveOccurred())
		g.Expect(daemonSets).NotTo(gomega.BeEmpty(), "no driver daemonsets found")
		for _, daemonSet := range daemonSets {
			g.Expect(daemonSet.Spec.Template.Spec.Containers).To(gomega.ContainElement(gomega.And(
				gomega.HaveField("Name", driverContainerName),
				gomega.HaveField("Image", gomega.ContainSubstring(version)),
			)), "daemonset %s", daemonSet.Name)
		}
	})
}
