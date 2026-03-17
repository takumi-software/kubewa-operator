package v1alpha1

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// IncidentSeverity categorises the urgency of an incident.
// +kubebuilder:validation:Enum=critical;high;medium;low
type IncidentSeverity string

const (
	SeverityCritical IncidentSeverity = "critical"
	SeverityHigh     IncidentSeverity = "high"
	SeverityMedium   IncidentSeverity = "medium"
	SeverityLow      IncidentSeverity = "low"
)

// IncidentState represents the lifecycle of an incident.
// +kubebuilder:validation:Enum=Open;Acked;Resolved;EscalatedToBackup
type IncidentState string

const (
	IncidentOpen              IncidentState = "Open"
	IncidentAcked             IncidentState = "Acked"
	IncidentResolved          IncidentState = "Resolved"
	IncidentEscalatedToBackup IncidentState = "EscalatedToBackup"
)

// RemediationActionType defines the kind of K8s remediation action.
// +kubebuilder:validation:Enum=RollbackDeployment;ScaleDeployment;RestartDeployment;PatchDeployment;RollbackStatefulSet;ScaleStatefulSet;RestartStatefulSet;CustomCommand
type RemediationActionType string

const (
	ActionRollbackDeployment  RemediationActionType = "RollbackDeployment"
	ActionScaleDeployment     RemediationActionType = "ScaleDeployment"
	ActionRestartDeployment   RemediationActionType = "RestartDeployment"
	ActionPatchDeployment     RemediationActionType = "PatchDeployment"
	ActionRollbackStatefulSet RemediationActionType = "RollbackStatefulSet"
	ActionScaleStatefulSet    RemediationActionType = "ScaleStatefulSet"
	ActionRestartStatefulSet  RemediationActionType = "RestartStatefulSet"
	ActionCustomCommand       RemediationActionType = "CustomCommand"
)

// RemediationActionSpec describes a single remediation action available for the incident.
type RemediationActionSpec struct {
	// Name is a human-readable identifier for this action (e.g. "rollback").
	Name string `json:"name"`

	// Type is the kind of K8s action to perform.
	Type RemediationActionType `json:"type"`

	// TargetNamespace is the namespace of the target resource.
	// Defaults to the incident namespace if omitted.
	// +optional
	TargetNamespace string `json:"targetNamespace,omitempty"`

	// TargetName is the name of the target K8s resource.
	TargetName string `json:"targetName"`

	// Replicas is the desired replica count for scale actions.
	// +optional
	Replicas *int32 `json:"replicas,omitempty"`

	// Patch is a strategic-merge-patch JSON string for PatchDeployment actions.
	// +optional
	Patch string `json:"patch,omitempty"`

	// ApprovalRequired indicates whether this action needs explicit WhatsApp confirmation.
	// +kubebuilder:default=true
	ApprovalRequired bool `json:"approvalRequired"`
}

// AuditEntry records a single action taken during incident handling.
type AuditEntry struct {
	// Timestamp is when the action was taken.
	Timestamp metav1.Time `json:"timestamp"`

	// Actor is the phone number or system that triggered the action.
	Actor string `json:"actor"`

	// Action describes what was done.
	Action string `json:"action"`

	// MessageSID is the Twilio SID of the WhatsApp message that triggered this action.
	// +optional
	MessageSID string `json:"messageSID,omitempty"`

	// Result is "success" or an error message.
	Result string `json:"result"`
}

// IncidentSpec defines the desired state of an Incident.
type IncidentSpec struct {
	// Title is a short human-readable description of the incident.
	// +kubebuilder:validation:MinLength=1
	Title string `json:"title"`

	// Severity of the incident.
	// +kubebuilder:default=high
	Severity IncidentSeverity `json:"severity"`

	// Cluster is the name of the target cluster (for multi-cluster setups).
	// +optional
	Cluster string `json:"cluster,omitempty"`

	// Namespace is the primary K8s namespace involved.
	// +optional
	Namespace string `json:"namespace,omitempty"`

	// OnCallScheduleRef references the OnCallSchedule to determine who to notify.
	// +kubebuilder:validation:MinLength=1
	OnCallScheduleRef string `json:"onCallScheduleRef"`

	// SuggestedFix is an AI-generated remediation suggestion in natural language.
	// +optional
	SuggestedFix string `json:"suggestedFix,omitempty"`

	// RemediationActions is the list of quick-action buttons offered via WhatsApp.
	// +optional
	// +kubebuilder:validation:MaxItems=3
	RemediationActions []RemediationActionSpec `json:"remediationActions,omitempty"`

	// Source indicates the origin of the incident (e.g. "prometheus", "argocd", "manual").
	// +optional
	Source string `json:"source,omitempty"`

	// SourceLabels carries labels from the originating alert (e.g. Prometheus alert labels).
	// +optional
	SourceLabels map[string]string `json:"sourceLabels,omitempty"`
}

// IncidentStatus defines the observed state of an Incident.
type IncidentStatus struct {
	// State is the current lifecycle state of the incident.
	// +optional
	State IncidentState `json:"state,omitempty"`

	// LastMessageSID is the Twilio SID of the most recent WhatsApp message sent.
	// +optional
	LastMessageSID string `json:"lastMessageSID,omitempty"`

	// NotifiedPhone is the phone number that received the latest alert.
	// +optional
	NotifiedPhone string `json:"notifiedPhone,omitempty"`

	// AckedBy is the phone number that acknowledged the incident.
	// +optional
	AckedBy string `json:"ackedBy,omitempty"`

	// AckedAt is the time the incident was acknowledged.
	// +optional
	AckedAt *metav1.Time `json:"ackedAt,omitempty"`

	// ResolvedAt is the time the incident was resolved.
	// +optional
	ResolvedAt *metav1.Time `json:"resolvedAt,omitempty"`

	// ResolutionAction describes how the incident was resolved.
	// +optional
	ResolutionAction string `json:"resolutionAction,omitempty"`

	// AuditTrail contains a chronological record of all actions taken.
	// +optional
	AuditTrail []AuditEntry `json:"auditTrail,omitempty"`

	// AlertSentAt is when the initial WhatsApp alert was dispatched.
	// +optional
	AlertSentAt *metav1.Time `json:"alertSentAt,omitempty"`

	// EscalatedAt is when the incident was escalated to the backup contact.
	// +optional
	EscalatedAt *metav1.Time `json:"escalatedAt,omitempty"`

	// Conditions represent the latest available observations of the incident's state.
	// +optional
	// +listType=map
	// +listMapKey=type
	Conditions []metav1.Condition `json:"conditions,omitempty"`
}

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:resource:scope=Namespaced,shortName=inc,categories=kubewa
// +kubebuilder:printcolumn:name="Title",type=string,JSONPath=`.spec.title`
// +kubebuilder:printcolumn:name="Severity",type=string,JSONPath=`.spec.severity`
// +kubebuilder:printcolumn:name="State",type=string,JSONPath=`.status.state`
// +kubebuilder:printcolumn:name="NotifiedPhone",type=string,JSONPath=`.status.notifiedPhone`
// +kubebuilder:printcolumn:name="Age",type=date,JSONPath=`.metadata.creationTimestamp`

// Incident is the Schema for the incidents API.
// When an Incident CR is created the operator sends a WhatsApp alert with
// interactive remediation buttons to the current on-call person.
type Incident struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   IncidentSpec   `json:"spec,omitempty"`
	Status IncidentStatus `json:"status,omitempty"`
}

// +kubebuilder:object:root=true

// IncidentList contains a list of Incident.
type IncidentList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []Incident `json:"items"`
}
