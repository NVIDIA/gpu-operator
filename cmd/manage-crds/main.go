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

	"github.com/NVIDIA/k8s-operator-libs/pkg/crdutil"
	log "github.com/sirupsen/logrus"
	"github.com/urfave/cli/v3"

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
