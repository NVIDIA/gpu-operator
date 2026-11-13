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

package controllers

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/runtime/serializer/json"
	"k8s.io/client-go/kubernetes/scheme"
	ctrl "sigs.k8s.io/controller-runtime"

	gpuv1 "github.com/NVIDIA/gpu-operator/api/nvidia/v1"
)

// namedContainerSecurity is one container securityContext, copied so later mutation
// of the DaemonSet does not change the snapshot.
type namedContainerSecurity struct {
	name string
	sc   *corev1.SecurityContext
}

func snapshotContainerSecurity(spec corev1.PodSpec) []namedContainerSecurity {
	out := make([]namedContainerSecurity, 0, len(spec.InitContainers)+len(spec.Containers))
	for i := range spec.InitContainers {
		c := spec.InitContainers[i]
		out = append(out, namedContainerSecurity{name: "init/" + c.Name, sc: c.SecurityContext.DeepCopy()})
	}
	for i := range spec.Containers {
		c := spec.Containers[i]
		out = append(out, namedContainerSecurity{name: c.Name, sc: c.SecurityContext.DeepCopy()})
	}
	return out
}

func loadDaemonSetAsset(t *testing.T, rel string) *appsv1.DaemonSet {
	t.Helper()
	buf, err := os.ReadFile(filepath.Join(cfg.root, rel))
	require.NoError(t, err)
	ds := &appsv1.DaemonSet{}
	ser := json.NewSerializerWithOptions(json.DefaultMetaFactory, scheme.Scheme, scheme.Scheme,
		json.SerializerOptions{Yaml: true, Pretty: false, Strict: false})
	_, _, err = ser.Decode(buf, nil, ds)
	require.NoError(t, err)
	return ds
}

// TestClusterPolicyDaemonsetsPodSecurityContext is the control for the GPUCluster renders.
// ClusterPolicy copies daemonsets.podSecurityContext onto the pod and still forces the
// validator containers to run as root, leaving privileged and container SELinux options in place.
func TestClusterPolicyDaemonsetsPodSecurityContext(t *testing.T) {
	t.Setenv("DCGM_IMAGE", "example.com/dcgm:test")
	t.Setenv("DCGM_EXPORTER_IMAGE", "example.com/dcgm-exporter:test")
	t.Setenv("VALIDATOR_IMAGE", "example.com/gpu-operator:test")

	podSecurityContext := &corev1.PodSecurityContext{
		SELinuxOptions: &corev1.SELinuxOptions{
			Type:  "spc_t",
			Level: "s0",
		},
		RunAsUser: new(int64(1000)),
	}

	cases := []struct {
		name      string
		asset     string
		runAsRoot []string
	}{
		{
			name:      "dcgm",
			asset:     "assets/state-dcgm/0400_dcgm.yml",
			runAsRoot: []string{"init/toolkit-validation"},
		},
		{
			name:      "dcgm-exporter",
			asset:     "assets/state-dcgm-exporter/0800_daemonset.yaml",
			runAsRoot: []string{"init/toolkit-validation"},
		},
		{
			name:  "validator",
			asset: "assets/state-operator-validation/0500_daemonset.yaml",
			runAsRoot: []string{
				"init/driver-validation",
				"init/toolkit-validation",
				"init/cuda-validation",
				"init/plugin-validation",
				"nvidia-operator-validator",
			},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			ds := loadDaemonSetAsset(t, tc.asset)
			before := snapshotContainerSecurity(ds.Spec.Template.Spec)

			n := ClusterPolicyController{
				singleton: &gpuv1.ClusterPolicy{
					Spec: gpuv1.ClusterPolicySpec{
						Daemonsets: gpuv1.DaemonsetsSpec{PodSecurityContext: podSecurityContext},
					},
				},
				logger:  ctrl.Log.WithName("test"),
				runtime: gpuv1.Containerd,
			}
			require.NoError(t, preProcessDaemonSet(ds, n))

			require.Equal(t, podSecurityContext, ds.Spec.Template.Spec.SecurityContext)

			after := snapshotContainerSecurity(ds.Spec.Template.Spec)
			require.Equal(t, len(before), len(after))
			runAsRoot := map[string]bool{}
			for _, name := range tc.runAsRoot {
				runAsRoot[name] = true
			}
			for i := range before {
				require.Equal(t, before[i].name, after[i].name)
				require.Equal(t, privileged(before[i].sc), privileged(after[i].sc), before[i].name)
				require.Equal(t, seLinux(before[i].sc), seLinux(after[i].sc), before[i].name)
				if runAsRoot[before[i].name] {
					require.NotNil(t, after[i].sc)
					require.NotNil(t, after[i].sc.RunAsUser)
					require.Equal(t, int64(0), *after[i].sc.RunAsUser)
					require.NotNil(t, before[i].sc)
					require.NotNil(t, before[i].sc.Privileged)
					require.True(t, *before[i].sc.Privileged)
				} else if before[i].sc != nil {
					require.Equal(t, before[i].sc.RunAsUser, after[i].sc.RunAsUser)
				}
			}
		})
	}

	t.Run("unset", func(t *testing.T) {
		ds := loadDaemonSetAsset(t, "assets/state-dcgm/0400_dcgm.yml")
		n := ClusterPolicyController{
			singleton: &gpuv1.ClusterPolicy{},
			logger:    ctrl.Log.WithName("test"),
			runtime:   gpuv1.Containerd,
		}
		require.NoError(t, preProcessDaemonSet(ds, n))
		require.Nil(t, ds.Spec.Template.Spec.SecurityContext)
	})
}

func privileged(sc *corev1.SecurityContext) *bool {
	if sc == nil {
		return nil
	}
	return sc.Privileged
}

func seLinux(sc *corev1.SecurityContext) *corev1.SELinuxOptions {
	if sc == nil {
		return nil
	}
	return sc.SELinuxOptions
}
