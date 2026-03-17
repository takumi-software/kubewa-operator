package controller

import (
	"testing"
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	kubewav1alpha1 "github.com/takumi-software/kubewa-operator/api/v1alpha1"
)

func TestComputeCurrentIndex_Daily(t *testing.T) {
	epoch := time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)
	ocs := &kubewav1alpha1.OnCallSchedule{
		ObjectMeta: metav1.ObjectMeta{
			CreationTimestamp: metav1.Time{Time: epoch},
		},
		Spec: kubewav1alpha1.OnCallScheduleSpec{
			Rotation: kubewav1alpha1.RotationDaily,
			Schedule: []kubewav1alpha1.OnCallEntry{
				{Name: "Alice", Phone: "+1111"},
				{Name: "Bob", Phone: "+2222"},
				{Name: "Charlie", Phone: "+3333"},
			},
		},
	}

	cases := []struct {
		now      time.Time
		wantIdx  int
		wantName string
	}{
		{epoch, 0, "Alice"},                                   // day 0
		{epoch.Add(24 * time.Hour), 1, "Bob"},                // day 1
		{epoch.Add(48 * time.Hour), 2, "Charlie"},            // day 2
		{epoch.Add(72 * time.Hour), 0, "Alice"},              // day 3 wraps
		{epoch.Add(100 * time.Hour), 1, "Bob"},               // day 4 (4%3=1)
	}

	for _, c := range cases {
		idx, _ := computeCurrentIndex(c.now, ocs)
		if idx != c.wantIdx {
			t.Errorf("at %v: index=%d, want %d", c.now, idx, c.wantIdx)
		}
		got := ocs.Spec.Schedule[idx].Name
		if got != c.wantName {
			t.Errorf("at %v: name=%s, want %s", c.now, got, c.wantName)
		}
	}
}

func TestComputeCurrentIndex_Weekly(t *testing.T) {
	epoch := time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)
	ocs := &kubewav1alpha1.OnCallSchedule{
		ObjectMeta: metav1.ObjectMeta{
			CreationTimestamp: metav1.Time{Time: epoch},
		},
		Spec: kubewav1alpha1.OnCallScheduleSpec{
			Rotation: kubewav1alpha1.RotationWeekly,
			Schedule: []kubewav1alpha1.OnCallEntry{
				{Name: "Alice", Phone: "+1111"},
				{Name: "Bob", Phone: "+2222"},
			},
		},
	}

	// Week 0 → Alice, week 1 → Bob, week 2 → Alice, ...
	cases := []struct {
		daysOffset int
		wantIdx    int
	}{
		{0, 0},
		{6, 0},
		{7, 1},
		{13, 1},
		{14, 0},
	}
	for _, c := range cases {
		now := epoch.Add(time.Duration(c.daysOffset) * 24 * time.Hour)
		idx, _ := computeCurrentIndex(now, ocs)
		if idx != c.wantIdx {
			t.Errorf("day %d: index=%d, want %d", c.daysOffset, idx, c.wantIdx)
		}
	}
}

func TestComputeCurrentIndex_NextShift(t *testing.T) {
	epoch := time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)
	ocs := &kubewav1alpha1.OnCallSchedule{
		ObjectMeta: metav1.ObjectMeta{
			CreationTimestamp: metav1.Time{Time: epoch},
		},
		Spec: kubewav1alpha1.OnCallScheduleSpec{
			Rotation: kubewav1alpha1.RotationDaily,
			Schedule: []kubewav1alpha1.OnCallEntry{
				{Name: "Alice", Phone: "+1111"},
				{Name: "Bob", Phone: "+2222"},
			},
		},
	}

	now := epoch.Add(12 * time.Hour) // halfway through day 0
	_, nextShift := computeCurrentIndex(now, ocs)
	expectedNextShift := epoch.Add(24 * time.Hour)
	if !nextShift.Equal(expectedNextShift) {
		t.Errorf("nextShift=%v, want %v", nextShift, expectedNextShift)
	}
}

func TestLoadTimezone(t *testing.T) {
	cases := []struct {
		tz      string
		wantErr bool
	}{
		{"UTC", false},
		{"", false},
		{"America/Sao_Paulo", false},
		{"America/New_York", false},
		{"Invalid/Zone", true},
	}
	for _, c := range cases {
		_, err := loadTimezone(c.tz)
		if c.wantErr && err == nil {
			t.Errorf("loadTimezone(%q): expected error", c.tz)
		}
		if !c.wantErr && err != nil {
			t.Errorf("loadTimezone(%q): unexpected error: %v", c.tz, err)
		}
	}
}

func TestSetCondition(t *testing.T) {
	conditions := []metav1.Condition{}
	setCondition(&conditions, metav1.Condition{
		Type:   "Ready",
		Status: metav1.ConditionTrue,
		Reason: "Test",
	})
	if len(conditions) != 1 {
		t.Fatalf("expected 1 condition, got %d", len(conditions))
	}
	// Update existing.
	setCondition(&conditions, metav1.Condition{
		Type:   "Ready",
		Status: metav1.ConditionFalse,
		Reason: "Updated",
	})
	if len(conditions) != 1 {
		t.Fatalf("expected 1 condition after update, got %d", len(conditions))
	}
	if conditions[0].Status != metav1.ConditionFalse {
		t.Errorf("condition status not updated")
	}
}
