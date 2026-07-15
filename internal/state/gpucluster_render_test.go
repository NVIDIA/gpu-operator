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
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/utils/ptr"

	nvidiav1 "github.com/NVIDIA/gpu-operator/api/nvidia/v1"
	nvidiav1alpha1 "github.com/NVIDIA/gpu-operator/api/nvidia/v1alpha1"
)

// fullSpecGPUCluster returns a CR exercising the optional DRA driver knobs: the
// computeDomains capability with controller/kubelet-plugin overrides and per-container
// env/resources on the gpus kubelet plugin.
func fullSpecGPUCluster() *nvidiav1alpha1.GPUCluster {
	cr := sampleGPUCluster()
	cr.Spec.DRADriver.GPUs.KubeletPlugin = nvidiav1alpha1.DRADriverKubeletPluginSpec{
		Env: []nvidiav1.EnvVar{{Name: "GPUS_EXTRA", Value: "1"}},
		Resources: &nvidiav1.ResourceRequirements{
			Requests: corev1.ResourceList{
				corev1.ResourceCPU:    resource.MustParse("100m"),
				corev1.ResourceMemory: resource.MustParse("128Mi"),
			},
		},
		Healthcheck: &nvidiav1alpha1.DRADriverHealthcheckSpec{Port: ptr.To(int32(52000))},
	}
	cr.Spec.DRADriver.ComputeDomains = nvidiav1alpha1.DRADriverComputeDomainsSpec{
		Enabled: ptr.To(true),
		Controller: nvidiav1alpha1.DRADriverControllerSpec{
			Env: []nvidiav1.EnvVar{{Name: "CD_CONTROLLER_EXTRA", Value: "1"}},
		},
		KubeletPlugin: nvidiav1alpha1.DRADriverKubeletPluginSpec{
			Env: []nvidiav1.EnvVar{{Name: "CD_PLUGIN_EXTRA", Value: "1"}},
		},
	}
	return cr
}

// TestGPUClusterRenderGolden renders each GPUCluster operand's manifests end to end and
// byte-compares the full YAML stream against fixtures in testdata/golden, so unintended
// template changes surface as diffs (mirroring the NVIDIADriver renderer tests).
func TestGPUClusterRenderGolden(t *testing.T) {
	cases := []struct {
		name   string
		render func(t *testing.T) []*unstructured.Unstructured
	}{
		{
			// gpus capability only: computeDomains defaults to disabled in-code.
			name: "gpucluster-dra-driver-gpus-only",
			render: func(t *testing.T) []*unstructured.Unstructured {
				s := newTestDRAState(t)
				objs, err := s.getManifestObjects(context.Background(), sampleGPUCluster(), draSupportedCatalog())
				require.NoError(t, err)
				return objs
			},
		},
		{
			name: "gpucluster-dra-driver-full-spec",
			render: func(t *testing.T) []*unstructured.Unstructured {
				s := newTestDRAState(t)
				objs, err := s.getManifestObjects(context.Background(), fullSpecGPUCluster(), draSupportedCatalog())
				require.NoError(t, err)
				return objs
			},
		},
		{
			name: "gpucluster-dra-validation",
			render: func(t *testing.T) []*unstructured.Unstructured {
				s := newTestDRAValidationState(t)
				objs, err := s.getManifestObjects(context.Background(), sampleGPUCluster(), draSupportedCatalog())
				require.NoError(t, err)
				return objs
			},
		},
		{
			name: "gpucluster-dcgm",
			render: func(t *testing.T) []*unstructured.Unstructured {
				s := newTestDCGMState(t)
				cr := sampleGPUCluster()
				cr.Spec.DCGM = &nvidiav1.DCGMSpec{Enabled: ptr.To(true)}
				objs, err := s.getManifestObjects(context.Background(), cr, draSupportedCatalog())
				require.NoError(t, err)
				return objs
			},
		},
		{
			// dcgm disabled: the exporter runs its embedded nv-hostengine.
			name: "gpucluster-dcgm-exporter-embedded",
			render: func(t *testing.T) []*unstructured.Unstructured {
				s := newTestDCGMExporterState(t, false)
				objs, err := s.getManifestObjects(context.Background(), exporterCR(&nvidiav1.DCGMExporterSpec{}), draSupportedCatalog())
				require.NoError(t, err)
				return objs
			},
		},
		{
			// dcgm enabled: the exporter wires DCGM_REMOTE_HOSTENGINE_INFO to the
			// standalone hostengine Service.
			name: "gpucluster-dcgm-exporter-remote-engine",
			render: func(t *testing.T) []*unstructured.Unstructured {
				s := newTestDCGMExporterState(t, false)
				cr := exporterCR(&nvidiav1.DCGMExporterSpec{})
				cr.Spec.DCGM = &nvidiav1.DCGMSpec{Enabled: ptr.To(true)}
				objs, err := s.getManifestObjects(context.Background(), cr, draSupportedCatalog())
				require.NoError(t, err)
				return objs
			},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			objs := tc.render(t)
			require.NotEmpty(t, objs)

			actual, err := getYAMLString(objs)
			require.NoError(t, err)

			expected, err := os.ReadFile(filepath.Join(manifestResultDir, tc.name+".yaml"))
			require.NoError(t, err)
			require.Equal(t, string(expected), actual)
		})
	}
}
