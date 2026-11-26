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

package main

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestGetDriverInfo(t *testing.T) {
	tests := map[string]struct {
		isHostDriver      bool
		hostRoot          string
		driverInstallDir  string
		devicesInDriver   bool
		wantDriverCtrPath string
	}{
		"host driver": {
			isHostDriver:      true,
			hostRoot:          "/",
			wantDriverCtrPath: "/host",
		},
		"host driver with custom host root": {
			isHostDriver:      true,
			hostRoot:          "/custom-host",
			wantDriverCtrPath: "/host",
		},
		"container driver with host devices": {
			hostRoot:          "/",
			driverInstallDir:  "/run/nvidia/driver",
			wantDriverCtrPath: "/driver-root/driver",
		},
		"container driver with driver devices": {
			hostRoot:          "/",
			driverInstallDir:  "/run/nvidia/driver",
			devicesInDriver:   true,
			wantDriverCtrPath: "/driver-root/driver",
		},
		"custom driver directory with driver devices": {
			hostRoot:          "/custom-host",
			driverInstallDir:  "/opt/nvidia/current",
			devicesInDriver:   true,
			wantDriverCtrPath: "/driver-root/current",
		},
		"custom driver directory with trailing slash": {
			hostRoot:          "/",
			driverInstallDir:  "/opt/nvidia/current/",
			devicesInDriver:   true,
			wantDriverCtrPath: "/driver-root/current",
		},
		"top-level driver directory with host devices": {
			hostRoot:          "/custom-host",
			driverInstallDir:  "/custom-root",
			wantDriverCtrPath: "/driver-root/custom-root",
		},
	}

	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			// The validation mount can differ from the management container mount.
			validationRoot := t.TempDir()
			if tc.devicesInDriver {
				require.NoError(t, os.MkdirAll(filepath.Join(validationRoot, "dev"), 0750))
			}

			got := getDriverInfo(tc.isHostDriver, tc.hostRoot, tc.driverInstallDir, validationRoot)
			want := driverInfo{
				isHostDriver:      tc.isHostDriver,
				hostRoot:          tc.hostRoot,
				driverRoot:        tc.driverInstallDir,
				driverRootCtrPath: tc.wantDriverCtrPath,
				devRoot:           tc.hostRoot,
				devRootCtrPath:    "/host",
			}
			if tc.isHostDriver {
				want.driverRoot = tc.hostRoot
			} else if tc.devicesInDriver {
				want.devRoot = tc.driverInstallDir
				want.devRootCtrPath = tc.wantDriverCtrPath
			}
			require.Equal(t, want, got)
		})
	}
}
