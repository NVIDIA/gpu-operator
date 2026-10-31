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

package helpers

import (
	"context"
	"fmt"
	"log"
	"time"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/util/retry"
	"sigs.k8s.io/controller-runtime/pkg/client"

	nvidiav1alpha1 "github.com/NVIDIA/gpu-operator/api/nvidia/v1alpha1"
	gpuclientset "github.com/NVIDIA/gpu-operator/api/versioned"
	"github.com/NVIDIA/gpu-operator/internal/consts"
)

const DriverComponentSelector = "app.kubernetes.io/component=nvidia-driver"

type NvidiaDriverClient struct {
	client          gpuclientset.Interface
	k8sClient       kubernetes.Interface
	nodeClient      *NodeClient
	daemonSetClient *DaemonSetClient
}

func NewNvidiaDriverClient(client gpuclientset.Interface, k8sClient kubernetes.Interface) *NvidiaDriverClient {
	return &NvidiaDriverClient{
		client:          client,
		k8sClient:       k8sClient,
		nodeClient:      NewNodeClient(k8sClient),
		daemonSetClient: NewDaemonSetClient(k8sClient),
	}
}

func (h *NvidiaDriverClient) Get(ctx context.Context, name string) (*nvidiav1alpha1.NVIDIADriver, error) {
	return h.client.NvidiaV1alpha1().NVIDIADrivers().Get(ctx, name, metav1.GetOptions{})
}

func (h *NvidiaDriverClient) Create(ctx context.Context, driver *nvidiav1alpha1.NVIDIADriver) (*nvidiav1alpha1.NVIDIADriver, error) {
	return h.client.NvidiaV1alpha1().NVIDIADrivers().Create(ctx, driver, metav1.CreateOptions{})
}

func (h *NvidiaDriverClient) Update(ctx context.Context, driver *nvidiav1alpha1.NVIDIADriver) (*nvidiav1alpha1.NVIDIADriver, error) {
	return h.client.NvidiaV1alpha1().NVIDIADrivers().Update(ctx, driver, metav1.UpdateOptions{})
}

func (h *NvidiaDriverClient) Delete(ctx context.Context, name string) error {
	return h.client.NvidiaV1alpha1().NVIDIADrivers().Delete(ctx, name, metav1.DeleteOptions{})
}

func (h *NvidiaDriverClient) DeleteIfExists(ctx context.Context, name string) error {
	if err := h.Delete(ctx, name); client.IgnoreNotFound(err) != nil {
		return fmt.Errorf("failed to delete NVIDIADriver %s: %w", name, err)
	}
	return nil
}

func (h *NvidiaDriverClient) List(ctx context.Context) (*nvidiav1alpha1.NVIDIADriverList, error) {
	return h.client.NvidiaV1alpha1().NVIDIADrivers().List(ctx, metav1.ListOptions{})
}

func (h *NvidiaDriverClient) ListDefaultNames(ctx context.Context) ([]string, error) {
	driverList, err := h.List(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to list NVIDIADrivers: %w", err)
	}

	var defaultDriverNames []string
	for _, driver := range driverList.Items {
		if driver.IsDefault() {
			defaultDriverNames = append(defaultDriverNames, driver.Name)
		}
	}
	return defaultDriverNames, nil
}

func (h *NvidiaDriverClient) Copy(ctx context.Context, sourceName, targetName string, isDefault bool) error {
	sourceDriver, err := h.Get(ctx, sourceName)
	if err != nil {
		return fmt.Errorf("failed to get NVIDIADriver %s: %w", sourceName, err)
	}

	targetDriver := &nvidiav1alpha1.NVIDIADriver{
		ObjectMeta: metav1.ObjectMeta{Name: targetName},
		Spec:       *sourceDriver.Spec.DeepCopy(),
	}
	targetDriver.Spec.Default = isDefault

	if _, err := h.Create(ctx, targetDriver); err != nil {
		return fmt.Errorf("failed to create NVIDIADriver %s from %s: %w", targetName, sourceName, err)
	}
	return nil
}

func (h *NvidiaDriverClient) Modify(ctx context.Context, name string, mutate func(*nvidiav1alpha1.NVIDIADriver)) error {
	return retry.RetryOnConflict(retry.DefaultBackoff, func() error {
		nvidiaDriver, err := h.Get(ctx, name)
		if err != nil {
			return err
		}

		mutate(nvidiaDriver)

		_, err = h.Update(ctx, nvidiaDriver)
		return err
	})
}

