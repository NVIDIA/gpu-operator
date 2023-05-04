/*
 * Copyright (c), NVIDIA CORPORATION.  All rights reserved.
 *
 * Licensed under the Apache License, Version 2.0 (the "License");
 * you may not use this file except in compliance with the License.
 * You may obtain a copy of the License at
 *
 *     http://www.apache.org/licenses/LICENSE-2.0
 *
 * Unless required by applicable law or agreed to in writing, software
 * distributed under the License is distributed on an "AS IS" BASIS,
 * WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
 * See the License for the specific language governing permissions and
 * limitations under the License.
 */

package validate

import (
	"github.com/sirupsen/logrus"
	cli "github.com/urfave/cli/v2"

	"github.com/NVIDIA/gpu-operator/cmd/gpuop-cfg/validate/clusterpolicy"
	"github.com/NVIDIA/gpu-operator/cmd/gpuop-cfg/validate/csv"
)

type command struct {
	logger *logrus.Logger
}

// NewCommand constructs a validate command with the specified logger
func NewCommand(logger *logrus.Logger) *cli.Command {
	c := command{
		logger: logger,
	}
	return c.build()
}

func (m command) build() *cli.Command {
	// Create the 'validate' command
	validate := cli.Command{
		Name:  "validate",
		Usage: "Perform various validations for GPU Operator configuration files",
	}

	validate.Subcommands = []*cli.Command{
		csv.NewCommand(m.logger),
		clusterpolicy.NewCommand(m.logger),
	}

	return &validate
}
