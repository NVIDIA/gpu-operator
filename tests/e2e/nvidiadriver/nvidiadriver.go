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

package nvidiadriver

import (
	"context"
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/onsi/ginkgo/v2"
	"github.com/onsi/gomega"

	"k8s.io/apimachinery/pkg/labels"

	nvidiav1alpha1 "github.com/NVIDIA/gpu-operator/api/nvidia/v1alpha1"
	"github.com/NVIDIA/gpu-operator/internal/conditions"
	"github.com/NVIDIA/gpu-operator/internal/validator"
	"github.com/NVIDIA/gpu-operator/tests/e2e/framework"
	e2eoperands "github.com/NVIDIA/gpu-operator/tests/e2e/framework/operands"
	e2eoperator "github.com/NVIDIA/gpu-operator/tests/e2e/framework/operator"
	e2eworkload "github.com/NVIDIA/gpu-operator/tests/e2e/framework/workload"
	"github.com/NVIDIA/gpu-operator/tests/e2e/helpers"
)

const (
	nvidiaDriverOwnershipTimeout = 15 * time.Minute

	userDriverName             = "e2e-driver"
	defaultDriverName          = "e2e-default-driver"
	duplicateDefaultDriverName = "e2e-duplicate-default-driver"

	conflictingNodeMessage = "multiple NVIDIADrivers match the same node"
)

var duplicateDefaultMessage = validator.ErrMultipleDefaultNVIDIADrivers.Error()

