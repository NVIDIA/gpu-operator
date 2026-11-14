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

package operands

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/onsi/ginkgo/v2"
	"github.com/onsi/gomega"

	resourcev1 "k8s.io/api/resource/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/fields"

	"github.com/NVIDIA/gpu-operator/tests/e2e/framework"
	e2eoperator "github.com/NVIDIA/gpu-operator/tests/e2e/framework/operator"
)

const (
	gpuDRADriverName = "gpu.nvidia.com"

	deviceClassCreationTimeout         = 2 * time.Minute
	gpuResourceSlicePublicationTimeout = 5 * time.Minute

	gpuDeviceType                                    = "gpu"
	deviceTypeAttributeName resourcev1.QualifiedName = "type"
)

func WaitForDeviceClass(ctx context.Context, clients e2eoperator.Clients, deviceClassName string) {
	ginkgo.GinkgoHelper()
	elapsed := framework.WaitFor(ctx, deviceClassCreationTimeout, fmt.Sprintf("Waiting for DeviceClass %s to be created", deviceClassName), func(g gomega.Gomega) {
		deviceClass, err := clients.Kube.ResourceV1().DeviceClasses().Get(ctx, deviceClassName, metav1.GetOptions{})
		g.Expect(err).NotTo(gomega.HaveOccurred(), "failed to get DeviceClass %s", deviceClassName)
		g.Expect(deviceClass.DeletionTimestamp).To(gomega.BeNil(), "DeviceClass %s is being deleted", deviceClassName)
	})
	ginkgo.GinkgoWriter.Printf("DeviceClass %s exists (%s)\n", deviceClassName, elapsed.Round(time.Second))
}

func WaitForGPUResourceSlices(ctx context.Context, clients e2eoperator.Clients) {
	ginkgo.GinkgoHelper()
	var gpuNodeCount int
	elapsed := framework.WaitFor(ctx, gpuResourceSlicePublicationTimeout, "Waiting for the DRA driver to publish a GPU for every GPU node", func(g gomega.Gomega) {
		gpuNodes, err := clients.Nodes.ListGPUNodes(ctx)
		g.Expect(err).NotTo(gomega.HaveOccurred(), "failed to list GPU nodes")
		g.Expect(gpuNodes).NotTo(gomega.BeEmpty(), "no GPU nodes found")
		devicesByNodeName, err := gpuDRADriverDevicesByNodeName(ctx, clients)
		g.Expect(err).NotTo(gomega.HaveOccurred(), "failed to list the ResourceSlices of %s", gpuDRADriverName)
		for _, node := range gpuNodes {
			deviceTypes := publishedDeviceTypes(devicesByNodeName[node.Name])
			g.Expect(deviceTypes).To(gomega.ContainElement(gpuDeviceType), "%s", describeMissingGPUDevice(node.Name, deviceTypes))
		}
		gpuNodeCount = len(gpuNodes)
	})
	ginkgo.GinkgoWriter.Printf("The DRA driver publishes a GPU for %d GPU node(s) (%s)\n", gpuNodeCount, elapsed.Round(time.Second))
}

func describeMissingGPUDevice(nodeName string, deviceTypes []string) string {
	if len(deviceTypes) == 0 {
		return fmt.Sprintf("node %s has no devices with a type attribute in a ResourceSlice yet", nodeName)
	}
	return fmt.Sprintf("node %s publishes no GPU yet, only device types %s", nodeName, strings.Join(deviceTypes, ", "))
}

func gpuDRADriverDevicesByNodeName(ctx context.Context, clients e2eoperator.Clients) (map[string][]resourcev1.Device, error) {
	resourceSlices, err := clients.Kube.ResourceV1().ResourceSlices().List(ctx, metav1.ListOptions{
		FieldSelector: fields.OneTermEqualSelector(resourcev1.ResourceSliceSelectorDriver, gpuDRADriverName).String(),
	})
	if err != nil {
		return nil, err
	}
	devicesByNodeName := map[string][]resourcev1.Device{}
	for _, resourceSlice := range resourceSlices.Items {
		if resourceSlice.Spec.NodeName == nil {
			continue
		}
		nodeName := *resourceSlice.Spec.NodeName
		devicesByNodeName[nodeName] = append(devicesByNodeName[nodeName], resourceSlice.Spec.Devices...)
	}
	return devicesByNodeName, nil
}

func publishedDeviceTypes(devices []resourcev1.Device) []string {
	deviceTypes := make([]string, 0, len(devices))
	for _, device := range devices {
		if deviceType := device.Attributes[deviceTypeAttributeName].StringValue; deviceType != nil {
			deviceTypes = append(deviceTypes, *deviceType)
		}
	}
	return deviceTypes
}
