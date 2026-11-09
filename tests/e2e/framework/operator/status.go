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
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/onsi/ginkgo/v2"
	"github.com/onsi/gomega"

	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	nvidiav1alpha1 "github.com/NVIDIA/gpu-operator/api/nvidia/v1alpha1"
	"github.com/NVIDIA/gpu-operator/internal/conditions"
	"github.com/NVIDIA/gpu-operator/internal/consts"
	"github.com/NVIDIA/gpu-operator/tests/e2e/framework"
)

const NVIDIADriverReconcileTimeout = 2 * time.Minute

func WaitForDriverReady(ctx context.Context, clients Clients, driverName string) {
	ginkgo.GinkgoHelper()
	elapsed := framework.WaitFor(ctx, NVIDIADriverReconcileTimeout, fmt.Sprintf("Waiting for NVIDIADriver/%s to be ready", driverName), func(g gomega.Gomega) {
		driver, err := clients.Drivers.Get(ctx, driverName)
		g.Expect(err).NotTo(gomega.HaveOccurred(), "failed to get NVIDIADriver/%s", driverName)
		g.Expect(driver.Status.State).To(gomega.Equal(nvidiav1alpha1.Ready), "%s", describeDriverStatus(driver))
		g.Expect(meta.IsStatusConditionTrue(driver.Status.Conditions, conditions.Ready)).To(gomega.BeTrue(), "%s", describeDriverStatus(driver))
		g.Expect(meta.IsStatusConditionTrue(driver.Status.Conditions, conditions.Error)).To(gomega.BeFalse(), "%s", describeDriverStatus(driver))
	})
	ginkgo.GinkgoWriter.Printf("NVIDIADriver/%s is ready (%s)\n", driverName, elapsed.Round(time.Second))
}

func WaitForDriverError(ctx context.Context, clients Clients, driverName, expectedReason, expectedMessage string) {
	ginkgo.GinkgoHelper()
	errorConditionMatcher := gomega.And(
		gomega.HaveField("Type", conditions.Error),
		gomega.HaveField("Status", metav1.ConditionTrue),
		gomega.HaveField("Reason", expectedReason),
		gomega.HaveField("Message", gomega.ContainSubstring(expectedMessage)),
	)
	elapsed := framework.WaitFor(ctx, NVIDIADriverReconcileTimeout, fmt.Sprintf("Waiting for NVIDIADriver/%s to report %s with reason %s", driverName, nvidiav1alpha1.NotReady, expectedReason), func(g gomega.Gomega) {
		driver, err := clients.Drivers.Get(ctx, driverName)
		g.Expect(err).NotTo(gomega.HaveOccurred(), "failed to get NVIDIADriver/%s", driverName)
		g.Expect(driver.Status.State).To(gomega.Equal(nvidiav1alpha1.NotReady), "%s", describeDriverStatus(driver))
		g.Expect(driver.Status.Conditions).To(gomega.ContainElement(errorConditionMatcher), "%s", describeDriverStatus(driver))
	})
	ginkgo.GinkgoWriter.Printf("NVIDIADriver/%s reports %s with reason %s (%s)\n", driverName, nvidiav1alpha1.NotReady, expectedReason, elapsed.Round(time.Second))
}

func describeDriverStatus(driver *nvidiav1alpha1.NVIDIADriver) string {
	driverStatus := fmt.Sprintf("NVIDIADriver/%s state is %q", driver.Name, driver.Status.State)
	statusCondition := meta.FindStatusCondition(driver.Status.Conditions, conditions.Error)
	if statusCondition == nil || statusCondition.Status != metav1.ConditionTrue {
		statusCondition = meta.FindStatusCondition(driver.Status.Conditions, conditions.Ready)
	}
	if statusCondition == nil {
		return driverStatus
	}
	return fmt.Sprintf("%s, %s=%s %s: %s", driverStatus, statusCondition.Type, statusCondition.Status, statusCondition.Reason, statusCondition.Message)
}

