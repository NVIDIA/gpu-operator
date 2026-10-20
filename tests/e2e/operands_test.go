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
	"time"

	"github.com/onsi/ginkgo/v2"
	"github.com/onsi/gomega"

	"github.com/NVIDIA/k8s-operator-libs/pkg/upgrade"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/NVIDIA/gpu-operator/tests/e2e/framework"
	e2elog "github.com/NVIDIA/gpu-operator/tests/e2e/framework/logs"
	"github.com/NVIDIA/gpu-operator/tests/e2e/helpers"
)

const (
	gpuWorkloadName      = "gpu-operator-test"
	gpuWorkloadNamespace = "default"

	driverUpgradeStateLabel = "nvidia.com/gpu-driver-upgrade-state"
)

// operandSelectors match the pods of the operands every install is expected to run. The driver
// pods of both ClusterPolicy and NVIDIADriver carry helpers.DriverComponentSelector.
var operandSelectors = []string{
	helpers.DriverComponentSelector,
	appSelector("nvidia-container-toolkit-daemonset"),
	appSelector("nvidia-device-plugin-daemonset"),
	appSelector("nvidia-dcgm-exporter"),
	appSelector("gpu-feature-discovery"),
	appSelector("nvidia-operator-validator"),
}

// restartCheckedOperandSelectors match the operands whose containers must not restart repeatedly.
var restartCheckedOperandSelectors = []string{
	helpers.DriverComponentSelector,
	appSelector("nvidia-container-toolkit-daemonset"),
	appSelector("nvidia-device-plugin-daemonset"),
	appSelector("gpu-feature-discovery"),
}

func appSelector(app string) string {
	return "app=" + app
}

// waitForPodsReady waits until at least one pod matches selector and every match is Ready and not terminating.
func waitForPodsReady(ctx context.Context, clients clusterClients, selector string) {
	ginkgo.GinkgoHelper()
	waitFor(ctx, operandReadyTimeout, fmt.Sprintf("waiting for pods %q in %s to be ready", selector, tcfg.namespace), func(g gomega.Gomega) {
		pods, err := clients.pods.List(ctx, tcfg.namespace, selector)
		g.Expect(err).NotTo(gomega.HaveOccurred())
		g.Expect(pods).NotTo(gomega.BeEmpty(), "no pods match %q", selector)
		for _, pod := range pods {
			g.Expect(helpers.IsPodReady(&pod)).To(gomega.BeTrue(), "pod %s is not ready", pod.Name)
			g.Expect(pod.DeletionTimestamp).To(gomega.BeNil(), "pod %s is terminating", pod.Name)
		}
	})
}

func waitForPodsDeleted(ctx context.Context, clients clusterClients, selector string) {
	ginkgo.GinkgoHelper()
	waitFor(ctx, podDeletionTimeout, fmt.Sprintf("waiting for pods %q in %s to be deleted", selector, tcfg.namespace), func(g gomega.Gomega) {
		pods, err := clients.pods.List(ctx, tcfg.namespace, selector)
		g.Expect(err).NotTo(gomega.HaveOccurred())
		g.Expect(pods).To(gomega.BeEmpty(), "pods matching %q still exist", selector)
	})
}

// expectNoRepeatedRestarts allows one restart of the first container, matching the bash
// check_no_restarts, because operands may restart once while the driver is being installed.
func expectNoRepeatedRestarts(ctx context.Context, clients clusterClients, selector string) {
	ginkgo.GinkgoHelper()

	pods, err := clients.pods.List(ctx, tcfg.namespace, selector)
	framework.ExpectNoError(err, "failed to list pods %q in %s", selector, tcfg.namespace)
	gomega.Expect(pods).NotTo(gomega.BeEmpty(), "no pods match %q", selector)

	for _, pod := range pods {
		if len(pod.Status.ContainerStatuses) == 0 {
			continue
		}
		firstContainerStatus := pod.Status.ContainerStatuses[0]
		restartCount := firstContainerStatus.RestartCount
		if restartCount > 1 {
			previousLogs, err := clients.pods.Logs(ctx, &pod, firstContainerStatus.Name, true)
			if err != nil {
				e2elog.Logf("WARN: failed to read previous logs of pod %s/%s: %v", pod.Namespace, pod.Name, err)
			} else {
				e2elog.Logf("previous logs of pod %s/%s:\n%s", pod.Namespace, pod.Name, previousLogs)
			}
			ginkgo.Fail(fmt.Sprintf("pod %s/%s restarted %d times", pod.Namespace, pod.Name, restartCount))
		}
	}
}

func waitForOperandsReady(ctx context.Context, clients clusterClients) {
	ginkgo.GinkgoHelper()
	for _, selector := range operandSelectors {
		waitForPodsReady(ctx, clients, selector)
	}
}

func expectNoRepeatedOperandRestarts(ctx context.Context, clients clusterClients) {
	ginkgo.GinkgoHelper()
	for _, selector := range restartCheckedOperandSelectors {
		expectNoRepeatedRestarts(ctx, clients, selector)
	}
}

func waitForDriverUpgradeDone(ctx context.Context, clients clusterClients) {
	ginkgo.GinkgoHelper()
	waitFor(ctx, driverUpgradeTimeout, "waiting for the driver upgrade to complete on every GPU node", func(g gomega.Gomega) {
		gpuNodes, err := clients.nodes.ListGPUNodes(ctx)
		g.Expect(err).NotTo(gomega.HaveOccurred())
		g.Expect(gpuNodes).NotTo(gomega.BeEmpty(), "no GPU nodes found")
		for _, node := range gpuNodes {
			g.Expect(node.Labels).To(gomega.HaveKeyWithValue(driverUpgradeStateLabel, upgrade.UpgradeStateDone), "node %s", node.Name)
		}
	})
}

