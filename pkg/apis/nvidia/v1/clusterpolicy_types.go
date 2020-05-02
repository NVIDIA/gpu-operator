package v1

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// EDIT THIS FILE!  THIS IS SCAFFOLDING FOR YOU TO OWN!
// NOTE: json tags are required.  Any new fields you add must have json tags for the fields to be serialized.
// INSERT ADDITIONAL SPEC/Status FIELDS
// Important: Run "operator-sdk generate k8s" to regenerate code after modifying this file
// Add custom validation using kubebuilder tags: https://book-v1.book.kubebuilder.io/beyond_basics/generating_crd.html

// ClusterPolicySpec defines the desired state of ClusterPolicy
type ClusterPolicySpec struct {
	Operator     OperatorSpec  `json:"operator"`
	Driver       ComponentSpec `json:"driver"`
	Toolkit      ComponentSpec `json:"toolkit"`
	DevicePlugin ComponentSpec `json:"devicePlugin"`
	DCGMExporter ComponentSpec `json:"dcgmExporter"`
}

type Runtime string

const (
	Docker Runtime = "docker"
	CRIO   Runtime = "crio"
)

// OperatorSpec describes configuration options for the operator
type OperatorSpec struct {
	// +kubebuilder:validation:Enum=docker;crio
	DefaultRuntime Runtime `json:"defaultRuntime"`
}

// Note these regex are obviously not handling well edge cases.
// Though we probably don't need to.

// ComponentSpec defines the path to the container image
type ComponentSpec struct {
	// +kubebuilder:validation:Pattern=[a-zA-Z0-9\.\-\/]+
	Repository string `json:"repository"`

	// +kubebuilder:validation:Pattern=[a-zA-Z0-9\-]+
	Image string `json:"image"`

	// +kubebuilder:validation:Pattern=[a-zA-Z0-9\.-]+
	Version string `json:"version"`
}

type State string

const (
	Ignored  State = "ignored"
	Ready    State = "ready"
	NotReady State = "notReady"
)

// ClusterPolicyStatus defines the observed state of ClusterPolicy
type ClusterPolicyStatus struct {
	// +kubebuilder:validation:Enum=ignored;ready;notReady
	State State `json:"state"`
}

// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object

// ClusterPolicy allows you to configure the GPU Operator
// +kubebuilder:subresource:status
// +kubebuilder:resource:path=clusterpolicies,scope=Cluster
type ClusterPolicy struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   ClusterPolicySpec   `json:"spec,omitempty"`
	Status ClusterPolicyStatus `json:"status,omitempty"`
}

// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object

// ClusterPolicyList contains a list of ClusterPolicy
type ClusterPolicyList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []ClusterPolicy `json:"items"`
}

func init() {
	SchemeBuilder.Register(&ClusterPolicy{}, &ClusterPolicyList{})
}

func (p *ClusterPolicy) SetState(s State) {
	p.Status.State = s
}

func (c *ComponentSpec) ImagePath() string {
	return c.Repository + "/" + c.Image + ":" + c.Version
}
