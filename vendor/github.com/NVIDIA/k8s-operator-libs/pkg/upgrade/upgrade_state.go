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
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/intstr"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/kubernetes/scheme"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/record"
	"sigs.k8s.io/controller-runtime/pkg/client"

	v1alpha1 "github.com/NVIDIA/k8s-operator-libs/api/upgrade/v1alpha1"
	"github.com/NVIDIA/k8s-operator-libs/pkg/consts"
)

// NodeUpgradeState contains a mapping between a node,
// the driver POD running on them and the daemon set, controlling this pod
type NodeUpgradeState struct {
	Node            *corev1.Node
	DriverPod       *corev1.Pod
	DriverDaemonSet *appsv1.DaemonSet
}

// ClusterUpgradeState contains a snapshot of the driver upgrade state in the cluster
// It contains driver upgrade policy and mappings between nodes and their upgrade state
// Nodes are grouped together with the driver POD running on them and the daemon set, controlling this pod
// This state is then used as an input for the ClusterUpgradeStateManager
type ClusterUpgradeState struct {
	NodeStates map[string][]*NodeUpgradeState
}

// NewClusterUpgradeState creates an empty ClusterUpgradeState object
func NewClusterUpgradeState() ClusterUpgradeState {
	return ClusterUpgradeState{NodeStates: make(map[string][]*NodeUpgradeState)}
}

// ClusterUpgradeStateManager serves as a state machine for the ClusterUpgradeState
// It processes each node and based on its state schedules the required jobs to change their state to the next one
type ClusterUpgradeStateManager struct {
	Log           logr.Logger
	K8sClient     client.Client
	K8sInterface  kubernetes.Interface
	EventRecorder record.EventRecorder

	DrainManager             DrainManager
	PodManager               PodManager
	CordonManager            CordonManager
	NodeUpgradeStateProvider NodeUpgradeStateProvider
	ValidationManager        ValidationManager

	// optional states
	podDeletionStateEnabled bool
	validationStateEnabled  bool
}

// NewClusterUpgradeStateManager creates a new instance of ClusterUpgradeStateManager
func NewClusterUpgradeStateManager(
	log logr.Logger,
	k8sConfig *rest.Config,
	eventRecorder record.EventRecorder) (*ClusterUpgradeStateManager, error) {

	k8sClient, err := client.New(k8sConfig, client.Options{Scheme: scheme.Scheme})
	if err != nil {
		return nil, fmt.Errorf("error creating k8s client: %v", err)
	}

	k8sInterface, err := kubernetes.NewForConfig(k8sConfig)
	if err != nil {
		return nil, fmt.Errorf("error creating k8s interface: %v", err)
	}

	nodeUpgradeStateProvider := NewNodeUpgradeStateProvider(k8sClient, log, eventRecorder)
	manager := &ClusterUpgradeStateManager{
		Log:                      log,
		K8sClient:                k8sClient,
		K8sInterface:             k8sInterface,
		EventRecorder:            eventRecorder,
		DrainManager:             NewDrainManager(k8sInterface, nodeUpgradeStateProvider, log, eventRecorder),
		PodManager:               NewPodManager(k8sInterface, nodeUpgradeStateProvider, log, nil, eventRecorder),
		CordonManager:            NewCordonManager(k8sInterface, log),
		NodeUpgradeStateProvider: nodeUpgradeStateProvider,
		ValidationManager:        NewValidationManager(k8sInterface, log, eventRecorder, nodeUpgradeStateProvider, ""),
	}
	return manager, nil
}

// WithPodDeletionEnabled provides an option to enable the optional 'pod-deletion' state and pass a custom PodDeletionFilter to use
func (m *ClusterUpgradeStateManager) WithPodDeletionEnabled(filter PodDeletionFilter) *ClusterUpgradeStateManager {
	if filter == nil {
		m.Log.V(consts.LogLevelWarning).Info("Cannot enable PodDeletion state as PodDeletionFilter is nil")
		return m
	}
	m.PodManager = NewPodManager(m.K8sInterface, m.NodeUpgradeStateProvider, m.Log, filter, m.EventRecorder)
	m.podDeletionStateEnabled = true
	return m
}

// WithValidationEnabled provides an option to enable the optional 'validation' state and pass a podSelector to specify which pods are performing the validation
func (m *ClusterUpgradeStateManager) WithValidationEnabled(podSelector string) *ClusterUpgradeStateManager {
	if podSelector == "" {
		m.Log.V(consts.LogLevelWarning).Info("Cannot enable Validation state as podSelector is empty")
		return m
	}
	m.ValidationManager = NewValidationManager(m.K8sInterface, m.Log, m.EventRecorder, m.NodeUpgradeStateProvider, podSelector)
	m.validationStateEnabled = true
	return m
}

func (m *ClusterUpgradeStateManager) IsPodDeletionEnabled() bool {
	return m.podDeletionStateEnabled
}

func (m *ClusterUpgradeStateManager) IsValidationEnabled() bool {
	return m.validationStateEnabled
}

