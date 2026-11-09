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

package workload

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/onsi/ginkgo/v2"
	"github.com/onsi/gomega"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/NVIDIA/gpu-operator/tests/e2e/framework"
	e2eoperator "github.com/NVIDIA/gpu-operator/tests/e2e/framework/operator"
	"github.com/NVIDIA/gpu-operator/tests/e2e/helpers"
)

const (
	gpuWorkloadNamePrefix = "gpu-operator-test"

	gpuWorkloadCompletionTimeout = 45 * time.Minute
	gpuWorkloadDeletionTimeout   = 45 * time.Minute
)

func RunGPUWorkload(ctx context.Context, clients e2eoperator.Clients) {
	ginkgo.GinkgoHelper()

	workloadPodName := fmt.Sprintf("%s-%s", gpuWorkloadNamePrefix, framework.RandomSuffix())
	workloadPod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      workloadPodName,
			Namespace: e2eoperator.WorkloadNamespace,
		},
		Spec: corev1.PodSpec{
			RestartPolicy: corev1.RestartPolicyNever,
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

	ginkgo.DeferCleanup(deleteGPUWorkload, clients, workloadPodName)

	_, err := clients.Workloads.DeployPod(ctx, e2eoperator.WorkloadNamespace, workloadPod)
	framework.ExpectNoError(err, "failed to create the GPU workload pod %s/%s", e2eoperator.WorkloadNamespace, workloadPodName)

	completionElapsed := framework.WaitFor(ctx, gpuWorkloadCompletionTimeout, fmt.Sprintf("Deploying GPU pod %s/%s", e2eoperator.WorkloadNamespace, workloadPodName), func(g gomega.Gomega) {
		currentWorkloadPod, err := clients.Workloads.Get(ctx, e2eoperator.WorkloadNamespace, workloadPodName)
		g.Expect(err).NotTo(gomega.HaveOccurred(), "failed to get GPU workload pod %s/%s", e2eoperator.WorkloadNamespace, workloadPodName)
		if currentWorkloadPod.Status.Phase == corev1.PodFailed {
			gomega.StopTrying(fmt.Sprintf("GPU workload pod %s/%s failed: %s", e2eoperator.WorkloadNamespace, workloadPodName, describePodFailure(currentWorkloadPod))).Now()
		}
		g.Expect(currentWorkloadPod.Status.Phase).To(gomega.Equal(corev1.PodSucceeded), "%s", helpers.PodStatusSummary(currentWorkloadPod))
	})

	workloadLogs, err := clients.Pods.Logs(ctx, workloadPod, &corev1.PodLogOptions{Container: workloadPod.Spec.Containers[0].Name})
	framework.ExpectNoError(err, "failed to read the logs of GPU workload pod %s/%s", e2eoperator.WorkloadNamespace, workloadPodName)
	gomega.Expect(workloadLogs).To(gomega.ContainSubstring("Test PASSED"), "vectorAdd did not report success")
	ginkgo.GinkgoWriter.Printf("Pod %s/%s completed successfully (%s)\n", e2eoperator.WorkloadNamespace, workloadPodName, completionElapsed.Round(time.Second))
}

func describePodFailure(pod *corev1.Pod) string {
	failureDetails := []string{helpers.PodStatusSummary(pod)}
	for _, containerStatus := range pod.Status.ContainerStatuses {
		if containerStatus.State.Terminated != nil {
			failureDetails = append(failureDetails, fmt.Sprintf("container %s exited with code %d", containerStatus.Name, containerStatus.State.Terminated.ExitCode))
		}
	}
	if pod.Status.Message != "" {
		failureDetails = append(failureDetails, pod.Status.Message)
	}
	return strings.Join(failureDetails, ", ")
}

func deleteGPUWorkload(ctx context.Context, clients e2eoperator.Clients, workloadPodName string) {
	ginkgo.GinkgoHelper()

	ginkgo.By(fmt.Sprintf("Deleting GPU pod %s/%s", e2eoperator.WorkloadNamespace, workloadPodName))
	err := clients.Workloads.Delete(ctx, e2eoperator.WorkloadNamespace, workloadPodName)
	framework.ExpectNoError(client.IgnoreNotFound(err), "failed to delete the GPU workload pod")

	framework.WaitForPodNotFound(ctx, clients.Kube, e2eoperator.WorkloadNamespace, workloadPodName, gpuWorkloadDeletionTimeout)
}
