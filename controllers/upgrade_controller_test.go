/*
 * Copyright (c) NVIDIA CORPORATION.  All rights reserved.
 *
 * Licensed under the Apache License, Version 2.0 (the "License");
 * you may not use this file except in compliance with the License.
 * You may obtain a copy of the License at
 *
 *     http://www.apache.org/licenses/LICENSE-2.0
 *
 * Unless required by applicable law or agreed to in writing, software
 * distributed under the License is distributed on an "AS IS" BASIS,
 * WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
 * See the License for the specific language governing permissions and
 * limitations under the License.
 */

package controllers

import (
	"context"
	"fmt"
	"testing"

	upgrade_v1alpha1 "github.com/NVIDIA/k8s-operator-libs/api/upgrade/v1alpha1"
	"github.com/NVIDIA/k8s-operator-libs/pkg/upgrade"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
)

func TestSetDrainSpecPodSelector(t *testing.T) {
	tests := []struct {
		name             string
		drainSpec        *upgrade_v1alpha1.DrainSpec
		expectedSelector string
	}{
		{
			name:             "nil DrainSpec should be initialized with default PodSelector",
			drainSpec:        nil,
			expectedSelector: UpgradeSkipDrainLabelSelector,
		},
		{
			name:             "empty PodSelector should be set to default",
			drainSpec:        &upgrade_v1alpha1.DrainSpec{},
			expectedSelector: UpgradeSkipDrainLabelSelector,
		},
		{
			name:             "existing PodSelector should be appended",
			drainSpec:        &upgrade_v1alpha1.DrainSpec{PodSelector: "app=myapp"},
			expectedSelector: fmt.Sprintf("app=myapp,%s", UpgradeSkipDrainLabelSelector),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			upgradePolicy := &upgrade_v1alpha1.DriverUpgradePolicySpec{
				AutoUpgrade: true,
				DrainSpec:   tt.drainSpec,
			}

			if upgradePolicy.DrainSpec == nil {
				upgradePolicy.DrainSpec = &upgrade_v1alpha1.DrainSpec{}
			}
			if upgradePolicy.DrainSpec.PodSelector == "" {
				upgradePolicy.DrainSpec.PodSelector = UpgradeSkipDrainLabelSelector
			} else {
				upgradePolicy.DrainSpec.PodSelector =
					fmt.Sprintf("%s,%s", upgradePolicy.DrainSpec.PodSelector, UpgradeSkipDrainLabelSelector)
			}

			assert.NotNil(t, upgradePolicy.DrainSpec)
			assert.Equal(t, tt.expectedSelector, upgradePolicy.DrainSpec.PodSelector)
		})
	}
}

func TestClearStaleUpgradeLabels(t *testing.T) {
	upgradeStateLabel := upgrade.GetUpgradeStateLabelKey()
	tests := []struct {
		name          string
		nodeLabels    map[string]string
		daemonSets    []appsv1.DaemonSet
		expectRemoved bool
	}{
		{
			name:          "removes label from node excluded by driver node selector",
			nodeLabels:    map[string]string{upgradeStateLabel: "upgrade-required", "gpu": "true"},
			daemonSets:    []appsv1.DaemonSet{{ObjectMeta: metav1.ObjectMeta{Name: "driver", Namespace: "gpu-operator", Labels: map[string]string{DriverLabelKey: DriverLabelValue}}, Spec: appsv1.DaemonSetSpec{Template: corev1.PodTemplateSpec{Spec: corev1.PodSpec{NodeSelector: map[string]string{"gpu": "true", "type": "A100"}}}}}},
			expectRemoved: true,
		},
		{
			name:          "keeps label while a driver DaemonSet still targets node",
			nodeLabels:    map[string]string{upgradeStateLabel: "upgrade-required", "gpu": "true", "type": "A100"},
			daemonSets:    []appsv1.DaemonSet{{ObjectMeta: metav1.ObjectMeta{Name: "driver", Namespace: "gpu-operator", Labels: map[string]string{DriverLabelKey: DriverLabelValue}}, Spec: appsv1.DaemonSetSpec{Template: corev1.PodTemplateSpec{Spec: corev1.PodSpec{NodeSelector: map[string]string{"gpu": "true", "type": "A100"}}}}}},
			expectRemoved: false,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			scheme := runtime.NewScheme()
			require.NoError(t, corev1.AddToScheme(scheme))
			require.NoError(t, appsv1.AddToScheme(scheme))

			node := &corev1.Node{ObjectMeta: metav1.ObjectMeta{Name: "node-1", Labels: test.nodeLabels}}
			objects := []runtime.Object{node}
			for index := range test.daemonSets {
				objects = append(objects, &test.daemonSets[index])
			}
			reconciler := &UpgradeReconciler{Client: fake.NewClientBuilder().WithScheme(scheme).WithRuntimeObjects(objects...).Build()}

			err := reconciler.clearStaleUpgradeLabels(context.Background(), &upgrade.ClusterUpgradeState{}, map[string]string{DriverLabelKey: DriverLabelValue}, "gpu-operator")
			require.NoError(t, err)

			updatedNode := &corev1.Node{}
			require.NoError(t, reconciler.Get(context.Background(), client.ObjectKey{Name: node.Name}, updatedNode))
			if test.expectRemoved {
				assert.NotContains(t, updatedNode.Labels, upgradeStateLabel)
			} else {
				assert.Contains(t, updatedNode.Labels, upgradeStateLabel)
			}
		})
	}
}
