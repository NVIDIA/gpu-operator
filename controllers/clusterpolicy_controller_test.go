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
	"context"
	"testing"
	"time"

	"github.com/NVIDIA/k8s-operator-libs/pkg/upgrade"
	"github.com/go-logr/logr"
	promcli "github.com/prometheus/client_golang/prometheus"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/utils/ptr"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	gpuv1 "github.com/NVIDIA/gpu-operator/api/nvidia/v1"
	nvidiav1alpha1 "github.com/NVIDIA/gpu-operator/api/nvidia/v1alpha1"
)

func TestIsIncompleteDriverUpgradeState(t *testing.T) {
	tests := []struct {
		name     string
		state    string
		expected bool
	}{
		{
			name:     "unknown state is inactive",
			state:    upgrade.UpgradeStateUnknown,
			expected: false,
		},
		{
			name:     "upgrade required is pending and in progress",
			state:    upgrade.UpgradeStateUpgradeRequired,
			expected: true,
		},
		{
			name:     "done is inactive",
			state:    upgrade.UpgradeStateDone,
			expected: false,
		},
		{
			name:     "failed is incomplete",
			state:    upgrade.UpgradeStateFailed,
			expected: true,
		},
		{
			name:     "pod restart required is active",
			state:    upgrade.UpgradeStatePodRestartRequired,
			expected: true,
		},
		{
			name:     "uncordon required is active",
			state:    upgrade.UpgradeStateUncordonRequired,
			expected: true,
		},
		{
			name:     "unrecognized state is incomplete",
			state:    "new-state",
			expected: true,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			require.Equal(t, tc.expected, isIncompleteDriverUpgradeState(tc.state))
		})
	}
}

func TestClusterPolicyNotReadyMessage(t *testing.T) {
	tests := []struct {
		name            string
		statesNotReady  []string
		notReadyReasons []string
		expected        string
	}{
		{
			name: "driver upgrade only",
			notReadyReasons: []string{
				"NVIDIADriver upgrade has not completed",
				"one or more NVIDIADriver-owned Nodes are marked pending, in-progress, or failed",
			},
			expected: "ClusterPolicy is not ready; NVIDIADriver upgrade has not completed; one or more NVIDIADriver-owned Nodes are marked pending, in-progress, or failed",
		},
		{
			name:           "not ready states and driver upgrade",
			statesNotReady: []string{"state-container-toolkit", "state-device-plugin"},
			notReadyReasons: []string{
				"NVIDIADriver upgrade has not completed",
				"one or more NVIDIADriver-owned Nodes are marked pending, in-progress, or failed",
			},
			expected: "ClusterPolicy is not ready; states not ready: [state-container-toolkit state-device-plugin]; NVIDIADriver upgrade has not completed; one or more NVIDIADriver-owned Nodes are marked pending, in-progress, or failed",
		},
		{
			name:           "not ready states only",
			statesNotReady: []string{"state-container-toolkit"},
			expected:       "ClusterPolicy is not ready; states not ready: [state-container-toolkit]",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			require.Equal(t, tc.expected, clusterPolicyNotReadyMessage(tc.statesNotReady, tc.notReadyReasons))
		})
	}
}

