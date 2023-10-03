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

package conditions

import (
	"context"
)

const (
	// Ready condition type indicates that all resources managed by the controller are in ready state
	Ready = "Ready"
	// Error condition type indicates one or more of the resources managed by the controller are in error state
	Error = "Error"
)

// Updater interface
type Updater interface {
	SetConditionsReady(ctx context.Context, cr any, reason, message string) error
	SetConditionsError(ctx context.Context, cr any, reason, message string) error
}
