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

package operator

import (
	"cmp"
	"context"
	"strings"
	"time"

	"github.com/onsi/ginkgo/v2"
	"github.com/onsi/gomega"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/types"

	"github.com/NVIDIA/gpu-operator/tests/e2e/framework"
	"github.com/NVIDIA/gpu-operator/tests/e2e/helpers"
)

const (
	OperatorComponentSelector = "app.kubernetes.io/component=gpu-operator"
	operatorRestartTimeout    = 100 * time.Second
)

func WaitForReadyReplacementOperatorPod(ctx context.Context, clients Clients, previousOperatorPods []corev1.Pod) {
	ginkgo.GinkgoHelper()

	previousPodUIDs := make(map[types.UID]bool, len(previousOperatorPods))
	for _, pod := range previousOperatorPods {
		previousPodUIDs[pod.UID] = true
	}

	var replacementPodName string
	elapsed := framework.WaitFor(ctx, operatorRestartTimeout, "Waiting for a replacement operator pod to be ready", func(g gomega.Gomega) {
		pods, err := clients.Pods.List(ctx, TestContext.OperatorNamespace, OperatorComponentSelector)
		g.Expect(err).NotTo(gomega.HaveOccurred(), "failed to list the operator pods")
		var readyReplacementPodNames, unreadyReplacementPodSummaries []string
		for _, pod := range pods {
			if previousPodUIDs[pod.UID] {
				continue
			}
			if pod.DeletionTimestamp == nil && helpers.IsPodReady(&pod) {
				readyReplacementPodNames = append(readyReplacementPodNames, pod.Name)
			} else {
				unreadyReplacementPodSummaries = append(unreadyReplacementPodSummaries, helpers.PodStatusSummary(&pod))
			}
		}
		g.Expect(readyReplacementPodNames).NotTo(gomega.BeEmpty(), "no replacement operator pod is ready: %s", cmp.Or(strings.Join(unreadyReplacementPodSummaries, ", "), "none created yet"))
		replacementPodName = readyReplacementPodNames[0]
	})
	ginkgo.GinkgoWriter.Printf("Replacement operator pod %s is ready (%s)\n", replacementPodName, elapsed.Round(time.Second))
}
