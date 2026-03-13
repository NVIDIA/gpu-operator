/*
Copyright 2022 NVIDIA CORPORATION & AFFILIATES

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

	"github.com/go-logr/logr"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/record"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/NVIDIA/k8s-operator-libs/api/upgrade/v1alpha1"
	"github.com/NVIDIA/k8s-operator-libs/pkg/consts"
)

// ClusterUpgradeStateManager is an interface for performing cluster upgrades of driver containers
type ClusterUpgradeStateManager interface {
	CommonUpgradeStateManager
	// WithPodDeletionEnabled provides an option to enable the optional 'pod-deletion'
	// state and pass a custom PodDeletionFilter to use
	WithPodDeletionEnabled(filter PodDeletionFilter) ClusterUpgradeStateManager
	// WithValidationEnabled provides an option to enable the optional 'validation' state
	// and pass a podSelector to specify which pods are performing the validation
	WithValidationEnabled(podSelector string) ClusterUpgradeStateManager
	// BuildState builds a point-in-time snapshot of the driver upgrade state in the cluster.
	BuildState(ctx context.Context, namespace string,
		driverLabels map[string]string) (*ClusterUpgradeState, error)
	// ApplyState receives a complete cluster upgrade state and, based on upgrade policy, processes each node's state.
	// Based on the current state of the node, it is calculated if the node can be moved to the next state right now
	// or whether any actions need to be scheduled for the node to move to the next state.
	// The function is stateless and idempotent. If the error was returned before all nodes' states were processed,
	// ApplyState would be called again and complete the processing - all the decisions are based on the input data.
	ApplyState(ctx context.Context,
		currentState *ClusterUpgradeState, upgradePolicy *v1alpha1.DriverUpgradePolicySpec) (err error)
}

// ClusterUpgradeStateManagerImpl serves as a state machine for the ClusterUpgradeState
// It processes each node and based on its state schedules the required jobs to change their state to the next one
type ClusterUpgradeStateManagerImpl struct {
	*CommonUpgradeManagerImpl
	inplace   ProcessNodeStateManager
	requestor ProcessNodeStateManager
	opts      StateOptions
}

// NewClusterUpgradeStateManager creates a new instance of ClusterUpgradeStateManagerImpl
func NewClusterUpgradeStateManager(
	log logr.Logger,
	k8sConfig *rest.Config,
	eventRecorder record.EventRecorder,
	opts StateOptions) (ClusterUpgradeStateManager, error) {
	commonmanager, err := NewCommonUpgradeStateManager(log, k8sConfig, Scheme, eventRecorder)
	if err != nil {
		return nil, fmt.Errorf("failed to create commonmanager upgrade state manager. %v", err)
	}
	requestor, err := NewRequestorNodeStateManagerImpl(commonmanager, opts.Requestor)
	if err != nil && err != ErrNodeMaintenanceUpgradeDisabled {
		return nil, fmt.Errorf("failed to create requestor upgrade state manager. %v", err)
	}

	inplace, err := NewInplaceNodeStateManagerImpl(commonmanager)
	if err != nil {
		return nil, fmt.Errorf("failed to create inplace upgrade state manager. %v", err)
	}

	manager := &ClusterUpgradeStateManagerImpl{
		CommonUpgradeManagerImpl: commonmanager,
		requestor:                requestor,
		inplace:                  inplace,
		opts:                     opts,
	}

	return manager, nil
}

type StateOptions struct {
	Requestor RequestorOptions
}

// BuildState builds a point-in-time snapshot of the driver upgrade state in the cluster.
func (m *ClusterUpgradeStateManagerImpl) BuildState(ctx context.Context, namespace string,
	driverLabels map[string]string) (*ClusterUpgradeState, error) {
	m.Log.V(consts.LogLevelInfo).Info("Building state")

	upgradeState := NewClusterUpgradeState()

	daemonSets, err := m.GetDriverDaemonSets(ctx, namespace, driverLabels)
	if err != nil {
		m.Log.V(consts.LogLevelError).Error(err, "Failed to get driver DaemonSet list")
		return nil, err
	}

	m.Log.V(consts.LogLevelDebug).Info("Got driver DaemonSets", "length", len(daemonSets))

	// Get list of driver pods
	podList := &corev1.PodList{}

	err = m.K8sClient.List(ctx, podList,
		client.InNamespace(namespace),
		client.MatchingLabels(driverLabels),
	)

	if err != nil {
		return nil, err
	}

	filteredPodList := []corev1.Pod{}
	for _, ds := range daemonSets {
		dsPods := m.GetPodsOwnedbyDs(ds, podList.Items)
		if int(ds.Status.DesiredNumberScheduled) != len(dsPods) {
			m.Log.V(consts.LogLevelInfo).Info("Driver DaemonSet has Unscheduled pods", "name", ds.Name)
			return nil, fmt.Errorf("driver DaemonSet should not have Unscheduled pods")
		}
		filteredPodList = append(filteredPodList, dsPods...)
	}

	// Collect also orphaned driver pods
	filteredPodList = append(filteredPodList, m.GetOrphanedPods(podList.Items)...)

	upgradeStateLabel := GetUpgradeStateLabelKey()

	for i := range filteredPodList {
		pod := &filteredPodList[i]
		var ownerDaemonSet *appsv1.DaemonSet
		if IsOrphanedPod(pod) {
			ownerDaemonSet = nil
		} else {
			ownerDaemonSet = daemonSets[pod.OwnerReferences[0].UID]
		}
		// Check if pod is already scheduled to a Node
		if pod.Spec.NodeName == "" && pod.Status.Phase == corev1.PodPending {
			m.Log.V(consts.LogLevelInfo).Info("Driver Pod has no NodeName, skipping", "pod", pod.Name)
			continue
		}
		nodeState, err := m.buildNodeUpgradeState(ctx, pod, ownerDaemonSet)
		if err != nil {
			m.Log.V(consts.LogLevelError).Error(err, "Failed to build node upgrade state for pod", "pod", pod)
			return nil, err
		}
		nodeStateLabel := nodeState.Node.Labels[upgradeStateLabel]
		upgradeState.NodeStates[nodeStateLabel] = append(
			upgradeState.NodeStates[nodeStateLabel], nodeState)
	}

	return &upgradeState, nil
}

// ApplyState receives a complete cluster upgrade state and, based on upgrade policy, processes each node's state.
// Based on the current state of the node, it is calculated if the node can be moved to the next state right now
// or whether any actions need to be scheduled for the node to move to the next state.
// The function is stateless and idempotent. If the error was returned before all nodes' states were processed,
// ApplyState would be called again and complete the processing - all the decisions are based on the input data.
func (m *ClusterUpgradeStateManagerImpl) ApplyState(ctx context.Context,
	currentState *ClusterUpgradeState, upgradePolicy *v1alpha1.DriverUpgradePolicySpec) (err error) {
	m.Log.V(consts.LogLevelInfo).Info("State Manager, got state update")

	if currentState == nil {
		return fmt.Errorf("currentState should not be empty")
	}

	if upgradePolicy == nil || !upgradePolicy.AutoUpgrade {
		m.Log.V(consts.LogLevelInfo).Info("Driver auto upgrade is disabled, skipping")
		return nil
	}

	m.Log.V(consts.LogLevelInfo).Info("Node states:",
		"Unknown", len(currentState.NodeStates[UpgradeStateUnknown]),
		UpgradeStateDone, len(currentState.NodeStates[UpgradeStateDone]),
		UpgradeStateUpgradeRequired, len(currentState.NodeStates[UpgradeStateUpgradeRequired]),
		UpgradeStateCordonRequired, len(currentState.NodeStates[UpgradeStateCordonRequired]),
		UpgradeStateWaitForJobsRequired, len(currentState.NodeStates[UpgradeStateWaitForJobsRequired]),
		UpgradeStatePodDeletionRequired, len(currentState.NodeStates[UpgradeStatePodDeletionRequired]),
		UpgradeStateFailed, len(currentState.NodeStates[UpgradeStateFailed]),
		UpgradeStateDrainRequired, len(currentState.NodeStates[UpgradeStateDrainRequired]),
		UpgradeStateNodeMaintenanceRequired, len(currentState.NodeStates[UpgradeStateNodeMaintenanceRequired]),
		UpgradeStatePostMaintenanceRequired, len(currentState.NodeStates[UpgradeStatePostMaintenanceRequired]),
		UpgradeStatePodRestartRequired, len(currentState.NodeStates[UpgradeStatePodRestartRequired]),
		UpgradeStateValidationRequired, len(currentState.NodeStates[UpgradeStateValidationRequired]),
		UpgradeStateUncordonRequired, len(currentState.NodeStates[UpgradeStateUncordonRequired]))

	// Determine the object to log this event
	// m.EventRecorder.Eventf(m.Namespace, v1.EventTypeNormal, GetEventReason(),
	// "InProgress: %d, MaxParallelUpgrades: %d, UpgradeSlotsAvailable: %s", upgradesInProgress,
	// upgradePolicy.MaxParallelUpgrades, upgradesAvailable)

	// First, check if unknown or ready nodes need to be upgraded
	err = m.ProcessDoneOrUnknownNodes(ctx, currentState, UpgradeStateUnknown)
	if err != nil {
		m.Log.V(consts.LogLevelError).Error(err, "Failed to process nodes", "state", UpgradeStateUnknown)
		return err
	}
	err = m.ProcessDoneOrUnknownNodes(ctx, currentState, UpgradeStateDone)
	if err != nil {
		m.Log.V(consts.LogLevelError).Error(err, "Failed to process nodes", "state", UpgradeStateDone)
		return err
	}
	// Start upgrade process for upgradesAvailable number of nodes
	err = m.ProcessUpgradeRequiredNodesWrapper(ctx, currentState, upgradePolicy)
	if err != nil {
		m.Log.V(consts.LogLevelError).Error(
			err, "Failed to process nodes", "state", UpgradeStateUpgradeRequired)
		return err
	}

	err = m.ProcessCordonRequiredNodes(ctx, currentState)
	if err != nil {
		m.Log.V(consts.LogLevelError).Error(err, "Failed to cordon nodes")
		return err
	}

	err = m.ProcessWaitForJobsRequiredNodes(ctx, currentState, upgradePolicy.WaitForCompletion)
	if err != nil {
		m.Log.V(consts.LogLevelError).Error(err, "Failed to waiting for required jobs to complete")
		return err
	}

	drainEnabled := upgradePolicy.DrainSpec != nil && upgradePolicy.DrainSpec.Enable
	err = m.ProcessPodDeletionRequiredNodes(ctx, currentState, upgradePolicy.PodDeletion, drainEnabled)
	if err != nil {
		m.Log.V(consts.LogLevelError).Error(err, "Failed to delete pods")
		return err
	}

	// Schedule nodes for drain
	err = m.ProcessDrainNodes(ctx, currentState, upgradePolicy.DrainSpec)
	if err != nil {
		m.Log.V(consts.LogLevelError).Error(err, "Failed to schedule nodes drain")
		return err
	}

	// TODO: in future versions we'll remove 'pod-restart-required' and use 'post-maintenance-required' instead
	// to indicate a general post maintennace node operations (e.g. restart driver pods, node reboot etc.)
	err = m.ProcessNodeMaintenanceRequiredNodesWrapper(ctx, currentState)
	if err != nil {
		m.Log.V(consts.LogLevelError).Error(err, "Failed for post maintenance")
		return err
	}

	err = m.ProcessPodRestartNodes(ctx, currentState)
	if err != nil {
		m.Log.V(consts.LogLevelError).Error(err, "Failed for 'pod-restart-required' state")
		return err
	}

	err = m.ProcessUpgradeFailedNodes(ctx, currentState)
	if err != nil {
		m.Log.V(consts.LogLevelError).Error(err, "Failed to process nodes in 'upgrade-failed' state")
		return err
	}
	err = m.ProcessValidationRequiredNodes(ctx, currentState)
	if err != nil {
		m.Log.V(consts.LogLevelError).Error(err, "Failed to validate driver upgrade")
		return err
	}

	err = m.ProcessUncordonRequiredNodesWrapper(ctx, currentState)
	if err != nil {
		m.Log.V(consts.LogLevelError).Error(err, "Failed to uncordon nodes")
		return err
	}
	m.Log.V(consts.LogLevelInfo).Info("State Manager, finished processing")
	return nil
}

func (m *ClusterUpgradeStateManagerImpl) GetRequestor() ProcessNodeStateManager {
	return m.requestor
}

func (m *ClusterUpgradeStateManagerImpl) ProcessUpgradeRequiredNodesWrapper(ctx context.Context,
	currentState *ClusterUpgradeState, upgradePolicy *v1alpha1.DriverUpgradePolicySpec) error {
	var err error
	// Start upgrade process for upgradesAvailable number of nodes
	if m.opts.Requestor.UseMaintenanceOperator {
		err = m.requestor.ProcessUpgradeRequiredNodes(ctx, currentState, upgradePolicy)
	} else {
		err = m.inplace.ProcessUpgradeRequiredNodes(ctx, currentState, upgradePolicy)
	}
	return err
}

func (m *ClusterUpgradeStateManagerImpl) ProcessNodeMaintenanceRequiredNodesWrapper(ctx context.Context,
	currentState *ClusterUpgradeState) error {
	var err error
	if m.opts.Requestor.UseMaintenanceOperator {
		if err = m.requestor.ProcessNodeMaintenanceRequiredNodes(ctx, currentState); err != nil {
			return err
		}
	}

	return err
}

func (m *ClusterUpgradeStateManagerImpl) ProcessUncordonRequiredNodesWrapper(ctx context.Context,
	currentState *ClusterUpgradeState) error {
	// The idea of calling both inplace and requestor ProcessUncordonRequiredNodes is to handle a case
	// where some nodes had already undergone inplace upgrage process, and yet to complete it,
	// before enabling requestor upgrade mode. In this case, although requestor upgrade mode is enabled,
	// inplace flow will keep processing pending nodes which already started inplace upgrade process.
	err := m.inplace.ProcessUncordonRequiredNodes(ctx, currentState)
	if err != nil {
		return err
	}
	if m.opts.Requestor.UseMaintenanceOperator {
		err = m.requestor.ProcessUncordonRequiredNodes(ctx, currentState)
	}
	return err
}

// WithPodDeletionEnabled provides an option to enable the optional 'pod-deletion' state and pass a custom
// PodDeletionFilter to use
func (m *ClusterUpgradeStateManagerImpl) WithPodDeletionEnabled(filter PodDeletionFilter) ClusterUpgradeStateManager {
	if filter == nil {
		m.Log.V(consts.LogLevelWarning).Info("Cannot enable PodDeletion state as PodDeletionFilter is nil")
		return m
	}
	m.PodManager = NewPodManager(m.K8sInterface, m.NodeUpgradeStateProvider, m.Log, filter, m.EventRecorder)
	m.podDeletionStateEnabled = true
	return m
}

// WithValidationEnabled provides an option to enable the optional 'validation' state and pass a podSelector to specify
// which pods are performing the validation
func (m *ClusterUpgradeStateManagerImpl) WithValidationEnabled(podSelector string) ClusterUpgradeStateManager {
	if podSelector == "" {
		m.Log.V(consts.LogLevelWarning).Info("Cannot enable Validation state as podSelector is empty")
		return m
	}
	m.ValidationManager = NewValidationManager(m.K8sInterface, m.Log, m.EventRecorder, m.NodeUpgradeStateProvider,
		podSelector)
	m.validationStateEnabled = true
	return m
}

// buildNodeUpgradeState creates a mapping between a node,
// the driver POD running on them and the daemon set, controlling this pod
func (m *ClusterUpgradeStateManagerImpl) buildNodeUpgradeState(
	ctx context.Context, pod *corev1.Pod, ds *appsv1.DaemonSet) (*NodeUpgradeState, error) {
	var nm client.Object
	node, err := m.NodeUpgradeStateProvider.GetNode(ctx, pod.Spec.NodeName)
	if err != nil {
		return nil, fmt.Errorf("unable to get node %s: %v", pod.Spec.NodeName, err)
	}

	if m.opts.Requestor.UseMaintenanceOperator {
		rum, ok := m.requestor.(*RequestorNodeStateManagerImpl)
		if !ok {
			return nil, fmt.Errorf("failed to cast rquestor upgrade manager: %v", err)
		}
		nm, err = rum.GetNodeMaintenanceObj(ctx, node.Name)
		if err != nil {
			return nil, fmt.Errorf("failed while trying to fetch nodeMaintennace obj: %v", err)
		}
	}

	upgradeStateLabel := GetUpgradeStateLabelKey()
	m.Log.V(consts.LogLevelInfo).Info("Node hosting a driver pod",
		"node", node.Name, "state", node.Labels[upgradeStateLabel])

	return &NodeUpgradeState{Node: node, DriverPod: pod, DriverDaemonSet: ds, NodeMaintenance: nm}, nil
}
