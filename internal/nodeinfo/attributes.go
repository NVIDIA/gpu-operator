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
	"fmt"

	corev1 "k8s.io/api/core/v1"
	logf "sigs.k8s.io/controller-runtime/pkg/log"

	"github.com/NVIDIA/gpu-operator/internal/consts"
)

var log = logf.Log.WithName("nodeinfo")

// Node labels used by nodeinfo package
const (
	NodeLabelOSName           = "feature.node.kubernetes.io/system-os_release.ID"
	NodeLabelOSVer            = "feature.node.kubernetes.io/system-os_release.VERSION_ID"
	NodeLabelKernelVerFull    = "feature.node.kubernetes.io/kernel-version.full"
	NodeLabelHostname         = "kubernetes.io/hostname"
	NodeLabelCPUArch          = "kubernetes.io/arch"
	NodeLabelMlnxNIC          = "feature.node.kubernetes.io/pci-15b3.present"
	NodeLabelNvGPU            = "nvidia.com/gpu.present"
	NodeLabelWaitOFED         = "network.nvidia.com/operator.mofed.wait"
	NodeLabelCudaVersionMajor = "nvidia.com/cuda.driver.major"
)

type AttributeType int

// Attribute type Enum, add new types before Last and update the mapping below
const (
	// required attrs
	AttrTypeHostname = iota
	AttrTypeCPUArch
	AttrTypeOSName
	AttrTypeOSVer
	// optional attrs
	AttrTypeCudaVersionMajor

	OptionalAttrsStart = AttrTypeCudaVersionMajor
)

var attrToLabel = []string{
	// AttrTypeHostname
	NodeLabelHostname,
	// AttrTypeCPUArch
	NodeLabelCPUArch,
	// AttrTypeOSName
	NodeLabelOSName,
	// AttrTypeOSVer
	NodeLabelOSVer,
	// AttrTypeCudaVersionMajor
	NodeLabelCudaVersionMajor,
}

// NodeAttributes provides attributes of a specific node
type NodeAttributes struct {
	// Node Name
	Name string
	// Node Attributes
	Attributes map[AttributeType]string
}

// fromLabel adds a new attribute of type attrT to NodeAttributes by extracting value of selectedLabel
func (a *NodeAttributes) fromLabel(attrT AttributeType, nodeLabels map[string]string, selectedLabel string) error {
	attrVal, ok := nodeLabels[selectedLabel]
	if !ok {
		return fmt.Errorf("cannot create node attribute, missing label: %s", selectedLabel)
	}

	// Note: attrVal may be empty, this could indicate a binary attribute which relies on key existence
	a.Attributes[attrT] = attrVal
	return nil
}

// newNodeAttributes creates a new NodeAttributes
func newNodeAttributes(node *corev1.Node) NodeAttributes {
	attr := NodeAttributes{
		Name:       node.GetName(),
		Attributes: make(map[AttributeType]string),
	}
	var err error

	nLabels := node.GetLabels()
	for attrType, label := range attrToLabel {
		err = attr.fromLabel(AttributeType(attrType), nLabels, label)
		if err != nil && attrType < OptionalAttrsStart {
			log.V(consts.LogLevelWarning).Info("Cannot create NodeAttribute",
				"attribute", attrType, "error:", err.Error())
		}
	}
	return attr
}
