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
	"errors"
	"io"
	"strings"
	"testing"
	"time"

	"github.com/sirupsen/logrus"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"

	"github.com/NVIDIA/gpu-operator/internal/gpuconfig/decision"
)

func TestReadSettings(t *testing.T) {
	cases := map[string]struct {
		env     map[string]string
		want    settings
		wantErr []string
	}{
		"defaults": {
			env: map[string]string{envNodeName: "node-1", envNamespace: "gpu-operator", envStack: "dra"},
			want: settings{
				nodeName: "node-1", namespace: "gpu-operator", stack: decision.DRAStack,
				clusterDocument: defaultClusterDocument, poolLabelKey: defaultPoolLabelKey,
			},
		},
		"overrides": {
			env: map[string]string{
				envNodeName: "node-1", envNamespace: "gpu-operator", envStack: "container",
				envClusterDocument: "gpu-config-v2", envPoolLabelKey: "karpenter.sh/nodepool",
			},
			want: settings{
				nodeName: "node-1", namespace: "gpu-operator", stack: decision.ContainerStack,
				clusterDocument: "gpu-config-v2", poolLabelKey: "karpenter.sh/nodepool",
			},
		},
		"invalid": {
			env:     map[string]string{envStack: "both"},
			wantErr: []string{envNodeName, envNamespace, envStack},
		},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			s, err := readSettings(func(k string) string { return tc.env[k] })
			if tc.wantErr != nil {
				for _, msg := range tc.wantErr {
					require.ErrorContains(t, err, msg)
				}
				return
			}
			require.NoError(t, err)
			require.Equal(t, tc.want, s)
		})
	}
}

func TestDocumentNames(t *testing.T) {
	log := logrus.New()
	log.SetOutput(io.Discard)
	s := settings{nodeName: "node-1", clusterDocument: "gpu-config", poolLabelKey: defaultPoolLabelKey}
	cases := map[string]struct {
		settings settings
		labels   map[string]string
		want     []string
	}{
		"no pool label":      {settings: s, want: []string{"gpu-config-node-node-1", "", "gpu-config"}},
		"pool label":         {settings: s, labels: map[string]string{defaultPoolLabelKey: "GPU_Train"}, want: []string{"gpu-config-node-node-1", "gpu-config-pool-gpu-train", "gpu-config"}},
		"invalid pool label": {settings: s, labels: map[string]string{defaultPoolLabelKey: "a..b"}, want: []string{"gpu-config-node-node-1", "", "gpu-config"}},
		"node name too long": {
			settings: settings{nodeName: strings.Repeat("a", 238), clusterDocument: "gpu-config", poolLabelKey: defaultPoolLabelKey},
			want:     []string{"", "", "gpu-config"},
		},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			node := &corev1.Node{ObjectMeta: metav1.ObjectMeta{Name: tc.settings.nodeName, Labels: tc.labels}}
			require.Equal(t, tc.want, documentNames(tc.settings, node, log))
		})
	}
}

// flakyMapper fails its first failures mappings, then maps any version.
type flakyMapper struct {
	meta.RESTMapper
	failures int
	calls    int
}

func (m *flakyMapper) RESTMapping(gk schema.GroupKind, versions ...string) (*meta.RESTMapping, error) {
	m.calls++
	if m.calls <= m.failures {
		return nil, errors.New("connection refused")
	}
	return &meta.RESTMapping{GroupVersionKind: gk.WithVersion(versions[0])}, nil
}

func TestServedDRAVersionRetries(t *testing.T) {
	log := logrus.New()
	log.SetOutput(io.Discard)
	cases := map[string]struct {
		failures   int
		wantSleeps int
	}{
		"first attempt succeeds": {failures: 0, wantSleeps: 0},
		"transient failures":     {failures: 2, wantSleeps: 2},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			sleeps := 0
			sleep := func(context.Context, time.Duration) error { sleeps++; return nil }
			got, err := servedDRAVersion(context.Background(), &flakyMapper{failures: tc.failures}, sleep, log)
			require.NoError(t, err)
			require.Equal(t, "v1", got)
			require.Equal(t, tc.wantSleeps, sleeps)
		})
	}
}
