// Package controller implements the reconciliation controllers for KubeWA CRDs.
package controller

import (
	"context"
	"fmt"
	"time"

	"github.com/go-logr/logr"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/log"

	kubewav1alpha1 "github.com/takumi-software/kubewa-operator/api/v1alpha1"
)

// OnCallScheduleReconciler reconciles OnCallSchedule objects.
//
// +kubebuilder:rbac:groups=kubewa.dev,resources=oncallschedules,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=kubewa.dev,resources=oncallschedules/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=kubewa.dev,resources=oncallschedules/finalizers,verbs=update
type OnCallScheduleReconciler struct {
	client.Client
	Scheme *runtime.Scheme
	Log    logr.Logger
}

// Reconcile implements the reconciliation loop for OnCallSchedule.
// It computes who is currently on call based on the rotation type and updates status.
func (r *OnCallScheduleReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	logger := log.FromContext(ctx).WithValues("oncallschedule", req.NamespacedName)

	ocs := &kubewav1alpha1.OnCallSchedule{}
	if err := r.Get(ctx, req.NamespacedName, ocs); err != nil {
		return ctrl.Result{}, client.IgnoreNotFound(err)
	}

	if len(ocs.Spec.Schedule) == 0 {
		logger.Info("schedule is empty, skipping")
		return ctrl.Result{}, nil
	}

	now := time.Now().UTC()
	tz, err := loadTimezone(ocs.Spec.Timezone)
	if err != nil {
		logger.Error(err, "invalid timezone, falling back to UTC")
		tz = time.UTC
	}
	now = now.In(tz)

	idx, nextShift := computeCurrentIndex(now, ocs)
	current := ocs.Spec.Schedule[idx]

	ocsCopy := ocs.DeepCopy()
	ocsCopy.Status.CurrentIndex = idx
	ocsCopy.Status.CurrentOnCallName = current.Name
	ocsCopy.Status.CurrentOnCallPhone = current.Phone
	ocsCopy.Status.NextShift = &metav1.Time{Time: nextShift}

	setCondition(&ocsCopy.Status.Conditions, metav1.Condition{
		Type:               "Ready",
		Status:             metav1.ConditionTrue,
		ObservedGeneration: ocs.Generation,
		Reason:             "ScheduleComputed",
		Message:            fmt.Sprintf("On-call: %s (%s)", current.Name, current.Phone),
	})

	if err := r.Status().Update(ctx, ocsCopy); err != nil {
		logger.Error(err, "update status")
		return ctrl.Result{}, err
	}

	logger.Info("rotation computed",
		"currentOnCall", current.Name,
		"phone", current.Phone,
		"nextShift", nextShift,
	)

	// Re-reconcile at the next shift boundary.
	return ctrl.Result{RequeueAfter: time.Until(nextShift) + time.Second}, nil
}

// SetupWithManager registers the controller with the manager.
func (r *OnCallScheduleReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&kubewav1alpha1.OnCallSchedule{}).
		Complete(r)
}

// computeCurrentIndex returns the schedule index for the current time slot
// and the time when the next rotation will occur.
func computeCurrentIndex(now time.Time, ocs *kubewav1alpha1.OnCallSchedule) (int, time.Time) {
	n := len(ocs.Spec.Schedule)
	if n == 0 {
		return 0, now.Add(24 * time.Hour)
	}

	var slotDuration time.Duration
	switch ocs.Spec.Rotation {
	case kubewav1alpha1.RotationDaily:
		slotDuration = 24 * time.Hour
	default: // weekly
		slotDuration = 7 * 24 * time.Hour
	}

	// Use the creation timestamp as the rotation epoch so the schedule is
	// deterministic regardless of when the operator started.
	epoch := ocs.CreationTimestamp.UTC()
	if epoch.IsZero() {
		epoch = time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)
	}
	elapsed := now.Sub(epoch)
	if elapsed < 0 {
		elapsed = 0
	}
	slotIndex := int(elapsed/slotDuration) % n
	slotStart := epoch.Add(time.Duration(int(elapsed/slotDuration)) * slotDuration)
	nextShift := slotStart.Add(slotDuration)

	return slotIndex, nextShift
}

// loadTimezone parses an IANA timezone string. Returns UTC on empty input.
func loadTimezone(tz string) (*time.Location, error) {
	if tz == "" || tz == "UTC" {
		return time.UTC, nil
	}
	return time.LoadLocation(tz)
}

// setCondition upserts a condition in the conditions slice.
func setCondition(conditions *[]metav1.Condition, cond metav1.Condition) {
	cond.LastTransitionTime = metav1.NewTime(time.Now())
	for i, c := range *conditions {
		if c.Type == cond.Type {
			(*conditions)[i] = cond
			return
		}
	}
	*conditions = append(*conditions, cond)
}
