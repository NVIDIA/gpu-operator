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

import corev1 "k8s.io/api/core/v1"

// A Filter applies a filter on a list of Nodes
type Filter interface {
	// Apply filters a list of nodes according to some internal predicate
	Apply([]*corev1.Node) []*corev1.Node
}

// A node label filter. use NewNodeLabelFilterBuilder to create instances
type nodeLabelFilter struct {
	labels map[string]string
}

// addLabel adds a label to nodeLabelFilter
func (nlf *nodeLabelFilter) addLabel(key, val string) {
	nlf.labels[key] = val
}

// Apply Filter on Nodes
func (nlf *nodeLabelFilter) Apply(nodes []*corev1.Node) (filtered []*corev1.Node) {
NextIter:
	for _, node := range nodes {
		nodeLabels := node.GetLabels()
		for k, v := range nlf.labels {
			if nodeLabelVal, ok := nodeLabels[k]; ok && nodeLabelVal == v {
				continue
			}
			// label not found on node or label value missmatch
			continue NextIter
		}
		filtered = append(filtered, node)
	}
	return filtered
}

// newNodeLabelFilter creates a new nodeLabelFilter
func newNodeLabelFilter() nodeLabelFilter {
	return nodeLabelFilter{labels: make(map[string]string)}
}

// NodeLabelFilterBuilder is a builder for nodeLabelFilter
type NodeLabelFilterBuilder struct {
	filter nodeLabelFilter
}

// NewNodeLabelFilterBuilder returns a new NodeLabelFilterBuilder
func NewNodeLabelFilterBuilder() *NodeLabelFilterBuilder {
	return &NodeLabelFilterBuilder{filter: newNodeLabelFilter()}
}

// WithLabel adds a label for the Build process of the Label filter
func (b *NodeLabelFilterBuilder) WithLabel(key, val string) *NodeLabelFilterBuilder {
	b.filter.addLabel(key, val)
	return b
}

// Build the Filter
func (b *NodeLabelFilterBuilder) Build() Filter {
	return &b.filter
}

// Reset NodeLabelFilterBuilder
func (b *NodeLabelFilterBuilder) Reset() *NodeLabelFilterBuilder {
	b.filter = newNodeLabelFilter()
	return b
}

// A node label filter which ignores label value. use NewNodeLabelNoValFilterBuilder to create instances
type nodeLabelNoValFilter struct {
	labels map[string]struct{}
}

// addLabel adds a label to nodeLabelFilter
func (nlf *nodeLabelNoValFilter) addLabel(key string) {
	nlf.labels[key] = struct{}{}
}

// Apply Filter on Nodes
func (nlf *nodeLabelNoValFilter) Apply(nodes []*corev1.Node) (filtered []*corev1.Node) {
NextIter:
	for _, node := range nodes {
		nodeLabels := node.GetLabels()
		for k := range nlf.labels {
			if _, ok := nodeLabels[k]; ok {
				continue
			}
			// label not found on node or label value missmatch
			continue NextIter
		}
		filtered = append(filtered, node)
	}
	return filtered
}

// newNodeLabelNoValFilter creates a new nodeLabelNoValFilter
func newNodeLabelNoValFilter() nodeLabelNoValFilter {
	return nodeLabelNoValFilter{labels: make(map[string]struct{})}
}

// NodeLabelNoValFilterBuilder is a builder for nodeLabelFilter
type NodeLabelNoValFilterBuilder struct {
	filter nodeLabelNoValFilter
}

// NewNodeLabelNoValFilterBuilderr returns a new NodeLabelNoValFilterBuilder
func NewNodeLabelNoValFilterBuilderr() *NodeLabelNoValFilterBuilder {
	return &NodeLabelNoValFilterBuilder{filter: newNodeLabelNoValFilter()}
}

// WithLabel adds a label for the Build process of the Label filter
func (b *NodeLabelNoValFilterBuilder) WithLabel(key string) *NodeLabelNoValFilterBuilder {
	b.filter.addLabel(key)
	return b
}

// Build the Filter
func (b *NodeLabelNoValFilterBuilder) Build() Filter {
	return &b.filter
}

// Reset NodeLabelNoValFilterBuilder
func (b *NodeLabelNoValFilterBuilder) Reset() *NodeLabelNoValFilterBuilder {
	b.filter = newNodeLabelNoValFilter()
	return b
}
