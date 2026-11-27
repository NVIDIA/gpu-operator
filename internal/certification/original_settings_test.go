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

package certification

import (
	"context"
	"fmt"
	"sort"
	"testing"

	"github.com/stretchr/testify/require"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	utiljson "k8s.io/apimachinery/pkg/util/json"
)

// fakeCRClient keeps the CRs in memory and applies the "replace /spec" JSON
// patches that the restore sends. Unlike the API server it applies no CRD
// defaults, so a field left out of a restored spec stays missing.
type fakeCRClient struct {
	clusterPolicy *unstructured.Unstructured
	drivers       map[string]*unstructured.Unstructured
}

func (f *fakeCRClient) GetClusterPolicyUnstructured(context.Context) (*unstructured.Unstructured, error) {
	return f.clusterPolicy.DeepCopy(), nil
}

func (f *fakeCRClient) ListNVIDIADriversUnstructured(context.Context) ([]unstructured.Unstructured, error) {
	names := make([]string, 0, len(f.drivers))
	for name := range f.drivers {
		names = append(names, name)
	}
	sort.Strings(names)

	items := make([]unstructured.Unstructured, 0, len(names))
	for _, name := range names {
		items = append(items, *f.drivers[name].DeepCopy())
	}
	return items, nil
}

func (f *fakeCRClient) ModifyClusterPolicy(_ context.Context, patchJSON []byte) error {
	return replaceSpec(f.clusterPolicy, patchJSON)
}

func (f *fakeCRClient) ModifyNVIDIADriver(_ context.Context, name string, patchJSON []byte) error {
	obj, ok := f.drivers[name]
	if !ok {
		return fmt.Errorf("NVIDIADriver %q not found", name)
	}
	return replaceSpec(obj, patchJSON)
}

// replaceSpec applies a JSON patch made of a single "replace /spec" operation,
// decoding numbers the way the API server returns them (int64 for integers).
func replaceSpec(obj *unstructured.Unstructured, patchJSON []byte) error {
	var ops []struct {
		Op    string         `json:"op"`
		Path  string         `json:"path"`
		Value map[string]any `json:"value"`
	}
	if err := utiljson.Unmarshal(patchJSON, &ops); err != nil {
		return err
	}
	if len(ops) != 1 || ops[0].Op != "replace" || ops[0].Path != "/spec" {
		return fmt.Errorf("unexpected patch %s", patchJSON)
	}
	obj.Object["spec"] = ops[0].Value
	return nil
}

// The restore has to send back the specs the API server returned. Re-encoding
// the typed specs drops explicit zero values from omitempty fields, and the API
// server then applies the CRD defaults: a user's autoUpgrade: false comes back
// as true, and maxParallelUpgrades: 0 as 1.
func TestRestoreKeepsExplicitZeroValues(t *testing.T) {
	ctx := context.Background()
	c := &fakeCRClient{
		clusterPolicy: &unstructured.Unstructured{Object: map[string]any{
			"apiVersion": "nvidia.com/v1",
			"kind":       "ClusterPolicy",
			"metadata":   map[string]any{"name": "cluster-policy"},
			"spec": map[string]any{
				"driver": map[string]any{
					"version": "620.70.06",
					"upgradePolicy": map[string]any{
						"autoUpgrade":         true,
						"maxParallelUpgrades": int64(0),
						"drain": map[string]any{
							"enable":         true,
							"timeoutSeconds": int64(0),
						},
					},
				},
			},
		}},
		drivers: map[string]*unstructured.Unstructured{
			"default": {Object: map[string]any{
				"apiVersion": "nvidia.com/v1alpha1",
				"kind":       "NVIDIADriver",
				"metadata":   map[string]any{"name": "default"},
				"spec": map[string]any{
					"version": "620.70.06",
					"upgradePolicy": map[string]any{
						"autoUpgrade":         false,
						"maxParallelUpgrades": int64(0),
					},
				},
			}},
		},
	}

	s, err := captureSettings(ctx, c)
	require.NoError(t, err)

	// A TestSet changes the driver version in both CRs.
	require.NoError(t, unstructured.SetNestedField(c.clusterPolicy.Object, "999.0.0", "spec", "driver", "version"))
	require.NoError(t, unstructured.SetNestedField(c.drivers["default"].Object, "999.0.0", "spec", "version"))

	require.True(t, s.restoreClusterPolicy(ctx, c))
	require.True(t, s.restoreNVIDIADrivers(ctx, c))

	for name, tc := range map[string]struct {
		obj  *unstructured.Unstructured
		path []string
		want any
	}{
		"ClusterPolicy driver version": {
			obj: c.clusterPolicy, path: []string{"spec", "driver", "version"}, want: "620.70.06",
		},
		"ClusterPolicy maxParallelUpgrades": {
			obj: c.clusterPolicy, path: []string{"spec", "driver", "upgradePolicy", "maxParallelUpgrades"}, want: int64(0),
		},
		"ClusterPolicy drain timeoutSeconds": {
			obj: c.clusterPolicy, path: []string{"spec", "driver", "upgradePolicy", "drain", "timeoutSeconds"}, want: int64(0),
		},
		"NVIDIADriver version": {
			obj: c.drivers["default"], path: []string{"spec", "version"}, want: "620.70.06",
		},
		"NVIDIADriver autoUpgrade": {
			obj: c.drivers["default"], path: []string{"spec", "upgradePolicy", "autoUpgrade"}, want: false,
		},
		"NVIDIADriver maxParallelUpgrades": {
			obj: c.drivers["default"], path: []string{"spec", "upgradePolicy", "maxParallelUpgrades"}, want: int64(0),
		},
	} {
		t.Run(name, func(t *testing.T) {
			got, found, err := unstructured.NestedFieldNoCopy(tc.obj.Object, tc.path...)
			require.NoError(t, err)
			require.True(t, found, "%v is missing from the restored spec", tc.path)
			require.Equal(t, tc.want, got)
		})
	}

	// Once restored, the specs match the captured ones, so nothing is patched again.
	require.False(t, s.restoreClusterPolicy(ctx, c))
	require.False(t, s.restoreNVIDIADrivers(ctx, c))
}
