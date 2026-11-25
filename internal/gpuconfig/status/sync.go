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

// Package status derives the node label values that report whether the GPUs
// hold the configuration the node should have.
package status

import "github.com/NVIDIA/gpu-operator/internal/gpuconfig/statefile"

// SyncLabel is the node label that reports whether the GPUs hold the
// configuration of the document the node uses.
const SyncLabel = "nvidia.com/gpu-config.state"

// Values of SyncLabel.
const (
	InSync    = "in-sync"
	OutOfSync = "out-of-sync"
)

// Sync derives the sync label at the container's load ID l. h is the hash of
// the document the node uses as the applier sees it now, not the snapshot of
// the decision that wrote applied; it is "" when no document applies.
func Sync(l uint64, s *statefile.State, h string) string {
	if s.AppliedAt(l) && s.Apply.Config == h {
		return InSync
	}
	return OutOfSync
}
