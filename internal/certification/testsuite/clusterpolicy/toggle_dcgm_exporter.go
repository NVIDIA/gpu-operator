package clusterpolicy

import (
	"context"
	"strconv"

	"github.com/NVIDIA/gpu-operator-self-certification/internal/certification"
	"github.com/NVIDIA/gpu-operator-self-certification/internal/certification/k8s"
)

type ToggleDCGMExporter struct{}

func (t *ToggleDCGMExporter) Name() string {
	return "clusterpolicy-toggle-dcgm-exporter"
}

func (t *ToggleDCGMExporter) Run(ctx context.Context, clients *k8s.Clients, cfg certification.Config) *certification.TestResult {
	result := certification.NewTestResult(t.Name())
	defer result.Finalize()

	timeout := cfg.ParseTimeout()

	// Step 1: Disable DCGM Exporter
	result.AddDetail("disabling DCGM Exporter via ClusterPolicy patch")
	if err := t.setDCGMExporterEnabled(ctx, clients, false); err != nil {
		result.Fail("failed to disable DCGM Exporter: %v", err)
		return result
	}

	// Step 2: Wait for DCGM Exporter pod to be deleted
	result.AddDetail("waiting for DCGM Exporter pod to be deleted")
	if err := clients.WaitForPodDeleted(ctx, cfg.Namespace, DCGMExporterAppLabel, timeout); err != nil {
		result.Fail("DCGM Exporter pod was not deleted after disabling: %v", err)
		// Attempt to re-enable before returning
		_ = t.setDCGMExporterEnabled(ctx, clients, true)
		return result
	}
	result.AddDetail("DCGM Exporter pod deleted successfully")

	// Step 3: Re-enable DCGM Exporter
	result.AddDetail("re-enabling DCGM Exporter via ClusterPolicy patch")
	if err := t.setDCGMExporterEnabled(ctx, clients, true); err != nil {
		result.Fail("failed to re-enable DCGM Exporter: %v", err)
		return result
	}

	// Step 4: Wait for DCGM Exporter pod to become ready
	result.AddDetail("waiting for DCGM Exporter pod to become ready")
	if err := clients.WaitForPodReady(ctx, cfg.Namespace, DCGMExporterAppLabel, timeout); err != nil {
		result.Fail("DCGM Exporter pod not ready after re-enabling: %v", err)
		return result
	}
	result.AddDetail("DCGM Exporter pod ready after re-enabling")

	return result
}

func (t *ToggleDCGMExporter) setDCGMExporterEnabled(ctx context.Context, clients *k8s.Clients, enabled bool) error {
	patch := []byte(`[{"op": "replace", "path": "/spec/dcgmExporter/enabled", "value": ` + strconv.FormatBool(enabled) + `}]`)
	return clients.ModifyClusterPolicy(ctx, patch)
}
