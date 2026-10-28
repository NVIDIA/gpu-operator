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
	"io/fs"
	"os"
	"path/filepath"
	"testing"

	"github.com/go-logr/logr"
	"github.com/stretchr/testify/require"
	rbacv1 "k8s.io/api/rbac/v1"
)

func TestManifestKind(t *testing.T) {
	tests := []struct {
		name         string
		manifest     string
		expectedKind string
		errorMessage string
	}{
		{
			name: "YAML",
			manifest: `apiVersion: v1
kind: ServiceAccount
metadata:
  name: test
`,
			expectedKind: "ServiceAccount",
		},
		{
			name:         "JSON",
			manifest:     `{"apiVersion":"v1","kind":"ConfigMap","metadata":{"name":"test"}}`,
			expectedKind: "ConfigMap",
		},
		{
			name: "kind-like text in another field",
			manifest: `apiVersion: v1
metadata:
  annotations:
    example.com/value: "kind: WrongKind"
  name: test
kind: Service
`,
			expectedKind: "Service",
		},
		{
			name: "missing kind",
			manifest: `apiVersion: v1
metadata:
  name: test
`,
			errorMessage: "manifest is missing kind",
		},
		{
			name:         "malformed YAML",
			manifest:     "kind: [",
			errorMessage: "failed to decode manifest metadata",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			kind, err := manifestKind([]byte(tc.manifest))
			if tc.errorMessage != "" {
				require.ErrorContains(t, err, tc.errorMessage)
				require.Empty(t, kind)
				return
			}
			require.NoError(t, err)
			require.Equal(t, tc.expectedKind, kind)
		})
	}
}

func TestAllAssetsHaveManifestKind(t *testing.T) {
	root, err := os.OpenRoot("../assets")
	require.NoError(t, err)
	t.Cleanup(func() {
		require.NoError(t, root.Close())
	})

	err = fs.WalkDir(root.FS(), ".", func(path string, entry fs.DirEntry, walkErr error) error {
		require.NoError(t, walkErr)
		if entry.IsDir() {
			return nil
		}

		manifest, err := root.ReadFile(path)
		require.NoError(t, err)
		kind, err := manifestKind(manifest)
		require.NoErrorf(t, err, "failed to read kind from %s", path)
		require.NotEmptyf(t, kind, "empty kind in %s", path)
		return nil
	})
	require.NoError(t, err)
}

func TestOpenShiftAssetAndRBACFiltering(t *testing.T) {
	for _, component := range []string{"state-driver", "state-dcgm", "state-device-plugin"} {
		t.Run(component, func(t *testing.T) {
			wantClusterRoles := 1
			if component == "state-dcgm" {
				wantClusterRoles = 0
			}
			path := filepath.Join("../assets", component)
			for _, tc := range []struct {
				name      string
				version   string
				wantRoles int
			}{
				{name: "Kubernetes", wantRoles: 0},
				{name: "OpenShift", version: "4.18", wantRoles: 1},
			} {
				t.Run(tc.name, func(t *testing.T) {
					ctrl := &ClusterPolicyController{logger: logr.Discard()}
					assets := getAssetsFrom(ctrl, path, tc.version)
					counts := map[string]int{}
					for _, asset := range assets {
						kind, err := manifestKind(asset)
						require.NoError(t, err)
						counts[kind]++
					}
					require.Equal(t, tc.wantRoles, counts["Role"])
					require.Equal(t, tc.wantRoles, counts["RoleBinding"])
					require.Equal(t, wantClusterRoles, counts["ClusterRole"])
					require.Equal(t, wantClusterRoles, counts["ClusterRoleBinding"])
				})
			}
		})
	}
}

func TestStripOnlyOpenShiftClusterRoleRules(t *testing.T) {
	rules := []rbacv1.PolicyRule{
		{APIGroups: []string{"config.openshift.io"}, Resources: []string{"clusterversions"}, Verbs: []string{"get"}},
		{APIGroups: []string{"", "security.openshift.io"}, Resources: []string{"pods"}, Verbs: []string{"get"}},
		{APIGroups: []string{"apps"}, Resources: []string{"daemonsets"}, Verbs: []string{"get"}},
	}
	filtered := withoutOpenShiftAPIGroups(rules)
	require.Len(t, filtered, 2)
	require.Equal(t, []string{""}, filtered[0].APIGroups)
	require.Equal(t, []string{"apps"}, filtered[1].APIGroups)
	require.Equal(t, []string{"", "security.openshift.io"}, rules[1].APIGroups, "input rules must not be mutated")
}

// Check the real loader, not just the helper: the nvidia-driver ClusterRole
// combines core Kubernetes permissions with OpenShift-specific ones.
func TestDriverRBACFollowsDetectedPlatform(t *testing.T) {
	for _, tc := range []struct {
		name              string
		openshiftVersion  string
		wantOpenShiftRule bool
	}{
		{name: "Kubernetes"},
		{name: "OpenShift", openshiftVersion: "4.18", wantOpenShiftRule: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ctrl := &ClusterPolicyController{logger: logr.Discard(), openshift: tc.openshiftVersion}
			resources, _ := addResourcesControls(ctrl, "../assets/state-driver")
			require.Equal(t, "nvidia-driver", resources.ClusterRole.Name)
			require.Equal(t, "nvidia-driver", resources.ClusterRoleBinding.Name)
			require.Equal(t, tc.wantOpenShiftRule, resources.Role.Name != "")
			require.Equal(t, tc.wantOpenShiftRule, resources.RoleBinding.Name != "")

			foundNodes := false
			foundOpenShift := false
			for _, rule := range resources.ClusterRole.Rules {
				for _, group := range rule.APIGroups {
					if group == "" {
						foundNodes = true
					}
					if group == "config.openshift.io" {
						foundOpenShift = true
					}
				}
			}
			require.True(t, foundNodes, "common node permissions must be kept on both platforms")
			require.Equal(t, tc.wantOpenShiftRule, foundOpenShift)
		})
	}
}

// A parent directory containing "openshift" is not an OpenShift-only asset.
func TestOpenShiftAssetFilterUsesFileName(t *testing.T) {
	path := filepath.Join(t.TempDir(), "openshift-test-dir")
	require.NoError(t, os.Mkdir(path, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(path, "0100_service_account.yaml"),
		[]byte("apiVersion: v1\nkind: ServiceAccount\nmetadata:\n  name: demo\n"), 0o600))
	require.NoError(t, os.WriteFile(filepath.Join(path, "0200_role.openshift.yaml"),
		[]byte("apiVersion: rbac.authorization.k8s.io/v1\nkind: Role\nmetadata:\n  name: ocp\n"), 0o600))
	ctrl := &ClusterPolicyController{logger: logr.Discard()}
	require.Len(t, getAssetsFrom(ctrl, path, ""), 1)
	require.Len(t, getAssetsFrom(ctrl, path, "4.18"), 2)
}
