package scheduler_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/MiviaLabs/mivia-ai-sdk/scheduler"
)

// TestAdd covers the per-field rejection shape and the fully valid
// call.
func TestAdd(t *testing.T) {
	noop := func(ctx context.Context) error { return nil }

	tests := []struct {
		name    string
		id      string
		sched   scheduler.Schedule
		job     scheduler.Job
		wantErr error
	}{
		{"blank id", "", scheduler.Every(time.Second), noop, scheduler.ErrBlankID},
		{"whitespace-only id", "   ", scheduler.Every(time.Second), noop, scheduler.ErrBlankID},
		{"nil schedule", "job-1", nil, noop, scheduler.ErrNilSchedule},
		{"nil job", "job-1", scheduler.Every(time.Second), nil, scheduler.ErrNilJob},
		{"valid call", "job-1", scheduler.Every(time.Second), noop, nil},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			s := scheduler.New()
			err := s.Add(tc.id, tc.sched, tc.job)
			if !errors.Is(err, tc.wantErr) {
				t.Fatalf("Add(%q) error = %v, want %v", tc.id, err, tc.wantErr)
			}
		})
	}
}

// TestAddDuplicateID covers ErrDuplicateID on a second Add for the
// same id.
func TestAddDuplicateID(t *testing.T) {
	s := scheduler.New()
	noop := func(ctx context.Context) error { return nil }
	if err := s.Add("job-1", scheduler.Every(time.Second), noop); err != nil {
		t.Fatalf("first Add error = %v, want nil", err)
	}
	err := s.Add("job-1", scheduler.Every(time.Second), noop)
	if !errors.Is(err, scheduler.ErrDuplicateID) {
		t.Fatalf("second Add error = %v, want ErrDuplicateID", err)
	}
}

// TestRemove covers both orders: add-then-remove and
// remove-with-nothing-added.
func TestRemove(t *testing.T) {
	s := scheduler.New()
	noop := func(ctx context.Context) error { return nil }

	if got := s.Remove("absent"); got {
		t.Fatalf("Remove(absent) = true, want false")
	}

	if err := s.Add("job-1", scheduler.Every(time.Second), noop); err != nil {
		t.Fatalf("Add error = %v, want nil", err)
	}
	if got := s.Remove("job-1"); !got {
		t.Fatalf("Remove(job-1) = false, want true")
	}
	if got := s.Remove("job-1"); got {
		t.Fatalf("second Remove(job-1) = true, want false")
	}
}
