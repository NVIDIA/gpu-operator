/*
# Copyright 2024 NVIDIA CORPORATION
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
*/

package main

import (
	"context"
	"fmt"
	"os"
	"os/exec"

	"github.com/NVIDIA/gpu-operator/internal/driver"
	"github.com/NVIDIA/gpu-operator/internal/utils"
)

// driverInfo contains information about an NVIDIA driver installation.
//
// isHostDriver indicates whether the driver is installed directly on
// the host at the host's root filesystem.
//
// hostRoot represents the host's root filesystem (typically '/').
//
// driverRoot and devRoot represent the absolute paths of the driver install
// and NVIDIA device nodes on the host.
//
// driverRootCtrPath and devRootCtrPath represent the paths of the driver install
// and NVIDIA device nodes in the management containers that require them, like
// the Toolkit Container, the Device Plugin, and MIG Manager.
type driverInfo struct {
	isHostDriver      bool
	hostRoot          string
	driverRoot        string
	driverRootCtrPath string
	devRoot           string
	devRootCtrPath    string
}

func getDriverInfo(isHostDriver bool, hostRoot string, driverInstallDir string, driverInstallDirCtrPath string) driverInfo {
	if isHostDriver {
		return driverInfo{
			isHostDriver:      true,
			hostRoot:          hostRoot,
			driverRoot:        hostRoot,
			driverRootCtrPath: "/host",
			devRoot:           hostRoot,
			devRootCtrPath:    "/host",
		}
	}

	// For drivers not installed directly on the host, devRoot can either be
	// hostRoot or driverInstallDir
	var devRoot, devRootCtrPath string
	devRoot = driver.Root(driverInstallDirCtrPath).GetDevRoot()
	if devRoot == "/" {
		devRoot = hostRoot
		devRootCtrPath = "/host"
	} else {
		devRoot = driverInstallDir
		devRootCtrPath = "/driver-root"
	}

	return driverInfo{
		isHostDriver:      false,
		hostRoot:          hostRoot,
		driverRoot:        driverInstallDir,
		driverRootCtrPath: "/driver-root",
		devRoot:           devRoot,
		devRootCtrPath:    devRootCtrPath,
	}
}

// nvidiaSMICommand returns a command that runs nvidia-smi from the driver installed
// under driverRoot with args. It fails if nvidia-smi or libnvidia-ml.so.1 cannot
// be found.
func nvidiaSMICommand(ctx context.Context, driverRoot string, args ...string) (*exec.Cmd, error) {
	root := driver.Root(driverRoot)
	library, err := root.GetDriverLibraryPath()
	if err != nil {
		return nil, fmt.Errorf("failed to locate driver libraries: %w", err)
	}
	binary, err := root.GetNvidiaSMIPath()
	if err != nil {
		return nil, fmt.Errorf("failed to locate nvidia-smi: %w", err)
	}
	cmd := exec.CommandContext(ctx, binary, args...)
	// In order for nvidia-smi to run, we need to update LD_PRELOAD to include the path to libnvidia-ml.so.1.
	cmd.Env = utils.SetEnvVar(os.Environ(), "LD_PRELOAD", utils.PrependPathListEnvvar("LD_PRELOAD", library))
	return cmd, nil
}
