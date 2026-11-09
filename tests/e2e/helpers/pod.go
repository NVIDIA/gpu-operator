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

package helpers

import (
	"context"
	"fmt"
	"slices"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	corev1client "k8s.io/client-go/kubernetes/typed/core/v1"
)

type PodClient struct {
	k8sClient corev1client.CoreV1Interface
}

func NewPodClient(k8sClient corev1client.CoreV1Interface) *PodClient {
	return &PodClient{
		k8sClient: k8sClient,
	}
}

func IsPodReady(pod *corev1.Pod) bool {
	return slices.ContainsFunc(pod.Status.Conditions, func(condition corev1.PodCondition) bool {
		return condition.Type == corev1.PodReady && condition.Status == corev1.ConditionTrue
	})
}

func PodStatusSummary(pod *corev1.Pod) string {
	readyContainerCount := 0
	for _, containerStatus := range pod.Status.ContainerStatuses {
		if containerStatus.Ready {
			readyContainerCount++
		}
	}
	return fmt.Sprintf("%s %d/%d %s", pod.Name, readyContainerCount, len(pod.Spec.Containers), kubectlPodStatus(pod))
}

func kubectlPodStatus(pod *corev1.Pod) string {
	if pod.DeletionTimestamp != nil {
		return "Terminating"
	}

	for initContainerIndex, initContainerStatus := range pod.Status.InitContainerStatuses {
		switch {
		case initContainerStatus.State.Terminated != nil && initContainerStatus.State.Terminated.ExitCode == 0:
			continue
		case initContainerStatus.State.Terminated != nil && initContainerStatus.State.Terminated.Reason != "":
			return "Init:" + initContainerStatus.State.Terminated.Reason
		case initContainerStatus.State.Terminated != nil:
			return fmt.Sprintf("Init:ExitCode:%d", initContainerStatus.State.Terminated.ExitCode)
		case initContainerStatus.State.Waiting != nil && initContainerStatus.State.Waiting.Reason != "" && initContainerStatus.State.Waiting.Reason != "PodInitializing":
			return "Init:" + initContainerStatus.State.Waiting.Reason
		default:
			return fmt.Sprintf("Init:%d/%d", initContainerIndex, len(pod.Spec.InitContainers))
		}
	}

	status := string(pod.Status.Phase)
	if pod.Status.Reason != "" {
		status = pod.Status.Reason
	}
	for _, containerStatus := range slices.Backward(pod.Status.ContainerStatuses) {
		switch {
		case containerStatus.State.Waiting != nil && containerStatus.State.Waiting.Reason != "":
			status = containerStatus.State.Waiting.Reason
		case containerStatus.State.Terminated != nil && containerStatus.State.Terminated.Reason != "":
			status = containerStatus.State.Terminated.Reason
		case containerStatus.State.Terminated != nil:
			status = fmt.Sprintf("ExitCode:%d", containerStatus.State.Terminated.ExitCode)
		}
	}
	return status
}

func (c *PodClient) Logs(ctx context.Context, pod *corev1.Pod, logOptions *corev1.PodLogOptions) (string, error) {
	logBytes, err := c.k8sClient.Pods(pod.Namespace).GetLogs(pod.Name, logOptions).DoRaw(ctx)
	if err != nil {
		return "", fmt.Errorf("failed to get logs of pod %s/%s (container %q, previous %t): %w", pod.Namespace, pod.Name, logOptions.Container, logOptions.Previous, err)
	}
	return string(logBytes), nil
}

func (c *PodClient) List(ctx context.Context, namespace, selector string) ([]corev1.Pod, error) {
	podList, err := c.k8sClient.Pods(namespace).List(ctx, metav1.ListOptions{LabelSelector: selector})
	if err != nil {
		return nil, fmt.Errorf("failed to list pods %q in namespace %s: %w", selector, namespace, err)
	}
	return podList.Items, nil
}

func (c *PodClient) DeleteCollection(ctx context.Context, namespace, selector string) error {
	err := c.k8sClient.Pods(namespace).DeleteCollection(ctx, metav1.DeleteOptions{}, metav1.ListOptions{LabelSelector: selector})
	if err != nil {
		return fmt.Errorf("failed to delete pods %q in namespace %s: %w", selector, namespace, err)
	}
	return nil
}
