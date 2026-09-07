package main

import (
	"context"
	"fmt"
	"time"

	"github.com/MiviaLabs/mivia-ai-sdk/channel"
	"github.com/MiviaLabs/mivia-ai-sdk/events"
	"github.com/MiviaLabs/mivia-ai-sdk/flow"
	"github.com/MiviaLabs/mivia-ai-sdk/internal/e2e"
	"github.com/MiviaLabs/mivia-ai-sdk/ledger"
	"github.com/MiviaLabs/mivia-ai-sdk/machine"
)

// e2eSurface builds the harness pieces an end-to-end scenario composes:
// the agent under test, the recorder, the thread capture, and a fault
// notifier driven through one ask.
func e2eSurface() error {
	d, err := flow.New([]flow.Step{{ID: "only", To: "done"}}, nil)
	if err != nil {
		return fmt.Errorf("flow.New: %w", err)
	}
	agent, err := e2e.NewAgent("surface-agent", d)
	if err != nil {
		return fmt.Errorf("NewAgent: %w", err)
	}
	if agent == nil {
		return fmt.Errorf("NewAgent returned nil")
	}
	_ = e2e.NewRecorder()
	_ = e2e.NewThreadCapture()
	fn := &e2e.FaultNotifier{
		Notifier: func(_ context.Context, _ channel.Question) (channel.Answer, error) {
			return channel.Answer{}, nil
		},
	}
	if _, err := fn.Notify(context.Background(), channel.Question{}); err != nil {
		return fmt.Errorf("Notify with no fault scheduled: %w", err)
	}
	return nil
}

// ledgerSurface drives the admission ceremony paths a long-running
// dispatcher owns: admit, claim, renew, block on an unmet dependency,
// and snapshot restore into a fresh ledger.
func ledgerSurface() error {
	now := time.Unix(1_800_000_000, 0)
	actor := ledger.Actor("surface")
	bus := events.New()
	l, err := ledger.New(ledger.NewMemStore(), bus)
	if err != nil {
		return fmt.Errorf("ledger.New: %w", err)
	}
	ok, err := l.Admit(context.Background(), actor, "root", 1, "root-task", now)
	if err != nil || !ok {
		return fmt.Errorf("Admit(root): ok=%v err=%v", ok, err)
	}
	if _, err := l.Admit(context.Background(), actor, "dep", 1, "dep-task", now, "root"); err != nil {
		return fmt.Errorf("Admit(dep): %w", err)
	}
	fence, err := l.Claim(context.Background(), actor, "root", "owner-a", time.Minute, now)
	if err != nil {
		return fmt.Errorf("Claim: %w", err)
	}
	if err := l.Renew(context.Background(), actor, "root", "owner-a", fence, time.Minute, now.Add(30*time.Second)); err != nil {
		return fmt.Errorf("Renew: %w", err)
	}
	if _, _, err := l.Blocked(context.Background(), "dep"); err != nil {
		return fmt.Errorf("Blocked: %w", err)
	}
	snap, err := l.Snapshot(context.Background())
	if err != nil {
		return fmt.Errorf("Snapshot: %w", err)
	}
	fresh, err := ledger.New(ledger.NewMemStore(), bus)
	if err != nil {
		return fmt.Errorf("ledger.New fresh: %w", err)
	}
	if err := fresh.Restore(context.Background(), snap); err != nil {
		return fmt.Errorf("Restore: %w", err)
	}
	return nil
}

// machineSurface queries the transition table and emits the move event
// the way a state observer does.
func machineSurface() error {
	m, err := machine.New("start",
		machine.Transition{From: "start", To: "done", Trigger: "go"},
	)
	if err != nil {
		return fmt.Errorf("machine.New: %w", err)
	}
	if got := m.AllowedTriggers("start"); len(got) != 1 || got[0] != "go" {
		return fmt.Errorf("AllowedTriggers = %v, want [go]", got)
	}
	bus := events.New()
	_ = bus.Subscribe(machine.MoveEvent, func(_ context.Context, _ events.Event) error { return nil })
	if err := bus.Emit(context.Background(), events.Event{Name: machine.MoveEvent, Data: "start->done"}); err != nil {
		return fmt.Errorf("Emit: %w", err)
	}
	return nil
}
