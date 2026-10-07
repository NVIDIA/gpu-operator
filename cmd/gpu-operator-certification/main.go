package main

import (
	"context"
	"fmt"
	"log"
	"os"
	"os/signal"
	"syscall"

	"github.com/NVIDIA/gpu-operator-self-certification/internal/certification"
	"github.com/NVIDIA/gpu-operator-self-certification/internal/certification/k8s"
	"github.com/NVIDIA/gpu-operator-self-certification/internal/reporting"

	// Register TestSets
	_ "github.com/NVIDIA/gpu-operator-self-certification/internal/testsuite"
)

func main() {
	ctx, cancel := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer cancel()

	if err := run(ctx); err != nil {
		log.Fatalf("error: %v", err)
	}
}

func run(ctx context.Context) error {
	cfg := certification.LoadConfigFromEnv()

	clients, err := k8s.NewClients()
	if err != nil {
		return fmt.Errorf("creating k8s clients: %w", err)
	}

	r := certification.NewRunner(clients, cfg)
	results := r.Run(ctx)

	reporter := reporting.New(clients, reporting.Config{
		ConfigMapName: os.Getenv("RESULTS_CONFIGMAP_NAME"),
		Namespace:     cfg.Namespace,
		OutputDir:     getEnvDefault("OUTPUT_DIR", "output"),
	})
	if err := reporter.Output(ctx, results); err != nil {
		return err
	}

	if results.Summary.Failed > 0 {
		log.Printf("%d test(s) failed", results.Summary.Failed)
	}

	return nil
}

func getEnvDefault(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}
