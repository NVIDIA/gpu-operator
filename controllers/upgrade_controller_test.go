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
	"github.com/go-logr/logr"
	"github.com/stretchr/testify/assert"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"

	driverconfig "github.com/NVIDIA/gpu-operator/internal/config"
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

func TestDriverPodRestartOnly(t *testing.T) {
	driverPod := func(digest string) *corev1.Pod {
		return &corev1.Pod{Spec: corev1.PodSpec{Containers: []corev1.Container{{
			Name: "nvidia-driver-ctr",
			Env:  []corev1.EnvVar{{Name: driverconfig.DriverConfigDigestEnvName, Value: digest}},
		}}}}
	}
	driverDS := func(digest string) *appsv1.DaemonSet {
		return &appsv1.DaemonSet{Spec: appsv1.DaemonSetSpec{Template: corev1.PodTemplateSpec{
			Spec: corev1.PodSpec{Containers: []corev1.Container{{
				Name: "nvidia-driver-ctr",
				Env:  []corev1.EnvVar{{Name: driverconfig.DriverConfigDigestEnvName, Value: digest}},
			}}},
		}}}
	}

	r := &UpgradeReconciler{Log: logr.Discard()}
	ctx := context.Background()

	tests := []struct {
		name        string
		pod         *corev1.Pod
		ds          *appsv1.DaemonSet
		wantRestart bool
	}{
		{name: "equal digests -> restart-only", pod: driverPod("same"), ds: driverDS("same"), wantRestart: true},
		{name: "differing digests -> full upgrade", pod: driverPod("old"), ds: driverDS("new"), wantRestart: false},
		{name: "missing digest on pod -> full upgrade", pod: driverPod(""), ds: driverDS("new"), wantRestart: false},
		{name: "missing digest on daemonset -> full upgrade", pod: driverPod("old"), ds: driverDS(""), wantRestart: false},
		{name: "nil pod -> full upgrade", pod: nil, ds: driverDS("x"), wantRestart: false},
		{name: "nil daemonset -> full upgrade", pod: driverPod("x"), ds: nil, wantRestart: false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := r.driverPodRestartOnly(ctx, tt.pod, tt.ds)
			assert.NoError(t, err)
			assert.Equal(t, tt.wantRestart, got)
		})
	}
}
