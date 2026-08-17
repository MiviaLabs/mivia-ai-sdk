// Package agent_test also holds the cross-cutting heartbeat
// integration cases for Run: a full successful run leaves Dead empty,
// two concurrent runs sharing one Monitor avoid the same-id race, an
// external-sweep case where a second goroutine reacts to Dead by
// canceling ctx, and the panel-coverage-gap case that pins the
// disclosed scope limit: a panel wave never reaches a beat call.
package agent_test

import (
	"context"
	"runtime"
	"sync"
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

// TestLivenessFullRunLeavesDeadEmpty proves a full successful run
// forgets its beat id regardless of the Monitor's timeout: Dead stays
// empty after Run returns, even with a timeout of one nanosecond.
func TestLivenessFullRunLeavesDeadEmpty(t *testing.T) {
	a, _, m := oneStepFixtureWithIdentity(t)
	bus := newRunBus(t)
	hb, err := heartbeat.New(time.Nanosecond)
	if err != nil {
		t.Fatalf("heartbeat.New() unexpected error: %v", err)
	}
	_, _, err = a.Run(context.Background(), "thread-1", m, machine.InOut{}, confirmingWait, bus, hb, "")
	if err != nil {
		t.Fatalf("Run() unexpected error: %v", err)
	}
	if got := hb.Dead(time.Now()); len(got) != 0 {
		t.Fatalf("hb.Dead() = %v after Run returns, want empty", got)
	}
}

// TestLivenessTwoConcurrentThreadsShareOneMonitor proves the
// identity-plus-thread beat id avoids the same-id race: two
// goroutines call Run on the same *Agent, on two different threadID
// values, sharing one *heartbeat.Monitor. Both must succeed with no
// ErrStaleBeat-derived failure. Run under go test -race.
func TestLivenessTwoConcurrentThreadsShareOneMonitor(t *testing.T) {
	a, _, m := oneStepFixtureWithIdentity(t)
	bus := newRunBus(t)
	hb, err := heartbeat.New(time.Minute)
	if err != nil {
		t.Fatalf("heartbeat.New() unexpected error: %v", err)
	}

	var wg sync.WaitGroup
	errs := make([]error, 2)
	threads := []string{"thread-a", "thread-b"}
	for i, threadID := range threads {
		wg.Add(1)
		go func(i int, threadID string) {
			defer wg.Done()
			_, _, err := a.Run(context.Background(), threadID, m, machine.InOut{}, confirmingWait, bus, hb, "")
			errs[i] = err
		}(i, threadID)
	}
	wg.Wait()

	for i, err := range errs {
		if err != nil {
			t.Fatalf("Run() goroutine %d error = %v, want nil", i, err)
		}
	}
}

// TestLivenessExternalSweepCancelsStalledWait proves the intended
// pattern: an external caller polls Dead and reacts by canceling ctx
// to unblock a stalled wait. Run must unwind cleanly through the
// deferred Forget.
func TestLivenessExternalSweepCancelsStalledWait(t *testing.T) {
	a, id, m := oneStepFixtureWithIdentity(t)
	bus := newRunBus(t)
	hb, err := heartbeat.New(time.Nanosecond)
	if err != nil {
		t.Fatalf("heartbeat.New() unexpected error: %v", err)
	}
	wantID := id.Signer() + ":thread-1"

	ctx, cancel := context.WithCancel(context.Background())
	blockedWait := func(ctx context.Context, msg envelope.Message) (envelope.Ack, error) {
		<-ctx.Done()
		return envelope.Ack{}, ctx.Err()
	}

	done := make(chan struct{})
	var runErr error
	go func() {
		defer close(done)
		_, _, runErr = a.Run(ctx, "thread-1", m, machine.InOut{}, blockedWait, bus, hb, "")
	}()

	for {
		dead := hb.Dead(time.Now())
		found := false
		for _, deadID := range dead {
			if deadID == wantID {
				found = true
				break
			}
		}
		if found {
			cancel()
			break
		}
		runtime.Gosched()
	}
	<-done

	if runErr == nil {
		t.Fatal("Run() returned a nil error, want a non-nil error after ctx cancellation")
	}
	if hb.Alive(wantID, time.Now()) {
		t.Fatal("hb.Alive(id) = true after Run returns, want false: Forget must run")
	}
}

// TestLivenessPanelWaveReachesNoBeat pins the disclosed scope limit:
// a two-member panel plan with no gated step, run with hb != nil,
// never beats the panel wave's would-be id. hb.Dead stays empty, and
// hb.Alive reads false for the identity-plus-thread id a gated run
// would have used.
func TestLivenessPanelWaveReachesNoBeat(t *testing.T) {
	id, err := identity.New()
	if err != nil {
		t.Fatalf("identity.New() unexpected error: %v", err)
	}
	plan, err := flow.New([]flow.Step{
		{ID: "p1", To: "panel-done"},
		{ID: "p2", To: "panel-done"},
	}, []flow.Panel{{"p1", "p2"}})
	if err != nil {
		t.Fatalf("flow.New() unexpected error: %v", err)
	}
	card := discovery.Card{Name: "Panelist", Capabilities: []string{"run"}}
	a, err := agent.New(id, card, plan)
	if err != nil {
		t.Fatalf("agent.New() unexpected error: %v", err)
	}
	m, err := machine.New("start", machine.Transition{From: "start", To: "panel-done", Trigger: "go-panel"})
	if err != nil {
		t.Fatalf("machine.New() unexpected error: %v", err)
	}
	bus := newRunBus(t)
	hb, err := heartbeat.New(time.Minute)
	if err != nil {
		t.Fatalf("heartbeat.New() unexpected error: %v", err)
	}
	wantID := id.Signer() + ":thread-1"

	status, _, err := a.Run(context.Background(), "thread-1", m, machine.InOut{}, confirmingWait, bus, hb, "")
	if err != nil {
		t.Fatalf("Run() unexpected error: %v", err)
	}
	if status != machine.Status("panel-done") {
		t.Fatalf("Run() status = %q, want %q", status, "panel-done")
	}

	if got := hb.Dead(time.Now()); len(got) != 0 {
		t.Fatalf("hb.Dead() = %v, want empty: a panel wave must never beat", got)
	}
	if hb.Alive(wantID, time.Now()) {
		t.Fatal("hb.Alive(id) = true, want false: a panel wave must never beat")
	}
}
