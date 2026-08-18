// Package agent_test also holds the scheduled and triggered
// invocation test. It proves scheduler.Job and trigger.Action each
// wrap agent.Run as a plain closure, with ledger admission around the
// task and a channel.Notifier-shaped stub resolving the gated step.
package agent_test

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"
	"time"

	"github.com/MiviaLabs/mivia-ai-sdk/agent"
	"github.com/MiviaLabs/mivia-ai-sdk/channel"
	"github.com/MiviaLabs/mivia-ai-sdk/discovery"
	"github.com/MiviaLabs/mivia-ai-sdk/envelope"
	"github.com/MiviaLabs/mivia-ai-sdk/events"
	"github.com/MiviaLabs/mivia-ai-sdk/flow"
	"github.com/MiviaLabs/mivia-ai-sdk/identity"
	"github.com/MiviaLabs/mivia-ai-sdk/ledger"
	"github.com/MiviaLabs/mivia-ai-sdk/machine"
	"github.com/MiviaLabs/mivia-ai-sdk/scheduler"
	"github.com/MiviaLabs/mivia-ai-sdk/trigger"
)

// invokedFixture is the small fixture the scheduling scenarios share:
// one agent with a single gated step, one ledger, one bus, and the
// notifier stub that resolves the step.
type invokedFixture struct {
	a        *agent.Agent
	m        *machine.Definition
	l        *ledger.Ledger
	bus      *events.Bus
	notified atomic.Int64
}

// newInvokedFixture builds the fixture. The plan is deliberately one
// step: this scenario's concern is the scheduling wrapper, not the
// graph shape system_composition_integration_test.go already proves.
func newInvokedFixture(t testing.TB) *invokedFixture {
	t.Helper()
	fx := &invokedFixture{bus: newSystemBus(t)}
	id, err := identity.New()
	if err != nil {
		t.Fatalf("identity.New() unexpected error: %v", err)
	}
	plan, err := flow.New([]flow.Step{
		{ID: "sweep", To: "swept", Payload: "sweep the queue"},
	}, nil)
	if err != nil {
		t.Fatalf("flow.New() unexpected error: %v", err)
	}
	card := discovery.Card{Name: "Sweeper", Capabilities: []string{"sweep"}}
	if fx.a, err = agent.New(id, card, plan); err != nil {
		t.Fatalf("agent.New() unexpected error: %v", err)
	}
	if fx.m, err = machine.New("start",
		machine.Transition{From: "start", To: "swept", Trigger: "go-sweep"},
	); err != nil {
		t.Fatalf("machine.New() unexpected error: %v", err)
	}
	fx.l = newSystemLedger(t, fx.bus)
	return fx
}

// ackWaitFromNotifier adapts a channel.Notifier to agent.AckWait's
// exact signature. The notifier answers the question; this closure
// turns the answer into a real envelope.Ack. Assigning the result to
// an agent.AckWait compiles only if the two shapes compose.
func ackWaitFromNotifier(n channel.Notifier, from string) agent.AckWait {
	return func(ctx context.Context, msg envelope.Message) (envelope.Ack, error) {
		answer, err := n(ctx, channel.Question{
			ID:        msg.ID,
			Recipient: from,
			Payload:   msg.Payload,
		})
		if err != nil {
			return envelope.Ack{}, err
		}
		ack, err := envelope.NewAck(msg, from, answer.Payload)
		if err != nil {
			return envelope.Ack{}, err
		}
		if !answer.Approved {
			// Ack has no reject state; a refusal is a correction, which
			// Run treats as a non-confirmed ack and halts on.
			return ack.Correct("the operator refused " + msg.ID), nil
		}
		return ack.Confirm(), nil
	}
}

// runTask is the claim-run-complete closure both scheduler.Job and
// trigger.Action wrap. It admits and claims key, runs the agent, then
// completes the key with the ledger's own terminal status.
func (fx *invokedFixture) runTask(t testing.TB, key ledger.IdempotencyKey, threadID string) func(context.Context) error {
	notifier := channel.Notifier(func(ctx context.Context, q channel.Question) (channel.Answer, error) {
		fx.notified.Add(1)
		return channel.Answer{QuestionID: q.ID, Approved: true, Payload: "acknowledged " + q.ID}, nil
	})
	wait := ackWaitFromNotifier(notifier, "operator")
	return func(ctx context.Context) error {
		now := time.Now()
		fence := admitAndClaim(t, fx.l, "invoker", key, "owner-invoked", now)
		status, _, err := fx.a.Run(ctx, threadID, fx.m, machine.InOut{}, wait, fx.bus, nil, "", nil)
		outcome := ledger.StatusCompleted
		if err != nil || status != machine.Status("swept") {
			outcome = ledger.StatusFailed
		}
		if cerr := fx.l.Complete(ctx, "invoker", key, "owner-invoked", fence, outcome, now); cerr != nil {
			return cerr
		}
		return err
	}
}

