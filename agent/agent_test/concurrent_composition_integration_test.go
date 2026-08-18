// Package agent_test also holds the concurrent composition test.
// Eight agent.Run calls share one memory.Store, one tools.Registry,
// one ledger.Ledger, one events.Bus, and one heartbeat.Monitor. It
// proves the shared blocks stay correct under real contention.
// See docs/plans/agents/PHASES.md's phase 47 paragraph.
package agent_test

import (
	"context"
	"fmt"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/MiviaLabs/mivia-ai-sdk/agent"
	"github.com/MiviaLabs/mivia-ai-sdk/discovery"
	"github.com/MiviaLabs/mivia-ai-sdk/envelope"
	"github.com/MiviaLabs/mivia-ai-sdk/events"
	"github.com/MiviaLabs/mivia-ai-sdk/flow"
	"github.com/MiviaLabs/mivia-ai-sdk/heartbeat"
	"github.com/MiviaLabs/mivia-ai-sdk/identity"
	"github.com/MiviaLabs/mivia-ai-sdk/ledger"
	"github.com/MiviaLabs/mivia-ai-sdk/machine"
	"github.com/MiviaLabs/mivia-ai-sdk/memory"
	"github.com/MiviaLabs/mivia-ai-sdk/scheduler"
	"github.com/MiviaLabs/mivia-ai-sdk/tools"
	"github.com/MiviaLabs/mivia-ai-sdk/trigger"
)

// concurrentRuns is the number of goroutines that share every block.
const concurrentRuns = 8

// concurrentSteps is the gated step count of one run's plan. Both
// steps reach Confirm, so each run drives two approvals and two
// tool calls.
const concurrentSteps = 2

// sharedBlocks holds the one instance of every stateful block the
// concurrent runs contend over.
type sharedBlocks struct {
	store    *memory.Store
	registry *tools.Registry
	scope    *tools.Scope
	tool     *reviewTool
	l        *ledger.Ledger
	bus      *events.Bus
	hb       *heartbeat.Monitor
	approves atomic.Int64
	counts   map[events.Name]*atomic.Int64
}

// newSharedBlocks builds one instance of each shared block, plus an
// atomic counter per emitted event name.
func newSharedBlocks(t testing.TB) *sharedBlocks {
	t.Helper()
	sb := &sharedBlocks{
		bus:      newSystemBus(t),
		registry: tools.New(),
		tool:     &reviewTool{},
		counts:   make(map[events.Name]*atomic.Int64),
	}
	if err := sb.registry.Add(sb.tool); err != nil {
		t.Fatalf("tools.Registry.Add() unexpected error: %v", err)
	}
	sb.scope = newReviewScope(approvalNotifier(&sb.approves))
	var err error
	if sb.store, err = memory.New(1 << 20); err != nil {
		t.Fatalf("memory.New() unexpected error: %v", err)
	}
	if sb.hb, err = heartbeat.New(time.Minute); err != nil {
		t.Fatalf("heartbeat.New() unexpected error: %v", err)
	}
	sb.l = newSystemLedger(t, sb.bus)
	for _, name := range []events.Name{
		agent.MessageDeliveredEvent, agent.MessageAckedEvent,
		agent.ThreadVerifiedEvent, flow.StepCompletedEvent,
	} {
		counter := &atomic.Int64{}
		sb.counts[name] = counter
		subscribeCounter(t, sb.bus, name, counter)
	}
	return sb
}

// concurrentAgent builds one run's own agent and machine. Each
// goroutine owns its identity, card, and plan; only the blocks in
// sharedBlocks are shared.
func concurrentAgent(t testing.TB, index int) (*agent.Agent, *machine.Definition) {
	t.Helper()
	id, err := identity.New()
	if err != nil {
		t.Fatalf("identity.New() unexpected error: %v", err)
	}
	plan, err := flow.New([]flow.Step{
		{ID: "draft", To: "drafted", Payload: fmt.Sprintf("draft for run %d", index)},
		{ID: "submit", Needs: []string{"draft"}, To: "submitted", Payload: fmt.Sprintf("submit for run %d", index)},
	}, nil)
	if err != nil {
		t.Fatalf("flow.New() unexpected error: %v", err)
	}
	card := discovery.Card{Name: fmt.Sprintf("Worker %d", index), Capabilities: []string{"draft"}}
	a, err := agent.New(id, card, plan)
	if err != nil {
		t.Fatalf("agent.New() unexpected error: %v", err)
	}
	m, err := machine.New("start",
		machine.Transition{From: "start", To: "drafted", Trigger: "go-draft"},
		machine.Transition{From: "drafted", To: "submitted", Trigger: "go-submit"},
	)
	if err != nil {
		t.Fatalf("machine.New() unexpected error: %v", err)
	}
	return a, m
}

