package upgrade

import (
	"context"
	"fmt"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"

	"github.com/go-logr/logr"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/record"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/NVIDIA/k8s-operator-libs/api/upgrade/v1alpha1"
	"github.com/NVIDIA/k8s-operator-libs/pkg/consts"
)

// CommonUpgradeStateManager interface is a unified cluster upgrade abstraction for both upgrade modes
type CommonUpgradeStateManager interface {
	// GetTotalManagedNodes returns the total count of nodes managed for driver upgrades
	GetTotalManagedNodes(currentState *ClusterUpgradeState) int
	// GetUpgradesInProgress returns count of nodes on which upgrade is in progress
	GetUpgradesInProgress(currentState *ClusterUpgradeState) int
	// GetUpgradesDone returns count of nodes on which upgrade is complete
	GetUpgradesDone(currentState *ClusterUpgradeState) int
	// GetUpgradesAvailable returns count of nodes on which upgrade can be done
	GetUpgradesAvailable(currentState *ClusterUpgradeState, maxParallelUpgrades int,
		maxUnavailable int) int
	// GetUpgradesFailed returns count of nodes on which upgrades have failed
	GetUpgradesFailed(currentState *ClusterUpgradeState) int
	// GetUpgradesPending returns count of nodes on which are marked for upgrades and upgrade is pending
	GetUpgradesPending(currentState *ClusterUpgradeState) int
	// IsPodDeletionEnabled returns true if 'pod-deletion' state is enabled
	IsPodDeletionEnabled() bool
	// IsValidationEnabled returns true if 'validation' state is enabled
	IsValidationEnabled() bool
}

// ProcessNodeStateManager interface is used for abstracting both upgrade modes: in-place,
// requestor (e.g. maintenance OP)
// Similar node states are used in both modes, while changes are introduced within ApplyState Process<state>
// methods to support both modes logic
type ProcessNodeStateManager interface {
	ProcessUpgradeRequiredNodes(ctx context.Context,
		currentClusterState *ClusterUpgradeState, upgradePolicy *v1alpha1.DriverUpgradePolicySpec) error
	ProcessNodeMaintenanceRequiredNodes(ctx context.Context,
		currentClusterState *ClusterUpgradeState) error
	ProcessUncordonRequiredNodes(
		ctx context.Context, currentClusterState *ClusterUpgradeState) error
}

// NodeUpgradeState contains a mapping between a node,
// the driver POD running on them and the daemon set, controlling this pod
type NodeUpgradeState struct {
	Node            *corev1.Node
	DriverPod       *corev1.Pod
	DriverDaemonSet *appsv1.DaemonSet
	NodeMaintenance client.Object
}

// IsOrphanedPod returns true if Pod is not associated to a DaemonSet
func (nus *NodeUpgradeState) IsOrphanedPod() bool {
	return nus.DriverDaemonSet == nil
}

// ClusterUpgradeState contains a snapshot of the driver upgrade state in the cluster
// Nodes are grouped together with the driver POD running on them and the daemon set, controlling this pod
// This state is then used as an input for the ClusterUpgradeStateManager
type ClusterUpgradeState struct {
	NodeStates map[string][]*NodeUpgradeState
}

// NewClusterUpgradeState creates an empty ClusterUpgradeState object
func NewClusterUpgradeState() ClusterUpgradeState {
	return ClusterUpgradeState{NodeStates: make(map[string][]*NodeUpgradeState)}
}

// CommonUpgradeManagerImpl is an implementation of the CommonUpgradeStateManager interface.
// It facilitates common logic implementation for both upgrade modes: in-place and requestor (e.g. maintenance OP).
type CommonUpgradeManagerImpl struct {
	Log           logr.Logger
	K8sClient     client.Client
	K8sInterface  kubernetes.Interface
	EventRecorder record.EventRecorder

	DrainManager             DrainManager
	PodManager               PodManager
	CordonManager            CordonManager
	NodeUpgradeStateProvider NodeUpgradeStateProvider
	ValidationManager        ValidationManager
	SafeDriverLoadManager    SafeDriverLoadManager

	// optional states
	podDeletionStateEnabled bool
	validationStateEnabled  bool
}

