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

const (
	// UpgradeStateLabelKeyFmt is the format of the node label key indicating driver upgrade states
	UpgradeStateLabelKeyFmt = "nvidia.com/%s-driver-upgrade-state"
	// UpgradeSkipNodeLabelKeyFmt is the format of the node label boolean key indicating to skip driver upgrade
	UpgradeSkipNodeLabelKeyFmt = "nvidia.com/%s-driver-upgrade.skip"
	// UpgradeSkipDrainDriverSelectorFmt is the format of the pod selector key indicating to skip driver
	// in upgrade drain spec
	UpgradeSkipDrainDriverSelectorFmt = "nvidia.com/%s-driver-upgrade-drain.skip"
	// UpgradeWaitForSafeDriverLoadAnnotationKeyFmt is the format of the node annotation key indicating that
	// the driver is waiting for safe load. Meaning node should be cordoned and workloads should be removed from the
	// node before the driver can continue to load.
	UpgradeWaitForSafeDriverLoadAnnotationKeyFmt = "nvidia.com/%s-driver-upgrade.driver-wait-for-safe-load"
	// UpgradeInitialStateAnnotationKeyFmt is the format of the node annotation indicating node was unschedulable at
	// beginning of upgrade process
	UpgradeInitialStateAnnotationKeyFmt = "nvidia.com/%s-driver-upgrade.node-initial-state.unschedulable"
	// UpgradeWaitForPodCompletionStartTimeAnnotationKeyFmt is the format of the node annotation indicating start time
	// for waiting on pod completions
	//nolint: lll
	UpgradeWaitForPodCompletionStartTimeAnnotationKeyFmt = "nvidia.com/%s-driver-upgrade-wait-for-pod-completion-start-time"
	// UpgradeValidationStartTimeAnnotationKeyFmt is the format of the node annotation indicating start time for
	// validation-required state
	UpgradeValidationStartTimeAnnotationKeyFmt = "nvidia.com/%s-driver-upgrade-validation-start-time"
	// UpgradeRequestedAnnotationKeyFmt is the format of the node label key indicating driver upgrade was requested
	// (used for orphaned pods)
	// Setting this label will trigger setting upgrade state to upgrade-required
	UpgradeRequestedAnnotationKeyFmt = "nvidia.com/%s-driver-upgrade-requested"
	// UpgradeRequestorModeAnnotationKeyFmt is the format of the node annotation indicating requestor driver upgrade
	// mode is used for underlying node
	UpgradeRequestorModeAnnotationKeyFmt = "nvidia.com/%s-driver-upgrade-requestor-mode"
	// UpgradeStateUnknown Node has this state when the upgrade flow is disabled or the node hasn't been processed yet
	UpgradeStateUnknown = ""
	// UpgradeStateUpgradeRequired is set when the driver pod on the node is not up-to-date and required upgrade
	// No actions are performed at this stage
	UpgradeStateUpgradeRequired = "upgrade-required"
	// UpgradeStateCordonRequired is set when the node needs to be made unschedulable in preparation for driver upgrade
	UpgradeStateCordonRequired = "cordon-required"
	// UpgradeStateWaitForJobsRequired is set on the node when we need to wait on jobs to complete until given timeout.
	UpgradeStateWaitForJobsRequired = "wait-for-jobs-required"
	// UpgradeStatePodDeletionRequired is set when deletion of pods is required for the driver upgrade to proceed.
	UpgradeStatePodDeletionRequired = "pod-deletion-required"
	// UpgradeStateDrainRequired is set when the node is required to be scheduled for drain. After the drain the state
	// is changed either to UpgradeStatePodRestartRequired or UpgradeStateFailed
	UpgradeStateDrainRequired = "drain-required"
	// UpgradeStateNodeMaintenanceRequired is set when the node is scheduled for node maintenance.
	// The node maintenance operations, like cordon, drain, etc., are carried out by an external maintenance
	// operator. This state is only ever used / valid when UseMaintenanceOperator is true and
	// an external maintenance operator exists.
	UpgradeStateNodeMaintenanceRequired = "node-maintenance-required"
	// UpgradeStatePostMaintenanceRequired is set after node maintenance is completed by an
	// external maintenance operator. This state indicates that the requestor is required to perform
	// post-maintenance operations (e.g. restart driver pods).
	UpgradeStatePostMaintenanceRequired = "post-maintenance-required"
	// UpgradeStatePodRestartRequired is set when the driver pod on the node is scheduled for restart
	// or when unblock of the driver loading is required (safe driver load)
	UpgradeStatePodRestartRequired = "pod-restart-required"
	// UpgradeStateValidationRequired is set when validation of the new driver deployed on the node is
	// required before moving to UpgradeStateUncordonRequired.
	UpgradeStateValidationRequired = "validation-required"
	// UpgradeStateUncordonRequired is set when driver pod on the node is up-to-date and has "Ready" status
	UpgradeStateUncordonRequired = "uncordon-required"
	// UpgradeStateDone is set when driver pod is up to date and running on the node, the node is schedulable
	UpgradeStateDone = "upgrade-done"
	// UpgradeStateFailed is set when there are any failures during the driver upgrade
	UpgradeStateFailed = "upgrade-failed"
)

const (
	// nodeNameFieldSelectorFmt is the format of a field selector that can be used in metav1.ListOptions to filter by
	// node
	nodeNameFieldSelectorFmt = "spec.nodeName=%s"
	// nullString is the word null as string to avoid duplication and linting errors
	nullString = "null"
	// trueString is the word true as string to avoid duplication and linting errors
	trueString = "true"
)
