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
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"time"

	"github.com/onsi/ginkgo/v2"
	"github.com/onsi/gomega"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/kubernetes"

	nvidiav1 "github.com/NVIDIA/gpu-operator/api/nvidia/v1"
	"github.com/NVIDIA/gpu-operator/internal/consts"
	"github.com/NVIDIA/gpu-operator/tests/e2e/framework"
	e2eoperands "github.com/NVIDIA/gpu-operator/tests/e2e/framework/operands"
	e2eoperator "github.com/NVIDIA/gpu-operator/tests/e2e/framework/operator"
	e2eworkload "github.com/NVIDIA/gpu-operator/tests/e2e/framework/workload"
	"github.com/NVIDIA/gpu-operator/tests/e2e/helpers"
)

const (
	daemonSetUpdateTimeout     = 2 * time.Minute
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
			err := install.Clients.ClusterPolicies.UpdateDriverVersion(ctx, e2eoperator.ClusterPolicyName, e2eoperator.TestContext.TargetDriverVersion)
			framework.ExpectNoError(err, "failed to update the ClusterPolicy driver version to %s", e2eoperator.TestContext.TargetDriverVersion)

			listLegacyDriverDaemonSets := func(ctx context.Context) ([]appsv1.DaemonSet, error) {
				return install.Clients.DaemonSets.List(ctx, e2eoperator.TestContext.OperatorNamespace, e2eoperands.AppSelector(legacyDriverApp))
			}
			e2eoperands.WaitForDriverImageVersion(ctx, daemonSetUpdateTimeout, listLegacyDriverDaemonSets, e2eoperator.TestContext.TargetDriverVersion)
			e2eoperands.RolloutDriverPods(ctx, install.Clients)
		})

		ginkgo.It("passes env vars to the device plugin", func(ctx context.Context) {
			testEnvVar := nvidiav1.EnvVar{Name: "MY_TEST_ENV_NAME", Value: "test"}
			err := install.Clients.ClusterPolicies.Modify(ctx, e2eoperator.ClusterPolicyName, func(clusterPolicy *nvidiav1.ClusterPolicy) {
				clusterPolicy.Spec.DevicePlugin.Env = []nvidiav1.EnvVar{testEnvVar}
			})
			framework.ExpectNoError(err, "failed to set the device plugin env")

			e2eoperands.WaitForDaemonSetEnv(ctx, install.Clients, "nvidia-device-plugin-daemonset", testEnvVar.Name, testEnvVar.Value)
			e2eoperands.WaitForDaemonSetRolledOut(ctx, install.Clients, "nvidia-device-plugin-daemonset")
			e2eoperands.WaitForPodsReady(ctx, install.Clients, e2eoperands.AppSelector("nvidia-device-plugin-daemonset"))
		})

		ginkgo.It("passes the MIG strategy to GFD and the device plugin", func(ctx context.Context) {
			migStrategy := string(nvidiav1.MIGStrategyMixed)
			err := install.Clients.ClusterPolicies.SetMIGStrategy(ctx, e2eoperator.ClusterPolicyName, migStrategy)
			framework.ExpectNoError(err, "failed to set the MIG strategy to %s", migStrategy)

			e2eoperands.WaitForDaemonSetEnv(ctx, install.Clients, "gpu-feature-discovery", "MIG_STRATEGY", migStrategy)
			e2eoperands.WaitForDaemonSetEnv(ctx, install.Clients, "nvidia-device-plugin-daemonset", "MIG_STRATEGY", migStrategy)
			for _, app := range []string{"gpu-feature-discovery", "nvidia-device-plugin-daemonset"} {
				e2eoperands.WaitForDaemonSetRolledOut(ctx, install.Clients, app)
				e2eoperands.WaitForPodsReady(ctx, install.Clients, e2eoperands.AppSelector(app))
			}
		})

		ginkgo.It("deploys standalone DCGM with a node-local service", func(ctx context.Context) {
			framework.ExpectNoError(install.Clients.ClusterPolicies.EnableDCGM(ctx, e2eoperator.ClusterPolicyName), "failed to enable standalone DCGM")

			e2eoperands.WaitForPodsReady(ctx, install.Clients, e2eoperands.AppSelector("nvidia-dcgm"))
			e2eoperands.WaitForDaemonSetEnv(ctx, install.Clients, "nvidia-dcgm-exporter", dcgmRemoteHostEngineEnvName, dcgmRemoteHostEngineEnvValue)
			e2eoperands.WaitForDaemonSetRolledOut(ctx, install.Clients, "nvidia-dcgm-exporter")
			e2eoperands.WaitForPodsReady(ctx, install.Clients, e2eoperands.AppSelector("nvidia-dcgm-exporter"))

			dcgmService, err := install.Clients.Kube.CoreV1().Services(e2eoperator.TestContext.OperatorNamespace).Get(ctx, "nvidia-dcgm", metav1.GetOptions{})
			framework.ExpectNoError(err, "failed to get the nvidia-dcgm service")
			gomega.Expect(dcgmService.Spec.InternalTrafficPolicy).To(gomega.HaveValue(gomega.Equal(corev1.ServiceInternalTrafficPolicyLocal)))
		})

		ginkgo.It("shares the GPU with time-slicing", func(ctx context.Context) {
			testGPUSharing(ctx, install.Clients)
		})

		ginkgo.It("removes and restores GFD", func(ctx context.Context) {
			e2eoperands.RemoveAndRestoreOperand(ctx, install.Clients, "gpu-feature-discovery", onClusterPolicy(install.Clients.ClusterPolicies.DisableGFD), onClusterPolicy(install.Clients.ClusterPolicies.EnableGFD))
		})

		ginkgo.It("removes and restores the DCGM exporter", func(ctx context.Context) {
			e2eoperands.RemoveAndRestoreOperand(ctx, install.Clients, "nvidia-dcgm-exporter", onClusterPolicy(install.Clients.ClusterPolicies.DisableDCGMExporter), onClusterPolicy(install.Clients.ClusterPolicies.EnableDCGMExporter))
		})

		ginkgo.It("applies custom labels to every operand pod", func(ctx context.Context) {
			customLabels := map[string]string{"cloudprovider": "aws", "platform": "kubernetes"}
			err := install.Clients.ClusterPolicies.Modify(ctx, e2eoperator.ClusterPolicyName, func(clusterPolicy *nvidiav1.ClusterPolicy) {
				clusterPolicy.Spec.Daemonsets.Labels = customLabels
			})
			framework.ExpectNoError(err, "failed to set custom operand labels")

			e2eoperands.WaitForOperandPodLabels(ctx, install.Clients, e2eoperands.DefaultOperandSelectors, customLabels)
			e2eoperands.WaitForDriverUpgradeDone(ctx, install.Clients)
			e2eoperands.WaitForOperandsReady(ctx, install.Clients, e2eoperands.DefaultOperandSelectors)
		})
	})

	ginkgo.It("recovers after the operator restarts", func(ctx context.Context) {
		e2eoperator.RestartOperator(ctx, install.Clients)
	})

	ginkgo.It("removes operands when nodes opt out and restores them when they opt back in", func(ctx context.Context) {
		originalNodes, err := install.Clients.Nodes.ListNodes(ctx)
		framework.ExpectNoError(err, "failed to list nodes")
		ginkgo.DeferCleanup(restoreNodeLabel, install.Clients.Kube, originalNodes, deployOperandsLabel)

		err = install.Clients.Nodes.LabelAllNodes(ctx, deployOperandsLabel, "false")
		framework.ExpectNoError(err, "failed to label all nodes %s=false", deployOperandsLabel)
		for _, selector := range slices.Concat(e2eoperands.DefaultOperandSelectors, []string{e2eoperands.AppSelector("nvidia-dcgm")}) {
			e2eoperands.WaitForPodsDeleted(ctx, install.Clients, selector)
		}

		err = install.Clients.Nodes.LabelAllNodes(ctx, deployOperandsLabel, "true")
		framework.ExpectNoError(err, "failed to label all nodes %s=true", deployOperandsLabel)
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
		framework.WaitForPodNotFound(ctx, install.Clients.Kube, e2eoperator.TestContext.OperatorNamespace, legacyDriverPodName, driverMigrationStepTimeout)
		e2eoperands.WaitForPodsReady(ctx, install.Clients, helpers.DriverComponentSelector)
		e2eoperands.WaitForDriverUpgradeDone(ctx, install.Clients)
	})

	ginkgo.It("runs a GPU workload after the migration", func(ctx context.Context) {
		e2eworkload.RunGPUWorkload(ctx, install.Clients)
	})
})

