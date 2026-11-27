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
	"maps"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"

	nvidiav1alpha1 "github.com/NVIDIA/gpu-operator/api/nvidia/v1alpha1"
	"github.com/NVIDIA/gpu-operator/internal/consts"
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
// In NVIDIADriver mode, returns the GPU nodes the operator assigned to non-precompiled CRs.
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
		nodeList, err := c.ListNodes(ctx, driverNodeSelector(driver))
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

	patch, err := json.Marshal([]map[string]any{
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

// driverNodeSelector returns the label selector of the nodes whose driver pods
// the NVIDIADriver manages: its nodeSelector restricted to the GPU nodes the
// operator assigned to it, as the operator selects them for the CR's driver
// DaemonSets.
func driverNodeSelector(driver nvidiav1alpha1.NVIDIADriver) string {
	selector := labels.Set{}
	maps.Copy(selector, driver.Spec.NodeSelector)
	selector[consts.GPUPresentLabel] = "true"
	selector[consts.NVIDIADriverOwnerLabel] = driver.Name
	return labels.SelectorFromSet(selector).String()
}
