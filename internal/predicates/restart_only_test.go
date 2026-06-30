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

package predicates

import (
	"testing"

	"github.com/go-logr/logr"
	"github.com/stretchr/testify/assert"
	corev1 "k8s.io/api/core/v1"

	driverconfig "github.com/NVIDIA/gpu-operator/internal/config"
)

func TestDriverPodRestartOnly(t *testing.T) {
	podSpec := func(digest string) *corev1.PodSpec {
		return &corev1.PodSpec{Containers: []corev1.Container{{
			Name: "nvidia-driver-ctr",
			Env:  []corev1.EnvVar{{Name: driverconfig.DriverConfigDigestEnvName, Value: digest}},
		}}}
	}

	predicate := DriverPodRestartOnly(logr.Discard())

	tests := []struct {
		name        string
		running     *corev1.PodSpec
		desired     *corev1.PodSpec
		wantRestart bool
	}{
		{name: "equal digests -> restart-only", running: podSpec("same"), desired: podSpec("same"), wantRestart: true},
		{name: "differing digests -> full upgrade", running: podSpec("old"), desired: podSpec("new"), wantRestart: false},
		{name: "missing digest on running pod -> full upgrade", running: podSpec(""), desired: podSpec("new"), wantRestart: false},
		{name: "missing digest on desired template -> full upgrade", running: podSpec("old"), desired: podSpec(""), wantRestart: false},
		{name: "nil running spec -> full upgrade", running: nil, desired: podSpec("x"), wantRestart: false},
		{name: "nil desired spec -> full upgrade", running: podSpec("x"), desired: nil, wantRestart: false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := predicate(tt.running, tt.desired)
			assert.NoError(t, err)
			assert.Equal(t, tt.wantRestart, got)
		})
	}
}
