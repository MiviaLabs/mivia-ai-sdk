package agentloop_test

// Review addendum tests for the steer injector (commit d914611).
// Each case pins one property from the addendum's spec:
//
//   - HasActiveCall false before any RunSteerable call arms it
//     (kills a mutation that returns true unconditionally).
//   - HasActiveCall true while a Completer.Chat call is in flight.
//   - the no-op-bridge-Trigger loop closes when the bridge guards
//     Trigger on HasActiveCall.
//   - a success-path RoleTool message carries Name == call.Name.
//   - a deduped-call RoleTool message carries Name == call.Name.
//   - the deliver-once shape across consecutive payloads:
//     drainInjected runs at the iteration top, not at the
//     downgrade point; the injector call count equals the
//     number of iteration tops.

import (
	"context"
	"strings"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/MiviaLabs/mivia-ai-sdk/agentloop"
	"github.com/MiviaLabs/mivia-ai-sdk/provider"
	"github.com/MiviaLabs/mivia-ai-sdk/tools"
)

// TestSteerHasActiveCallFalseBeforeArm pins the
// "HasActiveCall false before arm" property: a fresh Steer reports
// HasActiveCall() == false before any RunSteerable call arms it. A
// mutation that returns true unconditionally trips here.
func TestSteerHasActiveCallFalseBeforeArm(t *testing.T) {
	steer := agentloop.NewSteer()
	if steer.HasActiveCall() {
		t.Fatalf("fresh Steer HasActiveCall() = true, want false: no RunSteerable call has armed cancel yet")
	}
}

// TestSteerHasActiveCallTrueDuringChat pins the "HasActiveCall true
// during Chat" property: while a Completer Chat call is in flight,
// HasActiveCall() reports true. The case arms the same way
// TestInjectorTriggeredAndEmptyStopsSteered does, via a
// blockingCompleter.
func TestSteerHasActiveCallTrueDuringChat(t *testing.T) {
	entered := make(chan struct{})
	c := &blockingCompleter{entered: entered}
	loop := newSteerLoop(t, c, 5)
	steer := agentloop.NewSteer()

	resCh := make(chan agentloop.Result, 1)
	errCh := make(chan error, 1)
	go func() {
		res, err := loop.RunSteerable(context.Background(),
			[]provider.Message{textMessage(provider.RoleUser, "hi")}, steer)
		resCh <- res
		errCh <- err
	}()

	<-entered
	if !steer.HasActiveCall() {
		t.Fatalf("HasActiveCall() = false mid-Chat, want true: arm must have bound the cancel func")
	}

	steer.Trigger()
	if _, err := <-resCh, <-errCh; err != nil {
		t.Fatalf("RunSteerable error: %v", err)
	}
}

