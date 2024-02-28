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

package kubernetes

import (
	"bytes"
	"context"
	"fmt"
	"io"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	corev1client "k8s.io/client-go/kubernetes/typed/core/v1"
)

type Client struct {
	k8sClient corev1client.CoreV1Interface
}

func NewClient(k8sClient corev1client.CoreV1Interface) *Client {
	return &Client{
		k8sClient: k8sClient,
	}
}

func (c *Client) GetPodsByLabel(ctx context.Context, namespace string, labelMap map[string]string) ([]corev1.Pod, error) {
	podList, err := c.k8sClient.Pods(namespace).List(ctx, metav1.ListOptions{
		LabelSelector: labels.SelectorFromSet(labelMap).String(),
	})
	if err != nil {
		return nil, err
	}
	return podList.Items, nil
}

func (c *Client) IsPodReady(ctx context.Context, podName, namespace string) (bool, error) {
	pod, err := c.k8sClient.Pods(namespace).Get(ctx, podName, metav1.GetOptions{})
	if err != nil {
		return false, fmt.Errorf("unexpected error getting pod  %s: %w", podName, err)
	}

	for _, c := range pod.Status.Conditions {
		if c.Type != corev1.PodReady {
			continue
		}
		if c.Status == corev1.ConditionTrue {
			return true, nil
		}
	}

	return false, nil
}

func (c *Client) EnsureNoPodRestarts(ctx context.Context, podName, namespace string) (bool, error) {
	pod, err := c.k8sClient.Pods(namespace).Get(ctx, podName, metav1.GetOptions{})
	if err != nil {
		return false, fmt.Errorf("unexpected error getting pod  %s: %w", podName, err)
	}

	for _, cs := range pod.Status.ContainerStatuses {
		if cs.RestartCount > 0 {
			return false, nil
		}
	}
	return true, nil
}

func (c *Client) GetPodLogs(ctx context.Context, pod corev1.Pod) string {
	podLogOpts := corev1.PodLogOptions{}
	req := c.k8sClient.Pods(pod.Namespace).GetLogs(pod.Name, &podLogOpts)
	podLogs, err := req.Stream(ctx)
	if err != nil {
		return "error in opening stream"
	}
	defer podLogs.Close()

	buf := new(bytes.Buffer)
	_, err = io.Copy(buf, podLogs)
	if err != nil {
		return "error in copy information from podLogs to buf"
	}
	str := buf.String()

	return str
}

func (c *Client) CreateNamespace(ctx context.Context, namespaceName string, labels map[string]string) (*corev1.Namespace, error) {
	namespaceObj := &corev1.Namespace{
		ObjectMeta: metav1.ObjectMeta{
			Name:   namespaceName,
			Labels: labels,
		},
		Status: corev1.NamespaceStatus{},
	}

	return c.k8sClient.Namespaces().Create(ctx, namespaceObj, metav1.CreateOptions{})
}

func (c *Client) DeleteNamespace(ctx context.Context, namespaceName string) error {
	return c.k8sClient.Namespaces().Delete(ctx, namespaceName, metav1.DeleteOptions{})
}