var _ = e2eoperator.Describe("nvidia-driver", "nvidia-driver", ginkgo.Ordered, ginkgo.Serial, func() {
	install := e2eoperator.NewInstall(
		"driver.nvidiaDriverCRD.enabled=true",
		"driver.nvidiaDriverCRD.deployDefaultCR=true",
	)

	ginkgo.BeforeAll(func() {
		for _, driverName := range []string{defaultDriverName, userDriverName, duplicateDefaultDriverName} {
			ginkgo.DeferCleanup(install.Drivers.DeleteIfExists, driverName)
		}
	})

	ginkgo.DescribeTable("renders the default NVIDIADriver only when both chart options are enabled",
		func(ctx context.Context, chartValues []string, expectedDefaultDriverRendered bool) {
			ginkgo.By(fmt.Sprintf("Rendering the chart with %s", strings.Join(chartValues, ", ")))
			manifests, err := install.Operator.Template(ctx, chartValues)
			framework.ExpectNoError(err, "failed to render the chart with %v", chartValues)
			nvidiaDriverManifests := slices.DeleteFunc(strings.Split(manifests, "\n---"), func(manifest string) bool {
				return !strings.Contains(manifest, "kind: NVIDIADriver")
			})
			if expectedDefaultDriverRendered {
				gomega.Expect(nvidiaDriverManifests).To(gomega.ConsistOf(gomega.ContainSubstring("default: true")), "rendered with %v", chartValues)
				ginkgo.GinkgoWriter.Printf("The chart rendered a single default NVIDIADriver\n")
			} else {
				gomega.Expect(nvidiaDriverManifests).To(gomega.BeEmpty(), "rendered with %v", chartValues)
				ginkgo.GinkgoWriter.Printf("The chart rendered no NVIDIADriver\n")
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
		e2eoperands.WaitForOperandsReady(ctx, install.Clients, e2eoperands.DefaultOperandSelectors)
	})

	ginkgo.It("does not restart operands repeatedly", func(ctx context.Context) {
		e2eoperands.ExpectNoRepeatedOperandRestarts(ctx, install.Clients, e2eoperands.DefaultRestartCheckedOperandSelectors)
	})

	ginkgo.It("runs a GPU workload", func(ctx context.Context) {
		e2eworkload.RunGPUWorkload(ctx, install.Clients)
	})

	ginkgo.It("moves the default to an arbitrarily named NVIDIADriver", func(ctx context.Context) {
		ginkgo.By("Finding the default NVIDIADriver")
		currentDefaultDriverName := singleDefaultDriverName(ctx, install.Clients)
		ginkgo.GinkgoWriter.Printf("NVIDIADriver/%s is the default\n", currentDefaultDriverName)

		ginkgo.By(fmt.Sprintf("Clearing the default field of NVIDIADriver/%s", currentDefaultDriverName))
		framework.ExpectNoError(install.Drivers.SetDefault(ctx, currentDefaultDriverName, false), "failed to clear the default of NVIDIADriver/%s", currentDefaultDriverName)

		ginkgo.By(fmt.Sprintf("Creating NVIDIADriver/%s from NVIDIADriver/%s as the default", defaultDriverName, currentDefaultDriverName))
		framework.ExpectNoError(install.Drivers.Copy(ctx, currentDefaultDriverName, defaultDriverName, true), "failed to create NVIDIADriver/%s", defaultDriverName)

		ginkgo.By(fmt.Sprintf("Deleting NVIDIADriver/%s", currentDefaultDriverName))
		framework.ExpectNoError(install.Drivers.Delete(ctx, currentDefaultDriverName), "failed to delete NVIDIADriver/%s", currentDefaultDriverName)

		e2eoperator.WaitForSingleDefaultDriver(ctx, install.Clients, defaultDriverName, e2eoperator.NVIDIADriverReconcileTimeout)
		e2eoperator.WaitForNodesOwnedByDriver(ctx, install.Clients, defaultDriverName, nvidiaDriverOwnershipTimeout)
		e2eoperator.WaitForDriverDaemonSets(ctx, install.Clients, defaultDriverName, nvidiaDriverOwnershipTimeout)
	})

	ginkgo.It("hands GPU nodes to a user-defined NVIDIADriver", func(ctx context.Context) {
		ginkgo.By("Finding the default NVIDIADriver")
		sourceDriverName := singleDefaultDriverName(ctx, install.Clients)
		ginkgo.GinkgoWriter.Printf("NVIDIADriver/%s is the default\n", sourceDriverName)

		ginkgo.By(fmt.Sprintf("Creating the user-defined NVIDIADriver/%s from NVIDIADriver/%s", userDriverName, sourceDriverName))
		err := install.Drivers.Copy(ctx, sourceDriverName, userDriverName, false)
		framework.ExpectNoError(err, "failed to create NVIDIADriver/%s", userDriverName)

		e2eoperator.WaitForNodesOwnedByDriver(ctx, install.Clients, userDriverName, nvidiaDriverOwnershipTimeout)
		e2eoperator.WaitForDriverDaemonSets(ctx, install.Clients, userDriverName, nvidiaDriverOwnershipTimeout)
		e2eoperands.WaitForDriverPodsReady(ctx, install.Clients)
	})

	ginkgo.It("upgrades the driver of the user-defined NVIDIADriver", func(ctx context.Context) {
		ginkgo.By(fmt.Sprintf("Updating NVIDIADriver/%s to driver version %s", userDriverName, e2eoperator.TestContext.TargetDriverVersion))
		framework.ExpectNoError(install.Drivers.UpdateDriverVersion(ctx, userDriverName, e2eoperator.TestContext.TargetDriverVersion), "failed to update the version of NVIDIADriver/%s", userDriverName)
		waitForDriverImageVersion(ctx, install.Clients, userDriverName, e2eoperator.TestContext.TargetDriverVersion)

		ginkgo.By("Deleting the driver pods to trigger the update")
		err := install.Pods.DeleteCollection(ctx, e2eoperator.TestContext.OperatorNamespace, helpers.DriverComponentSelector)
		framework.ExpectNoError(err, "failed to delete the driver pods")
		e2eoperands.WaitForDriverPodsUpgraded(ctx, install.Clients)
	})

	ginkgo.It("applies custom labels to the driver pods", func(ctx context.Context) {
		customLabels := map[string]string{"cloudprovider": "aws", "platform": "kubernetes"}
		customLabelSelector := labels.SelectorFromSet(customLabels).String()
		ginkgo.By(fmt.Sprintf("Setting the custom labels %s on NVIDIADriver/%s", customLabelSelector, userDriverName))
		err := install.Drivers.Modify(ctx, userDriverName, func(driver *nvidiav1alpha1.NVIDIADriver) {
			driver.Spec.Labels = customLabels
		})
		framework.ExpectNoError(err, "failed to set the labels of NVIDIADriver/%s", userDriverName)

		labelUpdateElapsed := framework.WaitFor(ctx, e2eoperator.NVIDIADriverReconcileTimeout, fmt.Sprintf("Waiting for the driver daemonsets of NVIDIADriver/%s to carry the custom labels", userDriverName), func(g gomega.Gomega) {
			daemonSets, err := install.Drivers.OwnedDaemonSets(ctx, e2eoperator.TestContext.OperatorNamespace, userDriverName)
			g.Expect(err).NotTo(gomega.HaveOccurred(), "failed to list the driver daemonsets of NVIDIADriver/%s", userDriverName)
			g.Expect(daemonSets).NotTo(gomega.BeEmpty(), "NVIDIADriver/%s owns no driver daemonsets yet", userDriverName)
			for _, daemonSet := range daemonSets {
				for labelKey, labelValue := range customLabels {
					g.Expect(daemonSet.Spec.Template.Labels).To(gomega.HaveKeyWithValue(labelKey, labelValue), "pod template labels of daemonset %s", daemonSet.Name)
				}
			}
		})
		ginkgo.GinkgoWriter.Printf("Driver daemonsets of NVIDIADriver/%s carry the custom labels %s (%s)\n", userDriverName, customLabelSelector, labelUpdateElapsed.Round(time.Second))

		ginkgo.By("Deleting the driver pods to recreate them with the custom labels")
		err = install.Pods.DeleteCollection(ctx, e2eoperator.TestContext.OperatorNamespace, helpers.DriverComponentSelector)
		framework.ExpectNoError(err, "failed to delete the driver pods")
		e2eoperands.WaitForDriverPodsUpgraded(ctx, install.Clients)

		ginkgo.By(fmt.Sprintf("Checking that every driver pod carries the custom labels %s", customLabelSelector))
		gpuNodes, err := install.Nodes.ListGPUNodes(ctx)
		framework.ExpectNoError(err, "failed to list GPU nodes")
		labeledDriverSelector := helpers.DriverComponentSelector + "," + customLabelSelector
		labeledDriverPods, err := install.Pods.List(ctx, e2eoperator.TestContext.OperatorNamespace, labeledDriverSelector)
		framework.ExpectNoError(err, "failed to list pods matching %s", labeledDriverSelector)
		gomega.Expect(labeledDriverPods).To(gomega.HaveLen(len(gpuNodes)), "custom labels are missing from one or more driver pods")
		ginkgo.GinkgoWriter.Printf("All %d driver pods carry the custom labels %s\n", len(labeledDriverPods), customLabelSelector)
	})

	ginkgo.It("reports a node conflict when the default field is cleared", func(ctx context.Context) {
		ginkgo.By(fmt.Sprintf("Clearing the default field of NVIDIADriver/%s", defaultDriverName))
		framework.ExpectNoError(install.Drivers.SetDefault(ctx, defaultDriverName, false), "failed to clear the default of NVIDIADriver/%s", defaultDriverName)
		e2eoperator.WaitForDriverError(ctx, install.Clients, userDriverName, conditions.ConflictingNodeSelector, conflictingNodeMessage)
		e2eoperator.WaitForDriverError(ctx, install.Clients, defaultDriverName, conditions.ConflictingNodeSelector, conflictingNodeMessage)
		e2eoperator.ExpectNodesOwnedByDriver(ctx, install.Clients, userDriverName)

		ginkgo.By(fmt.Sprintf("Setting NVIDIADriver/%s as the default", defaultDriverName))
		framework.ExpectNoError(install.Drivers.SetDefault(ctx, defaultDriverName, true), "failed to set the default of NVIDIADriver/%s", defaultDriverName)
		e2eoperator.WaitForSingleDefaultDriver(ctx, install.Clients, defaultDriverName, e2eoperator.NVIDIADriverReconcileTimeout)
		e2eoperator.WaitForDriverReady(ctx, install.Clients, userDriverName)
		e2eoperator.WaitForDriverReady(ctx, install.Clients, defaultDriverName)
		e2eoperator.WaitForNodesOwnedByDriver(ctx, install.Clients, userDriverName, nvidiaDriverOwnershipTimeout)
	})

	ginkgo.It("reports multiple default NVIDIADrivers as not ready", func(ctx context.Context) {
		ginkgo.By(fmt.Sprintf("Creating NVIDIADriver/%s from NVIDIADriver/%s as a second default", duplicateDefaultDriverName, defaultDriverName))
		framework.ExpectNoError(install.Drivers.Copy(ctx, defaultDriverName, duplicateDefaultDriverName, true), "failed to create NVIDIADriver/%s", duplicateDefaultDriverName)
		e2eoperator.WaitForDriverError(ctx, install.Clients, defaultDriverName, conditions.ReconcileFailed, duplicateDefaultMessage)
		e2eoperator.WaitForDriverError(ctx, install.Clients, duplicateDefaultDriverName, conditions.ReconcileFailed, duplicateDefaultMessage)
		e2eoperator.ExpectNodesOwnedByDriver(ctx, install.Clients, userDriverName)

		ginkgo.By(fmt.Sprintf("Deleting NVIDIADriver/%s", duplicateDefaultDriverName))
		framework.ExpectNoError(install.Drivers.Delete(ctx, duplicateDefaultDriverName), "failed to delete NVIDIADriver/%s", duplicateDefaultDriverName)
		e2eoperator.WaitForSingleDefaultDriver(ctx, install.Clients, defaultDriverName, e2eoperator.NVIDIADriverReconcileTimeout)
		e2eoperator.WaitForDriverReady(ctx, install.Clients, defaultDriverName)
		e2eoperator.WaitForDriverReady(ctx, install.Clients, userDriverName)
		e2eoperator.WaitForNodesOwnedByDriver(ctx, install.Clients, userDriverName, nvidiaDriverOwnershipTimeout)
	})
})

func waitForDriverImageVersion(ctx context.Context, clients e2eoperator.Clients, driverName, version string) {
	ginkgo.GinkgoHelper()
	imageUpdateElapsed := framework.WaitFor(ctx, e2eoperands.DaemonSetUpdateTimeout, fmt.Sprintf("Waiting for the driver daemonsets of NVIDIADriver/%s to run version %s", driverName, version), func(g gomega.Gomega) {
		daemonSets, err := clients.Drivers.OwnedDaemonSets(ctx, e2eoperator.TestContext.OperatorNamespace, driverName)
		g.Expect(err).NotTo(gomega.HaveOccurred(), "failed to list the driver daemonsets of NVIDIADriver/%s", driverName)
		e2eoperands.ExpectDriverImageVersion(g, daemonSets, version)
	})
	ginkgo.GinkgoWriter.Printf("Driver daemonsets of NVIDIADriver/%s run version %s (%s)\n", driverName, version, imageUpdateElapsed.Round(time.Second))
}

func singleDefaultDriverName(ctx context.Context, clients e2eoperator.Clients) string {
	ginkgo.GinkgoHelper()
	defaultDriverNames, err := clients.Drivers.ListDefaultNames(ctx)
	framework.ExpectNoError(err, "failed to list the default NVIDIADrivers")
	gomega.Expect(defaultDriverNames).To(gomega.HaveLen(1), "expected exactly one default NVIDIADriver")
	return defaultDriverNames[0]
}
