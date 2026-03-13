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

package controllers

import (
	"context"
	"testing"

	"github.com/NVIDIA/k8s-operator-libs/pkg/upgrade"
	"github.com/go-logr/logr"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/log/zap"
)

const (
	testNamespace = "gpu-operator"
)

func TestClearStaleUpgradeLabels(t *testing.T) {
	previousDriverName := upgrade.DriverName
	t.Cleanup(func() {
		upgrade.SetDriverName(previousDriverName)
	})
	upgrade.SetDriverName("gpu")
	upgradeLabel := upgrade.GetUpgradeStateLabelKey()

	tests := []struct {
		name                  string
		nodes                 []*corev1.Node
		daemonsets            []*appsv1.DaemonSet
		pods                  []*corev1.Pod
		state                 *upgrade.ClusterUpgradeState
		expectedRemovedLabels []string // node names that should have label removed
	}{
		{
			name:                  "no nodes with upgrade labels",
			nodes:                 createNodes([]nodeConfig{{name: "node1"}}),
			daemonsets:            []*appsv1.DaemonSet{},
			pods:                  []*corev1.Pod{},
			state:                 createEmptyState(),
			expectedRemovedLabels: []string{},
		},
		{
			name: "all labeled nodes are actively managed",
			nodes: createNodes([]nodeConfig{
				{name: "node1", hasUpgradeLabel: true, upgradeState: "upgrade-required"},
			}),
			daemonsets: []*appsv1.DaemonSet{createDaemonSet("driver-ds", nil)},
			pods:       []*corev1.Pod{createPod("driver-pod-1", "node1", "driver-ds")},
			state: createStateWithNodes([]*corev1.Node{
				createNode("node1", true, "upgrade-required"),
			}, []*corev1.Pod{
				createPod("driver-pod-1", "node1", "driver-ds"),
			}),
			expectedRemovedLabels: []string{},
		},
		{
			name: "node with stale label and no pod - should remove",
			nodes: createNodes([]nodeConfig{
				{name: "node1", hasUpgradeLabel: true, upgradeState: "upgrade-required"},
			}),
			daemonsets:            []*appsv1.DaemonSet{},
			pods:                  []*corev1.Pod{},
			state:                 createEmptyState(),
			expectedRemovedLabels: []string{"node1"},
		},
		{
			name: "node with label but DaemonSet still targets it - should NOT remove",
			nodes: createNodes([]nodeConfig{
				{name: "node1", hasUpgradeLabel: true, upgradeState: "upgrade-done", labels: map[string]string{"gpu": "true"}},
			}),
			daemonsets: []*appsv1.DaemonSet{
				createDaemonSet("driver-ds", map[string]string{"gpu": "true"}),
			},
			pods:                  []*corev1.Pod{},
			state:                 createEmptyState(),
			expectedRemovedLabels: []string{},
		},
		{
			name: "node excluded by nodeSelector change - should remove",
			nodes: createNodes([]nodeConfig{
				{name: "node1", hasUpgradeLabel: true, upgradeState: "upgrade-required", labels: map[string]string{"gpu": "true"}},
				{name: "node2", hasUpgradeLabel: true, upgradeState: "upgrade-required", labels: map[string]string{"gpu": "true", "type": "A100"}},
			}),
			daemonsets: []*appsv1.DaemonSet{
				// DaemonSet now requires both gpu=true AND type=A100
				createDaemonSet("driver-ds", map[string]string{"gpu": "true", "type": "A100"}),
			},
			pods:                  []*corev1.Pod{},
			state:                 createEmptyState(),
			expectedRemovedLabels: []string{"node1"}, // node1 doesn't have type=A100
		},
		{
			name: "multiple NVIDIADriver CRs - node targeted by at least one - should NOT remove",
			nodes: createNodes([]nodeConfig{
				{name: "node1", hasUpgradeLabel: true, upgradeState: "upgrade-done", labels: map[string]string{"type": "A100"}},
			}),
			daemonsets: []*appsv1.DaemonSet{
				createDaemonSet("driver-ds-1", map[string]string{"type": "V100"}), // doesn't match
				createDaemonSet("driver-ds-2", map[string]string{"type": "A100"}), // matches!
			},
			pods:                  []*corev1.Pod{},
			state:                 createEmptyState(),
			expectedRemovedLabels: []string{},
		},
		{
			name: "mixed scenario - some keep, some remove",
			nodes: createNodes([]nodeConfig{
				{name: "node1", hasUpgradeLabel: true, upgradeState: "upgrade-done", labels: map[string]string{"gpu": "true"}},      // has pod
				{name: "node2", hasUpgradeLabel: true, upgradeState: "upgrade-required", labels: map[string]string{"gpu": "false"}}, // excluded by selector
				{name: "node3", hasUpgradeLabel: true, upgradeState: "upgrade-done", labels: map[string]string{"gpu": "true"}},      // targeted by DS
				{name: "node4"}, // no label
			}),
			daemonsets: []*appsv1.DaemonSet{
				createDaemonSet("driver-ds", map[string]string{"gpu": "true"}),
			},
			pods: []*corev1.Pod{
				createPod("driver-pod-1", "node1", "driver-ds"),
			},
			state: createStateWithNodes([]*corev1.Node{
				createNode("node1", true, "upgrade-done"),
			}, []*corev1.Pod{
				createPod("driver-pod-1", "node1", "driver-ds"),
			}),
			expectedRemovedLabels: []string{"node2"}, // node2 excluded by selector
		},
		{
			name: "rolling update - pod being recreated - should NOT remove",
			nodes: createNodes([]nodeConfig{
				{name: "node1", hasUpgradeLabel: true, upgradeState: "upgrade-required", labels: map[string]string{"gpu": "true"}},
			}),
			daemonsets: []*appsv1.DaemonSet{
				createDaemonSet("driver-ds", map[string]string{"gpu": "true"}),
			},
			pods:                  []*corev1.Pod{}, // pod deleted, not yet recreated
			state:                 createEmptyState(),
			expectedRemovedLabels: []string{}, // should NOT remove - DaemonSet still targets it
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Create fake client with tracking
			objs := []client.Object{}
			for _, node := range tt.nodes {
				objs = append(objs, node.DeepCopy())
			}
			for _, ds := range tt.daemonsets {
				objs = append(objs, ds)
			}
			for _, pod := range tt.pods {
				objs = append(objs, pod)
			}

			scheme := runtime.NewScheme()
			_ = corev1.AddToScheme(scheme)
			_ = appsv1.AddToScheme(scheme)

			baseClient := fake.NewClientBuilder().
				WithScheme(scheme).
				WithObjects(objs...).
				Build()

			// Wrap client to track patches
			trackingClient := &patchTrackingClient{
				Client:       baseClient,
				patchedNodes: make(map[string]*corev1.Node),
			}

			// Create reconciler
			reconciler := &UpgradeReconciler{
				Client: trackingClient,
				Log:    zap.New(zap.UseDevMode(true)),
				Scheme: scheme,
			}

			// Call the function
			driverLabel := map[string]string{AppComponentLabelKey: AppComponentLabelValue}
			err := reconciler.clearStaleUpgradeLabels(context.Background(), tt.state, driverLabel, testNamespace)
			require.NoError(t, err)

			// Verify labels were removed from expected nodes
			for _, nodeName := range tt.expectedRemovedLabels {
				patchedNode, wasPatched := trackingClient.patchedNodes[nodeName]
				assert.True(t, wasPatched, "expected node %s to be patched", nodeName)
				if wasPatched {
					_, hasLabel := patchedNode.Labels[upgradeLabel]
					assert.False(t, hasLabel, "expected upgrade label to be removed from node %s", nodeName)
				}
			}

			// Verify labels were NOT removed from other nodes with upgrade labels
			for _, node := range tt.nodes {
				shouldRemove := false
				for _, removedNode := range tt.expectedRemovedLabels {
					if node.Name == removedNode {
						shouldRemove = true
						break
					}
				}

				if !shouldRemove && hasUpgradeLabel(node, upgradeLabel) {
					_, wasPatched := trackingClient.patchedNodes[node.Name]
					// Node should NOT have been patched if it should keep its label
					assert.False(t, wasPatched, "expected upgrade label to remain on node %s (should not be patched)", node.Name)
				}
			}
		})
	}
}

