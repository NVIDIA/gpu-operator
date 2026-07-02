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
	"context"
	"fmt"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	"sigs.k8s.io/controller-runtime/pkg/source"

	nvidiav1alpha1 "github.com/NVIDIA/gpu-operator/api/nvidia/v1alpha1"
	"github.com/NVIDIA/gpu-operator/controllers/clusterinfo"
)

// Helpers shared by the GPUCluster operand states (DRA driver, DCGM, ...).

const (
	// draAdminNamespaceLabelKey is the label the kube-scheduler requires on a namespace
	// before it allows adminAccess: true in ResourceClaim/ResourceClaimTemplate objects.
	draAdminNamespaceLabelKey = "resource.kubernetes.io/admin-access"
)

// dcgmEnabled reports whether the standalone DCGM hostengine operand is enabled.
// The DRA stack defaults it to disabled, so it does not use the reused v1
// DCGMSpec.IsEnabled() (which treats a nil Enabled as enabled).
func dcgmEnabled(cr *nvidiav1alpha1.GPUCluster) bool {
	return cr.Spec.DCGM != nil && cr.Spec.DCGM.Enabled != nil && *cr.Spec.DCGM.Enabled
}

// ensureAdminAccessLabel patches the operator namespace with the label required by the
// kube-scheduler to allow adminAccess: true in ResourceClaim/ResourceClaimTemplate
// objects. The label is deliberately never removed: it is namespace-level configuration
// that other adminAccess consumers in the namespace may rely on.
func ensureAdminAccessLabel(ctx context.Context, k8sClient client.Client, namespace string) error {
	ns := &corev1.Namespace{}
	if err := k8sClient.Get(ctx, client.ObjectKey{Name: namespace}, ns); err != nil {
		return fmt.Errorf("could not get namespace %s: %w", namespace, err)
	}
	if ns.Labels[draAdminNamespaceLabelKey] == "true" {
		return nil
	}
	patch := client.MergeFrom(ns.DeepCopy())
	if ns.Labels == nil {
		ns.Labels = make(map[string]string)
	}
	ns.Labels[draAdminNamespaceLabelKey] = "true"
	return k8sClient.Patch(ctx, ns, patch)
}

// gpuClusterDaemonSetSource watches DaemonSets and enqueues the owning
// GPUCluster. Every operand state returns the same source; the manager
// deduplicates them by key.
func gpuClusterDaemonSetSource(mgr ctrlManager) map[string]SyncingSource {
	return map[string]SyncingSource{
		"DaemonSet": source.Kind(
			mgr.GetCache(),
			&appsv1.DaemonSet{},
			handler.TypedEnqueueRequestForOwner[*appsv1.DaemonSet](mgr.GetScheme(), mgr.GetRESTMapper(),
				&nvidiav1alpha1.GPUCluster{}, handler.OnlyControllerOwner()),
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
