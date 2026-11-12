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
	"k8s.io/apimachinery/pkg/labels"

	"github.com/NVIDIA/gpu-operator/tests/e2e/framework"
	e2eoperator "github.com/NVIDIA/gpu-operator/tests/e2e/framework/operator"
)

const DaemonSetUpdateTimeout = 2 * time.Minute

func WaitForDaemonSetEnv(ctx context.Context, clients e2eoperator.Clients, app, envName, envValue string) {
	ginkgo.GinkgoHelper()
	var daemonSetName string
	elapsed := framework.WaitFor(ctx, DaemonSetUpdateTimeout, fmt.Sprintf("Waiting for %s=%s on daemonset %s", envName, envValue, app), func(g gomega.Gomega) {
		daemonSet, err := clients.DaemonSets.GetByLabel(ctx, e2eoperator.TestContext.OperatorNamespace, "app", app)
		g.Expect(err).NotTo(gomega.HaveOccurred(), "failed to get daemonset %s", app)
		var envVarsWithName []corev1.EnvVar
		for _, container := range daemonSet.Spec.Template.Spec.Containers {
			for _, envVar := range container.Env {
				if envVar.Name == envName {
					envVarsWithName = append(envVarsWithName, envVar)
				}
			}
		}
		g.Expect(envVarsWithName).To(gomega.ContainElement(corev1.EnvVar{Name: envName, Value: envValue}), "daemonset %s has %s", daemonSet.Name, describeEnvVars(envName, envVarsWithName))
		daemonSetName = daemonSet.Name
	})
	ginkgo.GinkgoWriter.Printf("Daemonset %s has %s=%s (%s)\n", daemonSetName, envName, envValue, elapsed.Round(time.Second))
}

func describeEnvVars(envName string, envVarsWithName []corev1.EnvVar) string {
	if len(envVarsWithName) == 0 {
		return "no " + envName
	}
	envAssignments := make([]string, 0, len(envVarsWithName))
	for _, envVar := range envVarsWithName {
		envAssignments = append(envAssignments, envVar.Name+"="+envVar.Value)
	}
	return strings.Join(envAssignments, ", ")
}

func WaitForDaemonSetRolledOut(ctx context.Context, clients e2eoperator.Clients, app string) {
	ginkgo.GinkgoHelper()
	var daemonSetName string
	elapsed := framework.WaitFor(ctx, OperandReadyTimeout, fmt.Sprintf("Waiting for daemonset %s to roll out", app), func(g gomega.Gomega) {
		daemonSet, err := clients.DaemonSets.GetByLabel(ctx, e2eoperator.TestContext.OperatorNamespace, "app", app)
		g.Expect(err).NotTo(gomega.HaveOccurred(), "failed to get daemonset %s", app)
		g.Expect(daemonSet.Status.ObservedGeneration).To(gomega.BeNumerically(">=", daemonSet.Generation), "daemonset %s has not observed generation %d", daemonSet.Name, daemonSet.Generation)
		g.Expect(daemonSet.Status.DesiredNumberScheduled).To(gomega.BeNumerically(">", 0), "daemonset %s has no pods scheduled", daemonSet.Name)
		g.Expect(daemonSet.Status.UpdatedNumberScheduled).To(gomega.BeNumerically(">=", daemonSet.Status.DesiredNumberScheduled), "daemonset %s has updated %d/%d pods", daemonSet.Name, daemonSet.Status.UpdatedNumberScheduled, daemonSet.Status.DesiredNumberScheduled)
		g.Expect(daemonSet.Status.NumberAvailable).To(gomega.BeNumerically(">=", daemonSet.Status.DesiredNumberScheduled), "daemonset %s has %d/%d pods available", daemonSet.Name, daemonSet.Status.NumberAvailable, daemonSet.Status.DesiredNumberScheduled)
		daemonSetName = daemonSet.Name
	})
	ginkgo.GinkgoWriter.Printf("Daemonset %s rolled out (%s)\n", daemonSetName, elapsed.Round(time.Second))
}

func WaitForOperandPodLabels(ctx context.Context, clients e2eoperator.Clients, operandSelectors []string, expectedLabels map[string]string) {
	ginkgo.GinkgoHelper()
	expectedLabelsText := labels.Set(expectedLabels).String()
	elapsed := framework.WaitFor(ctx, OperandReadyTimeout, fmt.Sprintf("Waiting for every operand pod to have labels %s", expectedLabelsText), func(g gomega.Gomega) {
		for _, selector := range operandSelectors {
			operandName := operandNameFromSelector(selector)
			pods, err := clients.Pods.List(ctx, e2eoperator.TestContext.OperatorNamespace, selector)
			g.Expect(err).NotTo(gomega.HaveOccurred(), "failed to list %s pods", operandName)
			g.Expect(pods).NotTo(gomega.BeEmpty(), "no %s pods found", operandName)
			for _, pod := range pods {
				for labelKey, labelValue := range expectedLabels {
					g.Expect(pod.Labels).To(gomega.HaveKeyWithValue(labelKey, labelValue), "pod %s does not have label %s=%s", pod.Name, labelKey, labelValue)
				}
			}
		}
	})
	ginkgo.GinkgoWriter.Printf("Every operand pod has labels %s (%s)\n", expectedLabelsText, elapsed.Round(time.Second))
}

func WaitForDaemonSetsDeleted(ctx context.Context, clients e2eoperator.Clients, selector string, timeout time.Duration) {
	ginkgo.GinkgoHelper()
	operandName := operandNameFromSelector(selector)
	elapsed := framework.WaitFor(ctx, timeout, fmt.Sprintf("Checking if %s daemonsets have been deleted", operandName), func(g gomega.Gomega) {
		daemonSets, err := clients.DaemonSets.List(ctx, e2eoperator.TestContext.OperatorNamespace, selector)
		g.Expect(err).NotTo(gomega.HaveOccurred(), "failed to list %s daemonsets", operandName)
		daemonSetNames := make([]string, 0, len(daemonSets))
		for _, daemonSet := range daemonSets {
			daemonSetNames = append(daemonSetNames, daemonSet.Name)
		}
		g.Expect(daemonSetNames).To(gomega.BeEmpty(), "%s daemonsets have not been deleted: %s", operandName, strings.Join(daemonSetNames, ", "))
	})
	ginkgo.GinkgoWriter.Printf("%s daemonsets have been deleted (%s)\n", operandName, elapsed.Round(time.Second))
}
