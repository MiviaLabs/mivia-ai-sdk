// Package agent_test also holds the whole-system composition test.
// One agent.Run drives a graph with a panel wave, a retried step, a
// caught failure, and an approval-gated tool call, over real identity,
// room, provider, memory, ledger, heartbeat, a2a, and events values.
package agent_test

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"
	"time"

	"github.com/MiviaLabs/mivia-ai-sdk/a2a"
	"github.com/MiviaLabs/mivia-ai-sdk/agent"
	"github.com/MiviaLabs/mivia-ai-sdk/contextbudget"
	"github.com/MiviaLabs/mivia-ai-sdk/discovery"
	"github.com/MiviaLabs/mivia-ai-sdk/envelope"
	"github.com/MiviaLabs/mivia-ai-sdk/events"
	"github.com/MiviaLabs/mivia-ai-sdk/flow"
	"github.com/MiviaLabs/mivia-ai-sdk/heartbeat"
	"github.com/MiviaLabs/mivia-ai-sdk/identity"
	"github.com/MiviaLabs/mivia-ai-sdk/ledger"
	"github.com/MiviaLabs/mivia-ai-sdk/machine"
	"github.com/MiviaLabs/mivia-ai-sdk/memory"
	"github.com/MiviaLabs/mivia-ai-sdk/room"
	"github.com/MiviaLabs/mivia-ai-sdk/tools"
)

// systemFixture bundles every real value the composition test wires
// together. No field is a mock; each one is the shipped type.
type systemFixture struct {
	idA        *identity.Identity
	idB        *identity.Identity
	a          *agent.Agent
	m          *machine.Definition
	r          *room.Room
	registry   *tools.Registry
	scope      *tools.Scope
	tool       *reviewTool
	store      *memory.Store
	l          *ledger.Ledger
	bus        *events.Bus
	hb         *heartbeat.Monitor
	approvals  atomic.Int64
	fireCounts map[string]*atomic.Int64
	caught     atomic.Value
	refs       []string
	declined   atomic.Int64
	aliveSeen  atomic.Int64
}

// systemPlan builds the graph the composition test runs: a two-member
// panel wave, a retried review step seeded from a provider turn and a
// memory ref, a step whose transition always fails, and a fallback
// step admitted through that failure.
func systemPlan(t testing.TB, seeded string) *flow.Definition {
	t.Helper()
	plan, err := flow.New([]flow.Step{
		{ID: "scan-a", To: "scanned", Payload: "scan the left half"},
		{ID: "scan-b", To: "scanned", Payload: "scan the right half"},
		{
			ID:      "review",
			Needs:   []string{"scan-a", "scan-b"},
			To:      "reviewed",
			Payload: seeded,
			Retry: &flow.RetryPolicy{
				MaxAttempts: 3,
				BaseDelay:   time.Microsecond,
				MaxDelay:    time.Millisecond,
				Sleep:       func(ctx context.Context, d time.Duration) {},
			},
		},
		{ID: "publish", Needs: []string{"review"}, To: "published", Payload: "publish the review"},
		{
			ID:      "recover",
			Needs:   []string{"publish"},
			To:      "recovered",
			When:    flow.AdmissionOnFailed,
			Payload: declineMarker + ": fall back after the failed publish",
		},
	}, []flow.Panel{{"scan-a", "scan-b"}})
	if err != nil {
		t.Fatalf("flow.New() unexpected error: %v", err)
	}
	return plan
}

// systemMachine builds the status model systemPlan's steps target. The
// review transition's guard fails twice before it succeeds, so the
// step's RetryPolicy has something real to retry. The publish
// transition's guard always rejects, so the fallback has a real
// failure to catch. The recover transition's guard reads the injected
// Failure through flow.FailureFrom.
func systemMachine(t testing.TB, fx *systemFixture) *machine.Definition {
	t.Helper()
	m, err := machine.New("start",
		machine.Transition{From: "start", To: "scanned", Trigger: "go-scan"},
		machine.Transition{
			From: "scanned", To: "reviewed", Trigger: "go-review",
			Guard: func(ctx context.Context) (bool, error) {
				if n := fx.fireCounts["review"].Add(1); n < 3 {
					return false, errors.New("review: transient backend failure")
				}
				return true, nil
			},
		},
		machine.Transition{
			From: "reviewed", To: "published", Trigger: "go-publish",
			Guard: func(ctx context.Context) (bool, error) {
				fx.fireCounts["publish"].Add(1)
				return false, errors.New("publish: downstream rejected the artifact")
			},
		},
		machine.Transition{
			From: "reviewed", To: "recovered", Trigger: "go-recover",
			Guard: func(ctx context.Context) (bool, error) {
				if f, ok := flow.FailureFrom(ctx); ok {
					fx.caught.Store(f)
				}
				fx.fireCounts["recover"].Add(1)
				return true, nil
			},
		},
	)
	if err != nil {
		t.Fatalf("machine.New() unexpected error: %v", err)
	}
	return m
}

