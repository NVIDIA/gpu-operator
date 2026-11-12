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

	appsv1 "k8s.io/api/apps/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
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
	daemonSetClient *DaemonSetClient
}

func NewNvidiaDriverClient(client gpuclientset.Interface, k8sClient kubernetes.Interface) *NvidiaDriverClient {
	return &NvidiaDriverClient{
		client:          client,
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
