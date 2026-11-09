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
	"strings"
	"time"

	"github.com/onsi/ginkgo/v2"
	"github.com/onsi/gomega"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
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
			ginkgo.By(fmt.Sprintf("Updating the ClusterPolicy driver version to %s", e2eoperator.TestContext.TargetDriverVersion))
			err := install.ClusterPolicies.UpdateDriverVersion(ctx, e2eoperator.ClusterPolicyName, e2eoperator.TestContext.TargetDriverVersion)
			framework.ExpectNoError(err, "failed to update the ClusterPolicy driver version to %s", e2eoperator.TestContext.TargetDriverVersion)

			waitForLegacyDriverImageVersion(ctx, install.Clients, e2eoperator.TestContext.TargetDriverVersion)

			ginkgo.By("Deleting the driver pods to trigger the update")
			err = install.Pods.DeleteCollection(ctx, e2eoperator.TestContext.OperatorNamespace, helpers.DriverComponentSelector)
			framework.ExpectNoError(err, "failed to delete the driver pods")
			e2eoperands.WaitForDriverPodsUpgraded(ctx, install.Clients)
		})

		ginkgo.It("passes env vars to the device plugin", func(ctx context.Context) {
			testEnvVar := nvidiav1.EnvVar{Name: "MY_TEST_ENV_NAME", Value: "test"}
			ginkgo.By(fmt.Sprintf("Setting env %s=%s on the device plugin", testEnvVar.Name, testEnvVar.Value))
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
			ginkgo.By(fmt.Sprintf("Setting the ClusterPolicy MIG strategy to %s", migStrategy))
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
			ginkgo.By("Enabling standalone nvidia-dcgm in the ClusterPolicy")
			framework.ExpectNoError(install.ClusterPolicies.EnableDCGM(ctx, e2eoperator.ClusterPolicyName), "failed to enable standalone DCGM")

			e2eoperands.WaitForPodsReady(ctx, install.Clients, e2eoperands.AppSelector("nvidia-dcgm"))
			e2eoperands.WaitForDaemonSetEnv(ctx, install.Clients, "nvidia-dcgm-exporter", dcgmRemoteHostEngineEnvName, dcgmRemoteHostEngineEnvValue)
			e2eoperands.WaitForDaemonSetRolledOut(ctx, install.Clients, "nvidia-dcgm-exporter")
			e2eoperands.WaitForPodsReady(ctx, install.Clients, e2eoperands.AppSelector("nvidia-dcgm-exporter"))

			ginkgo.By("Checking that the nvidia-dcgm service keeps traffic on the local node")
			dcgmService, err := install.Kube.CoreV1().Services(e2eoperator.TestContext.OperatorNamespace).Get(ctx, "nvidia-dcgm", metav1.GetOptions{})
			framework.ExpectNoError(err, "failed to get the nvidia-dcgm service")
			gomega.Expect(dcgmService.Spec.InternalTrafficPolicy).To(gomega.HaveValue(gomega.Equal(corev1.ServiceInternalTrafficPolicyLocal)), "internalTrafficPolicy of service nvidia-dcgm")
			ginkgo.GinkgoWriter.Printf("Service nvidia-dcgm has internalTrafficPolicy %s\n", *dcgmService.Spec.InternalTrafficPolicy)
		})

		ginkgo.It("shares the GPU with time-slicing", func(ctx context.Context) {
			ginkgo.By("Listing the GPU nodes")
			gpuNodes, err := install.Nodes.ListGPUNodes(ctx)
			framework.ExpectNoError(err, "failed to list the GPU nodes")
			gomega.Expect(gpuNodes).NotTo(gomega.BeEmpty(), "no GPU nodes found")
			productName, physicalGPUCount := gpuProductAndCount(gpuNodes)
			productNameByNodeName := gpuProductNameByNodeName(gpuNodes)
			ginkgo.GinkgoWriter.Printf("Found %d GPU node(s) with %d %s GPU(s)\n", len(gpuNodes), physicalGPUCount, productName)

			ginkgo.By(fmt.Sprintf("Creating ConfigMap %s with %d time-slicing replicas per GPU", timeSlicingConfigMapName, timeSlicingReplicas))
			createTimeSlicingConfig(ctx, install.Clients)
			ginkgo.By(fmt.Sprintf("Updating the device plugin to use ConfigMap %s for GPU sharing", timeSlicingConfigMapName))
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

			gpuSharingReplicas := physicalGPUCount + 1
			ginkgo.By(fmt.Sprintf("Creating deployment %s with %d replicas on the %d time-sliced %s GPU(s)", gpuSharingWorkloadName, gpuSharingReplicas, physicalGPUCount, sharedGPUProductName(productName)))
			ginkgo.DeferCleanup(deleteGPUSharingDeployment, install.Clients)
			gpuSharingDeployment := newGPUSharingDeployment(sharedGPUProductName(productName), gpuSharingReplicas)
			_, err = install.Kube.AppsV1().Deployments(e2eoperator.TestContext.OperatorNamespace).Create(ctx, gpuSharingDeployment, metav1.CreateOptions{})
			framework.ExpectNoError(err, "failed to create deployment %s", gpuSharingWorkloadName)
			waitForGPUSharingDeploymentAvailable(ctx, install.Clients)
		})

		ginkgo.It("removes and restores GFD", func(ctx context.Context) {
			ginkgo.By("Disabling gpu-feature-discovery in the ClusterPolicy")
			err := install.ClusterPolicies.DisableGFD(ctx, e2eoperator.ClusterPolicyName)
			framework.ExpectNoError(err, "failed to disable GFD")
			e2eoperands.WaitForPodsDeleted(ctx, install.Clients, e2eoperands.AppSelector("gpu-feature-discovery"))

			ginkgo.By("Enabling gpu-feature-discovery in the ClusterPolicy")
			err = install.ClusterPolicies.EnableGFD(ctx, e2eoperator.ClusterPolicyName)
			framework.ExpectNoError(err, "failed to enable GFD")
			e2eoperands.WaitForPodsReady(ctx, install.Clients, e2eoperands.AppSelector("gpu-feature-discovery"))
		})

		ginkgo.It("removes and restores the DCGM exporter", func(ctx context.Context) {
			ginkgo.By("Disabling nvidia-dcgm-exporter in the ClusterPolicy")
			err := install.ClusterPolicies.DisableDCGMExporter(ctx, e2eoperator.ClusterPolicyName)
			framework.ExpectNoError(err, "failed to disable the DCGM exporter")
			e2eoperands.WaitForPodsDeleted(ctx, install.Clients, e2eoperands.AppSelector("nvidia-dcgm-exporter"))

			ginkgo.By("Enabling nvidia-dcgm-exporter in the ClusterPolicy")
			err = install.ClusterPolicies.EnableDCGMExporter(ctx, e2eoperator.ClusterPolicyName)
			framework.ExpectNoError(err, "failed to enable the DCGM exporter")
			e2eoperands.WaitForPodsReady(ctx, install.Clients, e2eoperands.AppSelector("nvidia-dcgm-exporter"))
		})

		ginkgo.It("applies custom labels to every operand pod", func(ctx context.Context) {
			customLabels := map[string]string{"cloudprovider": "aws", "platform": "kubernetes"}
			ginkgo.By(fmt.Sprintf("Setting custom labels %s on the operand daemonsets", labels.Set(customLabels)))
			err := install.ClusterPolicies.Modify(ctx, e2eoperator.ClusterPolicyName, func(clusterPolicy *nvidiav1.ClusterPolicy) {
				clusterPolicy.Spec.Daemonsets.Labels = customLabels
			})
			framework.ExpectNoError(err, "failed to set custom operand labels")

			e2eoperands.WaitForOperandPodLabels(ctx, install.Clients, e2eoperands.DefaultOperandSelectors, customLabels)
			e2eoperands.WaitForDriverUpgradeDone(ctx, install.Clients)
			e2eoperands.WaitForOperandsReady(ctx, install.Clients, e2eoperands.DefaultOperandSelectors)
		})
	})

	ginkgo.It("recovers after the operator restarts", func(ctx context.Context) {
		ginkgo.By("Checking the state of the GPU Operator")
		operatorPods, err := install.Pods.List(ctx, e2eoperator.TestContext.OperatorNamespace, e2eoperator.OperatorComponentSelector)
		framework.ExpectNoError(err, "failed to list the operator pods")
		gomega.Expect(operatorPods).NotTo(gomega.BeEmpty(), "operator pod not found")
		operatorPodNames := make([]string, 0, len(operatorPods))
		for _, operatorPod := range operatorPods {
			ginkgo.GinkgoWriter.Printf("GPU Operator pod %s is %s\n", operatorPod.Name, operatorPod.Status.Phase)
			operatorPodNames = append(operatorPodNames, operatorPod.Name)
		}

		ginkgo.By(fmt.Sprintf("Deleting GPU Operator pod(s) %s", strings.Join(operatorPodNames, ", ")))
		err = install.Pods.DeleteCollection(ctx, e2eoperator.TestContext.OperatorNamespace, e2eoperator.OperatorComponentSelector)
		framework.ExpectNoError(err, "failed to delete the operator pods")
		e2eoperator.WaitForReadyReplacementOperatorPod(ctx, install.Clients, operatorPods)
	})

	ginkgo.It("removes operands when nodes opt out and restores them when they opt back in", func(ctx context.Context) {
		originalNodes, err := install.Nodes.ListNodes(ctx)
		framework.ExpectNoError(err, "failed to list nodes")
		nodeNames := make([]string, 0, len(originalNodes))
		for _, originalNode := range originalNodes {
			nodeNames = append(nodeNames, originalNode.Name)
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

		ginkgo.By(fmt.Sprintf("Disabling GPU Operator operands on node(s) %s", strings.Join(nodeNames, ", ")))
		for _, originalNode := range originalNodes {
			err = install.Nodes.LabelNode(ctx, originalNode.Name, deployOperandsLabel, "false")
			framework.ExpectNoError(err, "failed to label node %s %s=false", originalNode.Name, deployOperandsLabel)
		}
		for _, selector := range slices.Concat(e2eoperands.DefaultOperandSelectors, []string{e2eoperands.AppSelector("nvidia-dcgm")}) {
			e2eoperands.WaitForPodsDeleted(ctx, install.Clients, selector)
		}

		ginkgo.By(fmt.Sprintf("Enabling GPU Operator operands on node(s) %s", strings.Join(nodeNames, ", ")))
		for _, originalNode := range originalNodes {
			err = install.Nodes.LabelNode(ctx, originalNode.Name, deployOperandsLabel, "true")
			framework.ExpectNoError(err, "failed to label node %s %s=true", originalNode.Name, deployOperandsLabel)
		}
		e2eoperands.WaitForOperandsReady(ctx, install.Clients, e2eoperands.DefaultOperandSelectors)
	})

	ginkgo.It("migrates driver management from the ClusterPolicy to NVIDIADriver", func(ctx context.Context) {
		ginkgo.By("Finding the ClusterPolicy-managed driver pod")
		legacyDriverSelector := e2eoperands.AppSelector(legacyDriverApp)
		legacyDriverPods, err := install.Pods.List(ctx, e2eoperator.TestContext.OperatorNamespace, legacyDriverSelector)
		framework.ExpectNoError(err, "failed to list the legacy ClusterPolicy driver pods")
		gomega.Expect(legacyDriverPods).NotTo(gomega.BeEmpty(), "legacy ClusterPolicy driver pod not found")
		legacyDriverPodName := legacyDriverPods[0].Name
		ginkgo.GinkgoWriter.Printf("Legacy driver pod %s is %s\n", legacyDriverPodName, legacyDriverPods[0].Status.Phase)

		ginkgo.By("Migrating the GPU Operator release from ClusterPolicy driver management to NVIDIADriver")
		install.Upgrade(ctx,
			fmt.Sprintf("operator.defaultRuntime=%s", e2eoperator.TestContext.ContainerRuntime),
			"driver.nvidiaDriverCRD.enabled=true",
			"driver.nvidiaDriverCRD.deployDefaultCR=true",
		)

		e2eoperands.WaitForDaemonSetsDeleted(ctx, install.Clients, legacyDriverSelector, driverMigrationStepTimeout)
		framework.WaitForPodOrphaned(ctx, install.Kube, e2eoperator.TestContext.OperatorNamespace, legacyDriverPodName, driverMigrationStepTimeout)
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
	elapsed := framework.WaitFor(ctx, e2eoperands.DaemonSetUpdateTimeout, fmt.Sprintf("Waiting for the %s image to update to version %s", legacyDriverApp, version), func(g gomega.Gomega) {
		legacyDriverDaemonSets, err := clients.DaemonSets.List(ctx, e2eoperator.TestContext.OperatorNamespace, e2eoperands.AppSelector(legacyDriverApp))
		g.Expect(err).NotTo(gomega.HaveOccurred(), "failed to list the %s daemonsets", legacyDriverApp)
		e2eoperands.ExpectDriverImageVersion(g, legacyDriverDaemonSets, version)
	})
	ginkgo.GinkgoWriter.Printf("Driver daemonset %s image updated to version %s (%s)\n", legacyDriverApp, version, elapsed.Round(time.Second))
}
