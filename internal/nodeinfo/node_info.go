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

package nodeinfo

/*
 nodeinfo package provides k8s node information. Apart from fetching k8s API Node objects, it wraps the lookup
 of specific attributes (mainly labels) for easier use.
*/

import (
	corev1 "k8s.io/api/core/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// MellanoxNICListOptions will match on Mellanox NIC bearing Nodes when queried via k8s client
var MellanoxNICListOptions = []client.ListOption{
	client.MatchingLabels{NodeLabelMlnxNIC: "true"}}

// Provider provides Node attributes
type Provider interface {
	// GetNodesAttributes retrieves node attributes for nodes matching the filter criteria
	GetNodesAttributes(filters ...Filter) []NodeAttributes
}

// NewProvider creates a new Provider object
func NewProvider(nodeList []*corev1.Node) Provider {
	return &provider{nodes: nodeList}
}

// provider is an implementation of the Provider interface
type provider struct {
	nodes []*corev1.Node
}

// GetNodesAttributes retrieves node attributes for nodes matching the filter criteria
func (p *provider) GetNodesAttributes(filters ...Filter) (attrs []NodeAttributes) {
	filtered := p.nodes
	for _, filter := range filters {
		filtered = filter.Apply(filtered)
	}
	for _, node := range filtered {
		attrs = append(attrs, newNodeAttributes(node))
	}
	return attrs
}
