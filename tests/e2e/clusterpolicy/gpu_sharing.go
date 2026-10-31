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

package clusterpolicy

import (
	"context"
	"fmt"
	"strconv"
	"time"

	"github.com/onsi/ginkgo/v2"
	"github.com/onsi/gomega"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"

	nvidiav1 "github.com/NVIDIA/gpu-operator/api/nvidia/v1"
	"github.com/NVIDIA/gpu-operator/tests/e2e/framework"
	e2eoperands "github.com/NVIDIA/gpu-operator/tests/e2e/framework/operands"
	e2eoperator "github.com/NVIDIA/gpu-operator/tests/e2e/framework/operator"
)

const (
	gpuSharingWorkloadTimeout = 5 * time.Minute

	timeSlicingConfigMapName = "plugin-config"
	timeSlicingConfigKey     = "plugin-config.yaml"
	timeSlicingReplicas      = 10
	gpuSharingWorkloadName   = "nvidia-plugin-test"

	gpuProductLabel = "nvidia.com/gpu.product"
	gpuCountLabel   = "nvidia.com/gpu.count"
)

func testGPUSharing(ctx context.Context, clients e2eoperator.Clients) {
	ginkgo.GinkgoHelper()
	productName, physicalGPUCount := gpuProductAndCount(ctx, clients)
	sharedProductName := sharedGPUProductName(productName)
	productNameByNodeName := gpuProductNameByNodeName(ctx, clients)

	createTimeSlicingConfig(ctx, clients)
	err := clients.ClusterPolicies.Modify(ctx, e2eoperator.ClusterPolicyName, func(clusterPolicy *nvidiav1.ClusterPolicy) {
		clusterPolicy.Spec.DevicePlugin.Config = &nvidiav1.DevicePluginConfig{
			Name:    timeSlicingConfigMapName,
			Default: timeSlicingConfigKey,
		}
	})
	framework.ExpectNoError(err, "failed to point the device plugin at the time-slicing config")

	waitForSharedGPUNodeLabels(ctx, clients, productNameByNodeName)
	for _, app := range []string{"nvidia-device-plugin-daemonset", "gpu-feature-discovery"} {
		e2eoperands.WaitForDaemonSetRolledOut(ctx, clients, app)
		e2eoperands.WaitForPodsReady(ctx, clients, e2eoperands.AppSelector(app))
	}

	deployments := clients.Kube.AppsV1().Deployments(e2eoperator.TestContext.OperatorNamespace)
	_, err = deployments.Create(ctx, newGPUSharingDeployment(sharedProductName, physicalGPUCount+1), metav1.CreateOptions{})
	framework.ExpectNoError(err, "failed to create deployment %s", gpuSharingWorkloadName)
	ginkgo.DeferCleanup(func(ctx context.Context) {
		err := deployments.Delete(ctx, gpuSharingWorkloadName, metav1.DeleteOptions{})
		framework.ExpectNoError(client.IgnoreNotFound(err), "failed to delete deployment %s", gpuSharingWorkloadName)
	})

	framework.WaitFor(ctx, gpuSharingWorkloadTimeout, "waiting for every time-sliced workload pod to become available", func(g gomega.Gomega) {
		deployment, err := deployments.Get(ctx, gpuSharingWorkloadName, metav1.GetOptions{})
		g.Expect(err).NotTo(gomega.HaveOccurred())
		g.Expect(deployment.Status.ObservedGeneration).To(gomega.BeNumerically(">=", deployment.Generation))
		g.Expect(deployment.Status.AvailableReplicas).To(gomega.Equal(*deployment.Spec.Replicas))
	})
}

func gpuProductAndCount(ctx context.Context, clients e2eoperator.Clients) (productName string, gpuCount int32) {
	ginkgo.GinkgoHelper()
	gpuNodes, err := clients.Nodes.ListGPUNodes(ctx)
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

func createTimeSlicingConfig(ctx context.Context, clients e2eoperator.Clients) {
	ginkgo.GinkgoHelper()
	timeSlicingConfig := fmt.Sprintf(`version: v1
sharing:
  timeSlicing:
    renameByDefault: false
    resources:
    - name: nvidia.com/gpu
      replicas: %d
`, timeSlicingReplicas)
	_, err := clients.Kube.CoreV1().ConfigMaps(e2eoperator.TestContext.OperatorNamespace).Create(ctx, &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{Name: timeSlicingConfigMapName},
		Data:       map[string]string{timeSlicingConfigKey: timeSlicingConfig},
	}, metav1.CreateOptions{})
	framework.ExpectNoError(err, "failed to create configmap %s", timeSlicingConfigMapName)
}

func waitForSharedGPUNodeLabels(ctx context.Context, clients e2eoperator.Clients, productNameByNodeName map[string]string) {
	ginkgo.GinkgoHelper()
	framework.WaitFor(ctx, e2eoperands.OperandReadyTimeout, "waiting for GFD to label the GPU nodes as shared", func(g gomega.Gomega) {
		gpuNodes, err := clients.Nodes.ListGPUNodes(ctx)
		g.Expect(err).NotTo(gomega.HaveOccurred())
		g.Expect(gpuNodes).NotTo(gomega.BeEmpty())
		for _, node := range gpuNodes {
			g.Expect(productNameByNodeName).To(gomega.HaveKey(node.Name), "node %s was not a GPU node before time-slicing", node.Name)
			g.Expect(node.Labels).To(gomega.HaveKeyWithValue("nvidia.com/gpu.replicas", fmt.Sprint(timeSlicingReplicas)), "node %s", node.Name)
			g.Expect(node.Labels).To(gomega.HaveKeyWithValue(gpuProductLabel, sharedGPUProductName(productNameByNodeName[node.Name])), "node %s", node.Name)
		}
	})
}

func sharedGPUProductName(productName string) string {
	return productName + "-SHARED"
}

func gpuProductNameByNodeName(ctx context.Context, clients e2eoperator.Clients) map[string]string {
	ginkgo.GinkgoHelper()
	gpuNodes, err := clients.Nodes.ListGPUNodes(ctx)
	framework.ExpectNoError(err, "failed to list the GPU nodes")
	productNameByNodeName := make(map[string]string, len(gpuNodes))
	for _, node := range gpuNodes {
		productName := node.Labels[gpuProductLabel]
		gomega.Expect(productName).NotTo(gomega.BeEmpty(), "node %s has no %s label", node.Name, gpuProductLabel)
		productNameByNodeName[node.Name] = productName
	}
	return productNameByNodeName
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
						Image:           fmt.Sprintf("%s/%s:%s", e2eoperator.TestContext.ValidatorRepository, e2eoperator.TestContext.ValidatorImage, e2eoperator.TestContext.ValidatorVersion),
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
