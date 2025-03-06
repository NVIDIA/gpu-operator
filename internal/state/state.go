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
	"context"
)

type SyncState string

// Represents the Sync state of a specific State or a collection of States
const (
	SyncStateReady    = "ready"
	SyncStateNotReady = "notReady"
	SyncStateIgnore   = "ignore"
	SyncStateReset    = "reset"
	SyncStateError    = "error"
)

type State interface {
	Name() string
	Description() string
	Sync(ctx context.Context, customResource interface{}, infoCatalog InfoCatalog) (SyncState, error)
	GetWatchSources(ctrlManager) map[string]SyncingSource
}
