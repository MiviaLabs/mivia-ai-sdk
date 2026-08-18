// composition_test.go proves the three composition patterns
// documented in docs/plans/trigger.md compile and behave as
// described. It declares local stand-ins for scheduler.Job,
// events.Handler, and channel.Notifier's signatures instead of
// importing those packages, so trigger stays a leaf package with no
// import edge to any of them.
package trigger_test

import (
	"context"
	"errors"
	"testing"

	"github.com/MiviaLabs/mivia-ai-sdk/trigger"
)

// job stands in for scheduler.Job: func(ctx context.Context) error.
type job func(ctx context.Context) error

// stubEvent stands in for events.Event: a name and opaque data.
type stubEvent struct {
	name string
	data string
}

// handler stands in for events.Handler: func(ctx, Event) error.
type handler func(ctx context.Context, e stubEvent) error

// stubAnswer stands in for channel.Answer: an approval flag.
type stubAnswer struct {
	approved bool
}

func TestCompositionScheduledPolling(t *testing.T) {
	r := trigger.New()
	ready := false
	sentinel := errors.New("action failed")
	calls := 0
	cond := func(context.Context) (bool, error) { return ready, nil }
	action := func(context.Context) error { calls++; return sentinel }
	if err := r.Add("poll", cond, action); err != nil {
		t.Fatalf("Add: %v", err)
	}

	var pollJob job = func(ctx context.Context) error {
		err := r.Fire(ctx, "poll")
		if errors.Is(err, trigger.ErrConditionNotMet) {
			return nil
		}
		return err
	}

	if err := pollJob(context.Background()); err != nil {
		t.Fatalf("pollJob (not ready) = %v, want nil", err)
	}
	if calls != 0 {
		t.Fatalf("Action called %d times before ready, want 0", calls)
	}

	ready = true
	err := pollJob(context.Background())
	if !errors.Is(err, sentinel) {
		t.Fatalf("pollJob (ready) = %v, want sentinel", err)
	}
	if calls != 1 {
		t.Fatalf("Action called %d times after ready, want 1", calls)
	}
}

func TestCompositionEventDriven(t *testing.T) {
	r := trigger.New()
	var ranPayload string
	action := func(context.Context) error { return nil }
	if err := r.Add("order-created", nil, action); err != nil {
		t.Fatalf("Add: %v", err)
	}
	if err := r.Add("order-cancelled", nil, action); err != nil {
		t.Fatalf("Add: %v", err)
	}

	var onEvent handler = func(ctx context.Context, e stubEvent) error {
		ranPayload = e.data
		return r.Fire(ctx, e.name)
	}

	if err := onEvent(context.Background(), stubEvent{name: "order-created", data: "order-1"}); err != nil {
		t.Fatalf("onEvent: %v", err)
	}
	if ranPayload != "order-1" {
		t.Fatalf("ranPayload = %q, want order-1", ranPayload)
	}
}

func TestCompositionAnswerGated(t *testing.T) {
	r := trigger.New()
	calls := 0
	action := func(context.Context) error { calls++; return nil }
	if err := r.Add("approved-action", nil, action); err != nil {
		t.Fatalf("Add: %v", err)
	}

	fireOnApproval := func(ctx context.Context, a stubAnswer) error {
		if !a.approved {
			return nil
		}
		return r.Fire(ctx, "approved-action")
	}

	if err := fireOnApproval(context.Background(), stubAnswer{approved: false}); err != nil {
		t.Fatalf("fireOnApproval(declined): %v", err)
	}
	if calls != 0 {
		t.Fatalf("Action called %d times on decline, want 0", calls)
	}

	if err := fireOnApproval(context.Background(), stubAnswer{approved: true}); err != nil {
		t.Fatalf("fireOnApproval(approved): %v", err)
	}
	if calls != 1 {
		t.Fatalf("Action called %d times on approval, want 1", calls)
	}
}
