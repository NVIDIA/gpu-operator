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
	"strings"
	"time"

	log "github.com/sirupsen/logrus"
	"github.com/urfave/cli/v3"

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
	var timeout time.Duration

	c := cli.Command{}
	c.Name = "check-gpuclusters"
	c.Usage = "Fail while GPUCluster CRs exist so that helm uninstall aborts before the operator is removed"
	c.Version = info.GetVersionString()
	c.Flags = []cli.Flag{
		&cli.BoolFlag{
			Name:        "debug",
			Aliases:     []string{"d"},
			Usage:       "Enable debug-level logging",
			Destination: &debug,
			Sources:     cli.EnvVars("DEBUG"),
		},
		&cli.DurationFlag{
			Name:        "timeout",
			Usage:       "How long to wait for already-terminating GPUCluster CRs to be deleted",
			Value:       5 * time.Minute,
			Destination: &timeout,
			Sources:     cli.EnvVars("TIMEOUT"),
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
		return checkGPUClusters(ctx, timeout)
	}

	err := c.Run(context.Background(), os.Args)
	if err != nil {
		log.Errorf("%v", err)
		log.Exit(1)
	}
}

// checkGPUClusters fails while any GPUCluster CR exists, so that the chart's pre-delete hook
// aborts helm uninstall before the operator is removed. A GPUCluster deleted after the
// operator is gone has no controller to process its finalizer: the CR stays stuck
// terminating and the DRA operands keep running. CRs that are already terminating are
// waited on instead of failed on, so an uninstall that follows a delete succeeds
// once the finalizer drain completes. The guard only lists CRs; it never deletes them.
func checkGPUClusters(ctx context.Context, timeout time.Duration) error {
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

	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	for {
		list := &nvidiav1alpha1.GPUClusterList{}
		if err := k8sClient.List(ctx, list); err != nil {
			// The GPUCluster CRD may not be installed (e.g. the DRA stack was never
			// deployed); nothing to check in that case.
			if meta.IsNoMatchError(err) {
				logger.Info("GPUCluster CRD not installed, nothing to check")
				return nil
			}
			return fmt.Errorf("failed to list GPUCluster objects: %w", err)
		}

		var live []string
		terminating := 0
		for _, cr := range list.Items {
			if cr.DeletionTimestamp.IsZero() {
				live = append(live, cr.Name)
			} else {
				terminating++
			}
		}
		if len(live) > 0 {
			return fmt.Errorf("GPUCluster CR(s) %s exist; delete them (kubectl delete gpuclusters %s) and wait for the deletion to complete, then retry the uninstall",
				strings.Join(live, ", "), strings.Join(live, " "))
		}
		if terminating == 0 {
			logger.Info("No GPUCluster CRs found")
			return nil
		}
		logger.Infof("Waiting for %d terminating GPUCluster CR(s) to be deleted", terminating)
		select {
		case <-ctx.Done():
			return fmt.Errorf("timed out waiting for terminating GPUCluster CRs to be deleted: %w", ctx.Err())
		case <-time.After(5 * time.Second):
		}
	}
}
