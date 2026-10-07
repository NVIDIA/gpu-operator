package baseline

import (
	"context"
	"fmt"
	"strconv"
	"time"

	"github.com/NVIDIA/gpu-operator/internal/certification"
	"github.com/NVIDIA/gpu-operator/internal/certification/k8s"
)

const (
	gpuPresentLabel           = "nvidia.com/gpu.present"
	gpuProductLabel           = "nvidia.com/gpu.product"
	gpuCountLabel             = "nvidia.com/gpu.count"
	cudaDriverVersionMajor    = "nvidia.com/cuda.driver-version.major"
	cudaDriverVersionMinor    = "nvidia.com/cuda.driver-version.minor"
	cudaDriverVersionRevision = "nvidia.com/cuda.driver-version.revision"
)

var requiredGFDLabels = []string{
	gpuPresentLabel,
	gpuProductLabel,
	gpuCountLabel,
	cudaDriverVersionMajor,
	cudaDriverVersionMinor,
	cudaDriverVersionRevision,
	"nvidia.com/cuda.runtime-version.major",
	"nvidia.com/cuda.runtime-version.minor",
	"nvidia.com/gpu.memory",
	"nvidia.com/gpu.compute.major",
	"nvidia.com/gpu.compute.minor",
	"nvidia.com/gpu.family",
	"nvidia.com/gpu.machine",
	"nvidia.com/gfd.timestamp",
	"nvidia.com/mig.capable",
	"nvidia.com/gpu.replicas",
}

type GFDLabels struct{}

func (g *GFDLabels) Name() string {
	return "gfd-labels"
}

func (g *GFDLabels) Run(ctx context.Context, clients *k8s.Clients, cfg certification.Config) *certification.TestResult {
	result := certification.NewTestResult(g.Name())
	defer result.Finalize()

	gpuNodes, err := clients.GetGPUNodes(ctx)
	if err != nil {
		result.Fail("failed to get GPU nodes: %v", err)
		return result
	}

	if len(gpuNodes) == 0 {
		result.Fail("no GPU nodes found (nodes with nvidia.com/gpu.present=true)")
		return result
	}

	result.AddDetail("found %d GPU node(s)", len(gpuNodes))

	// Poll for GFD labels with a short timeout to handle transient label absence
	// (e.g., after MIG reconfiguration, GFD may need time to re-apply labels)
	gfdTimeout := 2 * time.Minute
	err = clients.WaitForCondition(ctx, gfdTimeout, func(ctx context.Context) (bool, error) {
		for _, nodeName := range gpuNodes {
			node, err := clients.GetNode(ctx, nodeName)
			if err != nil {
				return false, nil
			}
			for _, labelKey := range requiredGFDLabels {
				if _, exists := node.Labels[labelKey]; !exists {
					return false, nil
				}
			}
		}
		return true, nil
	})
	if err != nil {
		result.Fail("GFD required labels not found after waiting %s", gfdTimeout)
		return result
	}

	for _, nodeName := range gpuNodes {
		node, err := clients.GetNode(ctx, nodeName)
		if err != nil {
			result.Fail("failed to get node %s: %v", nodeName, err)
			return result
		}

		if err := g.verifyRequiredLabels(nodeName, node.Labels, result); err != nil {
			result.Fail("%v", err)
			return result
		}
	}

	return result
}

func (g *GFDLabels) verifyRequiredLabels(nodeName string, labels map[string]string, result *certification.TestResult) error {
	for _, labelKey := range requiredGFDLabels {
		value, exists := labels[labelKey]
		if !exists {
			return fmt.Errorf("node %s: required label %s not found", nodeName, labelKey)
		}

		if value == "" {
			return fmt.Errorf("node %s: required label %s is empty", nodeName, labelKey)
		}

		if err := validateLabelValue(labelKey, value); err != nil {
			return fmt.Errorf("node %s: %v", nodeName, err)
		}

		result.AddDetail("node %s: %s=%s", nodeName, labelKey, value)
	}

	return nil
}

func validateLabelValue(labelKey, value string) error {
	switch labelKey {
	case gpuPresentLabel:
		if value != "true" {
			return fmt.Errorf("label %s must be 'true', got '%s'", labelKey, value)
		}
	case gpuCountLabel:
		count, err := strconv.Atoi(value)
		if err != nil {
			return fmt.Errorf("label %s must be a valid integer, got '%s'", labelKey, value)
		}
		if count <= 0 {
			return fmt.Errorf("label %s must be positive, got %d", labelKey, count)
		}
	case cudaDriverVersionMajor, cudaDriverVersionMinor, cudaDriverVersionRevision:
		_, err := strconv.Atoi(value)
		if err != nil {
			return fmt.Errorf("label %s must be a valid integer, got '%s'", labelKey, value)
		}
	}

	return nil
}
