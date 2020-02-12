package v1alpha1

import (
	corev1 "k8s.io/api/core/v1"
	schedv1 "k8s.io/api/scheduling/v1beta1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

type SchedulingType string

const (
	PriorityPreemption SchedulingType = "PrioriyPreemption"
	TaintsTolerations  SchedulingType = "TaintsToleration"
	None               SchedulingType = "None"
)

// SpecialResourceSpec defines the desired state of SpecialResource
type SpecialResourceSpec struct {
	// Important: Run "operator-sdk generate k8s" to regenerate code after modifying this file
	Scheduling         SchedulingType          `json:"schedulingType"`
	PriorityClassItems []schedv1.PriorityClass `json:"priorityClasses" protobuf:"bytes,2,rep,name=priorityClasses"`
	Taints             []corev1.Taint          `json:"taints,omitempty" protobuf:"bytes,5,opt,name=taints"`
}

// SpecialResourceStatus defines the observed state of SpecialResource
type SpecialResourceStatus struct {
	// INSERT ADDITIONAL STATUS FIELD - define observed state of cluster
	// Important: Run "operator-sdk generate k8s" to regenerate code after modifying this file

}

// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object

// SpecialResource is the Schema for the specialresources API
// +k8s:openapi-gen=true
type SpecialResource struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   SpecialResourceSpec   `json:"spec,omitempty"`
	Status SpecialResourceStatus `json:"status,omitempty"`
}

// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object

// SpecialResourceList contains a list of SpecialResource
type SpecialResourceList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []SpecialResource `json:"items"`
}

func init() {
	SchemeBuilder.Register(&SpecialResource{}, &SpecialResourceList{})
}