func WaitForSingleDefaultDriver(ctx context.Context, clients Clients, expectedName string, timeout time.Duration) {
	ginkgo.GinkgoHelper()
	elapsed := framework.WaitFor(ctx, timeout, fmt.Sprintf("Waiting for NVIDIADriver/%s to be the only default NVIDIADriver", expectedName), func(g gomega.Gomega) {
		defaultDriverNames, err := clients.Drivers.ListDefaultNames(ctx)
		g.Expect(err).NotTo(gomega.HaveOccurred(), "failed to list the default NVIDIADrivers")
		g.Expect(defaultDriverNames).To(gomega.Equal([]string{expectedName}), "the default NVIDIADrivers are %v", defaultDriverNames)
	})
	ginkgo.GinkgoWriter.Printf("NVIDIADriver/%s is the only default NVIDIADriver (%s)\n", expectedName, elapsed.Round(time.Second))
}

func countNodesOwnedByDriver(ctx context.Context, clients Clients, driverName string) (ownedNodeCount, gpuNodeCount int, err error) {
	gpuNodes, err := clients.Nodes.ListGPUNodes(ctx)
	if err != nil {
		return 0, 0, err
	}
	for _, node := range gpuNodes {
		if node.Labels[consts.NVIDIADriverOwnerLabel] == driverName {
			ownedNodeCount++
		}
	}
	return ownedNodeCount, len(gpuNodes), nil
}

func WaitForNodesOwnedByDriver(ctx context.Context, clients Clients, driverName string, timeout time.Duration) {
	ginkgo.GinkgoHelper()
	var ownedNodeCount, gpuNodeCount int
	elapsed := framework.WaitFor(ctx, timeout, fmt.Sprintf("Waiting for every GPU node to be owned by NVIDIADriver/%s", driverName), func(g gomega.Gomega) {
		var err error
		ownedNodeCount, gpuNodeCount, err = countNodesOwnedByDriver(ctx, clients, driverName)
		g.Expect(err).NotTo(gomega.HaveOccurred(), "failed to count the GPU nodes owned by NVIDIADriver/%s", driverName)
		g.Expect(gpuNodeCount).To(gomega.BeNumerically(">", 0), "no GPU nodes found")
		g.Expect(ownedNodeCount).To(gomega.Equal(gpuNodeCount), "%d/%d GPU node(s) owned by NVIDIADriver/%s", ownedNodeCount, gpuNodeCount, driverName)
	})
	ginkgo.GinkgoWriter.Printf("%d/%d GPU node(s) owned by NVIDIADriver/%s (%s)\n", ownedNodeCount, gpuNodeCount, driverName, elapsed.Round(time.Second))
}

func ExpectNodesOwnedByDriver(ctx context.Context, clients Clients, driverName string) {
	ginkgo.GinkgoHelper()
	ownedNodeCount, gpuNodeCount, err := countNodesOwnedByDriver(ctx, clients, driverName)
	framework.ExpectNoError(err, "failed to count the GPU nodes owned by NVIDIADriver/%s", driverName)
	gomega.Expect(gpuNodeCount).To(gomega.BeNumerically(">", 0), "no GPU nodes found")
	gomega.Expect(ownedNodeCount).To(gomega.Equal(gpuNodeCount), "GPU nodes should remain owned by NVIDIADriver/%s", driverName)
	ginkgo.GinkgoWriter.Printf("%d/%d GPU node(s) owned by NVIDIADriver/%s\n", ownedNodeCount, gpuNodeCount, driverName)
}

func WaitForDriverDaemonSets(ctx context.Context, clients Clients, driverName string, timeout time.Duration) {
	ginkgo.GinkgoHelper()
	var daemonSetNames []string
	elapsed := framework.WaitFor(ctx, timeout, fmt.Sprintf("Waiting for NVIDIADriver/%s to create its driver daemonsets", driverName), func(g gomega.Gomega) {
		daemonSets, err := clients.Drivers.OwnedDaemonSets(ctx, TestContext.OperatorNamespace, driverName)
		g.Expect(err).NotTo(gomega.HaveOccurred(), "failed to list the driver daemonsets of NVIDIADriver/%s", driverName)
		daemonSetNames = nil
		for _, daemonSet := range daemonSets {
			daemonSetNames = append(daemonSetNames, daemonSet.Name)
		}
		g.Expect(daemonSetNames).NotTo(gomega.BeEmpty(), "NVIDIADriver/%s has no driver daemonsets", driverName)
	})
	ginkgo.GinkgoWriter.Printf("NVIDIADriver/%s has driver daemonset(s) %s (%s)\n", driverName, strings.Join(daemonSetNames, ", "), elapsed.Round(time.Second))
}
