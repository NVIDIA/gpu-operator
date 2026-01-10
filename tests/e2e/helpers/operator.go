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
	"bufio"
	"bytes"
	"context"
	"fmt"
	"os/exec"
	"strings"
	"time"
)

type OperatorClient struct {
	Chart      string
	Namespace  string
	Kubeconfig string
}

type ChartOptions struct {
	CleanupOnFail bool
	GenerateName  bool
	ReleaseName   string
	Timeout       time.Duration
	Wait          bool
}

func (op *OperatorClient) Install(ctx context.Context, params []string, chartOpts ChartOptions) (string, error) {
	if op.Chart == "" {
		return "", fmt.Errorf("chart must be provided")
	}

	args := []string{"install"}

	if !chartOpts.GenerateName {
		if chartOpts.ReleaseName == "" {
			return "", fmt.Errorf("release name must be provided when the GenerateName chart option is unset")
		}
		args = append(args, chartOpts.ReleaseName)
	}

	args = append(args, op.Chart)
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

	releaseName, err := parseReleaseName(stdout.String())
	if err != nil {
		return "", fmt.Errorf("error parsing release name from helm output: %w", err)
	}

	return releaseName, nil
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

// parseReleaseName extracts the release name from helm install output.
// Expected format: "NAME: <release-name>"
func parseReleaseName(output string) (string, error) {
	scanner := bufio.NewScanner(strings.NewReader(output))
	for scanner.Scan() {
		line := scanner.Text()
		if strings.HasPrefix(line, "NAME:") {
			parts := strings.SplitN(line, ":", 2)
			if len(parts) == 2 {
				return strings.TrimSpace(parts[1]), nil
			}
		}
	}
	return "", fmt.Errorf("release name not found in helm output")
}
