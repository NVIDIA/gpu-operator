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

package image

import (
	"fmt"
	"os"
	"strings"
)

func ImagePath(repository string, image string, version string, imagePathEnvName string) (string, error) {
	// ImagePath is obtained using following priority
	// 1. CR (i.e through repository/image/path variables in CRD)
	var crdImagePath string
	if repository == "" && version == "" {
		if image != "" {
			// this is useful for tools like kbld(carvel) which transform templates into image as path@digest
			crdImagePath = image
		}
	} else {
		// use @ if image digest is specified instead of tag
		if strings.HasPrefix(version, "sha256:") {
			crdImagePath = repository + "/" + image + "@" + version
		} else {
			crdImagePath = repository + "/" + image + ":" + version
		}
	}
	if crdImagePath != "" {
		return crdImagePath, nil
	}

	// 2. Env passed to GPU Operator Pod (eg OLM)
	envImagePath := os.Getenv(imagePathEnvName)
	if envImagePath != "" {
		return envImagePath, nil
	}

	// 3. If both are not set, error out
	return "", fmt.Errorf("empty image path provided through both CR and ENV %s", imagePathEnvName)
}
