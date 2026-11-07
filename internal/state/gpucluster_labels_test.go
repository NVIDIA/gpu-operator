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
	"fmt"
	"maps"
	"testing"

	"github.com/stretchr/testify/require"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/runtime"

	nvidiav1 "github.com/NVIDIA/gpu-operator/api/nvidia/v1"
	nvidiav1alpha1 "github.com/NVIDIA/gpu-operator/api/nvidia/v1alpha1"
)

func TestGPUClusterDaemonSetLabels(t *testing.T) {
	type manifestState interface {
		getManifestObjects(context.Context, *nvidiav1alpha1.GPUCluster, InfoCatalog) ([]*unstructured.Unstructured, error)
	}
	states := map[string]struct {
		state          manifestState
		selector       map[string]string
		fixedComponent string
	}{
		"dcgm": {
			state:          newTestDCGMState(t),
			selector:       map[string]string{"app": "nvidia-dcgm-dra"},
			fixedComponent: "nvidia-dcgm",
		},
		"dcgm exporter": {
			state:          newTestDCGMExporterState(t, false),
			selector:       map[string]string{"app": "nvidia-dcgm-exporter-dra"},
			fixedComponent: "nvidia-dcgm-exporter",
		},
		"dra driver": {
			state:    newTestDRAState(t),
			selector: map[string]string{"app": "nvidia-dra-driver-kubelet-plugin"},
		},
		"dra validator": {
			state:    newTestDRAValidationState(t),
			selector: map[string]string{"app.kubernetes.io/name": "nvidia-dra-validator"},
		},
	}
	for operand, state := range states {
		t.Run(operand, func(t *testing.T) {
			cr := sampleGPUCluster()
			cr.Spec.DCGM = &nvidiav1.DCGMSpec{Enabled: new(true)}
			cr.Spec.DCGMExporter = &nvidiav1.DCGMExporterSpec{}
			objs, err := state.state.getManifestObjects(context.Background(), cr, draSupportedCatalog())
			require.NoError(t, err)
			baseline := findDaemonSet(t, objs)
			require.Equal(t, &metav1.LabelSelector{MatchLabels: state.selector}, baseline.Spec.Selector)

			// Collide with every existing template and selector key, including keys
			// introduced by future operands, rather than testing only the known bug.
			collisions := map[string]string{
				"app":                       "custom",
				"app.kubernetes.io/part-of": "custom",
				"team":                      "platform",
			}
			for key := range baseline.Spec.Template.Labels {
				collisions[key] = "custom"
			}
			for key := range baseline.Spec.Selector.MatchLabels {
				collisions[key] = "custom"
			}
			for _, requirement := range baseline.Spec.Selector.MatchExpressions {
				collisions[requirement.Key] = "custom"
			}
			arbitrary := maps.Clone(collisions)
			for i := range 32 {
				arbitrary[fmt.Sprintf("example.com/label-%d", i)] = fmt.Sprintf("value-%d", i)
			}
			tests := map[string]map[string]string{
				"default":                            nil,
				"custom label":                       {"team": "platform"},
				"name label collision":               {"app.kubernetes.io/name": "gpu-platform"},
				"upgrade controller label collision": {"app": "gpu-platform"},
				"part-of label":                      {"app.kubernetes.io/part-of": "gpu-platform"},
				"component label collision":          {AppComponentLabelKey: "custom-component"},
				"all label collisions":               collisions,
				"arbitrary common labels":            arbitrary,
			}
			for name, commonLabels := range tests {
				t.Run(name, func(t *testing.T) {
					cr := cr.DeepCopy()
					cr.Spec.Daemonsets.Labels = maps.Clone(commonLabels)
					objs, err := state.state.getManifestObjects(context.Background(), cr, draSupportedCatalog())
					require.NoError(t, err)
					ds := findDaemonSet(t, objs)

					// Preserve the immutable selector itself as well as its agreement
					// with the pod template. Common labels must not mutate the CR.
					require.Equal(t, baseline.Spec.Selector, ds.Spec.Selector)
					selector, err := metav1.LabelSelectorAsSelector(ds.Spec.Selector)
					require.NoError(t, err)
					require.True(t, selector.Matches(labels.Set(ds.Spec.Template.Labels)))
					require.Equal(t, commonLabels, cr.Spec.Daemonsets.Labels)
					require.Equal(t, baseline.Spec.Template.Labels["app"], ds.Spec.Template.Labels["app"])
					require.NotContains(t, ds.Spec.Template.Labels, "app.kubernetes.io/part-of")
					if state.fixedComponent != "" {
						require.Equal(t, state.fixedComponent, ds.Labels[AppComponentLabelKey])
						require.Equal(t, state.fixedComponent, ds.Spec.Template.Labels[AppComponentLabelKey])
					}
					for key, value := range commonLabels {
						// DCGM component labels retain the upstream template exclusion;
						// other DaemonSet metadata remains customizable.
						if key == AppComponentLabelKey && state.fixedComponent != "" {
							continue
						}
						require.Equal(t, value, ds.Labels[key])
						_, reserved := ds.Spec.Selector.MatchLabels[key]
						for _, requirement := range ds.Spec.Selector.MatchExpressions {
							reserved = reserved || requirement.Key == key
						}
						if !reserved && key != "app" && key != "app.kubernetes.io/part-of" {
							require.Equal(t, value, ds.Spec.Template.Labels[key])
						}
					}
				})
			}
		})
	}
}

