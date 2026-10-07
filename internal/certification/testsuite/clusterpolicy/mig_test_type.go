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

package clusterpolicy

import (
	"context"

	"github.com/NVIDIA/gpu-operator/internal/certification"
	"github.com/NVIDIA/gpu-operator/internal/certification/k8s"
)

// MIG is a parameterized test for MIG single and mixed strategies.
type MIG struct {
	Strategy migStrategy
}

func (t *MIG) Name() string {
	return "mig-" + string(t.Strategy) + "-strategy"
}

func (t *MIG) Run(ctx context.Context, clients *k8s.Clients, cfg certification.Config) *certification.TestResult {
	result := certification.NewTestResult(t.Name())
	defer result.Finalize()

	runMIGTest(ctx, clients, result, cfg, t.Strategy)
	return result
}
