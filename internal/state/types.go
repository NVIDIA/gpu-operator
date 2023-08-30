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

// A wrapper of NVIDIADriverSpec with an additional ImagePath field
// which is to be populated with the fully-qualified image path.
type driverSpec struct {
	Spec             *nvidiav1alpha1.NVIDIADriverSpec
	ImagePath        string
	ManagerImagePath string
}

// A wrapper of ValidatorSpec with an additional ImagePath field
// which is to be populated with the fully-qualified image path.
type validatorSpec struct {
	Spec      *gpuv1.ValidatorSpec
	ImagePath string
}
