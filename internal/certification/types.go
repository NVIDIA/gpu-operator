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

package certification

import (
	"context"

	"github.com/NVIDIA/gpu-operator/internal/certification/k8s"
)

// Test is the fundamental unit of validation. Tests are reusable building blocks
// that can be composed into TestSets.
type Test interface {
	Name() string
	Run(ctx context.Context, clients *k8s.Clients, cfg Config) *TestResult
}

// TestSet groups related Tests together. Tests within a set are executed in order.
type TestSet struct {
	Name        string
	Description string
	Tests       []Test
	Params      map[string]any
}
