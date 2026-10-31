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

	"github.com/onsi/ginkgo/v2"
	"github.com/onsi/gomega"

	corev1 "k8s.io/api/core/v1"

	"github.com/NVIDIA/gpu-operator/tests/e2e/framework"
	e2elog "github.com/NVIDIA/gpu-operator/tests/e2e/framework/logs"
	e2eoperator "github.com/NVIDIA/gpu-operator/tests/e2e/framework/operator"
)

func expectNoRepeatedRestarts(ctx context.Context, clients e2eoperator.Clients, selector string) {
	ginkgo.GinkgoHelper()

	pods, err := clients.Pods.List(ctx, e2eoperator.TestContext.OperatorNamespace, selector)
	framework.ExpectNoError(err, "failed to list pods %q in %s", selector, e2eoperator.TestContext.OperatorNamespace)
	gomega.Expect(pods).NotTo(gomega.BeEmpty(), "no pods match %q", selector)

	for _, pod := range pods {
		if len(pod.Status.ContainerStatuses) == 0 {
			continue
		}
		firstContainerStatus := pod.Status.ContainerStatuses[0]
		restartCount := firstContainerStatus.RestartCount
		if restartCount > 1 {
			previousLogs, err := clients.Pods.Logs(ctx, &pod, &corev1.PodLogOptions{Container: firstContainerStatus.Name, Previous: true})
			if err != nil {
				e2elog.Logf("WARN: failed to read previous logs of pod %s/%s: %v", pod.Namespace, pod.Name, err)
			} else {
				e2elog.Logf("previous logs of pod %s/%s:\n%s", pod.Namespace, pod.Name, previousLogs)
			}
			ginkgo.Fail(fmt.Sprintf("pod %s/%s restarted %d times", pod.Namespace, pod.Name, restartCount))
		}
	}
}

func WaitForOperandsReady(ctx context.Context, clients e2eoperator.Clients, operandSelectors []string) {
	ginkgo.GinkgoHelper()
	for _, selector := range operandSelectors {
		WaitForPodsReady(ctx, clients, selector)
	}
}

func ExpectNoRepeatedOperandRestarts(ctx context.Context, clients e2eoperator.Clients, operandSelectors []string) {
	ginkgo.GinkgoHelper()
	for _, selector := range operandSelectors {
		expectNoRepeatedRestarts(ctx, clients, selector)
	}
}
