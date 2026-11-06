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

package clusterpolicy

import (
	"context"
	"fmt"
	"slices"
	"time"

	"github.com/onsi/ginkgo/v2"
	"github.com/onsi/gomega"

	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"

	nvidiav1 "github.com/NVIDIA/gpu-operator/api/nvidia/v1"
	"github.com/NVIDIA/gpu-operator/internal/consts"
	"github.com/NVIDIA/gpu-operator/tests/e2e/framework"
	e2eoperands "github.com/NVIDIA/gpu-operator/tests/e2e/framework/operands"
	e2eoperator "github.com/NVIDIA/gpu-operator/tests/e2e/framework/operator"
	e2eworkload "github.com/NVIDIA/gpu-operator/tests/e2e/framework/workload"
	"github.com/NVIDIA/gpu-operator/tests/e2e/helpers"
)

const (
	driverMigrationStepTimeout = 5 * time.Minute

	deployOperandsLabel = "nvidia.com/gpu.deploy.operands"

	legacyDriverApp = "nvidia-driver-daemonset"

	dcgmRemoteHostEngineEnvName  = "DCGM_REMOTE_HOSTENGINE_INFO"
	dcgmRemoteHostEngineEnvValue = "nvidia-dcgm:5555"
)

var _ = e2eoperator.Describe("clusterpolicy", "clusterpolicy", ginkgo.Ordered, ginkgo.Serial, func() {
	install := e2eoperator.NewInstall()

	ginkgo.It("brings up every operand", func(ctx context.Context) {
		e2eoperands.WaitForOperandsReady(ctx, install.Clients, e2eoperands.DefaultOperandSelectors)
	})

	ginkgo.It("does not restart operands repeatedly", func(ctx context.Context) {
		e2eoperands.ExpectNoRepeatedOperandRestarts(ctx, install.Clients, e2eoperands.DefaultRestartCheckedOperandSelectors)
	})

	ginkgo.It("runs a GPU workload", func(ctx context.Context) {
		e2eworkload.RunGPUWorkload(ctx, install.Clients)
	})

	ginkgo.Context("when the ClusterPolicy changes", func() {
		ginkgo.It("rolls out a new driver version", func(ctx context.Context) {
			err := install.ClusterPolicies.UpdateDriverVersion(ctx, e2eoperator.ClusterPolicyName, e2eoperator.TestContext.TargetDriverVersion)
			framework.ExpectNoError(err, "failed to update the ClusterPolicy driver version to %s", e2eoperator.TestContext.TargetDriverVersion)

			waitForLegacyDriverImageVersion(ctx, install.Clients, e2eoperator.TestContext.TargetDriverVersion)

			err = install.Pods.DeleteCollection(ctx, e2eoperator.TestContext.OperatorNamespace, helpers.DriverComponentSelector)
			framework.ExpectNoError(err, "failed to delete the driver pods")
			e2eoperands.WaitForDriverPodsUpgraded(ctx, install.Clients)
		})

		ginkgo.It("passes env vars to the device plugin", func(ctx context.Context) {
			testEnvVar := nvidiav1.EnvVar{Name: "MY_TEST_ENV_NAME", Value: "test"}
			err := install.ClusterPolicies.Modify(ctx, e2eoperator.ClusterPolicyName, func(clusterPolicy *nvidiav1.ClusterPolicy) {
				clusterPolicy.Spec.DevicePlugin.Env = []nvidiav1.EnvVar{testEnvVar}
			})
			framework.ExpectNoError(err, "failed to set the device plugin env")

			e2eoperands.WaitForDaemonSetEnv(ctx, install.Clients, "nvidia-device-plugin-daemonset", testEnvVar.Name, testEnvVar.Value)
			e2eoperands.WaitForDaemonSetRolledOut(ctx, install.Clients, "nvidia-device-plugin-daemonset")
			e2eoperands.WaitForPodsReady(ctx, install.Clients, e2eoperands.AppSelector("nvidia-device-plugin-daemonset"))
		})

		ginkgo.It("passes the MIG strategy to GFD and the device plugin", func(ctx context.Context) {
			migStrategy := string(nvidiav1.MIGStrategyMixed)
			err := install.ClusterPolicies.SetMIGStrategy(ctx, e2eoperator.ClusterPolicyName, migStrategy)
			framework.ExpectNoError(err, "failed to set the MIG strategy to %s", migStrategy)

			e2eoperands.WaitForDaemonSetEnv(ctx, install.Clients, "gpu-feature-discovery", "MIG_STRATEGY", migStrategy)
			e2eoperands.WaitForDaemonSetEnv(ctx, install.Clients, "nvidia-device-plugin-daemonset", "MIG_STRATEGY", migStrategy)
			for _, app := range []string{"gpu-feature-discovery", "nvidia-device-plugin-daemonset"} {
				e2eoperands.WaitForDaemonSetRolledOut(ctx, install.Clients, app)
				e2eoperands.WaitForPodsReady(ctx, install.Clients, e2eoperands.AppSelector(app))
			}
		})

		ginkgo.It("deploys standalone DCGM with a node-local service", func(ctx context.Context) {
			framework.ExpectNoError(install.ClusterPolicies.EnableDCGM(ctx, e2eoperator.ClusterPolicyName), "failed to enable standalone DCGM")

			e2eoperands.WaitForPodsReady(ctx, install.Clients, e2eoperands.AppSelector("nvidia-dcgm"))
			e2eoperands.WaitForDaemonSetEnv(ctx, install.Clients, "nvidia-dcgm-exporter", dcgmRemoteHostEngineEnvName, dcgmRemoteHostEngineEnvValue)
			e2eoperands.WaitForDaemonSetRolledOut(ctx, install.Clients, "nvidia-dcgm-exporter")
			e2eoperands.WaitForPodsReady(ctx, install.Clients, e2eoperands.AppSelector("nvidia-dcgm-exporter"))

			dcgmService, err := install.Kube.CoreV1().Services(e2eoperator.TestContext.OperatorNamespace).Get(ctx, "nvidia-dcgm", metav1.GetOptions{})
			framework.ExpectNoError(err, "failed to get the nvidia-dcgm service")
			gomega.Expect(dcgmService.Spec.InternalTrafficPolicy).To(gomega.HaveValue(gomega.Equal(corev1.ServiceInternalTrafficPolicyLocal)))
		})

		ginkgo.It("shares the GPU with time-slicing", func(ctx context.Context) {
			gpuNodes, err := install.Nodes.ListGPUNodes(ctx)
			framework.ExpectNoError(err, "failed to list the GPU nodes")
			gomega.Expect(gpuNodes).NotTo(gomega.BeEmpty(), "no GPU nodes found")
			productName, physicalGPUCount := gpuProductAndCount(gpuNodes)
			productNameByNodeName := gpuProductNameByNodeName(gpuNodes)

			createTimeSlicingConfig(ctx, install.Clients)
			err = install.ClusterPolicies.Modify(ctx, e2eoperator.ClusterPolicyName, func(clusterPolicy *nvidiav1.ClusterPolicy) {
				clusterPolicy.Spec.DevicePlugin.Config = &nvidiav1.DevicePluginConfig{
					Name:    timeSlicingConfigMapName,
					Default: timeSlicingConfigKey,
				}
			})
			framework.ExpectNoError(err, "failed to point the device plugin at the time-slicing config")

			waitForSharedGPUNodeLabels(ctx, install.Clients, productNameByNodeName)
			for _, app := range []string{"nvidia-device-plugin-daemonset", "gpu-feature-discovery"} {
				e2eoperands.WaitForDaemonSetRolledOut(ctx, install.Clients, app)
				e2eoperands.WaitForPodsReady(ctx, install.Clients, e2eoperands.AppSelector(app))
			}

			ginkgo.DeferCleanup(deleteGPUSharingDeployment, install.Clients)
			gpuSharingDeployment := newGPUSharingDeployment(sharedGPUProductName(productName), physicalGPUCount+1)
			_, err = install.Kube.AppsV1().Deployments(e2eoperator.TestContext.OperatorNamespace).Create(ctx, gpuSharingDeployment, metav1.CreateOptions{})
			framework.ExpectNoError(err, "failed to create deployment %s", gpuSharingWorkloadName)
			waitForGPUSharingDeploymentAvailable(ctx, install.Clients)
		})

		ginkgo.It("removes and restores GFD", func(ctx context.Context) {
			err := install.ClusterPolicies.DisableGFD(ctx, e2eoperator.ClusterPolicyName)
			framework.ExpectNoError(err, "failed to disable GFD")
			e2eoperands.WaitForPodsDeleted(ctx, install.Clients, e2eoperands.AppSelector("gpu-feature-discovery"))

			err = install.ClusterPolicies.EnableGFD(ctx, e2eoperator.ClusterPolicyName)
			framework.ExpectNoError(err, "failed to enable GFD")
			e2eoperands.WaitForPodsReady(ctx, install.Clients, e2eoperands.AppSelector("gpu-feature-discovery"))
		})

		ginkgo.It("removes and restores the DCGM exporter", func(ctx context.Context) {
			err := install.ClusterPolicies.DisableDCGMExporter(ctx, e2eoperator.ClusterPolicyName)
			framework.ExpectNoError(err, "failed to disable the DCGM exporter")
			e2eoperands.WaitForPodsDeleted(ctx, install.Clients, e2eoperands.AppSelector("nvidia-dcgm-exporter"))

			err = install.ClusterPolicies.EnableDCGMExporter(ctx, e2eoperator.ClusterPolicyName)
			framework.ExpectNoError(err, "failed to enable the DCGM exporter")
			e2eoperands.WaitForPodsReady(ctx, install.Clients, e2eoperands.AppSelector("nvidia-dcgm-exporter"))
		})

		ginkgo.It("applies custom labels to every operand pod", func(ctx context.Context) {
			customLabels := map[string]string{"cloudprovider": "aws", "platform": "kubernetes"}
			err := install.ClusterPolicies.Modify(ctx, e2eoperator.ClusterPolicyName, func(clusterPolicy *nvidiav1.ClusterPolicy) {
				clusterPolicy.Spec.Daemonsets.Labels = customLabels
			})
			framework.ExpectNoError(err, "failed to set custom operand labels")

			e2eoperands.WaitForOperandPodLabels(ctx, install.Clients, e2eoperands.DefaultOperandSelectors, customLabels)
			framework.ExpectNoError(install.Drivers.WaitForPodsUpgradeDone(ctx, e2eoperands.DriverUpgradeTimeout), "failed to wait for the driver upgrade to complete on every GPU node")
			e2eoperands.WaitForOperandsReady(ctx, install.Clients, e2eoperands.DefaultOperandSelectors)
		})
	})

	ginkgo.It("recovers after the operator restarts", func(ctx context.Context) {
		operatorPods, err := install.Pods.List(ctx, e2eoperator.TestContext.OperatorNamespace, e2eoperator.OperatorComponentSelector)
		framework.ExpectNoError(err, "failed to list the operator pods")
		gomega.Expect(operatorPods).NotTo(gomega.BeEmpty(), "operator pod not found")

		err = install.Pods.DeleteCollection(ctx, e2eoperator.TestContext.OperatorNamespace, e2eoperator.OperatorComponentSelector)
		framework.ExpectNoError(err, "failed to delete the operator pods")
		e2eoperator.WaitForReadyReplacementOperatorPod(ctx, install.Clients, operatorPods)
	})

	ginkgo.It("removes operands when nodes opt out and restores them when they opt back in", func(ctx context.Context) {
		originalNodes, err := install.Nodes.ListNodes(ctx)
		framework.ExpectNoError(err, "failed to list nodes")
		for _, originalNode := range originalNodes {
			if originalValue, hasLabel := originalNode.Labels[deployOperandsLabel]; hasLabel {
				ginkgo.DeferCleanup(func(ctx context.Context) error {
					return client.IgnoreNotFound(install.Nodes.LabelNode(ctx, originalNode.Name, deployOperandsLabel, originalValue))
				})
			} else {
				ginkgo.DeferCleanup(func(ctx context.Context) error {
					return client.IgnoreNotFound(install.Nodes.UnlabelNode(ctx, originalNode.Name, deployOperandsLabel))
				})
			}
		}

		for _, originalNode := range originalNodes {
			err = install.Nodes.LabelNode(ctx, originalNode.Name, deployOperandsLabel, "false")
			framework.ExpectNoError(err, "failed to label node %s %s=false", originalNode.Name, deployOperandsLabel)
		}
		for _, selector := range slices.Concat(e2eoperands.DefaultOperandSelectors, []string{e2eoperands.AppSelector("nvidia-dcgm")}) {
			e2eoperands.WaitForPodsDeleted(ctx, install.Clients, selector)
		}

		for _, originalNode := range originalNodes {
			err = install.Nodes.LabelNode(ctx, originalNode.Name, deployOperandsLabel, "true")
			framework.ExpectNoError(err, "failed to label node %s %s=true", originalNode.Name, deployOperandsLabel)
		}
		e2eoperands.WaitForOperandsReady(ctx, install.Clients, e2eoperands.DefaultOperandSelectors)
	})

	ginkgo.It("migrates driver management from the ClusterPolicy to NVIDIADriver", func(ctx context.Context) {
		legacyDriverPodName := firstLegacyDriverPodName(ctx, install.Clients)

		install.Upgrade(ctx,
			fmt.Sprintf("operator.defaultRuntime=%s", e2eoperator.TestContext.ContainerRuntime),
			"driver.nvidiaDriverCRD.enabled=true",
			"driver.nvidiaDriverCRD.deployDefaultCR=true",
		)

		waitForLegacyDriverDaemonSetDeleted(ctx, install.Clients)
		waitForPodOrphaned(ctx, install.Clients, legacyDriverPodName)
		e2eoperator.WaitForSingleDefaultDriver(ctx, install.Clients, consts.DefaultNVIDIADriverName, driverMigrationStepTimeout)
		e2eoperator.WaitForNodesOwnedByDriver(ctx, install.Clients, consts.DefaultNVIDIADriverName, driverMigrationStepTimeout)
		e2eoperator.WaitForDriverDaemonSets(ctx, install.Clients, consts.DefaultNVIDIADriverName, driverMigrationStepTimeout)
		framework.WaitForPodNotFound(ctx, install.Kube, e2eoperator.TestContext.OperatorNamespace, legacyDriverPodName, driverMigrationStepTimeout)
		e2eoperands.WaitForDriverPodsUpgraded(ctx, install.Clients)
	})

	ginkgo.It("runs a GPU workload after the migration", func(ctx context.Context) {
		e2eworkload.RunGPUWorkload(ctx, install.Clients)
	})
})

