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
	"time"

	appsv1 "k8s.io/api/apps/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/client-go/kubernetes"
)

type DaemonSetClient struct {
	client kubernetes.Interface
}

func NewDaemonSetClient(client kubernetes.Interface) *DaemonSetClient {
	return &DaemonSetClient{
		client: client,
	}
}

func (h *DaemonSetClient) GetByLabel(ctx context.Context, namespace, labelKey, labelValue string) (*appsv1.DaemonSet, error) {
	labelSelector := labels.SelectorFromSet(map[string]string{
		labelKey: labelValue,
	}).String()

	daemonSetList, err := h.client.AppsV1().DaemonSets(namespace).List(ctx, metav1.ListOptions{
		LabelSelector: labelSelector,
	})
	if err != nil {
		return nil, fmt.Errorf("failed to list DaemonSets: %w", err)
	}

	if len(daemonSetList.Items) == 0 {
		return nil, fmt.Errorf("no DaemonSet found with label %s=%s", labelKey, labelValue)
	}

	if len(daemonSetList.Items) > 1 {
		return nil, fmt.Errorf("multiple DaemonSets found with label %s=%s", labelKey, labelValue)
	}

	return &daemonSetList.Items[0], nil
}

func (h *DaemonSetClient) Get(ctx context.Context, namespace, name string) (*appsv1.DaemonSet, error) {
	return h.client.AppsV1().DaemonSets(namespace).Get(ctx, name, metav1.GetOptions{})
}

func (h *DaemonSetClient) WaitForReady(ctx context.Context, namespace, name string, timeout time.Duration) error {
	return wait.PollUntilContextTimeout(ctx, defaultPollingInterval, timeout, true, func(ctx context.Context) (bool, error) {
		daemonSet, err := h.Get(ctx, namespace, name)
		if err != nil {
			return false, err
		}

		if daemonSet.Status.NumberReady == daemonSet.Status.DesiredNumberScheduled &&
			daemonSet.Status.NumberReady > 0 {
			return true, nil
		}

		return false, nil
	})
}

func (h *DaemonSetClient) IsReady(ctx context.Context, namespace, name string) (bool, error) {
	daemonSet, err := h.Get(ctx, namespace, name)
	if err != nil {
		return false, err
	}

	return daemonSet.Status.NumberReady == daemonSet.Status.DesiredNumberScheduled && daemonSet.Status.NumberReady > 0, nil
}

func (h *DaemonSetClient) GetImage(ctx context.Context, namespace, name string) (string, error) {
	daemonSet, err := h.Get(ctx, namespace, name)
	if err != nil {
		return "", fmt.Errorf("failed to get DaemonSet: %w", err)
	}

	if len(daemonSet.Spec.Template.Spec.Containers) == 0 {
		return "", fmt.Errorf("DaemonSet has no containers")
	}

	return daemonSet.Spec.Template.Spec.Containers[0].Image, nil
}

func (h *DaemonSetClient) CheckNoRestarts(ctx context.Context, namespace, name string) error {
	daemonSet, err := h.Get(ctx, namespace, name)
	if err != nil {
		return fmt.Errorf("failed to get DaemonSet: %w", err)
	}

	labelSelector := labels.SelectorFromSet(daemonSet.Spec.Selector.MatchLabels).String()
	podList, err := h.client.CoreV1().Pods(namespace).List(ctx, metav1.ListOptions{
		LabelSelector: labelSelector,
	})
	if err != nil {
		return fmt.Errorf("failed to list pods: %w", err)
	}

	for _, pod := range podList.Items {
		for _, containerStatus := range pod.Status.ContainerStatuses {
			if containerStatus.RestartCount > 0 {
				return fmt.Errorf("pod %s/%s container %s has %d restarts",
					pod.Namespace, pod.Name, containerStatus.Name, containerStatus.RestartCount)
			}
		}
	}

	return nil
}

