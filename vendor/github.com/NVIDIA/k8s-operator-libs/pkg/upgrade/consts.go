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
	// UpgradeInitialStateAnnotationKeyFmt is the format of the node annotation indicating node was unschedulable at beginning of upgrade process
	UpgradeInitialStateAnnotationKeyFmt = "nvidia.com/%s-driver-upgrade.node-initial-state.unschedulable"
	// UpgradeWaitForPodCompletionStartTimeAnnotationKeyFmt is the format of the node annotation indicating start time for waiting on pod completions
	UpgradeWaitForPodCompletionStartTimeAnnotationKeyFmt = "nvidia.com/%s-driver-upgrade-wait-for-pod-completion-start-time"
	// UpgradeValidationStartTimeAnnotationKeyFmt is the format of the node annotation indicating start time for validation-required state
	UpgradeValidationStartTimeAnnotationKeyFmt = "nvidia.com/%s-driver-upgrade-validation-start-time"
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
	// UpgradeStateDrainRequired is set when the node is required to be scheduled for drain. After the drain the state is changed
	// either to UpgradeStatePodRestartRequired or UpgradeStateFailed
	UpgradeStateDrainRequired = "drain-required"
	// UpgradeStatePodRestartRequired is set when the driver pod on the node is scheduled for restart.
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
