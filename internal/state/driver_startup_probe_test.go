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

package state

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestDriverStartupProbeDevices(t *testing.T) {
	for _, manifest := range []string{"../../assets/state-driver/0400_configmap.yaml", "../../manifests/state-driver/0400_configmap.yaml"} {
		t.Run(manifest, func(t *testing.T) {
			content, err := os.ReadFile(manifest)
			require.NoError(t, err)
			_, script, found := strings.Cut(string(content), "  startup-probe.sh: |-\n")
			require.True(t, found)
			script = strings.TrimPrefix(strings.ReplaceAll(script, "\n    ", "\n"), "    ")

			for name, tc := range map[string]struct {
				missingDevice string
				regularDevice string
				mkdirFailure  bool
				smiFailure    bool
				missingModule bool
				daemonsFailed bool
				wantError     string
			}{
				"ready":                    {},
				"missing kernel module":    {missingModule: true, wantError: "NVIDIA kernel module not loaded"},
				"daemons not ready":        {daemonsFailed: true, wantError: "NVIDIA driver daemons are not ready"},
				"mkdir failure":            {mkdirFailure: true, wantError: "Failed to create"},
				"nvidia-smi failure":       {smiFailure: true, wantError: "nvidia-smi failed"},
				"missing control device":   {missingDevice: "nvidiactl", wantError: "missing character device"},
				"missing UVM device":       {missingDevice: "nvidia-uvm", wantError: "missing character device"},
				"missing UVM tools device": {missingDevice: "nvidia-uvm-tools", wantError: "missing character device"},
				"regular file":             {regularDevice: "nvidia-uvm", wantError: "missing character device"},
			} {
				t.Run(name, func(t *testing.T) {
					root := t.TempDir()
					devDir := filepath.Join(root, "dev")
					require.NoError(t, os.Mkdir(devDir, 0700))
					for _, device := range []string{"nvidiactl", "nvidia-uvm", "nvidia-uvm-tools"} {
						switch device {
						case tc.missingDevice:
							continue
						case tc.regularDevice:
							require.NoError(t, os.WriteFile(filepath.Join(devDir, device), nil, 0600))
						default:
							// Use a real character device without requiring GPU hardware or mknod privileges.
							require.NoError(t, os.Symlink("/dev/null", filepath.Join(devDir, device)))
						}
					}
					if tc.mkdirFailure {
						require.NoError(t, os.WriteFile(filepath.Join(devDir, "char"), nil, 0600))
					}
					refcnt := filepath.Join(root, "refcnt")
					if !tc.missingModule {
						require.NoError(t, os.WriteFile(refcnt, nil, 0600))
					}
					smiCommand := `#!/bin/sh
[ -d "$DRIVER_PROBE_TEST_DEV_DIR/char" ] || exit 1
`
					if tc.smiFailure {
						smiCommand += "exit 1\n"
					} else {
						smiCommand += "exit 0\n"
					}
					require.NoError(t, os.WriteFile(filepath.Join(root, "nvidia-smi"), []byte(smiCommand), 0700)) // #nosec G306 -- The test stub must be executable and is accessible only to its owner.
					validations := filepath.Join(root, "validations")
					require.NoError(t, os.Mkdir(validations, 0700))
					readyFile := filepath.Join(validations, ".driver-ctr-ready")
					if tc.daemonsFailed {
						require.NoError(t, os.WriteFile(filepath.Join(validations, ".driver-daemons-status"), []byte("NotReady"), 0600))
					}
					probe := strings.NewReplacer(
						"/dev/", devDir+"/",
						"/sys/module/nvidia/refcnt", refcnt,
						"/run/nvidia/validations", validations,
					).Replace(script)
					cmd := exec.Command("sh", "-c", probe)
					cmd.Env = append(os.Environ(), "PATH="+root+":"+os.Getenv("PATH"), "DRIVER_PROBE_TEST_DEV_DIR="+devDir)
					output, err := cmd.CombinedOutput()
					if tc.wantError == "" {
						require.NoError(t, err, string(output))
						require.FileExists(t, readyFile)
						require.DirExists(t, filepath.Join(devDir, "char"))
					} else {
						require.Error(t, err)
						require.Contains(t, string(output), tc.wantError)
						require.NoFileExists(t, readyFile)
					}
				})
			}
		})
	}
}
