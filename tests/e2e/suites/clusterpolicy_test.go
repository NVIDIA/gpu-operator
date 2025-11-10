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

// Package suites contains end-to-end test suites for GPU Operator ClusterPolicy management.
// These tests verify ClusterPolicy updates, component toggling, and configuration changes.
package suites

import (
	"context"
	"fmt"
	"strings"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	nvidiav1 "github.com/NVIDIA/gpu-operator/api/nvidia/v1"
	gpuclientset "github.com/NVIDIA/gpu-operator/api/versioned"
	"github.com/NVIDIA/gpu-operator/tests/e2e/framework"
	e2elog "github.com/NVIDIA/gpu-operator/tests/e2e/framework/logs"
	"github.com/NVIDIA/gpu-operator/tests/e2e/helpers"
)

const (
	defaultNamespace       = "gpu-operator"
	defaultPolicyName      = "cluster-policy"
	specUpdateTimeout      = 30 * time.Second
	componentReadyTimeout  = 3 * time.Minute
	podDeletionTimeout     = 2 * time.Minute
	daemonsetUpdateTimeout = 1 * time.Minute
)

var _ = Describe("ClusterPolicy Management", func() {
	f := framework.NewFramework("clusterpolicy-suite")
	f.SkipNamespaceCreation = true

	var (
		clusterPolicyClient        *helpers.ClusterPolicyClient
		daemonSetClient *helpers.DaemonSetClient
		testNamespace   string
		policyName      string
	)

	BeforeEach(func() {
		config := f.ClientConfig()
		gpuClient, err := gpuclientset.NewForConfig(config)
		Expect(err).NotTo(HaveOccurred())

		clusterPolicyClient = helpers.NewClusterPolicyClient(gpuClient)
		daemonSetClient = helpers.NewDaemonSetClient(f.ClientSet)
		testNamespace = defaultNamespace
		policyName = defaultPolicyName
	})

	getClusterPolicyOrSkip := func(ctx context.Context) *nvidiav1.ClusterPolicy {
		clusterPolicy, err := clusterPolicyClient.Get(ctx, policyName)
		if err != nil {
			Skip("ClusterPolicy not deployed - skipping test")
		}
		return clusterPolicy
	}

	waitForDaemonSetReady := func(ctx context.Context, name string) {
		Eventually(func() bool {
			isReady, err := daemonSetClient.IsReady(ctx, testNamespace, name)
			if err != nil {
				e2elog.Logf("WARN: error checking daemonset %s: %v", name, err)
				return false
			}
			return isReady
		}).WithPolling(5 * time.Second).Within(componentReadyTimeout).WithContext(ctx).Should(BeTrue())
	}

	waitForPodsDeleted := func(ctx context.Context, labelSelector string) {
		Eventually(func() bool {
			pods, err := f.ClientSet.CoreV1().Pods(testNamespace).List(ctx, metav1.ListOptions{
				LabelSelector: labelSelector,
			})
			if err != nil {
				return false
			}
			return len(pods.Items) == 0
		}).WithPolling(5 * time.Second).Within(podDeletionTimeout).WithContext(ctx).Should(BeTrue())
	}

	waitForSpecUpdate := func(ctx context.Context, checkFn func(*nvidiav1.ClusterPolicy) bool) {
		Eventually(func() bool {
			clusterPolicy, err := clusterPolicyClient.Get(ctx, policyName)
			if err != nil {
				return false
			}
			return checkFn(clusterPolicy)
		}).WithPolling(2 * time.Second).Within(specUpdateTimeout).WithContext(ctx).Should(BeTrue())
	}

	verifyEnvInDaemonSet := func(ctx context.Context, dsName, envName, envValue string) {
		Eventually(func() bool {
			ds, err := daemonSetClient.GetByLabel(ctx, testNamespace, "app", dsName)
			if err != nil {
				e2elog.Logf("WARN: error getting daemonset %s: %v", dsName, err)
				return false
			}
			if len(ds.Spec.Template.Spec.Containers) == 0 {
				return false
			}
			for _, env := range ds.Spec.Template.Spec.Containers[0].Env {
				if env.Name == envName && env.Value == envValue {
					return true
				}
			}
			return false
		}).WithPolling(5 * time.Second).Within(daemonsetUpdateTimeout).WithContext(ctx).Should(BeTrue())
	}

	waitForPodsReady := func(ctx context.Context, labelSelector string) {
		Eventually(func() bool {
			pods, err := f.ClientSet.CoreV1().Pods(testNamespace).List(ctx, metav1.ListOptions{
				LabelSelector: labelSelector,
			})
			if err != nil || len(pods.Items) == 0 {
				return false
			}
			for _, pod := range pods.Items {
				podReady := false
				for _, condition := range pod.Status.Conditions {
					if condition.Type == corev1.PodReady && condition.Status == corev1.ConditionTrue {
						podReady = true
						break
					}
				}
				if !podReady {
					return false
				}
			}
			return true
		}).WithPolling(5 * time.Second).Within(componentReadyTimeout).WithContext(ctx).Should(BeTrue())
	}

	// test_image_updates - Update driver image version
	Context("When updating driver image version", Label("driver", "upgrade"), func() {
		It("should update driver daemonset image and complete upgrade", func(ctx context.Context) {
			Skip("Requires specific driver version availability and upgrade flow")

			clusterPolicy := getClusterPolicyOrSkip(ctx)
			originalVersion := clusterPolicy.Spec.Driver.Version
			targetVersion := "550.90.07"
			DeferCleanup(func(ctx context.Context) {
				_ = clusterPolicyClient.UpdateDriverVersion(ctx, policyName, originalVersion)
			})

			err := clusterPolicyClient.UpdateDriverVersion(ctx, policyName, targetVersion)
			Expect(err).NotTo(HaveOccurred(), "Failed to update driver version in ClusterPolicy")

			Eventually(func() bool {
				image, err := daemonSetClient.GetImage(ctx, testNamespace, "nvidia-driver-daemonset")
				if err != nil {
					return false
				}
				return strings.Contains(image, targetVersion)
			}).WithPolling(5 * time.Second).Within(daemonsetUpdateTimeout).WithContext(ctx).Should(BeTrue())

			waitForDaemonSetReady(ctx, "nvidia-driver-daemonset")
		})
	})

	// test_env_updates - Add ENV to Device Plugin
	Context("When updating device plugin environment variables", Label("config", "envvars"), func() {
		It("should add env variable to device plugin daemonset", func(ctx context.Context) {
			clusterPolicy := getClusterPolicyOrSkip(ctx)
			originalEnv := clusterPolicy.Spec.DevicePlugin.Env
			DeferCleanup(func(ctx context.Context) {
				clusterPolicy, _ := clusterPolicyClient.Get(ctx, policyName)
				if clusterPolicy != nil {
					clusterPolicy.Spec.DevicePlugin.Env = originalEnv
					_, _ = clusterPolicyClient.Update(ctx, clusterPolicy)
				}
			})

			testEnvName := "MY_TEST_ENV_NAME"
			testEnvValue := "test"

			clusterPolicy.Spec.DevicePlugin.Env = append(clusterPolicy.Spec.DevicePlugin.Env, nvidiav1.EnvVar{
				Name:  testEnvName,
				Value: testEnvValue,
			})

			_, err := clusterPolicyClient.Update(ctx, clusterPolicy)
			Expect(err).NotTo(HaveOccurred(), "Failed to update ClusterPolicy with new environment variable")

			verifyEnvInDaemonSet(ctx, "nvidia-device-plugin-daemonset", testEnvName, testEnvValue)
			waitForDaemonSetReady(ctx, "nvidia-device-plugin-daemonset")
		})
	})

	// test_mig_strategy_updates - Test MIG strategy updates
	Context("When updating MIG strategy", Label("mig", "config"), func() {
		It("should apply MIG_STRATEGY to both GFD and device plugin daemonsets", func(ctx context.Context) {
			clusterPolicy := getClusterPolicyOrSkip(ctx)
			originalStrategy := clusterPolicy.Spec.MIG.Strategy
			newStrategy := nvidiav1.MIGStrategyMixed
			DeferCleanup(func(ctx context.Context) {
				_ = clusterPolicyClient.SetMIGStrategy(ctx, policyName, string(originalStrategy))
			})

			err := clusterPolicyClient.SetMIGStrategy(ctx, policyName, string(newStrategy))
			Expect(err).NotTo(HaveOccurred(), "Failed to update MIG strategy in ClusterPolicy")

			verifyEnvInDaemonSet(ctx, "gpu-feature-discovery", "MIG_STRATEGY", string(newStrategy))
			verifyEnvInDaemonSet(ctx, "nvidia-device-plugin-daemonset", "MIG_STRATEGY", string(newStrategy))
		})
	})

	// test_enable_dcgm - Enable standalone DCGM and verify service
	Context("When enabling standalone DCGM", Label("dcgm"), func() {
		It("should enable DCGM and verify service with local traffic policy", func(ctx context.Context) {
			getClusterPolicyOrSkip(ctx)

			err := clusterPolicyClient.EnableDCGM(ctx, policyName)
			Expect(err).NotTo(HaveOccurred(), "Failed to enable DCGM in ClusterPolicy")

			waitForPodsReady(ctx, "app=nvidia-dcgm")
			waitForDaemonSetReady(ctx, "nvidia-dcgm-exporter")

			Eventually(func() bool {
				svc, err := f.ClientSet.CoreV1().Services(testNamespace).Get(ctx, "nvidia-dcgm", metav1.GetOptions{})
				if err != nil {
					e2elog.Logf("WARN: error getting nvidia-dcgm service: %v", err)
					return false
				}

				if svc.Spec.InternalTrafficPolicy == nil {
					return false
				}

				return *svc.Spec.InternalTrafficPolicy == corev1.ServiceInternalTrafficPolicyLocal
			}).WithPolling(5 * time.Second).Within(daemonsetUpdateTimeout).WithContext(ctx).Should(BeTrue())
		})
	})

	// test_disable_enable_gfd - Disable and re-enable GFD
	Context("When toggling GPU Feature Discovery", Label("gfd", "toggle"), func() {
		It("should disable GFD and verify pods deleted", func(ctx context.Context) {
			clusterPolicy := getClusterPolicyOrSkip(ctx)
			originalState := clusterPolicy.Spec.GPUFeatureDiscovery.Enabled
			DeferCleanup(func(ctx context.Context) {
				if originalState != nil && *originalState {
					_ = clusterPolicyClient.EnableGFD(ctx, policyName)
					waitForDaemonSetReady(ctx, "gpu-feature-discovery")
				}
			})

			err := clusterPolicyClient.DisableGFD(ctx, policyName)
			Expect(err).NotTo(HaveOccurred(), "Failed to disable GFD in ClusterPolicy")

			waitForPodsDeleted(ctx, "app=gpu-feature-discovery")
		})

		It("should re-enable GFD and verify pods running", func(ctx context.Context) {
			clusterPolicy := getClusterPolicyOrSkip(ctx)
			originalState := clusterPolicy.Spec.GPUFeatureDiscovery.Enabled
			DeferCleanup(func(ctx context.Context) {
				if originalState != nil && !*originalState {
					_ = clusterPolicyClient.DisableGFD(ctx, policyName)
				}
			})

			err := clusterPolicyClient.EnableGFD(ctx, policyName)
			Expect(err).NotTo(HaveOccurred(), "Failed to enable GFD in ClusterPolicy")

			waitForDaemonSetReady(ctx, "gpu-feature-discovery")
		})
	})

	// test_disable_enable_dcgm_exporter - Disable and re-enable DCGM Exporter
	Context("When toggling DCGM Exporter", Label("dcgm", "toggle"), func() {
		It("should disable DCGM Exporter and verify pods deleted", func(ctx context.Context) {
			clusterPolicy := getClusterPolicyOrSkip(ctx)
			originalState := clusterPolicy.Spec.DCGMExporter.Enabled
			DeferCleanup(func(ctx context.Context) {
				if originalState != nil && *originalState {
					_ = clusterPolicyClient.EnableDCGMExporter(ctx, policyName)
					waitForDaemonSetReady(ctx, "nvidia-dcgm-exporter")
				}
			})

			err := clusterPolicyClient.DisableDCGMExporter(ctx, policyName)
			Expect(err).NotTo(HaveOccurred(), "Failed to disable DCGM Exporter in ClusterPolicy")

			waitForPodsDeleted(ctx, "app=nvidia-dcgm-exporter")
		})

		It("should re-enable DCGM Exporter and verify pods running", func(ctx context.Context) {
			clusterPolicy := getClusterPolicyOrSkip(ctx)
			originalState := clusterPolicy.Spec.DCGMExporter.Enabled
			DeferCleanup(func(ctx context.Context) {
				if originalState != nil && !*originalState {
					_ = clusterPolicyClient.DisableDCGMExporter(ctx, policyName)
				}
			})

			err := clusterPolicyClient.EnableDCGMExporter(ctx, policyName)
			Expect(err).NotTo(HaveOccurred(), "Failed to enable DCGM Exporter in ClusterPolicy")

			waitForDaemonSetReady(ctx, "nvidia-dcgm-exporter")
		})
	})

	// test_custom_labels_override - Test custom labels on daemonsets
	Context("When updating daemonset custom labels", Label("labels", "config"), func() {
		It("should apply custom labels to all operand pods", func(ctx context.Context) {
			clusterPolicy := getClusterPolicyOrSkip(ctx)
			originalLabels := clusterPolicy.Spec.Daemonsets.Labels
			DeferCleanup(func(ctx context.Context) {
				clusterPolicy, _ := clusterPolicyClient.Get(ctx, policyName)
				if clusterPolicy != nil {
					clusterPolicy.Spec.Daemonsets.Labels = originalLabels
					_, _ = clusterPolicyClient.Update(ctx, clusterPolicy)
				}
			})

			customLabels := map[string]string{
				"cloudprovider": "aws",
				"platform":      "kubernetes",
			}

			clusterPolicy.Spec.Daemonsets.Labels = customLabels
			_, err := clusterPolicyClient.Update(ctx, clusterPolicy)
			Expect(err).NotTo(HaveOccurred(), "Failed to update ClusterPolicy with custom labels")

			// Wait for spec update to be applied
			waitForSpecUpdate(ctx, func(clusterPolicy *nvidiav1.ClusterPolicy) bool {
				if len(clusterPolicy.Spec.Daemonsets.Labels) != len(customLabels) {
					return false
				}
				for k, v := range customLabels {
					if clusterPolicy.Spec.Daemonsets.Labels[k] != v {
						return false
					}
				}
				return true
			})

			// DaemonSet operands that should have custom labels
			daemonsetOperands := []string{
				"nvidia-driver-daemonset",
				"nvidia-container-toolkit-daemonset",
				"nvidia-device-plugin-daemonset",
				"gpu-feature-discovery",
				"nvidia-dcgm-exporter",
			}

			for _, operand := range daemonsetOperands {
				e2elog.Logf("Waiting for daemonset %s to be ready", operand)
				waitForDaemonSetReady(ctx, operand)
			}

			// Validator pods (may be Jobs/Pods, not DaemonSets)
			e2elog.Logf("Waiting for validator pods to be ready")
			waitForPodsReady(ctx, "app=nvidia-operator-validator")

			// Verify labels on all operand pods
			allOperands := append(daemonsetOperands, "nvidia-operator-validator")
			for _, operand := range allOperands {
				e2elog.Logf("Checking %s labels", operand)
				labelSelector := fmt.Sprintf("app=%s", operand)
				pods, err := f.ClientSet.CoreV1().Pods(testNamespace).List(ctx, metav1.ListOptions{
					LabelSelector: labelSelector,
				})

				if err != nil || len(pods.Items) == 0 {
					e2elog.Logf("Skipping label check for %s - no pods found", operand)
					continue
				}

				for _, pod := range pods.Items {
					for key, expectedValue := range customLabels {
						actualValue, exists := pod.Labels[key]
						Expect(exists).To(BeTrue(), fmt.Sprintf("Label %s missing on %s pod %s", key, operand, pod.Name))
						Expect(actualValue).To(Equal(expectedValue), fmt.Sprintf("Label %s has wrong value on %s pod %s", key, operand, pod.Name))
					}
				}
			}
		})
	})
})
