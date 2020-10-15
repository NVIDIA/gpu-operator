package v1

import (
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// EDIT THIS FILE!  THIS IS SCAFFOLDING FOR YOU TO OWN!
// NOTE: json tags are required.  Any new fields you add must have json tags for the fields to be serialized.
// INSERT ADDITIONAL SPEC/Status FIELDS
// Important: Run "operator-sdk generate k8s" to regenerate code after modifying this file
// Add custom validation using kubebuilder tags: https://book-v1.book.kubebuilder.io/beyond_basics/generating_crd.html

// ClusterPolicySpec defines the desired state of ClusterPolicy
type ClusterPolicySpec struct {
	Operator              OperatorSpec            `json:"operator"`
	Driver                ComponentSpec           `json:"driver"`
	Toolkit               ComponentSpec           `json:"toolkit"`
	DevicePlugin          ComponentSpec           `json:"devicePlugin"`
	DCGMExporter          ComponentSpec           `json:"dcgmExporter"`
	GroupFeatureDiscovery GPUFeatureDiscoverySpec `json:"gfd"`
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
	DeployGFD      bool    `json:"deployGFD"`
}

// Note these regex are obviously not handling well edge cases.
// Though we probably don't need to.

// ComponentSpec defines the properties for individual GPU operator components
type ComponentSpec struct {
	// +kubebuilder:validation:Pattern=[a-zA-Z0-9\.\-\/]+
	Repository string `json:"repository"`

	// +kubebuilder:validation:Pattern=[a-zA-Z0-9\-]+
	Image string `json:"image"`

	// +kubebuilder:validation:Pattern=[a-zA-Z0-9\.-]+
	Version string `json:"version"`

	// Image pull policy
	// +kubebuilder:validation:Optional
	ImagePullPolicy string `json:"imagePullPolicy,omitempty"`

	// Image pull secrets
	// +kubebuilder:validation:Optional
	// +operator-sdk:gen-csv:customresourcedefinitions.specDescriptors=true
	// +operator-sdk:gen-csv:customresourcedefinitions.specDescriptors.displayName="Image pull secrets"
	// +operator-sdk:gen-csv:customresourcedefinitions.specDescriptors.x-descriptors="urn:alm:descriptor:io.kubernetes:Secret"
	ImagePullSecrets []string `json:"imagePullSecrets"`

	// Node selector to control the selection of nodes (optional)
	// +operator-sdk:gen-csv:customresourcedefinitions.specDescriptors=true
	// +operator-sdk:gen-csv:customresourcedefinitions.specDescriptors.displayName="Node Selector"
	// +operator-sdk:gen-csv:customresourcedefinitions.specDescriptors.x-descriptors="urn:alm:descriptor:com.tectonic.ui:selector:Node"
	NodeSelector map[string]string `json:"nodeSelector,omitempty"`

	// Optional: Set tolerations
	// +operator-sdk:gen-csv:customresourcedefinitions.specDescriptors=true
	// +operator-sdk:gen-csv:customresourcedefinitions.specDescriptors.displayName="Tolerations"
	// +operator-sdk:gen-csv:customresourcedefinitions.specDescriptors.x-descriptors="urn:alm:descriptor:com.tectonic.ui:advanced,urn:alm:descriptor:io.kubernetes:Tolerations"
	Tolerations []corev1.Toleration `json:"tolerations,omitempty"`

	// Optional: Set Node affinity
	// +operator-sdk:gen-csv:customresourcedefinitions.specDescriptors=true
	// +operator-sdk:gen-csv:customresourcedefinitions.specDescriptors.displayName="Node affinity"
	Affinity *corev1.Affinity `json:"affinity,omitempty"`

	// Optional: Pod Security Context
	PodSecurityContext *corev1.PodSecurityContext `json:"podSecurityContext,omitempty"`

	// Optional: Security Context
	SecurityContext *corev1.SecurityContext `json:"securityContext,omitempty"`

	// Optional: Resources
	Resources *corev1.ResourceRequirements `json:"resources,omitempty"`
}

// GPUFeatureDiscoverySpec defines the properties for GPU Feature Discovery Plugin
type GPUFeatureDiscoverySpec struct {
	// +kubebuilder:validation:Pattern=[a-zA-Z0-9\.\-\/]+
	Repository string `json:"repository"`

	// +kubebuilder:validation:Pattern=[a-zA-Z0-9\-]+
	Image string `json:"image"`

	// +kubebuilder:validation:Pattern=[a-zA-Z0-9\.-]+
	Version string `json:"version"`

	// Image pull policy
	// +kubebuilder:validation:Optional
	ImagePullPolicy string `json:"imagePullPolicy,omitempty"`

	// Image pull secrets
	// +kubebuilder:validation:Optional
	// +operator-sdk:gen-csv:customresourcedefinitions.specDescriptors=true
	// +operator-sdk:gen-csv:customresourcedefinitions.specDescriptors.displayName="Image pull secrets"
	// +operator-sdk:gen-csv:customresourcedefinitions.specDescriptors.x-descriptors="urn:alm:descriptor:io.kubernetes:Secret"
	ImagePullSecrets []string `json:"imagePullSecrets"`

	// Node selector to control the selection of nodes (optional)
	// +operator-sdk:gen-csv:customresourcedefinitions.specDescriptors=true
	// +operator-sdk:gen-csv:customresourcedefinitions.specDescriptors.displayName="Node Selector"
	// +operator-sdk:gen-csv:customresourcedefinitions.specDescriptors.x-descriptors="urn:alm:descriptor:com.tectonic.ui:selector:Node"
	NodeSelector map[string]string `json:"nodeSelector,omitempty"`

	// Optional: Set tolerations
	// +operator-sdk:gen-csv:customresourcedefinitions.specDescriptors=true
	// +operator-sdk:gen-csv:customresourcedefinitions.specDescriptors.displayName="Tolerations"
	// +operator-sdk:gen-csv:customresourcedefinitions.specDescriptors.x-descriptors="urn:alm:descriptor:com.tectonic.ui:advanced,urn:alm:descriptor:io.kubernetes:Tolerations"
	Tolerations []corev1.Toleration `json:"tolerations,omitempty"`

	// Optional: Set Node affinity
	// +operator-sdk:gen-csv:customresourcedefinitions.specDescriptors=true
	// +operator-sdk:gen-csv:customresourcedefinitions.specDescriptors.displayName="Node affinity"
	Affinity *corev1.Affinity `json:"affinity,omitempty"`

	// Optional: Pod Security Context
	PodSecurityContext *corev1.PodSecurityContext `json:"podSecurityContext,omitempty"`

	// Optional: Security Context
	SecurityContext *corev1.SecurityContext `json:"securityContext,omitempty"`

	// Optional: Resources
	Resources *corev1.ResourceRequirements `json:"resources,omitempty"`

	// Optional: MigStrategy for GPU feature discovery plugin
	// +kubebuilder:validation:Enum=none;single;mixed
	MigStrategy MigStrategy `json:"migStrategy,omitempty"`

	// Optional: Discovery Interval for GPU feature discovery plugin
	DiscoveryIntervalSeconds int `json:"discoveryIntervalSeconds,omitempty"`
}

type MigStrategy string

// Constants representing different MIG strategies.
const (
	MigStrategyNone   MigStrategy = "none"
	MigStrategySingle MigStrategy = "single"
	MigStrategyMixed  MigStrategy = "mixed"
)

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

func (c *ComponentSpec) ImagePolicy(pullPolicy string) corev1.PullPolicy {
	var imagePullPolicy corev1.PullPolicy
	switch pullPolicy {
	case "Always":
		imagePullPolicy = corev1.PullAlways
	case "Never":
		imagePullPolicy = corev1.PullNever
	case "IfNotPresent":
		imagePullPolicy = corev1.PullIfNotPresent
	default:
		imagePullPolicy = corev1.PullIfNotPresent
	}
	return imagePullPolicy
}

func (g *GPUFeatureDiscoverySpec) ImagePath() string {
	return g.Repository + "/" + g.Image + ":" + g.Version
}

func (g *GPUFeatureDiscoverySpec) ImagePolicy(pullPolicy string) corev1.PullPolicy {
	var imagePullPolicy corev1.PullPolicy
	switch pullPolicy {
	case "Always":
		imagePullPolicy = corev1.PullAlways
	case "Never":
		imagePullPolicy = corev1.PullNever
	case "IfNotPresent":
		imagePullPolicy = corev1.PullIfNotPresent
	default:
		imagePullPolicy = corev1.PullIfNotPresent
	}
	return imagePullPolicy
}
