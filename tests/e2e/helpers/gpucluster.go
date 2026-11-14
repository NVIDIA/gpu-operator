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

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	nvidiav1alpha1 "github.com/NVIDIA/gpu-operator/api/nvidia/v1alpha1"
	gpuclientset "github.com/NVIDIA/gpu-operator/api/versioned"
)

type GPUClusterClient struct {
	client gpuclientset.Interface
}

func NewGPUClusterClient(client gpuclientset.Interface) *GPUClusterClient {
	return &GPUClusterClient{
		client: client,
	}
}

func (h *GPUClusterClient) Get(ctx context.Context, name string) (*nvidiav1alpha1.GPUCluster, error) {
	return h.client.NvidiaV1alpha1().GPUClusters().Get(ctx, name, metav1.GetOptions{})
}
