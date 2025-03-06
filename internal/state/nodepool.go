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
	"maps"

	corev1 "k8s.io/api/core/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/log"
)

const (
	nfdKernelLabelKey        = "feature.node.kubernetes.io/kernel-version.full"
	nfdOSTreeVersionLabelKey = "feature.node.kubernetes.io/system-os_release.OSTREE_VERSION"
)

// TODO: move this code to it's own module?
// TODO: add unit tests
type nodePool struct {
	name         string
	osRelease    string
	osVersion    string
	rhcosVersion string
	kernel       string
	nodeSelector map[string]string
}

// getNodePools partitions nodes into one or more node pools. The list of nodes to partition
// is defined by the labelSelector provided as input.
//
// Nodes can be partitioned in the following ways:
//  1. When precompiled drivers are enabled, we create one node pool per osVersion-kernelVersion pair.
//  2. When running on OpenShift and precompiled is disabled, we create one node pool per rhcosVersion.
//  3. Otherwise, we create one node pool per osVersion.
//
// Each nodePool object contains information needed to identify the corresonding node pool.
// Most importantly, it contains a nodeSelector used to identify the node pool.
func getNodePools(ctx context.Context, k8sClient client.Client, selector map[string]string, precompiled bool, openshift bool) ([]nodePool, error) {
	nodePoolMap := make(map[string]nodePool)

	logger := log.FromContext(ctx)

	nodeSelector := map[string]string{
		"nvidia.com/gpu.present": "true",
	}

	maps.Copy(nodeSelector, selector)

	nodeList := &corev1.NodeList{}
	err := k8sClient.List(ctx, nodeList, client.MatchingLabels(nodeSelector))
	if err != nil {
		logger.Error(err, "failed to list nodes")
		return nil, err
	}

	for _, node := range nodeList.Items {
		node := node
		nodeLabels := node.GetLabels()

		nodePool := nodePool{}
		nodePool.nodeSelector = make(map[string]string)
		maps.Copy(nodePool.nodeSelector, nodeSelector)

		osID, ok := nodeLabels[nfdOSReleaseIDLabelKey]
		if !ok {
			logger.Info("WARNING: Could not find NFD labels for node. Is NFD installed?", "Node", node.Name)
			continue
		}
		nodePool.nodeSelector[nfdOSReleaseIDLabelKey] = osID

		osVersion, ok := nodeLabels[nfdOSVersionIDLabelKey]
		if !ok {
			logger.Info("WARNING: Could not find NFD labels for node. Is NFD installed?", "Node", node.Name)
			continue
		}
		nodePool.nodeSelector[nfdOSVersionIDLabelKey] = osVersion
		nodePool.osRelease = osID
		nodePool.osVersion = osVersion
		nodePool.name = nodePool.getOS()

		if precompiled {
			kernelVersion, ok := nodeLabels[nfdKernelLabelKey]
			if !ok {
				logger.Info("WARNING: Could not find NFD labels for node. Is NFD installed?", "Node", node.Name)
				continue
			}
			nodePool.nodeSelector[nfdKernelLabelKey] = kernelVersion
			nodePool.kernel = kernelVersion
			nodePool.name = fmt.Sprintf("%s-%s", nodePool.name, getSanitizedKernelVersion(kernelVersion))
		}

		if !precompiled && openshift {
			rhcosVersion, ok := nodeLabels[nfdOSTreeVersionLabelKey]
			if !ok {
				logger.Info("WARNING: Could not find NFD labels for node. Is NFD installed?", "Node", node.Name)
				continue
			}
			nodePool.nodeSelector[nfdOSTreeVersionLabelKey] = rhcosVersion
			nodePool.rhcosVersion = rhcosVersion
			nodePool.name = rhcosVersion
		}

		if _, exists := nodePoolMap[nodePool.name]; !exists {
			logger.Info("Detected new node pool", "NodePool", nodePool)
			nodePoolMap[nodePool.name] = nodePool
		}
	}

	var nodePools []nodePool
	for _, nodePool := range nodePoolMap {
		nodePools = append(nodePools, nodePool)
	}

	return nodePools, nil
}

func (n nodePool) getOS() string {
	return fmt.Sprintf("%s%s", n.osRelease, n.osVersion)
}
