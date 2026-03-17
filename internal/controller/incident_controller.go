package controller

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/go-logr/logr"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/tools/record"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/log"

	kubewav1alpha1 "github.com/takumi-software/kubewa-operator/api/v1alpha1"
	aiclient "github.com/takumi-software/kubewa-operator/internal/ai"
	twiliointernal "github.com/takumi-software/kubewa-operator/internal/twilio"
)

// defaultAckTimeout is used when AckTimeout is not parseable.
const defaultAckTimeout = 15 * time.Minute

// IncidentReconciler reconciles Incident objects.
//
// +kubebuilder:rbac:groups=kubewa.dev,resources=incidents,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=kubewa.dev,resources=incidents/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=kubewa.dev,resources=incidents/finalizers,verbs=update
// +kubebuilder:rbac:groups=kubewa.dev,resources=oncallschedules,verbs=get;list;watch
// +kubebuilder:rbac:groups="",resources=events,verbs=create;patch
// +kubebuilder:rbac:groups=apps,resources=deployments;statefulsets,verbs=get;list;watch;update;patch
// +kubebuilder:rbac:groups=apps,resources=deployments/scale;statefulsets/scale,verbs=get;update
// +kubebuilder:rbac:groups=apps,resources=replicasets,verbs=get;list;watch
type IncidentReconciler struct {
	client.Client
	Scheme       *runtime.Scheme
	Log          logr.Logger
	TwilioClient *twiliointernal.Client
	AIClient     *aiclient.Client
	Recorder     record.EventRecorder
}

// Reconcile implements the reconciliation loop for Incident.
func (r *IncidentReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	logger := log.FromContext(ctx).WithValues("incident", req.NamespacedName)

	incident := &kubewav1alpha1.Incident{}
	if err := r.Get(ctx, req.NamespacedName, incident); err != nil {
		return ctrl.Result{}, client.IgnoreNotFound(err)
	}

	// Skip terminal incidents.
	if incident.Status.State == kubewav1alpha1.IncidentResolved {
		logger.Info("incident already resolved, skipping")
		return ctrl.Result{}, nil
	}

	incidentCopy := incident.DeepCopy()
	result, err := r.reconcileIncident(ctx, logger, incidentCopy)
	if err != nil {
		logger.Error(err, "reconcile incident")
	}
	return result, err
}

func (r *IncidentReconciler) reconcileIncident(ctx context.Context, logger logr.Logger, incident *kubewav1alpha1.Incident) (ctrl.Result, error) {
	// 1. Resolve on-call phone from the referenced OnCallSchedule.
	phone, scheduleName, err := r.resolveOnCallPhone(ctx, incident)
	if err != nil {
		logger.Error(err, "resolve on-call phone")
		r.emitEvent(incident, corev1.EventTypeWarning, "OnCallResolveFailed", err.Error())
		return ctrl.Result{RequeueAfter: 30 * time.Second}, err
	}

	// 2. If no alert has been sent yet, generate AI suggestion (if available) and send.
	if incident.Status.State == "" || incident.Status.State == kubewav1alpha1.IncidentOpen {
		if incident.Status.AlertSentAt == nil {
			if err := r.sendInitialAlert(ctx, logger, incident, phone, scheduleName); err != nil {
				return ctrl.Result{RequeueAfter: 30 * time.Second}, err
			}
			// Requeue to check ack timeout.
			return ctrl.Result{RequeueAfter: r.ackTimeout(incident)}, nil
		}
	}

	// 3. Check ack timeout and escalate to backup if needed.
	if incident.Status.State == kubewav1alpha1.IncidentOpen {
		ackDeadline := incident.Status.AlertSentAt.Time.Add(r.ackTimeout(incident))
		if time.Now().After(ackDeadline) {
			if err := r.escalateToBackup(ctx, logger, incident); err != nil {
				return ctrl.Result{RequeueAfter: 30 * time.Second}, err
			}
		} else {
			return ctrl.Result{RequeueAfter: time.Until(ackDeadline) + time.Second}, nil
		}
	}

	// 4. Check escalation ack timeout for backup.
	if incident.Status.State == kubewav1alpha1.IncidentEscalatedToBackup {
		if incident.Status.EscalatedAt != nil {
			backupDeadline := incident.Status.EscalatedAt.Time.Add(r.ackTimeout(incident))
			if time.Now().After(backupDeadline) {
				logger.Info("backup also did not ack, incident remains escalated")
				r.emitEvent(incident, corev1.EventTypeWarning, "BackupAckTimeout",
					"backup contact did not acknowledge within timeout")
			} else {
				return ctrl.Result{RequeueAfter: time.Until(backupDeadline) + time.Second}, nil
			}
		}
	}

	return ctrl.Result{}, nil
}

