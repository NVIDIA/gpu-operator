package clusterpolicy

import (
	"context"

	"github.com/NVIDIA/gpu-operator/internal/certification"
	"github.com/NVIDIA/gpu-operator/internal/certification/k8s"
)

// MIG is a parameterized test for MIG single and mixed strategies.
type MIG struct {
	Strategy migStrategy
}

func (t *MIG) Name() string {
	return "mig-" + string(t.Strategy) + "-strategy"
}

func (t *MIG) Run(ctx context.Context, clients *k8s.Clients, cfg certification.Config) *certification.TestResult {
	result := certification.NewTestResult(t.Name())
	defer result.Finalize()

	runMIGTest(ctx, clients, result, cfg, t.Strategy)
	return result
}
