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

package webhook

import (
	"context"

	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"

	nvidiav1alpha1 "github.com/NVIDIA/gpu-operator/api/nvidia/v1alpha1"
)

const NVIDIADriverPath = "/validate-nvidia-com-v1alpha1-nvidiadriver"

// NVIDIADriverValidator receives a read-only API reader for future validation
// against NodeList and NVIDIADriverList resources.
type NVIDIADriverValidator struct {
	Reader client.Reader
}

var _ admission.Validator[*nvidiav1alpha1.NVIDIADriver] = (*NVIDIADriverValidator)(nil)

func (v *NVIDIADriverValidator) ValidateCreate(ctx context.Context, _ *nvidiav1alpha1.NVIDIADriver) (admission.Warnings, error) {
	log.FromContext(ctx).Info("admitting NVIDIADriver create")
	return nil, nil
}

func (v *NVIDIADriverValidator) ValidateUpdate(ctx context.Context, _, _ *nvidiav1alpha1.NVIDIADriver) (admission.Warnings, error) {
	log.FromContext(ctx).Info("admitting NVIDIADriver update")
	return nil, nil
}

func (v *NVIDIADriverValidator) ValidateDelete(_ context.Context, _ *nvidiav1alpha1.NVIDIADriver) (admission.Warnings, error) {
	return nil, nil
}