// sharedWait builds one run's AckWait. It runs the shared registry's
// approval-gated tool, puts the result into the shared store, and
// records the ref so the caller can read it back byte for byte.
func sharedWait(sb *sharedBlocks, refs *sync.Map, index int) agent.AckWait {
	return func(ctx context.Context, msg envelope.Message) (envelope.Ack, error) {
		out, err := sb.registry.RunScoped(ctx, reviewToolName, tools.InOut{Value: msg.Payload}, sb.scope)
		if err != nil {
			return envelope.Ack{}, err
		}
		result, ok := out.Value.(string)
		if !ok {
			return envelope.Ack{}, fmt.Errorf("review tool result is %T, want string", out.Value)
		}
		ref, err := sb.store.Put([]byte(result))
		if err != nil {
			return envelope.Ack{}, err
		}
		refs.Store(fmt.Sprintf("%d/%s", index, msg.ID), [2]string{ref, result})
		ack, err := envelope.NewAck(msg, "receiver", "restating "+msg.ID)
		if err != nil {
			return envelope.Ack{}, err
		}
		return ack.Confirm(), nil
	}
}

// TestConcurrentCompositionSharesEveryStatefulBlock runs eight
// agent.Run calls at once over one instance of every stateful block.
// Run it under go test -race.
func TestConcurrentCompositionSharesEveryStatefulBlock(t *testing.T) {
	sb := newSharedBlocks(t)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	var refs sync.Map
	var wg sync.WaitGroup
	errs := make([]error, concurrentRuns)
	statuses := make([]machine.Status, concurrentRuns)
	for i := 0; i < concurrentRuns; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			a, m := concurrentAgent(t, i)
			key := ledger.IdempotencyKey(fmt.Sprintf("concurrent-run-%d", i))
			owner := ledger.OwnerID(fmt.Sprintf("owner-%d", i))
			now := time.Now()
			fence := admitAndClaim(t, sb.l, "concurrent-suite", key, owner, now)
			threadID := fmt.Sprintf("concurrent-thread-%d", i)
			status, _, err := a.Run(ctx, threadID, m, machine.InOut{},
				sharedWait(sb, &refs, i), sb.bus, sb.hb, "", nil)
			statuses[i], errs[i] = status, err
			outcome := ledger.StatusCompleted
			if err != nil {
				outcome = ledger.StatusFailed
			}
			if cerr := sb.l.Complete(ctx, "concurrent-suite", key, owner, fence, outcome, now); cerr != nil {
				errs[i] = cerr
			}
		}(i)
	}
	wg.Wait()

	for i := range errs {
		if errs[i] != nil {
			t.Fatalf("run %d returned an unexpected error: %v", i, errs[i])
		}
		if statuses[i] != machine.Status("submitted") {
			t.Fatalf("run %d status = %q, want %q", i, statuses[i], "submitted")
		}
		key := ledger.IdempotencyKey(fmt.Sprintf("concurrent-run-%d", i))
		if got := ledgerStatus(t, sb.l, key); got != ledger.StatusCompleted {
			t.Fatalf("ledger.State(%q).Status = %q, want %q", key, got, ledger.StatusCompleted)
		}
	}
	assertSharedStoreIntact(t, sb, &refs)
	assertSharedCounters(t, sb)
}

// assertSharedStoreIntact reads every ref back from the shared store
// and proves no blob was corrupted by a concurrent Put.
func assertSharedStoreIntact(t *testing.T, sb *sharedBlocks, refs *sync.Map) {
	t.Helper()
	seen := 0
	refs.Range(func(k, v any) bool {
		seen++
		pair := v.([2]string)
		got, err := sb.store.Get(pair[0])
		if err != nil {
			t.Fatalf("memory.Store.Get(%q) unexpected error: %v", pair[0], err)
		}
		if string(got) != pair[1] {
			t.Fatalf("blob for %v = %q, want %q", k, got, pair[1])
		}
		return true
	})
	if want := concurrentRuns * concurrentSteps; seen != want {
		t.Fatalf("stored %d blobs, want %d", seen, want)
	}
}

