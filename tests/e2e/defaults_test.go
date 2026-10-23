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
	"slices"
	"strconv"
	"time"

	"github.com/onsi/ginkgo/v2"
	"github.com/onsi/gomega"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"

	nvidiav1 "github.com/NVIDIA/gpu-operator/api/nvidia/v1"
	"github.com/NVIDIA/gpu-operator/internal/consts"
	"github.com/NVIDIA/gpu-operator/tests/e2e/framework"
	"github.com/NVIDIA/gpu-operator/tests/e2e/helpers"
)

const (
	daemonSetUpdateTimeout     = 2 * time.Minute
	gpuSharingWorkloadTimeout  = 5 * time.Minute
	driverMigrationStepTimeout = 5 * time.Minute

	timeSlicingConfigMapName = "plugin-config"
	timeSlicingConfigKey     = "plugin-config.yaml"
	timeSlicingReplicas      = 10
	gpuSharingWorkloadName   = "nvidia-plugin-test"

	deployOperandsLabel = "nvidia.com/gpu.deploy.operands"
	gpuProductLabel     = "nvidia.com/gpu.product"
	gpuCountLabel       = "nvidia.com/gpu.count"

	legacyDriverApp = "nvidia-driver-daemonset"
)

var _ = ginkgo.Describe(e2eTestPrefix+"-defaults", ginkgo.Label("defaults"), ginkgo.Ordered, func() {
	install := newOperatorInstall()

	ginkgo.It("brings up every operand", func(ctx context.Context) {
		waitForOperandsReady(ctx, install.clients)
	})

	ginkgo.It("does not restart operands repeatedly", func(ctx context.Context) {
		expectNoRepeatedOperandRestarts(ctx, install.clients)
	})

	ginkgo.It("runs a GPU workload", func(ctx context.Context) {
		runGPUWorkload(ctx, install.clients)
	})

	ginkgo.Context("when the ClusterPolicy changes", func() {
		ginkgo.It("rolls out a new driver version", func(ctx context.Context) {
			clients := install.clients
			err := clients.clusterPolicies.UpdateDriverVersion(ctx, clusterPolicyName, tcfg.targetDriverVersion)
			framework.ExpectNoError(err, "failed to update the ClusterPolicy driver version to %s", tcfg.targetDriverVersion)

			listLegacyDriverDaemonSets := func(ctx context.Context) ([]appsv1.DaemonSet, error) {
				return clients.daemonSets.List(ctx, tcfg.namespace, appSelector(legacyDriverApp))
			}
			waitForDaemonSetImage(ctx, daemonSetUpdateTimeout, listLegacyDriverDaemonSets, tcfg.targetDriverVersion)
			rolloutDriverPods(ctx, clients)
		})

		ginkgo.It("passes env vars to the device plugin", func(ctx context.Context) {
			clients := install.clients
			testEnv := nvidiav1.EnvVar{Name: "MY_TEST_ENV_NAME", Value: "test"}
			err := clients.clusterPolicies.Modify(ctx, clusterPolicyName, func(clusterPolicy *nvidiav1.ClusterPolicy) {
				clusterPolicy.Spec.DevicePlugin.Env = []nvidiav1.EnvVar{testEnv}
			})
			framework.ExpectNoError(err, "failed to set the device plugin env")

			waitForDaemonSetEnv(ctx, clients, "nvidia-device-plugin-daemonset", testEnv.Name, testEnv.Value)
			waitForPodsReady(ctx, clients, appSelector("nvidia-device-plugin-daemonset"))
		})

		ginkgo.It("passes the MIG strategy to GFD and the device plugin", func(ctx context.Context) {
			clients := install.clients
			migStrategy := string(nvidiav1.MIGStrategyMixed)
			err := clients.clusterPolicies.SetMIGStrategy(ctx, clusterPolicyName, migStrategy)
			framework.ExpectNoError(err, "failed to set the MIG strategy to %s", migStrategy)

			waitForDaemonSetEnv(ctx, clients, "gpu-feature-discovery", "MIG_STRATEGY", migStrategy)
			waitForDaemonSetEnv(ctx, clients, "nvidia-device-plugin-daemonset", "MIG_STRATEGY", migStrategy)
		})

		ginkgo.It("deploys standalone DCGM with a node-local service", func(ctx context.Context) {
			clients := install.clients
			framework.ExpectNoError(clients.clusterPolicies.EnableDCGM(ctx, clusterPolicyName), "failed to enable standalone DCGM")

			waitForPodsReady(ctx, clients, appSelector("nvidia-dcgm"))
			waitForPodsReady(ctx, clients, appSelector("nvidia-dcgm-exporter"))

			dcgmService, err := clients.kube.CoreV1().Services(tcfg.namespace).Get(ctx, "nvidia-dcgm", metav1.GetOptions{})
			framework.ExpectNoError(err, "failed to get the nvidia-dcgm service")
			gomega.Expect(dcgmService.Spec.InternalTrafficPolicy).To(gomega.HaveValue(gomega.Equal(corev1.ServiceInternalTrafficPolicyLocal)))
		})

		ginkgo.It("shares the GPU with time-slicing", func(ctx context.Context) {
			testGPUSharing(ctx, install.clients)
		})

		ginkgo.It("removes and restores GFD", func(ctx context.Context) {
			clients := install.clients
			removeAndRestoreOperand(ctx, clients, "gpu-feature-discovery", clients.clusterPolicies.DisableGFD, clients.clusterPolicies.EnableGFD)
		})

		ginkgo.It("removes and restores the DCGM exporter", func(ctx context.Context) {
			clients := install.clients
			removeAndRestoreOperand(ctx, clients, "nvidia-dcgm-exporter", clients.clusterPolicies.DisableDCGMExporter, clients.clusterPolicies.EnableDCGMExporter)
		})

		ginkgo.It("applies custom labels to every operand pod", func(ctx context.Context) {
			clients := install.clients
			customLabels := map[string]string{"cloudprovider": "aws", "platform": "kubernetes"}
			err := clients.clusterPolicies.Modify(ctx, clusterPolicyName, func(clusterPolicy *nvidiav1.ClusterPolicy) {
				clusterPolicy.Spec.Daemonsets.Labels = customLabels
			})
			framework.ExpectNoError(err, "failed to set custom operand labels")

			// The labels change rolls out every operand, including the driver. The labels are checked
			// first, so the upgrade state and readiness checks see the new pods rather than the old ones.
			waitForOperandPodLabels(ctx, clients, customLabels)
			waitForDriverUpgradeDone(ctx, clients)
			waitForOperandsReady(ctx, clients)
		})
	})

	ginkgo.It("recovers after the operator restarts", func(ctx context.Context) {
		restartOperator(ctx, install.clients)
	})

	ginkgo.It("removes operands when nodes opt out and restores them when they opt back in", func(ctx context.Context) {
		clients := install.clients
		err := clients.nodes.LabelAllNodes(ctx, deployOperandsLabel, "false")
		framework.ExpectNoError(err, "failed to label all nodes %s=false", deployOperandsLabel)
		for _, selector := range slices.Concat(operandSelectors, []string{appSelector("nvidia-dcgm")}) {
			waitForPodsDeleted(ctx, clients, selector)
		}

		err = clients.nodes.LabelAllNodes(ctx, deployOperandsLabel, "true")
		framework.ExpectNoError(err, "failed to label all nodes %s=true", deployOperandsLabel)
		waitForOperandsReady(ctx, clients)
	})

	ginkgo.It("migrates driver management from the ClusterPolicy to NVIDIADriver", func(ctx context.Context) {
		clients := install.clients
		legacyDriverPodName := firstLegacyDriverPodName(ctx, clients)

		install.upgrade(ctx,
			fmt.Sprintf("operator.defaultRuntime=%s", tcfg.containerRuntime),
			"driver.nvidiaDriverCRD.enabled=true",
			"driver.nvidiaDriverCRD.deployDefaultCR=true",
		)

		waitForLegacyDriverDaemonSetDeleted(ctx, clients)
		waitForPodOrphaned(ctx, clients, legacyDriverPodName)
		waitForSingleDefaultDriver(ctx, clients, consts.DefaultNVIDIADriverName, driverMigrationStepTimeout)
		waitForNodesOwnedByDriver(ctx, clients, consts.DefaultNVIDIADriverName, driverMigrationStepTimeout)
		waitForDriverDaemonSets(ctx, clients, consts.DefaultNVIDIADriverName, driverMigrationStepTimeout)
		waitForNotFound(ctx, driverMigrationStepTimeout, "waiting for the upgrade flow to delete the orphaned legacy driver pod",
			func(ctx context.Context) error {
				_, err := clients.kube.CoreV1().Pods(tcfg.namespace).Get(ctx, legacyDriverPodName, metav1.GetOptions{})
				return err
			})
		waitForPodsReady(ctx, clients, helpers.DriverComponentSelector)
		waitForDriverUpgradeDone(ctx, clients)
	})

	ginkgo.It("runs a GPU workload after the migration", func(ctx context.Context) {
		runGPUWorkload(ctx, install.clients)
	})
})

