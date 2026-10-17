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
	"context"
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/onsi/ginkgo/v2"
	"github.com/onsi/gomega"

	appsv1 "k8s.io/api/apps/v1"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"

	nvidiav1alpha1 "github.com/NVIDIA/gpu-operator/api/nvidia/v1alpha1"
	"github.com/NVIDIA/gpu-operator/internal/conditions"
	"github.com/NVIDIA/gpu-operator/internal/consts"
	"github.com/NVIDIA/gpu-operator/internal/validator"
	"github.com/NVIDIA/gpu-operator/tests/e2e/framework"
	"github.com/NVIDIA/gpu-operator/tests/e2e/helpers"
)

const (
	nvidiaDriverReconcileTimeout = 2 * time.Minute
	nvidiaDriverOwnerTimeout     = 15 * time.Minute

	userDriverName             = "e2e-driver"
	defaultDriverName          = "e2e-default-driver"
	duplicateDefaultDriverName = "e2e-duplicate-default-driver"

	nodeConflictMessage = "multiple NVIDIADrivers match the same node"
)

var duplicateDefaultMessage = validator.ErrMultipleDefaultNVIDIADrivers.Error()

var _ = ginkgo.Describe(e2eTestPrefix+"-nvidia-driver", ginkgo.Label("nvidia-driver"), ginkgo.Ordered, func() {
	install := newOperatorInstall(
		"driver.nvidiaDriverCRD.enabled=true",
		"driver.nvidiaDriverCRD.deployDefaultCR=true",
	)

	ginkgo.BeforeAll(func() {
		ginkgo.DeferCleanup(func(ctx context.Context) {
			for _, driverName := range []string{duplicateDefaultDriverName, userDriverName, defaultDriverName} {
				framework.ExpectNoError(install.clients.drivers.DeleteIfExists(ctx, driverName), "failed to delete NVIDIADriver/%s", driverName)
			}
		})
	})

	ginkgo.DescribeTable("renders the default NVIDIADriver only when both chart options are enabled",
		func(ctx context.Context, chartValues []string, expectedDefaultDriverRendered bool) {
			manifests, err := install.operator.Template(ctx, chartValues)
			framework.ExpectNoError(err, "failed to render the chart with %v", chartValues)
			nvidiaDriverManifests := slices.DeleteFunc(strings.Split(manifests, "\n---"), func(manifest string) bool {
				return !strings.Contains(manifest, "kind: NVIDIADriver")
			})
			if expectedDefaultDriverRendered {
				gomega.Expect(nvidiaDriverManifests).To(gomega.ConsistOf(gomega.ContainSubstring("default: true")), "rendered with %v", chartValues)
			} else {
				gomega.Expect(nvidiaDriverManifests).To(gomega.BeEmpty(), "rendered with %v", chartValues)
			}
		},
		ginkgo.Entry("with both options enabled",
			[]string{"driver.nvidiaDriverCRD.enabled=true", "driver.nvidiaDriverCRD.deployDefaultCR=true"}, true),
		ginkgo.Entry("not with deployDefaultCR disabled",
			[]string{"driver.nvidiaDriverCRD.enabled=true", "driver.nvidiaDriverCRD.deployDefaultCR=false"}, false),
		ginkgo.Entry("not with nvidiaDriverCRD disabled",
			[]string{"driver.nvidiaDriverCRD.enabled=false", "driver.nvidiaDriverCRD.deployDefaultCR=true"}, false),
	)

	ginkgo.It("brings up every operand", func(ctx context.Context) {
		waitForOperandsReady(ctx, install.clients)
	})

	ginkgo.It("does not restart operands repeatedly", func(ctx context.Context) {
		expectNoRepeatedOperandRestarts(ctx, install.clients)
	})

	ginkgo.It("runs a GPU workload", func(ctx context.Context) {
		runGPUWorkload(ctx, install.clients)
	})

	ginkgo.It("moves the default to an arbitrarily named NVIDIADriver", func(ctx context.Context) {
		clients := install.clients
		currentDefaultDriverName := singleDefaultDriverName(ctx, clients)
		framework.ExpectNoError(clients.drivers.SetDefault(ctx, currentDefaultDriverName, false), "failed to clear the default of NVIDIADriver/%s", currentDefaultDriverName)
		framework.ExpectNoError(clients.drivers.Copy(ctx, currentDefaultDriverName, defaultDriverName, true), "failed to create NVIDIADriver/%s", defaultDriverName)
		framework.ExpectNoError(clients.drivers.Delete(ctx, currentDefaultDriverName), "failed to delete NVIDIADriver/%s", currentDefaultDriverName)

		waitForSingleDefaultDriver(ctx, clients, defaultDriverName, nvidiaDriverReconcileTimeout)
		waitForNodesOwnedByDriver(ctx, clients, defaultDriverName, nvidiaDriverOwnerTimeout)
		waitForDriverDaemonSets(ctx, clients, defaultDriverName, nvidiaDriverOwnerTimeout)
	})

	ginkgo.It("hands GPU nodes to a user-defined NVIDIADriver", func(ctx context.Context) {
		clients := install.clients
		framework.ExpectNoError(clients.drivers.Copy(ctx, singleDefaultDriverName(ctx, clients), userDriverName, false), "failed to create NVIDIADriver/%s", userDriverName)

		waitForNodesOwnedByDriver(ctx, clients, userDriverName, nvidiaDriverOwnerTimeout)
		waitForDriverDaemonSets(ctx, clients, userDriverName, nvidiaDriverOwnerTimeout)
		waitForPodsReady(ctx, clients, helpers.DriverComponentSelector)
	})

	ginkgo.It("upgrades the driver of the user-defined NVIDIADriver", func(ctx context.Context) {
		clients := install.clients
		framework.ExpectNoError(clients.drivers.UpdateDriverVersion(ctx, userDriverName, tcfg.targetDriverVersion), "failed to update the version of NVIDIADriver/%s", userDriverName)

		listUserDriverDaemonSets := func(ctx context.Context) ([]appsv1.DaemonSet, error) {
			return clients.drivers.OwnedDaemonSets(ctx, tcfg.namespace, userDriverName)
		}
		waitForDaemonSetImage(ctx, nvidiaDriverReconcileTimeout, listUserDriverDaemonSets, tcfg.targetDriverVersion)
		rolloutDriverPods(ctx, clients)
	})

	ginkgo.It("applies custom labels to the driver pods", func(ctx context.Context) {
		clients := install.clients
		customLabels := map[string]string{"cloudprovider": "aws", "platform": "kubernetes"}
		setCustomLabels := func(driver *nvidiav1alpha1.NVIDIADriver) { driver.Spec.Labels = customLabels }
		framework.ExpectNoError(clients.drivers.Modify(ctx, userDriverName, setCustomLabels), "failed to set the labels of NVIDIADriver/%s", userDriverName)

		waitFor(ctx, nvidiaDriverReconcileTimeout, fmt.Sprintf("waiting for the driver daemonsets of NVIDIADriver/%s to carry the custom labels", userDriverName), func(g gomega.Gomega) {
			daemonSets, err := clients.drivers.OwnedDaemonSets(ctx, tcfg.namespace, userDriverName)
			g.Expect(err).NotTo(gomega.HaveOccurred())
			g.Expect(daemonSets).NotTo(gomega.BeEmpty())
			for _, daemonSet := range daemonSets {
				for key, value := range customLabels {
					g.Expect(daemonSet.Spec.Template.Labels).To(gomega.HaveKeyWithValue(key, value), "daemonset %s", daemonSet.Name)
				}
			}
		})
		rolloutDriverPods(ctx, clients)

		gpuNodes, err := clients.nodes.ListGPUNodes(ctx)
		framework.ExpectNoError(err, "failed to list GPU nodes")
		labeledDriverSelector := helpers.DriverComponentSelector + "," + labels.SelectorFromSet(customLabels).String()
		labeledDriverPods, err := clients.pods.List(ctx, tcfg.namespace, labeledDriverSelector)
		framework.ExpectNoError(err, "failed to list pods matching %s", labeledDriverSelector)
		gomega.Expect(labeledDriverPods).To(gomega.HaveLen(len(gpuNodes)), "custom labels are missing from one or more driver pods")
	})

	ginkgo.It("reports a node conflict when the default field is cleared", func(ctx context.Context) {
		clients := install.clients
		framework.ExpectNoError(clients.drivers.SetDefault(ctx, defaultDriverName, false), "failed to clear the default of NVIDIADriver/%s", defaultDriverName)
		waitForDriverState(ctx, clients, userDriverName, nvidiav1alpha1.NotReady, nodeConflictMessage)
		waitForDriverState(ctx, clients, defaultDriverName, nvidiav1alpha1.NotReady, nodeConflictMessage)
		expectNodesOwnedByDriver(ctx, clients, userDriverName)

		framework.ExpectNoError(clients.drivers.SetDefault(ctx, defaultDriverName, true), "failed to set the default of NVIDIADriver/%s", defaultDriverName)
		waitForSingleDefaultDriver(ctx, clients, defaultDriverName, nvidiaDriverReconcileTimeout)
		waitForNodesOwnedByDriver(ctx, clients, userDriverName, nvidiaDriverOwnerTimeout)
	})

	ginkgo.It("reports multiple default NVIDIADrivers as not ready", func(ctx context.Context) {
		clients := install.clients
		framework.ExpectNoError(clients.drivers.Copy(ctx, defaultDriverName, duplicateDefaultDriverName, true), "failed to create NVIDIADriver/%s", duplicateDefaultDriverName)
		waitForDriverState(ctx, clients, defaultDriverName, nvidiav1alpha1.NotReady, duplicateDefaultMessage)
		waitForDriverState(ctx, clients, duplicateDefaultDriverName, nvidiav1alpha1.NotReady, duplicateDefaultMessage)
		expectNodesOwnedByDriver(ctx, clients, userDriverName)

		framework.ExpectNoError(clients.drivers.Delete(ctx, duplicateDefaultDriverName), "failed to delete NVIDIADriver/%s", duplicateDefaultDriverName)
		waitForSingleDefaultDriver(ctx, clients, defaultDriverName, nvidiaDriverReconcileTimeout)
		waitForDriverState(ctx, clients, defaultDriverName, nvidiav1alpha1.Ready, "")
		waitForNodesOwnedByDriver(ctx, clients, userDriverName, nvidiaDriverOwnerTimeout)
	})
})

