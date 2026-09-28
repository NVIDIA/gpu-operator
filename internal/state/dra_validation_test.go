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
	"context"
	"testing"

	"github.com/stretchr/testify/require"
	appsv1 "k8s.io/api/apps/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
)

const draValidationManifestDir = "../../manifests/state-dra-validation"

func newTestDRAValidationState(t *testing.T) *configurableState {
	t.Helper()
	t.Setenv("VALIDATOR_IMAGE", "nvcr.io/nvidia/gpu-operator-validator:test")
	c := fake.NewClientBuilder().Build()
	s, err := NewStateDRAValidation(c, "test-operator", runtime.NewScheme(), draValidationManifestDir)
	require.NoError(t, err)
	return s.(*configurableState)
}

func TestDRAValidationRender(t *testing.T) {
	s := newTestDRAValidationState(t)

	objs, err := s.getManifestObjects(context.Background(), sampleGPUCluster(), draSupportedCatalog())
	require.NoError(t, err)
	require.NotEmpty(t, objs)

	var ds *appsv1.DaemonSet
	for _, o := range objs {
		if o.GetKind() == "DaemonSet" {
			ds = &appsv1.DaemonSet{}
			require.NoError(t, runtime.DefaultUnstructuredConverter.FromUnstructured(o.Object, ds))
		}
	}
	require.NotNil(t, ds, "validator DaemonSet must be rendered")
	// Scheduling is gated on the deploy label so the k8s-driver-manager can pause the
	// validator to drain it across a driver reload.
	require.Equal(t, "true", ds.Spec.Template.Spec.NodeSelector["nvidia.com/gpu.deploy.dra-validator"])
	// The upgrade controller's validation gate selects pods by this label.
	require.Equal(t, "nvidia-operator-validator", ds.Spec.Template.Labels["app"])
	// The validator proves the DRA driver by consuming an adminAccess GPU claim.
	require.NotEmpty(t, ds.Spec.Template.Spec.ResourceClaims)
	require.NotEmpty(t, ds.Spec.Template.Spec.Containers)
	require.Equal(t, "nvcr.io/nvidia/gpu-operator-validator:test", ds.Spec.Template.Spec.Containers[0].Image)
}
