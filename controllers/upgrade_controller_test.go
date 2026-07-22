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

	"github.com/go-logr/logr"
	promcli "github.com/prometheus/client_golang/prometheus"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	upgrade_v1alpha1 "github.com/NVIDIA/k8s-operator-libs/api/upgrade/v1alpha1"
	"github.com/NVIDIA/k8s-operator-libs/pkg/upgrade"

	gpuv1 "github.com/NVIDIA/gpu-operator/api/nvidia/v1"
	nvidiav1alpha1 "github.com/NVIDIA/gpu-operator/api/nvidia/v1alpha1"
	gpuconsts "github.com/NVIDIA/gpu-operator/internal/consts"
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

// fakeUpgradeStateManager records BuildState/ApplyState calls and serves a canned state.
type fakeUpgradeStateManager struct {
	state           upgrade.ClusterUpgradeState
	buildCalls      int
	buildNamespace  string
	buildLabels     map[string]string
	applyCalls      int
	appliedPolicies []*upgrade_v1alpha1.DriverUpgradePolicySpec
}

func (f *fakeUpgradeStateManager) GetTotalManagedNodes(*upgrade.ClusterUpgradeState) int { return 0 }
func (f *fakeUpgradeStateManager) GetUpgradesInProgress(*upgrade.ClusterUpgradeState) int {
	return 0
}
func (f *fakeUpgradeStateManager) GetUpgradesDone(*upgrade.ClusterUpgradeState) int { return 0 }
func (f *fakeUpgradeStateManager) GetUpgradesAvailable(*upgrade.ClusterUpgradeState, int, int) int {
	return 0
}
func (f *fakeUpgradeStateManager) GetUpgradesFailed(*upgrade.ClusterUpgradeState) int  { return 0 }
func (f *fakeUpgradeStateManager) GetUpgradesPending(*upgrade.ClusterUpgradeState) int { return 0 }
func (f *fakeUpgradeStateManager) IsPodDeletionEnabled() bool                          { return false }
func (f *fakeUpgradeStateManager) IsValidationEnabled() bool                           { return false }

func (f *fakeUpgradeStateManager) WithPodDeletionEnabled(upgrade.PodDeletionFilter) upgrade.ClusterUpgradeStateManager {
	return f
}

func (f *fakeUpgradeStateManager) WithValidationEnabled(string) upgrade.ClusterUpgradeStateManager {
	return f
}

func (f *fakeUpgradeStateManager) WithRestartOnlyPredicate(upgrade.RestartOnlyPredicate) upgrade.ClusterUpgradeStateManager {
	return f
}

func (f *fakeUpgradeStateManager) BuildState(_ context.Context, namespace string, driverLabels map[string]string) (*upgrade.ClusterUpgradeState, error) {
	f.buildCalls++
	f.buildNamespace = namespace
	f.buildLabels = driverLabels
	return &f.state, nil
}

func (f *fakeUpgradeStateManager) ApplyState(_ context.Context, _ *upgrade.ClusterUpgradeState, upgradePolicy *upgrade_v1alpha1.DriverUpgradePolicySpec) error {
	f.applyCalls++
	f.appliedPolicies = append(f.appliedPolicies, upgradePolicy)
	return nil
}

// newTestOperatorMetrics builds the gauges the upgrade reconciler touches without
// registering them; InitOperatorMetrics panics when registered twice per binary.
func newTestOperatorMetrics() *OperatorMetrics {
	newGauge := func(name string) promcli.Gauge {
		return promcli.NewGauge(promcli.GaugeOpts{Name: name})
	}
	return &OperatorMetrics{
		reconciliationStatus:     newGauge("test_reconciliation_status"),
		driverAutoUpgradeEnabled: newGauge("test_driver_auto_upgrade_enabled"),
		upgradesInProgress:       newGauge("test_upgrades_in_progress"),
		upgradesDone:             newGauge("test_upgrades_done"),
		upgradesAvailable:        newGauge("test_upgrades_available"),
		upgradesFailed:           newGauge("test_upgrades_failed"),
		upgradesPending:          newGauge("test_upgrades_pending"),
	}
}

const testOperatorNamespace = "test-operator-namespace"

