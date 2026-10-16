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
	"github.com/onsi/ginkgo/v2"

	"k8s.io/client-go/kubernetes"

	gpuclientset "github.com/NVIDIA/gpu-operator/api/versioned"
	"github.com/NVIDIA/gpu-operator/tests/e2e/framework"
	"github.com/NVIDIA/gpu-operator/tests/e2e/helpers"
)

type clusterClients struct {
	kube            kubernetes.Interface
	pods            *helpers.PodClient
	nodes           *helpers.NodeClient
	daemonSets      *helpers.DaemonSetClient
	clusterPolicies *helpers.ClusterPolicyClient
	drivers         *helpers.NvidiaDriverClient
}

// newClusterClients builds clients directly because framework.Framework only creates
// its clientset in BeforeEach, which runs after the BeforeAll that installs the operator.
func newClusterClients() clusterClients {
	ginkgo.GinkgoHelper()

	config, err := framework.LoadConfig()
	framework.ExpectNoError(err, "failed to load kubeconfig")

	kubeClient, err := kubernetes.NewForConfig(config)
	framework.ExpectNoError(err, "failed to create the Kubernetes clientset")

	gpuClient, err := gpuclientset.NewForConfig(config)
	framework.ExpectNoError(err, "failed to create the GPU Operator clientset")

	return clusterClients{
		kube:            kubeClient,
		pods:            helpers.NewPodClient(kubeClient.CoreV1()),
		nodes:           helpers.NewNodeClient(kubeClient),
		daemonSets:      helpers.NewDaemonSetClient(kubeClient),
		clusterPolicies: helpers.NewClusterPolicyClient(gpuClient),
		drivers:         helpers.NewNvidiaDriverClient(gpuClient, kubeClient),
	}
}
