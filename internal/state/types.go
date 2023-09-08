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
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/source"

	gpuv1 "github.com/NVIDIA/gpu-operator/api/v1"
	nvidiav1alpha1 "github.com/NVIDIA/gpu-operator/api/v1alpha1"
)

type ctrlManager ctrl.Manager
type SyncingSource source.SyncingSource

// driverSpec is a wrapper of NVIDIADriverSpec with an additional ImagePath field
// which is to be populated with the fully-qualified image path.
type driverSpec struct {
	Spec              *nvidiav1alpha1.NVIDIADriverSpec
	Name              string
	ImagePath         string
	ManagerImagePath  string
	OCPToolkitEnabled bool
}

// validatorSpec is a wrapper of ValidatorSpec with an additional ImagePath field
// which is to be populated with the fully-qualified image path.
type validatorSpec struct {
	Spec      *gpuv1.ValidatorSpec
	ImagePath string
}

// validatorSpec is a wrapper of GPUDirectStorageSpec with an additional ImagePath field
// which is to be populated with the fully-qualified image path.
type gdsDriverSpec struct {
	Spec      *gpuv1.GPUDirectStorageSpec
	ImagePath string
}

type openshiftDriverSpecOverlay struct {
	NameSuffix         string
	ContainerEnvs      map[string][]gpuv1.EnvVar
	ContainerArgs      map[string][]string
	ContainerCmd       map[string][]string
	DriverToolkitImage string
	Labels             map[string]string
	NodeSelector       map[string]string
	PodTemplateLabels  map[string]string
}
