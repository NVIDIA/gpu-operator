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

	corev1 "k8s.io/api/core/v1"

	"github.com/NVIDIA/gpu-operator/tests/e2e/framework"
	e2eoperator "github.com/NVIDIA/gpu-operator/tests/e2e/framework/operator"
)

const daemonSetUpdateTimeout = 2 * time.Minute

func WaitForDaemonSetEnv(ctx context.Context, clients e2eoperator.Clients, app, envName, envValue string) {
	ginkgo.GinkgoHelper()
	framework.WaitFor(ctx, daemonSetUpdateTimeout, fmt.Sprintf("waiting for %s=%s on daemonset %s", envName, envValue, app), func(g gomega.Gomega) {
		daemonSet, err := clients.DaemonSets.GetByLabel(ctx, e2eoperator.TestContext.OperatorNamespace, "app", app)
		g.Expect(err).NotTo(gomega.HaveOccurred())
		var containerEnv []corev1.EnvVar
		for _, container := range daemonSet.Spec.Template.Spec.Containers {
			containerEnv = append(containerEnv, container.Env...)
		}
		g.Expect(containerEnv).To(gomega.ContainElement(corev1.EnvVar{Name: envName, Value: envValue}))
	})
}

func WaitForDaemonSetRolledOut(ctx context.Context, clients e2eoperator.Clients, app string) {
	ginkgo.GinkgoHelper()
	framework.WaitFor(ctx, OperandReadyTimeout, fmt.Sprintf("waiting for daemonset %s to roll out", app), func(g gomega.Gomega) {
		daemonSet, err := clients.DaemonSets.GetByLabel(ctx, e2eoperator.TestContext.OperatorNamespace, "app", app)
		g.Expect(err).NotTo(gomega.HaveOccurred())
		g.Expect(daemonSet.Status.ObservedGeneration).To(gomega.BeNumerically(">=", daemonSet.Generation), "daemonset %s has not observed generation %d", daemonSet.Name, daemonSet.Generation)
		g.Expect(daemonSet.Status.DesiredNumberScheduled).To(gomega.BeNumerically(">", 0), "daemonset %s has no pods scheduled", daemonSet.Name)
		g.Expect(daemonSet.Status.UpdatedNumberScheduled).To(gomega.BeNumerically(">=", daemonSet.Status.DesiredNumberScheduled), "daemonset %s has not updated all of its pods", daemonSet.Name)
		g.Expect(daemonSet.Status.NumberAvailable).To(gomega.BeNumerically(">=", daemonSet.Status.DesiredNumberScheduled), "daemonset %s does not have all of its pods available", daemonSet.Name)
	})
}

func RemoveAndRestoreOperand(ctx context.Context, clients e2eoperator.Clients, app string, disable, enable func(context.Context) error) {
	ginkgo.GinkgoHelper()
	framework.ExpectNoError(disable(ctx), "failed to disable %s", app)
	WaitForPodsDeleted(ctx, clients, AppSelector(app))

	framework.ExpectNoError(enable(ctx), "failed to enable %s", app)
	WaitForPodsReady(ctx, clients, AppSelector(app))
}

func WaitForOperandPodLabels(ctx context.Context, clients e2eoperator.Clients, operandSelectors []string, expectedLabels map[string]string) {
	ginkgo.GinkgoHelper()
	framework.WaitFor(ctx, OperandReadyTimeout, "waiting for the custom labels on every operand pod", func(g gomega.Gomega) {
		for _, selector := range operandSelectors {
			pods, err := clients.Pods.List(ctx, e2eoperator.TestContext.OperatorNamespace, selector)
			g.Expect(err).NotTo(gomega.HaveOccurred())
			g.Expect(pods).NotTo(gomega.BeEmpty(), "no pods match %q", selector)
			for _, pod := range pods {
				for labelKey, labelValue := range expectedLabels {
					g.Expect(pod.Labels).To(gomega.HaveKeyWithValue(labelKey, labelValue), "pod %s", pod.Name)
				}
			}
		}
	})
}