// Helper types and functions

type nodeConfig struct {
	name            string
	hasUpgradeLabel bool
	upgradeState    string
	labels          map[string]string
}

func createNodes(configs []nodeConfig) []*corev1.Node {
	nodes := []*corev1.Node{}
	for _, cfg := range configs {
		nodes = append(nodes, createNodeWithConfig(cfg))
	}
	return nodes
}

func createNodeWithConfig(cfg nodeConfig) *corev1.Node {
	labels := make(map[string]string)
	for k, v := range cfg.labels {
		labels[k] = v
	}
	if cfg.hasUpgradeLabel {
		labels[upgrade.GetUpgradeStateLabelKey()] = cfg.upgradeState
	}
	return &corev1.Node{
		ObjectMeta: metav1.ObjectMeta{
			Name:   cfg.name,
			Labels: labels,
		},
	}
}

func createNode(name string, hasUpgradeLabel bool, upgradeState string) *corev1.Node {
	labels := map[string]string{}
	if hasUpgradeLabel {
		labels[upgrade.GetUpgradeStateLabelKey()] = upgradeState
	}
	return &corev1.Node{
		ObjectMeta: metav1.ObjectMeta{
			Name:   name,
			Labels: labels,
		},
	}
}

func createDaemonSet(name string, nodeSelector map[string]string) *appsv1.DaemonSet {
	return &appsv1.DaemonSet{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: testNamespace,
			Labels: map[string]string{
				AppComponentLabelKey: AppComponentLabelValue,
			},
		},
		Spec: appsv1.DaemonSetSpec{
			Template: corev1.PodTemplateSpec{
				Spec: corev1.PodSpec{
					NodeSelector: nodeSelector,
				},
			},
		},
	}
}

