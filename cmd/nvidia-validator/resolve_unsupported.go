//go:build !linux

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

import "fmt"

// resolveHostNvidiaSMI is only implemented on linux, the only OS the
// nvidia-validator binary is ever built and run for in production. This stub
// exists so the package still builds and unit tests still run on other
// platforms during local development.
func resolveHostNvidiaSMI(hostRootCtrPath string) (string, error) {
	return "", fmt.Errorf("resolving nvidia-smi on the host is only supported on linux")
}
