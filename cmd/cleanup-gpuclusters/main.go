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

func main() {
	var debug bool
	var crName string

	c := cli.Command{}
	c.Name = "cleanup-gpuclusters"
	c.Usage = "Delete the chart-managed GPUCluster CR and wait until it is gone"
	c.Version = info.GetVersionString()
	c.Flags = []cli.Flag{
		&cli.BoolFlag{
			Name:        "debug",
			Aliases:     []string{"d"},
			Usage:       "Enable debug-level logging",
			Destination: &debug,
			Sources:     cli.EnvVars("DEBUG"),
		},
		&cli.StringFlag{
			Name:        "gpucluster-name",
			Usage:       "Name of the chart-managed GPUCluster CR to delete",
			Required:    true,
			Destination: &crName,
			Sources:     cli.EnvVars("GPUCLUSTER_NAME"),
		},
	}
	c.Before = func(ctx context.Context, cli *cli.Command) (context.Context, error) {
		logLevel := log.InfoLevel
		if debug {
			logLevel = log.DebugLevel
		}
		logger.SetLevel(logLevel)
		return ctx, nil
	}
	c.Action = func(ctx context.Context, _ *cli.Command) error {
		return runDeleteGPUCluster(ctx, crName)
	}

	err := c.Run(context.Background(), os.Args)
	if err != nil {
		log.Errorf("%v", err)
		log.Exit(1)
	}
}

// runDeleteGPUCluster deletes the named GPUCluster CR and blocks until it is gone. The
// GPUCluster controller drains ResourceClaim-consuming operands under a finalizer
// before the CR disappears, so waiting here (from the chart's pre-delete hook) keeps
// the operator alive until that ordered teardown has completed. Scoped to the chart's
// own CR by name so CRs created outside the chart are never touched.
func runDeleteGPUCluster(ctx context.Context, name string) error {
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
		cr := &nvidiav1alpha1.GPUCluster{}
		if err := k8sClient.Get(ctx, ctrlclient.ObjectKey{Name: name}, cr); err != nil {
			// The GPUCluster CRD may not be installed (e.g. cleanup enabled without
			// the DRA stack ever deployed); nothing to drain in that case.
			if meta.IsNoMatchError(err) {
				logger.Info("GPUCluster CRD not installed, nothing to delete")
				return nil
			}
			if apierrors.IsNotFound(err) {
				logger.Infof("GPUCluster %s deleted", name)
				return nil
			}
			return fmt.Errorf("failed to get GPUCluster %s: %w", name, err)
		}
		if cr.DeletionTimestamp.IsZero() {
			logger.Infof("Deleting GPUCluster %s", name)
			if err := k8sClient.Delete(ctx, cr); err != nil && !apierrors.IsNotFound(err) {
				return fmt.Errorf("failed to delete GPUCluster %s: %w", name, err)
			}
		}
		logger.Infof("Waiting for GPUCluster %s to be deleted", name)
		select {
		case <-ctx.Done():
			return fmt.Errorf("timed out waiting for GPUCluster %s to be deleted: %w", name, ctx.Err())
		case <-time.After(5 * time.Second):
		}
	}
}