// newSystemFixture wires every block the composition test needs. It
// runs one provider turn and one memory Put before it builds the plan,
// so the review step's payload carries both the model's reply and the
// content ref of the stored context blob.
func newSystemFixture(t testing.TB) *systemFixture {
	t.Helper()
	fx := &systemFixture{
		bus: newSystemBus(t),
		fireCounts: map[string]*atomic.Int64{
			"review": {}, "publish": {}, "recover": {},
		},
	}
	var err error
	if fx.idA, err = identity.New(); err != nil {
		t.Fatalf("identity.New() unexpected error: %v", err)
	}
	if fx.idB, err = identity.New(); err != nil {
		t.Fatalf("identity.New() unexpected error: %v", err)
	}
	if fx.store, err = memory.New(4096); err != nil {
		t.Fatalf("memory.New() unexpected error: %v", err)
	}
	ref, err := fx.store.Put([]byte("the shared context blob every step reads"))
	if err != nil {
		t.Fatalf("memory.Store.Put() unexpected error: %v", err)
	}
	seeded := completerReply(t, "review the diff") + " [ref " + ref + "]"

	if fx.r, err = room.New("system-room", fx.idA.Signer()); err != nil {
		t.Fatalf("room.New() unexpected error: %v", err)
	}
	if err := fx.r.Admit(fx.idB.Signer(), fx.idA.Signer()); err != nil {
		t.Fatalf("room.Admit() unexpected error: %v", err)
	}
	fx.tool = &reviewTool{}
	fx.registry = tools.New()
	if err := fx.registry.Add(fx.tool); err != nil {
		t.Fatalf("tools.Registry.Add() unexpected error: %v", err)
	}
	fx.scope = newReviewScope(approvalNotifier(&fx.approvals))
	fx.l = newSystemLedger(t, fx.bus)
	if fx.hb, err = heartbeat.New(time.Minute); err != nil {
		t.Fatalf("heartbeat.New() unexpected error: %v", err)
	}
	card := discovery.Card{
		Name:         "System Composer",
		Description:  "drives the whole-system composition scenario",
		Capabilities: []string{"review", "publish"},
	}
	if fx.a, err = agent.New(fx.idA, card, systemPlan(t, seeded)); err != nil {
		t.Fatalf("agent.New() unexpected error: %v", err)
	}
	fx.m = systemMachine(t, fx)
	return fx
}

// systemWait builds the AckWait closure the composition run uses. It
// maps each step message through a2a and back, verifies the signature
// after the hop, checks the heartbeat is alive mid-run, gates the
// message on Agent B's room membership, runs the approval-gated tool,
// stores the result, and confirms the ack.
func systemWait(t testing.TB, fx *systemFixture, threadID string) agent.AckWait {
	hbID := fx.idA.Signer() + ":" + threadID
	return func(ctx context.Context, msg envelope.Message) (envelope.Ack, error) {
		roundTripped, err := systemHop(msg)
		if err != nil {
			return envelope.Ack{}, err
		}
		if fx.hb.Alive(hbID, time.Now()) {
			fx.aliveSeen.Add(1)
		}
		receipt, err := buildReceiptMessage(fx.idB, roundTripped)
		if err != nil {
			return envelope.Ack{}, err
		}
		if err := fx.r.Accepts(receipt); err != nil {
			return envelope.Ack{}, err
		}
		if err := fx.runGatedTool(ctx, roundTripped.Payload); err != nil {
			return envelope.Ack{}, err
		}
		ack, err := envelope.NewAck(roundTripped, fx.idB.Signer(), "restating "+roundTripped.ID)
		if err != nil {
			return envelope.Ack{}, err
		}
		return ack.Confirm(), nil
	}
}

// systemHop maps msg through a2a.ToPart and a2a.FromPart, then
// re-verifies the signature the hop had to preserve.
func systemHop(msg envelope.Message) (envelope.Message, error) {
	mapped, err := a2a.ToPart(msg)
	if err != nil {
		return envelope.Message{}, err
	}
	roundTripped, err := a2a.FromPart(mapped)
	if err != nil {
		return envelope.Message{}, err
	}
	if err := roundTripped.VerifySignature(); err != nil {
		return envelope.Message{}, err
	}
	return roundTripped, nil
}

