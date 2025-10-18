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
	"time"

	nvidiav1alpha1 "github.com/NVIDIA/gpu-operator/api/nvidia/v1alpha1"
	gpuclientset "github.com/NVIDIA/gpu-operator/api/versioned"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/wait"
)

type NvidiaDriverClient struct {
	client gpuclientset.Interface
}

func NewNvidiaDriverClient(client gpuclientset.Interface) *NvidiaDriverClient {
	return &NvidiaDriverClient{
		client: client,
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

func (h *NvidiaDriverClient) List(ctx context.Context) (*nvidiav1alpha1.NVIDIADriverList, error) {
	return h.client.NvidiaV1alpha1().NVIDIADrivers().List(ctx, metav1.ListOptions{})
}

func (h *NvidiaDriverClient) UpdateDriverVersion(ctx context.Context, name, version string) error {
	nvidiaDriver, err := h.Get(ctx, name)
	if err != nil {
		return fmt.Errorf("failed to get NVIDIADriver: %w", err)
	}

	nvidiaDriver.Spec.Version = version

	_, err = h.Update(ctx, nvidiaDriver)
	if err != nil {
		return fmt.Errorf("failed to update NVIDIADriver: %w", err)
	}

	return nil
}

func (h *NvidiaDriverClient) WaitForReady(ctx context.Context, name string, timeout time.Duration) error {
	return wait.PollUntilContextTimeout(ctx, defaultPollingInterval, timeout, true, func(ctx context.Context) (bool, error) {
		nvidiaDriver, err := h.Get(ctx, name)
		if err != nil {
			return false, err
		}

		if nvidiaDriver.Status.State == nvidiav1alpha1.Ready {
			return true, nil
		}

		return false, nil
	})
}

func (h *NvidiaDriverClient) WaitForUpgradeDone(ctx context.Context, name string, timeout time.Duration) error {
	return wait.PollUntilContextTimeout(ctx, defaultPollingInterval, timeout, true, func(ctx context.Context) (bool, error) {
		nvidiaDriver, err := h.Get(ctx, name)
		if err != nil {
			return false, err
		}

		if nvidiaDriver.Status.State == upgradeDoneState {
			return true, nil
		}

		return false, nil
	})
}

