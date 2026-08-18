package flow_test

// Red step: before phase 6, Run scanned steps with nextReady, one
// step at a time, and never spawned a goroutine. A panel of steps
// with a shared To ran as separate singleton steps, never together,
// so a cross-panel scheduling deadlock could not surface as the
// stall error and a wave-level failure could not join member errors.
// nextReadyGroup, runWave, and markDone landed in flow/runner.go; the
// cases below then passed.
//
// The New-validation phase 6 cases (panel homogeneity, the duplicate
// check, and panel independence) live in phase06_tdd_new_test.go, to
// keep each test file at or below the 500-line structure cap.

import (
	"context"
	"errors"
	"math/rand"
	"runtime"
	"strings"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/MiviaLabs/mivia-ai-sdk/flow"
	"github.com/MiviaLabs/mivia-ai-sdk/machine"
)

// TestRunCrossPanelDeadlockStalls proves Run returns the pinned stall
// error on a cross-panel scheduling deadlock: a member of panel A
// needs a member of panel B, and a member of panel B needs a member
// of panel A. Neither panel repeats a member and neither panel's own
// Needs closure reaches a fellow member, so New accepts the
// definition. Run still stalls at the first wave, because neither
// panel ever becomes fully ready.
func TestRunCrossPanelDeadlockStalls(t *testing.T) {
	t.Parallel()
	d, err := flow.New([]flow.Step{
		{ID: "a1", Needs: []string{"b1"}, To: "x"},
		{ID: "a2", To: "x"},
		{ID: "b1", To: "y"},
		{ID: "b2", Needs: []string{"a2"}, To: "y"},
	}, []flow.Panel{
		{"a1", "a2"},
		{"b1", "b2"},
	})
	if err != nil {
		t.Fatalf("unexpected error from New: %v", err)
	}
	m, err := machine.New(statusStart,
		machine.Transition{From: statusStart, To: machine.Status("x"), Trigger: triggerGo},
		machine.Transition{From: statusStart, To: machine.Status("y"), Trigger: machine.Trigger("go2")},
	)
	if err != nil {
		t.Fatalf("machine.New: %v", err)
	}
	report, err := flow.Run(context.Background(), d, m, machine.InOut{}, noopConfirm, nil)
	if err == nil {
		t.Fatal("expected the stall error, got nil")
	}
	if err.Error() != "flow: no ready step; graph stalled" {
		t.Fatalf("error = %q, want %q", err.Error(), "flow: no ready step; graph stalled")
	}
	if outcomes := report.Outcomes(); len(outcomes) != 0 {
		t.Fatalf("Outcomes() = %v, want empty: both panels stall before any member runs", outcomes)
	}
}

// TestRunSingletonForStepInNoPanel proves a step named in no panel
// runs alone, exactly as it did before panels existed: Run reaches
// the final status and never blocks it behind an unrelated panel.
func TestRunSingletonForStepInNoPanel(t *testing.T) {
	t.Parallel()
	const done = machine.Status("done")
	d, err := flow.New([]flow.Step{
		{ID: "solo", To: string(done)},
	}, nil)
	if err != nil {
		t.Fatalf("flow.New: %v", err)
	}
	m, err := machine.New(statusStart,
		machine.Transition{From: statusStart, To: done, Trigger: triggerGo},
	)
	if err != nil {
		t.Fatalf("machine.New: %v", err)
	}
	report, err := flow.Run(context.Background(), d, m, machine.InOut{}, noopConfirm, nil)
	status := report.Status()
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if status != done {
		t.Fatalf("status = %q, want %q", status, done)
	}
}

