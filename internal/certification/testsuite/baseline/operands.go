package baseline

import (
	"context"

	"github.com/NVIDIA/gpu-operator/internal/certification"
	"github.com/NVIDIA/gpu-operator/internal/certification/k8s"
)

var operandLabels = []string{
	"nvidia-container-toolkit-daemonset",
	"nvidia-device-plugin-daemonset",
	"nvidia-dcgm-exporter",
	"gpu-feature-discovery",
	"nvidia-operator-validator",
}

type Operands struct{}

func (o *Operands) Name() string {
	return "operand-verification"
}

func (o *Operands) Run(ctx context.Context, clients *k8s.Clients, cfg certification.Config) *certification.TestResult {
	result := certification.NewTestResult(o.Name())
	defer result.Finalize()

	timeout := cfg.ParseTimeout()

	// Check driver pods (skip if driver is disabled or pre-installed on host)
	if clients.HasDriverPods(ctx, cfg.Namespace) {
		if err := clients.WaitForPodsReady(ctx, cfg.Namespace, k8s.DriverLabelSelector, timeout); err != nil {
			result.Fail("driver pods: %v", err)
			return result
		}
		result.AddDetail("driver pods: ready")
	} else {
		result.AddDetail("driver pods: skipped (no operator-managed driver pods found)")
	}

	for _, operand := range operandLabels {
		if err := clients.WaitForPodReady(ctx, cfg.Namespace, operand, timeout); err != nil {
			result.Fail("operand %s: %v", operand, err)
			return result
		}
		result.AddDetail("%s: ready", operand)
	}

	return result
}
