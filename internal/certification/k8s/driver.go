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

package k8s

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"

	nvidiav1alpha1 "github.com/NVIDIA/gpu-operator/api/nvidia/v1alpha1"
)

var nvidiaDriverGVR = schema.GroupVersionResource{
	Group:    "nvidia.com",
	Version:  "v1alpha1",
	Resource: "nvidiadrivers",
}

// ListNVIDIADrivers returns all NVIDIADriver CRs in the cluster.
func (c *Clients) ListNVIDIADrivers(ctx context.Context) ([]nvidiav1alpha1.NVIDIADriver, error) {
	unstructuredList, err := c.dynamic.Resource(nvidiaDriverGVR).List(ctx, metav1.ListOptions{})
	if err != nil {
		return nil, err
	}

	drivers := make([]nvidiav1alpha1.NVIDIADriver, 0, len(unstructuredList.Items))
	for _, item := range unstructuredList.Items {
		var driver nvidiav1alpha1.NVIDIADriver
		if err := runtime.DefaultUnstructuredConverter.FromUnstructured(item.Object, &driver); err != nil {
			continue
		}
		drivers = append(drivers, driver)
	}
	return drivers, nil
}

// ModifyNVIDIADriver applies a JSON patch to the specified NVIDIADriver CR.
func (c *Clients) ModifyNVIDIADriver(ctx context.Context, name string, patchJSON []byte) error {
	_, err := c.dynamic.Resource(nvidiaDriverGVR).Patch(
		ctx,
		name,
		types.JSONPatchType,
		patchJSON,
		metav1.PatchOptions{},
	)
	if err != nil {
		return fmt.Errorf("patching NVIDIADriver %q: %w", name, err)
	}
	return nil
}

// HasDriverPods returns true if there are running driver pods managed by the GPU Operator.
// Returns false when the driver is disabled or the host has a pre-installed driver.
func (c *Clients) HasDriverPods(ctx context.Context, namespace string) bool {
	pods, err := c.ListPods(ctx, namespace, DriverLabelSelector)
	return err == nil && len(pods.Items) > 0
}

// EnableRDMA enables GPUDirect RDMA on the appropriate resource.
// It also detects whether MOFED is host-installed (no network operator MOFED
// DaemonSet found) and sets useHostMofed accordingly.
// In ClusterPolicy mode, patches /spec/driver/rdma/{enabled,useHostMofed}.
// In NVIDIADriver mode, patches /spec/rdma/{enabled,useHostMofed} on all NVIDIADriver CRs.
func (c *Clients) EnableRDMA(ctx context.Context) error {
	hostMofed := c.isHostMOFED(ctx)

	drivers, err := c.ListNVIDIADrivers(ctx)
	if err != nil {
		return fmt.Errorf("listing NVIDIADrivers: %w", err)
	}

	if len(drivers) > 0 {
		patch, err := json.Marshal([]map[string]interface{}{
			{"op": "replace", "path": "/spec/rdma/enabled", "value": true},
			{"op": "replace", "path": "/spec/rdma/useHostMofed", "value": hostMofed},
		})
		if err != nil {
			return fmt.Errorf("marshaling patch: %w", err)
		}
		var errs []error
		for _, driver := range drivers {
			if err := c.ModifyNVIDIADriver(ctx, driver.Name, patch); err != nil {
				errs = append(errs, err)
			}
		}
		return errors.Join(errs...)
	}

	patch, err := json.Marshal([]map[string]interface{}{
		{"op": "replace", "path": "/spec/driver/rdma/enabled", "value": true},
		{"op": "replace", "path": "/spec/driver/rdma/useHostMofed", "value": hostMofed},
	})
	if err != nil {
		return fmt.Errorf("marshaling patch: %w", err)
	}
	return c.ModifyClusterPolicy(ctx, patch)
}

// isHostMOFED detects whether MOFED is host-installed by checking for the
// absence of network operator MOFED DaemonSets across all namespaces.
// The NVIDIA Network Operator deploys DaemonSets with "mofed" in their name
// (e.g., mofed-ubuntu22.04-ds) when managing containerized MOFED.
// If no such DaemonSet is found, we assume MOFED is installed on the host.
func (c *Clients) isHostMOFED(ctx context.Context) bool {
	dsList, err := c.k8s.AppsV1().DaemonSets("").List(ctx, metav1.ListOptions{})
	if err != nil {
		return false
	}
	for _, ds := range dsList.Items {
		if strings.Contains(strings.ToLower(ds.Name), "mofed") {
			return false
		}
	}
	return true
}