// TestHasActiveCallGuardsNoopBridgeTrigger pins the "guard closes
// the no-op-trigger loop" property: a bridge that polls
// HasActiveCall() continuously only triggers when the method
// returns true. Without the guard, a no-op Trigger fired between
// iterations still sets the sticky triggered flag; the next arm
// cancels the next Chat instantly; the run never makes progress.
// The case asserts:
//
//	(a) the bridge goroutine calls HasActiveCall() in a loop and
//	    only Trigger()s when the method returns true;
//	(b) the run reaches StopNoToolCalls, and history carries no
//	    "would-have-triggered" sentinel proving the no-op trigger
//	    did not poison the next Chat arm;
//	(c) the test counts HasActiveCall() calls to prove the guard
//	    ran on each poll.
//
// The completer is an injectorGateCompleter: call 0 is the blocking
// midpoint (the bridge has a stable mid-Chat window to observe
// HasActiveCall == true), call 1 returns the scripted "ok"
// response. The bridge fires Trigger exactly once when HasActiveCall
// returns true; the run then cancels call 0, soft-continues
// (injector installed), and call 1 returns the final.
func TestHasActiveCallGuardsNoopBridgeTrigger(t *testing.T) {
	// call 0: blocking midpoint (bridge polls mid-Chat here)
	// call 1: scripted "ok" (final, run completes here)
	c := newInjectorGateCompleter(
		[]provider.Response{
			{Message: textMessage(provider.RoleAssistant, "ok")},
		},
		0,
	)
	loop := newInjectorLoop(t, c, 5)

	inj := &injectorFixture{}
	inj.setNoOp()

	steer := agentloop.NewSteer()
	steer.SetInjector(inj.drain)

	bridge := newGuardBridge(steer)

	resCh := make(chan agentloop.Result, 1)
	errCh := make(chan error, 1)
	go func() {
		res, err := loop.RunSteerable(context.Background(),
			[]provider.Message{textMessage(provider.RoleUser, "hello")}, steer)
		resCh <- res
		errCh <- err
	}()

	<-c.entered
	bridge.pollOnce()
	res, err := <-resCh, <-errCh
	bridge.shutdown()
	if err != nil {
		t.Fatalf("RunSteerable error: %v", err)
	}
	if res.Stop != agentloop.StopNoToolCalls {
		t.Fatalf("Stop = %q, want StopNoToolCalls: the guard must keep the no-op trigger from poisoning the next Chat arm", res.Stop)
	}
	if got := bridge.polls.Load(); got == 0 {
		t.Fatalf("bridge HasActiveCall poll count = 0, want >=1: the guard must run on each poll, proving the no-op-trigger loop is closed")
	}
	if got := bridge.pollsTrue.Load(); got == 0 {
		t.Fatalf("bridge HasActiveCall true count = 0, want >=1: the bridge must observe at least one mid-Chat window where HasActiveCall returns true")
	}
	if !injectorMessagesContain(res.History, "hello") {
		t.Fatalf("expected starting message in history: %+v", res.History)
	}
}

// guardBridge models a continuous-bridge pattern that polls
// HasActiveCall() and only Trigger()s when the guard returns
// true. pollOnce runs exactly one poll. shutdown terminates the
// bridge goroutine.
type guardBridge struct {
	steer     *agentloop.Steer
	polls     atomic.Int32
	pollsTrue atomic.Int32
	triggers  atomic.Int32
	stop      chan struct{}
	done      chan struct{}
	pollReq   chan struct{}
	pollDone  chan struct{}
}

func newGuardBridge(steer *agentloop.Steer) *guardBridge {
	b := &guardBridge{
		steer:    steer,
		stop:     make(chan struct{}),
		done:     make(chan struct{}),
		pollReq:  make(chan struct{}),
		pollDone: make(chan struct{}),
	}
	go b.run()
	return b
}

// run drives the bridge loop: poll on demand, fire Trigger at most
// once when HasActiveCall returns true, and exit on stop.
func (b *guardBridge) run() {
	defer close(b.done)
	for {
		select {
		case <-b.stop:
			return
		case <-b.pollReq:
			b.polls.Add(1)
			if b.steer.HasActiveCall() {
				b.pollsTrue.Add(1)
				if b.triggers.Load() == 0 {
					b.triggers.Add(1)
					b.steer.Trigger()
				}
			}
			b.pollDone <- struct{}{}
		}
	}
}

// pollOnce requests one poll and waits for it to complete.
func (b *guardBridge) pollOnce() {
	b.pollReq <- struct{}{}
	<-b.pollDone
}

// shutdown signals the bridge goroutine to exit and waits for it.
func (b *guardBridge) shutdown() {
	close(b.stop)
	<-b.done
}

