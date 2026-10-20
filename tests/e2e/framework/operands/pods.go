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
	"fmt"
	"time"

	"github.com/onsi/ginkgo/v2"
	"github.com/onsi/gomega"

	"github.com/NVIDIA/gpu-operator/tests/e2e/framework"
	e2eoperator "github.com/NVIDIA/gpu-operator/tests/e2e/framework/operator"
	"github.com/NVIDIA/gpu-operator/tests/e2e/helpers"
)

const (
	OperandReadyTimeout = 45 * time.Minute
)

func WaitForPodsReady(ctx context.Context, clients e2eoperator.Clients, selector string) {
	ginkgo.GinkgoHelper()
	framework.WaitFor(ctx, OperandReadyTimeout, fmt.Sprintf("waiting for pods %q in %s to be ready", selector, e2eoperator.TestContext.OperatorNamespace), func(g gomega.Gomega) {
		pods, err := clients.Pods.List(ctx, e2eoperator.TestContext.OperatorNamespace, selector)
		g.Expect(err).NotTo(gomega.HaveOccurred())
		g.Expect(pods).NotTo(gomega.BeEmpty(), "no pods match %q", selector)
		for _, pod := range pods {
			g.Expect(helpers.IsPodReady(&pod)).To(gomega.BeTrue(), "pod %s is not ready", pod.Name)
			g.Expect(pod.DeletionTimestamp).To(gomega.BeNil(), "pod %s is terminating", pod.Name)
		}
	})
}
