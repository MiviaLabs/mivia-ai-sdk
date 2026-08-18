package taskrun_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/MiviaLabs/mivia-ai-sdk/events"
	"github.com/MiviaLabs/mivia-ai-sdk/ledger"
	"github.com/MiviaLabs/mivia-ai-sdk/taskrun"
)

// TestRunValidation is table-driven over every validation sentinel and
// the Now default. A validation failure returns the sentinel without
// running work.
func TestRunValidation(t *testing.T) {
	ctx := context.Background()
	l, err := ledger.New(nil, nil)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	bt := taskrun.Task{Key: "key", Seq: 1}
	tests := []struct {
		name string
		opts taskrun.Options
		task taskrun.Task
		want error
	}{
		{"no ledger", taskrun.Options{Actor: "actor", Owner: "owner", Lease: fixedLease}, bt, taskrun.ErrNoLedger},
		{"no owner", taskrun.Options{Ledger: l, Actor: "actor", Lease: fixedLease}, bt, taskrun.ErrNoOwner},
		{"no actor", taskrun.Options{Ledger: l, Owner: "owner", Lease: fixedLease}, bt, taskrun.ErrNoActor},
		{"no lease", taskrun.Options{Ledger: l, Actor: "actor", Owner: "owner"}, bt, taskrun.ErrNoLease},
		{"no key", runOpts(l), taskrun.Task{Seq: 1}, taskrun.ErrNoKey},
		{"now default", func() taskrun.Options { o := runOpts(l); o.Now = nil; return o }(), bt, nil},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			called := 0
			got := taskrun.Run(ctx, tc.opts, tc.task, func(context.Context) error {
				called++
				return nil
			})
			if tc.want != nil {
				if !errors.Is(got, tc.want) {
					t.Fatalf("Run = %v, want %v", got, tc.want)
				}
				if called != 0 {
					t.Fatalf("work ran on a validation failure (%d calls)", called)
				}
				return
			}
			if got != nil {
				t.Fatalf("Run = %v, want nil", got)
			}
			st, found, err := l.State(ctx, tc.task.Key)
			if err != nil {
				t.Fatalf("State: %v", err)
			}
			if !found || st.Status != ledger.StatusCompleted {
				t.Fatalf("Status = %q (found %v), want StatusCompleted", st.Status, found)
			}
		})
	}
}

// TestValidationEmitsNoAdmitted proves a validation failure on a
// bus-wired ledger emits no AdmittedEvent.
func TestValidationEmitsNoAdmitted(t *testing.T) {
	ctx := context.Background()
	admitted := 0
	bus := events.New()
	if err := bus.Subscribe(ledger.AdmittedEvent, func(_ context.Context, _ events.Event) error {
		admitted++
		return nil
	}); err != nil {
		t.Fatalf("Subscribe: %v", err)
	}
	l, err := ledger.New(nil, bus)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	opts := taskrun.Options{Ledger: l, Actor: "actor", Lease: time.Hour}
	err = taskrun.Run(ctx, opts, taskrun.Task{Key: "key", Seq: 1}, func(context.Context) error { return nil })
	if !errors.Is(err, taskrun.ErrNoOwner) {
		t.Fatalf("Run = %v, want ErrNoOwner", err)
	}
	if admitted != 0 {
		t.Fatalf("AdmittedEvent fired %d times on a validation failure", admitted)
	}
}
