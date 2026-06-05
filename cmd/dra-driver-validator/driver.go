/*
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
*/

package main

// driverInfo contains the location of an NVIDIA driver installation as consumed
// by the DRA kubelet-plugin containers via the driver-ready env contract.
//
// driverRoot is the absolute path of the driver install on the host (the plugin's
// NVIDIA_DRIVER_ROOT, used as the target root when generating CDI specifications).
//
// driverRootCtrPath is the path where that driver install is mounted inside the
// kubelet-plugin containers (the plugin's DRIVER_ROOT_CTR_PATH, used to read driver
// files and compute the dev root).
//
// The plugin reads only these two values; it derives its own dev root from
// driverRootCtrPath (see cmd/gpu-kubelet-plugin/device_state.go in k8s-dra-driver-gpu).
type driverInfo struct {
	driverRoot        string
	driverRootCtrPath string
}

func getDriverInfo(isHostDriver bool, hostRoot string, driverInstallDir string, driverInstallDirCtrPath string) driverInfo {
	if isHostDriver {
		// Driver installed directly on the host root; the plugin reads it via /host.
		return driverInfo{
			driverRoot:        hostRoot,
			driverRootCtrPath: "/host",
		}
	}

	// Containerized driver. Unlike nvidia-validator (which hardcodes "/driver-root"),
	// the DRA stack mounts the containerized driver at the same path it occupies on
	// the host (driverInstallDirCtrPath, e.g. /run/nvidia/driver), so we emit that.
	return driverInfo{
		driverRoot:        driverInstallDir,
		driverRootCtrPath: driverInstallDirCtrPath,
	}
}
