/*
 Copyright 2024, NVIDIA CORPORATION & AFFILIATES

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

package v1alpha1

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/intstr"
)

// OperatorLogLevel is the operator log level. one of: ["debug", "info", "error"]
// +kubebuilder:validation:Enum=debug;info;error
type OperatorLogLevel string

// MaintenanceOperatorConfigSpec defines the desired state of MaintenanceOperatorConfig
type MaintenanceOperatorConfigSpec struct {
	// MaxParallelOperations indicates the maximal number nodes that can undergo maintenance
	// at a given time. 0 means no limit
	// value can be an absolute number (ex: 5) or a percentage of total nodes in the cluster (ex: 10%).
	// absolute number is calculated from percentage by rounding up.
	// defaults to 1. The actual number of nodes that can undergo maintenance may be lower depending
	// on the value of MaintenanceOperatorConfigSpec.MaxUnavailable.
	// +kubebuilder:default=1
	// +kubebuilder:validation:XIntOrString
	MaxParallelOperations *intstr.IntOrString `json:"maxParallelOperations,omitempty"`

	// MaxUnavailable is the maximum number of nodes that can become unavailable in the cluster.
	// value can be an absolute number (ex: 5) or a percentage of total nodes in the cluster (ex: 10%).
	// absolute number is calculated from percentage by rounding up.
	// by default, unset.
	// new nodes will not be processed if the number of unavailable node will exceed this value
	// +kubebuilder:validation:XIntOrString
	MaxUnavailable *intstr.IntOrString `json:"maxUnavailable,omitempty"`

	// LogLevel is the operator logging level
	// +kubebuilder:default="info"
	LogLevel OperatorLogLevel `json:"logLevel,omitempty"`

	// MaxNodeMaintenanceTimeSeconds is the time from when a NodeMaintenance is marked as ready (phase: Ready)
	// until the NodeMaintenance is considered stale and removed by the operator.
	// should be less than idle time for any autoscaler that is running.
	// default to 30m (1600 seconds)
	// +kubebuilder:default=1600
	// +kubebuilder:validation:Minimum:=0
	MaxNodeMaintenanceTimeSeconds int32 `json:"maxNodeMaintenanceTimeSeconds,omitempty"`
}

type MaintenanceOperatorConfigStatus struct {
}

//+kubebuilder:object:root=true
//+kubebuilder:subresource:status

// MaintenanceOperatorConfig is the Schema for the maintenanceoperatorconfigs API
type MaintenanceOperatorConfig struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   MaintenanceOperatorConfigSpec   `json:"spec,omitempty"`
	Status MaintenanceOperatorConfigStatus `json:"status,omitempty"`
}

//+kubebuilder:object:root=true

// MaintenanceOperatorConfigList contains a list of MaintenanceOperatorConfig
type MaintenanceOperatorConfigList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []MaintenanceOperatorConfig `json:"items"`
}

func init() {
	SchemeBuilder.Register(&MaintenanceOperatorConfig{}, &MaintenanceOperatorConfigList{})
}
