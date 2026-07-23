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

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	nvidiav1 "github.com/NVIDIA/gpu-operator/api/nvidia/v1"
)

const dcgmManifestDir = "../../manifests/state-dcgm"

func newTestDCGMState(t *testing.T) *configurableState {
	t.Helper()
	t.Setenv("DCGM_IMAGE", "nvcr.io/nvidia/cloud-native/dcgm:test")
	client := fake.NewClientBuilder().Build()
	s, err := NewStateDCGM(client, "test-operator", runtime.NewScheme(), dcgmManifestDir)
	require.NoError(t, err)
	return s.(*configurableState)
}

func kindCounts(objs []*unstructured.Unstructured) map[string]int {
	kinds := map[string]int{}
	for _, o := range objs {
		kinds[o.GetKind()]++
	}
	return kinds
}

func findByKind(objs []*unstructured.Unstructured, kind string) *unstructured.Unstructured {
	for _, o := range objs {
		if o.GetKind() == kind {
			return o
		}
	}
	return nil
}

// claimHasAdminAccess verifies the ResourceClaimTemplate requests all GPUs with adminAccess.
func claimHasAdminAccess(t *testing.T, rct *unstructured.Unstructured) {
	t.Helper()
	requests, found, err := unstructured.NestedSlice(rct.Object, "spec", "spec", "devices", "requests")
	require.NoError(t, err)
	require.True(t, found)
	require.Len(t, requests, 1)
	req := requests[0].(map[string]interface{})
	// v1/v1beta2 nest under "exactly"; v1beta1 is flat. The test cluster serves v1.
	device := req
	if exactly, ok := req["exactly"].(map[string]interface{}); ok {
		device = exactly
	}
	assert.Equal(t, "gpu.nvidia.com", device["deviceClassName"])
	assert.Equal(t, "All", device["allocationMode"])
	assert.Equal(t, true, device["adminAccess"])
}

func TestDCGMDisabledByDefault(t *testing.T) {
	s := newTestDCGMState(t)

	// DCGM absent entirely.
	objs, err := s.getManifestObjects(context.Background(), sampleGPUCluster(), draSupportedCatalog())
	require.NoError(t, err)
	assert.Empty(t, objs, "DCGM must not render when the dcgm block is absent")

	// DCGM present but Enabled nil — the reused v1 type would treat this as enabled, but the
	// DRA stack must default it to disabled.
	cr := sampleGPUCluster()
	cr.Spec.DCGM = &nvidiav1.DCGMSpec{Repository: "nvcr.io/nvidia/cloud-native", Image: "dcgm", Version: "test"}
	objs, err = s.getManifestObjects(context.Background(), cr, draSupportedCatalog())
	require.NoError(t, err)
	assert.Empty(t, objs, "DCGM must default to disabled when enabled is nil")

	// Explicitly disabled.
	cr.Spec.DCGM.Enabled = ptr.To(false)
	objs, err = s.getManifestObjects(context.Background(), cr, draSupportedCatalog())
	require.NoError(t, err)
	assert.Empty(t, objs, "DCGM must not render when explicitly disabled")
}

func TestDCGMEnabled(t *testing.T) {
	s := newTestDCGMState(t)
	cr := sampleGPUCluster()
	cr.Spec.DCGM = &nvidiav1.DCGMSpec{
		Enabled:    ptr.To(true),
		Repository: "nvcr.io/nvidia/cloud-native",
		Image:      "dcgm",
		Version:    "4.5.2",
	}

	objs, err := s.getManifestObjects(context.Background(), cr, draSupportedCatalog())
	require.NoError(t, err)

	kinds := kindCounts(objs)
	assert.Equal(t, 1, kinds["ServiceAccount"])
	assert.Equal(t, 1, kinds["Role"])
	assert.Equal(t, 1, kinds["RoleBinding"])
	assert.Equal(t, 1, kinds["ResourceClaimTemplate"])
	assert.Equal(t, 1, kinds["DaemonSet"])
	assert.Equal(t, 1, kinds["Service"])

	claimHasAdminAccess(t, findByKind(objs, "ResourceClaimTemplate"))

	ds := findDaemonSet(t, objs)
	podSpec := ds.Spec.Template.Spec
	assert.Equal(t, "true", podSpec.NodeSelector["nvidia.com/gpu.deploy.dcgm"])
	require.Len(t, podSpec.Containers, 1)
	ctr := podSpec.Containers[0]
	assert.Equal(t, "nvidia-dcgm-ctr", ctr.Name)
	assert.Equal(t, "nvcr.io/nvidia/cloud-native/dcgm:4.5.2", ctr.Image)
	require.Len(t, ctr.Ports, 1)
	assert.Equal(t, int32(5555), ctr.Ports[0].ContainerPort)
	// GPU visibility comes from the admin-access claim, not a privileged hostPath.
	require.Len(t, podSpec.ResourceClaims, 1)
	assert.Equal(t, "admin-gpus", podSpec.ResourceClaims[0].Name)
	require.NotNil(t, podSpec.ResourceClaims[0].ResourceClaimTemplateName)
	assert.Equal(t, "nvidia-dcgm-admin", *podSpec.ResourceClaims[0].ResourceClaimTemplateName)
	require.Len(t, ctr.Resources.Claims, 1)
	assert.Equal(t, "admin-gpus", ctr.Resources.Claims[0].Name)

	// The exporter targets this Service on port 5555.
	svc := findByKind(objs, "Service")
	port, found, err := unstructured.NestedSlice(svc.Object, "spec", "ports")
	require.NoError(t, err)
	require.True(t, found)
	require.Len(t, port, 1)
	assert.Equal(t, int64(5555), port[0].(map[string]interface{})["port"])
}

func TestDCGMImageFromEnvFallback(t *testing.T) {
	s := newTestDCGMState(t)
	cr := sampleGPUCluster()
	// No repository/image/version — must fall back to DCGM_IMAGE.
	cr.Spec.DCGM = &nvidiav1.DCGMSpec{Enabled: ptr.To(true)}

	objs, err := s.getManifestObjects(context.Background(), cr, draSupportedCatalog())
	require.NoError(t, err)
	ds := findDaemonSet(t, objs)
	assert.Equal(t, "nvcr.io/nvidia/cloud-native/dcgm:test", ds.Spec.Template.Spec.Containers[0].Image)
}