func newTestUpgradeReconciler(t *testing.T, objs ...client.Object) (*UpgradeReconciler, *fakeUpgradeStateManager) {
	t.Helper()
	upgrade.SetDriverName("gpu")

	scheme := runtime.NewScheme()
	require.NoError(t, gpuv1.AddToScheme(scheme))
	require.NoError(t, nvidiav1alpha1.AddToScheme(scheme))
	require.NoError(t, corev1.AddToScheme(scheme))

	stateManager := &fakeUpgradeStateManager{state: upgrade.NewClusterUpgradeState()}
	r := &UpgradeReconciler{
		Client:            fake.NewClientBuilder().WithScheme(scheme).WithObjects(objs...).Build(),
		Log:               logr.Discard(),
		Scheme:            scheme,
		StateManager:      stateManager,
		OperatorMetrics:   newTestOperatorMetrics(),
		OperatorNamespace: testOperatorNamespace,
	}
	return r, stateManager
}

func upgradeSingletonRequest() ctrl.Request {
	return ctrl.Request{NamespacedName: types.NamespacedName{Name: upgradeControllerSingletonName}}
}

func nodeWithUpgradeState(name, owner string) *corev1.Node {
	node := &corev1.Node{ObjectMeta: metav1.ObjectMeta{
		Name:   name,
		Labels: map[string]string{upgrade.GetUpgradeStateLabelKey(): "upgrade-required"},
	}}
	if owner != "" {
		node.Labels[gpuconsts.NVIDIADriverOwnerLabel] = owner
	}
	return node
}

// TestUpgradeReconcileWithoutConfigSources covers the preinstalled-driver scenario: no
// ClusterPolicy and no NVIDIADriver CRs must leave the cluster untouched apart from
// clearing stale upgrade-state labels.
func TestUpgradeReconcileWithoutConfigSources(t *testing.T) {
	tests := []struct {
		name       string
		gpuCluster *nvidiav1alpha1.GPUCluster
	}{
		{name: "with a GPUCluster", gpuCluster: &nvidiav1alpha1.GPUCluster{ObjectMeta: metav1.ObjectMeta{Name: "gpu-cluster"}}},
		{name: "without a GPUCluster"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			node := nodeWithUpgradeState("node-1", "")
			objs := []client.Object{node}
			if tt.gpuCluster != nil {
				objs = append(objs, tt.gpuCluster)
			}
			r, stateManager := newTestUpgradeReconciler(t, objs...)

			result, err := r.Reconcile(context.Background(), upgradeSingletonRequest())
			require.NoError(t, err)
			assert.Equal(t, ctrl.Result{}, result)
			assert.Zero(t, stateManager.buildCalls)
			assert.Zero(t, stateManager.applyCalls)

			// The stale upgrade-state label is stripped; no CR by design is not an error.
			updated := &corev1.Node{}
			require.NoError(t, r.Get(context.Background(), types.NamespacedName{Name: node.Name}, updated))
			assert.NotContains(t, updated.Labels, upgrade.GetUpgradeStateLabelKey())
		})
	}
}

func TestUpgradeReconcileNVIDIADriverWithoutClusterPolicy(t *testing.T) {
	nvd := &nvidiav1alpha1.NVIDIADriver{ObjectMeta: metav1.ObjectMeta{Name: "gpu-driver"}}
	node := nodeWithUpgradeState("node-1", nvd.Name)
	r, stateManager := newTestUpgradeReconciler(t, nvd, node)
	stateManager.state.NodeStates["upgrade-required"] = []*upgrade.NodeUpgradeState{{Node: node}}

	result, err := r.Reconcile(context.Background(), upgradeSingletonRequest())
	require.NoError(t, err)
	assert.Equal(t, plannedRequeueInterval, result.RequeueAfter)

	assert.Equal(t, 1, stateManager.buildCalls)
	assert.Equal(t, testOperatorNamespace, stateManager.buildNamespace)
	assert.Equal(t, map[string]string{AppComponentLabelKey: DriverAppComponentLabelValue}, stateManager.buildLabels)

	require.Len(t, stateManager.appliedPolicies, 1)
	assert.True(t, stateManager.appliedPolicies[0].AutoUpgrade, "nil upgradePolicy must default to autoUpgrade enabled")
}

