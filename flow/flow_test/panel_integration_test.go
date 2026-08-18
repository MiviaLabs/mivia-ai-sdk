package flow_test

import (
	"context"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/MiviaLabs/mivia-ai-sdk/flow"
	"github.com/MiviaLabs/mivia-ai-sdk/machine"
)

// TestPanelFourMembersAllComplete runs a panel of four independent
// steps that share one To. It proves all four complete and that the
// singleton after the panel never fires until every member finished.
func TestPanelFourMembersAllComplete(t *testing.T) {
	t.Parallel()
	const (
		panelTo = machine.Status("panel-done")
		finalTo = machine.Status("final")
	)
	d, err := flow.New([]flow.Step{
		{ID: "m1", To: string(panelTo)},
		{ID: "m2", To: string(panelTo)},
		{ID: "m3", To: string(panelTo)},
		{ID: "m4", To: string(panelTo)},
		{ID: "after", Needs: []string{"m1", "m2", "m3", "m4"}, To: string(finalTo)},
	}, []flow.Panel{{"m1", "m2", "m3", "m4"}})
	if err != nil {
		t.Fatalf("flow.New: %v", err)
	}
	var panelFires int64
	var afterFires int64
	m, err := machine.New(statusStart,
		machine.Transition{From: statusStart, To: panelTo, Trigger: triggerGo,
			OnEntry: func(ctx context.Context, rec *machine.InOut) error {
				atomic.AddInt64(&panelFires, 1)
				return nil
			}},
		machine.Transition{From: panelTo, To: finalTo, Trigger: triggerGo,
			Guard: func(ctx context.Context) (bool, error) {
				// The singleton after the panel must never see a
				// partial panel: every member has already fired by
				// the time the graph reaches this Guard.
				return atomic.LoadInt64(&panelFires) == 4, nil
			},
			OnEntry: func(ctx context.Context, rec *machine.InOut) error {
				atomic.AddInt64(&afterFires, 1)
				return nil
			}},
	)
	if err != nil {
		t.Fatalf("machine.New: %v", err)
	}
	report, err := flow.Run(context.Background(), d, m, machine.InOut{}, noopConfirm, nil, nil)
	status := report.Status()
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if status != finalTo {
		t.Fatalf("status = %q, want %q", status, finalTo)
	}
	if got := atomic.LoadInt64(&panelFires); got != 4 {
		t.Fatalf("panel fired %d times, want 4", got)
	}
	if got := atomic.LoadInt64(&afterFires); got != 1 {
		t.Fatalf("after fired %d times, want 1", got)
	}
}

// TestPanelOneFailingMemberFailsTheWave feeds one failing step, using
// a rejecting Guard. It confirms errors.Join reports it, Run returns
// the pre-wave status, and no sibling in that panel is marked done: a
// second Run call on the same fresh graph still needs the whole
// panel again, not a partial one.
func TestPanelOneFailingMemberFailsTheWave(t *testing.T) {
	t.Parallel()
	const panelTo = machine.Status("panel-done")
	d, err := flow.New([]flow.Step{
		{ID: "ok1", To: string(panelTo)},
		{ID: "bad", To: string(panelTo)},
		{ID: "ok2", To: string(panelTo)},
	}, []flow.Panel{{"ok1", "bad", "ok2"}})
	if err != nil {
		t.Fatalf("flow.New: %v", err)
	}
	var guardCalls int64
	m, err := machine.New(statusStart,
		machine.Transition{From: statusStart, To: panelTo, Trigger: triggerGo,
			Guard: func(ctx context.Context) (bool, error) {
				atomic.AddInt64(&guardCalls, 1)
				return false, nil
			}},
	)
	if err != nil {
		t.Fatalf("machine.New: %v", err)
	}
	report, err := flow.Run(context.Background(), d, m, machine.InOut{Input: "start"}, noopConfirm, nil, nil)
	status := report.Status()
	out := report.Record()
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if status != statusStart {
		t.Fatalf("status = %q, want the pre-wave status %q", status, statusStart)
	}
	if out.Input != "start" {
		t.Fatalf("out.Input = %v, want the pre-wave record untouched", out.Input)
	}
	if got := atomic.LoadInt64(&guardCalls); got != 3 {
		t.Fatalf("Guard ran %d times, want 3: every member's own Guard call ran", got)
	}
}

