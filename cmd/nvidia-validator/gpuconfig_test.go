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

package main

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/NVIDIA/gpu-operator/internal/gpuconfig/gate"
)

func TestGPUConfigGateConfig(t *testing.T) {
	saved := []string{nodeNameFlag, namespaceFlag}
	t.Cleanup(func() { nodeNameFlag, namespaceFlag = saved[0], saved[1] })
	nodeNameFlag, namespaceFlag = "node-1", "gpu-operator"

	cases := map[string]struct {
		env     map[string]string
		want    gate.Config
		wantErr bool
	}{
		"container stack, default bound": {
			env: map[string]string{envGPUConfigStack: "container"},
			want: gate.Config{NodeName: "node-1", Namespace: "gpu-operator", ApplierDaemonSet: "nvidia-gpu-config-manager",
				Interval: gate.DefaultInterval, NoModuleBound: gate.DefaultNoModuleBound},
		},
		"DRA stack, custom bound": {
			env: map[string]string{envGPUConfigStack: "dra", envGPUConfigNoModuleTimeout: "90s"},
			want: gate.Config{NodeName: "node-1", Namespace: "gpu-operator", ApplierDaemonSet: "nvidia-gpu-config-manager-dra",
				Interval: gate.DefaultInterval, NoModuleBound: 90 * time.Second},
		},
		"missing stack":      {env: map[string]string{}, wantErr: true},
		"unknown stack":      {env: map[string]string{envGPUConfigStack: "both"}, wantErr: true},
		"invalid bound":      {env: map[string]string{envGPUConfigStack: "dra", envGPUConfigNoModuleTimeout: "soon"}, wantErr: true},
		"zero bound":         {env: map[string]string{envGPUConfigStack: "dra", envGPUConfigNoModuleTimeout: "0s"}, wantErr: true},
		"negative bound":     {env: map[string]string{envGPUConfigStack: "dra", envGPUConfigNoModuleTimeout: "-1m"}, wantErr: true},
		"bound without unit": {env: map[string]string{envGPUConfigStack: "dra", envGPUConfigNoModuleTimeout: "300"}, wantErr: true},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			g := &GPUConfig{getenv: func(k string) string { return tc.env[k] }}
			got, err := g.gateConfig()
			if tc.wantErr {
				require.Error(t, err)
				return
			}
			require.NoError(t, err)
			require.Equal(t, tc.want, got)
		})
	}
}

func TestGPUConfigFlags(t *testing.T) {
	saved := []string{componentFlag, nodeNameFlag, namespaceFlag}
	t.Cleanup(func() { componentFlag, nodeNameFlag, namespaceFlag = saved[0], saved[1], saved[2] })

	componentFlag = gpuConfigComponent
	require.True(t, isValidComponent())

	nodeNameFlag, namespaceFlag = "node-1", ""
	_, err := validateFlags(context.Background(), nil)
	require.Error(t, err)

	nodeNameFlag, namespaceFlag = "", "gpu-operator"
	_, err = validateFlags(context.Background(), nil)
	require.Error(t, err)

	nodeNameFlag, namespaceFlag = "node-1", "gpu-operator"
	_, err = validateFlags(context.Background(), nil)
	require.NoError(t, err)
}
