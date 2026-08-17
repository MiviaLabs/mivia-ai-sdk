// Package agent_test also holds the red-green unit cases for Run's
// optional heartbeat parameter: a nil hb is fully inert, a non-nil hb
// beats before wait and forgets its id on every return path, one id
// serves a whole run, and a one-nanosecond-timeout Monitor proves
// staleness deterministically with no time.Sleep.
package agent_test

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/MiviaLabs/mivia-ai-sdk/agent"
	"github.com/MiviaLabs/mivia-ai-sdk/discovery"
	"github.com/MiviaLabs/mivia-ai-sdk/envelope"
	"github.com/MiviaLabs/mivia-ai-sdk/flow"
	"github.com/MiviaLabs/mivia-ai-sdk/heartbeat"
	"github.com/MiviaLabs/mivia-ai-sdk/identity"
	"github.com/MiviaLabs/mivia-ai-sdk/machine"
)

// oneStepFixtureWithIdentity builds the same one-step, no-panel plan
// as oneStepFixture, and also returns the *identity.Identity behind
// the returned Agent, so a test can compute the beat id
// identity.Signer()+":"+threadID.
func oneStepFixtureWithIdentity(t *testing.T) (*agent.Agent, *identity.Identity, *machine.Definition) {
	t.Helper()
	id, err := identity.New()
	if err != nil {
		t.Fatalf("identity.New() unexpected error: %v", err)
	}
	plan, err := flow.New([]flow.Step{
		{ID: "step-a", To: "done", Payload: "do the thing"},
	}, nil)
	if err != nil {
		t.Fatalf("flow.New() unexpected error: %v", err)
	}
	card := discovery.Card{Name: "Runner", Capabilities: []string{"run"}}
	a, err := agent.New(id, card, plan)
	if err != nil {
		t.Fatalf("agent.New() unexpected error: %v", err)
	}
	m, err := machine.New("start", machine.Transition{From: "start", To: "done", Trigger: "go"})
	if err != nil {
		t.Fatalf("machine.New() unexpected error: %v", err)
	}
	return a, id, m
}

// TestRunHeartbeatNilIsInert proves a nil hb leaves Run's outcome
// unchanged from the equivalent phase 13 case: same status, same
// record, nil error.
func TestRunHeartbeatNilIsInert(t *testing.T) {
	a, _, m := oneStepFixtureWithIdentity(t)
	bus := newRunBus(t)
	status, _, err := a.Run(context.Background(), "thread-1", m, machine.InOut{}, confirmingWait, bus, nil)
	if err != nil {
		t.Fatalf("Run() unexpected error: %v", err)
	}
	if status != machine.Status("done") {
		t.Fatalf("Run() status = %q, want %q", status, "done")
	}
}

// TestRunHeartbeatBeatsBeforeWait proves a beat lands before wait
// runs: the AckWait itself checks hb.Alive for the run's id.
func TestRunHeartbeatBeatsBeforeWait(t *testing.T) {
	a, id, m := oneStepFixtureWithIdentity(t)
	bus := newRunBus(t)
	hb, err := heartbeat.New(time.Minute)
	if err != nil {
		t.Fatalf("heartbeat.New() unexpected error: %v", err)
	}
	wantID := id.Signer() + ":thread-1"
	seenAlive := false
	wait := func(ctx context.Context, msg envelope.Message) (envelope.Ack, error) {
		seenAlive = hb.Alive(wantID, time.Now())
		return confirmingWait(ctx, msg)
	}
	_, _, err = a.Run(context.Background(), "thread-1", m, machine.InOut{}, wait, bus, hb)
	if err != nil {
		t.Fatalf("Run() unexpected error: %v", err)
	}
	if !seenAlive {
		t.Fatal("hb.Alive(id) = false inside wait, want true: the beat must land before wait runs")
	}
}

// TestRunHeartbeatForgetsOnSuccess proves the deferred Forget runs
// after Run returns on a successful, confirmed step.
func TestRunHeartbeatForgetsOnSuccess(t *testing.T) {
	a, id, m := oneStepFixtureWithIdentity(t)
	bus := newRunBus(t)
	hb, err := heartbeat.New(time.Minute)
	if err != nil {
		t.Fatalf("heartbeat.New() unexpected error: %v", err)
	}
	wantID := id.Signer() + ":thread-1"
	_, _, err = a.Run(context.Background(), "thread-1", m, machine.InOut{}, confirmingWait, bus, hb)
	if err != nil {
		t.Fatalf("Run() unexpected error: %v", err)
	}
	if hb.Alive(wantID, time.Now()) {
		t.Fatal("hb.Alive(id) = true after Run returns, want false: Forget must run")
	}
}

