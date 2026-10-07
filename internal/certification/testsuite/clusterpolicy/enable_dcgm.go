package clusterpolicy

import (
	"context"
	"strconv"

	"github.com/NVIDIA/gpu-operator/internal/certification"
	"github.com/NVIDIA/gpu-operator/internal/certification/k8s"
)

const expectedTrafficPolicy = "Local"

type EnableDCGM struct{}

func (t *EnableDCGM) Name() string {
	return "clusterpolicy-enable-dcgm"
}

func (t *EnableDCGM) Run(ctx context.Context, clients *k8s.Clients, cfg certification.Config) *certification.TestResult {
	result := certification.NewTestResult(t.Name())
	defer result.Finalize()

	timeout := cfg.ParseTimeout()

	// Step 1: Enable standalone DCGM engine
	result.AddDetail("enabling standalone DCGM via spec.dcgm.enabled=true")
	if err := t.setDCGMEnabled(ctx, clients, true); err != nil {
		result.Fail("failed to enable DCGM: %v", err)
		return result
	}

	// Step 2: Verify nvidia-dcgm pod is ready
	result.AddDetail("waiting for nvidia-dcgm pod to be ready")
	if err := clients.WaitForPodReady(ctx, cfg.Namespace, DCGMAppLabel, timeout); err != nil {
		result.Fail("nvidia-dcgm pod not ready: %v", err)
		return result
	}
	result.AddDetail("nvidia-dcgm pod ready")

	// Step 3: Verify nvidia-dcgm-exporter pod is ready
	result.AddDetail("waiting for nvidia-dcgm-exporter pod to be ready")
	if err := clients.WaitForPodReady(ctx, cfg.Namespace, DCGMExporterAppLabel, timeout); err != nil {
		result.Fail("nvidia-dcgm-exporter pod not ready: %v", err)
		return result
	}
	result.AddDetail("nvidia-dcgm-exporter pod ready")

	// Step 4: Verify nvidia-dcgm service has internalTrafficPolicy set to "Local"
	result.AddDetail("verifying nvidia-dcgm service internalTrafficPolicy")
	trafficPolicy, err := clients.GetServiceInternalTrafficPolicy(ctx, cfg.Namespace, DCGMServiceName)
	if err != nil {
		result.Fail("failed to get nvidia-dcgm service: %v", err)
		return result
	}

	if trafficPolicy != expectedTrafficPolicy {
		result.Fail("nvidia-dcgm service internalTrafficPolicy: expected %q, got %q", expectedTrafficPolicy, trafficPolicy)
		return result
	}
	result.AddDetail("nvidia-dcgm service has internalTrafficPolicy=%s", trafficPolicy)

	return result
}

func (t *EnableDCGM) setDCGMEnabled(ctx context.Context, clients *k8s.Clients, enabled bool) error {
	patch := []byte(`[{"op": "replace", "path": "/spec/dcgm/enabled", "value": ` + strconv.FormatBool(enabled) + `}]`)
	return clients.ModifyClusterPolicy(ctx, patch)
}