func waitForLegacyDriverImageVersion(ctx context.Context, clients e2eoperator.Clients, version string) {
	ginkgo.GinkgoHelper()
	framework.WaitFor(ctx, e2eoperands.DaemonSetUpdateTimeout, fmt.Sprintf("waiting for the ClusterPolicy driver daemonsets to run version %s", version), func(g gomega.Gomega) {
		legacyDriverDaemonSets, err := clients.DaemonSets.List(ctx, e2eoperator.TestContext.OperatorNamespace, e2eoperands.AppSelector(legacyDriverApp))
		g.Expect(err).NotTo(gomega.HaveOccurred())
		e2eoperands.ExpectDriverImageVersion(g, legacyDriverDaemonSets, version)
	})
}

func firstLegacyDriverPodName(ctx context.Context, clients e2eoperator.Clients) string {
	ginkgo.GinkgoHelper()
	legacyDriverPods, err := clients.Pods.List(ctx, e2eoperator.TestContext.OperatorNamespace, e2eoperands.AppSelector(legacyDriverApp))
	framework.ExpectNoError(err, "failed to list the legacy ClusterPolicy driver pods")
	gomega.Expect(legacyDriverPods).NotTo(gomega.BeEmpty(), "legacy ClusterPolicy driver pod not found")
	return legacyDriverPods[0].Name
}

