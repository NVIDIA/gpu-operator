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
	"fmt"
	"testing"

	upgrade_v1alpha1 "github.com/NVIDIA/k8s-operator-libs/api/upgrade/v1alpha1"
	"github.com/NVIDIA/k8s-operator-libs/pkg/upgrade"
	"github.com/stretchr/testify/assert"
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

func TestUpgradeNodeLabelsChanged(t *testing.T) {
	tests := []struct {
		name      string
		oldLabels map[string]string
		newLabels map[string]string
		expected  bool
	}{
		{
			name:      "upgrade state changes",
			oldLabels: map[string]string{upgrade.GetUpgradeStateLabelKey(): upgrade.UpgradeStateUpgradeRequired},
			newLabels: map[string]string{upgrade.GetUpgradeStateLabelKey(): upgrade.UpgradeStateDone},
			expected:  true,
		},
		{
			name:      "skip label changes",
			oldLabels: map[string]string{upgrade.GetUpgradeSkipNodeLabelKey(): "false"},
			newLabels: map[string]string{upgrade.GetUpgradeSkipNodeLabelKey(): "true"},
			expected:  true,
		},
		{
			name:      "unrelated label changes",
			oldLabels: map[string]string{"example.com/label": "old"},
			newLabels: map[string]string{"example.com/label": "new"},
			expected:  false,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			assert.Equal(t, tc.expected, upgradeNodeLabelsChanged(tc.oldLabels, tc.newLabels))
		})
	}
}