// GetCurrentUnavailableNodes returns all nodes that are not in ready state
func (m *ClusterUpgradeStateManager) GetCurrentUnavailableNodes(ctx context.Context, currentState *ClusterUpgradeState) int {
	unavailableNodes := 0
	for _, nodeUpgradeStateList := range currentState.NodeStates {
		for _, nodeUpgradeState := range nodeUpgradeStateList {
			// check if the node is cordoned
			if m.isNodeUnschedulable(nodeUpgradeState.Node) {
				m.Log.V(consts.LogLevelDebug).Info("Node is cordoned", "node", nodeUpgradeState.Node.Name)
				unavailableNodes++
				continue
			}
			// check if the node is not ready
			if !m.isNodeConditionReady(nodeUpgradeState.Node) {
				m.Log.V(consts.LogLevelDebug).Info("Node is not-ready", "node", nodeUpgradeState.Node.Name)
				unavailableNodes++
			}
		}
	}
	return unavailableNodes
}

func (m *ClusterUpgradeStateManager) BuildState(ctx context.Context, namespace string, driverLabels map[string]string) (*ClusterUpgradeState, error) {
	m.Log.V(consts.LogLevelInfo).Info("Building state")

	upgradeState := NewClusterUpgradeState()

	daemonSets, err := m.getDriverDaemonSets(ctx, namespace, driverLabels)
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
		dsPods := m.getPodsOwnedbyDs(ds, podList.Items)
		if int(ds.Status.DesiredNumberScheduled) != len(dsPods) {
			m.Log.V(consts.LogLevelInfo).Info("Driver DaemonSet has Unscheduled pods", "name", ds.Name)
			return nil, fmt.Errorf("Driver DaemonSet should not have Unscheduled pods")
		}
		filteredPodList = append(filteredPodList, dsPods...)
	}

	upgradeStateLabel := GetUpgradeStateLabelKey()

	for i := range filteredPodList {
		pod := &filteredPodList[i]
		ownerDaemonSet := daemonSets[pod.OwnerReferences[0].UID]
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

// buildNodeUpgradeState creates a mapping between a node,
// the driver POD running on them and the daemon set, controlling this pod
func (m *ClusterUpgradeStateManager) buildNodeUpgradeState(
	ctx context.Context, pod *corev1.Pod, ds *appsv1.DaemonSet) (*NodeUpgradeState, error) {
	node, err := m.NodeUpgradeStateProvider.GetNode(ctx, pod.Spec.NodeName)
	if err != nil {
		return nil, fmt.Errorf("unable to get node %s: %v", pod.Spec.NodeName, err)
	}

	upgradeStateLabel := GetUpgradeStateLabelKey()
	m.Log.V(consts.LogLevelInfo).Info("Node hosting a driver pod",
		"node", node.Name, "state", node.Labels[upgradeStateLabel])

	return &NodeUpgradeState{Node: node, DriverPod: pod, DriverDaemonSet: ds}, nil
}

// getDriverDaemonSets retrieves DaemonSets with given labels and returns UID->DaemonSet map
func (m *ClusterUpgradeStateManager) getDriverDaemonSets(ctx context.Context, namespace string, labels map[string]string) (map[types.UID]*appsv1.DaemonSet, error) {
	// Get list of driver pods
	daemonSetList := &appsv1.DaemonSetList{}

	err := m.K8sClient.List(ctx, daemonSetList,
		client.InNamespace(namespace),
		client.MatchingLabels(labels))
	if err != nil {
		return nil, fmt.Errorf("error getting DaemonSet list: %v", err)
	}

	daemonSetMap := make(map[types.UID]*appsv1.DaemonSet)
	for i := range daemonSetList.Items {
		daemonSet := &daemonSetList.Items[i]
		daemonSetMap[daemonSet.UID] = daemonSet
	}

	return daemonSetMap, nil
}

// getPodsOwnedbyDs returns a list of the pods owned by the specified DaemonSet
func (m *ClusterUpgradeStateManager) getPodsOwnedbyDs(ds *appsv1.DaemonSet, pods []corev1.Pod) []corev1.Pod {
	dsPodList := []corev1.Pod{}
	for i := range pods {
		pod := &pods[i]
		if pod.OwnerReferences == nil || len(pod.OwnerReferences) < 1 {
			m.Log.V(consts.LogLevelInfo).Info("Driver Pod has no owner DaemonSet", "pod", pod.Name)
			continue
		}
		m.Log.V(consts.LogLevelInfo).Info("Pod", "pod", pod.Name, "owner", pod.OwnerReferences[0].Name)

		if ds.UID != pod.OwnerReferences[0].UID {
			m.Log.V(consts.LogLevelInfo).Info("Driver Pod is not owned by an Driver DaemonSet",
				"pod", pod, "actual owner", pod.OwnerReferences[0])
			continue
		}
		dsPodList = append(dsPodList, *pod)
	}
	return dsPodList
}

// ApplyState receives a complete cluster upgrade state and, based on upgrade policy, processes each node's state.
// Based on the current state of the node, it is calculated if the node can be moved to the next state right now
// or whether any actions need to be scheduled for the node to move to the next state.
// The function is stateless and idempotent. If the error was returned before all nodes' states were processed,
// ApplyState would be called again and complete the processing - all the decisions are based on the input data.
func (m *ClusterUpgradeStateManager) ApplyState(ctx context.Context,
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
		UpgradeStatePodRestartRequired, len(currentState.NodeStates[UpgradeStatePodRestartRequired]),
		UpgradeStateValidationRequired, len(currentState.NodeStates[UpgradeStateValidationRequired]),
		UpgradeStateUncordonRequired, len(currentState.NodeStates[UpgradeStateUncordonRequired]))

	totalNodes := m.GetTotalManagedNodes(ctx, currentState)
	upgradesInProgress := m.GetUpgradesInProgress(ctx, currentState)
	currentUnavailableNodes := m.GetCurrentUnavailableNodes(ctx, currentState)
	maxUnavailable := totalNodes

	if upgradePolicy.MaxUnavailable != nil {
		maxUnavailable, err = intstr.GetScaledValueFromIntOrPercent(upgradePolicy.MaxUnavailable, totalNodes, true)
		if err != nil {
			m.Log.V(consts.LogLevelError).Error(err, "Failed to compute maxUnavailable from the current total nodes")
			return err
		}
	}

	upgradesAvailable := m.GetUpgradesAvailable(ctx, currentState, upgradePolicy.MaxParallelUpgrades, maxUnavailable)

	m.Log.V(consts.LogLevelInfo).Info("Upgrades in progress",
		"currently in progress", upgradesInProgress,
		"max parallel upgrades", upgradePolicy.MaxParallelUpgrades,
		"upgrade slots available", upgradesAvailable,
		"currently unavailable nodes", currentUnavailableNodes,
		"total number of nodes", totalNodes,
		"maximum nodes that can be unavailable", maxUnavailable)

	// Determine the object to log this event
	//m.EventRecorder.Eventf(m.Namespace, v1.EventTypeNormal, GetEventReason(), "InProgress: %d, MaxParallelUpgrades: %d, UpgradeSlotsAvailable: %s", upgradesInProgress, upgradePolicy.MaxParallelUpgrades, upgradesAvailable)

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
	err = m.ProcessUpgradeRequiredNodes(ctx, currentState, upgradesAvailable)
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

	drainEnabled := upgradePolicy.DrainSpec != nil && upgradePolicy.DrainSpec.Enable == true
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
	err = m.ProcessPodRestartNodes(ctx, currentState)
	if err != nil {
		m.Log.V(consts.LogLevelError).Error(err, "Failed to schedule pods restart")
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
	err = m.ProcessUncordonRequiredNodes(ctx, currentState)
	if err != nil {
		m.Log.V(consts.LogLevelError).Error(err, "Failed to uncordon nodes")
		return err
	}
	m.Log.V(consts.LogLevelInfo).Info("State Manager, finished processing")
	return nil
}

// ProcessDoneOrUnknownNodes iterates over UpgradeStateDone or UpgradeStateUnknown nodes and determines
// whether each specific node should be in UpgradeStateUpgradeRequired or UpgradeStateDone state.
func (m *ClusterUpgradeStateManager) ProcessDoneOrUnknownNodes(
	ctx context.Context, currentClusterState *ClusterUpgradeState, nodeStateName string) error {
	m.Log.V(consts.LogLevelInfo).Info("ProcessDoneOrUnknownNodes")

	for _, nodeState := range currentClusterState.NodeStates[nodeStateName] {
		podRevisionHash, err := m.PodManager.GetPodControllerRevisionHash(ctx, nodeState.DriverPod)
		if err != nil {
			m.Log.V(consts.LogLevelError).Error(
				err, "Failed to get pod template revision hash", "pod", nodeState.DriverPod)
			return err
		}
		m.Log.V(consts.LogLevelDebug).Info("pod template revision hash", "hash", podRevisionHash)
		daemonsetRevisionHash, err := m.PodManager.GetDaemonsetControllerRevisionHash(ctx, nodeState.DriverDaemonSet)
		if err != nil {
			m.Log.V(consts.LogLevelError).Error(
				err, "Failed to get daemonset template revision hash", "daemonset", nodeState.DriverDaemonSet)
			return err
		}
		m.Log.V(consts.LogLevelDebug).Info("daemonset template revision hash", "hash", daemonsetRevisionHash)
		if podRevisionHash != daemonsetRevisionHash {
			// If node requires upgrade and is Unschedulable, track this in an
			// annotation and leave node in Unschedulable state when upgrade completes.
			if isNodeUnschedulable(nodeState.Node) {
				annotationKey := GetUpgradeInitialStateAnnotationKey()
				annotationValue := "true"
				m.Log.V(consts.LogLevelInfo).Info("Node is unschedulable, adding annotation to track initial state of the node",
					"node", nodeState.Node.Name, "annotation", annotationKey)
				err = m.NodeUpgradeStateProvider.ChangeNodeUpgradeAnnotation(ctx, nodeState.Node, annotationKey, annotationValue)
				if err != nil {
					return err
				}
			}
			err := m.NodeUpgradeStateProvider.ChangeNodeUpgradeState(ctx, nodeState.Node, UpgradeStateUpgradeRequired)
			if err != nil {
				m.Log.V(consts.LogLevelError).Error(
					err, "Failed to change node upgrade state", "state", UpgradeStateUpgradeRequired)
				return err
			}
			m.Log.V(consts.LogLevelInfo).Info("Node requires upgrade, changed its state to UpgradeRequired",
				"node", nodeState.Node.Name)
			continue
		}

		if nodeStateName == UpgradeStateUnknown {
			err := m.NodeUpgradeStateProvider.ChangeNodeUpgradeState(ctx, nodeState.Node, UpgradeStateDone)
			if err != nil {
				m.Log.V(consts.LogLevelError).Error(
					err, "Failed to change node upgrade state", "state", UpgradeStateDone)
				return err
			}
			m.Log.V(consts.LogLevelInfo).Info("Changed node state to UpgradeDone",
				"node", nodeState.Node.Name)
			continue
		}
		m.Log.V(consts.LogLevelDebug).Info("Node in UpgradeDone state, upgrade not required",
			"node", nodeState.Node.Name)
	}
	return nil
}

// ProcessUpgradeRequiredNodes processes UpgradeStateUpgradeRequired nodes and moves them to UpgradeStateCordonRequired until
// the limit on max parallel upgrades is reached.
func (m *ClusterUpgradeStateManager) ProcessUpgradeRequiredNodes(
	ctx context.Context, currentClusterState *ClusterUpgradeState, upgradesAvailable int) error {
	m.Log.V(consts.LogLevelInfo).Info("ProcessUpgradeRequiredNodes")
	for _, nodeState := range currentClusterState.NodeStates[UpgradeStateUpgradeRequired] {
		if m.skipNodeUpgrade(nodeState.Node) {
			m.Log.V(consts.LogLevelInfo).Info("Node is marked for skipping upgrades", "node", nodeState.Node.Name)
			continue
		}

		if upgradesAvailable <= 0 {
			// when no new node upgrades are available, progess with manually cordoned nodes
			if m.isNodeUnschedulable(nodeState.Node) {
				m.Log.V(consts.LogLevelDebug).Info("Node is already cordoned, progressing for driver upgrade", "node", nodeState.Node.Name)
			} else {
				m.Log.V(consts.LogLevelDebug).Info("Node upgrade limit reached, pausing further upgrades", "node", nodeState.Node.Name)
				continue
			}
		}

		err := m.NodeUpgradeStateProvider.ChangeNodeUpgradeState(ctx, nodeState.Node, UpgradeStateCordonRequired)
		if err == nil {
			upgradesAvailable--
			m.Log.V(consts.LogLevelInfo).Info("Node waiting for cordon",
				"node", nodeState.Node.Name)
		} else {
			m.Log.V(consts.LogLevelError).Error(
				err, "Failed to change node upgrade state", "state", UpgradeStateCordonRequired)
			return err
		}
	}

	return nil
}

// ProcessCordonRequiredNodes processes UpgradeStateCordonRequired nodes,
// cordons them and moves them to UpgradeStateWaitForJobsRequired state
func (m *ClusterUpgradeStateManager) ProcessCordonRequiredNodes(
	ctx context.Context, currentClusterState *ClusterUpgradeState) error {
	m.Log.V(consts.LogLevelInfo).Info("ProcessCordonRequiredNodes")

	for _, nodeState := range currentClusterState.NodeStates[UpgradeStateCordonRequired] {
		err := m.CordonManager.Cordon(ctx, nodeState.Node)
		if err != nil {
			m.Log.V(consts.LogLevelWarning).Error(
				err, "Node cordon failed", "node", nodeState.Node)
			return err
		}
		err = m.NodeUpgradeStateProvider.ChangeNodeUpgradeState(ctx, nodeState.Node, UpgradeStateWaitForJobsRequired)
		if err != nil {
			m.Log.V(consts.LogLevelError).Error(
				err, "Failed to change node upgrade state", "state", UpgradeStateWaitForJobsRequired)
			return err
		}
	}
	return nil
}

// ProcessWaitForJobsRequiredNodes processes UpgradeStateWaitForJobsRequired nodes,
// waits for completion of jobs and moves them to UpgradeStatePodDeletionRequired state.
func (m *ClusterUpgradeStateManager) ProcessWaitForJobsRequiredNodes(
	ctx context.Context, currentClusterState *ClusterUpgradeState, waitForCompletionSpec *v1alpha1.WaitForCompletionSpec) error {
	m.Log.V(consts.LogLevelInfo).Info("ProcessWaitForJobsRequiredNodes")

	var nodes []*corev1.Node
	for _, nodeState := range currentClusterState.NodeStates[UpgradeStateWaitForJobsRequired] {
		nodes = append(nodes, nodeState.Node)
		if waitForCompletionSpec == nil || waitForCompletionSpec.PodSelector == "" {
			// update node state to next state as no pod selector is specified for waiting
			m.Log.V(consts.LogLevelInfo).Info("No jobs to wait for as no pod selector was provided. Moving to next state.")
			nextState := UpgradeStatePodDeletionRequired
			if !m.IsPodDeletionEnabled() {
				nextState = UpgradeStateDrainRequired
			}
			_ = m.NodeUpgradeStateProvider.ChangeNodeUpgradeState(ctx, nodeState.Node, nextState)
			m.Log.V(consts.LogLevelInfo).Info("Updated the node state", "node", nodeState.Node.Name, "state", nextState)
		}
	}
	// return if no pod selector is provided for waiting
	if waitForCompletionSpec == nil || waitForCompletionSpec.PodSelector == "" {
		return nil
	}

	if len(nodes) == 0 {
		// no nodes to process in this state
		return nil
	}

	podManagerConfig := PodManagerConfig{WaitForCompletionSpec: waitForCompletionSpec, Nodes: nodes}
	err := m.PodManager.ScheduleCheckOnPodCompletion(ctx, &podManagerConfig)
	if err != nil {
		return err
	}
	return nil
}

// ProcessPodDeletionRequiredNodes processes UpgradeStatePodDeletionRequired nodes,
// deletes select pods on a node, and moves the nodes to UpgradeStateDrainRequiredRequired state.
// Pods selected for deletion are determined via PodManager.PodDeletion
func (m *ClusterUpgradeStateManager) ProcessPodDeletionRequiredNodes(
	ctx context.Context, currentClusterState *ClusterUpgradeState, podDeletionSpec *v1alpha1.PodDeletionSpec, drainEnabled bool) error {
	m.Log.V(consts.LogLevelInfo).Info("ProcessPodDeletionRequiredNodes")

	podManagerConfig := PodManagerConfig{
		DeletionSpec: podDeletionSpec,
		DrainEnabled: drainEnabled,
		Nodes:        make([]*corev1.Node, 0, len(currentClusterState.NodeStates[UpgradeStatePodDeletionRequired])),
	}

	for _, nodeState := range currentClusterState.NodeStates[UpgradeStatePodDeletionRequired] {
		podManagerConfig.Nodes = append(podManagerConfig.Nodes, nodeState.Node)
	}

	if len(podManagerConfig.Nodes) == 0 {
		// no nodes to process in this state
		return nil
	}

	return m.PodManager.SchedulePodEviction(ctx, &podManagerConfig)
}

// ProcessDrainNodes schedules UpgradeStateDrainRequired nodes for drain.
// If drain is disabled by upgrade policy, moves the nodes straight to UpgradeStatePodRestartRequired state.
func (m *ClusterUpgradeStateManager) ProcessDrainNodes(
	ctx context.Context, currentClusterState *ClusterUpgradeState, drainSpec *v1alpha1.DrainSpec) error {
	m.Log.V(consts.LogLevelInfo).Info("ProcessDrainNodes")
	if drainSpec == nil || !drainSpec.Enable {
		// If node drain is disabled, move nodes straight to PodRestart stage
		m.Log.V(consts.LogLevelInfo).Info("Node drain is disabled by policy, skipping this step")
		for _, nodeState := range currentClusterState.NodeStates[UpgradeStateDrainRequired] {
			err := m.NodeUpgradeStateProvider.ChangeNodeUpgradeState(ctx, nodeState.Node, UpgradeStatePodRestartRequired)
			if err != nil {
				m.Log.V(consts.LogLevelError).Error(
					err, "Failed to change node upgrade state", "state", UpgradeStatePodRestartRequired)
				return err
			}
		}
		return nil
	}

	// We want to skip operator itself during the drain because the upgrade process might hang
	// if the operator is evicted and can't be rescheduled to any other node, e.g. in a single-node cluster.
	// It's safe to do because the goal of the node draining during the upgrade is to
	// evict pods that might use driver and operator doesn't use in its own pod.
	skipDrainPodSelector := fmt.Sprintf("%s!=true", GetUpgradeSkipDrainPodLabelKey())
	if drainSpec.PodSelector == "" {
		drainSpec.PodSelector = skipDrainPodSelector
	} else {
		drainSpec.PodSelector = fmt.Sprintf("%s,%s", drainSpec.PodSelector, skipDrainPodSelector)
	}

	drainConfig := DrainConfiguration{
		Spec:  drainSpec,
		Nodes: make([]*corev1.Node, 0, len(currentClusterState.NodeStates[UpgradeStateDrainRequired])),
	}
	for _, nodeState := range currentClusterState.NodeStates[UpgradeStateDrainRequired] {
		drainConfig.Nodes = append(drainConfig.Nodes, nodeState.Node)
	}

	return m.DrainManager.ScheduleNodesDrain(ctx, &drainConfig)
}

// ProcessPodRestartNodes processes UpgradeStatePodRestartRequirednodes and schedules driver pod restart for them.
// If the pod has already been restarted and is in Ready state - moves the node to UpgradeStateUncordonRequired state.
func (m *ClusterUpgradeStateManager) ProcessPodRestartNodes(
	ctx context.Context, currentClusterState *ClusterUpgradeState) error {
	m.Log.V(consts.LogLevelInfo).Info("ProcessPodRestartNodes")

	pods := make([]*corev1.Pod, 0, len(currentClusterState.NodeStates[UpgradeStatePodRestartRequired]))
	for _, nodeState := range currentClusterState.NodeStates[UpgradeStatePodRestartRequired] {
		podRevisionHash, err := m.PodManager.GetPodControllerRevisionHash(ctx, nodeState.DriverPod)
		if err != nil {
			m.Log.V(consts.LogLevelError).Error(
				err, "Failed to get pod template revision hash", "pod", nodeState.DriverPod)
			return err
		}
		m.Log.V(consts.LogLevelDebug).Info("pod template revision hash", "hash", podRevisionHash)
		daemonsetRevisionHash, err := m.PodManager.GetDaemonsetControllerRevisionHash(ctx, nodeState.DriverDaemonSet)
		if err != nil {
			m.Log.V(consts.LogLevelError).Error(
				err, "Failed to get daemonset template revision hash", "daemonset", nodeState.DriverDaemonSet)
			return err
		}
		m.Log.V(consts.LogLevelDebug).Info("daemonset template revision hash", "hash", daemonsetRevisionHash)
		if podRevisionHash != daemonsetRevisionHash {
			// Pods should only be scheduled for restart if they are not terminating or restarting already
			// To determinate terminating state we need to check for deletion timestamp with will be filled
			// one pod termination process started
			if nodeState.DriverPod.ObjectMeta.DeletionTimestamp.IsZero() {
				pods = append(pods, nodeState.DriverPod)
			}
		} else {
			driverPodInSync, err := m.isDriverPodInSync(ctx, nodeState)
			if err != nil {
				m.Log.V(consts.LogLevelError).Error(
					err, "Failed to check if driver pod on the node is in sync", "nodeState", nodeState)
				return err
			}
			if driverPodInSync {
				if !m.IsValidationEnabled() {
					err = m.updateNodeToUncordonOrDoneState(ctx, nodeState.Node)
					if err != nil {
						return err
					}
					continue
				}

				err = m.NodeUpgradeStateProvider.ChangeNodeUpgradeState(ctx, nodeState.Node, UpgradeStateValidationRequired)
				if err != nil {
					m.Log.V(consts.LogLevelError).Error(
						err, "Failed to change node upgrade state", "state", UpgradeStateValidationRequired)
					return err
				}
			} else {
				// driver pod not in sync, move node to failed state if repeated container restarts
				if !m.isDriverPodFailing(nodeState.DriverPod) {
					continue
				}
				m.Log.V(consts.LogLevelInfo).Info("Driver pod is failing on node with repeated restarts",
					"node", nodeState.Node.Name, "pod", nodeState.DriverPod.Name)
				err = m.NodeUpgradeStateProvider.ChangeNodeUpgradeState(ctx, nodeState.Node, UpgradeStateFailed)
				if err != nil {
					m.Log.V(consts.LogLevelError).Error(
						err, "Failed to change node upgrade state for node", "node", nodeState.Node.Name, "state", UpgradeStateFailed)
					return err
				}
			}
		}
	}

	// Create pod restart manager to handle pod restarts
	return m.PodManager.SchedulePodsRestart(ctx, pods)
}

// ProcessUpgradeFailedNodes processes UpgradeStateFailed nodes and checks whether the driver pod on the node
// has been successfully restarted. If the pod is in Ready state - moves the node to UpgradeStateUncordonRequired state.
func (m *ClusterUpgradeStateManager) ProcessUpgradeFailedNodes(
	ctx context.Context, currentClusterState *ClusterUpgradeState) error {
	m.Log.V(consts.LogLevelInfo).Info("ProcessUpgradeFailedNodes")

	for _, nodeState := range currentClusterState.NodeStates[UpgradeStateFailed] {
		driverPodInSync, err := m.isDriverPodInSync(ctx, nodeState)
		if err != nil {
			m.Log.V(consts.LogLevelError).Error(
				err, "Failed to check if driver pod on the node is in sync", "nodeState", nodeState)
			return err
		}
		if driverPodInSync {
			newUpgradeState := UpgradeStateUncordonRequired
			// If node was Unschedulable at beginning of upgrade, skip the
			// uncordon state so that node remains in the same state as
			// when the upgrade started.
			annotationKey := GetUpgradeInitialStateAnnotationKey()
			if _, ok := nodeState.Node.Annotations[annotationKey]; ok {
				m.Log.V(consts.LogLevelInfo).Info("Node was Unschedulable at beginning of upgrade, skipping uncordon",
					"node", nodeState.Node.Name)
				newUpgradeState = UpgradeStateDone
			}

			err = m.NodeUpgradeStateProvider.ChangeNodeUpgradeState(ctx, nodeState.Node, newUpgradeState)
			if err != nil {
				m.Log.V(consts.LogLevelError).Error(
					err, "Failed to change node upgrade state", "state", newUpgradeState)
				return err
			}

			if newUpgradeState == UpgradeStateDone {
				m.Log.V(consts.LogLevelDebug).Info("Removing node upgrade annotation",
					"node", nodeState.Node.Name, "annotation", annotationKey)
				err = m.NodeUpgradeStateProvider.ChangeNodeUpgradeAnnotation(ctx, nodeState.Node, annotationKey, "null")
				if err != nil {
					return err
				}
			}
		}
	}

	return nil
}

// ProcessValidationRequiredNodes
func (m *ClusterUpgradeStateManager) ProcessValidationRequiredNodes(
	ctx context.Context, currentClusterState *ClusterUpgradeState) error {
	m.Log.V(consts.LogLevelInfo).Info("ProcessValidationRequiredNodes")

	for _, nodeState := range currentClusterState.NodeStates[UpgradeStateValidationRequired] {
		node := nodeState.Node
		validationDone, err := m.ValidationManager.Validate(ctx, node)
		if err != nil {
			m.Log.V(consts.LogLevelError).Error(err, "Failed to validate driver upgrade", "node", node.Name)
			return err
		}

		if !validationDone {
			m.Log.V(consts.LogLevelInfo).Info("Validations not complete on the node", "node", node.Name)
			continue
		}

		err = m.updateNodeToUncordonOrDoneState(ctx, node)
		if err != nil {
			return err
		}
	}
	return nil
}

// ProcessUncordonRequiredNodes processes UpgradeStateUncordonRequired nodes,
// uncordons them and moves them to UpgradeStateDone state
func (m *ClusterUpgradeStateManager) ProcessUncordonRequiredNodes(
	ctx context.Context, currentClusterState *ClusterUpgradeState) error {
	m.Log.V(consts.LogLevelInfo).Info("ProcessUncordonRequiredNodes")

	for _, nodeState := range currentClusterState.NodeStates[UpgradeStateUncordonRequired] {
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

func (m *ClusterUpgradeStateManager) isDriverPodInSync(ctx context.Context, nodeState *NodeUpgradeState) (bool, error) {
	podRevisionHash, err := m.PodManager.GetPodControllerRevisionHash(ctx, nodeState.DriverPod)
	if err != nil {
		m.Log.V(consts.LogLevelError).Error(
			err, "Failed to get pod template revision hash", "pod", nodeState.DriverPod)
		return false, err
	}
	m.Log.V(consts.LogLevelDebug).Info("pod template revision hash", "hash", podRevisionHash)
	daemonsetRevisionHash, err := m.PodManager.GetDaemonsetControllerRevisionHash(ctx, nodeState.DriverDaemonSet)
	if err != nil {
		m.Log.V(consts.LogLevelError).Error(
			err, "Failed to get daemonset template revision hash", "daemonset", nodeState.DriverDaemonSet)
		return false, err
	}
	m.Log.V(consts.LogLevelDebug).Info("daemonset template revision hash", "hash", daemonsetRevisionHash)
	// If the pod generation matches the daemonset generation
	if podRevisionHash == daemonsetRevisionHash &&
		// And the pod is running
		nodeState.DriverPod.Status.Phase == "Running" &&
		// And it has at least 1 container
		len(nodeState.DriverPod.Status.ContainerStatuses) != 0 {
		for i := range nodeState.DriverPod.Status.ContainerStatuses {
			if !nodeState.DriverPod.Status.ContainerStatuses[i].Ready {
				// Return false if at least 1 container isn't ready
				return false, nil
			}
		}

		// And each container is ready
		return true, nil
	}

	return false, nil
}

func (m *ClusterUpgradeStateManager) isDriverPodFailing(pod *corev1.Pod) bool {
	for _, status := range pod.Status.InitContainerStatuses {
		if !status.Ready && status.RestartCount > 10 {
			return true
		}
	}
	for _, status := range pod.Status.ContainerStatuses {
		if !status.Ready && status.RestartCount > 10 {
			return true
		}
	}
	return false
}

// isNodeUnschedulable returns true if the node is cordoned
func (m *ClusterUpgradeStateManager) isNodeUnschedulable(node *corev1.Node) bool {
	return node.Spec.Unschedulable
}

// isNodeConditionReady returns true if the node condition is ready
func (m *ClusterUpgradeStateManager) isNodeConditionReady(node *corev1.Node) bool {
	for _, condition := range node.Status.Conditions {
		if condition.Type == corev1.NodeReady && condition.Status != corev1.ConditionTrue {
			return false
		}
	}
	return true
}

// skipNodeUpgrade returns true if node is labelled to skip driver upgrades
func (m *ClusterUpgradeStateManager) skipNodeUpgrade(node *corev1.Node) bool {
	if node.Labels[GetUpgradeSkipNodeLabelKey()] == "true" {
		return true
	}
	return false
}

// updateNodeToUncordonOrDoneState skips moving the node to the UncordonRequired state if the node
// was Unschedulable at the beginning of the upgrade so that the node remains in the same state as
// when the ugprade started. In addition, the annotation tracking this information is removed.
func (m *ClusterUpgradeStateManager) updateNodeToUncordonOrDoneState(ctx context.Context, node *corev1.Node) error {
	newUpgradeState := UpgradeStateUncordonRequired
	annotationKey := GetUpgradeInitialStateAnnotationKey()
	if _, ok := node.Annotations[annotationKey]; ok {
		m.Log.V(consts.LogLevelInfo).Info("Node was Unschedulable at beginning of upgrade, skipping uncordon",
			"node", node.Name)
		newUpgradeState = UpgradeStateDone
	}

	err := m.NodeUpgradeStateProvider.ChangeNodeUpgradeState(ctx, node, newUpgradeState)
	if err != nil {
		m.Log.V(consts.LogLevelError).Error(
			err, "Failed to change node upgrade state", "node", node.Name, "state", newUpgradeState)
		return err
	}

	if newUpgradeState == UpgradeStateDone {
		m.Log.V(consts.LogLevelDebug).Info("Removing node upgrade annotation",
			"node", node.Name, "annotation", annotationKey)
		err = m.NodeUpgradeStateProvider.ChangeNodeUpgradeAnnotation(ctx, node, annotationKey, "null")
		if err != nil {
			return err
		}
	}
	return nil
}

func isNodeUnschedulable(node *corev1.Node) bool {
	if node.Spec.Unschedulable {
		return true
	}
	return false
}

// GetTotalManagedNodes returns the total count of nodes managed for driver upgrades
func (m *ClusterUpgradeStateManager) GetTotalManagedNodes(ctx context.Context,
	currentState *ClusterUpgradeState) int {
	totalNodes := len(currentState.NodeStates[UpgradeStateUnknown]) +
		len(currentState.NodeStates[UpgradeStateDone]) +
		len(currentState.NodeStates[UpgradeStateUpgradeRequired]) +
		len(currentState.NodeStates[UpgradeStateCordonRequired]) +
		len(currentState.NodeStates[UpgradeStateWaitForJobsRequired]) +
		len(currentState.NodeStates[UpgradeStatePodDeletionRequired]) +
		len(currentState.NodeStates[UpgradeStateFailed]) +
		len(currentState.NodeStates[UpgradeStateDrainRequired]) +
		len(currentState.NodeStates[UpgradeStatePodRestartRequired]) +
		len(currentState.NodeStates[UpgradeStateUncordonRequired]) +
		len(currentState.NodeStates[UpgradeStateValidationRequired])

	return totalNodes
}

// GetUpgradesInProgress returns count of nodes on which upgrade is in progress
func (m *ClusterUpgradeStateManager) GetUpgradesInProgress(ctx context.Context,
	currentState *ClusterUpgradeState) int {
	totalNodes := m.GetTotalManagedNodes(ctx, currentState)
	return totalNodes - (len(currentState.NodeStates[UpgradeStateUnknown]) +
		len(currentState.NodeStates[UpgradeStateDone]) +
		len(currentState.NodeStates[UpgradeStateUpgradeRequired]))
}

// GetUpgradesDone returns count of nodes on which upgrade is complete
func (m *ClusterUpgradeStateManager) GetUpgradesDone(ctx context.Context,
	currentState *ClusterUpgradeState) int {
	return len(currentState.NodeStates[UpgradeStateDone])
}

// GetUpgradesAvailable returns count of nodes on which upgrade can be done
func (m *ClusterUpgradeStateManager) GetUpgradesAvailable(ctx context.Context,
	currentState *ClusterUpgradeState, maxParallelUpgrades int, maxUnavailable int) int {
	upgradesInProgress := m.GetUpgradesInProgress(ctx, currentState)
	totalNodes := m.GetTotalManagedNodes(ctx, currentState)

	var upgradesAvailable int
	if maxParallelUpgrades == 0 {
		// Only nodes in UpgradeStateUpgradeRequired can start upgrading, so all of them will move to drain stage
		upgradesAvailable = len(currentState.NodeStates[UpgradeStateUpgradeRequired])
	} else {
		upgradesAvailable = maxParallelUpgrades - upgradesInProgress
	}

	// Apply the maxUnavailable constraint based on the number of nodes unavailable in the cluster
	// Get nodes in cordoned/not-ready state and also include nodes that are about to be cordoned.
	currentUnavailableNodes := m.GetCurrentUnavailableNodes(ctx, currentState) + len(currentState.NodeStates[UpgradeStateCordonRequired])
	// always limit upgradesAvailalbe to maxUnavailable
	if upgradesAvailable > maxUnavailable {
		upgradesAvailable = maxUnavailable
	}
	// apply additional limits when there are already unavailable nodes
	if currentUnavailableNodes >= maxUnavailable {
		upgradesAvailable = 0
	} else if maxUnavailable < totalNodes && currentUnavailableNodes+upgradesAvailable > maxUnavailable {
		upgradesAvailable = maxUnavailable - currentUnavailableNodes
	}
	return upgradesAvailable
}

// GetUpgradesFailed returns count of nodes on which upgrades have failed
func (m *ClusterUpgradeStateManager) GetUpgradesFailed(ctx context.Context,
	currentState *ClusterUpgradeState) int {
	return len(currentState.NodeStates[UpgradeStateFailed])
}

// GetUpgradesPending returns count of nodes on which are marked for upgrades and upgrade is pending
func (m *ClusterUpgradeStateManager) GetUpgradesPending(ctx context.Context,
	currentState *ClusterUpgradeState) int {
	return len(currentState.NodeStates[UpgradeStateUpgradeRequired])
}