func createPod(name, nodeName, ownerName string) *corev1.Pod {
	pod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: testNamespace,
			Labels: map[string]string{
				AppComponentLabelKey: AppComponentLabelValue,
			},
		},
		Spec: corev1.PodSpec{
			NodeName: nodeName,
		},
	}
	if ownerName != "" {
		pod.OwnerReferences = []metav1.OwnerReference{
			{
				Kind: "DaemonSet",
				Name: ownerName,
				UID:  types.UID(ownerName + "-uid"),
			},
		}
	}
	return pod
}

func createEmptyState() *upgrade.ClusterUpgradeState {
	return &upgrade.ClusterUpgradeState{
		NodeStates: make(map[string][]*upgrade.NodeUpgradeState),
	}
}

func createStateWithNodes(nodes []*corev1.Node, pods []*corev1.Pod) *upgrade.ClusterUpgradeState {
	state := createEmptyState()

	// Build a map of pod -> node
	podToNode := make(map[string]*corev1.Node)
	for _, pod := range pods {
		for _, node := range nodes {
			if pod.Spec.NodeName == node.Name {
				podToNode[pod.Name] = node
				break
			}
		}
	}

	// Add nodes to state based on their upgrade label
	for _, pod := range pods {
		node := podToNode[pod.Name]
		if node == nil {
			continue
		}
		upgradeState := node.Labels[upgrade.GetUpgradeStateLabelKey()]
		if upgradeState == "" {
			upgradeState = "unknown"
		}

		nodeState := &upgrade.NodeUpgradeState{
			Node:      node,
			DriverPod: pod,
		}
		state.NodeStates[upgradeState] = append(state.NodeStates[upgradeState], nodeState)
	}

	return state
}

func hasUpgradeLabel(node *corev1.Node, upgradeLabel string) bool {
	_, hasLabel := node.Labels[upgradeLabel]
	return hasLabel
}

// patchTrackingClient wraps a fake client and tracks Patch operations on nodes
type patchTrackingClient struct {
	client.Client
	patchedNodes map[string]*corev1.Node
}

func (c *patchTrackingClient) Patch(ctx context.Context, obj client.Object, patch client.Patch, opts ...client.PatchOption) error {
	// Track node patches
	if node, ok := obj.(*corev1.Node); ok {
		c.patchedNodes[node.Name] = node.DeepCopy()
	}
	// Delegate to underlying client
	return c.Client.Patch(ctx, obj, patch, opts...)
}

func TestClearStaleUpgradeLabels_ErrorHandling(t *testing.T) {
	previousDriverName := upgrade.DriverName
	t.Cleanup(func() {
		upgrade.SetDriverName(previousDriverName)
	})
	upgrade.SetDriverName("gpu")

	t.Run("handles list nodes error gracefully", func(t *testing.T) {
		scheme := runtime.NewScheme()
		_ = corev1.AddToScheme(scheme)

		// Create a client that will fail on List operations
		fakeClient := &errorClient{
			shouldFailList: true,
		}

		reconciler := &UpgradeReconciler{
			Client: fakeClient,
			Log:    logr.Discard(),
			Scheme: scheme,
		}

		state := createEmptyState()
		driverLabel := map[string]string{AppComponentLabelKey: AppComponentLabelValue}

		err := reconciler.clearStaleUpgradeLabels(context.Background(), state, driverLabel, testNamespace)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "failed to list nodes")
	})
}

// errorClient is a fake client that returns errors for testing
type errorClient struct {
	client.Client
	shouldFailList bool
}

func (c *errorClient) List(ctx context.Context, list client.ObjectList, opts ...client.ListOption) error {
	if c.shouldFailList {
		return assert.AnError
	}
	return nil
}

func (c *errorClient) Get(ctx context.Context, key client.ObjectKey, obj client.Object, opts ...client.GetOption) error {
	return assert.AnError
}

func (c *errorClient) Patch(ctx context.Context, obj client.Object, patch client.Patch, opts ...client.PatchOption) error {
	return nil
}
