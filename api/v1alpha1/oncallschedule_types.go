package v1alpha1

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// RotationType defines the rotation cadence for on-call shifts.
// +kubebuilder:validation:Enum=daily;weekly
type RotationType string

const (
	// RotationDaily rotates on-call personnel every day.
	RotationDaily RotationType = "daily"
	// RotationWeekly rotates on-call personnel every week.
	RotationWeekly RotationType = "weekly"
)

// OnCallEntry represents a single on-call participant.
type OnCallEntry struct {
	// Name is the display name of the on-call person.
	// +kubebuilder:validation:MinLength=1
	Name string `json:"name"`

	// Phone is the WhatsApp-enabled phone number (E.164 format, e.g. +1234567890).
	// +kubebuilder:validation:Pattern=`^\+[1-9]\d{1,14}$`
	Phone string `json:"phone"`

	// Email is the optional email address for additional notifications.
	// +optional
	Email string `json:"email,omitempty"`
}

// OnCallScheduleSpec defines the desired state of OnCallSchedule.
type OnCallScheduleSpec struct {
	// Rotation defines how often the on-call rotation shifts.
	// +kubebuilder:default=weekly
	Rotation RotationType `json:"rotation"`

	// Schedule is the ordered list of on-call participants.
	// +kubebuilder:validation:MinItems=1
	Schedule []OnCallEntry `json:"schedule"`

	// Backup is the fallback contact when the primary on-call person does not
	// acknowledge an incident within the AckTimeout.
	// +optional
	Backup *OnCallEntry `json:"backup,omitempty"`

	// AckTimeout is the duration (e.g. "15m") to wait for acknowledgement
	// before escalating to the backup contact.
	// +kubebuilder:default="15m"
	// +optional
	AckTimeout string `json:"ackTimeout,omitempty"`

	// Timezone is the IANA timezone used for rotation boundaries
	// (e.g. "America/Sao_Paulo").
	// +kubebuilder:default="UTC"
	// +optional
	Timezone string `json:"timezone,omitempty"`

	// PodSelector restricts which pods this schedule applies to via label selectors.
	// +optional
	PodSelector *metav1.LabelSelector `json:"podSelector,omitempty"`
}

// OnCallScheduleStatus defines the observed state of OnCallSchedule.
type OnCallScheduleStatus struct {
	// CurrentOnCallPhone is the phone number currently on call.
	// +optional
	CurrentOnCallPhone string `json:"currentOnCallPhone,omitempty"`

	// CurrentOnCallName is the name of the person currently on call.
	// +optional
	CurrentOnCallName string `json:"currentOnCallName,omitempty"`

	// NextShift is the time when the next rotation will occur.
	// +optional
	NextShift *metav1.Time `json:"nextShift,omitempty"`

	// CurrentIndex tracks the index in the Schedule slice.
	// +optional
	CurrentIndex int `json:"currentIndex,omitempty"`

	// Conditions represent the latest available observations of the schedule's state.
	// +optional
	// +listType=map
	// +listMapKey=type
	Conditions []metav1.Condition `json:"conditions,omitempty"`
}

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:resource:scope=Namespaced,shortName=ocs,categories=kubewa
// +kubebuilder:printcolumn:name="CurrentOnCall",type=string,JSONPath=`.status.currentOnCallName`
// +kubebuilder:printcolumn:name="Phone",type=string,JSONPath=`.status.currentOnCallPhone`
// +kubebuilder:printcolumn:name="NextShift",type=string,JSONPath=`.status.nextShift`
// +kubebuilder:printcolumn:name="Age",type=date,JSONPath=`.metadata.creationTimestamp`

// OnCallSchedule is the Schema for the oncallschedules API.
// It defines rotating on-call personnel that receive WhatsApp incident alerts.
type OnCallSchedule struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   OnCallScheduleSpec   `json:"spec,omitempty"`
	Status OnCallScheduleStatus `json:"status,omitempty"`
}

// +kubebuilder:object:root=true

// OnCallScheduleList contains a list of OnCallSchedule.
type OnCallScheduleList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []OnCallSchedule `json:"items"`
}
