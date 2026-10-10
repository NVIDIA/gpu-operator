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

type openshiftCollector struct{}

func (c *openshiftCollector) Name() string { return "openshift" }

func (c *openshiftCollector) Collect(ctx context.Context, clients *k8s.Clients, namespace string, s *EnvironmentSnapshot) error {
	// Skip if not OpenShift (detected by cluster collector)
	if s.Cluster.Platform != "openshift" {
		return nil
	}

	info := &OpenShiftInfo{}

	// Get cluster version details
	cv, err := clients.GetDynamicResource(ctx, clusterVersionGVR, "", "version")
	if err == nil && cv != nil {
		info.Version, _, _ = unstructured.NestedString(cv.Object, "status", "desired", "version")
		info.Channel, _, _ = unstructured.NestedString(cv.Object, "spec", "channel")
		info.ClusterID, _, _ = unstructured.NestedString(cv.Object, "spec", "clusterID")
	}

	// Get proxy configuration
	proxy, err := clients.GetDynamicResource(ctx, proxyGVR, "", "cluster")
	if err == nil && proxy != nil {
		info.ProxyHTTP, _, _ = unstructured.NestedString(proxy.Object, "status", "httpProxy")
		info.ProxyHTTPS, _, _ = unstructured.NestedString(proxy.Object, "status", "httpsProxy")
		info.ProxyNoProxy, _, _ = unstructured.NestedString(proxy.Object, "status", "noProxy")
	}

	// Get image config
	imageConfig, err := clients.GetDynamicResource(ctx, imageConfigGVR, "", "cluster")
	if err == nil && imageConfig != nil {
		info.InternalRegistryHostname, _, _ = unstructured.NestedString(imageConfig.Object, "status", "internalRegistryHostname")
	}

	// Try to get DriverToolkit image from driver DaemonSet
	daemonsets, err := clients.ListDaemonSets(ctx, namespace, "")
	if err == nil {
		for _, ds := range daemonsets.Items {
			if ds.Labels["app"] == OperandDriver {
				// Look for driver-toolkit init container
				for _, init := range ds.Spec.Template.Spec.InitContainers {
					if init.Name == "openshift-driver-toolkit-ctr" {
						info.DriverToolkitImage = init.Image
						break
					}
				}
				break
			}
		}
	}

	// Get machines list
	if clients.ResourceExists(ctx, machineGVR) {
		machines, err := clients.ListDynamicResource(ctx, machineGVR, "")
		if err == nil {
			for _, m := range machines.Items {
				info.Machines = append(info.Machines, m.GetName())
			}
		}
	}

	s.OpenShift = info
	return nil
}
