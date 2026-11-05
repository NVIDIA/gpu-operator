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

package csv

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"testing"

	"github.com/operator-framework/api/pkg/operators/v1alpha1"
	"github.com/sirupsen/logrus"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/urfave/cli/v3"
	appsv1 "k8s.io/api/apps/v1"
)

func TestNewCommand(t *testing.T) {
	cmd := NewCommand(logrus.New())

	require.NotNil(t, cmd)
	assert.Equal(t, "csv", cmd.Name)
	assert.NotEmpty(t, cmd.Usage)

	idx := slices.IndexFunc(cmd.Flags, func(f cli.Flag) bool {
		return slices.Contains(f.Names(), "input")
	})
	require.GreaterOrEqual(t, idx, 0)

	inputFlag, ok := cmd.Flags[idx].(*cli.StringFlag)
	require.True(t, ok)
	assert.Contains(t, inputFlag.Names(), "input")
	assert.NotEmpty(t, inputFlag.Usage)
	assert.Equal(t, "-", inputFlag.Value)
}

func TestValidateReplicas(t *testing.T) {
	testCases := map[string]struct {
		name     string
		replicas *int32
		wantErr  bool
	}{
		"omitted defaults to one": {name: "gpu-operator"},
		"one replica":             {name: "gpu-operator", replicas: new(int32(1))},
		"two replicas":            {name: "gpu-operator", replicas: new(int32(2))},
		"zero replicas":           {name: "gpu-operator", replicas: new(int32(0)), wantErr: true},
		"negative replicas":       {name: "gpu-operator", replicas: new(int32(-1)), wantErr: true},
		"other deployment":        {name: "other", replicas: new(int32(0))},
	}

	for name, tc := range testCases {
		t.Run(name, func(t *testing.T) {
			csv := &v1alpha1.ClusterServiceVersion{}
			csv.Spec.InstallStrategy.StrategySpec.DeploymentSpecs = []v1alpha1.StrategyDeploymentSpec{
				{Name: "other"},
				{Name: tc.name, Spec: appsv1.DeploymentSpec{Replicas: tc.replicas}},
			}

			err := validateReplicas(csv)
			if tc.wantErr {
				require.ErrorContains(t, err, "gpu-operator replicas must be at least 1")
			} else {
				require.NoError(t, err)
			}
		})
	}
}

func TestRunRejectsInvalidReplicaCount(t *testing.T) {
	for _, replicas := range []int{0, -1} {
		t.Run(fmt.Sprint(replicas), func(t *testing.T) {
			manifest := fmt.Sprintf(`apiVersion: operators.coreos.com/v1alpha1
kind: ClusterServiceVersion
spec:
  install:
    strategy: deployment
    spec:
      deployments:
      - name: gpu-operator
        spec:
          replicas: %d
`, replicas)
			path := filepath.Join(t.TempDir(), "csv.yaml")
			require.NoError(t, os.WriteFile(path, []byte(manifest), 0o600))

			err := (command{}).run(context.Background(), &options{input: path})
			require.ErrorContains(t, err, "failed to validate replicas: gpu-operator replicas must be at least 1")
		})
	}
}