// runGatedTool runs the review tool through the approval scope. A
// declined call is recorded and swallowed, so the run continues; that
// is the realistic shape of an operator refusing one action.
func (fx *systemFixture) runGatedTool(ctx context.Context, payload string) error {
	out, err := fx.registry.RunScoped(ctx, reviewToolName, tools.InOut{Value: payload}, fx.scope)
	if errors.Is(err, tools.ErrToolDeclined) {
		fx.declined.Add(1)
		return nil
	}
	if err != nil {
		return err
	}
	result, ok := out.Value.(string)
	if !ok {
		return errors.New("review tool returned a non-string result")
	}
	ref, err := fx.store.Put([]byte(result))
	if err != nil {
		return err
	}
	fx.refs = append(fx.refs, ref)
	return nil
}

// TestSystemCompositionRunsEveryShippedBlock drives one agent.Run
// through the whole graph and asserts every block did its job: the
// panel wave ran without a Confirm, the retry retried, the fallback
// caught the failure and read it back, the approval gate ran both
// branches, the ledger claim is real, and the thread verifies.
func TestSystemCompositionRunsEveryShippedBlock(t *testing.T) {
	fx := newSystemFixture(t)
	rec := &lifecycleRecorder{}
	rec.subscribe(t, fx.bus, agent.MessageDeliveredEvent, agent.MessageAckedEvent,
		flow.StepCompletedEvent, agent.ThreadVerifiedEvent)

	const threadID = "system-thread-1"
	const key = ledger.IdempotencyKey("system-composition-1")
	now := time.Now()
	fence := admitAndClaim(t, fx.l, "system-suite", key, "owner-1", now)

	status, _, err := fx.a.Run(context.Background(), threadID, fx.m, machine.InOut{},
		systemWait(t, fx, threadID), fx.bus, fx.hb, fx.r.ID(), nil)
	if err != nil {
		t.Fatalf("Run() unexpected error: %v", err)
	}
	if status != machine.Status("recovered") {
		t.Fatalf("Run() status = %q, want %q", status, "recovered")
	}
	// Complete takes the ledger's own terminal status, not the run's
	// final machine.Status; it rejects anything but StatusCompleted or
	// StatusFailed.
	if err := fx.l.Complete(context.Background(), "system-suite", key, "owner-1", fence, ledger.StatusCompleted, now); err != nil {
		t.Fatalf("ledger.Complete() unexpected error: %v", err)
	}

	assertSystemStepCounts(t, fx)
	assertSystemGates(t, fx)
	assertSystemLedger(t, fx, key, fence, now)
	assertSystemEvents(t, rec)
}

// assertSystemStepCounts proves the retry retried and the fallback
// ran. The review guard must have fired three times, matching the
// step's MaxAttempts of three.
func assertSystemStepCounts(t *testing.T, fx *systemFixture) {
	t.Helper()
	if got := fx.fireCounts["review"].Load(); got != 3 {
		t.Fatalf("review guard fired %d times, want 3: the RetryPolicy must retry", got)
	}
	if got := fx.fireCounts["publish"].Load(); got != 1 {
		t.Fatalf("publish guard fired %d times, want 1", got)
	}
	if got := fx.fireCounts["recover"].Load(); got != 1 {
		t.Fatalf("recover guard fired %d times, want 1: the fallback must run", got)
	}
	caught, ok := fx.caught.Load().(flow.Failure)
	if !ok {
		t.Fatal("flow.FailureFrom never yielded a Failure inside the fallback transition")
	}
	if caught.Step != "publish" {
		t.Fatalf("caught Failure.Step = %q, want %q", caught.Step, "publish")
	}
	if caught.Err == nil {
		t.Fatal("caught Failure.Err is nil, want the wrapped publish error")
	}
}

// assertSystemGates proves the approval gate ran both branches and the
// heartbeat stayed alive while the run was in flight.
func assertSystemGates(t *testing.T, fx *systemFixture) {
	t.Helper()
	// Confirm gates two steps: review and recover. A panel wave of two
	// members never reaches Confirm, and publish fails at Fire.
	if got := fx.approvals.Load(); got != 2 {
		t.Fatalf("approval notifier called %d times, want 2", got)
	}
	if got := fx.declined.Load(); got != 1 {
		t.Fatalf("declined %d tool calls, want 1: the decline branch must run", got)
	}
	if got := fx.tool.calls.Load(); got != 1 {
		t.Fatalf("review tool ran %d times, want 1: a declined call must not reach the tool", got)
	}
	if len(fx.refs) != 1 {
		t.Fatalf("stored %d tool-result refs, want 1", len(fx.refs))
	}
	if _, err := fx.store.Get(fx.refs[0]); err != nil {
		t.Fatalf("memory.Store.Get() unexpected error: %v", err)
	}
	if got := fx.aliveSeen.Load(); got != 2 {
		t.Fatalf("heartbeat was alive on %d of 2 gated steps, want 2", got)
	}
	if got := fx.hb.Dead(time.Now()); len(got) != 0 {
		t.Fatalf("hb.Dead() = %v after Run returns, want empty: Run must Forget its id", got)
	}
}