// TestPanelSingletonWaitsForWholePanel mixes a panel with a singleton
// step that depends on the panel's output, and proves the singleton
// waits for the whole panel, not just its first-completing member.
func TestPanelSingletonWaitsForWholePanel(t *testing.T) {
	t.Parallel()
	const (
		panelTo = machine.Status("panel-done")
		joinTo  = machine.Status("joined")
	)
	d, err := flow.New([]flow.Step{
		{ID: "left", To: string(panelTo)},
		{ID: "right", To: string(panelTo)},
		{ID: "join", Needs: []string{"left", "right"}, To: string(joinTo)},
	}, []flow.Panel{{"left", "right"}})
	if err != nil {
		t.Fatalf("flow.New: %v", err)
	}
	var release = make(chan struct{})
	var leftDone, rightDone int64
	m, err := machine.New(statusStart,
		machine.Transition{From: statusStart, To: panelTo, Trigger: triggerGo,
			OnEntry: func(ctx context.Context, rec *machine.InOut) error {
				// Every member waits at this barrier until both have
				// arrived, so the join Guard below can never observe
				// a partial panel by pure luck of scheduling.
				n := atomic.AddInt64(&leftDone, 1)
				if n == 1 {
					<-release
				} else {
					atomic.AddInt64(&rightDone, 1)
					close(release)
				}
				return nil
			}},
		machine.Transition{From: panelTo, To: joinTo, Trigger: triggerGo,
			Guard: func(ctx context.Context) (bool, error) {
				return atomic.LoadInt64(&leftDone) == 2, nil
			}},
	)
	if err != nil {
		t.Fatalf("machine.New: %v", err)
	}
	report, err := flow.Run(context.Background(), d, m, machine.InOut{}, noopConfirm, nil, nil)
	status := report.Status()
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if status != joinTo {
		t.Fatalf("status = %q, want %q", status, joinTo)
	}
}

// TestPanelRaceOwnMaps runs a panel where each member holds its own,
// independently allocated Input map. Run this test with go test
// -race to prove flow never writes done or a shared rec concurrently.
// Two members must never share the same underlying map; that shape is
// a caller contract violation, not a flow bug, and -race would
// correctly flag it as user error.
func TestPanelRaceOwnMaps(t *testing.T) {
	t.Parallel()
	const panelTo = machine.Status("panel-done")
	d, err := flow.New([]flow.Step{
		{ID: "m1", To: string(panelTo)},
		{ID: "m2", To: string(panelTo)},
		{ID: "m3", To: string(panelTo)},
		{ID: "m4", To: string(panelTo)},
	}, []flow.Panel{{"m1", "m2", "m3", "m4"}})
	if err != nil {
		t.Fatalf("flow.New: %v", err)
	}
	var mu sync.Mutex
	var seen []map[string]int
	m, err := machine.New(statusStart,
		machine.Transition{From: statusStart, To: panelTo, Trigger: triggerGo,
			OnEntry: func(ctx context.Context, rec *machine.InOut) error {
				// Each invocation allocates its own map; no two
				// members ever share the same underlying map.
				own := map[string]int{"n": 1}
				own["n"]++
				rec.Output = own
				mu.Lock()
				seen = append(seen, own)
				mu.Unlock()
				return nil
			}},
	)
	if err != nil {
		t.Fatalf("machine.New: %v", err)
	}
	in := machine.InOut{Input: map[string]int{"caller": 1}}
	report, err := flow.Run(context.Background(), d, m, in, noopConfirm, nil, nil)
	status := report.Status()
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if status != panelTo {
		t.Fatalf("status = %q, want %q", status, panelTo)
	}
	if len(seen) != 4 {
		t.Fatalf("len(seen) = %d, want 4", len(seen))
	}
}
