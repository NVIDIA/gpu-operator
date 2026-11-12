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
	"strings"
	"time"

	"github.com/onsi/ginkgo/v2"
	"github.com/onsi/gomega"

	corev1 "k8s.io/api/core/v1"

	"github.com/NVIDIA/gpu-operator/tests/e2e/framework"
	e2eoperator "github.com/NVIDIA/gpu-operator/tests/e2e/framework/operator"
	"github.com/NVIDIA/gpu-operator/tests/e2e/helpers"
)

const (
	OperandReadyTimeout = 45 * time.Minute
	podDeletionTimeout  = 45 * time.Minute
)

func WaitForPodsReady(ctx context.Context, clients e2eoperator.Clients, selector string) {
	ginkgo.GinkgoHelper()
	operandName := operandNameFromSelector(selector)
	readyPods, elapsed := waitForReadyPods(ctx, clients, selector, operandName, fmt.Sprintf("Checking %s pod readiness", operandName))
	for _, pod := range readyPods {
		ginkgo.GinkgoWriter.Printf("Pod %s is ready (%s)\n", pod.Name, elapsed.Round(time.Second))
	}
}

func waitForReadyPods(ctx context.Context, clients e2eoperator.Clients, selector, operandName, description string) ([]corev1.Pod, time.Duration) {
	ginkgo.GinkgoHelper()
	var readyPods []corev1.Pod
	elapsed := framework.WaitFor(ctx, OperandReadyTimeout, description, func(g gomega.Gomega) {
		pods, err := clients.Pods.List(ctx, e2eoperator.TestContext.OperatorNamespace, selector)
		g.Expect(err).NotTo(gomega.HaveOccurred(), "failed to list %s pods", operandName)
		g.Expect(pods).NotTo(gomega.BeEmpty(), "no %s pods found", operandName)
		for _, pod := range pods {
			g.Expect(helpers.IsPodReady(&pod)).To(gomega.BeTrue(), "%s", helpers.PodStatusSummary(&pod))
			g.Expect(pod.DeletionTimestamp).To(gomega.BeNil(), "%s", helpers.PodStatusSummary(&pod))
		}
		readyPods = pods
	})
	return readyPods, elapsed
}

func WaitForPodsDeleted(ctx context.Context, clients e2eoperator.Clients, selector string) {
	ginkgo.GinkgoHelper()
	operandName := operandNameFromSelector(selector)
	elapsed := framework.WaitFor(ctx, podDeletionTimeout, fmt.Sprintf("Checking if %s pods have been deleted", operandName), func(g gomega.Gomega) {
		pods, err := clients.Pods.List(ctx, e2eoperator.TestContext.OperatorNamespace, selector)
		g.Expect(err).NotTo(gomega.HaveOccurred(), "failed to list %s pods", operandName)
		g.Expect(len(pods)).To(gomega.BeZero(), "%s pods have not been deleted: %s", operandName, describePods(pods))
	})
	ginkgo.GinkgoWriter.Printf("%s pods have been deleted (%s)\n", operandName, elapsed.Round(time.Second))
}

func describePods(pods []corev1.Pod) string {
	podStatusSummaries := make([]string, 0, len(pods))
	for _, pod := range pods {
		podStatusSummaries = append(podStatusSummaries, helpers.PodStatusSummary(&pod))
	}
	return strings.Join(podStatusSummaries, ", ")
}
