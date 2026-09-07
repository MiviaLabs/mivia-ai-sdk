package main

import (
	"context"
	"fmt"

	"github.com/MiviaLabs/mivia-ai-sdk/events"
	"github.com/MiviaLabs/mivia-ai-sdk/flow"
	"github.com/MiviaLabs/mivia-ai-sdk/machine"
)

// flowSurface runs a two-step plan, pauses it at the checkpoint hook,
// resumes it, and reads the report; it also probes the context
// accessors, the plan roots, and the heartbeat event kind.
func flowSurface() error {
	d, err := flow.New([]flow.Step{
		{ID: "first", To: "firstDone", Payload: "do it"},
		{ID: "second", To: "secondDone", Needs: []string{"first"}},
	}, nil)
	if err != nil {
		return fmt.Errorf("flow.New: %w", err)
	}
	m, err := machine.New("start",
		machine.Transition{From: "start", To: "firstDone", Trigger: "go"},
		machine.Transition{From: "firstDone", To: "secondDone", Trigger: "go"},
	)
	if err != nil {
		return fmt.Errorf("machine.New: %w", err)
	}
	if roots := d.Roots(); len(roots) != 1 || roots[0] != "first" {
		return fmt.Errorf("Roots = %v, want [first]", roots)
	}
	confirm := func(_ context.Context, _ flow.Step) error { return nil }
	ctx, cancel := context.WithCancel(context.Background())
	checkpoints := 0
	var cp flow.Checkpoint
	onCheckpoint := func(c flow.Checkpoint) { cp = c; checkpoints++; cancel() }
	if _, err := flow.Run(ctx, d, m, machine.InOut{}, confirm, nil, onCheckpoint); err == nil {
		return fmt.Errorf("paused Run returned nil error")
	}
	if checkpoints == 0 {
		return fmt.Errorf("Run paused without emitting a checkpoint")
	}
	report, err := flow.Resume(context.Background(), d, m, cp, confirm, nil, nil)
	if err != nil {
		return fmt.Errorf("Resume: %w", err)
	}
	outcomes := report.Outcomes()
	if len(outcomes) != 2 {
		return fmt.Errorf("Outcomes = %v, want two entries", outcomes)
	}
	return nil
}

// ctxProbeSurface reads the context accessors the way an inspecting
// tool wrapper does: absent contexts return the zero value and false.
func ctxProbeSurface() error {
	if _, ok := flow.FailureFrom(context.Background()); ok {
		return fmt.Errorf("FailureFrom found a failure on a bare context")
	}
	if _, ok := flow.LoopStateFrom(context.Background()); ok {
		return fmt.Errorf("LoopStateFrom found loop state on a bare context")
	}
	return nil
}

// heartbeatSurface subscribes to the missed-heartbeat event kind and
// emits one beat miss, the way a liveness monitor's owner does.
func heartbeatSurface() error {
	bus := events.New()
	got := make(chan struct{}, 1)
	err := bus.Subscribe(flow.MissedEvent, func(_ context.Context, _ events.Event) error {
		got <- struct{}{}
		return nil
	})
	if err != nil {
		return fmt.Errorf("Subscribe: %w", err)
	}
	if err := bus.Emit(context.Background(), events.Event{Name: flow.MissedEvent, Data: "beat 7"}); err != nil {
		return fmt.Errorf("Emit: %w", err)
	}
	select {
	case <-got:
		return nil
	default:
		return fmt.Errorf("subscriber never observed the missed beat")
	}
}