// NewCommonUpgradeStateManager creates a new instance of CommonUpgradeManagerImpl
func NewCommonUpgradeStateManager(
	log logr.Logger,
	k8sConfig *rest.Config,
	scheme *runtime.Scheme,
	eventRecorder record.EventRecorder) (*CommonUpgradeManagerImpl, error) {
	k8sClient, err := client.New(k8sConfig, client.Options{Scheme: scheme})
	if err != nil {
		return &CommonUpgradeManagerImpl{}, fmt.Errorf("error creating k8s client: %v", err)
	}

	k8sInterface, err := kubernetes.NewForConfig(k8sConfig)
	if err != nil {
		return &CommonUpgradeManagerImpl{}, fmt.Errorf("error creating k8s interface: %v", err)
	}

	nodeUpgradeStateProvider := NewNodeUpgradeStateProvider(k8sClient, log, eventRecorder)
	commonUpgrade := CommonUpgradeManagerImpl{
		Log:                      log,
		K8sClient:                k8sClient,
		K8sInterface:             k8sInterface,
		EventRecorder:            eventRecorder,
		DrainManager:             NewDrainManager(k8sInterface, nodeUpgradeStateProvider, log, eventRecorder),
		PodManager:               NewPodManager(k8sInterface, nodeUpgradeStateProvider, log, nil, eventRecorder),
		CordonManager:            NewCordonManager(k8sInterface, log),
		NodeUpgradeStateProvider: nodeUpgradeStateProvider,
		ValidationManager:        NewValidationManager(k8sInterface, log, eventRecorder, nodeUpgradeStateProvider, ""),
		SafeDriverLoadManager:    NewSafeDriverLoadManager(nodeUpgradeStateProvider, log),
	}

	return &commonUpgrade, nil
}

// IsPodDeletionEnabled returns true if 'pod-deletion' state is enabled
func (m *CommonUpgradeManagerImpl) IsPodDeletionEnabled() bool {
	return m.podDeletionStateEnabled
}

// IsValidationEnabled returns true if 'validation' state is enabled
func (m *CommonUpgradeManagerImpl) IsValidationEnabled() bool {
	return m.validationStateEnabled
}

