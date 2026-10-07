package k8s

import (
	"context"
	"fmt"
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"

	nvidiav1 "github.com/NVIDIA/gpu-operator/api/nvidia/v1"
)

var clusterPolicyGVR = schema.GroupVersionResource{
	Group:    "nvidia.com",
	Version:  "v1",
	Resource: "clusterpolicies",
}

// GetClusterPolicy retrieves the ClusterPolicy resource by listing all
// instances and returning the first one found.
func (c *Clients) GetClusterPolicy(ctx context.Context) (*nvidiav1.ClusterPolicy, error) {
	list, err := c.dynamic.Resource(clusterPolicyGVR).List(ctx, metav1.ListOptions{})
	if err != nil {
		return nil, fmt.Errorf("listing ClusterPolicies: %w", err)
	}
	if len(list.Items) == 0 {
		return nil, fmt.Errorf("no ClusterPolicy resource found")
	}

	var cp nvidiav1.ClusterPolicy
	if err := runtime.DefaultUnstructuredConverter.FromUnstructured(list.Items[0].Object, &cp); err != nil {
		return nil, fmt.Errorf("converting to ClusterPolicy: %w", err)
	}
	return &cp, nil
}

// ModifyClusterPolicy applies a JSON patch to the ClusterPolicy resource.
func (c *Clients) ModifyClusterPolicy(ctx context.Context, patchJSON []byte) error {
	cp, err := c.GetClusterPolicy(ctx)
	if err != nil {
		return err
	}
	_, err = c.dynamic.Resource(clusterPolicyGVR).Patch(
		ctx,
		cp.Name,
		types.JSONPatchType,
		patchJSON,
		metav1.PatchOptions{},
	)
	if err != nil {
		return fmt.Errorf("patching ClusterPolicy: %w", err)
	}
	return nil
}

// WaitForClusterPolicyNotReady polls until ClusterPolicy state is not "ready",
// confirming the operator has begun reconciling. Returns nil if notReady is
// observed, or the context/timeout error if it never appears (which is OK —
// the operator may have reconciled faster than our poll interval).
func (c *Clients) WaitForClusterPolicyNotReady(ctx context.Context, timeout time.Duration) error {
	return c.WaitForCondition(ctx, timeout, func(ctx context.Context) (bool, error) {
		cp, err := c.GetClusterPolicy(ctx)
		if err != nil {
			return false, nil
		}
		return cp.Status.State != nvidiav1.Ready, nil
	})
}

// WaitForClusterPolicyReady polls until ClusterPolicy state is "ready"
// and the Ready condition is True. This confirms all operands are healthy.
func (c *Clients) WaitForClusterPolicyReady(ctx context.Context, timeout time.Duration) error {
	return c.WaitForCondition(ctx, timeout, func(ctx context.Context) (bool, error) {
		cp, err := c.GetClusterPolicy(ctx)
		if err != nil {
			return false, nil
		}
		if cp.Status.State != nvidiav1.Ready {
			return false, nil
		}
		for _, cond := range cp.Status.Conditions {
			if cond.Type == "Ready" {
				return cond.Status == metav1.ConditionTrue, nil
			}
		}
		// No Ready condition found yet.
		return false, nil
	})
}