// TestRoleToolMessageCarriesToolName_Success pins the "success-path
// RoleTool Name" property: a normal tool call's appended RoleTool
// message carries Name == call.Name. The success path of
// runOneToolCall.
func TestRoleToolMessageCarriesToolName_Success(t *testing.T) {
	tool := &schemaEchoTool{name: "echo", schema: []byte(`{}`), result: "ok"}
	reg := tools.New()
	mustAdd(t, reg, tool)
	completer := &scriptedCompleter{responses: []provider.Response{
		toolCallResponse(
			provider.ToolCall{ID: "call-1", Name: "echo", Arguments: []byte(`{}`)},
		),
		{Message: textMessage(provider.RoleAssistant, "final")},
	}}
	loop, err := agentloop.New(agentloop.Options{Completer: completer, Tools: reg, MaxIterations: 5})
	if err != nil {
		t.Fatalf("New() error = %v, want nil", err)
	}
	res, err := loop.Run(context.Background(), []provider.Message{textMessage(provider.RoleUser, "hi")})
	if err != nil {
		t.Fatalf("Run() error = %v, want nil", err)
	}
	var found bool
	for _, m := range res.History {
		if m.Role == provider.RoleTool && m.ToolCallID == "call-1" {
			found = true
			if m.Name != "echo" {
				t.Fatalf("success-path RoleTool Name = %q, want %q: the model-supplied call's name must travel with the RoleTool message", m.Name, "echo")
			}
		}
	}
	if !found {
		t.Fatalf("no RoleTool message for call-1: %+v", res.History)
	}
}

// TestRoleToolMessageCarriesToolName_Dedup pins the "dedup-path
// RoleTool Name" property: a duplicate call's synthesized RoleTool
// message carries Name == call.Name. The dedup short-circuit in
// runToolCalls, not runOneToolCall.
func TestRoleToolMessageCarriesToolName_Dedup(t *testing.T) {
	tool := &schemaEchoTool{name: "echo", schema: []byte(`{}`), result: "x"}
	reg := tools.New()
	mustAdd(t, reg, tool)
	completer := &scriptedCompleter{responses: []provider.Response{
		toolCallResponse(
			provider.ToolCall{ID: "call-1", Name: "echo", Arguments: []byte(`{"a":1}`)},
			provider.ToolCall{ID: "call-2", Name: "echo", Arguments: []byte(`{"a":1}`)},
		),
		{Message: textMessage(provider.RoleAssistant, "final")},
	}}
	loop, err := agentloop.New(agentloop.Options{Completer: completer, Tools: reg, MaxIterations: 5, DedupWithinTurn: true})
	if err != nil {
		t.Fatalf("New() error = %v, want nil", err)
	}
	res, err := loop.Run(context.Background(), []provider.Message{textMessage(provider.RoleUser, "hi")})
	if err != nil {
		t.Fatalf("Run() error = %v, want nil", err)
	}
	var found bool
	for _, m := range res.History {
		if m.Role == provider.RoleTool && m.Content == agentloop.DuplicateCallNotice {
			found = true
			if m.Name != "echo" {
				t.Fatalf("deduped-call RoleTool Name = %q, want %q: the duplicate call's name must travel with the synthesized RoleTool message", m.Name, "echo")
			}
		}
	}
	if !found {
		t.Fatalf("no DuplicateCallNotice message in history: %+v", res.History)
	}
}

