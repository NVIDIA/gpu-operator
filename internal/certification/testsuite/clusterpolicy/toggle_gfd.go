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
	"strconv"

	"github.com/NVIDIA/gpu-operator/internal/certification"
	"github.com/NVIDIA/gpu-operator/internal/certification/k8s"
)

type ToggleGFD struct{}

func (t *ToggleGFD) Name() string {
	return "clusterpolicy-toggle-gfd"
}

func (t *ToggleGFD) Run(ctx context.Context, clients *k8s.Clients, cfg certification.Config) *certification.TestResult {
	result := certification.NewTestResult(t.Name())
	defer result.Finalize()

	timeout := cfg.ParseTimeout()

	// Step 1: Disable GFD
	result.AddDetail("disabling GFD via ClusterPolicy patch")
	if err := t.setGFDEnabled(ctx, clients, false); err != nil {
		result.Fail("failed to disable GFD: %v", err)
		return result
	}

	// Step 2: Wait for GFD pod to be deleted
	result.AddDetail("waiting for GFD pod to be deleted")
	if err := clients.WaitForPodDeleted(ctx, cfg.Namespace, GFDAppLabel, timeout); err != nil {
		result.Fail("GFD pod was not deleted after disabling: %v", err)
		// Attempt to re-enable before returning
		_ = t.setGFDEnabled(ctx, clients, true)
		return result
	}
	result.AddDetail("GFD pod deleted successfully")

	// Step 3: Re-enable GFD
	result.AddDetail("re-enabling GFD via ClusterPolicy patch")
	if err := t.setGFDEnabled(ctx, clients, true); err != nil {
		result.Fail("failed to re-enable GFD: %v", err)
		return result
	}

	// Step 4: Wait for GFD pod to become ready
	result.AddDetail("waiting for GFD pod to become ready")
	if err := clients.WaitForPodReady(ctx, cfg.Namespace, GFDAppLabel, timeout); err != nil {
		result.Fail("GFD pod not ready after re-enabling: %v", err)
		return result
	}
	result.AddDetail("GFD pod ready after re-enabling")

	return result
}

func (t *ToggleGFD) setGFDEnabled(ctx context.Context, clients *k8s.Clients, enabled bool) error {
	patch := []byte(`[{"op": "replace", "path": "/spec/gfd/enabled", "value": ` + strconv.FormatBool(enabled) + `}]`)
	return clients.ModifyClusterPolicy(ctx, patch)
}
