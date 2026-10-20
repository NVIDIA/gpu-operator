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
	"errors"
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/onsi/ginkgo/v2"
	"github.com/onsi/gomega"

	appsv1 "k8s.io/api/apps/v1"
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
	nvidiaDriverReconcileTimeout = 2 * time.Minute
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
		ginkgo.DeferCleanup(func(ctx context.Context) {
			var cleanupErrs []error
			for _, driverName := range []string{duplicateDefaultDriverName, userDriverName, defaultDriverName} {
				if err := install.Clients.Drivers.DeleteIfExists(ctx, driverName); err != nil {
					cleanupErrs = append(cleanupErrs, fmt.Errorf("failed to delete NVIDIADriver/%s: %w", driverName, err))
				}
			}
			framework.ExpectNoError(errors.Join(cleanupErrs...))
		})
	})

	ginkgo.DescribeTable("renders the default NVIDIADriver only when both chart options are enabled",
		func(ctx context.Context, chartValues []string, expectedDefaultDriverRendered bool) {
			manifests, err := install.Operator.Template(ctx, chartValues)
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
		e2eoperands.WaitForOperandsReady(ctx, install.Clients, e2eoperands.DefaultOperandSelectors)
	})

	ginkgo.It("does not restart operands repeatedly", func(ctx context.Context) {
		e2eoperands.ExpectNoRepeatedOperandRestarts(ctx, install.Clients, e2eoperands.DefaultRestartCheckedOperandSelectors)
	})

	ginkgo.It("runs a GPU workload", func(ctx context.Context) {
		e2eworkload.RunGPUWorkload(ctx, install.Clients)
	})

	ginkgo.It("moves the default to an arbitrarily named NVIDIADriver", func(ctx context.Context) {
		currentDefaultDriverName := singleDefaultDriverName(ctx, install.Clients)
		framework.ExpectNoError(install.Clients.Drivers.SetDefault(ctx, currentDefaultDriverName, false), "failed to clear the default of NVIDIADriver/%s", currentDefaultDriverName)
		framework.ExpectNoError(install.Clients.Drivers.Copy(ctx, currentDefaultDriverName, defaultDriverName, true), "failed to create NVIDIADriver/%s", defaultDriverName)
		framework.ExpectNoError(install.Clients.Drivers.Delete(ctx, currentDefaultDriverName), "failed to delete NVIDIADriver/%s", currentDefaultDriverName)

		e2eoperator.WaitForSingleDefaultDriver(ctx, install.Clients, defaultDriverName, nvidiaDriverReconcileTimeout)
		e2eoperator.WaitForNodesOwnedByDriver(ctx, install.Clients, defaultDriverName, nvidiaDriverOwnershipTimeout)
		e2eoperator.WaitForDriverDaemonSets(ctx, install.Clients, defaultDriverName, nvidiaDriverOwnershipTimeout)
	})

	ginkgo.It("hands GPU nodes to a user-defined NVIDIADriver", func(ctx context.Context) {
		framework.ExpectNoError(install.Clients.Drivers.Copy(ctx, singleDefaultDriverName(ctx, install.Clients), userDriverName, false), "failed to create NVIDIADriver/%s", userDriverName)

		e2eoperator.WaitForNodesOwnedByDriver(ctx, install.Clients, userDriverName, nvidiaDriverOwnershipTimeout)
		e2eoperator.WaitForDriverDaemonSets(ctx, install.Clients, userDriverName, nvidiaDriverOwnershipTimeout)
		e2eoperands.WaitForPodsReady(ctx, install.Clients, helpers.DriverComponentSelector)
	})

	ginkgo.It("upgrades the driver of the user-defined NVIDIADriver", func(ctx context.Context) {
		framework.ExpectNoError(install.Clients.Drivers.UpdateDriverVersion(ctx, userDriverName, e2eoperator.TestContext.TargetDriverVersion), "failed to update the version of NVIDIADriver/%s", userDriverName)

		listUserDriverDaemonSets := func(ctx context.Context) ([]appsv1.DaemonSet, error) {
			return install.Clients.Drivers.OwnedDaemonSets(ctx, e2eoperator.TestContext.OperatorNamespace, userDriverName)
		}
		e2eoperands.WaitForDriverImageVersion(ctx, nvidiaDriverReconcileTimeout, listUserDriverDaemonSets, e2eoperator.TestContext.TargetDriverVersion)
		e2eoperands.RolloutDriverPods(ctx, install.Clients)
	})

	ginkgo.It("applies custom labels to the driver pods", func(ctx context.Context) {
		customLabels := map[string]string{"cloudprovider": "aws", "platform": "kubernetes"}
		err := install.Clients.Drivers.Modify(ctx, userDriverName, func(driver *nvidiav1alpha1.NVIDIADriver) {
			driver.Spec.Labels = customLabels
		})
		framework.ExpectNoError(err, "failed to set the labels of NVIDIADriver/%s", userDriverName)

		framework.WaitFor(ctx, nvidiaDriverReconcileTimeout, fmt.Sprintf("waiting for the driver daemonsets of NVIDIADriver/%s to carry the custom labels", userDriverName), func(g gomega.Gomega) {
			daemonSets, err := install.Clients.Drivers.OwnedDaemonSets(ctx, e2eoperator.TestContext.OperatorNamespace, userDriverName)
			g.Expect(err).NotTo(gomega.HaveOccurred())
			g.Expect(daemonSets).NotTo(gomega.BeEmpty())
			for _, daemonSet := range daemonSets {
				for labelKey, labelValue := range customLabels {
					g.Expect(daemonSet.Spec.Template.Labels).To(gomega.HaveKeyWithValue(labelKey, labelValue), "daemonset %s", daemonSet.Name)
				}
			}
		})
		e2eoperands.RolloutDriverPods(ctx, install.Clients)

		gpuNodes, err := install.Clients.Nodes.ListGPUNodes(ctx)
		framework.ExpectNoError(err, "failed to list GPU nodes")
		labeledDriverSelector := helpers.DriverComponentSelector + "," + labels.SelectorFromSet(customLabels).String()
		labeledDriverPods, err := install.Clients.Pods.List(ctx, e2eoperator.TestContext.OperatorNamespace, labeledDriverSelector)
		framework.ExpectNoError(err, "failed to list pods matching %s", labeledDriverSelector)
		gomega.Expect(labeledDriverPods).To(gomega.HaveLen(len(gpuNodes)), "custom labels are missing from one or more driver pods")
	})

	ginkgo.It("reports a node conflict when the default field is cleared", func(ctx context.Context) {
		framework.ExpectNoError(install.Clients.Drivers.SetDefault(ctx, defaultDriverName, false), "failed to clear the default of NVIDIADriver/%s", defaultDriverName)
		e2eoperator.WaitForDriverError(ctx, install.Clients, userDriverName, conditions.ConflictingNodeSelector, conflictingNodeMessage)
		e2eoperator.WaitForDriverError(ctx, install.Clients, defaultDriverName, conditions.ConflictingNodeSelector, conflictingNodeMessage)
		e2eoperator.ExpectNodesOwnedByDriver(ctx, install.Clients, userDriverName)

		framework.ExpectNoError(install.Clients.Drivers.SetDefault(ctx, defaultDriverName, true), "failed to set the default of NVIDIADriver/%s", defaultDriverName)
		e2eoperator.WaitForSingleDefaultDriver(ctx, install.Clients, defaultDriverName, nvidiaDriverReconcileTimeout)
		e2eoperator.WaitForDriverReady(ctx, install.Clients, userDriverName)
		e2eoperator.WaitForDriverReady(ctx, install.Clients, defaultDriverName)
		e2eoperator.WaitForNodesOwnedByDriver(ctx, install.Clients, userDriverName, nvidiaDriverOwnershipTimeout)
	})

	ginkgo.It("reports multiple default NVIDIADrivers as not ready", func(ctx context.Context) {
		framework.ExpectNoError(install.Clients.Drivers.Copy(ctx, defaultDriverName, duplicateDefaultDriverName, true), "failed to create NVIDIADriver/%s", duplicateDefaultDriverName)
		e2eoperator.WaitForDriverError(ctx, install.Clients, defaultDriverName, conditions.ReconcileFailed, duplicateDefaultMessage)
		e2eoperator.WaitForDriverError(ctx, install.Clients, duplicateDefaultDriverName, conditions.ReconcileFailed, duplicateDefaultMessage)
		e2eoperator.ExpectNodesOwnedByDriver(ctx, install.Clients, userDriverName)

		framework.ExpectNoError(install.Clients.Drivers.Delete(ctx, duplicateDefaultDriverName), "failed to delete NVIDIADriver/%s", duplicateDefaultDriverName)
		e2eoperator.WaitForSingleDefaultDriver(ctx, install.Clients, defaultDriverName, nvidiaDriverReconcileTimeout)
		e2eoperator.WaitForDriverReady(ctx, install.Clients, defaultDriverName)
		e2eoperator.WaitForDriverReady(ctx, install.Clients, userDriverName)
		e2eoperator.WaitForNodesOwnedByDriver(ctx, install.Clients, userDriverName, nvidiaDriverOwnershipTimeout)
	})
})

func singleDefaultDriverName(ctx context.Context, clients e2eoperator.Clients) string {
	ginkgo.GinkgoHelper()
	defaultDriverNames, err := clients.Drivers.ListDefaultNames(ctx)
	framework.ExpectNoError(err, "failed to list the default NVIDIADrivers")
	gomega.Expect(defaultDriverNames).To(gomega.HaveLen(1), "expected exactly one default NVIDIADriver")
	return defaultDriverNames[0]
}
