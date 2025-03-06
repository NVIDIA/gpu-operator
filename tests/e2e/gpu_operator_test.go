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

package e2e

import (
	"context"
	"fmt"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	corev1 "k8s.io/api/core/v1"

	"github.com/NVIDIA/gpu-operator/tests/e2e/framework"
	e2elog "github.com/NVIDIA/gpu-operator/tests/e2e/framework/logs"
	k8stest "github.com/NVIDIA/gpu-operator/tests/e2e/kubernetes"
	"github.com/NVIDIA/gpu-operator/tests/e2e/operator"
)

var _ = Describe(e2eTestPrefix+"-premerge-suite", func() {
	f := framework.NewFramework("gpu-operator")
	f.SkipNamespaceCreation = true

	Describe("GPU Operator ClusterPolicy", func() {
		Context("When deploying gpu-operator", Ordered, func() {
			if tcfg.helmChart == "" {
				Fail("No helm-chart for gpu-operator specified")
			}

			// Init global suite vars vars
			var (
				operatorClient  *operator.Client
				helmReleaseName string
				k8sClient       *k8stest.Client
				testNamespace   *corev1.Namespace
			)

			BeforeAll(func(ctx context.Context) {
				var err error
				k8sClient = k8stest.NewClient(f.ClientSet.CoreV1())
				nsLabels := map[string]string{
					"e2e-run": string(framework.RunID),
				}

				testNamespace, err = k8sClient.CreateNamespace(ctx, tcfg.namespace, nsLabels)
				if err != nil {
					Fail(fmt.Sprintf("failed to create gpu operator namespace %s: %v", tcfg.namespace, err))
				}

				operatorClient, err = operator.NewClient(
					operator.WithNamespace(testNamespace.Name),
					operator.WithKubeConfig(framework.TestContext.KubeConfig),
					operator.WithChart(tcfg.helmChart),
				)
				if err != nil {
					Fail(fmt.Sprintf("failed to instantiate gpu operator client: %v", err))
				}

				values := []string{
					fmt.Sprintf("operator.repository=%s", tcfg.operatorRepository),
					fmt.Sprintf("operator.image=%s", tcfg.operatorImage),
					fmt.Sprintf("operator.version=%s", tcfg.operatorVersion),
					fmt.Sprintf("validator.repository=%s", tcfg.validatorRepository),
					fmt.Sprintf("validator.image=%s", tcfg.validatorImage),
					fmt.Sprintf("validator.version=%s", tcfg.validatorVersion),
				}
				helmReleaseName, err = operatorClient.Install(ctx, values, operator.ChartOptions{
					CleanupOnFail: true,
					GenerateName:  true,
					Timeout:       5 * time.Minute,
					Wait:          true,
				})
				Expect(err).NotTo(HaveOccurred())
			})

			AfterAll(func(ctx context.Context) {
				err := operatorClient.Uninstall(helmReleaseName)
				if err != nil {
					Expect(err).NotTo(HaveOccurred())
				}

				err = k8sClient.DeleteNamespace(ctx, testNamespace.Name)
				if err != nil {
					Expect(err).NotTo(HaveOccurred())
				}
			})

			It("should bring up the all of the operand pods successfully", func(ctx context.Context) {
				operands := []string{
					"nvidia-driver-daemonset",
					"nvidia-container-toolkit-daemonset",
					"nvidia-device-plugin-daemonset",
					"nvidia-dcgm-exporter",
					"gpu-feature-discovery",
					"nvidia-operator-validator",
				}
				e2elog.Logf("Ensure that the gpu operator operands come up")
				for _, operand := range operands {
					Eventually(func() bool {
						labelMap := map[string]string{
							"app": operand,
						}
						pods, err := k8sClient.GetPodsByLabel(ctx, testNamespace.Name, labelMap)
						if err != nil {
							e2elog.Logf("WARN: error retrieving pods of operand %s: %v", operand, err)
							return false
						}

						var readyCount int
						for _, pod := range pods {
							e2elog.Logf("Checking status of pod %s", pod.Name)
							isReady, err := k8sClient.IsPodReady(ctx, pod.Name, pod.Namespace)
							if err != nil {
								e2elog.Logf("WARN: error when retrieving pod status of %s/%s: %v", testNamespace.Name, operand, err)
								return false
							}
							if isReady {
								readyCount++
							}
						}
						return readyCount == len(pods)
					}).WithPolling(5 * time.Second).Within(15 * time.Minute).WithContext(ctx).Should(BeTrue())
				}
			})

			It("should ensure there are no operand pod restarts", func(ctx context.Context) {
				operands := []string{
					"nvidia-driver-daemonset",
					"nvidia-container-toolkit-daemonset",
					"nvidia-device-plugin-daemonset",
					"gpu-feature-discovery",
				}

				for _, operand := range operands {
					labelMap := map[string]string{
						"app": operand,
					}
					pods, err := k8sClient.GetPodsByLabel(ctx, testNamespace.Name, labelMap)
					Expect(err).To(Not(HaveOccurred()))

					for _, pod := range pods {
						hasRestarts, err := k8sClient.EnsureNoPodRestarts(ctx, pod.Name, pod.Namespace)
						Expect(err).NotTo(HaveOccurred())
						if !hasRestarts {
							errLogs := k8sClient.GetPodLogs(ctx, pod)
							e2elog.Logf("printing logs from the pod %s/%s: %s", pod.Namespace, pod.Name, errLogs)
							e2elog.Failf("pod %s/%s has unexpected restarts", pod.Namespace, pod.Name)
						}
					}
				}
			})
		})
	})

})
