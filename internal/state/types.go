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

package state

import (
	corev1 "k8s.io/api/core/v1"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/source"

	nvidiav1 "github.com/NVIDIA/gpu-operator/api/nvidia/v1"
	nvidiav1alpha1 "github.com/NVIDIA/gpu-operator/api/nvidia/v1alpha1"
)

type ctrlManager ctrl.Manager
type SyncingSource source.SyncingSource

// driverSpec is a wrapper of NVIDIADriverSpec with an additional ImagePath field
// which is to be populated with the fully-qualified image path.
type driverSpec struct {
	Spec             *nvidiav1alpha1.NVIDIADriverSpec
	AppName          string
	Name             string
	ImagePath        string
	ManagerImagePath string
	OSVersion        string
}

// gdsDriverSpec is a wrapper of GPUDirectStorageSpec with an additional ImagePath field
// which is to be populated with the fully-qualified image path.
type gdsDriverSpec struct {
	Spec      *nvidiav1alpha1.GPUDirectStorageSpec
	ImagePath string
}

// gdrcopyDriverSpec is a wrapper of GDRCopySpec with an additional ImagePath field
// which is to be populated with the fully-qualified image path.
type gdrcopyDriverSpec struct {
	Spec      *nvidiav1alpha1.GDRCopySpec
	ImagePath string
}

// draDriverSpec is a wrapper of DRADriverSpec with the fully-qualified image paths
// populated: ImagePath for the DRA driver containers and InitImagePath for the
// driver-validation init container (shipped in the gpu-operator image).
type draDriverSpec struct {
	Spec          *nvidiav1alpha1.DRADriverSpec
	ImagePath     string
	InitImagePath string
}

// draDriverRenderData is the templating data for the DRA driver manifests.
type draDriverRenderData struct {
	DRADriver  *draDriverSpec
	HostPaths  *nvidiav1.HostPathsSpec
	Daemonsets *nvidiav1.DaemonsetsSpec
	Namespace  string
	// DeviceClassAPIVersion is the apiVersion to render on DeviceClass objects,
	// determined from the resource.k8s.io version served by the cluster.
	DeviceClassAPIVersion string
	// FeatureGates is the pre-rendered FEATURE_GATES env value (empty when none).
	FeatureGates string
	// The kubelet-plugin DaemonSet hosts both the gpus and computeDomains containers,
	// so these pod-level scheduling fields are merged from the daemonsets defaults and
	// the per-capability kubeletPlugin blocks (see mergeKubeletPluginScheduling).
	KubeletPluginPriorityClassName string
	KubeletPluginNodeSelector      map[string]string
	KubeletPluginTolerations       []corev1.Toleration
	KubeletPluginAffinity          *corev1.Affinity
}
