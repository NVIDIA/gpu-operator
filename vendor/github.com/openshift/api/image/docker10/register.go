package docker10

import (
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
)

const (
	GroupName       = "image.openshift.io"
	LegacyGroupName = ""
)

// SchemeGroupVersion is group version used to register these objects
var (
	GroupVersion             = schema.GroupVersion{Group: GroupName, Version: "1.0"}
	LegacySchemeGroupVersion = schema.GroupVersion{Group: LegacyGroupName, Version: "1.0"}

	SchemeBuilder       = runtime.NewSchemeBuilder(addKnownTypes)
	LegacySchemeBuilder = runtime.NewSchemeBuilder(addLegacyKnownTypes)

	AddToSchemeInCoreGroup = LegacySchemeBuilder.AddToScheme

	// Install is a function which adds this version to a scheme
	Install = SchemeBuilder.AddToScheme

	// SchemeGroupVersion generated code relies on this name
	// Deprecated
	SchemeGroupVersion = GroupVersion
	// AddToScheme exists solely to keep the old generators creating valid code
	// DEPRECATED
	AddToScheme = SchemeBuilder.AddToScheme
)

// Adds the list of known types to api.Scheme.
func addKnownTypes(scheme *runtime.Scheme) error {
	scheme.AddKnownTypes(SchemeGroupVersion,
		&DockerImage{},
	)
	return nil
}

func addLegacyKnownTypes(scheme *runtime.Scheme) error {
	scheme.AddKnownTypes(LegacySchemeGroupVersion,
		&DockerImage{},
	)
	return nil
}
