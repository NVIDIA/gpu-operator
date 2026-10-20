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

package operator

import (
	"github.com/onsi/ginkgo/v2"

	"k8s.io/client-go/kubernetes"

	gpuclientset "github.com/NVIDIA/gpu-operator/api/versioned"
	"github.com/NVIDIA/gpu-operator/tests/e2e/framework"
	"github.com/NVIDIA/gpu-operator/tests/e2e/helpers"
)

type Clients struct {
	Kube            kubernetes.Interface
	Pods            *helpers.PodClient
	Nodes           *helpers.NodeClient
	DaemonSets      *helpers.DaemonSetClient
	ClusterPolicies *helpers.ClusterPolicyClient
	Drivers         *helpers.NvidiaDriverClient
}

func NewClients() Clients {
	ginkgo.GinkgoHelper()

	config, err := framework.LoadConfig()
	framework.ExpectNoError(err, "failed to load kubeconfig")

	kubeClient, err := kubernetes.NewForConfig(config)
	framework.ExpectNoError(err, "failed to create the Kubernetes clientset")

	gpuClient, err := gpuclientset.NewForConfig(config)
	framework.ExpectNoError(err, "failed to create the GPU Operator clientset")

	return Clients{
		Kube:            kubeClient,
		Pods:            helpers.NewPodClient(kubeClient.CoreV1()),
		Nodes:           helpers.NewNodeClient(kubeClient),
		DaemonSets:      helpers.NewDaemonSetClient(kubeClient),
		ClusterPolicies: helpers.NewClusterPolicyClient(gpuClient),
		Drivers:         helpers.NewNvidiaDriverClient(gpuClient, kubeClient),
	}
}