func TestNVIDIADriverUpgradeIncomplete(t *testing.T) {
	upgradeStateLabel := upgrade.GetUpgradeStateLabelKey()

	tests := []struct {
		name     string
		nodes    []client.Object
		expected bool
	}{
		{
			name: "active upgrade state on NVIDIADriver-owned node",
			nodes: []client.Object{
				nodeWithLabels("gpu-node", map[string]string{
					nvidiav1alpha1.NVIDIADriverOwnerLabel: "default",
					upgradeStateLabel:                     upgrade.UpgradeStatePodRestartRequired,
				}),
			},
			expected: true,
		},
		{
			name: "pending upgrade keeps rollout in progress after another node completes",
			nodes: []client.Object{
				nodeWithLabels("upgraded-gpu-node", map[string]string{
					nvidiav1alpha1.NVIDIADriverOwnerLabel: "default",
					upgradeStateLabel:                     upgrade.UpgradeStateDone,
				}),
				nodeWithLabels("pending-gpu-node", map[string]string{
					nvidiav1alpha1.NVIDIADriverOwnerLabel: "default",
					upgradeStateLabel:                     upgrade.UpgradeStateUpgradeRequired,
				}),
			},
			expected: true,
		},
		{
			name: "active upgrade state on unowned node is ignored",
			nodes: []client.Object{
				nodeWithLabels("gpu-node", map[string]string{
					upgradeStateLabel: upgrade.UpgradeStatePodRestartRequired,
				}),
			},
			expected: false,
		},
		{
			name: "failed upgrade state keeps rollout incomplete",
			nodes: []client.Object{
				nodeWithLabels("gpu-node", map[string]string{
					nvidiav1alpha1.NVIDIADriverOwnerLabel: "default",
					upgradeStateLabel:                     upgrade.UpgradeStateFailed,
				}),
			},
			expected: true,
		},
		{
			name: "completed upgrade state is not treated as in progress",
			nodes: []client.Object{
				nodeWithLabels("gpu-node", map[string]string{
					nvidiav1alpha1.NVIDIADriverOwnerLabel: "default",
					upgradeStateLabel:                     upgrade.UpgradeStateDone,
				}),
			},
			expected: false,
		},
		{
			name: "skipped node is excluded from the upgrade aggregate",
			nodes: []client.Object{
				nodeWithLabels("skipped-gpu-node", map[string]string{
					nvidiav1alpha1.NVIDIADriverOwnerLabel: "default",
					upgradeStateLabel:                     upgrade.UpgradeStateUpgradeRequired,
					upgrade.GetUpgradeSkipNodeLabelKey():  "true",
				}),
			},
			expected: false,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			scheme := runtime.NewScheme()
			require.NoError(t, corev1.AddToScheme(scheme))
			reconciler := &ClusterPolicyReconciler{
				Client: fake.NewClientBuilder().WithScheme(scheme).WithObjects(tc.nodes...).Build(),
			}

			actual, err := reconciler.nvidiaDriverUpgradeIncomplete(context.Background())
			require.NoError(t, err)
			require.Equal(t, tc.expected, actual)
		})
	}
}

func nodeWithLabels(name string, labels map[string]string) *corev1.Node {
	return &corev1.Node{
		ObjectMeta: metav1.ObjectMeta{
			Name:   name,
			Labels: labels,
		},
	}
}

func TestDriverUpgradeLabelsChanged(t *testing.T) {
	upgradeStateLabel := upgrade.GetUpgradeStateLabelKey()

	tests := []struct {
		name                string
		oldLabels           map[string]string
		newLabels           map[string]string
		ownerChanged        bool
		upgradeStateChanged bool
		upgradeSkipChanged  bool
	}{
		{
			name:         "driver ownership changes",
			oldLabels:    map[string]string{nvidiav1alpha1.NVIDIADriverOwnerLabel: "old-driver"},
			newLabels:    map[string]string{nvidiav1alpha1.NVIDIADriverOwnerLabel: "new-driver"},
			ownerChanged: true,
		},
		{
			name:                "upgrade state changes",
			oldLabels:           map[string]string{upgradeStateLabel: upgrade.UpgradeStateUpgradeRequired},
			newLabels:           map[string]string{upgradeStateLabel: upgrade.UpgradeStateDone},
			upgradeStateChanged: true,
		},
		{
			name:               "upgrade skip label changes",
			oldLabels:          map[string]string{upgrade.GetUpgradeSkipNodeLabelKey(): "false"},
			newLabels:          map[string]string{upgrade.GetUpgradeSkipNodeLabelKey(): "true"},
			upgradeSkipChanged: true,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			ownerChanged, upgradeStateChanged, upgradeSkipChanged := driverUpgradeLabelsChanged(tc.oldLabels, tc.newLabels)
			require.Equal(t, tc.ownerChanged, ownerChanged)
			require.Equal(t, tc.upgradeStateChanged, upgradeStateChanged)
			require.Equal(t, tc.upgradeSkipChanged, upgradeSkipChanged)
		})
	}
}

func TestShouldReconcileClusterPolicyOnNodeDeletion(t *testing.T) {
	tests := []struct {
		name     string
		labels   map[string]string
		expected bool
	}{
		{
			name: "NVIDIADriver-owned node",
			labels: map[string]string{
				nvidiav1alpha1.NVIDIADriverOwnerLabel: "driver-a",
			},
			expected: true,
		},
		{
			name: "unrelated node",
			labels: map[string]string{
				"example.com/label": "value",
			},
			expected: false,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			require.Equal(t, tc.expected, shouldReconcileClusterPolicyOnNodeDeletion(tc.labels))
		})
	}
}