var _ = ginkgo.Describe(e2eTestPrefix+"-sandbox-workloads", ginkgo.Label("defaults"), ginkgo.Ordered, func() {
	install := newOperatorInstall(
		"sandboxWorkloads.enabled=true",
		"sandboxWorkloads.defaultWorkload=container",
	)

	ginkgo.It("brings up every operand for container workloads", func(ctx context.Context) {
		waitForOperandsReady(ctx, install.clients)
	})

	ginkgo.It("runs a GPU workload", func(ctx context.Context) {
		runGPUWorkload(ctx, install.clients)
	})
})

func waitForDaemonSetEnv(ctx context.Context, clients clusterClients, app, envName, envValue string) {
	ginkgo.GinkgoHelper()
	waitFor(ctx, daemonSetUpdateTimeout, fmt.Sprintf("waiting for %s=%s on daemonset %s", envName, envValue, app), func(g gomega.Gomega) {
		daemonSet, err := clients.daemonSets.GetByLabel(ctx, tcfg.namespace, "app", app)
		g.Expect(err).NotTo(gomega.HaveOccurred())
		var containerEnv []corev1.EnvVar
		for _, container := range daemonSet.Spec.Template.Spec.Containers {
			containerEnv = append(containerEnv, container.Env...)
		}
		g.Expect(containerEnv).To(gomega.ContainElement(corev1.EnvVar{Name: envName, Value: envValue}))
	})
}

