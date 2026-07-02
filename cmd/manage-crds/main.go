/*
Copyright (c), NVIDIA CORPORATION.  All rights reserved.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package main

import (
	"context"
	"fmt"
	"os"
	"time"

	"github.com/NVIDIA/k8s-operator-libs/pkg/crdutil"
	log "github.com/sirupsen/logrus"
	"github.com/urfave/cli/v3"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	"k8s.io/apimachinery/pkg/runtime"
	ctrlclient "sigs.k8s.io/controller-runtime/pkg/client"
	ctrlconfig "sigs.k8s.io/controller-runtime/pkg/client/config"

	nvidiav1alpha1 "github.com/NVIDIA/gpu-operator/api/nvidia/v1alpha1"
	"github.com/NVIDIA/gpu-operator/internal/info"
)

var logger = log.New()

type config struct {
	Debug     bool
	crdsPaths []string
}

func main() {
	config := config{}

	// Create the top-level CLI
	c := cli.Command{}
	c.Name = "manage-crds"
	c.Usage = "Tools for managing Custom Resource Definitions (CRDs) for NVIDIA GPU Operator"
	c.Version = info.GetVersionString()

	// Setup the flags for this command
	c.Flags = []cli.Flag{
		&cli.BoolFlag{
			Name:        "debug",
			Aliases:     []string{"d"},
			Usage:       "Enable debug-level logging",
			Destination: &config.Debug,
			Sources:     cli.EnvVars("DEBUG"),
		},
	}

	// Set log-level for all subcommands
	c.Before = func(ctx context.Context, cli *cli.Command) (context.Context, error) {
		logLevel := log.InfoLevel
		if config.Debug {
			logLevel = log.DebugLevel
		}
		logger.SetLevel(logLevel)
		return ctx, nil
	}

	// Common flags for both apply and delete subcommands
	commonFlags := []cli.Flag{
		&cli.StringSliceFlag{
			Name:        "filepath",
			Aliases:     []string{"f"},
			Usage:       "Path to CRD manifest file or directory (can be specified multiple times, directories are searched recursively)",
			Required:    true,
			Destination: &config.crdsPaths,
		},
	}

	// Define the subcommands
	c.Commands = []*cli.Command{
		{
			Name:  "apply",
			Usage: "Apply CRDs from the specified path",
			Flags: commonFlags,
			Action: func(ctx context.Context, cli *cli.Command) error {
				return runApply(ctx, config)
			},
		},
		{
			Name:  "delete",
			Usage: "Delete CRDs from the specified path",
			Flags: commonFlags,
			Action: func(ctx context.Context, cli *cli.Command) error {
				return runDelete(ctx, config)
			},
		},
		{
			Name:  "delete-gpuclusters",
			Usage: "Delete all GPUCluster CRs and wait until they are gone",
			Action: func(ctx context.Context, _ *cli.Command) error {
				return runDeleteGPUClusters(ctx)
			},
		},
	}

	err := c.Run(context.Background(), os.Args)
	if err != nil {
		log.Errorf("%v", err)
		log.Exit(1)
	}
}

func runApply(ctx context.Context, cfg config) error {
	paths := cfg.crdsPaths
	logger.Infof("Applying CRDs from %d path(s): %v", len(paths), paths)

	if err := crdutil.ProcessCRDs(ctx, crdutil.CRDOperationApply, paths...); err != nil {
		return fmt.Errorf("failed to apply CRDs: %w", err)
	}

	logger.Info("Successfully applied CRDs")
	return nil
}

func runDelete(ctx context.Context, cfg config) error {
	paths := cfg.crdsPaths
	logger.Infof("Deleting CRDs from %d path(s): %v", len(paths), paths)

	if err := crdutil.ProcessCRDs(ctx, crdutil.CRDOperationDelete, paths...); err != nil {
		return fmt.Errorf("failed to delete CRDs: %w", err)
	}

	logger.Info("Successfully deleted CRDs")
	return nil
}

// runDeleteGPUClusters deletes every GPUCluster CR and blocks until all are gone. The
// GPUCluster controller drains ResourceClaim-consuming operands under a finalizer
// before the CR disappears, so waiting here (from the chart's pre-delete hook) keeps
// the operator alive until that ordered teardown has completed.
func runDeleteGPUClusters(ctx context.Context) error {
	scheme := runtime.NewScheme()
	if err := nvidiav1alpha1.AddToScheme(scheme); err != nil {
		return fmt.Errorf("failed to add GPUCluster types to scheme: %w", err)
	}
	restConfig, err := ctrlconfig.GetConfig()
	if err != nil {
		return fmt.Errorf("failed to get kubeconfig: %w", err)
	}
	k8sClient, err := ctrlclient.New(restConfig, ctrlclient.Options{Scheme: scheme})
	if err != nil {
		return fmt.Errorf("failed to create client: %w", err)
	}

	ctx, cancel := context.WithTimeout(ctx, 5*time.Minute)
	defer cancel()

	for {
		list := &nvidiav1alpha1.GPUClusterList{}
		if err := k8sClient.List(ctx, list); err != nil {
			// The GPUCluster CRD may not be installed (e.g. cleanup enabled without
			// the DRA stack ever deployed); nothing to drain in that case.
			if meta.IsNoMatchError(err) {
				logger.Info("GPUCluster CRD not installed, nothing to delete")
				return nil
			}
			return fmt.Errorf("failed to list GPUClusters: %w", err)
		}
		if len(list.Items) == 0 {
			logger.Info("All GPUCluster CRs deleted")
			return nil
		}
		for i := range list.Items {
			cr := &list.Items[i]
			if cr.DeletionTimestamp.IsZero() {
				logger.Infof("Deleting GPUCluster %s", cr.Name)
				if err := k8sClient.Delete(ctx, cr); err != nil && !apierrors.IsNotFound(err) {
					return fmt.Errorf("failed to delete GPUCluster %s: %w", cr.Name, err)
				}
			}
		}
		logger.Infof("Waiting for %d GPUCluster CR(s) to be deleted", len(list.Items))
		select {
		case <-ctx.Done():
			return fmt.Errorf("timed out waiting for GPUCluster CRs to be deleted: %w", ctx.Err())
		case <-time.After(5 * time.Second):
		}
	}
}
