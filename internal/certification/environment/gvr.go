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

package environment

import "k8s.io/apimachinery/pkg/runtime/schema"

// OpenShift GVRs
var (
	clusterVersionGVR = schema.GroupVersionResource{
		Group:    "config.openshift.io",
		Version:  "v1",
		Resource: "clusterversions",
	}

	proxyGVR = schema.GroupVersionResource{
		Group:    "config.openshift.io",
		Version:  "v1",
		Resource: "proxies",
	}

	imageConfigGVR = schema.GroupVersionResource{
		Group:    "config.openshift.io",
		Version:  "v1",
		Resource: "images",
	}

	machineGVR = schema.GroupVersionResource{
		Group:    "machine.openshift.io",
		Version:  "v1beta1",
		Resource: "machines",
	}
)

// Virtualization GVRs
var (
	kubevirtGVR = schema.GroupVersionResource{
		Group:    "kubevirt.io",
		Version:  "v1",
		Resource: "kubevirts",
	}

	hyperconvergedGVR = schema.GroupVersionResource{
		Group:    "hco.kubevirt.io",
		Version:  "v1beta1",
		Resource: "hyperconvergeds",
	}
)