func removeAndRestoreOperand(ctx context.Context, clients clusterClients, app string, disable, enable func(context.Context, string) error) {
	ginkgo.GinkgoHelper()
	framework.ExpectNoError(disable(ctx, clusterPolicyName), "failed to disable %s", app)
	waitForPodsDeleted(ctx, clients, appSelector(app))

	framework.ExpectNoError(enable(ctx, clusterPolicyName), "failed to enable %s", app)
	waitForPodsReady(ctx, clients, appSelector(app))
}

func waitForOperandPodLabels(ctx context.Context, clients clusterClients, expectedLabels map[string]string) {
	ginkgo.GinkgoHelper()
	waitFor(ctx, operandReadyTimeout, "waiting for the custom labels on every operand pod", func(g gomega.Gomega) {
		for _, selector := range operandSelectors {
			pods, err := clients.pods.List(ctx, tcfg.namespace, selector)
			g.Expect(err).NotTo(gomega.HaveOccurred())
			g.Expect(pods).NotTo(gomega.BeEmpty(), "no pods match %q", selector)
			for _, pod := range pods {
				for key, value := range expectedLabels {
					g.Expect(pod.Labels).To(gomega.HaveKeyWithValue(key, value), "pod %s", pod.Name)
				}
			}
		}
	})
}

func firstLegacyDriverPodName(ctx context.Context, clients clusterClients) string {
	ginkgo.GinkgoHelper()
	legacyDriverPods, err := clients.pods.List(ctx, tcfg.namespace, appSelector(legacyDriverApp))
	framework.ExpectNoError(err, "failed to list the legacy ClusterPolicy driver pods")
	gomega.Expect(legacyDriverPods).NotTo(gomega.BeEmpty(), "legacy ClusterPolicy driver pod not found")
	return legacyDriverPods[0].Name
}

