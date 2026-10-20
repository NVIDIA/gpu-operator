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
	"context"
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

	framework.WaitFor(ctx, operatorRestartTimeout, "waiting for a replacement operator pod to become ready", func(g gomega.Gomega) {
		pods, err := clients.Pods.List(ctx, TestContext.OperatorNamespace, OperatorComponentSelector)
		g.Expect(err).NotTo(gomega.HaveOccurred())
		readyReplacementCount := 0
		for _, pod := range pods {
			if !previousPodUIDs[pod.UID] && pod.DeletionTimestamp == nil && helpers.IsPodReady(&pod) {
				readyReplacementCount++
			}
		}
		g.Expect(readyReplacementCount).To(gomega.BeNumerically(">", 0), "no replacement operator pod is ready")
	})
}
