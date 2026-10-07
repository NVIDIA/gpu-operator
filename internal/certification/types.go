package certification

import (
	"context"

	"github.com/NVIDIA/gpu-operator-self-certification/internal/certification/k8s"
)

// Test is the fundamental unit of validation. Tests are reusable building blocks
// that can be composed into TestSets.
type Test interface {
	Name() string
	Run(ctx context.Context, clients *k8s.Clients, cfg Config) *TestResult
}

// TestSet groups related Tests together. Tests within a set are executed in order.
type TestSet struct {
	Name        string
	Description string
	Tests       []Test
	Params      map[string]any
}
