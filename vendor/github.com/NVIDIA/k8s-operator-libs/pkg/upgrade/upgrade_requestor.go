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
	"cmp"
	"context"
	"errors"
	"fmt"
	"os"
	"reflect"
	"slices"

	//nolint:depguard
	maintenancev1alpha1 "github.com/Mellanox/maintenance-operator/api/v1alpha1"
	"github.com/go-logr/logr"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/event"
	"sigs.k8s.io/controller-runtime/pkg/predicate"

	"github.com/NVIDIA/k8s-operator-libs/api/upgrade/v1alpha1"
	"github.com/NVIDIA/k8s-operator-libs/pkg/consts"
)

const (
	// MaintenanceOPEvictionGPU is a default filter for GPU OP pods eviction
	MaintenanceOPEvictionGPU = "nvidia.com/gpu-*"
	// MaintenanceOPEvictionRDMA is a default filter for Network OP pods eviction
	MaintenanceOPEvictionRDMA = "nvidia.com/rdma*"
)

var (
	ErrNodeMaintenanceUpgradeDisabled = errors.New("node maintenance upgrade mode is disabled")
	defaultNodeMaintenance            *maintenancev1alpha1.NodeMaintenance
	Scheme                            = runtime.NewScheme()
)

type ConditionChangedPredicate struct {
	predicate.Funcs
	requestorID string

	log logr.Logger
}

type RequestorOptions struct {
	// UseMaintenanceOperator enables requestor upgrade mode
	UseMaintenanceOperator bool
	// MaintenanceOPRequestorID is the requestor ID for maintenance operator
	MaintenanceOPRequestorID string
	// MaintenanceOPRequestorNS is a user defined namespace which nodeMaintennace
	// objects will be created
	MaintenanceOPRequestorNS string
	// MaintenanceOPPodEvictionFilter is a filter to be used for pods eviction
	// by maintenance operator
	MaintenanceOPPodEvictionFilter []maintenancev1alpha1.PodEvictionFiterEntry
}

// RequestorNodeStateManagerImpl contains concrete implementations for distinct requestor
// (e.g. maintenance OP) upgrade mode
type RequestorNodeStateManagerImpl struct {
	*CommonUpgradeManagerImpl
	opts RequestorOptions
}

// NewConditionChangedPredicate creates a new ConditionChangedPredicate
func NewConditionChangedPredicate(log logr.Logger, requestorID string) ConditionChangedPredicate {
	return ConditionChangedPredicate{
		Funcs:       predicate.Funcs{},
		log:         log,
		requestorID: requestorID,
	}
}

// Update implements Predicate.
func (p ConditionChangedPredicate) Update(e event.TypedUpdateEvent[client.Object]) bool {
	p.log.V(consts.LogLevelDebug).Info("ConditionChangedPredicate Update event")

	if e.ObjectOld == nil {
		p.log.Error(nil, "old object is nil in update event, ignoring event.")
		return false
	}
	if e.ObjectNew == nil {
		p.log.Error(nil, "new object is nil in update event, ignoring event.")
		return false
	}

	oldO, ok := e.ObjectOld.(*maintenancev1alpha1.NodeMaintenance)
	if !ok {
		p.log.Error(nil, "failed to cast old object to NodeMaintenance in update event, ignoring event.")
		return false
	}

	newO, ok := e.ObjectNew.(*maintenancev1alpha1.NodeMaintenance)
	if !ok {
		p.log.Error(nil, "failed to cast new object to NodeMaintenance in update event, ignoring event.")
		return false
	}

	// check for matching requestor ID
	if newO.Spec.RequestorID != p.requestorID {
		return false
	}

	cmpByType := func(a, b metav1.Condition) int {
		return cmp.Compare(a.Type, b.Type)
	}

	// sort old and new obj.Status.Conditions so they can be compared using DeepEqual
	slices.SortFunc(oldO.Status.Conditions, cmpByType)
	slices.SortFunc(newO.Status.Conditions, cmpByType)

	condChanged := !reflect.DeepEqual(oldO.Status.Conditions, newO.Status.Conditions)
	// Check if the object is marked for deletion
	deleting := len(newO.Finalizers) == 0 && len(oldO.Finalizers) > 0
	deleting = deleting && !newO.DeletionTimestamp.IsZero()
	enqueue := condChanged || deleting

	p.log.V(consts.LogLevelDebug).Info("update event for NodeMaintenance",
		"name", newO.Name, "namespace", newO.Namespace,
		"condition-changed", condChanged,
		"deleting", deleting, "enqueue-request", enqueue)

	return enqueue
}

