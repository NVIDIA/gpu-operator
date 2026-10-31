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