func TestGPUClusterDaemonSetLabelSelectorExpressions(t *testing.T) {
	ds := &appsv1.DaemonSet{
		TypeMeta:   metav1.TypeMeta{APIVersion: "apps/v1", Kind: "DaemonSet"},
		ObjectMeta: metav1.ObjectMeta{Name: "future-operand"},
		Spec: appsv1.DaemonSetSpec{
			Selector: &metav1.LabelSelector{
				MatchLabels: map[string]string{"example.com/operand": "future"},
				MatchExpressions: []metav1.LabelSelectorRequirement{
					{Key: "in", Operator: metav1.LabelSelectorOpIn, Values: []string{"original"}},
					{Key: "not-in", Operator: metav1.LabelSelectorOpNotIn, Values: []string{"custom"}},
					{Key: "exists", Operator: metav1.LabelSelectorOpExists},
					{Key: "absent", Operator: metav1.LabelSelectorOpDoesNotExist},
				},
			},
			Template: corev1.PodTemplateSpec{ObjectMeta: metav1.ObjectMeta{Labels: map[string]string{
				"example.com/operand": "future",
				"in":                  "original",
				"not-in":              "original",
				"exists":              "original",
				"component":           "original",
			}}},
		},
	}
	object, err := runtime.DefaultUnstructuredConverter.ToUnstructured(ds)
	require.NoError(t, err)
	obj := &unstructured.Unstructured{Object: object}
	commonLabels := map[string]string{
		"example.com/operand": "custom",
		"in":                  "custom",
		"not-in":              "custom",
		"exists":              "custom",
		"absent":              "custom",
		"component":           "custom",
		"team":                "platform",
	}
	untouched := &unstructured.Unstructured{Object: map[string]any{
		"apiVersion": "v1",
		"kind":       "ConfigMap",
		"metadata":   map[string]any{"name": "untouched"},
	}}
	untouchedBefore := untouched.DeepCopy()
	require.NoError(t, applyGPUClusterDaemonSetLabels([]*unstructured.Unstructured{obj, untouched}, commonLabels))
	actual := findDaemonSet(t, []*unstructured.Unstructured{obj})
	require.Equal(t, ds.Spec.Selector, actual.Spec.Selector)
	expectedLabels := maps.Clone(ds.Spec.Template.Labels)
	expectedLabels["component"] = "custom"
	expectedLabels["team"] = "platform"
	require.Equal(t, expectedLabels, actual.Spec.Template.Labels)
	selector, err := metav1.LabelSelectorAsSelector(actual.Spec.Selector)
	require.NoError(t, err)
	require.True(t, selector.Matches(labels.Set(actual.Spec.Template.Labels)))
	require.Equal(t, untouchedBefore, untouched)
}