func TestClusterPolicyReconcileDriverUpgradeTransitions(t *testing.T) {
	upgradeStateLabel := upgrade.GetUpgradeStateLabelKey()
	cp := clusterPolicyForUpgradeTest(true)
	node := nodeWithLabels("gpu-node", map[string]string{
		nvidiav1alpha1.NVIDIADriverOwnerLabel: "driver-a",
		upgradeStateLabel:                     upgrade.UpgradeStateDone,
	})
	r, c, _ := newClusterPolicyUpgradeTestReconciler(t, cp, node)

	result, err := r.Reconcile(t.Context(), ctrl.Request{NamespacedName: client.ObjectKeyFromObject(cp)})
	require.NoError(t, err)
	require.Equal(t, gpuv1.Ready, clusterPolicyState(t, c, cp.Name))
	// No NFD labels are present in this focused test fixture, so Ready follows
	// the existing NFD polling path.
	require.Equal(t, 45*time.Second, result.RequeueAfter)

	require.NoError(t, c.Get(t.Context(), client.ObjectKeyFromObject(node), node))
	node.Labels[upgradeStateLabel] = upgrade.UpgradeStateUpgradeRequired
	require.NoError(t, c.Update(t.Context(), node))

	result, err = r.Reconcile(t.Context(), ctrl.Request{NamespacedName: client.ObjectKeyFromObject(cp)})
	require.NoError(t, err)
	require.Equal(t, gpuv1.NotReady, clusterPolicyState(t, c, cp.Name))
	require.Zero(t, result.RequeueAfter)

	require.NoError(t, c.Get(t.Context(), client.ObjectKeyFromObject(node), node))
	node.Labels[upgrade.GetUpgradeSkipNodeLabelKey()] = "true"
	require.NoError(t, c.Update(t.Context(), node))

	_, err = r.Reconcile(t.Context(), ctrl.Request{NamespacedName: client.ObjectKeyFromObject(cp)})
	require.NoError(t, err)
	require.Equal(t, gpuv1.Ready, clusterPolicyState(t, c, cp.Name))

	require.NoError(t, c.Get(t.Context(), client.ObjectKeyFromObject(node), node))
	delete(node.Labels, upgrade.GetUpgradeSkipNodeLabelKey())
	require.NoError(t, c.Update(t.Context(), node))

	result, err = r.Reconcile(t.Context(), ctrl.Request{NamespacedName: client.ObjectKeyFromObject(cp)})
	require.NoError(t, err)
	require.Equal(t, gpuv1.NotReady, clusterPolicyState(t, c, cp.Name))
	require.Zero(t, result.RequeueAfter)

	require.NoError(t, c.Get(t.Context(), client.ObjectKeyFromObject(node), node))
	node.Labels[upgradeStateLabel] = upgrade.UpgradeStateDone
	require.NoError(t, c.Update(t.Context(), node))

	_, err = r.Reconcile(t.Context(), ctrl.Request{NamespacedName: client.ObjectKeyFromObject(cp)})
	require.NoError(t, err)
	require.Equal(t, gpuv1.Ready, clusterPolicyState(t, c, cp.Name))
}

func TestClusterPolicyReconcileBecomesReadyAfterIncompleteNodeDeletion(t *testing.T) {
	upgradeStateLabel := upgrade.GetUpgradeStateLabelKey()
	cp := clusterPolicyForUpgradeTest(true)
	node := nodeWithLabels("failed-gpu-node", map[string]string{
		nvidiav1alpha1.NVIDIADriverOwnerLabel: "driver-a",
		upgradeStateLabel:                     upgrade.UpgradeStateFailed,
	})
	r, c, _ := newClusterPolicyUpgradeTestReconciler(t, cp, node)
	request := ctrl.Request{NamespacedName: client.ObjectKeyFromObject(cp)}

	result, err := r.Reconcile(t.Context(), request)
	require.NoError(t, err)
	require.Equal(t, gpuv1.NotReady, clusterPolicyState(t, c, cp.Name))
	require.Zero(t, result.RequeueAfter)

	require.NoError(t, c.Delete(t.Context(), node))
	_, err = r.Reconcile(t.Context(), request)
	require.NoError(t, err)
	require.Equal(t, gpuv1.Ready, clusterPolicyState(t, c, cp.Name))
}