// TestRunSkipsPartiallyReadyPanel proves the scan skips a
// partially-ready panel and returns another ready step instead of
// blocking on it. It also proves the panel returns as a whole group
// once every member is ready.
func TestRunSkipsPartiallyReadyPanel(t *testing.T) {
	t.Parallel()
	const (
		root    = machine.Status("root")
		blocked = machine.Status("blocked")
		panelTo = machine.Status("panel")
	)
	var order []string
	var mu sync.Mutex
	record := func(id string) {
		mu.Lock()
		defer mu.Unlock()
		order = append(order, id)
	}
	// root has no Needs. p1 and blocker both need root; p2 needs
	// blocker. Panel A = {p1, p2}. p1 is ready right after root, but
	// panel A is not fully ready until blocker finishes, so the scan
	// must skip p1's panel and fire blocker instead.
	d, err := flow.New([]flow.Step{
		{ID: "root", To: string(root)},
		{ID: "p1", Needs: []string{"root"}, To: string(panelTo)},
		{ID: "blocker", Needs: []string{"root"}, To: string(blocked)},
		{ID: "p2", Needs: []string{"blocker"}, To: string(panelTo)},
	}, []flow.Panel{{"p1", "p2"}})
	if err != nil {
		t.Fatalf("flow.New: %v", err)
	}
	recordEntry := func(ctx context.Context, rec *machine.InOut) error {
		return nil
	}
	var panelFires int64
	m, err := machine.New(statusStart,
		machine.Transition{From: statusStart, To: root, Trigger: triggerGo,
			OnEntry: recordEntry},
		machine.Transition{From: root, To: blocked, Trigger: triggerGo,
			OnEntry: recordEntry},
		machine.Transition{From: blocked, To: panelTo, Trigger: triggerGo,
			OnEntry: func(ctx context.Context, rec *machine.InOut) error {
				atomic.AddInt64(&panelFires, 1)
				return nil
			}},
	)
	if err != nil {
		t.Fatalf("machine.New: %v", err)
	}
	confirm := func(ctx context.Context, step flow.Step) error {
		record(step.ID)
		return nil
	}
	report, err := flow.Run(context.Background(), d, m, machine.InOut{}, confirm, nil)
	status := report.Status()
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if status != panelTo {
		t.Fatalf("status = %q, want %q", status, panelTo)
	}
	// Only root and blocker call confirm as singletons; the panel
	// wave does not call confirm in this phase. Both must have run,
	// blocker after root, before the walk could finish.
	want := []string{"root", "blocker"}
	if len(order) != len(want) {
		t.Fatalf("order = %v, want %v", order, want)
	}
	for i := range want {
		if order[i] != want[i] {
			t.Fatalf("order = %v, want %v", order, want)
		}
	}
	// The panel returns as a whole group once every member is ready:
	// both p1 and p2 fire the shared row, in the same wave.
	if got := atomic.LoadInt64(&panelFires); got != 2 {
		t.Fatalf("panel fired %d times, want 2 (the whole panel, once ready)", got)
	}
}

// TestRunWaveJoinedErrorPreservesState proves a wave with a rejecting
// Guard returns a joined error and leaves cur and rec at their
// pre-wave values.
func TestRunWaveJoinedErrorPreservesState(t *testing.T) {
	t.Parallel()
	const target = machine.Status("panel-done")
	d, err := flow.New([]flow.Step{
		{ID: "a", To: string(target)},
		{ID: "b", To: string(target)},
	}, []flow.Panel{{"a", "b"}})
	if err != nil {
		t.Fatalf("flow.New: %v", err)
	}
	m, err := machine.New(statusStart,
		machine.Transition{From: statusStart, To: target, Trigger: triggerGo,
			Guard: func(ctx context.Context) (bool, error) { return false, nil },
		},
	)
	if err != nil {
		t.Fatalf("machine.New: %v", err)
	}
	in := machine.InOut{Input: "untouched"}
	report, err := flow.Run(context.Background(), d, m, in, noopConfirm, nil)
	status := report.Status()
	out := report.Record()
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if status != statusStart {
		t.Fatalf("status = %q, want the pre-wave status %q", status, statusStart)
	}
	if out.Input != "untouched" {
		t.Fatalf("out.Input = %v, want the pre-wave record untouched", out.Input)
	}
}

