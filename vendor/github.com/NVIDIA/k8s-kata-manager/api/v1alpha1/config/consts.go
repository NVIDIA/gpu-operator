/*
 * Copyright (c), NVIDIA CORPORATION.  All rights reserved.
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

package config

// Runtime defines container runtime type
type Runtime string

const (
	DefaultKataArtifactsDir = "/opt/nvidia-gpu-operator/artifacts/runtimeclasses"
	DefaultCrioRuntime      = "crun"
	// CRIO runtime
	CRIO Runtime = "crio"
	// Containerd runtime
	Containerd Runtime = "containerd"
)

func (r Runtime) String() string {
	switch r {
	case CRIO:
		return "crio"
	case Containerd:
		return "containerd"
	default:
		return ""
	}
}