func TestClusterPolicyReconcileDriverUpgradeFailureCases(t *testing.T) {
	upgradeStateLabel := upgrade.GetUpgradeStateLabelKey()

	t.Run("one failed driver among multiple drivers keeps ClusterPolicy not ready", func(t *testing.T) {
		cp := clusterPolicyForUpgradeTest(true)
		r, c, _ := newClusterPolicyUpgradeTestReconciler(t, cp,
			nodeWithLabels("completed", map[string]string{nvidiav1alpha1.NVIDIADriverOwnerLabel: "driver-a", upgradeStateLabel: upgrade.UpgradeStateDone}),
			nodeWithLabels("failed", map[string]string{nvidiav1alpha1.NVIDIADriverOwnerLabel: "driver-b", upgradeStateLabel: upgrade.UpgradeStateFailed}),
		)

		result, err := r.Reconcile(t.Context(), ctrl.Request{NamespacedName: client.ObjectKeyFromObject(cp)})
		require.NoError(t, err)
		require.Equal(t, gpuv1.NotReady, clusterPolicyState(t, c, cp.Name))
		require.Zero(t, result.RequeueAfter)
	})

	t.Run("legacy driver management ignores upgrade labels", func(t *testing.T) {
		cp := clusterPolicyForUpgradeTest(false)
		r, c, _ := newClusterPolicyUpgradeTestReconciler(t, cp,
			nodeWithLabels("failed", map[string]string{nvidiav1alpha1.NVIDIADriverOwnerLabel: "driver-a", upgradeStateLabel: upgrade.UpgradeStateFailed}),
		)

		_, err := r.Reconcile(t.Context(), ctrl.Request{NamespacedName: client.ObjectKeyFromObject(cp)})
		require.NoError(t, err)
		require.Equal(t, gpuv1.Ready, clusterPolicyState(t, c, cp.Name))
	})

	t.Run("terminal failure relies on Node events instead of polling", func(t *testing.T) {
		cp := clusterPolicyForUpgradeTest(true)
		calls := 0
		r, _, metrics := newClusterPolicyUpgradeTestReconciler(t, cp,
			nodeWithLabels("failed", map[string]string{nvidiav1alpha1.NVIDIADriverOwnerLabel: "driver-a", upgradeStateLabel: upgrade.UpgradeStateFailed}),
		)
		clusterPolicyCtrl.controls = []controlFunc{{func(ClusterPolicyController) (gpuv1.State, error) {
			calls++
			return gpuv1.Ready, nil
		}}}

		result, err := r.Reconcile(t.Context(), ctrl.Request{NamespacedName: client.ObjectKeyFromObject(cp)})
		require.NoError(t, err)
		require.Zero(t, result.RequeueAfter)
		require.Equal(t, 1, calls)
		require.Equal(t, 1, metrics.reconciliationFailed.(*countingCounter).increments)
	})
}

func clusterPolicyForUpgradeTest(useNvidiaDriverCRD bool) *gpuv1.ClusterPolicy {
	return &gpuv1.ClusterPolicy{
		ObjectMeta: metav1.ObjectMeta{Name: "cluster-policy"},
		Spec:       gpuv1.ClusterPolicySpec{Driver: gpuv1.DriverSpec{UseNvidiaDriverCRD: ptr.To(useNvidiaDriverCRD)}},
	}
}

// The ClusterPolicy with the oldest creationTimestamp is the singleton regardless of
// reconcile order; any other instance is skipped without running any states.
func TestClusterPolicyReconcileSkipsNonSingleton(t *testing.T) {
	older := clusterPolicyForUpgradeTest(true)
	older.Name = "older"
	older.CreationTimestamp = metav1.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	r, c, _ := newClusterPolicyUpgradeTestReconciler(t, older)

	newer := clusterPolicyForUpgradeTest(true)
	newer.Name = "newer"
	newer.CreationTimestamp = metav1.Date(2026, 1, 2, 0, 0, 0, 0, time.UTC)
	require.NoError(t, c.Create(t.Context(), newer))

	// The newer instance reconciles first but is not the singleton: no error, no
	// requeue, and no state is persisted since its states never run.
	result, err := r.Reconcile(t.Context(), ctrl.Request{NamespacedName: client.ObjectKeyFromObject(newer)})
	require.NoError(t, err)
	require.Zero(t, result)
	require.Empty(t, clusterPolicyState(t, c, newer.Name))

	_, err = r.Reconcile(t.Context(), ctrl.Request{NamespacedName: client.ObjectKeyFromObject(older)})
	require.NoError(t, err)
	require.Equal(t, gpuv1.Ready, clusterPolicyState(t, c, older.Name))
}

