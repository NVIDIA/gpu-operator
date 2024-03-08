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

package v1alpha1

import (
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/util/intstr"
)

// DriverUpgradePolicySpec describes policy configuration for automatic upgrades
// +kubebuilder:object:root=true
// +kubebuilder:object:generate=true
type DriverUpgradePolicySpec struct {
	// AutoUpgrade is a global switch for automatic upgrade feature
	// if set to false all other options are ignored
	// +optional
	// +kubebuilder:default:=false
	AutoUpgrade bool `json:"autoUpgrade,omitempty"`
	// MaxParallelUpgrades indicates how many nodes can be upgraded in parallel
	// 0 means no limit, all nodes will be upgraded in parallel
	// +optional
	// +kubebuilder:default:=1
	// +kubebuilder:validation:Minimum:=0
	MaxParallelUpgrades int `json:"maxParallelUpgrades,omitempty"`
	// MaxUnavailable is the maximum number of nodes with the driver installed, that can be unavailable during the upgrade.
	// Value can be an absolute number (ex: 5) or a percentage of total nodes at the start of upgrade (ex: 10%).
	// Absolute number is calculated from percentage by rounding up.
	// By default, a fixed value of 25% is used.
	// +optional
	// +kubebuilder:default:="25%"
	MaxUnavailable    *intstr.IntOrString    `json:"maxUnavailable,omitempty"`
	PodDeletion       *PodDeletionSpec       `json:"podDeletion,omitempty"`
	WaitForCompletion *WaitForCompletionSpec `json:"waitForCompletion,omitempty"`
	DrainSpec         *DrainSpec             `json:"drain,omitempty"`
}

// WaitForCompletionSpec describes the configuration for waiting on job completions
type WaitForCompletionSpec struct {
	// PodSelector specifies a label selector for the pods to wait for completion
	// For more details on label selectors, see:
	// https://kubernetes.io/docs/concepts/overview/working-with-objects/labels/#label-selectors
	// +optional
	PodSelector string `json:"podSelector,omitempty"`
	// TimeoutSecond specifies the length of time in seconds to wait before giving up on pod termination, zero means
	// infinite
	// +optional
	// +kubebuilder:default:=0
	// +kubebuilder:validation:Minimum:=0
	TimeoutSecond int `json:"timeoutSeconds,omitempty"`
}

// PodDeletionSpec describes configuration for deletion of pods using special resources during automatic upgrade
type PodDeletionSpec struct {
	// Force indicates if force deletion is allowed
	// +optional
	// +kubebuilder:default:=false
	Force bool `json:"force,omitempty"`
	// TimeoutSecond specifies the length of time in seconds to wait before giving up on pod termination, zero means
	// infinite
	// +optional
	// +kubebuilder:default:=300
	// +kubebuilder:validation:Minimum:=0
	TimeoutSecond int `json:"timeoutSeconds,omitempty"`
	// DeleteEmptyDir indicates if should continue even if there are pods using emptyDir
	// (local data that will be deleted when the pod is deleted)
	// +optional
	// +kubebuilder:default:=false
	DeleteEmptyDir bool `json:"deleteEmptyDir,omitempty"`
}

// DrainSpec describes configuration for node drain during automatic upgrade
type DrainSpec struct {
	// Enable indicates if node draining is allowed during upgrade
	// +optional
	// +kubebuilder:default:=false
	Enable bool `json:"enable,omitempty"`
	// Force indicates if force draining is allowed
	// +optional
	// +kubebuilder:default:=false
	Force bool `json:"force,omitempty"`
	// PodSelector specifies a label selector to filter pods on the node that need to be drained
	// For more details on label selectors, see:
	// https://kubernetes.io/docs/concepts/overview/working-with-objects/labels/#label-selectors
	// +optional
	PodSelector string `json:"podSelector,omitempty"`
	// TimeoutSecond specifies the length of time in seconds to wait before giving up drain, zero means infinite
	// +optional
	// +kubebuilder:default:=300
	// +kubebuilder:validation:Minimum:=0
	TimeoutSecond int `json:"timeoutSeconds,omitempty"`
	// DeleteEmptyDir indicates if should continue even if there are pods using emptyDir
	// (local data that will be deleted when the node is drained)
	// +optional
	// +kubebuilder:default:=false
	DeleteEmptyDir bool `json:"deleteEmptyDir,omitempty"`
}

// GetObjectKind return ObjectKind
func (obj *DriverUpgradePolicySpec) GetObjectKind() schema.ObjectKind { return nil }
