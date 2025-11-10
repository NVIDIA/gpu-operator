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
	"bytes"
	"context"
	"fmt"
	"io"
	"strings"
	"time"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/client-go/kubernetes"
)

type WorkloadClient struct {
	client kubernetes.Interface
}

func NewWorkloadClient(client kubernetes.Interface) *WorkloadClient {
	return &WorkloadClient{
		client: client,
	}
}

func (h *WorkloadClient) DeployGPUPod(ctx context.Context, namespace string, podSpec *corev1.Pod) (*corev1.Pod, error) {
	return h.client.CoreV1().Pods(namespace).Create(ctx, podSpec, metav1.CreateOptions{})
}

func (h *WorkloadClient) WaitForCompletion(ctx context.Context, namespace, name string, timeout time.Duration) error {
	return wait.PollUntilContextTimeout(ctx, defaultPollingInterval, timeout, true, func(ctx context.Context) (bool, error) {
		workloadPod, err := h.client.CoreV1().Pods(namespace).Get(ctx, name, metav1.GetOptions{})
		if err != nil {
			return false, err
		}

		if workloadPod.Status.Phase == corev1.PodSucceeded {
			return true, nil
		}

		if workloadPod.Status.Phase == corev1.PodFailed {
			return false, fmt.Errorf("pod %s/%s failed", namespace, name)
		}

		return false, nil
	})
}

func (h *WorkloadClient) WaitForRunning(ctx context.Context, namespace, name string, timeout time.Duration) error {
	return wait.PollUntilContextTimeout(ctx, defaultPollingInterval, timeout, true, func(ctx context.Context) (bool, error) {
		workloadPod, err := h.client.CoreV1().Pods(namespace).Get(ctx, name, metav1.GetOptions{})
		if err != nil {
			return false, err
		}

		if workloadPod.Status.Phase == corev1.PodRunning {
			for _, condition := range workloadPod.Status.Conditions {
				if condition.Type == corev1.PodReady && condition.Status == corev1.ConditionTrue {
					return true, nil
				}
			}
		}

		if workloadPod.Status.Phase == corev1.PodFailed {
			return false, fmt.Errorf("pod %s/%s failed", namespace, name)
		}

		return false, nil
	})
}

func (h *WorkloadClient) GetLogs(ctx context.Context, namespace, name string) (string, error) {
	podLogOpts := corev1.PodLogOptions{}
	req := h.client.CoreV1().Pods(namespace).GetLogs(name, &podLogOpts)
	podLogs, err := req.Stream(ctx)
	if err != nil {
		return "", fmt.Errorf("failed to open log stream: %w", err)
	}
	defer podLogs.Close()

	buffer := new(bytes.Buffer)
	_, err = io.Copy(buffer, podLogs)
	if err != nil {
		return "", fmt.Errorf("failed to copy log stream: %w", err)
	}

	return buffer.String(), nil
}

// VerifyGPUAccess checks pod logs for evidence of GPU access
func (h *WorkloadClient) VerifyGPUAccess(ctx context.Context, namespace, name string) error {
	logs, err := h.GetLogs(ctx, namespace, name)
	if err != nil {
		return fmt.Errorf("failed to get pod logs: %w", err)
	}

	if !strings.Contains(logs, "NVIDIA") && !strings.Contains(logs, "GPU") {
		return fmt.Errorf("pod logs do not contain evidence of GPU access")
	}

	return nil
}

func (h *WorkloadClient) Delete(ctx context.Context, namespace, name string) error {
	return h.client.CoreV1().Pods(namespace).Delete(ctx, name, metav1.DeleteOptions{})
}

func CreateSimpleGPUPod(name, namespace string, gpuLimit int) *corev1.Pod {
	return &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: namespace,
		},
		Spec: corev1.PodSpec{
			RestartPolicy: corev1.RestartPolicyNever,
			Containers: []corev1.Container{
				{
					Name:  "gpu-test",
					Image: "nvidia/cuda:12.0.0-base-ubuntu22.04",
					Command: []string{
						"nvidia-smi",
					},
					Resources: corev1.ResourceRequirements{
						Limits: corev1.ResourceList{
							"nvidia.com/gpu": *resource.NewQuantity(int64(gpuLimit), resource.DecimalSI),
						},
					},
				},
			},
		},
	}
}

