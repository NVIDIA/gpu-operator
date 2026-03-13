/**
# Copyright (c) NVIDIA CORPORATION.  All rights reserved.
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

package helpers

import (
	"bytes"
	"context"
	"fmt"
	"os/exec"
	"time"
)

type OperatorClient struct {
	Chart      string
	Namespace  string
	Kubeconfig string
}

type ChartOptions struct {
	CleanupOnFail bool
	ReleaseName   string
	Timeout       time.Duration
	Wait          bool
}

func (op *OperatorClient) Install(ctx context.Context, params []string, chartOpts ChartOptions) (string, error) {
	if op.Chart == "" {
		return "", fmt.Errorf("chart must be provided")
	}
	if chartOpts.ReleaseName == "" {
		return "", fmt.Errorf("release name must be provided")
	}

	args := []string{"install", chartOpts.ReleaseName, op.Chart}
	args = append(args, "-n", op.Namespace, "--kubeconfig", op.Kubeconfig)

	for _, param := range params {
		args = append(args, "--set", param)
	}

	if chartOpts.Wait {
		args = append(args, "--wait")
	}

	if chartOpts.Timeout > 0 {
		args = append(args, "--timeout", chartOpts.Timeout.String())
	}

	if chartOpts.CleanupOnFail {
		args = append(args, "--cleanup-on-fail")
	}

	cmd := exec.CommandContext(ctx, "helm", args...)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	if err := cmd.Run(); err != nil {
		return "", fmt.Errorf("error installing operator: %w: %s", err, stderr.String())
	}

	return chartOpts.ReleaseName, nil
}

func (op *OperatorClient) Uninstall(releaseName string) error {
	args := []string{"uninstall", releaseName, "-n", op.Namespace, "--kubeconfig", op.Kubeconfig}

	cmd := exec.Command("helm", args...)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr

	if err := cmd.Run(); err != nil {
		return fmt.Errorf("error uninstalling release %s: %w: %s", releaseName, err, stderr.String())
	}

	return nil
}
