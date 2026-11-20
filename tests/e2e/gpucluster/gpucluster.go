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

package gpucluster

import (
	"context"

	"github.com/onsi/ginkgo/v2"
	"github.com/onsi/gomega"

	"github.com/NVIDIA/gpu-operator/tests/e2e/framework"
	e2eoperands "github.com/NVIDIA/gpu-operator/tests/e2e/framework/operands"
	e2eoperator "github.com/NVIDIA/gpu-operator/tests/e2e/framework/operator"
	e2eworkload "github.com/NVIDIA/gpu-operator/tests/e2e/framework/workload"
	"github.com/NVIDIA/gpu-operator/tests/e2e/helpers"
)

const (
	migDeviceClassName     = "mig.nvidia.com"
	vfioGPUDeviceClassName = "vfio.gpu.nvidia.com"

	draValidatorComponentSelector = "app.kubernetes.io/component=nvidia-dra-validator"
)

var draDeviceClassNames = []string{
	e2eoperator.GPUDeviceClassName,
	migDeviceClassName,
	vfioGPUDeviceClassName,
}

var operandSelectors = []string{
	helpers.DriverComponentSelector,
	e2eoperands.AppSelector("nvidia-dra-driver-kubelet-plugin"),
	e2eoperands.AppSelector("nvidia-dra-driver-controller"),
	e2eoperands.AppSelector("nvidia-dcgm-exporter-dra"),
	draValidatorComponentSelector,
}

var _ = e2eoperator.Describe("gpucluster", "gpucluster", ginkgo.Label("single-gpu", "disruptive"), ginkgo.Ordered, ginkgo.Serial, func() {
	install := e2eoperator.NewInstall(
		"clusterPolicy.deployCR=false",
		"gpuCluster.deployCR=true",
		"driver.nvidiaDriverCRD.enabled=true",
		"driver.nvidiaDriverCRD.deployDefaultCR=true",
	)

	ginkgo.It("brings up the GPUCluster and the operands", func(ctx context.Context) {
		e2eoperands.WaitForOperandsReady(ctx, install.Clients, operandSelectors)
		e2eoperator.WaitForGPUClusterReady(ctx, install.Clients, e2eoperator.GPUClusterName)
	})

	ginkgo.It("does not restart operands repeatedly", func(ctx context.Context) {
		e2eoperands.ExpectNoRepeatedOperandRestarts(ctx, install.Clients, operandSelectors)
	})

	ginkgo.It("does not deploy the device plugin or GFD", func(ctx context.Context) {
		for _, app := range []string{"nvidia-device-plugin-daemonset", "gpu-feature-discovery"} {
			daemonSets, err := install.DaemonSets.List(ctx, e2eoperator.TestContext.OperatorNamespace, e2eoperands.AppSelector(app))
			framework.ExpectNoError(err, "failed to list %s daemonsets", app)
			gomega.Expect(daemonSets).To(gomega.BeEmpty(),
				"%s daemonsets are deployed, but a GPUCluster install should not deploy them", app)

			pods, err := install.Pods.List(ctx, e2eoperator.TestContext.OperatorNamespace, e2eoperands.AppSelector(app))
			framework.ExpectNoError(err, "failed to list %s pods", app)
			gomega.Expect(pods).To(gomega.BeEmpty(),
				"%s pods exist, but a GPUCluster install should not run them", app)
		}
	})

	ginkgo.It("creates the DRA DeviceClasses", func(ctx context.Context) {
		for _, deviceClassName := range draDeviceClassNames {
			e2eoperands.WaitForDeviceClass(ctx, install.Clients, deviceClassName)
		}
	})

	ginkgo.It("publishes the GPU in a ResourceSlice", func(ctx context.Context) {
		e2eoperands.WaitForGPUResourceSlices(ctx, install.Clients)
	})

	ginkgo.It("runs vectorAdd with a ResourceClaim", func(ctx context.Context) {
		e2eworkload.RunVectorAddWithResourceClaim(ctx, install.Clients)
	})
})
