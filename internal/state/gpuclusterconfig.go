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

package state

import (
	"fmt"

	appsv1 "k8s.io/api/apps/v1"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	"sigs.k8s.io/controller-runtime/pkg/source"

	nvidiav1alpha1 "github.com/NVIDIA/gpu-operator/api/nvidia/v1alpha1"
	"github.com/NVIDIA/gpu-operator/controllers/clusterinfo"
)

// Helpers shared by the GPUClusterConfig operand states (DRA driver, GFD, ...).

// gpuClusterConfigDaemonSetSource watches DaemonSets and enqueues the owning
// GPUClusterConfig. Every operand state returns the same source; the manager
// deduplicates them by key.
func gpuClusterConfigDaemonSetSource(mgr ctrlManager) map[string]SyncingSource {
	return map[string]SyncingSource{
		"DaemonSet": source.Kind(
			mgr.GetCache(),
			&appsv1.DaemonSet{},
			handler.TypedEnqueueRequestForOwner[*appsv1.DaemonSet](mgr.GetScheme(), mgr.GetRESTMapper(),
				&nvidiav1alpha1.GPUClusterConfig{}, handler.OnlyControllerOwner()),
		),
	}
}

// draResourceAPIVersion returns the resource.k8s.io apiVersion served by the cluster,
// erroring when Dynamic Resource Allocation is not available.
func draResourceAPIVersion(infoCatalog InfoCatalog) (string, error) {
	info := infoCatalog.Get(InfoTypeClusterInfo)
	if info == nil {
		return "", fmt.Errorf("failed to get cluster info from info catalog")
	}
	clusterInfo := info.(clusterinfo.Interface)

	gvr, draSupported, err := clusterInfo.GetDRAResourceGVR()
	if err != nil {
		return "", fmt.Errorf("failed to determine DRA support: %w", err)
	}
	if !draSupported {
		return "", fmt.Errorf("the resource.k8s.io DeviceClass API is not served by the cluster; " +
			"ensure Dynamic Resource Allocation is enabled on the API server and kubelet")
	}
	return gvr.Group + "/" + gvr.Version, nil
}
