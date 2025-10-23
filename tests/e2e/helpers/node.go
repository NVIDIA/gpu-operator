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

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/client-go/kubernetes"
)

type NodeClient struct {
	client kubernetes.Interface
}

func NewNodeClient(client kubernetes.Interface) *NodeClient {
	return &NodeClient{
		client: client,
	}
}

func (h *NodeClient) LabelNode(ctx context.Context, nodeName, key, value string) error {
	node, err := h.client.CoreV1().Nodes().Get(ctx, nodeName, metav1.GetOptions{})
	if err != nil {
		return fmt.Errorf("failed to get node: %w", err)
	}

	if node.Labels == nil {
		node.Labels = make(map[string]string)
	}

	node.Labels[key] = value

	_, err = h.client.CoreV1().Nodes().Update(ctx, node, metav1.UpdateOptions{})
	if err != nil {
		return fmt.Errorf("failed to update node: %w", err)
	}

	return nil
}

func (h *NodeClient) UnlabelNode(ctx context.Context, nodeName, key string) error {
	node, err := h.client.CoreV1().Nodes().Get(ctx, nodeName, metav1.GetOptions{})
	if err != nil {
		return fmt.Errorf("failed to get node: %w", err)
	}

	if node.Labels != nil {
		delete(node.Labels, key)
	}

	_, err = h.client.CoreV1().Nodes().Update(ctx, node, metav1.UpdateOptions{})
	if err != nil {
		return fmt.Errorf("failed to update node: %w", err)
	}

	return nil
}

func (h *NodeClient) GetNodesByLabel(ctx context.Context, labelKey, labelValue string) ([]corev1.Node, error) {
	labelSelector := labels.SelectorFromSet(map[string]string{
		labelKey: labelValue,
	}).String()

	nodeList, err := h.client.CoreV1().Nodes().List(ctx, metav1.ListOptions{
		LabelSelector: labelSelector,
	})
	if err != nil {
		return nil, fmt.Errorf("failed to list nodes: %w", err)
	}

	return nodeList.Items, nil
}

func (h *NodeClient) ListNodes(ctx context.Context) ([]corev1.Node, error) {
	nodeList, err := h.client.CoreV1().Nodes().List(ctx, metav1.ListOptions{})
	if err != nil {
		return nil, fmt.Errorf("failed to list nodes: %w", err)
	}

	return nodeList.Items, nil
}

func (h *NodeClient) LabelAllNodes(ctx context.Context, key, value string) error {
	nodes, err := h.ListNodes(ctx)
	if err != nil {
		return err
	}

	for _, node := range nodes {
		if err := h.LabelNode(ctx, node.Name, key, value); err != nil {
			return fmt.Errorf("failed to label node %s: %w", node.Name, err)
		}
	}

	return nil
}

func (h *NodeClient) UnlabelAllNodes(ctx context.Context, key string) error {
	nodes, err := h.ListNodes(ctx)
	if err != nil {
		return err
	}

	for _, node := range nodes {
		if err := h.UnlabelNode(ctx, node.Name, key); err != nil {
			return fmt.Errorf("failed to unlabel node %s: %w", node.Name, err)
		}
	}

	return nil
}