func SetDefaultNodeMaintenance(opts RequestorOptions,
	upgradePolicy *v1alpha1.DriverUpgradePolicySpec) {
	drainSpec, podCompletion := convertV1Alpha1ToMaintenance(upgradePolicy, opts)
	defaultNodeMaintenance = &maintenancev1alpha1.NodeMaintenance{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: opts.MaintenanceOPRequestorNS,
		},
		Spec: maintenancev1alpha1.NodeMaintenanceSpec{
			RequestorID:          opts.MaintenanceOPRequestorID,
			WaitForPodCompletion: podCompletion,
			DrainSpec:            drainSpec,
		},
	}
}

func (m *RequestorNodeStateManagerImpl) NewNodeMaintenance(nodeName string) *maintenancev1alpha1.NodeMaintenance {
	nm := defaultNodeMaintenance.DeepCopy()
	nm.Name = nodeName
	nm.Spec.NodeName = nodeName

	return nm
}

// CreateNodeMaintenance creates nodeMaintenance obj for designated node upgrade-required state
func (m *RequestorNodeStateManagerImpl) CreateNodeMaintenance(ctx context.Context,
	nodeState *NodeUpgradeState) error {
	nm := m.NewNodeMaintenance(nodeState.Node.Name)
	nodeState.NodeMaintenance = nm
	m.Log.V(consts.LogLevelInfo).Info("creating node maintenance", nodeState.Node.Name, nm.Name)
	err := m.K8sClient.Create(ctx, nm, &client.CreateOptions{})
	if err != nil {
		if k8serrors.IsAlreadyExists(err) {
			m.Log.V(consts.LogLevelWarning).Info("nodeMaintenance", nm.Name, "already exists")
			return nil
		}
		return fmt.Errorf("failed to create node maintenance '%+v'. %v", nm, err)
	}

	return nil
}

// GetNodeMaintenanceObj creates nodeMaintenance obj for designated node upgrade-required state
func (m *RequestorNodeStateManagerImpl) GetNodeMaintenanceObj(ctx context.Context,
	nodeName string) (client.Object, error) {
	nm := &maintenancev1alpha1.NodeMaintenance{}
	err := m.K8sClient.Get(ctx, types.NamespacedName{
		Name: nodeName, Namespace: m.opts.MaintenanceOPRequestorNS},
		nm, &client.GetOptions{})
	if err != nil {
		if !k8serrors.IsNotFound(err) {
			return nil, err
		}
		// explicitly return nil so returned interface is truly nil
		//nolint:nilnil // this is intentional: returning nil obj and nil error to indicate "not found"
		return nil, nil
	}
	return nm, nil
}

// DeleteNodeMaintenance requests to delete nodeMaintenance obj
func (m *RequestorNodeStateManagerImpl) DeleteNodeMaintenance(ctx context.Context,
	nodeState *NodeUpgradeState) error {
	_, err := validateNodeMaintenance(nodeState)
	if err != nil {
		return err
	}
	nm := &maintenancev1alpha1.NodeMaintenance{}
	err = m.K8sClient.Get(ctx, types.NamespacedName{Name: nodeState.Node.Name,
		Namespace: m.opts.MaintenanceOPRequestorNS},
		nm, &client.GetOptions{})
	if err != nil {
		return err
	}
	// send deletion request assuming maintenance OP will handle actual obj deletion
	err = m.K8sClient.Delete(ctx, nm)
	if err != nil {
		return err
	}
	return nil
}

func validateNodeMaintenance(nodeState *NodeUpgradeState) (*maintenancev1alpha1.NodeMaintenance, error) {
	if nodeState.NodeMaintenance == nil {
		return nil, fmt.Errorf("missing nodeMaintenance for specified nodeUpgradeState. %v", nodeState)
	}
	nm, ok := nodeState.NodeMaintenance.(*maintenancev1alpha1.NodeMaintenance)
	if !ok {
		return nil, fmt.Errorf("failed to cast object to NodeMaintenance. %v", nodeState.NodeMaintenance)
	}
	return nm, nil
}

// NewRequestorNodeStateManagerImpl creates a new instance of (requestor) RequestorNodeStateManagerImpl
func NewRequestorNodeStateManagerImpl(
	common *CommonUpgradeManagerImpl,
	opts RequestorOptions) (ProcessNodeStateManager, error) {
	if !opts.UseMaintenanceOperator {
		common.Log.V(consts.LogLevelInfo).Info("node maintenance upgrade mode is disabled")
		return nil, ErrNodeMaintenanceUpgradeDisabled
	}
	manager := &RequestorNodeStateManagerImpl{
		opts:                     opts,
		CommonUpgradeManagerImpl: common,
	}

	return manager, nil
}