func waitForLegacyDriverDaemonSetDeleted(ctx context.Context, clients clusterClients) {
	ginkgo.GinkgoHelper()
	waitFor(ctx, driverMigrationStepTimeout, "waiting for the ClusterPolicy driver daemonset to be deleted", func(g gomega.Gomega) {
		legacyDriverDaemonSets, err := clients.daemonSets.List(ctx, tcfg.namespace, appSelector(legacyDriverApp))
		g.Expect(err).NotTo(gomega.HaveOccurred())
		g.Expect(legacyDriverDaemonSets).To(gomega.BeEmpty())
	})
}

// waitForPodOrphaned also passes once the pod is gone, since the upgrade flow may delete it first.
func waitForPodOrphaned(ctx context.Context, clients clusterClients, podName string) {
	ginkgo.GinkgoHelper()
	waitFor(ctx, driverMigrationStepTimeout, fmt.Sprintf("waiting for pod %s to be orphaned", podName), func(g gomega.Gomega) {
		pod, err := clients.kube.CoreV1().Pods(tcfg.namespace).Get(ctx, podName, metav1.GetOptions{})
		if apierrors.IsNotFound(err) {
			return
		}
		g.Expect(err).NotTo(gomega.HaveOccurred())
		g.Expect(pod.OwnerReferences).To(gomega.BeEmpty())
	})
}

// testGPUSharing enables time-slicing and checks that more pods than physical GPUs can run.
func testGPUSharing(ctx context.Context, clients clusterClients) {
	ginkgo.GinkgoHelper()
	// Read before time-slicing is applied, because GFD then rewrites the label with the -SHARED suffix.
	productName, physicalGPUCount := gpuProductAndCount(ctx, clients)
	sharedProductName := productName + "-SHARED"

	createTimeSlicingConfig(ctx, clients)
	err := clients.clusterPolicies.Modify(ctx, clusterPolicyName, func(clusterPolicy *nvidiav1.ClusterPolicy) {
		clusterPolicy.Spec.DevicePlugin.Config = &nvidiav1.DevicePluginConfig{
			Name:    timeSlicingConfigMapName,
			Default: timeSlicingConfigKey,
		}
	})
	framework.ExpectNoError(err, "failed to point the device plugin at the time-slicing config")

	waitForSharedGPUNodeLabels(ctx, clients, sharedProductName)
	waitForPodsReady(ctx, clients, appSelector("nvidia-device-plugin-daemonset"))
	waitForPodsReady(ctx, clients, appSelector("gpu-feature-discovery"))

	deployments := clients.kube.AppsV1().Deployments(tcfg.namespace)
	_, err = deployments.Create(ctx, newGPUSharingDeployment(sharedProductName, physicalGPUCount+1), metav1.CreateOptions{})
	framework.ExpectNoError(err, "failed to create deployment %s", gpuSharingWorkloadName)
	ginkgo.DeferCleanup(func(ctx context.Context) {
		err := deployments.Delete(ctx, gpuSharingWorkloadName, metav1.DeleteOptions{})
		framework.ExpectNoError(client.IgnoreNotFound(err), "failed to delete deployment %s", gpuSharingWorkloadName)
	})

	waitFor(ctx, gpuSharingWorkloadTimeout, "waiting for every time-sliced workload pod to become available", func(g gomega.Gomega) {
		deployment, err := deployments.Get(ctx, gpuSharingWorkloadName, metav1.GetOptions{})
		g.Expect(err).NotTo(gomega.HaveOccurred())
		g.Expect(deployment.Status.ObservedGeneration).To(gomega.BeNumerically(">=", deployment.Generation))
		// Not the DeploymentAvailable condition: it tolerates maxUnavailable pods, which would hide a
		// pod left pending without a GPU.
		g.Expect(deployment.Status.AvailableReplicas).To(gomega.Equal(*deployment.Spec.Replicas))
	})
}

