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

package conditions

const (
	// Reconciled is the generic reason for successful reconciliation of all states
	Reconciled = "Reconciled"
	// ReconcileFailed is the generic reason for reconciliation failures
	ReconcileFailed = "ReconcileFailed"
	// NFDLabelsMissing indicates that NFD labels for GPU nodes are missing
	NFDLabelsMissing = "NFDLabelsMissing"
	// NoGPUNodes indicates that there are no GPU nodes in the cluster
	NoGPUNodes = "NoGPUNodes"
	// NodeStatusExporterNotReady indicates that the node-status-exporter daemonset pods are not ready
	NodeStatusExporterNotReady = "NodeStatusExporterNotReady"

	// OperandNotReady is the generic reason for any operand pod failures
	OperandNotReady = "OperandNotReady"
	// DriverNotReady indicates that the driver daemonset pods are not ready
	DriverNotReady = "DriverNotReady"
)