// TestRunWaveJoinedErrorNoSiblingMarkedDone proves that when a wave
// fails, no member of that panel is marked done: a fresh Run on the
// same graph still attempts the whole panel again, not a partial one.
func TestRunWaveJoinedErrorNoSiblingMarkedDone(t *testing.T) {
	t.Parallel()
	const target = machine.Status("panel-done")
	d, err := flow.New([]flow.Step{
		{ID: "a", To: string(target)},
		{ID: "b", To: string(target)},
	}, []flow.Panel{{"a", "b"}})
	if err != nil {
		t.Fatalf("flow.New: %v", err)
	}
	var attempts int64
	m, err := machine.New(statusStart,
		machine.Transition{From: statusStart, To: target, Trigger: triggerGo,
			Guard: func(ctx context.Context) (bool, error) {
				atomic.AddInt64(&attempts, 1)
				return false, nil
			},
		},
	)
	if err != nil {
		t.Fatalf("machine.New: %v", err)
	}
	_, err = flow.Run(context.Background(), d, m, machine.InOut{}, noopConfirm, nil)
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if got := atomic.LoadInt64(&attempts); got != 2 {
		t.Fatalf("Guard ran %d times, want 2 (one per member, neither marked done ahead of the other)", got)
	}
}

// TestRunWaveAmbiguousRowFailsBeforeAnyGoroutine proves runWave
// returns a single, non-joined error, wrapped as "flow: panel: %w",
// when pickTransition fails for the shared row before any goroutine
// spawns. No member's Guard runs; a rejecting Guard would prove the
// miss if Fire ran, since it would still report a Guard-rejection
// error, not the panel wrap.
func TestRunWaveAmbiguousRowFailsBeforeAnyGoroutine(t *testing.T) {
	t.Parallel()
	const noMatch = machine.Status("nowhere")
	d, err := flow.New([]flow.Step{
		{ID: "a", To: string(noMatch)},
		{ID: "b", To: string(noMatch)},
	}, []flow.Panel{{"a", "b"}})
	if err != nil {
		t.Fatalf("flow.New: %v", err)
	}
	var guardRan int64
	m, err := machine.New(statusStart,
		machine.Transition{From: statusStart, To: machine.Status("elsewhere"), Trigger: triggerGo,
			Guard: func(ctx context.Context) (bool, error) {
				atomic.AddInt64(&guardRan, 1)
				return false, nil
			},
		},
	)
	if err != nil {
		t.Fatalf("machine.New: %v", err)
	}
	_, err = flow.Run(context.Background(), d, m, machine.InOut{}, noopConfirm, nil)
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	want := `flow: panel: no transition to status "nowhere" from "start"`
	if err.Error() != want {
		t.Fatalf("error = %q, want %q", err.Error(), want)
	}
	if got := atomic.LoadInt64(&guardRan); got != 0 {
		t.Fatalf("Guard ran %d times, want 0: no member's Fire should run", got)
	}
}

// TestRunWaveTwoFailuresJoinBoth proves a wave with two failing
// members returns an error that satisfies errors.Is and wraps both
// per-step failures. The test never asserts the full joined string,
// because errors.Join's argument order follows goroutine completion
// order, not declaration order.
func TestRunWaveTwoFailuresJoinBoth(t *testing.T) {
	t.Parallel()
	const target = machine.Status("panel-done")
	d, err := flow.New([]flow.Step{
		{ID: "a", To: string(target)},
		{ID: "b", To: string(target)},
	}, []flow.Panel{{"a", "b"}})
	if err != nil {
		t.Fatalf("flow.New: %v", err)
	}
	m, err := machine.New(statusStart,
		machine.Transition{From: statusStart, To: target, Trigger: triggerGo,
			Guard: func(ctx context.Context) (bool, error) { return false, nil },
		},
	)
	if err != nil {
		t.Fatalf("machine.New: %v", err)
	}
	_, err = flow.Run(context.Background(), d, m, machine.InOut{}, noopConfirm, nil)
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !strings.Contains(err.Error(), `step "a"`) {
		t.Fatalf("error %q should wrap step \"a\"'s failure", err.Error())
	}
	if !strings.Contains(err.Error(), `step "b"`) {
		t.Fatalf("error %q should wrap step \"b\"'s failure", err.Error())
	}
	var joined interface{ Unwrap() []error }
	if !errors.As(err, &joined) {
		t.Fatal("error does not unwrap as a joined error")
	}
	if len(joined.Unwrap()) != 2 {
		t.Fatalf("joined error has %d members, want 2", len(joined.Unwrap()))
	}
}