func onClusterPolicy(update func(ctx context.Context, clusterPolicyName string) error) func(context.Context) error {
	return func(ctx context.Context) error {
		return update(ctx, e2eoperator.ClusterPolicyName)
	}
}

func restoreNodeLabel(ctx context.Context, kubeClient kubernetes.Interface, originalNodes []corev1.Node, labelKey string) error {
	var restoreErrs []error
	for _, originalNode := range originalNodes {
		var originalValue any
		if labelValue, hasLabel := originalNode.Labels[labelKey]; hasLabel {
			originalValue = labelValue
		}

		patch, err := json.Marshal(map[string]any{
			"metadata": map[string]any{
				"labels": map[string]any{labelKey: originalValue},
			},
		})
		if err != nil {
			restoreErrs = append(restoreErrs, fmt.Errorf("failed to build the label patch for node %s: %w", originalNode.Name, err))
			continue
		}

		_, err = kubeClient.CoreV1().Nodes().Patch(ctx, originalNode.Name, types.MergePatchType, patch, metav1.PatchOptions{})
		if err != nil && !apierrors.IsNotFound(err) {
			restoreErrs = append(restoreErrs, fmt.Errorf("failed to restore label %s on node %s: %w", labelKey, originalNode.Name, err))
		}
	}

	return errors.Join(restoreErrs...)
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