// TestRunHeartbeatForgetsOnEscalation proves Forget runs on the
// escalated-step error path too.
func TestRunHeartbeatForgetsOnEscalation(t *testing.T) {
	a, id, m := oneStepFixtureWithIdentity(t)
	bus := newRunBus(t)
	hb, err := heartbeat.New(time.Minute)
	if err != nil {
		t.Fatalf("heartbeat.New() unexpected error: %v", err)
	}
	wantID := id.Signer() + ":thread-1"
	escalate := func(ctx context.Context, msg envelope.Message) (envelope.Ack, error) {
		return envelope.Ack{}, fmt.Errorf("step needs a human: %w", agent.ErrEscalated)
	}
	_, _, err = a.Run(context.Background(), "thread-1", m, machine.InOut{}, escalate, bus, hb)
	if !errors.Is(err, agent.ErrEscalated) {
		t.Fatalf("Run() error = %v, want errors.Is match for ErrEscalated", err)
	}
	if hb.Alive(wantID, time.Now()) {
		t.Fatal("hb.Alive(id) = true after an escalated run, want false: Forget must run")
	}
}

// TestRunHeartbeatForgetsOnPlainWaitError proves Forget is
// unconditional on the error's shape: a plain wait error, wrapping
// nothing, still forgets the id.
func TestRunHeartbeatForgetsOnPlainWaitError(t *testing.T) {
	a, id, m := oneStepFixtureWithIdentity(t)
	bus := newRunBus(t)
	hb, err := heartbeat.New(time.Minute)
	if err != nil {
		t.Fatalf("heartbeat.New() unexpected error: %v", err)
	}
	wantID := id.Signer() + ":thread-1"
	wantErr := errors.New("wait: connection refused")
	plainErr := func(ctx context.Context, msg envelope.Message) (envelope.Ack, error) {
		return envelope.Ack{}, wantErr
	}
	_, _, err = a.Run(context.Background(), "thread-1", m, machine.InOut{}, plainErr, bus, hb)
	if !errors.Is(err, wantErr) {
		t.Fatalf("Run() error = %v, want errors.Is match for %v", err, wantErr)
	}
	if hb.Alive(wantID, time.Now()) {
		t.Fatal("hb.Alive(id) = true after a plain wait error, want false: Forget must run")
	}
}

// TestRunHeartbeatOneIDServesTwoSteps proves one id serves the whole
// run: both step's wait calls see the same id as alive.
func TestRunHeartbeatOneIDServesTwoSteps(t *testing.T) {
	id, err := identity.New()
	if err != nil {
		t.Fatalf("identity.New() unexpected error: %v", err)
	}
	plan, err := flow.New([]flow.Step{
		{ID: "a", To: "a-done", Payload: "step a payload"},
		{ID: "b", Needs: []string{"a"}, To: "b-done", Payload: "step b payload"},
	}, nil)
	if err != nil {
		t.Fatalf("flow.New() unexpected error: %v", err)
	}
	card := discovery.Card{Name: "Runner", Capabilities: []string{"run"}}
	a, err := agent.New(id, card, plan)
	if err != nil {
		t.Fatalf("agent.New() unexpected error: %v", err)
	}
	m, err := machine.New("start",
		machine.Transition{From: "start", To: "a-done", Trigger: "go-a"},
		machine.Transition{From: "a-done", To: "b-done", Trigger: "go-b"},
	)
	if err != nil {
		t.Fatalf("machine.New() unexpected error: %v", err)
	}
	bus := newRunBus(t)
	hb, err := heartbeat.New(time.Minute)
	if err != nil {
		t.Fatalf("heartbeat.New() unexpected error: %v", err)
	}
	wantID := id.Signer() + ":thread-1"
	calls := 0
	alive := []bool{}
	wait := func(ctx context.Context, msg envelope.Message) (envelope.Ack, error) {
		calls++
		alive = append(alive, hb.Alive(wantID, time.Now()))
		return confirmingWait(ctx, msg)
	}
	_, _, err = a.Run(context.Background(), "thread-1", m, machine.InOut{}, wait, bus, hb)
	if err != nil {
		t.Fatalf("Run() unexpected error: %v", err)
	}
	if calls != 2 {
		t.Fatalf("wait called %d times, want 2", calls)
	}
	for i, got := range alive {
		if !got {
			t.Fatalf("hb.Alive(id) = false on call %d, want true for both calls, using the same id", i)
		}
	}
}

// TestRunHeartbeatOneNanosecondTimeoutAges proves a Monitor with a
// one-nanosecond timeout ages the id out of Alive between two reads
// inside the same wait call, with no time.Sleep: real wall-clock
// progress between the two time.Now() reads exceeds the timeout.
func TestRunHeartbeatOneNanosecondTimeoutAges(t *testing.T) {
	a, id, m := oneStepFixtureWithIdentity(t)
	bus := newRunBus(t)
	hb, err := heartbeat.New(time.Nanosecond)
	if err != nil {
		t.Fatalf("heartbeat.New() unexpected error: %v", err)
	}
	wantID := id.Signer() + ":thread-1"
	var second bool
	wait := func(ctx context.Context, msg envelope.Message) (envelope.Ack, error) {
		_ = hb.Alive(wantID, time.Now())
		second = hb.Alive(wantID, time.Now())
		return confirmingWait(ctx, msg)
	}
	_, _, err = a.Run(context.Background(), "thread-1", m, machine.InOut{}, wait, bus, hb)
	if err != nil {
		t.Fatalf("Run() unexpected error: %v", err)
	}
	if second {
		t.Fatal("hb.Alive(id) = true on the second read, want false: a one-nanosecond timeout must have aged the beat")
	}
}