func TestClusterPolicyBlockedByGPUCluster(t *testing.T) {
	cp := clusterPolicyForUpgradeTest(true)
	r, c, _ := newClusterPolicyUpgradeTestReconciler(t, cp)

	gc := &nvidiav1alpha1.GPUCluster{ObjectMeta: metav1.ObjectMeta{Name: "config"}}
	require.NoError(t, c.Create(t.Context(), gc))

	_, err := r.Reconcile(t.Context(), ctrl.Request{NamespacedName: client.ObjectKeyFromObject(cp)})
	require.Equal(t, gpuv1.NotReady, clusterPolicyState(t, c, cp.Name))
	require.ErrorContains(t, err, "ClusterPolicy and GPUCluster cannot co-exist")

	// Deleting the GPUCluster instance unblocks the next reconcile
	require.NoError(t, c.Delete(t.Context(), gc))
	_, err = r.Reconcile(t.Context(), ctrl.Request{NamespacedName: client.ObjectKeyFromObject(cp)})
	require.NoError(t, err)
	require.Equal(t, gpuv1.Ready, clusterPolicyState(t, c, cp.Name))
}

func newClusterPolicyUpgradeTestReconciler(t *testing.T, cp *gpuv1.ClusterPolicy, nodes ...*corev1.Node) (*ClusterPolicyReconciler, client.Client, *OperatorMetrics) {
	t.Helper()
	scheme := runtime.NewScheme()
	require.NoError(t, gpuv1.AddToScheme(scheme))
	require.NoError(t, corev1.AddToScheme(scheme))
	require.NoError(t, nvidiav1alpha1.AddToScheme(scheme))

	objects := []client.Object{cp}
	for _, node := range nodes {
		objects = append(objects, node)
	}
	c := fake.NewClientBuilder().WithScheme(scheme).WithObjects(objects...).WithStatusSubresource(&gpuv1.ClusterPolicy{}).Build()
	metrics := newClusterPolicyUpgradeTestMetrics()
	previousController := clusterPolicyCtrl
	clusterPolicyCtrl = ClusterPolicyController{
		controls:        []controlFunc{{func(ClusterPolicyController) (gpuv1.State, error) { return gpuv1.Ready, nil }}},
		stateNames:      []string{"test"},
		operatorMetrics: metrics,
	}
	t.Cleanup(func() { clusterPolicyCtrl = previousController })

	return &ClusterPolicyReconciler{Client: c, Scheme: scheme, Log: logr.Discard(), conditionUpdater: &FakeConditionUpdater{}}, c, metrics
}

func newClusterPolicyUpgradeTestMetrics() *OperatorMetrics {
	failedCounter := &countingCounter{Counter: promcli.NewCounter(promcli.CounterOpts{})}
	return &OperatorMetrics{
		gpuNodesTotal:                 promcli.NewGauge(promcli.GaugeOpts{}),
		reconciliationLastSuccess:     promcli.NewGauge(promcli.GaugeOpts{}),
		reconciliationStatus:          promcli.NewGauge(promcli.GaugeOpts{}),
		reconciliationTotal:           promcli.NewCounter(promcli.CounterOpts{}),
		reconciliationFailed:          failedCounter,
		reconciliationHasNFDLabels:    promcli.NewGauge(promcli.GaugeOpts{}),
		openshiftDriverToolkitEnabled: promcli.NewGauge(promcli.GaugeOpts{}),
	}
}

type countingCounter struct {
	promcli.Counter
	increments int
}

func (c *countingCounter) Inc() {
	c.increments++
	c.Counter.Inc()
}

func clusterPolicyState(t *testing.T, c client.Client, name string) gpuv1.State {
	t.Helper()
	cp := &gpuv1.ClusterPolicy{}
	require.NoError(t, c.Get(t.Context(), client.ObjectKey{Name: name}, cp))
	return cp.Status.State
}