func waitForLegacyDriverDaemonSetDeleted(ctx context.Context, clients e2eoperator.Clients) {
	ginkgo.GinkgoHelper()
	framework.WaitFor(ctx, driverMigrationStepTimeout, "waiting for the ClusterPolicy driver daemonset to be deleted", func(g gomega.Gomega) {
		legacyDriverDaemonSets, err := clients.DaemonSets.List(ctx, e2eoperator.TestContext.OperatorNamespace, e2eoperands.AppSelector(legacyDriverApp))
		g.Expect(err).NotTo(gomega.HaveOccurred())
		g.Expect(legacyDriverDaemonSets).To(gomega.BeEmpty())
	})
}

func waitForPodOrphaned(ctx context.Context, clients e2eoperator.Clients, podName string) {
	ginkgo.GinkgoHelper()
	framework.WaitFor(ctx, driverMigrationStepTimeout, fmt.Sprintf("waiting for pod %s to be orphaned", podName), func(g gomega.Gomega) {
		pod, err := clients.Kube.CoreV1().Pods(e2eoperator.TestContext.OperatorNamespace).Get(ctx, podName, metav1.GetOptions{})
		if apierrors.IsNotFound(err) {
			return
		}
		g.Expect(err).NotTo(gomega.HaveOccurred())
		g.Expect(pod.OwnerReferences).To(gomega.BeEmpty())
	})
}