func singleDefaultDriverName(ctx context.Context, clients clusterClients) string {
	ginkgo.GinkgoHelper()
	defaultDriverNames, err := clients.drivers.ListDefaultNames(ctx)
	framework.ExpectNoError(err, "failed to list the default NVIDIADrivers")
	gomega.Expect(defaultDriverNames).To(gomega.HaveLen(1), "expected exactly one default NVIDIADriver")
	return defaultDriverNames[0]
}

// waitForSingleDefaultDriver waits until NVIDIADriver/expectedName is the only default NVIDIADriver.
func waitForSingleDefaultDriver(ctx context.Context, clients clusterClients, expectedName string, timeout time.Duration) {
	ginkgo.GinkgoHelper()
	waitFor(ctx, timeout, fmt.Sprintf("waiting for a single default NVIDIADriver %q", expectedName), func(g gomega.Gomega) {
		defaultDriverNames, err := clients.drivers.ListDefaultNames(ctx)
		g.Expect(err).NotTo(gomega.HaveOccurred())
		g.Expect(defaultDriverNames).To(gomega.Equal([]string{expectedName}))
	})
}

func countNodesOwnedByDriver(ctx context.Context, clients clusterClients, driverName string) (ownedNodeCount, gpuNodeCount int, err error) {
	gpuNodes, err := clients.nodes.ListGPUNodes(ctx)
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

func waitForNodesOwnedByDriver(ctx context.Context, clients clusterClients, driverName string, timeout time.Duration) {
	ginkgo.GinkgoHelper()
	waitFor(ctx, timeout, fmt.Sprintf("waiting for every GPU node to be owned by NVIDIADriver/%s", driverName), func(g gomega.Gomega) {
		ownedNodeCount, gpuNodeCount, err := countNodesOwnedByDriver(ctx, clients, driverName)
		g.Expect(err).NotTo(gomega.HaveOccurred())
		g.Expect(gpuNodeCount).To(gomega.BeNumerically(">", 0), "no GPU nodes found")
		g.Expect(ownedNodeCount).To(gomega.Equal(gpuNodeCount))
	})
}

func expectNodesOwnedByDriver(ctx context.Context, clients clusterClients, driverName string) {
	ginkgo.GinkgoHelper()
	ownedNodeCount, gpuNodeCount, err := countNodesOwnedByDriver(ctx, clients, driverName)
	framework.ExpectNoError(err, "failed to count the GPU nodes owned by NVIDIADriver/%s", driverName)
	gomega.Expect(ownedNodeCount).To(gomega.Equal(gpuNodeCount), "GPU nodes should remain owned by NVIDIADriver/%s", driverName)
}

func waitForDriverDaemonSets(ctx context.Context, clients clusterClients, driverName string, timeout time.Duration) {
	ginkgo.GinkgoHelper()
	waitFor(ctx, timeout, fmt.Sprintf("waiting for the driver daemonsets of NVIDIADriver/%s", driverName), func(g gomega.Gomega) {
		daemonSets, err := clients.drivers.OwnedDaemonSets(ctx, tcfg.namespace, driverName)
		g.Expect(err).NotTo(gomega.HaveOccurred())
		g.Expect(daemonSets).NotTo(gomega.BeEmpty())
	})
}

// waitForDriverState waits for NVIDIADriver/driverName to report expectedState. A ready driver must
// also have a true Ready condition and no true Error condition; a non-empty expectedMessage must
// appear in one of its conditions.
func waitForDriverState(ctx context.Context, clients clusterClients, driverName string, expectedState nvidiav1alpha1.State, expectedMessage string) {
	ginkgo.GinkgoHelper()
	description := fmt.Sprintf("waiting for NVIDIADriver/%s to report %s", driverName, expectedState)
	if expectedMessage != "" {
		description += fmt.Sprintf(" with %q", expectedMessage)
	}
	waitFor(ctx, nvidiaDriverReconcileTimeout, description, func(g gomega.Gomega) {
		driver, err := clients.drivers.Get(ctx, driverName)
		g.Expect(err).NotTo(gomega.HaveOccurred())
		g.Expect(driver.Status.State).To(gomega.Equal(expectedState))
		if expectedState == nvidiav1alpha1.Ready {
			g.Expect(meta.IsStatusConditionTrue(driver.Status.Conditions, conditions.Ready)).To(gomega.BeTrue(), "Ready condition is not true")
			g.Expect(meta.IsStatusConditionTrue(driver.Status.Conditions, conditions.Error)).To(gomega.BeFalse(), "Error condition is true")
		}
		if expectedMessage != "" {
			g.Expect(driver.Status.Conditions).To(gomega.ContainElement(gomega.Satisfy(func(condition metav1.Condition) bool {
				return strings.Contains(condition.Message, expectedMessage)
			})))
		}
	})
}