// TestRunWaveGuardRaceUnderFourMembers proves the shared row's Guard
// runs once per member, concurrently, without a race. Run this test
// with go test -race.
func TestRunWaveGuardRaceUnderFourMembers(t *testing.T) {
	t.Parallel()
	const target = machine.Status("panel-done")
	d, err := flow.New([]flow.Step{
		{ID: "a", To: string(target)},
		{ID: "b", To: string(target)},
		{ID: "c", To: string(target)},
		{ID: "e", To: string(target)},
	}, []flow.Panel{{"a", "b", "c", "e"}})
	if err != nil {
		t.Fatalf("flow.New: %v", err)
	}
	var count atomic.Int64
	m, err := machine.New(statusStart,
		machine.Transition{From: statusStart, To: target, Trigger: triggerGo,
			Guard: func(ctx context.Context) (bool, error) {
				count.Add(1)
				return true, nil
			},
		},
	)
	if err != nil {
		t.Fatalf("machine.New: %v", err)
	}
	report, err := flow.Run(context.Background(), d, m, machine.InOut{}, noopConfirm, nil)
	status := report.Status()
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if status != target {
		t.Fatalf("status = %q, want %q", status, target)
	}
	if got := count.Load(); got != 4 {
		t.Fatalf("Guard ran %d times, want 4", got)
	}
}

// TestRunWaveGuardRacesConcurrently proves runWave fires every panel
// member's Guard concurrently, on its own goroutine, rather than one
// at a time. It does not, and cannot, prove which member's record
// runWave forwards: the shared row's Guard cannot see which declared
// step it runs for, so machine.Fire's signature carries no step
// identity, and an arrival-order marker written from inside Guard is
// uncorrelated with a step's own declared identity. See
// TestFirstByDeclarationPicksDeclaredFirstMember in the internal
// flow package for the declaration-order selection proof.
//
// The Go runtime's scheduler favors the most recently spawned
// goroutine (the runnext slot), so a naive two-member race is not
// enough: which member reaches the Guard first is stable across
// iterations within one process, not evenly random. The test defeats
// that bias with a random jitter before each invocation reaches the
// Guard, so which member arrives first varies across iterations, then
// runs many iterations and requires the run's own outcome to take
// both possible values at least once.
func TestRunWaveGuardRacesConcurrently(t *testing.T) {
	t.Parallel()
	const target = machine.Status("panel-done")
	seenFirst := false
	seenSecond := false
	for i := 0; i < 200 && !(seenFirst && seenSecond); i++ {
		d, err := flow.New([]flow.Step{
			{ID: "a", To: string(target)},
			{ID: "b", To: string(target)},
		}, []flow.Panel{{"a", "b"}})
		if err != nil {
			t.Fatalf("flow.New: %v", err)
		}
		var arrived int64
		release := make(chan struct{})
		m, err := machine.New(statusStart,
			machine.Transition{From: statusStart, To: target, Trigger: triggerGo,
				OnEntry: func(ctx context.Context, rec *machine.InOut) error {
					// A per-invocation random number of scheduler
					// yields, not a shared one: each goroutine's own
					// delay must differ, or the Go scheduler's runnext
					// bias would still pick the same winner every
					// iteration. runtime.Gosched, not time.Sleep,
					// keeps the synchronization deterministic in
					// wall-clock terms; only the yield count is random.
					for n := rand.Intn(50); n > 0; n-- {
						runtime.Gosched()
					}
					if atomic.AddInt64(&arrived, 1) == 1 {
						<-release
						rec.Output = "first-arrived"
						return nil
					}
					rec.Output = "second-arrived"
					close(release)
					return nil
				}},
		)
		if err != nil {
			t.Fatalf("machine.New: %v", err)
		}
		report, err := flow.Run(context.Background(), d, m, machine.InOut{}, noopConfirm, nil)
		out := report.Record()
		if err != nil {
			t.Fatalf("Run: %v", err)
		}
		switch out.Output {
		case "first-arrived":
			seenFirst = true
		case "second-arrived":
			seenSecond = true
		default:
			t.Fatalf("out.Output = %v, want one of the two guard-arrival values", out.Output)
		}
	}
	if !seenFirst || !seenSecond {
		t.Fatalf(
			"seenFirst=%v seenSecond=%v after 200 iterations; the forwarded"+
				" value should vary with which member wins the Guard race,"+
				" not always pick the same channel-arrival winner",
			seenFirst, seenSecond,
		)
	}
}
