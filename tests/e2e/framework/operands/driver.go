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
	"time"

	"github.com/onsi/ginkgo/v2"
	"github.com/onsi/gomega"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"

	"github.com/NVIDIA/gpu-operator/tests/e2e/framework"
	e2eoperator "github.com/NVIDIA/gpu-operator/tests/e2e/framework/operator"
	"github.com/NVIDIA/gpu-operator/tests/e2e/helpers"
)

const (
	DriverUpgradeTimeout = 45 * time.Minute

	driverContainerName     = "nvidia-driver-ctr"
	driverUpgradeStateLabel = "nvidia.com/gpu-driver-upgrade-state"
	driverUpgradeDoneState  = "upgrade-done"
)

func WaitForDriverPodsUpgraded(ctx context.Context, clients e2eoperator.Clients) {
	ginkgo.GinkgoHelper()
	WaitForDriverPodsReady(ctx, clients)
	WaitForDriverUpgradeDone(ctx, clients)
}

func WaitForDriverPodsReady(ctx context.Context, clients e2eoperator.Clients) {
	ginkgo.GinkgoHelper()
	readyDriverPods, elapsed := waitForReadyPods(ctx, clients, helpers.DriverComponentSelector, "driver", "Waiting for the driver pods to be ready")
	if len(readyDriverPods) == 1 {
		ginkgo.GinkgoWriter.Printf("Driver pod %s is ready (%s)\n", readyDriverPods[0].Name, elapsed.Round(time.Second))
		return
	}
	ginkgo.GinkgoWriter.Printf("%d driver pods are ready (%s)\n", len(readyDriverPods), elapsed.Round(time.Second))
}

func WaitForDriverUpgradeDone(ctx context.Context, clients e2eoperator.Clients) {
	ginkgo.GinkgoHelper()
	var upgradedNodeCount int
	elapsed := framework.WaitFor(ctx, DriverUpgradeTimeout, "Waiting for the GPU driver upgrade to complete", func(g gomega.Gomega) {
		gpuNodes, err := clients.Nodes.ListGPUNodes(ctx)
		g.Expect(err).NotTo(gomega.HaveOccurred(), "failed to list GPU nodes")
		if len(gpuNodes) == 0 {
			gomega.StopTrying("no GPU nodes found").Now()
		}
		for _, node := range gpuNodes {
			upgradeState, hasUpgradeState := node.Labels[driverUpgradeStateLabel]
			g.Expect(hasUpgradeState).To(gomega.BeTrue(), "node %s has no upgrade state yet", node.Name)
			g.Expect(upgradeState).To(gomega.Equal(driverUpgradeDoneState), "node %s is in upgrade state %s", node.Name, upgradeState)
		}
		upgradedNodeCount = len(gpuNodes)
	})
	ginkgo.GinkgoWriter.Printf("GPU driver upgrade is done on %d/%d node(s) (%s)\n", upgradedNodeCount, upgradedNodeCount, elapsed.Round(time.Second))
}

func ExpectDriverImageVersion(g gomega.Gomega, daemonSets []appsv1.DaemonSet, version string) {
	g.Expect(daemonSets).NotTo(gomega.BeEmpty(), "no driver daemonsets found")
	for _, daemonSet := range daemonSets {
		driverContainerIndex := slices.IndexFunc(daemonSet.Spec.Template.Spec.Containers, func(container corev1.Container) bool {
			return container.Name == driverContainerName
		})
		g.Expect(driverContainerIndex).NotTo(gomega.BeNumerically("<", 0), "daemonset %s has no %s container", daemonSet.Name, driverContainerName)
		driverImage := daemonSet.Spec.Template.Spec.Containers[driverContainerIndex].Image
		g.Expect(driverImage).To(gomega.ContainSubstring(version), "daemonset %s runs driver image %s, want version %s", daemonSet.Name, driverImage, version)
	}
}
