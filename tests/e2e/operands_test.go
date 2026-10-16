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

package e2e

import (
	"context"
	"fmt"

	"github.com/onsi/ginkgo/v2"
	"github.com/onsi/gomega"

	"github.com/NVIDIA/gpu-operator/tests/e2e/helpers"
)

// operandSelectors match the pods of the operands every install is expected to run. The driver
// pods of both ClusterPolicy and NVIDIADriver carry helpers.DriverComponentSelector.
var operandSelectors = []string{
	helpers.DriverComponentSelector,
	appSelector("nvidia-container-toolkit-daemonset"),
	appSelector("nvidia-device-plugin-daemonset"),
	appSelector("nvidia-dcgm-exporter"),
	appSelector("gpu-feature-discovery"),
	appSelector("nvidia-operator-validator"),
}

func appSelector(app string) string {
	return "app=" + app
}

// waitForPodsReady waits until at least one pod matches selector and every match is Ready and not terminating.
func waitForPodsReady(ctx context.Context, clients clusterClients, selector string) {
	ginkgo.GinkgoHelper()
	waitFor(ctx, operandReadyTimeout, fmt.Sprintf("waiting for pods %q in %s to be ready", selector, tcfg.namespace), func(g gomega.Gomega) {
		pods, err := clients.pods.List(ctx, tcfg.namespace, selector)
		g.Expect(err).NotTo(gomega.HaveOccurred())
		g.Expect(pods).NotTo(gomega.BeEmpty(), "no pods match %q", selector)
		for _, pod := range pods {
			g.Expect(helpers.IsPodReady(&pod)).To(gomega.BeTrue(), "pod %s is not ready", pod.Name)
			g.Expect(pod.DeletionTimestamp).To(gomega.BeNil(), "pod %s is terminating", pod.Name)
		}
	})
}

func waitForOperandsReady(ctx context.Context, clients clusterClients) {
	ginkgo.GinkgoHelper()
	for _, selector := range operandSelectors {
		waitForPodsReady(ctx, clients, selector)
	}
}
