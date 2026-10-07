package clusterpolicy

import (
	"context"
	"time"

	"github.com/NVIDIA/gpu-operator/internal/certification"
	"github.com/NVIDIA/gpu-operator/internal/certification/k8s"
)

var customLabelsOperands = []string{
	DriverAppLabel,
	ToolkitAppLabel,
	ValidatorAppLabel,
	GFDAppLabel,
	DCGMExporterAppLabel,
	DevicePluginAppLabel,
}

var expectedLabels = map[string]string{
	"cloudprovider": "aws",
	"platform":      "kubernetes",
}

type CustomLabels struct{}

func (t *CustomLabels) Name() string {
	return "clusterpolicy-custom-labels"
}

func (t *CustomLabels) Run(ctx context.Context, clients *k8s.Clients, cfg certification.Config) *certification.TestResult {
	result := certification.NewTestResult(t.Name())
	defer result.Finalize()

	timeout := cfg.ParseTimeout()

	// Step 1: Patch daemonsets.labels with custom labels
	result.AddDetail("patching spec.daemonsets.labels with custom labels")
	if err := t.setCustomLabels(ctx, clients); err != nil {
		result.Fail("failed to patch custom labels: %v", err)
		return result
	}

	// Step 2: Wait for operator to apply changes (labels override triggers rollout)
	result.AddDetail("waiting for operator to apply label changes")
	select {
	case <-time.After(15 * time.Second):
	case <-ctx.Done():
		result.Fail("context cancelled while waiting for label changes")
		return result
	}

	// Step 3: Wait for all operand pods to be ready
	result.AddDetail("waiting for all operand pods to be ready after label update")
	for _, operand := range customLabelsOperands {
		if err := clients.WaitForPodReady(ctx, cfg.Namespace, operand, timeout); err != nil {
			result.Fail("operand %s not ready after label update: %v", operand, err)
			return result
		}
	}
	result.AddDetail("all operand pods ready")

	// Step 4: Verify custom labels on ALL pods for each operand (not just first pod)
	result.AddDetail("verifying custom labels on all operand pods")
	for _, operand := range customLabelsOperands {
		allPodLabels, err := clients.GetAllPodLabels(ctx, cfg.Namespace, operand)
		if err != nil {
			result.Fail("failed to get labels for %s: %v", operand, err)
			return result
		}

		for podName, podLabels := range allPodLabels {
			for labelKey, expectedValue := range expectedLabels {
				actualValue, exists := podLabels[labelKey]
				if !exists {
					result.Fail("pod %s: missing label %s", podName, labelKey)
					return result
				}
				if actualValue != expectedValue {
					result.Fail("pod %s: label %s expected %q, got %q", podName, labelKey, expectedValue, actualValue)
					return result
				}
			}
		}
		result.AddDetail("%s: labels verified on %d pod(s)", operand, len(allPodLabels))
	}

	return result
}

func (t *CustomLabels) setCustomLabels(ctx context.Context, clients *k8s.Clients) error {
	patch := []byte(`[{"op": "add", "path": "/spec/daemonsets/labels", "value": {"cloudprovider": "aws", "platform": "kubernetes"}}]`)
	return clients.ModifyClusterPolicy(ctx, patch)
}