// assertSystemLedger proves the claim is real: the key completes, and
// a second Complete on the same fence is rejected.
func assertSystemLedger(
	t *testing.T, fx *systemFixture, key ledger.IdempotencyKey,
	fence ledger.FenceToken, now time.Time,
) {
	t.Helper()
	if got := ledgerStatus(t, fx.l, key); got != ledger.StatusCompleted {
		t.Fatalf("ledger.State(%q).Status = %q, want %q", key, got, ledger.StatusCompleted)
	}
	err := fx.l.Complete(context.Background(), "system-suite", key, "owner-1", fence, ledger.StatusCompleted, now)
	if !errors.Is(err, ledger.ErrNotClaimed) {
		t.Fatalf("second Complete() error = %v, want errors.Is match for ledger.ErrNotClaimed", err)
	}
}

// assertSystemEvents proves the bus saw the full ordered sequence the
// run must produce, and that no event fired for the skipped or failed
// steps.
func assertSystemEvents(t *testing.T, rec *lifecycleRecorder) {
	t.Helper()
	want := []events.Name{
		// The panel wave emits one StepCompletedEvent per member, and no
		// message pair: a wave of two or more members never reaches
		// Confirm, so it builds no envelope and beats no heartbeat.
		flow.StepCompletedEvent, flow.StepCompletedEvent,
		agent.MessageDeliveredEvent, agent.MessageAckedEvent, flow.StepCompletedEvent, // review
		agent.MessageDeliveredEvent, agent.MessageAckedEvent, flow.StepCompletedEvent, // recover
		// publish fails at Fire, so it emits nothing at all.
		agent.ThreadVerifiedEvent,
	}
	if !equalNames(rec.snapshot(), want) {
		t.Fatalf("event sequence = %v, want %v", rec.snapshot(), want)
	}
}

// TestSystemCompositionBudgetStopsTheRun proves contextbudget gates
// something real inside the composed graph. A generous limit completes
// the same run; a tight MaxBytes returns ErrOverBudget. The tight case
// still emits MessageDeliveredEvent for the failing step and never
// emits MessageAckedEvent, matching confirmStep's verified order.
func TestSystemCompositionBudgetStopsTheRun(t *testing.T) {
	t.Run("generous budget completes", func(t *testing.T) {
		fx := newSystemFixture(t)
		budget := &contextbudget.Limits{MaxBytes: 1_000_000, MaxEvents: 1_000}
		status, _, err := fx.a.Run(context.Background(), "budget-thread-ok", fx.m, machine.InOut{},
			systemWait(t, fx, "budget-thread-ok"), fx.bus, fx.hb, fx.r.ID(), budget)
		if err != nil {
			t.Fatalf("Run() unexpected error: %v", err)
		}
		if status != machine.Status("recovered") {
			t.Fatalf("Run() status = %q, want %q", status, "recovered")
		}
	})

	t.Run("tight budget returns ErrOverBudget", func(t *testing.T) {
		fx := newSystemFixture(t)
		rec := &lifecycleRecorder{}
		rec.subscribe(t, fx.bus, agent.MessageDeliveredEvent, agent.MessageAckedEvent)
		budget := &contextbudget.Limits{MaxBytes: 4, MaxEvents: 1_000}
		_, _, err := fx.a.Run(context.Background(), "budget-thread-tight", fx.m, machine.InOut{},
			systemWait(t, fx, "budget-thread-tight"), fx.bus, fx.hb, fx.r.ID(), budget)
		if !errors.Is(err, agent.ErrOverBudget) {
			t.Fatalf("Run() error = %v, want errors.Is match for agent.ErrOverBudget", err)
		}
		want := []events.Name{agent.MessageDeliveredEvent}
		if !equalNames(rec.snapshot(), want) {
			t.Fatalf("event sequence = %v, want %v: the budget check runs after Emit and before wait",
				rec.snapshot(), want)
		}
		if got := fx.tool.calls.Load(); got != 0 {
			t.Fatalf("review tool ran %d times, want 0: the budget must stop the run before wait", got)
		}
	})
}
