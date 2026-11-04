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

package gpuoperator

import (
	"os/exec"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
	appsv1 "k8s.io/api/apps/v1"
	policyv1 "k8s.io/api/policy/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/intstr"
	"sigs.k8s.io/yaml"
)

// renderChart returns the operator Deployment and, if rendered, its PodDisruptionBudget.
func renderChart(t *testing.T, values string) (*appsv1.Deployment, *policyv1.PodDisruptionBudget) {
	t.Helper()
	if _, err := exec.LookPath("helm"); err != nil {
		t.Skip("helm binary not found in PATH")
	}

	cmd := exec.Command("helm", "template", "gpu-operator", ".", "--namespace", "gpu-operator", "--values", "-")
	cmd.Stdin = strings.NewReader(values)
	var stderr strings.Builder
	cmd.Stderr = &stderr
	out, err := cmd.Output()
	require.NoError(t, err, stderr.String())

	var deployment *appsv1.Deployment
	var pdb *policyv1.PodDisruptionBudget
	for doc := range strings.SplitSeq(string(out), "\n---\n") {
		var meta metav1.PartialObjectMetadata
		require.NoError(t, yaml.Unmarshal([]byte(doc), &meta))
		if meta.Name != "gpu-operator" {
			continue
		}
		switch meta.Kind {
		case "Deployment":
			deployment = &appsv1.Deployment{}
			require.NoError(t, yaml.Unmarshal([]byte(doc), deployment))
		case "PodDisruptionBudget":
			pdb = &policyv1.PodDisruptionBudget{}
			require.NoError(t, yaml.Unmarshal([]byte(doc), pdb))
		}
	}
	require.NotNil(t, deployment)
	return deployment, pdb
}

func requireSoftPodAntiAffinity(t *testing.T, deployment *appsv1.Deployment) {
	t.Helper()
	podAntiAffinity := deployment.Spec.Template.Spec.Affinity.PodAntiAffinity
	require.NotNil(t, podAntiAffinity)
	require.Empty(t, podAntiAffinity.RequiredDuringSchedulingIgnoredDuringExecution)
	require.Len(t, podAntiAffinity.PreferredDuringSchedulingIgnoredDuringExecution, 1)
	term := podAntiAffinity.PreferredDuringSchedulingIgnoredDuringExecution[0].PodAffinityTerm
	require.Equal(t, "kubernetes.io/hostname", term.TopologyKey)
	require.Equal(t, deployment.Spec.Selector, term.LabelSelector)
}

func TestOperatorHighAvailability(t *testing.T) {
	testCases := map[string]struct {
		values string
		check  func(t *testing.T, deployment *appsv1.Deployment, pdb *policyv1.PodDisruptionBudget)
	}{
		"defaults render one replica, no anti-affinity and no PDB": {
			check: func(t *testing.T, deployment *appsv1.Deployment, pdb *policyv1.PodDisruptionBudget) {
				require.Equal(t, int32(1), *deployment.Spec.Replicas)
				require.Nil(t, deployment.Spec.Template.Spec.Affinity.PodAntiAffinity)
				require.Nil(t, pdb)
			},
		},
		"two replicas render soft anti-affinity and a PDB": {
			values: "operator: {replicaCount: 2}",
			check: func(t *testing.T, deployment *appsv1.Deployment, pdb *policyv1.PodDisruptionBudget) {
				require.NotNil(t, deployment.Spec.Replicas)
				require.Equal(t, int32(2), *deployment.Spec.Replicas)
				requireSoftPodAntiAffinity(t, deployment)
				require.NotNil(t, pdb)
				require.Equal(t, intstr.FromInt32(1), *pdb.Spec.MinAvailable)
				require.Equal(t, deployment.Spec.Selector, pdb.Spec.Selector)
			},
		},
		"two replicas with user-provided affinity render both the user's node affinity and HA anti-affinity": {
			// This is the user's intended node affinity
			values: `
operator:
  replicaCount: 2
  affinity:
    nodeAffinity:
      preferredDuringSchedulingIgnoredDuringExecution:
      - weight: 1
        preference:
          matchExpressions:
          - {key: example.com/pool, operator: Exists}
`,
			check: func(t *testing.T, deployment *appsv1.Deployment, _ *policyv1.PodDisruptionBudget) {
				require.NotNil(t, deployment.Spec.Replicas)
				require.Equal(t, int32(2), *deployment.Spec.Replicas)
				// Check if the rendered helm template includes user's intended node affinity
				terms := deployment.Spec.Template.Spec.Affinity.NodeAffinity.PreferredDuringSchedulingIgnoredDuringExecution
				require.Len(t, terms, 1)
				require.Equal(t, "example.com/pool", terms[0].Preference.MatchExpressions[0].Key)
				// Check if the rendered helm template includes the HA node affinity
				requireSoftPodAntiAffinity(t, deployment)
			},
		},
	}

	for name, tc := range testCases {
		t.Run(name, func(t *testing.T) {
			deployment, pdb := renderChart(t, tc.values)
			tc.check(t, deployment, pdb)
		})
	}
}
