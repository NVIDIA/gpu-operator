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

// Represent a Result of a single State.Sync() invocation
type Result struct {
	StateName string
	Status    SyncState
	// if SyncStateError then ErrInfo will contain additional error information
	ErrInfo error
}

// Represent the Results of a collection of State.Sync() invocations, Status reflects the global status of all states.
// If all are SyncStateReady then Status is SyncStateReady, if one is SyncStateNotReady, Status is SyncStateNotReady
type Results struct {
	Status       SyncState
	StatesStatus []Result
}
