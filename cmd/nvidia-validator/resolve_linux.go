//go:build linux

/*
 * Copyright (c) NVIDIA CORPORATION.  All rights reserved.
 *
 * Licensed under the Apache License, Version 2.0 (the "License");
 * you may not use this file except in compliance with the License.
 * You may obtain a copy of the License at
 *
 *     http://www.apache.org/licenses/LICENSE-2.0
 *
 * Unless required by applicable law or agreed to in writing, software
 * distributed under the License is distributed on an "AS IS" BASIS,
 * WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
 * See the License for the specific language governing permissions and
 * limitations under the License.
 */

package main

import (
	"fmt"

	"github.com/cyphar/filepath-securejoin/pathrs-lite"
	log "github.com/sirupsen/logrus"
)

var hostNvidiaSMISearchPaths = []string{
	"/usr/bin/nvidia-smi",
	"/usr/sbin/nvidia-smi",
	"/bin/nvidia-smi",
	"/sbin/nvidia-smi",
	wslNvidiaSMIPath,
	"/opt/bin/nvidia-smi",
}

// resolveHostNvidiaSMI searches common nvidia-smi locations within the mounted
// host root and returns the resolved path relative to the host root. A candidate
// is only accepted if it resolves to a regular, non-empty, executable file, so
// the privileged validator never execs a bogus binary from a non-standard path.
func resolveHostNvidiaSMI(hostRootCtrPath string) (string, error) {
	for _, nvidiaSMIPath := range hostNvidiaSMISearchPaths {
		f, err := pathrs.OpenInRoot(hostRootCtrPath, nvidiaSMIPath)
		if err != nil {
			log.Debugf("failed to open '%s' on the host: %v", nvidiaSMIPath, err)
			continue
		}

		fileInfo, err := f.Stat()
		_ = f.Close()
		if err != nil {
			log.Debugf("failed to stat '%s' on the host: %v", nvidiaSMIPath, err)
			continue
		}

		if !fileInfo.Mode().IsRegular() || fileInfo.Size() == 0 || fileInfo.Mode().Perm()&0o111 == 0 {
			log.Debugf("skipping '%s' on the host: not a non-empty executable regular file", nvidiaSMIPath)
			continue
		}

		return nvidiaSMIPath, nil
	}

	return "", fmt.Errorf("failed to find an executable 'nvidia-smi' on the host")
}
