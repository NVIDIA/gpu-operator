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

import (
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	corev1 "k8s.io/api/core/v1"
)

var _ = Describe("NodeAttributes tests", func() {
	var testNode corev1.Node

	JustBeforeEach(func() {
		testNode = corev1.Node{}
		testNode.Kind = "Node"
		testNode.Name = "test-node"
		testNode.Labels = make(map[string]string)
	})

	Context("Create NodeAttributes from node with all relevant labels", func() {
		It("Should return NodeAttributes with all attributes", func() {
			testNode.Labels[NodeLabelCPUArch] = "amd64"
			testNode.Labels[NodeLabelHostname] = "test-host"
			testNode.Labels[NodeLabelKernelVerFull] = "5.4.0-generic"
			testNode.Labels[NodeLabelOSName] = "ubuntu"
			testNode.Labels[NodeLabelOSVer] = "20.04"
			attr := newNodeAttributes(&testNode)

			Expect(attr.Name).To(Equal("test-node"))
			Expect(attr.Attributes[AttrTypeHostname]).To(Equal(testNode.Labels[NodeLabelHostname]))
			Expect(attr.Attributes[AttrTypeOSName]).To(Equal(testNode.Labels[NodeLabelOSName]))
			Expect(attr.Attributes[AttrTypeOSVer]).To(Equal(testNode.Labels[NodeLabelOSVer]))
			Expect(attr.Attributes[AttrTypeCPUArch]).To(Equal(testNode.Labels[NodeLabelCPUArch]))
		})
	})

	Context("Create NodeAttributes from node with some relevant labels", func() {
		It("Should return NodeAttributes with some attributes", func() {
			testNode.Labels[NodeLabelHostname] = "test-host"
			testNode.Labels[NodeLabelOSName] = "ubuntu"
			testNode.Labels[NodeLabelOSVer] = "20.04"
			attr := newNodeAttributes(&testNode)

			var exist bool
			_, exist = attr.Attributes[AttrTypeHostname]
			Expect(exist).To(BeTrue())
			_, exist = attr.Attributes[AttrTypeOSName]
			Expect(exist).To(BeTrue())
			_, exist = attr.Attributes[AttrTypeOSVer]
			Expect(exist).To(BeTrue())
			_, exist = attr.Attributes[AttrTypeCPUArch]
			Expect(exist).To(BeFalse())
		})
	})

	Context("Create NodeAttributes with no labels", func() {
		It("Should return NodeAttributes with no attributes", func() {
			attr := newNodeAttributes(&testNode)
			Expect(attr.Attributes).To(BeEmpty())
		})
	})

	Context("Node Attributes from labels", func() {
		It("Should return Node Attribute from labels", func() {
			testNode.Labels[NodeLabelOSName] = "ubuntu"
			attr := NodeAttributes{Attributes: make(map[AttributeType]string)}

			nLabels := testNode.GetLabels()
			err := attr.fromLabel(AttrTypeOSName, nLabels, NodeLabelOSName)
			Expect(err).ToNot(HaveOccurred())
			Expect(attr.Attributes[AttrTypeOSName]).To(Equal("ubuntu"))
		})
		It("Should return no Node Attribute from labels", func() {
			attr := NodeAttributes{Attributes: make(map[AttributeType]string)}

			nLabels := testNode.GetLabels()
			err := attr.fromLabel(AttrTypeOSName, nLabels, NodeLabelOSName)
			Expect(err).To(HaveOccurred())
		})
	})
})
