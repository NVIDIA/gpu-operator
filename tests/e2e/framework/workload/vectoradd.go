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
	resourcev1 "k8s.io/api/resource/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
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

	vectorAddImage          = "nvcr.io/nvidia/k8s/cuda-sample:vectoradd-cuda12.5.0-ubuntu22.04"
	vectorAddContainerName  = "cuda-vector-add"
	vectorAddSuccessMessage = "Test PASSED"

	resourceClaimWorkloadCompletionTimeout = 10 * time.Minute
	resourceClaimWorkloadDeletionTimeout   = 5 * time.Minute
	resourceClaimDeletionTimeout           = 5 * time.Minute
	cleanupTimeoutMargin                   = time.Minute

	gpuResourceName      = "nvidia.com/gpu"
	gpuClaimRequestName  = "gpu"
	podResourceClaimName = "gpu"
)

func newVectorAddPod(podName string) *corev1.Pod {
	return &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      podName,
			Namespace: e2eoperator.WorkloadNamespace,
		},
		Spec: corev1.PodSpec{
			RestartPolicy: corev1.RestartPolicyNever,
			Containers: []corev1.Container{{
				Name:  vectorAddContainerName,
				Image: vectorAddImage,
			}},
		},
	}
}

func RunGPUWorkload(ctx context.Context, clients e2eoperator.Clients) {
	ginkgo.GinkgoHelper()

	workloadPodName := fmt.Sprintf("%s-%s", gpuWorkloadNamePrefix, framework.RandomSuffix())
	workloadPod := newVectorAddPod(workloadPodName)
	workloadPod.Spec.Containers[0].Resources = corev1.ResourceRequirements{
		Limits: corev1.ResourceList{
			gpuResourceName: resource.MustParse("1"),
		},
	}

	ginkgo.DeferCleanup(deleteWorkloadPod, clients, workloadPodName, gpuWorkloadDeletionTimeout)

	_, err := clients.Workloads.DeployPod(ctx, e2eoperator.WorkloadNamespace, workloadPod)
	framework.ExpectNoError(err, "failed to create the GPU workload pod %s/%s", e2eoperator.WorkloadNamespace, workloadPodName)

	waitForVectorAddSuccess(ctx, clients, workloadPod, gpuWorkloadCompletionTimeout)
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

func deleteWorkloadPod(ctx context.Context, clients e2eoperator.Clients, podName string, deletionTimeout time.Duration) {
	ginkgo.GinkgoHelper()

	ginkgo.By(fmt.Sprintf("Deleting workload pod %s/%s", e2eoperator.WorkloadNamespace, podName))
	err := clients.Workloads.Delete(ctx, e2eoperator.WorkloadNamespace, podName)
	framework.ExpectNoError(client.IgnoreNotFound(err), "failed to delete the workload pod %s/%s", e2eoperator.WorkloadNamespace, podName)

	framework.WaitForPodNotFound(ctx, clients.Kube, e2eoperator.WorkloadNamespace, podName, deletionTimeout)
}

func waitForVectorAddSuccess(ctx context.Context, clients e2eoperator.Clients, pod *corev1.Pod, completionTimeout time.Duration) {
	ginkgo.GinkgoHelper()

	namespace, podName := pod.Namespace, pod.Name
	elapsed := framework.WaitFor(ctx, completionTimeout, fmt.Sprintf("Waiting for pod %s/%s to succeed", namespace, podName), func(g gomega.Gomega) {
		currentPod, err := clients.Workloads.Get(ctx, namespace, podName)
		g.Expect(err).NotTo(gomega.HaveOccurred(), "failed to get pod %s/%s", namespace, podName)
		if currentPod.Status.Phase == corev1.PodFailed {
			gomega.StopTrying(fmt.Sprintf("pod %s/%s failed: %s", namespace, podName, describePodFailure(currentPod))).Now()
		}
		g.Expect(currentPod.Status.Phase).To(gomega.Equal(corev1.PodSucceeded), "%s", describePodProgress(currentPod))
	})
	podLogs, err := clients.Pods.Logs(ctx, pod, &corev1.PodLogOptions{Container: vectorAddContainerName})
	framework.ExpectNoError(err, "failed to read the logs of pod %s/%s", namespace, podName)
	gomega.Expect(podLogs).To(gomega.ContainSubstring(vectorAddSuccessMessage), "vectorAdd in pod %s/%s did not report success", namespace, podName)
	ginkgo.GinkgoWriter.Printf("Pod %s/%s succeeded and vectorAdd reported success (%s)\n", namespace, podName, elapsed.Round(time.Second))
}

func describePodProgress(pod *corev1.Pod) string {
	for _, condition := range pod.Status.Conditions {
		if condition.Type == corev1.PodScheduled && condition.Status == corev1.ConditionFalse {
			return fmt.Sprintf("pod %s is %s: not scheduled (%s): %s", pod.Name, pod.Status.Phase, condition.Reason, condition.Message)
		}
	}
	return helpers.PodStatusSummary(pod)
}

func RunVectorAddWithResourceClaim(ctx context.Context, clients e2eoperator.Clients) {
	ginkgo.GinkgoHelper()

	namespace := e2eoperator.WorkloadNamespace
	podName := fmt.Sprintf("%s-claim-%s", gpuWorkloadNamePrefix, framework.RandomSuffix())
	claimName := podName + "-gpu"

	ginkgo.By(fmt.Sprintf("Creating ResourceClaim %s/%s for one GPU", namespace, claimName))
	ginkgo.DeferCleanup(deleteResourceClaim, clients, claimName, resourceClaimDeletionTimeout,
		ginkgo.NodeTimeout(resourceClaimDeletionTimeout+cleanupTimeoutMargin))
	_, err := clients.Kube.ResourceV1().ResourceClaims(namespace).Create(ctx, newGPUResourceClaim(claimName), metav1.CreateOptions{})
	framework.ExpectNoError(err, "failed to create ResourceClaim %s/%s", namespace, claimName)

	ginkgo.By(fmt.Sprintf("Deploying vectorAdd pod %s/%s with that claim", namespace, podName))
	claimPod := newVectorAddPodWithResourceClaim(podName, claimName)
	ginkgo.DeferCleanup(deleteWorkloadPod, clients, podName, resourceClaimWorkloadDeletionTimeout,
		ginkgo.NodeTimeout(resourceClaimWorkloadDeletionTimeout+cleanupTimeoutMargin))
	_, err = clients.Workloads.DeployPod(ctx, namespace, claimPod)
	framework.ExpectNoError(err, "failed to create pod %s/%s", namespace, podName)
	waitForVectorAddSuccess(ctx, clients, claimPod, resourceClaimWorkloadCompletionTimeout)
}

func newGPUResourceClaim(claimName string) *resourcev1.ResourceClaim {
	return &resourcev1.ResourceClaim{
		ObjectMeta: metav1.ObjectMeta{Name: claimName, Namespace: e2eoperator.WorkloadNamespace},
		Spec: resourcev1.ResourceClaimSpec{
			Devices: resourcev1.DeviceClaim{
				Requests: []resourcev1.DeviceRequest{{
					Name: gpuClaimRequestName,
					Exactly: &resourcev1.ExactDeviceRequest{
						DeviceClassName: e2eoperator.GPUDeviceClassName,
						AllocationMode:  resourcev1.DeviceAllocationModeExactCount,
						Count:           1,
					},
				}},
			},
		},
	}
}

func newVectorAddPodWithResourceClaim(podName, claimName string) *corev1.Pod {
	pod := newVectorAddPod(podName)
	pod.Spec.ResourceClaims = []corev1.PodResourceClaim{{
		Name:              podResourceClaimName,
		ResourceClaimName: new(claimName),
	}}
	pod.Spec.Containers[0].Resources.Claims = []corev1.ResourceClaim{{Name: podResourceClaimName}}
	pod.Spec.Tolerations = []corev1.Toleration{{
		Key:      gpuResourceName,
		Operator: corev1.TolerationOpExists,
		Effect:   corev1.TaintEffectNoSchedule,
	}}
	return pod
}

func deleteResourceClaim(ctx context.Context, clients e2eoperator.Clients, claimName string, deletionTimeout time.Duration) {
	ginkgo.GinkgoHelper()

	namespace := e2eoperator.WorkloadNamespace
	ginkgo.By(fmt.Sprintf("Deleting ResourceClaim %s/%s", namespace, claimName))
	resourceClaimClient := clients.Kube.ResourceV1().ResourceClaims(namespace)
	err := resourceClaimClient.Delete(ctx, claimName, metav1.DeleteOptions{})
	framework.ExpectNoError(client.IgnoreNotFound(err), "failed to delete ResourceClaim %s/%s", namespace, claimName)

	elapsed := framework.WaitFor(ctx, deletionTimeout, fmt.Sprintf("Waiting for ResourceClaim %s/%s to be deleted", namespace, claimName), func(g gomega.Gomega) {
		_, err := resourceClaimClient.Get(ctx, claimName, metav1.GetOptions{})
		g.Expect(err).To(gomega.MatchError(apierrors.IsNotFound, "a NotFound error"), "ResourceClaim %s/%s still exists", namespace, claimName)
	})
	ginkgo.GinkgoWriter.Printf("ResourceClaim %s/%s has been deleted (%s)\n", namespace, claimName, elapsed.Round(time.Second))
}
