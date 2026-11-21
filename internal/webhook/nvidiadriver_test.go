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
	"testing"

	"github.com/stretchr/testify/require"
	admissionv1 "k8s.io/api/admission/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"

	nvidiav1alpha1 "github.com/NVIDIA/gpu-operator/api/nvidia/v1alpha1"
)

func TestNVIDIADriverWebhook(t *testing.T) {
	scheme := runtime.NewScheme()
	require.NoError(t, nvidiav1alpha1.AddToScheme(scheme))
	webhook := admission.WithValidator(scheme, &NVIDIADriverValidator{})

	validDriver := []byte(`{"apiVersion":"nvidia.com/v1alpha1","kind":"NVIDIADriver","metadata":{"name":"driver"}}`)
	testCases := map[string]struct {
		operation admissionv1.Operation
		object    []byte
		oldObject []byte
		allowed   bool
	}{
		"create": {
			operation: admissionv1.Create,
			object:    validDriver,
			allowed:   true,
		},
		"update": {
			operation: admissionv1.Update,
			object:    validDriver,
			oldObject: validDriver,
			allowed:   true,
		},
		"malformed create": {
			operation: admissionv1.Create,
			object:    []byte(`{"apiVersion":`),
		},
		"malformed update": {
			operation: admissionv1.Update,
			object:    validDriver,
			oldObject: []byte(`{"apiVersion":`),
		},
	}

	for name, tc := range testCases {
		t.Run(name, func(t *testing.T) {
			request := admission.Request{AdmissionRequest: admissionv1.AdmissionRequest{
				Operation: tc.operation,
				Object:    runtime.RawExtension{Raw: tc.object},
				OldObject: runtime.RawExtension{Raw: tc.oldObject},
			}}
			response := webhook.Handle(t.Context(), request)
			require.Equal(t, tc.allowed, response.Allowed)
		})
	}
}
