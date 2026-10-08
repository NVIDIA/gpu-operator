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

package helm_test

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
	"sigs.k8s.io/yaml"
)

func TestDCGMExporterServiceAccountRendering(t *testing.T) {
	helm, err := exec.LookPath("helm")
	if err != nil {
		t.Skip("Helm is required for chart rendering tests")
	}
	for _, kind := range []string{"clusterpolicy", "gpucluster"} {
		t.Run(kind, func(t *testing.T) {
			for name, tc := range map[string]struct {
				args     []string
				values   string
				expected string
			}{
				"unset":          {},
				"empty block":    {args: []string{"--set-json", "dcgmExporter.serviceAccount={}"}},
				"null block":     {args: []string{"--set-json", "dcgmExporter.serviceAccount=null"}},
				"empty name":     {args: []string{"--set-string", "dcgmExporter.serviceAccount.name="}},
				"null name":      {args: []string{"--set-json", "dcgmExporter.serviceAccount.name=null"}},
				"custom name":    {args: []string{"--set", "dcgmExporter.serviceAccount.name=metrics-identity"}, expected: "metrics-identity"},
				"integer":        {args: []string{"--set", "dcgmExporter.serviceAccount.name=123"}, expected: "123"},
				"zero":           {args: []string{"--set", "dcgmExporter.serviceAccount.name=0"}, expected: "0"},
				"true":           {args: []string{"--set", "dcgmExporter.serviceAccount.name=true"}, expected: "true"},
				"false":          {args: []string{"--set", "dcgmExporter.serviceAccount.name=false"}, expected: "false"},
				"string integer": {args: []string{"--set-string", "dcgmExporter.serviceAccount.name=123"}, expected: "123"},
				"string false":   {args: []string{"--set-string", "dcgmExporter.serviceAccount.name=false"}, expected: "false"},
				"values zero":    {values: "dcgmExporter:\n  serviceAccount:\n    name: 0\n", expected: "0"},
				"values false":   {values: "dcgmExporter:\n  serviceAccount:\n    name: false\n", expected: "false"},
			} {
				t.Run(name, func(t *testing.T) {
					args := []string{"template", "identity-test", "../../deployments/gpu-operator", "--namespace", "gpu-operator",
						"--show-only", "templates/" + kind + ".yaml"}
					if kind == "gpucluster" {
						args = append(args, "--set", "clusterPolicy.deployCR=false,gpuCluster.deployCR=true,driver.nvidiaDriverCRD.enabled=true",
							"--api-versions", "resource.k8s.io/v1/DeviceClass")
					}
					args = append(args, tc.args...)
					if tc.values != "" {
						path := filepath.Join(t.TempDir(), "values.yaml")
						require.NoError(t, os.WriteFile(path, []byte(tc.values), 0o600))
						args = append(args, "--values", path)
					}
					output, err := exec.CommandContext(t.Context(), helm, args...).CombinedOutput()
					require.NoError(t, err, "%s", output)
					var cr struct {
						Spec struct {
							DCGMExporter map[string]any `json:"dcgmExporter"`
						} `json:"spec"`
					}
					require.NoError(t, yaml.Unmarshal(output, &cr))
					if tc.expected == "" {
						require.NotContains(t, cr.Spec.DCGMExporter, "serviceAccount")
					} else {
						require.Equal(t, map[string]any{"name": tc.expected}, cr.Spec.DCGMExporter["serviceAccount"],
							"the rendered name must be a string, including false and zero")
					}
				})
			}
		})
	}
}
