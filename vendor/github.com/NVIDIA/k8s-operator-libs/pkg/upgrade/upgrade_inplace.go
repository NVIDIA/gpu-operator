/*
Copyright 2025 NVIDIA CORPORATION & AFFILIATES

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package upgrade

import (
	"context"
	"fmt"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/util/intstr"

	"github.com/NVIDIA/k8s-operator-libs/api/upgrade/v1alpha1"
	"github.com/NVIDIA/k8s-operator-libs/pkg/consts"
)

// InplaceNodeStateManagerImpl contains concrete implementations for distinct inplace upgrade mode
type InplaceNodeStateManagerImpl struct {
	*CommonUpgradeManagerImpl
}

// NewClusterUpgradeStateManager creates a new instance of InplaceNodeStateManagerImpl
func NewInplaceNodeStateManagerImpl(commonmanager *CommonUpgradeManagerImpl) (ProcessNodeStateManager,
	error) {
	manager := &InplaceNodeStateManagerImpl{
		CommonUpgradeManagerImpl: commonmanager,
	}
	return manager, nil
}

// ProcessUpgradeRequiredNodes processes UpgradeStateUpgradeRequired nodes and moves them to UpgradeStateCordonRequired
// until the limit on max parallel upgrades is reached.
func (m *InplaceNodeStateManagerImpl) ProcessUpgradeRequiredNodes(
	ctx context.Context, currentClusterState *ClusterUpgradeState,
	upgradePolicy *v1alpha1.DriverUpgradePolicySpec) error {
	var err error

	totalNodes := m.GetTotalManagedNodes(currentClusterState)
	upgradesInProgress := m.GetUpgradesInProgress(currentClusterState)
	currentUnavailableNodes := m.GetCurrentUnavailableNodes(currentClusterState)
	maxUnavailable := totalNodes

	if upgradePolicy.MaxUnavailable != nil {
		maxUnavailable, err = intstr.GetScaledValueFromIntOrPercent(upgradePolicy.MaxUnavailable, totalNodes, true)
		if err != nil {
			m.Log.V(consts.LogLevelError).Error(err, "Failed to compute maxUnavailable from the current total nodes")
			return err
		}
	}
	upgradesAvailable := m.GetUpgradesAvailable(currentClusterState, upgradePolicy.MaxParallelUpgrades,
		maxUnavailable)
	m.Log.V(consts.LogLevelInfo).Info("Upgrades in progress",
		"currently in progress", upgradesInProgress,
		"max parallel upgrades", upgradePolicy.MaxParallelUpgrades,
		"upgrade slots available", upgradesAvailable,
		"currently unavailable nodes", currentUnavailableNodes,
		"total number of nodes", totalNodes,
		"maximum nodes that can be unavailable", maxUnavailable)

	for _, nodeState := range currentClusterState.NodeStates[UpgradeStateUpgradeRequired] {
		upgradeRequested := m.IsUpgradeRequested(nodeState.Node)
		if upgradeRequested {
			// Make sure to remove the upgrade-requested annotation
			err := m.NodeUpgradeStateProvider.ChangeNodeUpgradeAnnotation(ctx, nodeState.Node,
				GetUpgradeRequestedAnnotationKey(), "null")
			if err != nil {
				m.Log.V(consts.LogLevelError).Error(
					err, "Failed to delete node upgrade-requested annotation")
				return err
			}
		}
		if m.SkipNodeUpgrade(nodeState.Node) {
			m.Log.V(consts.LogLevelInfo).Info("Node is marked for skipping upgrades", "node", nodeState.Node.Name)
			continue
		}

		if upgradesAvailable <= 0 {
			// when no new node upgrades are available, progess with manually cordoned nodes
			if m.IsNodeUnschedulable(nodeState.Node) {
				m.Log.V(consts.LogLevelDebug).Info("Node is already cordoned, progressing for driver upgrade",
					"node", nodeState.Node.Name)
			} else {
				m.Log.V(consts.LogLevelDebug).Info("Node upgrade limit reached, pausing further upgrades",
					"node", nodeState.Node.Name)
				continue
			}
		}

		targetState, terr := m.nextStateForUpgradeRequiredNode(ctx, nodeState, upgradeRequested)
		if terr != nil {
			// Keep the node in upgrade-required and retry on the next reconcile instead
			// of starting a full upgrade.
			m.Log.V(consts.LogLevelError).Error(terr,
				"could not determine next upgrade state; node kept in upgrade-required for retry",
				"node", nodeState.Node.Name)
			logEventf(m.EventRecorder, nodeState.Node, corev1.EventTypeWarning, GetEventReason(),
				"%v, will retry", terr)
			continue
		}

		err := m.NodeUpgradeStateProvider.ChangeNodeUpgradeState(ctx, nodeState.Node, targetState)
		if err == nil {
			upgradesAvailable--
			m.Log.V(consts.LogLevelInfo).Info("Node moving to next upgrade state",
				"node", nodeState.Node.Name, "state", targetState)
		} else {
			m.Log.V(consts.LogLevelError).Error(
				err, "Failed to change node upgrade state", "state", targetState)
			return err
		}
	}

	return nil
}

// nextStateForUpgradeRequiredNode determines the state a node in upgrade-required moves to.
// It returns UpgradeStatePodRestartRequired when a registered restart-only predicate matches
// (after cordoning the node), and UpgradeStateCordonRequired for the full upgrade flow otherwise.
// A non-nil error means the decision could not be made; the caller keeps the node in
// upgrade-required and retries on the next reconcile.
func (m *InplaceNodeStateManagerImpl) nextStateForUpgradeRequiredNode(
	ctx context.Context, nodeState *NodeUpgradeState, upgradeRequested bool) (string, error) {
	restartOnly, err := m.shouldRestartOnly(ctx, nodeState, upgradeRequested)
	if err != nil {
		return "", err
	}
	if !restartOnly {
		return UpgradeStateCordonRequired, nil
	}
	// Restart-only change: cordon the node so it stays unschedulable if the pod restart fails, as in
	// the full upgrade flow, then restart the driver pod without evicting workloads.
	m.Log.V(consts.LogLevelInfo).Info(
		"Restart-only change detected; cordoning node and restarting driver pod in place, "+
			"skipping pod-deletion and drain", "node", nodeState.Node.Name)
	if err := m.CordonManager.Cordon(ctx, nodeState.Node); err != nil {
		return "", fmt.Errorf("failed to cordon node for restart-only upgrade: %w", err)
	}
	return UpgradeStatePodRestartRequired, nil
}

// shouldRestartOnly reports whether the node qualifies for an in-place driver pod restart instead
// of the full upgrade flow. It is false when no predicate is registered, for orphaned pods, for
// nodes that explicitly requested an upgrade, and for nodes waiting for safe driver load (which
// must take the full flow so workloads are evicted before the load is unblocked at
// pod-restart-required).
func (m *InplaceNodeStateManagerImpl) shouldRestartOnly(
	ctx context.Context, nodeState *NodeUpgradeState, upgradeRequested bool) (bool, error) {
	if m.restartOnlyPredicate == nil || upgradeRequested || nodeState.IsOrphanedPod() ||
		nodeState.DriverPod == nil {
		return false, nil
	}
	waitingForSafeLoad, err := m.SafeDriverLoadManager.IsWaitingForSafeDriverLoad(ctx, nodeState.Node)
	if err != nil {
		return false, fmt.Errorf("failed to check safe driver load status: %w", err)
	}
	if waitingForSafeLoad {
		return false, nil
	}
	restartOnly, err := m.restartOnlyPredicate(&nodeState.DriverPod.Spec,
		&nodeState.DriverDaemonSet.Spec.Template.Spec)
	if err != nil {
		return false, fmt.Errorf("failed to evaluate restart-only predicate: %w", err)
	}
	return restartOnly, nil
}

// ProcessNodeMaintenanceRequiredNodes is a used to satisfy ProcessNodeStateManager interface
func (m *InplaceNodeStateManagerImpl) ProcessNodeMaintenanceRequiredNodes(ctx context.Context,
	currentClusterState *ClusterUpgradeState) error {
	_ = ctx
	_ = currentClusterState
	return nil
}

// ProcessUncordonRequiredNodes processes UpgradeStateUncordonRequired nodes,
// uncordons them and moves them to UpgradeStateDone state
func (m *InplaceNodeStateManagerImpl) ProcessUncordonRequiredNodes(
	ctx context.Context, currentClusterState *ClusterUpgradeState) error {
	m.Log.V(consts.LogLevelInfo).Info("ProcessUncordonRequiredNodes")

	for _, nodeState := range currentClusterState.NodeStates[UpgradeStateUncordonRequired] {
		// check if if node upgrade is handled by requestor mode, if so node uncordon will be performed by requestor flow
		if IsNodeInRequestorMode(nodeState.Node) {
			continue
		}
		err := m.CordonManager.Uncordon(ctx, nodeState.Node)
		if err != nil {
			m.Log.V(consts.LogLevelWarning).Error(
				err, "Node uncordon failed", "node", nodeState.Node)
			return err
		}
		err = m.NodeUpgradeStateProvider.ChangeNodeUpgradeState(ctx, nodeState.Node, UpgradeStateDone)
		if err != nil {
			m.Log.V(consts.LogLevelError).Error(
				err, "Failed to change node upgrade state", "state", UpgradeStateDone)
			return err
		}
	}
	return nil
}
