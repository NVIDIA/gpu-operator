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

package reporting

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	"github.com/NVIDIA/gpu-operator/internal/certification"
	"github.com/NVIDIA/gpu-operator/internal/certification/k8s"
)

// Config holds reporting configuration from environment.
type Config struct {
	ConfigMapName string
	Namespace     string
	OutputDir     string
}

// Reporter handles output formatting and storage.
type Reporter struct {
	clients *k8s.Clients
	cfg     Config
}

// New creates a new Reporter.
func New(clients *k8s.Clients, cfg Config) *Reporter {
	return &Reporter{clients: clients, cfg: cfg}
}

// Output formats and stores the results.
// JSON is always printed to stdout. Additionally:
//   - If OutputDir is set, results.json and report.html are written to that directory.
//   - If ConfigMapName is set, both are stored in a Kubernetes ConfigMap.
func (r *Reporter) Output(ctx context.Context, results *certification.SuiteResult) error {
	jsonOutput, err := json.MarshalIndent(results, "", "  ")
	if err != nil {
		return fmt.Errorf("marshaling results: %w", err)
	}
	fmt.Println(string(jsonOutput))

	htmlOutput, err := GenerateHTMLReport(results)
	if err != nil {
		return fmt.Errorf("generating HTML report: %w", err)
	}

	if r.cfg.OutputDir != "" {
		if err := r.writeLocalFiles(jsonOutput, htmlOutput); err != nil {
			fmt.Fprintf(os.Stderr, "warning: failed to write local files: %v\n", err)
		}
	}

	if r.cfg.ConfigMapName != "" {
		r.storeInConfigMap(ctx, jsonOutput, htmlOutput)
	}

	return nil
}

func (r *Reporter) writeLocalFiles(jsonOutput []byte, htmlOutput string) error {
	if err := os.MkdirAll(r.cfg.OutputDir, 0o755); err != nil {
		return fmt.Errorf("creating output directory: %w", err)
	}

	jsonPath := filepath.Join(r.cfg.OutputDir, "results.json")
	if err := os.WriteFile(jsonPath, jsonOutput, 0o600); err != nil {
		return fmt.Errorf("writing results.json: %w", err)
	}

	htmlPath := filepath.Join(r.cfg.OutputDir, "report.html")
	if err := os.WriteFile(htmlPath, []byte(htmlOutput), 0o600); err != nil {
		return fmt.Errorf("writing report.html: %w", err)
	}

	fmt.Fprintf(os.Stderr, "\nResults written to:\n")
	fmt.Fprintf(os.Stderr, "  %s\n", jsonPath)
	fmt.Fprintf(os.Stderr, "  %s\n", htmlPath)
	return nil
}

func (r *Reporter) storeInConfigMap(ctx context.Context, jsonOutput []byte, htmlOutput string) {
	data := map[string]string{
		"results.json": string(jsonOutput),
		"report.html":  htmlOutput,
	}
	if err := r.clients.CreateOrUpdateConfigMap(ctx, r.cfg.Namespace, r.cfg.ConfigMapName, data); err != nil {
		fmt.Fprintf(os.Stderr, "warning: failed to store results in ConfigMap: %v\n", err)
		return
	}

	fmt.Fprintf(os.Stderr, "\nResults stored in ConfigMap '%s' in namespace '%s'.\n", r.cfg.ConfigMapName, r.cfg.Namespace)
	fmt.Fprintf(os.Stderr, "\nDownload results:\n")
	fmt.Fprintf(os.Stderr, "  kubectl get configmap %s -n %s -o jsonpath='{.data.results\\.json}' > results.json\n", r.cfg.ConfigMapName, r.cfg.Namespace)
	fmt.Fprintf(os.Stderr, "  kubectl get configmap %s -n %s -o jsonpath='{.data.report\\.html}' > report.html\n", r.cfg.ConfigMapName, r.cfg.Namespace)
}
