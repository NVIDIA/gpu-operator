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

package environment

import (
	"context"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"

	"github.com/NVIDIA/gpu-operator/internal/certification/k8s"
)

type virtualizationCollector struct{}

func (c *virtualizationCollector) Name() string { return "virtualization" }

func (c *virtualizationCollector) Collect(ctx context.Context, clients *k8s.Clients, namespace string, s *EnvironmentSnapshot) error {
	info := &VirtualizationInfo{}

	// Check for KubeVirt
	if clients.ResourceExists(ctx, kubevirtGVR) {
		kvList, err := clients.ListDynamicResource(ctx, kubevirtGVR, "")
		if err == nil && len(kvList.Items) > 0 {
			info.KubeVirtPresent = true
			kv := kvList.Items[0]
			info.KubeVirtVersion, _, _ = unstructured.NestedString(kv.Object, "status", "observedKubeVirtVersion")
			info.KubeVirtTargetVersion, _, _ = unstructured.NestedString(kv.Object, "status", "targetKubeVirtVersion")
		}
	}

	// Check for HyperConverged
	if clients.ResourceExists(ctx, hyperconvergedGVR) {
		hcList, err := clients.ListDynamicResource(ctx, hyperconvergedGVR, "")
		if err == nil && len(hcList.Items) > 0 {
			info.HyperConvergedPresent = true
			hc := hcList.Items[0]
			// Try to get version from annotation or label
			if v, ok := hc.GetAnnotations()["hco.kubevirt.io/version"]; ok {
				info.HyperConvergedVersion = v
			}
		}
	}

	// Only set if we found something
	if info.KubeVirtPresent || info.HyperConvergedPresent {
		s.Virtualization = info
	}

	return nil
}