func (h *NvidiaDriverClient) SetDefault(ctx context.Context, name string, isDefault bool) error {
	return h.Modify(ctx, name, func(nvidiaDriver *nvidiav1alpha1.NVIDIADriver) {
		nvidiaDriver.Spec.Default = isDefault
	})
}

func (h *NvidiaDriverClient) UpdateDriverVersion(ctx context.Context, name, version string) error {
	return h.Modify(ctx, name, func(nvidiaDriver *nvidiav1alpha1.NVIDIADriver) {
		nvidiaDriver.Spec.Version = version
	})
}

func (h *NvidiaDriverClient) OwnedDaemonSets(ctx context.Context, namespace, driverName string) ([]appsv1.DaemonSet, error) {
	driverDaemonSets, err := h.daemonSetClient.List(ctx, namespace, DriverComponentSelector)
	if err != nil {
		return nil, err
	}

	var ownedDaemonSets []appsv1.DaemonSet
	for _, daemonSet := range driverDaemonSets {
		if daemonSet.Spec.Template.Spec.NodeSelector[consts.NVIDIADriverOwnerLabel] == driverName {
			ownedDaemonSets = append(ownedDaemonSets, daemonSet)
		}
	}
	return ownedDaemonSets, nil
}

// WaitForReady waits for the nvidia driver pods to be ready and not terminating.
// This checks actual pod readiness similar to check_nvidia_driver_pods_ready() in the bash tests.
func (h *NvidiaDriverClient) WaitForPodsReady(ctx context.Context, namespace string, timeout time.Duration) error {
	return wait.PollUntilContextTimeout(ctx, defaultPollingInterval, timeout, true, func(ctx context.Context) (bool, error) {
		log.Println("Checking nvidia driver pods")

		labelSelector := labels.SelectorFromSet(map[string]string{
			"app.kubernetes.io/component": "nvidia-driver",
		}).String()

		podList, err := h.k8sClient.CoreV1().Pods(namespace).List(ctx, metav1.ListOptions{
			LabelSelector: labelSelector,
		})
		if err != nil {
			return false, fmt.Errorf("failed to list driver pods: %w", err)
		}

		if len(podList.Items) == 0 {
			log.Println("No nvidia driver pods found")
			return false, nil
		}

		log.Printf("Found %d nvidia driver pod(s)\n", len(podList.Items))

		// Check if all pods are ready and not terminating
		for _, pod := range podList.Items {
			// Check if pod is ready
			isReady := false
			for _, condition := range pod.Status.Conditions {
				if condition.Type == corev1.PodReady && condition.Status == corev1.ConditionTrue {
					isReady = true
					break
				}
			}

			if !isReady {
				log.Printf("Pod %s/%s is not ready yet\n", pod.Namespace, pod.Name)
				return false, nil
			}

			if pod.DeletionGracePeriodSeconds != nil {
				log.Printf("Pod %s/%s is in terminating state\n", pod.Namespace, pod.Name)
				return false, nil
			}
		}

		log.Println("All nvidia driver pods are ready")
		return true, nil
	})
}

// WaitForUpgradeDone waits for the driver upgrade to complete on all GPU nodes.
func (h *NvidiaDriverClient) WaitForPodsUpgradeDone(ctx context.Context, name string, timeout time.Duration) error {
	return wait.PollUntilContextTimeout(ctx, defaultPollingInterval, timeout, true, func(ctx context.Context) (bool, error) {
		// Get all GPU nodes
		gpuNodes, err := h.nodeClient.GetNodesByLabel(ctx, "nvidia.com/gpu.present", "true")
		if err != nil {
			return false, fmt.Errorf("failed to get GPU nodes: %w", err)
		}

		if len(gpuNodes) == 0 {
			return false, fmt.Errorf("no GPU nodes found")
		}

		// Check if all GPU nodes have the upgrade-done state
		for _, node := range gpuNodes {
			upgradeState, exists := node.Labels["nvidia.com/gpu-driver-upgrade-state"]
			if !exists || upgradeState != upgradeDoneState {
				return false, nil
			}
		}

		return true, nil
	})
}