// TestScheduledInvocationCompletesTheLedgerTask drives one
// scheduler.Job through a single deterministic tick and proves the run
// completed its ledger task without firing JobFailedEvent.
func TestScheduledInvocationCompletesTheLedgerTask(t *testing.T) {
	fx := newInvokedFixture(t)
	const key = ledger.IdempotencyKey("scheduled-sweep-1")
	var failures atomic.Int64
	subscribeCounter(t, fx.bus, scheduler.JobFailedEvent, &failures)

	fired := make(chan struct{}, 1)
	task := fx.runTask(t, key, "scheduled-thread")
	// At is one-shot: its Next returns the zero time once the listed
	// time has passed. That pins the job to a single deterministic
	// firing, so the ledger key is admitted exactly once and every
	// count below stays exact. Every would keep firing until cancel.
	s := scheduler.New()
	if err := s.Add("sweep-job", scheduler.At(time.Now().Add(5*time.Millisecond)), func(ctx context.Context) error {
		err := task(ctx)
		fired <- struct{}{}
		return err
	}); err != nil {
		t.Fatalf("scheduler.Add() unexpected error: %v", err)
	}
	runSchedulerUntilFired(t, s, fx.bus, fired)

	if got := ledgerStatus(t, fx.l, key); got != ledger.StatusCompleted {
		t.Fatalf("ledger.State(%q).Status = %q, want %q", key, got, ledger.StatusCompleted)
	}
	if got := failures.Load(); got != 0 {
		t.Fatalf("JobFailedEvent fired %d times on the happy path, want 0", got)
	}
	if got := fx.notified.Load(); got != 1 {
		t.Fatalf("notifier called %d times, want 1 per gated step", got)
	}
}

// TestScheduledInvocationFailureEmitsJobFailedEvent proves a job body
// that returns an error emits exactly one JobFailedEvent.
func TestScheduledInvocationFailureEmitsJobFailedEvent(t *testing.T) {
	fx := newInvokedFixture(t)
	var failures atomic.Int64
	subscribeCounter(t, fx.bus, scheduler.JobFailedEvent, &failures)

	fired := make(chan struct{}, 1)
	wantErr := errors.New("sweep: the queue backend is down")
	s := scheduler.New()
	if err := s.Add("failing-job", scheduler.At(time.Now().Add(5*time.Millisecond)), func(ctx context.Context) error {
		fired <- struct{}{}
		return wantErr
	}); err != nil {
		t.Fatalf("scheduler.Add() unexpected error: %v", err)
	}
	runSchedulerUntilFired(t, s, fx.bus, fired)

	if got := failures.Load(); got != 1 {
		t.Fatalf("JobFailedEvent fired %d times, want exactly 1", got)
	}
}

// TestTriggeredInvocationCompletesTheLedgerTask proves trigger.Action
// wraps agent.Run the same way scheduler.Job does, and that a false
// condition blocks the action entirely.
func TestTriggeredInvocationCompletesTheLedgerTask(t *testing.T) {
	fx := newInvokedFixture(t)
	const key = ledger.IdempotencyKey("triggered-sweep-1")
	reg := trigger.New()
	if err := reg.Add("on-queue-depth",
		func(ctx context.Context) (bool, error) { return true, nil },
		fx.runTask(t, key, "triggered-thread"),
	); err != nil {
		t.Fatalf("trigger.Add() unexpected error: %v", err)
	}
	if err := reg.Fire(context.Background(), "on-queue-depth"); err != nil {
		t.Fatalf("trigger.Fire() unexpected error: %v", err)
	}
	if got := ledgerStatus(t, fx.l, key); got != ledger.StatusCompleted {
		t.Fatalf("ledger.State(%q).Status = %q, want %q", key, got, ledger.StatusCompleted)
	}

	var ran atomic.Int64
	if err := reg.Add("never",
		func(ctx context.Context) (bool, error) { return false, nil },
		func(ctx context.Context) error { ran.Add(1); return nil },
	); err != nil {
		t.Fatalf("trigger.Add() unexpected error: %v", err)
	}
	if err := reg.Fire(context.Background(), "never"); !errors.Is(err, trigger.ErrConditionNotMet) {
		t.Fatalf("Fire() error = %v, want errors.Is match for trigger.ErrConditionNotMet", err)
	}
	if got := ran.Load(); got != 0 {
		t.Fatalf("action ran %d times behind a false condition, want 0", got)
	}
}

// runSchedulerUntilFired runs s in a goroutine, waits for one job
// firing, then cancels ctx and waits for Run to return. It fails the
// test when no job fires before the deadline.
func runSchedulerUntilFired(t *testing.T, s *scheduler.Scheduler, bus *events.Bus, fired <-chan struct{}) {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- s.Run(ctx, bus) }()

	select {
	case <-fired:
	case <-time.After(5 * time.Second):
		cancel()
		<-done
		t.Fatal("no job fired within the deadline")
	}
	cancel()
	if err := <-done; !errors.Is(err, context.Canceled) {
		t.Fatalf("scheduler.Run() error = %v, want context.Canceled", err)
	}
}

// subscribeCounter subscribes an atomic counter to name on bus.
func subscribeCounter(t testing.TB, bus *events.Bus, name events.Name, counter *atomic.Int64) {
	t.Helper()
	if err := bus.Subscribe(name, func(ctx context.Context, e events.Event) error {
		counter.Add(1)
		return nil
	}); err != nil {
		t.Fatalf("Subscribe(%q) unexpected error: %v", name, err)
	}
}
