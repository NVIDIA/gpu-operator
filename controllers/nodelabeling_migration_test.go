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

	"github.com/NVIDIA/k8s-operator-libs/pkg/upgrade"
	"github.com/go-logr/logr"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/tools/events"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	gpuv1 "github.com/NVIDIA/gpu-operator/api/nvidia/v1"
	nvidiav1alpha1 "github.com/NVIDIA/gpu-operator/api/nvidia/v1alpha1"
	"github.com/NVIDIA/gpu-operator/internal/consts"
)

func TestNodeLabelingMigrationRespectsPerDriverPolicies(t *testing.T) {
	for _, stack := range []string{"ClusterPolicy", "GPUCluster"} {
		t.Run(stack, func(t *testing.T) {
			ctx := context.Background()
			disabled := &nvidiav1alpha1.NVIDIADriver{
				ObjectMeta: metav1.ObjectMeta{Name: "disabled-driver"},
				Spec: nvidiav1alpha1.NVIDIADriverSpec{
					Default:       true,
					UpgradePolicy: &nvidiav1alpha1.DriverUpgradePolicySpec{AutoUpgrade: false},
				},
			}
			enabled := &nvidiav1alpha1.NVIDIADriver{
				ObjectMeta: metav1.ObjectMeta{Name: "enabled-driver"},
				Spec: nvidiav1alpha1.NVIDIADriverSpec{
					NodeSelector:  map[string]string{"driver-pool": "enabled"},
					UpgradePolicy: &nvidiav1alpha1.DriverUpgradePolicySpec{AutoUpgrade: true},
				},
			}
			defaulted := &nvidiav1alpha1.NVIDIADriver{
				ObjectMeta: metav1.ObjectMeta{Name: "defaulted-driver"},
				Spec: nvidiav1alpha1.NVIDIADriverSpec{
					NodeSelector: map[string]string{"driver-pool": "defaulted"},
				},
			}
			objects := []client.Object{disabled, enabled, defaulted}
			if stack == "ClusterPolicy" {
				objects = append(objects, &gpuv1.ClusterPolicy{
					ObjectMeta: metav1.ObjectMeta{Name: "cluster-policy"},
					Spec: gpuv1.ClusterPolicySpec{Driver: gpuv1.DriverSpec{
						UseNvidiaDriverCRD: new(true),
					}},
				})
			} else {
				objects = append(objects, &nvidiav1alpha1.GPUCluster{
					ObjectMeta: metav1.ObjectMeta{Name: "gpu-cluster"},
				})
			}
			for _, pool := range []string{"disabled", "enabled", "defaulted"} {
				node := &corev1.Node{ObjectMeta: metav1.ObjectMeta{
					Name: pool + "-node",
					Labels: map[string]string{
						gpuPCILabelKey: "true", commonGPULabelKey: commonGPULabelValue,
						"driver-pool": pool,
					},
				}}
				if pool == "disabled" {
					node.Annotations = map[string]string{driverAutoUpgradeAnnotationKey: "true"}
				}
				objects = append(objects, node, orphanedDriverPod(pool+"-pod", testOperatorNamespace, node.Name))
			}
			upgrader, _ := newTestUpgradeReconciler(t, objects...)
			recorder := events.NewFakeRecorder(10)
			labeler := &NodeLabelingReconciler{
				Client: upgrader.Client, Namespace: testOperatorNamespace, recorder: recorder,
			}
			stateKey := upgrade.GetUpgradeStateLabelKey()

			for cycle := range 3 {
				_, err := labeler.Reconcile(ctx, reconcile.Request{})
				require.NoError(t, err)
				for _, pool := range []string{"disabled", "enabled", "defaulted"} {
					node := &corev1.Node{}
					require.NoError(t, upgrader.Get(ctx, types.NamespacedName{Name: pool + "-node"}, node))
					assert.Equal(t, pool+"-driver", node.Labels[consts.NVIDIADriverOwnerLabel])
					if pool == "disabled" {
						assert.NotContains(t, node.Labels, stateKey, "cycle %d", cycle)
						assert.NotContains(t, node.Annotations, driverAutoUpgradeAnnotationKey)
					} else {
						assert.Equal(t, upgrade.UpgradeStateUpgradeRequired, node.Labels[stateKey])
						assert.Equal(t, "true", node.Annotations[driverAutoUpgradeAnnotationKey])
					}
				}
				require.Len(t, recorder.Events, 1)
				event := <-recorder.Events
				assert.Contains(t, event, "Warning ManualDriverPodReplacementRequired")
				assert.Contains(t, event, "disabled-driver")
				assert.Contains(t, event, "disabled-node")
				assert.Contains(t, event, "disabled-pod")

				_, err = upgrader.Reconcile(ctx, upgradeSingletonRequest())
				require.NoError(t, err)
				node := &corev1.Node{}
				require.NoError(t, upgrader.Get(ctx, types.NamespacedName{Name: "disabled-node"}, node))
				assert.NotContains(t, node.Labels, stateKey)
			}

			// Enabling automatic upgrades must allow migration to resume without changing ownership.
			require.NoError(t, upgrader.Get(ctx, types.NamespacedName{Name: disabled.Name}, disabled))
			disabled.Spec.UpgradePolicy.AutoUpgrade = true
			require.NoError(t, upgrader.Update(ctx, disabled))
			_, err := labeler.Reconcile(ctx, reconcile.Request{})
			require.NoError(t, err)
			node := &corev1.Node{}
			require.NoError(t, upgrader.Get(ctx, types.NamespacedName{Name: "disabled-node"}, node))
			assert.Equal(t, upgrade.UpgradeStateUpgradeRequired, node.Labels[stateKey])
			assert.Equal(t, "true", node.Annotations[driverAutoUpgradeAnnotationKey])
			assert.Empty(t, recorder.Events)
			require.NoError(t, upgrader.Get(ctx, types.NamespacedName{
				Name: "disabled-pod", Namespace: testOperatorNamespace,
			}, &corev1.Pod{}))
		})
	}
}