// assertSharedCounters proves every shared counter equals the exact
// per-run total, and that the monitor forgot every beat id.
func assertSharedCounters(t *testing.T, sb *sharedBlocks) {
	t.Helper()
	gated := int64(concurrentRuns * concurrentSteps)
	if got := sb.approves.Load(); got != gated {
		t.Fatalf("approval notifier called %d times, want %d", got, gated)
	}
	if got := sb.tool.calls.Load(); got != gated {
		t.Fatalf("review tool ran %d times, want %d", got, gated)
	}
	for _, name := range []events.Name{agent.MessageDeliveredEvent, agent.MessageAckedEvent, flow.StepCompletedEvent} {
		if got := sb.counts[name].Load(); got != gated {
			t.Fatalf("%s fired %d times, want %d", name, got, gated)
		}
	}
	if got := sb.counts[agent.ThreadVerifiedEvent].Load(); got != concurrentRuns {
		t.Fatalf("%s fired %d times, want %d", agent.ThreadVerifiedEvent, got, concurrentRuns)
	}
	if got := sb.hb.Dead(time.Now()); len(got) != 0 {
		t.Fatalf("hb.Dead() = %v after every run returns, want empty", got)
	}
}

// TestConcurrentAdmitContentionElectsOneWinner proves two goroutines
// racing Admit on one key elect exactly one winner. The loser sees
// false with no error, which is the idempotent-admission contract.
func TestConcurrentAdmitContentionElectsOneWinner(t *testing.T) {
	sb := newSharedBlocks(t)
	const key = ledger.IdempotencyKey("contended-key")
	var wins atomic.Int64
	var wg sync.WaitGroup
	now := time.Now()
	for i := 0; i < 2; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			admitted, err := sb.l.Admit(context.Background(), "contender",
				key, ledger.Sequence(1), "contended task", now)
			if err != nil {
				t.Errorf("Admit() unexpected error: %v", err)
				return
			}
			if admitted {
				wins.Add(1)
			}
		}(i)
	}
	wg.Wait()
	if got := wins.Load(); got != 1 {
		t.Fatalf("%d goroutines won Admit on one key, want exactly 1", got)
	}
}

// TestConcurrentWrappersShareOneBus proves a scheduler.Job and a
// trigger.Action wrapping agent.Run run correctly side by side over
// one shared bus and one shared ledger.
func TestConcurrentWrappersShareOneBus(t *testing.T) {
	fx := newInvokedFixture(t)
	var wg sync.WaitGroup
	errs := make([]error, 2)

	scheduled := fx.runTask(t, "wrapped-scheduled", "wrapped-scheduled-thread")
	triggered := fx.runTask(t, "wrapped-triggered", "wrapped-triggered-thread")
	reg := trigger.New()
	if err := reg.Add("wrapped", func(ctx context.Context) (bool, error) { return true, nil }, triggered); err != nil {
		t.Fatalf("trigger.Add() unexpected error: %v", err)
	}

	wg.Add(2)
	go func() {
		defer wg.Done()
		errs[0] = scheduler.Job(scheduled)(context.Background())
	}()
	go func() {
		defer wg.Done()
		errs[1] = reg.Fire(context.Background(), "wrapped")
	}()
	wg.Wait()

	for i, err := range errs {
		if err != nil {
			t.Fatalf("wrapper %d returned an unexpected error: %v", i, err)
		}
	}
	for _, key := range []ledger.IdempotencyKey{"wrapped-scheduled", "wrapped-triggered"} {
		if got := ledgerStatus(t, fx.l, key); got != ledger.StatusCompleted {
			t.Fatalf("ledger.State(%q).Status = %q, want %q", key, got, ledger.StatusCompleted)
		}
	}
	if got := fx.notified.Load(); got != 2 {
		t.Fatalf("notifier called %d times, want 2", got)
	}
}
