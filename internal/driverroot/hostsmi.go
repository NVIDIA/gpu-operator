//go:build linux

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

package driverroot

import (
	"fmt"
	"os"

	pathrs "github.com/cyphar/filepath-securejoin/pathrs-lite"
)

// hostNvidiaSMIPath is the expected location of nvidia-smi within the host root.
const hostNvidiaSMIPath = "/usr/bin/nvidia-smi"

// ResolveHostNvidiaSMI opens and stats nvidia-smi within the mounted host root.
func ResolveHostNvidiaSMI(hostRootCtrPath string) (os.FileInfo, error) {
	f, err := pathrs.OpenInRoot(hostRootCtrPath, hostNvidiaSMIPath)
	if err != nil {
		return nil, fmt.Errorf("failed to open 'nvidia-smi' on the host: %w", err)
	}
	defer f.Close()

	fileInfo, err := f.Stat()
	if err != nil {
		return nil, fmt.Errorf("failed to stat 'nvidia-smi' on the host: %w", err)
	}
	return fileInfo, nil
}