// EnableGDRCopy enables GDRCopy on eligible driver resources.
// In ClusterPolicy mode, patches /spec/gdrcopy/enabled (top-level).
// In NVIDIADriver mode, patches /spec/gdrcopy/enabled on non-precompiled CRs only
// (GDRCopy is not supported with pre-compiled drivers).
func (c *Clients) EnableGDRCopy(ctx context.Context) error {
	drivers, err := c.ListNVIDIADrivers(ctx)
	if err != nil {
		return fmt.Errorf("listing NVIDIADrivers: %w", err)
	}

	patch := []byte(`[{"op": "replace", "path": "/spec/gdrcopy/enabled", "value": true}]`)
	if len(drivers) > 0 {
		var errs []error
		for _, driver := range drivers {
			if driver.Spec.UsePrecompiledDrivers() {
				continue
			}
			if err := c.ModifyNVIDIADriver(ctx, driver.Name, patch); err != nil {
				errs = append(errs, err)
			}
		}
		return errors.Join(errs...)
	}
	return c.ModifyClusterPolicy(ctx, patch)
}

// GDRCopyEligibleNodes returns the set of node names where GDRCopy can be enabled.
// In NVIDIADriver mode, returns nodes targeted by non-precompiled CRs (via their NodeSelector).
// In ClusterPolicy mode, returns all GPU nodes if the driver is not precompiled.
// Returns nil when no nodes are eligible.
func (c *Clients) GDRCopyEligibleNodes(ctx context.Context) (map[string]bool, error) {
	drivers, err := c.ListNVIDIADrivers(ctx)
	if err != nil {
		return nil, fmt.Errorf("listing NVIDIADrivers: %w", err)
	}

	if len(drivers) == 0 {
		// ClusterPolicy mode
		cp, cpErr := c.GetClusterPolicy(ctx)
		if cpErr != nil {
			return nil, cpErr
		}
		if cp.Spec.Driver.UsePrecompiledDrivers() {
			return nil, nil
		}
		gpuNodes, err := c.GetGPUNodes(ctx)
		if err != nil {
			return nil, err
		}
		nodes := make(map[string]bool, len(gpuNodes))
		for _, n := range gpuNodes {
			nodes[n] = true
		}
		return nodes, nil
	}

	// NVIDIADriver mode — collect nodes from non-precompiled CRs
	nodes := make(map[string]bool)
	for _, driver := range drivers {
		if driver.Spec.UsePrecompiledDrivers() {
			continue
		}
		// Empty NodeSelector means the CR targets all nodes.
		parts := make([]string, 0, len(driver.Spec.NodeSelector))
		for k, v := range driver.Spec.NodeSelector {
			parts = append(parts, k+"="+v)
		}
		nodeList, err := c.ListNodes(ctx, strings.Join(parts, ","))
		if err != nil {
			continue
		}
		for _, node := range nodeList.Items {
			nodes[node.Name] = true
		}
	}
	if len(nodes) == 0 {
		return nil, nil
	}
	return nodes, nil
}

// UpdateDriverImage updates the driver image in the appropriate resource.
// In NVIDIADriver mode, patches all NVIDIADriver CRs. Otherwise, patches ClusterPolicy.
func (c *Clients) UpdateDriverImage(ctx context.Context, repository, imageName, version string) error {
	drivers, err := c.ListNVIDIADrivers(ctx)
	if err != nil {
		return fmt.Errorf("listing NVIDIADrivers: %w", err)
	}

	pathPrefix := "/spec/driver"
	if len(drivers) > 0 {
		pathPrefix = "/spec"
	}

	patch, err := json.Marshal([]map[string]interface{}{
		{"op": "replace", "path": pathPrefix + "/repository", "value": repository},
		{"op": "replace", "path": pathPrefix + "/image", "value": imageName},
		{"op": "replace", "path": pathPrefix + "/version", "value": version},
	})
	if err != nil {
		return fmt.Errorf("marshaling patch: %w", err)
	}

	if len(drivers) > 0 {
		var errs []error
		for _, driver := range drivers {
			if err := c.ModifyNVIDIADriver(ctx, driver.Name, patch); err != nil {
				errs = append(errs, err)
			}
		}
		return errors.Join(errs...)
	}
	return c.ModifyClusterPolicy(ctx, patch)
}
