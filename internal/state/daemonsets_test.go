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
	appsv1 "k8s.io/api/apps/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/util/intstr"

	nvidiav1 "github.com/NVIDIA/gpu-operator/api/nvidia/v1"
	nvidiav1alpha1 "github.com/NVIDIA/gpu-operator/api/nvidia/v1alpha1"
	"github.com/NVIDIA/gpu-operator/internal/render"
)

func TestGPUClusterDaemonSetUpdateStrategy(t *testing.T) {
	states := map[string]interface {
		getManifestObjects(context.Context, *nvidiav1alpha1.GPUCluster, InfoCatalog) ([]*unstructured.Unstructured, error)
	}{
		"dcgm":          newTestDCGMState(t),
		"dcgm-exporter": newTestDCGMExporterState(t, false),
		"dra-validator": newTestDRAValidationState(t),
		"dra-driver":    newTestDRAState(t),
	}
	for name, s := range states {
		t.Run(name, func(t *testing.T) {
			cases := map[string]struct {
				strategy      string
				rollingUpdate *nvidiav1.RollingUpdateSpec
				expectedType  appsv1.DaemonSetUpdateStrategyType
			}{
				"default":        {expectedType: appsv1.RollingUpdateDaemonSetStrategyType},
				"rolling update": {strategy: "RollingUpdate", expectedType: appsv1.RollingUpdateDaemonSetStrategyType},
				"rolling update with settings": {
					strategy:      "RollingUpdate",
					rollingUpdate: &nvidiav1.RollingUpdateSpec{MaxUnavailable: "50%"},
					expectedType:  appsv1.RollingUpdateDaemonSetStrategyType,
				},
				"on delete": {strategy: "OnDelete", expectedType: appsv1.OnDeleteDaemonSetStrategyType},
				"on delete with rolling update settings": {
					strategy:      "OnDelete",
					rollingUpdate: &nvidiav1.RollingUpdateSpec{MaxUnavailable: "50%"},
					expectedType:  appsv1.OnDeleteDaemonSetStrategyType,
				},
			}
			for testName, tc := range cases {
				t.Run(testName, func(t *testing.T) {
					cr := sampleGPUCluster()
					cr.Spec.DCGM = &nvidiav1.DCGMSpec{Enabled: new(true)}
					cr.Spec.DCGMExporter = &nvidiav1.DCGMExporterSpec{}
					cr.Spec.Daemonsets.UpdateStrategy = tc.strategy
					cr.Spec.Daemonsets.RollingUpdate = tc.rollingUpdate
					objs, err := s.getManifestObjects(context.Background(), cr, draSupportedCatalog())
					require.NoError(t, err)
					ds := findDaemonSet(t, objs)
					require.Equal(t, tc.expectedType, ds.Spec.UpdateStrategy.Type)
					switch {
					case tc.expectedType == appsv1.OnDeleteDaemonSetStrategyType:
						require.Nil(t, ds.Spec.UpdateStrategy.RollingUpdate)
					case name == "dra-driver" || name == "dra-validator":
						require.NotNil(t, ds.Spec.UpdateStrategy.RollingUpdate)
						require.NotNil(t, ds.Spec.UpdateStrategy.RollingUpdate.MaxUnavailable)
						require.Equal(t, intstr.FromString("100%"), *ds.Spec.UpdateStrategy.RollingUpdate.MaxUnavailable)
					default:
						require.Nil(t, ds.Spec.UpdateStrategy.RollingUpdate)
					}
				})
			}
		})
	}
}

func TestGPUClusterUpdateStrategyForNewOperand(t *testing.T) {
	// A new operand inherits the policy without templating the requested strategy.
	manifest := `apiVersion: apps/v1
kind: DaemonSet
metadata:
  name: new-operand
spec:
  updateStrategy:
    type: RollingUpdate
    rollingUpdate:
      maxUnavailable: 0
      maxSurge: 1
---
apiVersion: v1
kind: ConfigMap
metadata:
  name: new-operand-config
data:
  setting: unchanged
`
	path := filepath.Join(t.TempDir(), "operand.yaml")
	require.NoError(t, os.WriteFile(path, []byte(manifest), 0600))
	s := stateSkel{renderer: render.NewRenderer([]string{path})}
	for _, strategy := range []string{"", "RollingUpdate", "OnDelete"} {
		t.Run("strategy="+strategy, func(t *testing.T) {
			cr := sampleGPUCluster()
			cr.Spec.Daemonsets.UpdateStrategy = strategy
			baseline, err := s.renderObjects(context.Background(), nil)
			require.NoError(t, err)
			objs, err := s.renderGPUClusterObjects(context.Background(), cr, nil)
			require.NoError(t, err)
			require.Len(t, objs, 2)
			require.Equal(t, baseline[1], objs[1], "non-DaemonSet objects must remain unchanged")
			ds := findDaemonSet(t, objs)
			if strategy == "OnDelete" {
				require.Equal(t, appsv1.OnDeleteDaemonSetStrategyType, ds.Spec.UpdateStrategy.Type)
				require.Nil(t, ds.Spec.UpdateStrategy.RollingUpdate)
			} else {
				require.Equal(t, baseline[0], objs[0], "operand rolling-update settings must remain unchanged")
			}
		})
	}
}