// sendInitialAlert sends the first WhatsApp alert and updates status.
func (r *IncidentReconciler) sendInitialAlert(ctx context.Context, logger logr.Logger, incident *kubewav1alpha1.Incident, phone, _ string) error {
	// Generate AI-suggested fix if not already set.
	suggestedFix := incident.Spec.SuggestedFix
	if suggestedFix == "" && r.AIClient != nil {
		var aiErr error
		suggestedFix, aiErr = r.AIClient.SuggestFix(ctx, incident.Spec.Title, string(incident.Spec.Severity), incident.Namespace)
		if aiErr != nil {
			logger.Error(aiErr, "AI suggest fix failed, continuing without suggestion")
		}
	}

	// Build action buttons (max 3).
	buttons := make([]twiliointernal.ActionButton, 0, len(incident.Spec.RemediationActions))
	for _, a := range incident.Spec.RemediationActions {
		buttons = append(buttons, twiliointernal.ActionButton{
			ID:    a.Name,
			Title: actionTitle(a),
		})
		if len(buttons) == 3 {
			break
		}
	}

	sid, err := r.TwilioClient.SendIncidentAlert(
		phone,
		incident.Spec.Title,
		string(incident.Spec.Severity),
		suggestedFix,
		buttons,
	)
	if err != nil {
		r.emitEvent(incident, corev1.EventTypeWarning, "AlertSendFailed", err.Error())
		return fmt.Errorf("send initial alert: %w", err)
	}

	now := metav1.NewTime(time.Now())
	incident.Status.State = kubewav1alpha1.IncidentOpen
	incident.Status.LastMessageSID = sid
	incident.Status.NotifiedPhone = phone
	incident.Status.AlertSentAt = &now

	// Update spec with AI suggestion if we generated one.
	if suggestedFix != "" && incident.Spec.SuggestedFix == "" {
		incident.Spec.SuggestedFix = suggestedFix
		if err := r.Update(ctx, incident); err != nil {
			logger.Error(err, "update spec with AI suggestion")
		}
	}

	if err := r.Status().Update(ctx, incident); err != nil {
		return fmt.Errorf("update status after sending alert: %w", err)
	}

	r.emitEvent(incident, corev1.EventTypeNormal, "AlertSent",
		fmt.Sprintf("WhatsApp alert sent to %s (SID: %s)", phone, sid))
	logger.Info("alert sent", "phone", phone, "sid", sid)
	return nil
}

// escalateToBackup notifies the backup contact and updates status.
func (r *IncidentReconciler) escalateToBackup(ctx context.Context, logger logr.Logger, incident *kubewav1alpha1.Incident) error {
	ocs := &kubewav1alpha1.OnCallSchedule{}
	if err := r.Get(ctx, client.ObjectKey{
		Name:      incident.Spec.OnCallScheduleRef,
		Namespace: incident.Namespace,
	}, ocs); err != nil {
		return fmt.Errorf("get schedule for escalation: %w", err)
	}

	if ocs.Spec.Backup == nil {
		logger.Info("no backup configured, cannot escalate")
		return nil
	}

	backupPhone := ocs.Spec.Backup.Phone
	body := fmt.Sprintf("⚠️ *ESCALATION*: Primary on-call did not respond.\n\n"+
		"Incident: *%s*\nSeverity: *%s*\n\nPlease respond immediately.",
		incident.Spec.Title, strings.ToUpper(string(incident.Spec.Severity)))

	sid, err := r.TwilioClient.SendMessage(backupPhone, body)
	if err != nil {
		r.emitEvent(incident, corev1.EventTypeWarning, "EscalationFailed", err.Error())
		return fmt.Errorf("send escalation message: %w", err)
	}

	now := metav1.NewTime(time.Now())
	incident.Status.State = kubewav1alpha1.IncidentEscalatedToBackup
	incident.Status.NotifiedPhone = backupPhone
	incident.Status.LastMessageSID = sid
	incident.Status.EscalatedAt = &now

	if err := r.Status().Update(ctx, incident); err != nil {
		return fmt.Errorf("update status after escalation: %w", err)
	}

	r.emitEvent(incident, corev1.EventTypeWarning, "EscalatedToBackup",
		fmt.Sprintf("Incident escalated to backup contact %s (SID: %s)", backupPhone, sid))
	logger.Info("escalated to backup", "backupPhone", backupPhone)
	return nil
}

// resolveOnCallPhone fetches the OnCallSchedule referenced by the incident and returns
// the current on-call phone number.
func (r *IncidentReconciler) resolveOnCallPhone(ctx context.Context, incident *kubewav1alpha1.Incident) (string, string, error) {
	ocs := &kubewav1alpha1.OnCallSchedule{}
	key := client.ObjectKey{
		Name:      incident.Spec.OnCallScheduleRef,
		Namespace: incident.Namespace,
	}
	if err := r.Get(ctx, key, ocs); err != nil {
		return "", "", fmt.Errorf("get OnCallSchedule %q: %w", incident.Spec.OnCallScheduleRef, err)
	}

	phone := ocs.Status.CurrentOnCallPhone
	if phone == "" && len(ocs.Spec.Schedule) > 0 {
		phone = ocs.Spec.Schedule[0].Phone
	}
	if phone == "" {
		return "", "", fmt.Errorf("OnCallSchedule %q has no current on-call phone", incident.Spec.OnCallScheduleRef)
	}
	return phone, ocs.Name, nil
}

// ackTimeout returns the parsed ack timeout duration for the incident's schedule.
func (r *IncidentReconciler) ackTimeout(incident *kubewav1alpha1.Incident) time.Duration {
	// We don't have direct access to the schedule here; use a reasonable default.
	// A future improvement could cache the schedule.
	return defaultAckTimeout
}

// emitEvent records a Kubernetes Event for the incident.
func (r *IncidentReconciler) emitEvent(incident *kubewav1alpha1.Incident, eventType, reason, message string) {
	if r.Recorder != nil {
		r.Recorder.Event(incident, eventType, reason, message)
	}
}

// actionTitle returns a short button label for a RemediationActionSpec.
func actionTitle(a kubewav1alpha1.RemediationActionSpec) string {
	if a.Name != "" {
		return a.Name
	}
	return string(a.Type)
}

// SetupWithManager registers the controller with the manager.
func (r *IncidentReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&kubewav1alpha1.Incident{}).
		Complete(r)
}