func TestOrphanedDriverPodDisabledPolicyPreservesExistingState(t *testing.T) {
	upgrade.SetDriverName("gpu")
	stateKey := upgrade.GetUpgradeStateLabelKey()
	for _, state := range []string{
		upgrade.UpgradeStateUnknown, upgrade.UpgradeStateDone, upgrade.UpgradeStateUpgradeRequired,
		upgrade.UpgradeStatePodRestartRequired, upgrade.UpgradeStateFailed,
	} {
		t.Run("state="+state, func(t *testing.T) {
			ctx := context.Background()
			driver := &nvidiav1alpha1.NVIDIADriver{
				ObjectMeta: metav1.ObjectMeta{Name: "disabled-driver"},
				Spec: nvidiav1alpha1.NVIDIADriverSpec{
					UpgradePolicy: &nvidiav1alpha1.DriverUpgradePolicySpec{AutoUpgrade: false},
				},
			}
			node := nodeWithUpgradeState("disabled-node", driver.Name)
			if state == upgrade.UpgradeStateUnknown {
				delete(node.Labels, stateKey)
			} else {
				node.Labels[stateKey] = state
			}
			pod := orphanedDriverPod("legacy-driver", testOperatorNamespace, node.Name)
			upgrader, manager := newTestUpgradeReconciler(t, driver, node, pod)
			recorder := events.NewFakeRecorder(10)
			labeler := &nodeLabelingController{
				client: upgrader.Client, namespace: testOperatorNamespace, logger: logr.Discard(), recorder: recorder,
			}
			require.NoError(t, labeler.labelNodesWithOrphanedDriverPods(ctx))
			updated := &corev1.Node{}
			require.NoError(t, upgrader.Get(ctx, types.NamespacedName{Name: node.Name}, updated))
			assert.Equal(t, node.Labels, updated.Labels)
			require.Len(t, recorder.Events, 1)
			assert.Contains(t, <-recorder.Events, "ManualDriverPodReplacementRequired")

			// The upgrade reconciler remains responsible for clearing stale upgrade state.
			_, err := upgrader.Reconcile(ctx, upgradeSingletonRequest())
			require.NoError(t, err)
			require.NoError(t, upgrader.Get(ctx, types.NamespacedName{Name: node.Name}, updated))
			assert.NotContains(t, updated.Labels, stateKey)
			assert.Zero(t, manager.applyCalls)
			require.NoError(t, upgrader.Get(ctx, types.NamespacedName{Name: pod.Name, Namespace: pod.Namespace}, &corev1.Pod{}))
		})
	}
}
