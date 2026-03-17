// Package v1alpha1 contains API Schema definitions for the kubewa v1alpha1 API group.
// +kubebuilder:object:generate=true
// +groupName=kubewa.dev
package v1alpha1

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"sigs.k8s.io/controller-runtime/pkg/scheme"
)

var (
	// GroupVersion is group version used to register these objects.
	GroupVersion = schema.GroupVersion{Group: "kubewa.dev", Version: "v1alpha1"}

	// SchemeBuilder is used to add functions to the scheme.
	SchemeBuilder = &scheme.Builder{GroupVersion: GroupVersion}

	// AddToScheme adds the types in this group-version to the given scheme.
	AddToScheme = SchemeBuilder.AddToScheme
)

func init() {
	SchemeBuilder.Register(&OnCallSchedule{}, &OnCallScheduleList{})
	SchemeBuilder.Register(&Incident{}, &IncidentList{})
	metav1.AddToGroupVersion(runtime.NewScheme(), GroupVersion)
	_ = runtime.NewScheme()
}