// ProcessUpgradeRequiredNodes processes UpgradeStateUpgradeRequired nodes and moves them to UpgradeStateCordonRequired
// until the limit on max parallel upgrades is reached.
func (m *RequestorNodeStateManagerImpl) ProcessUpgradeRequiredNodes(
	ctx context.Context, currentClusterState *ClusterUpgradeState,
	upgradePolicy *v1alpha1.DriverUpgradePolicySpec) error {
	m.Log.V(consts.LogLevelInfo).Info("ProcessUpgradeRequiredNodes")

	SetDefaultNodeMaintenance(m.opts, upgradePolicy)
	for _, nodeState := range currentClusterState.NodeStates[UpgradeStateUpgradeRequired] {
		if m.IsUpgradeRequested(nodeState.Node) {
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

		err := m.CreateNodeMaintenance(ctx, nodeState)
		if err != nil {
			m.Log.V(consts.LogLevelError).Error(err, "failed to create nodeMaintenance")
			return err
		}

		annotationKey := GetUpgradeRequestorModeAnnotationKey()
		err = m.NodeUpgradeStateProvider.ChangeNodeUpgradeAnnotation(ctx, nodeState.Node, annotationKey, "true")
		if err != nil {
			return fmt.Errorf("failed annotate node for 'upgrade-requestor-mode'. %v", err)
		}
		// update node state to 'node-maintenance-required'
		err = m.NodeUpgradeStateProvider.ChangeNodeUpgradeState(ctx, nodeState.Node,
			UpgradeStateNodeMaintenanceRequired)
		if err != nil {
			return fmt.Errorf("failed to update node state. %v", err)
		}
	}

	return nil
}

// ProcessNodeMaintenanceRequiredNodes processes UpgradeStatePostMaintenanceRequired
// by adding UpgradeStatePodRestartRequired under existing UpgradeStatePodRestartRequired nodes list.
// the motivation is later to replace ProcessPodRestartNodes to a generic post node operation
// while using maintenance operator (e.g. post-maintenance-required)
func (m *RequestorNodeStateManagerImpl) ProcessNodeMaintenanceRequiredNodes(ctx context.Context,
	currentClusterState *ClusterUpgradeState) error {
	m.Log.V(consts.LogLevelInfo).Info("ProcessNodeMaintenanceRequiredNodes")
	for _, nodeState := range currentClusterState.NodeStates[UpgradeStateNodeMaintenanceRequired] {
		if nodeState.NodeMaintenance == nil {
			if _, ok := nodeState.Node.Annotations[GetUpgradeRequestorModeAnnotationKey()]; !ok {
				m.Log.V(consts.LogLevelWarning).Info("missing node annotation", "node", nodeState.Node.Name,
					"annotations", nodeState.Node.Annotations)
			}
			// update node state back to 'upgrade-required' in case of missing nodeMaintenance obj
			err := m.NodeUpgradeStateProvider.ChangeNodeUpgradeState(ctx, nodeState.Node,
				UpgradeStateUpgradeRequired)
			if err != nil {
				return fmt.Errorf("failed to update node state. %v", err)
			}
			continue
		}
		nm, ok := nodeState.NodeMaintenance.(*maintenancev1alpha1.NodeMaintenance)
		if !ok {
			return fmt.Errorf("failed to cast object to NodeMaintenance. %v", nodeState.NodeMaintenance)
		}
		cond := meta.FindStatusCondition(nm.Status.Conditions, maintenancev1alpha1.ConditionReasonReady)
		if cond != nil {
			if cond.Reason == maintenancev1alpha1.ConditionReasonReady {
				m.Log.V(consts.LogLevelDebug).Info("node maintenance operation completed", nm.Spec.NodeName, cond.Reason)
				// update node state to 'pod-restart-required'
				err := m.NodeUpgradeStateProvider.ChangeNodeUpgradeState(ctx, nodeState.Node,
					UpgradeStatePodRestartRequired)
				if err != nil {
					return fmt.Errorf("failed to update node state. %v", err)
				}
			}
		}
	}

	return nil
}

func (m *RequestorNodeStateManagerImpl) ProcessUncordonRequiredNodes(
	ctx context.Context, currentClusterState *ClusterUpgradeState) error {
	m.Log.V(consts.LogLevelInfo).Info("ProcessUncordonRequiredNodes")

	for _, nodeState := range currentClusterState.NodeStates[UpgradeStateUncordonRequired] {
		m.Log.V(consts.LogLevelDebug).Info("deleting node maintenance",
			nodeState.NodeMaintenance.GetName(), nodeState.NodeMaintenance.GetNamespace())
		// skip in case node undergoes uncordon by inplace flow
		if nodeState.NodeMaintenance == nil {
			return nil
		}
		err := m.DeleteNodeMaintenance(ctx, nodeState)
		if err != nil {
			if !k8serrors.IsNotFound(err) {
				m.Log.V(consts.LogLevelWarning).Error(
					err, "Node uncordon failed", "node", nodeState.Node)
				return err
			}
		}
		// this means that node maintenance obj has been deleted
		err = m.NodeUpgradeStateProvider.ChangeNodeUpgradeState(ctx, nodeState.Node,
			UpgradeStateDone)
		if err != nil {
			return fmt.Errorf("failed to update node state. %v", err)
		}
		// remove requestor upgrade annotation
		err = m.NodeUpgradeStateProvider.ChangeNodeUpgradeAnnotation(ctx,
			nodeState.Node, GetUpgradeRequestorModeAnnotationKey(), "null")
		if err != nil {
			return fmt.Errorf("failed to remove '%s' annotation . %v", GetUpgradeRequestorModeAnnotationKey(), err)
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

// convertV1Alpha1ToMaintenance explicitly converts v1alpha1.DriverUpgradePolicySpec
// to maintenancev1alpha1.DrainSpec and maintenancev1alpha1.WaitForPodCompletionSpec and
func convertV1Alpha1ToMaintenance(upgradePolicy *v1alpha1.DriverUpgradePolicySpec,
	opts RequestorOptions) (*maintenancev1alpha1.DrainSpec,
	*maintenancev1alpha1.WaitForPodCompletionSpec) {
	var podComplition *maintenancev1alpha1.WaitForPodCompletionSpec
	if upgradePolicy == nil {
		return nil, nil
	}
	drainSpec := &maintenancev1alpha1.DrainSpec{}
	if upgradePolicy.DrainSpec != nil {
		drainSpec.Force = upgradePolicy.DrainSpec.Force
		drainSpec.PodSelector = upgradePolicy.DrainSpec.PodSelector
		//nolint:gosec // G115: suppress potential integer overflow conversion warning
		drainSpec.TimeoutSecond = int32(upgradePolicy.DrainSpec.TimeoutSecond)
		drainSpec.DeleteEmptyDir = upgradePolicy.DrainSpec.DeleteEmptyDir
	}
	if upgradePolicy.PodDeletion != nil {
		drainSpec.PodEvictionFilters = opts.MaintenanceOPPodEvictionFilter
	}
	if upgradePolicy.WaitForCompletion != nil {
		podComplition = &maintenancev1alpha1.WaitForPodCompletionSpec{
			PodSelector: upgradePolicy.WaitForCompletion.PodSelector,
			//nolint:gosec // G115: suppress potential integer overflow conversion warning
			TimeoutSecond: int32(upgradePolicy.WaitForCompletion.TimeoutSecond),
		}
	}

	return drainSpec, podComplition
}

// GetRequestorEnvs returns requstor upgrade related options according to provided environment variables
func GetRequestorOptsFromEnvs() RequestorOptions {
	opts := RequestorOptions{}
	if os.Getenv("MAINTENANCE_OPERATOR_ENABLED") == "true" {
		opts.UseMaintenanceOperator = true
	}
	if os.Getenv("MAINTENANCE_OPERATOR_REQUESTOR_NAMESPACE") != "" {
		opts.MaintenanceOPRequestorNS = os.Getenv("MAINTENANCE_OPERATOR_REQUESTOR_NAMESPACE")
	} else {
		opts.MaintenanceOPRequestorNS = "default"
	}
	if os.Getenv("MAINTENANCE_OPERATOR_REQUESTOR_ID") != "" {
		opts.MaintenanceOPRequestorID = os.Getenv("MAINTENANCE_OPERATOR_REQUESTOR_ID")
	} else {
		opts.MaintenanceOPRequestorID = "nvidia.operator.com"
	}
	return opts
}

func init() {
	utilruntime.Must(clientgoscheme.AddToScheme(Scheme))
	utilruntime.Must(maintenancev1alpha1.AddToScheme(Scheme))
}
