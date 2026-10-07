package environment

import (
	"context"
	"log"
	"time"

	"github.com/NVIDIA/gpu-operator-self-certification/internal/certification/k8s"
)

// SnapshotBuilder constructs EnvironmentSnapshot using the builder pattern.
type SnapshotBuilder struct {
	clients    *k8s.Clients
	namespace  string
	collectors []Collector
	snapshot   *EnvironmentSnapshot
}

// NewSnapshotBuilder creates a new SnapshotBuilder with default collectors.
func NewSnapshotBuilder(clients *k8s.Clients, namespace string) *SnapshotBuilder {
	return &SnapshotBuilder{
		clients:    clients,
		namespace:  namespace,
		collectors: DefaultCollectors(),
		snapshot: &EnvironmentSnapshot{
			CapturedAt: time.Now(),
		},
	}
}

// Build executes all collectors and returns the completed snapshot.
func (b *SnapshotBuilder) Build(ctx context.Context) (*EnvironmentSnapshot, error) {
	for _, c := range b.collectors {
		log.Printf("Collecting %s...", c.Name())
		if err := c.Collect(ctx, b.clients, b.namespace, b.snapshot); err != nil {
			log.Printf("Warning: %s collector failed: %v", c.Name(), err)
			b.snapshot.Warnings = append(b.snapshot.Warnings, c.Name()+": "+err.Error())
		}
	}
	return b.snapshot, nil
}