// GetCurrentUnavailableNodes returns all nodes that are not in ready state
func (m *CommonUpgradeManagerImpl) GetCurrentUnavailableNodes(
	currentState *ClusterUpgradeState) int {
	unavailableNodes := 0
	for _, nodeUpgradeStateList := range currentState.NodeStates {
		for _, nodeUpgradeState := range nodeUpgradeStateList {
			// check if the node is cordoned
			if m.IsNodeUnschedulable(nodeUpgradeState.Node) {
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

// GetDriverDaemonSets retrieves DaemonSets with given labels and returns UID->DaemonSet map
func (m *CommonUpgradeManagerImpl) GetDriverDaemonSets(ctx context.Context, namespace string,
	labels map[string]string) (map[types.UID]*appsv1.DaemonSet, error) {
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

// GetPodsOwnedbyDs returns a list of the pods owned by the specified DaemonSet
func (m *CommonUpgradeManagerImpl) GetPodsOwnedbyDs(ds *appsv1.DaemonSet, pods []corev1.Pod) []corev1.Pod {
	dsPodList := []corev1.Pod{}
	for i := range pods {
		pod := &pods[i]
		if IsOrphanedPod(pod) {
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

// GetOrphanedPods returns a list of the pods not owned by any DaemonSet
func (m *CommonUpgradeManagerImpl) GetOrphanedPods(pods []corev1.Pod) []corev1.Pod {
	podList := []corev1.Pod{}
	for i := range pods {
		pod := &pods[i]
		if IsOrphanedPod(pod) {
			podList = append(podList, *pod)
		}
	}
	m.Log.V(consts.LogLevelInfo).Info("Total orphaned Pods found:", "count", len(podList))
	return podList
}

func IsOrphanedPod(pod *corev1.Pod) bool {
	return len(pod.OwnerReferences) < 1
}

// ProcessDoneOrUnknownNodes iterates over UpgradeStateDone or UpgradeStateUnknown nodes and determines
// whether each specific node should be in UpgradeStateUpgradeRequired or UpgradeStateDone state.
func (m *CommonUpgradeManagerImpl) ProcessDoneOrUnknownNodes(
	ctx context.Context, currentClusterState *ClusterUpgradeState, nodeStateName string) error {
	m.Log.V(consts.LogLevelInfo).Info("ProcessDoneOrUnknownNodes")

	for _, nodeState := range currentClusterState.NodeStates[nodeStateName] {
		isPodSynced, isOrphaned, err := m.podInSyncWithDS(ctx, nodeState)
		if err != nil {
			m.Log.V(consts.LogLevelError).Error(err, "Failed to get daemonset template/pod revision hash")
			return err
		}
		isUpgradeRequested := m.IsUpgradeRequested(nodeState.Node)
		isWaitingForSafeDriverLoad, err := m.SafeDriverLoadManager.IsWaitingForSafeDriverLoad(ctx, nodeState.Node)
		if err != nil {
			m.Log.V(consts.LogLevelError).Error(
				err, "Failed to check safe driver load status for the node", "node", nodeState.Node.Name)
			return err
		}
		if isWaitingForSafeDriverLoad {
			m.Log.V(consts.LogLevelInfo).Info("Node is waiting for safe driver load, initialize upgrade",
				"node", nodeState.Node.Name)
		}
		if (!isPodSynced && !isOrphaned) || isWaitingForSafeDriverLoad || isUpgradeRequested {
			// If node requires upgrade and is Unschedulable, track this in an
			// annotation and leave node in Unschedulable state when upgrade completes.
			if IsNodeUnschedulable(nodeState.Node) {
				annotationKey := GetUpgradeInitialStateAnnotationKey()
				annotationValue := trueString
				m.Log.V(consts.LogLevelInfo).Info(
					"Node is unschedulable, adding annotation to track initial state of the node",
					"node", nodeState.Node.Name, "annotation", annotationKey)
				err = m.NodeUpgradeStateProvider.ChangeNodeUpgradeAnnotation(ctx, nodeState.Node, annotationKey,
					annotationValue)
				if err != nil {
					return err
				}
			}
			err := m.NodeUpgradeStateProvider.ChangeNodeUpgradeState(ctx, nodeState.Node, UpgradeStateUpgradeRequired)
			if err != nil {
				m.Log.V(consts.LogLevelError).Error(
					err, "Failed to change node upgrade state", "state", UpgradeStateUpgradeRequired, "node:", nodeState.Node)
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

// podInSyncWithDS check if pod is in sync with DaemonSet, handling also Orphaned Pod
// Returns:
//
//	bool: True if Pod is in sync with DaemonSet. (For Orphanded Pods, always false)
//	bool: True if the Pod is Orphaned
//	error: In case of error retrivieng the Revision Hashes
func (m *CommonUpgradeManagerImpl) podInSyncWithDS(ctx context.Context,
	nodeState *NodeUpgradeState) (isPodSynced, isOrphened bool, err error) {
	if isOrphened = nodeState.IsOrphanedPod(); isOrphened {
		return isPodSynced, isOrphened, nil
	}
	podRevisionHash, err := m.PodManager.GetPodControllerRevisionHash(nodeState.DriverPod)
	if err != nil {
		m.Log.V(consts.LogLevelError).Error(
			err, "Failed to get pod template revision hash", "pod", nodeState.DriverPod)
		return isPodSynced, isOrphened, err
	}
	m.Log.V(consts.LogLevelDebug).Info("pod template revision hash", "hash", podRevisionHash)
	daemonsetRevisionHash, err := m.PodManager.GetDaemonsetControllerRevisionHash(ctx, nodeState.DriverDaemonSet)
	if err != nil {
		m.Log.V(consts.LogLevelError).Error(
			err, "Failed to get daemonset template revision hash", "daemonset", nodeState.DriverDaemonSet)
		return isPodSynced, isOrphened, err
	}
	m.Log.V(consts.LogLevelDebug).Info("daemonset template revision hash", "hash", daemonsetRevisionHash)
	isPodSynced = podRevisionHash == daemonsetRevisionHash
	return isPodSynced, isOrphened, nil
}

// isUpgradeRequested returns true if node is labeled to request an upgrade
func (m *CommonUpgradeManagerImpl) IsUpgradeRequested(node *corev1.Node) bool {
	return node.Annotations[GetUpgradeRequestedAnnotationKey()] == trueString
}

// ProcessDrainNodes schedules UpgradeStateDrainRequired nodes for drain.
// If drain is disabled by upgrade policy, moves the nodes straight to UpgradeStatePodRestartRequired state.
func (m *CommonUpgradeManagerImpl) ProcessDrainNodes(
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

	drainConfig := DrainConfiguration{
		Spec:  drainSpec,
		Nodes: make([]*corev1.Node, 0, len(currentClusterState.NodeStates[UpgradeStateDrainRequired])),
	}
	for _, nodeState := range currentClusterState.NodeStates[UpgradeStateDrainRequired] {
		drainConfig.Nodes = append(drainConfig.Nodes, nodeState.Node)
	}

	m.Log.V(consts.LogLevelInfo).Info("Scheduling nodes drain", "drainConfig", drainConfig)

	return m.DrainManager.ScheduleNodesDrain(ctx, &drainConfig)
}

// ProcessCordonRequiredNodes processes UpgradeStateCordonRequired nodes,
// cordons them and moves them to UpgradeStateWaitForJobsRequired state
func (m *CommonUpgradeManagerImpl) ProcessCordonRequiredNodes(
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
func (m *CommonUpgradeManagerImpl) ProcessWaitForJobsRequiredNodes(
	ctx context.Context, currentClusterState *ClusterUpgradeState,
	waitForCompletionSpec *v1alpha1.WaitForCompletionSpec) error {
	m.Log.V(consts.LogLevelInfo).Info("ProcessWaitForJobsRequiredNodes")

	nodes := make([]*corev1.Node, 0, len(currentClusterState.NodeStates[UpgradeStateWaitForJobsRequired]))
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
func (m *CommonUpgradeManagerImpl) ProcessPodDeletionRequiredNodes(
	ctx context.Context, currentClusterState *ClusterUpgradeState, podDeletionSpec *v1alpha1.PodDeletionSpec,
	drainEnabled bool) error {
	m.Log.V(consts.LogLevelInfo).Info("ProcessPodDeletionRequiredNodes")

	if !m.IsPodDeletionEnabled() {
		m.Log.V(consts.LogLevelInfo).Info("PodDeletion is not enabled, proceeding straight to the next state")
		for _, nodeState := range currentClusterState.NodeStates[UpgradeStatePodDeletionRequired] {
			_ = m.NodeUpgradeStateProvider.ChangeNodeUpgradeState(ctx, nodeState.Node, UpgradeStateDrainRequired)
		}
		return nil
	}

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

// ProcessPodRestartNodes processes UpgradeStatePodRestartRequired nodes and schedules driver pod restart for them.
// If the pod has already been restarted and is in Ready state - moves the node to UpgradeStateUncordonRequired state.
func (m *CommonUpgradeManagerImpl) ProcessPodRestartNodes(
	ctx context.Context, currentClusterState *ClusterUpgradeState) error {
	m.Log.V(consts.LogLevelInfo).Info("ProcessPodRestartNodes")

	pods := make([]*corev1.Pod, 0, len(currentClusterState.NodeStates[UpgradeStatePodRestartRequired]))
	for _, nodeState := range currentClusterState.NodeStates[UpgradeStatePodRestartRequired] {
		isPodSynced, isOrphaned, err := m.podInSyncWithDS(ctx, nodeState)
		if err != nil {
			m.Log.V(consts.LogLevelError).Error(err, "Failed to get daemonset template/pod revision hash")
			return err
		}
		if !isPodSynced || isOrphaned {
			// Pods should only be scheduled for restart if they are not terminating or restarting already
			// To determinate terminating state we need to check for deletion timestamp which will be set
			// once pod termination process started
			if nodeState.DriverPod.DeletionTimestamp.IsZero() {
				pods = append(pods, nodeState.DriverPod)
			}
		} else {
			err := m.SafeDriverLoadManager.UnblockLoading(ctx, nodeState.Node)
			if err != nil {
				m.Log.V(consts.LogLevelError).Error(
					err, "Failed to unblock loading of the driver", "nodeState", nodeState)
				return err
			}
			driverPodInSync, err := m.isDriverPodInSync(ctx, nodeState)
			if err != nil {
				m.Log.V(consts.LogLevelError).Error(
					err, "Failed to check if driver pod on the node is in sync", "nodeState", nodeState)
				return err
			}
			if driverPodInSync {
				if !m.IsValidationEnabled() {
					err = m.updateNodeToUncordonOrDoneState(ctx, nodeState)
					if err != nil {
						return err
					}
					continue
				}

				err = m.NodeUpgradeStateProvider.ChangeNodeUpgradeState(ctx, nodeState.Node,
					UpgradeStateValidationRequired)
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
						err, "Failed to change node upgrade state for node", "node", nodeState.Node.Name,
						"state", UpgradeStateFailed)
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
func (m *CommonUpgradeManagerImpl) ProcessUpgradeFailedNodes(
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

// ProcessValidationRequiredNodes processes UpgradeStateValidationRequired nodes
func (m *CommonUpgradeManagerImpl) ProcessValidationRequiredNodes(
	ctx context.Context, currentClusterState *ClusterUpgradeState) error {
	m.Log.V(consts.LogLevelInfo).Info("ProcessValidationRequiredNodes")

	for _, nodeState := range currentClusterState.NodeStates[UpgradeStateValidationRequired] {
		node := nodeState.Node
		// make sure that the driver Pod is not waiting for the safe load,
		// this may happen in case if driver restarted after it was moved to UpgradeStateValidationRequired state
		err := m.SafeDriverLoadManager.UnblockLoading(ctx, nodeState.Node)
		if err != nil {
			m.Log.V(consts.LogLevelError).Error(
				err, "Failed to unblock loading of the driver", "nodeState", nodeState)
			return err
		}
		validationDone, err := m.ValidationManager.Validate(ctx, node)
		if err != nil {
			m.Log.V(consts.LogLevelError).Error(err, "Failed to validate driver upgrade", "node", node.Name)
			return err
		}

		if !validationDone {
			m.Log.V(consts.LogLevelInfo).Info("Validations not complete on the node", "node", node.Name)
			continue
		}

		err = m.updateNodeToUncordonOrDoneState(ctx, nodeState)
		if err != nil {
			return err
		}
	}
	return nil
}

func (m *CommonUpgradeManagerImpl) isDriverPodInSync(ctx context.Context,
	nodeState *NodeUpgradeState) (bool, error) {
	isPodSynced, isOrphaned, err := m.podInSyncWithDS(ctx, nodeState)
	if err != nil {
		m.Log.V(consts.LogLevelError).Error(err, "Failed to get daemonset template/pod revision hash")
		return false, err
	}
	if isOrphaned {
		return false, nil
	}
	// If the pod generation matches the daemonset generation
	if isPodSynced &&
		// And the pod is running
		nodeState.DriverPod.Status.Phase == corev1.PodRunning &&
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

func (m *CommonUpgradeManagerImpl) isDriverPodFailing(pod *corev1.Pod) bool {
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
func (m *CommonUpgradeManagerImpl) IsNodeUnschedulable(node *corev1.Node) bool {
	return node.Spec.Unschedulable
}

// isNodeConditionReady returns true if the node condition is ready
func (m *CommonUpgradeManagerImpl) isNodeConditionReady(node *corev1.Node) bool {
	for _, condition := range node.Status.Conditions {
		if condition.Type == corev1.NodeReady && condition.Status != corev1.ConditionTrue {
			return false
		}
	}
	return true
}

// skipNodeUpgrade returns true if node is labeled to skip driver upgrades
func (m *CommonUpgradeManagerImpl) SkipNodeUpgrade(node *corev1.Node) bool {
	return node.Labels[GetUpgradeSkipNodeLabelKey()] == trueString
}

// updateNodeToUncordonOrDoneState skips moving the node to the UncordonRequired state if the node
// was Unschedulable at the beginning of the upgrade so that the node remains in the same state as
// when the upgrade started. In addition, the annotation tracking this information is removed.
func (m *CommonUpgradeManagerImpl) updateNodeToUncordonOrDoneState(ctx context.Context,
	nodeState *NodeUpgradeState) error {
	node := nodeState.Node
	newUpgradeState := UpgradeStateUncordonRequired
	annotationKey := GetUpgradeInitialStateAnnotationKey()
	isNodeUnderRequestorMode := IsNodeInRequestorMode(node)

	if _, ok := node.Annotations[annotationKey]; ok {
		// check if node is already in requestor mode, if not do update node to 'upgrade-done',
		// otherwise node state will be handled by the requestor mode at uncordon-required completion
		if !isNodeUnderRequestorMode {
			m.Log.V(consts.LogLevelInfo).Info("Node was Unschedulable at beginning of upgrade, skipping uncordon",
				"node", node.Name)
			newUpgradeState = UpgradeStateDone
		}
	}

	err := m.NodeUpgradeStateProvider.ChangeNodeUpgradeState(ctx, node, newUpgradeState)
	if err != nil {
		m.Log.V(consts.LogLevelError).Error(
			err, "Failed to change node upgrade state", "node", node.Name, "state", newUpgradeState)
		return err
	}

	// remove initial state annotation if node is in in-place mode, and was set to 'upgrade-done',
	// or is in requestor mode
	if newUpgradeState == UpgradeStateDone || isNodeUnderRequestorMode {
		m.Log.V(consts.LogLevelDebug).Info("Removing node upgrade annotation",
			"node", node.Name, "annotation", annotationKey)
		err = m.NodeUpgradeStateProvider.ChangeNodeUpgradeAnnotation(ctx, node, annotationKey, "null")
		if err != nil {
			return err
		}
	}
	return nil
}

func IsNodeUnschedulable(node *corev1.Node) bool {
	return node.Spec.Unschedulable
}

// GetTotalManagedNodes returns the total count of nodes managed for driver upgrades
func (m *CommonUpgradeManagerImpl) GetTotalManagedNodes(
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
func (m *CommonUpgradeManagerImpl) GetUpgradesInProgress(
	currentState *ClusterUpgradeState) int {
	totalNodes := m.GetTotalManagedNodes(currentState)
	return totalNodes - (len(currentState.NodeStates[UpgradeStateUnknown]) +
		len(currentState.NodeStates[UpgradeStateDone]) +
		len(currentState.NodeStates[UpgradeStateUpgradeRequired]))
}

// GetUpgradesDone returns count of nodes on which upgrade is complete
func (m *CommonUpgradeManagerImpl) GetUpgradesDone(
	currentState *ClusterUpgradeState) int {
	return len(currentState.NodeStates[UpgradeStateDone])
}

// GetUpgradesAvailable returns count of nodes on which upgrade can be done
func (m *CommonUpgradeManagerImpl) GetUpgradesAvailable(
	currentState *ClusterUpgradeState, maxParallelUpgrades int, maxUnavailable int) int {
	upgradesInProgress := m.GetUpgradesInProgress(currentState)
	totalNodes := m.GetTotalManagedNodes(currentState)

	var upgradesAvailable int
	if maxParallelUpgrades == 0 {
		// Only nodes in UpgradeStateUpgradeRequired can start upgrading, so all of them will move to drain stage
		upgradesAvailable = len(currentState.NodeStates[UpgradeStateUpgradeRequired])
	} else {
		upgradesAvailable = maxParallelUpgrades - upgradesInProgress
	}

	// Apply the maxUnavailable constraint based on the number of nodes unavailable in the cluster
	// Get nodes in cordoned/not-ready state and also include nodes that are about to be cordoned.
	currentUnavailableNodes := m.GetCurrentUnavailableNodes(currentState) +
		len(currentState.NodeStates[UpgradeStateCordonRequired])
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
func (m *CommonUpgradeManagerImpl) GetUpgradesFailed(
	currentState *ClusterUpgradeState) int {
	return len(currentState.NodeStates[UpgradeStateFailed])
}

// GetUpgradesPending returns count of nodes on which are marked for upgrades and upgrade is pending
func (m *CommonUpgradeManagerImpl) GetUpgradesPending(
	currentState *ClusterUpgradeState) int {
	return len(currentState.NodeStates[UpgradeStateUpgradeRequired])
}
