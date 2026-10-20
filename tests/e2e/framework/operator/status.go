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

const nvidiaDriverReconcileTimeout = 2 * time.Minute

func WaitForDriverReady(ctx context.Context, clients Clients, driverName string) {
	ginkgo.GinkgoHelper()
	framework.WaitFor(ctx, nvidiaDriverReconcileTimeout, fmt.Sprintf("waiting for NVIDIADriver/%s to report %s", driverName, nvidiav1alpha1.Ready), func(g gomega.Gomega) {
		driver, err := clients.Drivers.Get(ctx, driverName)
		g.Expect(err).NotTo(gomega.HaveOccurred())
		g.Expect(driver.Status.State).To(gomega.Equal(nvidiav1alpha1.Ready))
		g.Expect(meta.IsStatusConditionTrue(driver.Status.Conditions, conditions.Ready)).To(gomega.BeTrue(), "Ready condition is not true")
		g.Expect(meta.IsStatusConditionTrue(driver.Status.Conditions, conditions.Error)).To(gomega.BeFalse(), "Error condition is true")
	})
}

func WaitForDriverError(ctx context.Context, clients Clients, driverName, expectedReason, expectedMessage string) {
	ginkgo.GinkgoHelper()
	errorConditionMatcher := gomega.And(
		gomega.HaveField("Type", conditions.Error),
		gomega.HaveField("Status", metav1.ConditionTrue),
		gomega.HaveField("Reason", expectedReason),
		gomega.HaveField("Message", gomega.ContainSubstring(expectedMessage)),
	)
	framework.WaitFor(ctx, nvidiaDriverReconcileTimeout, fmt.Sprintf("waiting for NVIDIADriver/%s to report %s with reason %s", driverName, nvidiav1alpha1.NotReady, expectedReason), func(g gomega.Gomega) {
		driver, err := clients.Drivers.Get(ctx, driverName)
		g.Expect(err).NotTo(gomega.HaveOccurred())
		g.Expect(driver.Status.State).To(gomega.Equal(nvidiav1alpha1.NotReady))
		g.Expect(driver.Status.Conditions).To(gomega.ContainElement(errorConditionMatcher))
	})
}

func WaitForSingleDefaultDriver(ctx context.Context, clients Clients, expectedName string, timeout time.Duration) {
	ginkgo.GinkgoHelper()
	framework.WaitFor(ctx, timeout, fmt.Sprintf("waiting for a single default NVIDIADriver %q", expectedName), func(g gomega.Gomega) {
		defaultDriverNames, err := clients.Drivers.ListDefaultNames(ctx)
		g.Expect(err).NotTo(gomega.HaveOccurred())
		g.Expect(defaultDriverNames).To(gomega.Equal([]string{expectedName}))
	})
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
	framework.WaitFor(ctx, timeout, fmt.Sprintf("waiting for every GPU node to be owned by NVIDIADriver/%s", driverName), func(g gomega.Gomega) {
		ownedNodeCount, gpuNodeCount, err := countNodesOwnedByDriver(ctx, clients, driverName)
		g.Expect(err).NotTo(gomega.HaveOccurred())
		g.Expect(gpuNodeCount).To(gomega.BeNumerically(">", 0), "no GPU nodes found")
		g.Expect(ownedNodeCount).To(gomega.Equal(gpuNodeCount))
	})
}

func ExpectNodesOwnedByDriver(ctx context.Context, clients Clients, driverName string) {
	ginkgo.GinkgoHelper()
	ownedNodeCount, gpuNodeCount, err := countNodesOwnedByDriver(ctx, clients, driverName)
	framework.ExpectNoError(err, "failed to count the GPU nodes owned by NVIDIADriver/%s", driverName)
	gomega.Expect(gpuNodeCount).To(gomega.BeNumerically(">", 0), "no GPU nodes found")
	gomega.Expect(ownedNodeCount).To(gomega.Equal(gpuNodeCount), "GPU nodes should remain owned by NVIDIADriver/%s", driverName)
}

func WaitForDriverDaemonSets(ctx context.Context, clients Clients, driverName string, timeout time.Duration) {
	ginkgo.GinkgoHelper()
	framework.WaitFor(ctx, timeout, fmt.Sprintf("waiting for the driver daemonsets of NVIDIADriver/%s", driverName), func(g gomega.Gomega) {
		daemonSets, err := clients.Drivers.OwnedDaemonSets(ctx, TestContext.OperatorNamespace, driverName)
		g.Expect(err).NotTo(gomega.HaveOccurred())
		g.Expect(daemonSets).NotTo(gomega.BeEmpty())
	})
}
