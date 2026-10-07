package k8s

import (
	"context"
	"time"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/wait"
)

func (c *Clients) GetNode(ctx context.Context, name string) (*corev1.Node, error) {
	return c.k8s.CoreV1().Nodes().Get(ctx, name, metav1.GetOptions{})
}

func (c *Clients) ListNodes(ctx context.Context, labelSelector string) (*corev1.NodeList, error) {
	return c.k8s.CoreV1().Nodes().List(ctx, metav1.ListOptions{
		LabelSelector: labelSelector,
	})
}

func (c *Clients) PatchNode(ctx context.Context, name string, patchJSON []byte) (*corev1.Node, error) {
	return c.k8s.CoreV1().Nodes().Patch(ctx, name, types.JSONPatchType, patchJSON, metav1.PatchOptions{})
}

func (c *Clients) GetGPUNodes(ctx context.Context) ([]string, error) {
	nodes, err := c.k8s.CoreV1().Nodes().List(ctx, metav1.ListOptions{
		LabelSelector: GPUNodeSelector,
	})
	if err != nil {
		return nil, err
	}

	var gpuNodes []string
	for _, node := range nodes.Items {
		gpuNodes = append(gpuNodes, node.Name)
	}

	return gpuNodes, nil
}

func (c *Clients) GetNodeLabels(ctx context.Context, nodeName string) (map[string]string, error) {
	node, err := c.k8s.CoreV1().Nodes().Get(ctx, nodeName, metav1.GetOptions{})
	if err != nil {
		return nil, err
	}

	return node.Labels, nil
}

// WaitForGFDLabels waits until every GPU node has the core GFD labels present,
// confirming that GPU Feature Discovery has finished re-labeling after a
// configuration change. See https://github.com/NVIDIA/k8s-device-plugin
func (c *Clients) WaitForGFDLabels(ctx context.Context, timeout time.Duration) error {
	// Labels that GFD always writes on every GPU node. gpu.present is omitted
	// because the node list is already filtered by it.
	coreLabels := []string{
		"nvidia.com/gpu.product",
		"nvidia.com/gpu.count",
		"nvidia.com/gpu.memory",
		"nvidia.com/gfd.timestamp",
	}

	return wait.PollUntilContextTimeout(ctx, pollInterval, timeout, true, func(ctx context.Context) (bool, error) {
		nodes, err := c.k8s.CoreV1().Nodes().List(ctx, metav1.ListOptions{
			LabelSelector: GPUNodeSelector,
		})
		if err != nil {
			return false, nil
		}
		for _, node := range nodes.Items {
			for _, label := range coreLabels {
				if _, exists := node.Labels[label]; !exists {
					return false, nil
				}
			}
		}
		return true, nil
	})
}

// WaitForDriverUpgradeDone waits until the GPU operator marks every GPU node's
// upgrade state as done. Callers should ensure driver pods are already running
// before calling this to avoid a false-positive from stale or absent labels.
func (c *Clients) WaitForDriverUpgradeDone(ctx context.Context, timeout time.Duration) error {
	return wait.PollUntilContextTimeout(ctx, pollInterval, timeout, true, func(ctx context.Context) (bool, error) {
		nodes, err := c.k8s.CoreV1().Nodes().List(ctx, metav1.ListOptions{
			LabelSelector: GPUNodeSelector,
		})
		if err != nil {
			return false, nil
		}

		for _, node := range nodes.Items {
			state, exists := node.Labels["nvidia.com/gpu-driver-upgrade-state"]
			if exists && state != "upgrade-done" && state != "" {
				return false, nil
			}
		}
		return true, nil
	})
}
