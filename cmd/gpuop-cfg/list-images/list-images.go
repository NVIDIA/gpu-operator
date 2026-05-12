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

package listimages

import (
	"context"
	"fmt"
	"io"
	"os"

	"github.com/operator-framework/api/pkg/operators/v1alpha1"
	"github.com/sirupsen/logrus"
	cli "github.com/urfave/cli/v3"
	"sigs.k8s.io/yaml"

	v1 "github.com/NVIDIA/gpu-operator/api/nvidia/v1"
	"github.com/NVIDIA/gpu-operator/cmd/gpuop-cfg/internal/images"
)

type options struct {
	input string
}

func NewCommand(_ *logrus.Logger) *cli.Command {
	listImages := cli.Command{
		Name:  "list-images",
		Usage: "List container images referenced in GPU Operator configuration files",
	}

	listImages.Commands = []*cli.Command{
		buildCSV(),
		buildClusterPolicy(),
	}

	return &listImages
}

func buildCSV() *cli.Command {
	opts := options{}

	c := cli.Command{
		Name:  "csv",
		Usage: "List images from a ClusterServiceVersion manifest",
		Action: func(ctx context.Context, cmd *cli.Command) error {
			contents, err := getContents(opts.input)
			if err != nil {
				return fmt.Errorf("failed to read file: %v", err)
			}

			spec := &v1alpha1.ClusterServiceVersion{}
			if err := yaml.Unmarshal(contents, spec); err != nil {
				return fmt.Errorf("failed to unmarshal csv: %v", err)
			}

			for _, image := range images.FromCSV(spec) {
				fmt.Println(image)
			}
			return nil
		},
	}

	c.Flags = []cli.Flag{
		&cli.StringFlag{
			Name:        "input",
			Usage:       "Specify the input file. If this is '-' the file is read from STDIN",
			Value:       "-",
			Destination: &opts.input,
		},
	}

	return &c
}

func buildClusterPolicy() *cli.Command {
	opts := options{}

	c := cli.Command{
		Name:  "clusterpolicy",
		Usage: "List images from a ClusterPolicy manifest",
		Action: func(ctx context.Context, cmd *cli.Command) error {
			contents, err := getContents(opts.input)
			if err != nil {
				return fmt.Errorf("failed to read file: %v", err)
			}

			spec := &v1.ClusterPolicy{}
			if err := yaml.Unmarshal(contents, spec); err != nil {
				return fmt.Errorf("failed to unmarshal clusterpolicy: %v", err)
			}

			operandImages, err := images.FromClusterPolicy(&spec.Spec)
			if err != nil {
				return err
			}

			for _, op := range operandImages {
				fmt.Println(op.Image)
			}
			return nil
		},
	}

	c.Flags = []cli.Flag{
		&cli.StringFlag{
			Name:        "input",
			Usage:       "Specify the input file. If this is '-' the file is read from STDIN",
			Value:       "-",
			Destination: &opts.input,
		},
	}

	return &c
}

func getContents(input string) ([]byte, error) {
	if input == "-" {
		return io.ReadAll(os.Stdin)
	}
	return os.ReadFile(input)
}