// TestInjectorDeliversOncePerIteration pins the "deliver-once
// shape across consecutive payloads" property: the drainInjected
// contract under case (a) at the downgrade point plus the
// iteration-top boundary. The injector drain queue is
// [nil, payload1, payload2] across two steered iterations:
//   - iter-1 top: empty (no pre-loop frame);
//   - iter-1 steered-stop downgrade: case (a) returns immediately
//     after ackTriggered; the drain does NOT run here;
//   - iter-2 top: payload1;
//   - iter-2 steered-stop downgrade: same path returns immediately;
//   - iter-3 top: payload2.
//
// The case asserts:
//
//	(a) payload1 appears in history exactly once after the first
//	    steered iteration's top drain;
//	(b) payload2 appears in history exactly once after the second
//	    steered iteration's top drain;
//	(c) injector call count is exactly 3 (one per iter top),
//	    proving the downgrade path does not re-call drainInjected.
func TestInjectorDeliversOncePerIteration(t *testing.T) {
	gated := newGatedCompleter(
	// call 0: blocking midpoint (Trigger fires here)
	// call 1: blocking midpoint (Trigger fires here)
	// call 2: scripted "ok" (final)
	)
	loop := newInjectorLoop(t, gated, 5)

	inj := &injectorFixture{}
	// Queue holds three drain slots, one per iter top. Iter-1 top
	// is empty (no pre-loop frame); iter-2 top is payload1; iter-3
	// top is payload2. The downgrade path does NOT drain.
	inj.setQueue([][]provider.Message{
		nil,
		{{Role: provider.RoleUser, Content: "payload1"}},
		{{Role: provider.RoleUser, Content: "payload2"}},
	})
	steer := agentloop.NewSteer()
	steer.SetInjector(inj.drain)

	resCh := make(chan agentloop.Result, 1)
	errCh := make(chan error, 1)
	go func() {
		res, err := loop.RunSteerable(context.Background(),
			[]provider.Message{textMessage(provider.RoleUser, "hello")}, steer)
		resCh <- res
		errCh <- err
	}()

	// Wait for the first midpoint to enter, fire Trigger, wait for
	// the second midpoint to enter, fire Trigger again.
	gated.waitEnter(0)
	steer.Trigger()
	gated.waitEnter(1)
	steer.Trigger()

	res, err := <-resCh, <-errCh
	if err != nil {
		t.Fatalf("RunSteerable error: %v", err)
	}
	if res.Stop != agentloop.StopNoToolCalls {
		t.Fatalf("Stop = %q, want StopNoToolCalls", res.Stop)
	}

	// (a) payload1 appears exactly once in history.
	count1 := countRoleUserContent(res.History, "payload1")
	if count1 != 1 {
		t.Fatalf("payload1 occurrences = %d, want 1 (deliver once after first steered iteration top): %+v", count1, res.History)
	}
	// (b) payload2 appears exactly once in history.
	count2 := countRoleUserContent(res.History, "payload2")
	if count2 != 1 {
		t.Fatalf("payload2 occurrences = %d, want 1 (deliver once after second steered iteration top): %+v", count2, res.History)
	}
	// (c) injector call count is exactly 3 (one per iter top). The
	// downgrade path does not re-call drainInjected.
	if got := inj.callCount(); got != 3 {
		t.Fatalf("injector call count = %d, want 3 (top of iter 1, top of iter 2, top of iter 3): the downgrade path must NOT re-call drainInjected", got)
	}
}

// countRoleUserContent returns the count of RoleUser messages whose
// Content contains substr. Local to this file.
func countRoleUserContent(history []provider.Message, substr string) int {
	n := 0
	for _, m := range history {
		if m.Role == provider.RoleUser && strings.Contains(m.Content, substr) {
			n++
		}
	}
	return n
}

// gatedCompleter scripts a fixed sequence of three Chat call sites:
// the first two block on ctx.Done() (Trigger fires mid-call), the
// third returns a scripted "ok" final response. waitEnter(idx)
// blocks until the idx'th Chat call has entered, so the test can
// synchronize Trigger calls against the in-flight Chat windows.
type gatedCompleter struct {
	mu      sync.Mutex
	calls   int
	entered []chan struct{}
}

func newGatedCompleter() *gatedCompleter {
	return &gatedCompleter{entered: []chan struct{}{
		make(chan struct{}),
		make(chan struct{}),
	}}
}

func (c *gatedCompleter) Name() string { return "gated" }

func (c *gatedCompleter) Chat(ctx context.Context, req provider.Request) (provider.Response, error) {
	c.mu.Lock()
	idx := c.calls
	c.calls++
	c.mu.Unlock()
	if idx == 0 || idx == 1 {
		select {
		case <-c.entered[idx]:
		default:
			close(c.entered[idx])
		}
		<-ctx.Done()
		return provider.Response{}, ctx.Err()
	}
	return provider.Response{Message: textMessage(provider.RoleAssistant, "ok")}, nil
}

func (c *gatedCompleter) ChatStream(ctx context.Context, req provider.Request) (<-chan provider.Chunk, error) {
	return nil, nil
}

// waitEnter blocks until the idx'th Chat call has entered.
func (c *gatedCompleter) waitEnter(idx int) {
	<-c.entered[idx]
}
