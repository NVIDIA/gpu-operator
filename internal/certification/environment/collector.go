package environment

import (
	"context"

	"github.com/NVIDIA/gpu-operator/internal/certification/k8s"
)

// Collector defines the interface for environment data collectors.
type Collector interface {
	Name() string
	Collect(ctx context.Context, c *k8s.Clients, namespace string, s *EnvironmentSnapshot) error
}

// DefaultCollectors returns all collectors in execution order.
func DefaultCollectors() []Collector {
	return []Collector{
		&clusterCollector{},
		&operatorCollector{},
		&policyCollector{},
		&driverCollector{},
		&sharingConfigCollector{},
		&operandCollector{},
		&nodeCollector{},
		&virtualizationCollector{},
		&openshiftCollector{},
	}
}