// rolloutDriverPods deletes the driver pods because the driver daemonset uses the OnDelete
// update strategy, then waits for the upgrade to finish and the replacements to be ready.
func rolloutDriverPods(ctx context.Context, clients clusterClients) {
	ginkgo.GinkgoHelper()
	err := clients.pods.DeleteCollection(ctx, tcfg.namespace, helpers.DriverComponentSelector)
	framework.ExpectNoError(err, "failed to delete pods %q in %s", helpers.DriverComponentSelector, tcfg.namespace)
	// The node keeps the previous rollout's upgrade-done state until the upgrade controller sees the
	// new pod, so the pod is checked first.
	waitForPodsReady(ctx, clients, helpers.DriverComponentSelector)
	waitForDriverUpgradeDone(ctx, clients)
}

// waitForDaemonSetImage waits until listDaemonSets returns at least one daemonset and the
// first container image of each contains version.
func waitForDaemonSetImage(ctx context.Context, timeout time.Duration, listDaemonSets func(ctx context.Context) ([]appsv1.DaemonSet, error), version string) {
	ginkgo.GinkgoHelper()
	waitFor(ctx, timeout, fmt.Sprintf("waiting for the daemonsets to run version %s", version), func(g gomega.Gomega) {
		daemonSets, err := listDaemonSets(ctx)
		g.Expect(err).NotTo(gomega.HaveOccurred())
		g.Expect(daemonSets).NotTo(gomega.BeEmpty(), "no daemonsets found")
		for _, daemonSet := range daemonSets {
			g.Expect(daemonSet.Spec.Template.Spec.Containers[0].Image).To(gomega.ContainSubstring(version), "daemonset %s", daemonSet.Name)
		}
	})
}

func restartOperator(ctx context.Context, clients clusterClients) {
	ginkgo.GinkgoHelper()

	operatorPods, err := clients.pods.List(ctx, tcfg.namespace, operatorComponentSelector)
	framework.ExpectNoError(err, "failed to list the operator pods")
	gomega.Expect(operatorPods).NotTo(gomega.BeEmpty(), "operator pod not found")
	deletedPodUIDs := make(map[types.UID]bool, len(operatorPods))
	for _, pod := range operatorPods {
		deletedPodUIDs[pod.UID] = true
	}

	err = clients.pods.DeleteCollection(ctx, tcfg.namespace, operatorComponentSelector)
	framework.ExpectNoError(err, "failed to delete the operator pods")

	waitFor(ctx, operatorRestartTimeout, "waiting for a replacement operator pod to run", func(g gomega.Gomega) {
		pods, err := clients.pods.List(ctx, tcfg.namespace, operatorComponentSelector)
		g.Expect(err).NotTo(gomega.HaveOccurred())
		runningReplacementCount := 0
		for _, pod := range pods {
			if !deletedPodUIDs[pod.UID] && pod.Status.Phase == corev1.PodRunning && pod.DeletionTimestamp == nil {
				runningReplacementCount++
			}
		}
		g.Expect(runningReplacementCount).To(gomega.BeNumerically(">", 0), "no replacement operator pod is running")
	})
}

func runGPUWorkload(ctx context.Context, clients clusterClients) {
	ginkgo.GinkgoHelper()

	workloadPod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      gpuWorkloadName,
			Namespace: gpuWorkloadNamespace,
		},
		Spec: corev1.PodSpec{
			RestartPolicy: corev1.RestartPolicyOnFailure,
			Containers: []corev1.Container{{
				Name:  "cuda-vector-add",
				Image: "nvcr.io/nvidia/k8s/cuda-sample:vectoradd-cuda12.5.0-ubuntu22.04",
				Resources: corev1.ResourceRequirements{
					Limits: corev1.ResourceList{
						"nvidia.com/gpu": resource.MustParse("1"),
					},
				},
			}},
		},
	}

	workloadClient := helpers.NewWorkloadClient(clients.kube)
	_, err := workloadClient.DeployPod(ctx, gpuWorkloadNamespace, workloadPod)
	framework.ExpectNoError(err, "failed to create the GPU workload pod")
	ginkgo.DeferCleanup(deleteGPUWorkload, clients)

	err = workloadClient.WaitForCompletion(ctx, gpuWorkloadNamespace, gpuWorkloadName, gpuWorkloadTimeout)
	framework.ExpectNoError(err, "GPU workload pod %s/%s did not complete", gpuWorkloadNamespace, gpuWorkloadName)

	workloadLogs, err := clients.pods.Logs(ctx, workloadPod, workloadPod.Spec.Containers[0].Name, false)
	framework.ExpectNoError(err, "failed to read the logs of GPU workload pod %s/%s", gpuWorkloadNamespace, gpuWorkloadName)
	gomega.Expect(workloadLogs).To(gomega.ContainSubstring("Test PASSED"), "vectorAdd did not report success")
}

func deleteGPUWorkload(ctx context.Context, clients clusterClients) {
	ginkgo.GinkgoHelper()

	err := helpers.NewWorkloadClient(clients.kube).Delete(ctx, gpuWorkloadNamespace, gpuWorkloadName)
	framework.ExpectNoError(client.IgnoreNotFound(err), "failed to delete the GPU workload pod")

	waitForNotFound(ctx, podDeletionTimeout, "waiting for the GPU workload pod to be deleted", func(ctx context.Context) error {
		_, err := clients.kube.CoreV1().Pods(gpuWorkloadNamespace).Get(ctx, gpuWorkloadName, metav1.GetOptions{})
		return err
	})
}
