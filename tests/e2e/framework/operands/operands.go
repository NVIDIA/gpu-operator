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

package operands

import (
	"context"

	"github.com/onsi/ginkgo/v2"

	e2eoperator "github.com/NVIDIA/gpu-operator/tests/e2e/framework/operator"
)

func WaitForOperandsReady(ctx context.Context, clients e2eoperator.Clients, operandSelectors []string) {
	ginkgo.GinkgoHelper()
	for _, selector := range operandSelectors {
		WaitForPodsReady(ctx, clients, selector)
	}
}
