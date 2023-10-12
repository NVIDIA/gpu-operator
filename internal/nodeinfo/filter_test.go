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
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

var _ = Describe("NodeAttributes tests", func() {

	nodes := []*corev1.Node{
		{
			TypeMeta: metav1.TypeMeta{Kind: "Node"},
			ObjectMeta: metav1.ObjectMeta{
				Name: "node-1",
				Labels: map[string]string{
					NodeLabelOSName:           "ubuntu",
					NodeLabelCPUArch:          "amd64",
					NodeLabelKernelVerFull:    "5.4.0-generic",
					NodeLabelOSVer:            "20.04",
					NodeLabelCudaVersionMajor: "465"},
			},
		},
		{
			TypeMeta: metav1.TypeMeta{Kind: "Node"},
			ObjectMeta: metav1.ObjectMeta{
				Name: "node-2",
				Labels: map[string]string{
					NodeLabelOSName:           "rhel",
					NodeLabelCPUArch:          "x86_64",
					NodeLabelKernelVerFull:    "5.4.0-generic",
					NodeLabelCudaVersionMajor: "460"},
			},
		},
		{
			TypeMeta: metav1.TypeMeta{Kind: "Node"},
			ObjectMeta: metav1.ObjectMeta{
				Name: "node-3",
				Labels: map[string]string{
					NodeLabelOSName:        "ubuntu",
					NodeLabelCPUArch:       "amd64",
					NodeLabelKernelVerFull: "4.5.0-generic"},
			},
		},
		{
			TypeMeta: metav1.TypeMeta{Kind: "Node"},
			ObjectMeta: metav1.ObjectMeta{
				Name: "node-4",
				Labels: map[string]string{
					NodeLabelOSName:        "coreos",
					NodeLabelCPUArch:       "x86_64",
					NodeLabelKernelVerFull: "5.4.0-generic"},
			},
		}}

	Context("Filter on empty list of nodes", func() {
		It("Should return an empty list of nodes", func() {
			filter := NewNodeLabelFilterBuilder().
				WithLabel(NodeLabelHostname, "test-host").
				Build()
			filteredNodes := filter.Apply([]*corev1.Node{})

			Expect(filteredNodes).To(BeEmpty())
		})
	})

	Context("Filter with criteria that doesnt match any node", func() {
		It("Should return an empty list of nodes", func() {
			filter := NewNodeLabelFilterBuilder().
				WithLabel(NodeLabelCPUArch, "arm64").
				Build()
			filteredNodes := filter.Apply(nodes)

			Expect(filteredNodes).To(BeEmpty())
		})
	})

	Context("Filter with criteria that is missing from nodes", func() {
		It("Should return an empty list of nodes", func() {
			filter := NewNodeLabelFilterBuilder().
				WithLabel(NodeLabelHostname, "test-host").
				Build()
			filteredNodes := filter.Apply(nodes)

			Expect(filteredNodes).To(BeEmpty())
		})
	})

	Context("Filter with criteria that match on some nodes", func() {
		It("Should only return the relevant nodes", func() {
			filter := NewNodeLabelFilterBuilder().
				WithLabel(NodeLabelKernelVerFull, "5.4.0-generic").
				WithLabel(NodeLabelCPUArch, "x86_64").
				Build()
			filteredNodes := filter.Apply(nodes)
			Expect(len(filteredNodes)).To(Equal(2))
			Expect(filteredNodes[0].Name).To(Equal("node-2"))
			Expect(filteredNodes[1].Name).To(Equal("node-4"))
		})
	})

	Context("Filter by labels without values", func() {
		It("Should only return the relevant nodes", func() {
			filter := NewNodeLabelNoValFilterBuilderr().
				WithLabel(NodeLabelCudaVersionMajor).
				Build()
			filteredNodes := filter.Apply(nodes)
			Expect(len(filteredNodes)).To(Equal(2))
			Expect(filteredNodes[0].Name).To(Equal("node-1"))
			Expect(filteredNodes[1].Name).To(Equal("node-2"))
		})
		It("Should return an empty list of nodes", func() {
			filter := NewNodeLabelNoValFilterBuilderr().
				WithLabel("unknown_label").
				Build()
			filteredNodes := filter.Apply(nodes)
			Expect(filteredNodes).To(BeEmpty())
		})
	})
})