// gpuProductAndCount returns the GPU product of the first GPU node and the number of physical GPUs on the
// nodes with that product.
func gpuProductAndCount(ctx context.Context, clients clusterClients) (productName string, gpuCount int32) {
	ginkgo.GinkgoHelper()
	gpuNodes, err := clients.nodes.ListGPUNodes(ctx)
	framework.ExpectNoError(err, "failed to list the GPU nodes")
	gomega.Expect(gpuNodes).NotTo(gomega.BeEmpty(), "no GPU nodes found")
	productName = gpuNodes[0].Labels[gpuProductLabel]
	gomega.Expect(productName).NotTo(gomega.BeEmpty(), "node %s has no %s label", gpuNodes[0].Name, gpuProductLabel)

	for _, node := range gpuNodes {
		if node.Labels[gpuProductLabel] != productName {
			continue
		}
		nodeGPUCount, err := strconv.ParseInt(node.Labels[gpuCountLabel], 10, 32)
		framework.ExpectNoError(err, "failed to read the %s label of node %s", gpuCountLabel, node.Name)
		gpuCount += int32(nodeGPUCount)
	}
	return productName, gpuCount
}

func createTimeSlicingConfig(ctx context.Context, clients clusterClients) {
	ginkgo.GinkgoHelper()
	timeSlicingConfig := fmt.Sprintf(`version: v1
sharing:
  timeSlicing:
    renameByDefault: false
    resources:
    - name: nvidia.com/gpu
      replicas: %d
`, timeSlicingReplicas)
	_, err := clients.kube.CoreV1().ConfigMaps(tcfg.namespace).Create(ctx, &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{Name: timeSlicingConfigMapName},
		Data:       map[string]string{timeSlicingConfigKey: timeSlicingConfig},
	}, metav1.CreateOptions{})
	framework.ExpectNoError(err, "failed to create configmap %s", timeSlicingConfigMapName)
}

func waitForSharedGPUNodeLabels(ctx context.Context, clients clusterClients, sharedProductName string) {
	ginkgo.GinkgoHelper()
	waitFor(ctx, operandReadyTimeout, "waiting for GFD to label the GPU nodes as shared", func(g gomega.Gomega) {
		gpuNodes, err := clients.nodes.ListGPUNodes(ctx)
		g.Expect(err).NotTo(gomega.HaveOccurred())
		g.Expect(gpuNodes).NotTo(gomega.BeEmpty())
		for _, node := range gpuNodes {
			g.Expect(node.Labels).To(gomega.HaveKeyWithValue("nvidia.com/gpu.replicas", fmt.Sprint(timeSlicingReplicas)), "node %s", node.Name)
			g.Expect(node.Labels).To(gomega.HaveKeyWithValue(gpuProductLabel, sharedProductName), "node %s", node.Name)
		}
	})
}

func newGPUSharingDeployment(sharedProductName string, replicas int32) *appsv1.Deployment {
	workloadLabels := map[string]string{"app": gpuSharingWorkloadName}
	return &appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{Name: gpuSharingWorkloadName, Labels: workloadLabels},
		Spec: appsv1.DeploymentSpec{
			Replicas: new(replicas),
			Selector: &metav1.LabelSelector{MatchLabels: workloadLabels},
			Template: corev1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{Labels: workloadLabels},
				Spec: corev1.PodSpec{
					Tolerations: []corev1.Toleration{{
						Key:      "nvidia.com/gpu",
						Operator: corev1.TolerationOpExists,
						Effect:   corev1.TaintEffectNoSchedule,
					}},
					ServiceAccountName: "nvidia-device-plugin",
					Containers: []corev1.Container{{
						Name:            "nvidia-plugin-test-ctr",
						Image:           fmt.Sprintf("%s/%s:%s", tcfg.validatorRepository, tcfg.validatorImage, tcfg.validatorVersion),
						ImagePullPolicy: corev1.PullIfNotPresent,
						Command:         []string{"sh", "-c"},
						Args:            []string{"while true; do vectorAdd; sleep 30; done"},
						SecurityContext: &corev1.SecurityContext{AllowPrivilegeEscalation: new(false)},
						Resources: corev1.ResourceRequirements{
							Limits: corev1.ResourceList{"nvidia.com/gpu": resource.MustParse("1")},
						},
					}},
					NodeSelector: map[string]string{gpuProductLabel: sharedProductName},
				},
			},
		},
	}
}
