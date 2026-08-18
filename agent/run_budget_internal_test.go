// Package agent holds the internal-state case for confirmStep's
// budget check: a direct proof that Fits runs before hb.Beat, using
// confirmStep's own closure instead of Run. Run's caller-visible
// behavior always forgets its heartbeat id on return (see run.go's
// deferred hb.Forget), so a black-box Run-level test cannot tell
// "Fits failed before Beat ran" apart from "Beat ran, then Fits
// failed, then Forget erased it": both leave hb.Alive false once Run
// returns. Calling confirmStep directly skips Run's Forget and
// observes hb state right after the closure returns.
package agent

import (
	"context"
	"testing"
	"time"

	"github.com/MiviaLabs/mivia-ai-sdk/contextbudget"
	"github.com/MiviaLabs/mivia-ai-sdk/discovery"
	"github.com/MiviaLabs/mivia-ai-sdk/envelope"
	"github.com/MiviaLabs/mivia-ai-sdk/events"
	"github.com/MiviaLabs/mivia-ai-sdk/flow"
	"github.com/MiviaLabs/mivia-ai-sdk/heartbeat"
	"github.com/MiviaLabs/mivia-ai-sdk/identity"
)

// TestConfirmStepFitsFailureNeverBeats proves confirmStep checks
// budget.Fits before it calls hb.Beat: with a budget whose Fits check
// fails on the one step confirmStep is asked to confirm, hb.Alive for
// the beat id reads false immediately after the closure returns,
// without Run's own deferred hb.Forget in play to explain it away. A
// build that swaps the Fits check and the hb.Beat call (Beat first,
// Fits second) still records a beat before it returns the error, and
// this test catches that: hb.Alive would read true.
func TestConfirmStepFitsFailureNeverBeats(t *testing.T) {
	id, err := identity.New()
	if err != nil {
		t.Fatalf("identity.New() unexpected error: %v", err)
	}
	plan, err := flow.New([]flow.Step{{ID: "step-a", To: "done", Payload: "do the thing"}}, nil)
	if err != nil {
		t.Fatalf("flow.New() unexpected error: %v", err)
	}
	a, err := New(id, discovery.Card{Name: "Runner", Capabilities: []string{"run"}}, plan)
	if err != nil {
		t.Fatalf("New() unexpected error: %v", err)
	}

	bus := events.New()
	if err := bus.Subscribe(MessageDeliveredEvent, func(ctx context.Context, e events.Event) error { return nil }); err != nil {
		t.Fatalf("Subscribe(MessageDeliveredEvent) unexpected error: %v", err)
	}

	hb, err := heartbeat.New(time.Minute)
	if err != nil {
		t.Fatalf("heartbeat.New() unexpected error: %v", err)
	}
	hbID := id.Signer() + ":thread-1"

	waitCalls := 0
	wait := func(ctx context.Context, msg envelope.Message) (envelope.Ack, error) {
		waitCalls++
		ack, err := envelope.NewAck(msg, "receiver", "restating "+msg.ID)
		if err != nil {
			return envelope.Ack{}, err
		}
		return ack.Confirm(), nil
	}

	// "do the thing" is 12 bytes; a cap of 1 is below it, so Fits
	// fails on the run's single step, before wait would ever run.
	budget := &contextbudget.Limits{MaxBytes: 1}
	var built []envelope.Message
	var runningBytes int
	confirm := a.confirmStep("thread-1", wait, bus, &built, hb, hbID, "", budget, &runningBytes)

	step := flow.Step{ID: "step-a", To: "done", Payload: "do the thing"}
	err = confirm(context.Background(), step)
	if err == nil {
		t.Fatal("confirm() = nil error, want a non-nil ErrOverBudget-wrapping error")
	}
	if waitCalls != 0 {
		t.Fatalf("wait called %d times, want 0: Fits must fail before wait runs", waitCalls)
	}
	if hb.Alive(hbID, time.Now()) {
		t.Fatal("hb.Alive(id) = true right after confirm() fails Fits, want false: Fits must run before hb.Beat")
	}
}