func TestUpgradeReconcileNVIDIADriverAutoUpgradeDisabledScope(t *testing.T) {
	enabled := &nvidiav1alpha1.NVIDIADriver{ObjectMeta: metav1.ObjectMeta{Name: "enabled-driver"}}
	disabled := &nvidiav1alpha1.NVIDIADriver{
		ObjectMeta: metav1.ObjectMeta{Name: "disabled-driver"},
		Spec: nvidiav1alpha1.NVIDIADriverSpec{
			UpgradePolicy: &nvidiav1alpha1.DriverUpgradePolicySpec{AutoUpgrade: false},
		},
	}
	enabledNode := nodeWithUpgradeState("node-enabled", enabled.Name)
	disabledNode := nodeWithUpgradeState("node-disabled", disabled.Name)
	r, stateManager := newTestUpgradeReconciler(t, enabled, disabled, enabledNode, disabledNode)
	stateManager.state.NodeStates["upgrade-required"] = []*upgrade.NodeUpgradeState{{Node: enabledNode}, {Node: disabledNode}}

	_, err := r.Reconcile(context.Background(), upgradeSingletonRequest())
	require.NoError(t, err)

	// Cleanup is scoped to the disabled CR's nodes; the enabled CR still gets ApplyState.
	assert.Equal(t, 1, stateManager.applyCalls)
	updated := &corev1.Node{}
	require.NoError(t, r.Get(context.Background(), types.NamespacedName{Name: disabledNode.Name}, updated))
	assert.NotContains(t, updated.Labels, upgrade.GetUpgradeStateLabelKey())
	require.NoError(t, r.Get(context.Background(), types.NamespacedName{Name: enabledNode.Name}, updated))
	assert.Contains(t, updated.Labels, upgrade.GetUpgradeStateLabelKey())
}

func TestUpgradeReconcileClusterPolicyPathsUnchanged(t *testing.T) {
	t.Run("legacy driver builds state with the daemonset app label", func(t *testing.T) {
		cp := &gpuv1.ClusterPolicy{ObjectMeta: metav1.ObjectMeta{Name: "cluster-policy"}}
		cp.Spec.Driver.UpgradePolicy = &upgrade_v1alpha1.DriverUpgradePolicySpec{AutoUpgrade: true}
		r, stateManager := newTestUpgradeReconciler(t, cp)

		result, err := r.Reconcile(context.Background(), upgradeSingletonRequest())
		require.NoError(t, err)
		assert.Equal(t, plannedRequeueInterval, result.RequeueAfter)
		assert.Equal(t, map[string]string{DriverLabelKey: DriverLabelValue}, stateManager.buildLabels)
		assert.Equal(t, 1, stateManager.applyCalls)
	})

	t.Run("sandbox workloads clean up and skip", func(t *testing.T) {
		cp := &gpuv1.ClusterPolicy{ObjectMeta: metav1.ObjectMeta{Name: "cluster-policy"}}
		enabled := true
		cp.Spec.SandboxWorkloads.Enabled = &enabled
		node := nodeWithUpgradeState("node-1", "")
		r, stateManager := newTestUpgradeReconciler(t, cp, node)

		result, err := r.Reconcile(context.Background(), upgradeSingletonRequest())
		require.NoError(t, err)
		assert.Equal(t, ctrl.Result{}, result)
		assert.Zero(t, stateManager.buildCalls)

		updated := &corev1.Node{}
		require.NoError(t, r.Get(context.Background(), types.NamespacedName{Name: node.Name}, updated))
		assert.NotContains(t, updated.Labels, upgrade.GetUpgradeStateLabelKey())
	})

	t.Run("useNvidiaDriverCRD takes the NVIDIADriver path", func(t *testing.T) {
		cp := &gpuv1.ClusterPolicy{ObjectMeta: metav1.ObjectMeta{Name: "cluster-policy"}}
		useCRD := true
		cp.Spec.Driver.UseNvidiaDriverCRD = &useCRD
		nvd := &nvidiav1alpha1.NVIDIADriver{ObjectMeta: metav1.ObjectMeta{Name: "gpu-driver"}}
		r, stateManager := newTestUpgradeReconciler(t, cp, nvd)

		_, err := r.Reconcile(context.Background(), upgradeSingletonRequest())
		require.NoError(t, err)
		assert.Equal(t, map[string]string{AppComponentLabelKey: DriverAppComponentLabelValue}, stateManager.buildLabels)
	})
}
