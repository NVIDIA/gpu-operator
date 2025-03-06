/**
# Copyright (c), NVIDIA CORPORATION.  All rights reserved.
#
# Licensed under the Apache License, Version 2.0 (the "License");
# you may not use this file except in compliance with the License.
# You may obtain a copy of the License at
#
#     http://www.apache.org/licenses/LICENSE-2.0
#
# Unless required by applicable law or agreed to in writing, software
# distributed under the License is distributed on an "AS IS" BASIS,
# WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
# See the License for the specific language governing permissions and
# limitations under the License.
**/

package clusterpolicy

import (
	"fmt"
	"io"
	"os"

	"github.com/sirupsen/logrus"
	"github.com/urfave/cli/v2"
	"sigs.k8s.io/yaml"

	v1 "github.com/NVIDIA/gpu-operator/api/nvidia/v1"
)

type command struct {
	logger *logrus.Logger
}

type options struct {
	input string
}

// NewCommand constructs a clusterpolicy command with the specified logger
func NewCommand(logger *logrus.Logger) *cli.Command {
	c := command{
		logger: logger,
	}
	return c.build()
}

// build creates the CLI command
func (m command) build() *cli.Command {
	opts := options{}

	// Create the 'clusterpolicy' command
	c := cli.Command{
		Name:  "clusterpolicy",
		Usage: "Validate clusterpolicy",
		Before: func(c *cli.Context) error {
			return m.validateFlags(c, &opts)
		},
		Action: func(c *cli.Context) error {
			return m.run(c, &opts)
		},
	}

	c.Flags = []cli.Flag{
		&cli.StringFlag{
			Name:        "input",
			Usage:       "Specify the input file containing the clusterpolicy yaml. If this is '-' the file is read from STDIN",
			Value:       "-",
			Destination: &opts.input,
		},
	}

	return &c
}

func (m command) validateFlags(c *cli.Context, opts *options) error {
	return nil
}

func (m command) run(c *cli.Context, opts *options) error {
	cp, err := opts.load()
	if err != nil {
		return fmt.Errorf("failed to load clusterpolicy spec: %v", err)
	}

	err = validateImages(c.Context, &cp.Spec)
	if err != nil {
		return fmt.Errorf("failed to validate images: %v", err)
	}

	return nil
}

func (o options) load() (*v1.ClusterPolicy, error) {
	contents, err := o.getContents()
	if err != nil {
		return nil, fmt.Errorf("failed to read file: %v", err)
	}

	spec := &v1.ClusterPolicy{}
	err = yaml.Unmarshal(contents, spec)
	if err != nil {
		return nil, fmt.Errorf("failed to unmarshal spec: %v", err)
	}
	return spec, nil
}

func (o options) getContents() ([]byte, error) {
	if o.input == "-" {
		return io.ReadAll(os.Stdin)
	}

	return os.ReadFile(o.input)
}
