package agentloop_test

// Pull-based steer injector tests (mivia-agent blocker 2). The Steer
// injector is the SDK-side carrier of mivia-agent's legacy BeforeStep
// hook: a host installs a func that returns messages, the loop drains
// the injector at the top of every iteration and at every steered-
// stop decision point, and a non-empty return appends those messages
// to history while the run CONTINUES (a pending StopSteered is
// downgraded in that case).
//
// The cases here pin the contract end to end: ordering (injected
// messages land AFTER the prior iteration's tool results, not
// interleaved with them), downgrade semantics (mid-Chat Trigger +
// non-empty injector continues the run; Trigger + empty injector
// still stops with StopSteered), the sticky-trigger fix (the
// triggered flag MUST be cleared at the downgrade point, otherwise
// the post-injection Chat call cancels instantly and the run
// silently dies), the nil/no-op injector (behavior identical to
// no injector), and MaxIterations accounting (an injection does not
// consume an iteration).
//
// Fixtures (injectorFixture, newInjectorLoop) live in
// steer_injector_fixtures_test.go.

import (
	"context"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/MiviaLabs/mivia-ai-sdk/agentloop"
	"github.com/MiviaLabs/mivia-ai-sdk/provider"
	"github.com/MiviaLabs/mivia-ai-sdk/tools"
)

// injectorMessagesContain reports whether messages carries a
// provider.Message whose Content contains substr. Local to this file
// so the steer-injector tests stand alone.
func injectorMessagesContain(messages []provider.Message, substr string) bool {
	for _, m := range messages {
		if strings.Contains(m.Content, substr) {
			return true
		}
	}
	return false
}

// TestInjectorDrainsAtTopOfIteration1 proves the injector is called
// at the top of the first iteration and the returned user message
// reaches the Completer request for iteration 1. The injector fires
// BEFORE the first Completer.Chat call; the injected user-role
// message must therefore appear in the call's Messages slice.
func TestInjectorDrainsAtTopOfIteration1(t *testing.T) {
	inj := &injectorFixture{}
	inj.setNext([]provider.Message{
		{Role: provider.RoleUser, Content: "injected before first call"},
	})
	final := provider.Response{Message: textMessage(provider.RoleAssistant, "ok")}
	c := &scriptedCompleter{responses: []provider.Response{final}}
	loop := newInjectorLoop(t, c, 5)

	steer := agentloop.NewSteer()
	steer.SetInjector(inj.drain)

	msgs := []provider.Message{textMessage(provider.RoleUser, "hello")}
	res, err := loop.RunSteerable(context.Background(), msgs, steer)
	if err != nil {
		t.Fatalf("RunSteerable error: %v", err)
	}
	if res.Stop != agentloop.StopNoToolCalls {
		t.Fatalf("Stop = %q, want StopNoToolCalls", res.Stop)
	}
	if inj.callCount() != 1 {
		t.Fatalf("injector calls = %d, want 1 (top of iteration 1)", inj.callCount())
	}
	req := c.requestAt(0)
	want := "injected before first call"
	if !injectorMessagesContain(req.Messages, want) {
		t.Fatalf("call 1 request missing injected message %q: %+v", want, req.Messages)
	}
}

// TestInjectorLandsAfterToolResults proves the "before trim" placement
// keeps injected messages after the previous iteration's tool-result
// frames: the injector fires once per iteration, the ordering inside
// history must be [..., assistant tool-call, tool result, injected
// frame, ...]. This is the same placement the legacy context.go:15-19
// BeforeStep path used; the test guards against the SDK landing it
// in the wrong order.
func TestInjectorLandsAfterToolResults(t *testing.T) {
	reg := tools.New()
	mustAdd(t, reg, &schemaEchoTool{name: "echo", schema: []byte(`{"type":"object"}`), result: "ok"})
	first := toolCallResponse(provider.ToolCall{ID: "c1", Name: "echo", Arguments: []byte("{}")})
	final := provider.Response{Message: textMessage(provider.RoleAssistant, "ok")}
	c := &scriptedCompleter{responses: []provider.Response{first, final}}
	loop, err := agentloop.New(agentloop.Options{Completer: c, Tools: reg, MaxIterations: 5})
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	inj := &injectorFixture{}
	// Iteration 1 top: nil. Iteration 2 top: "steer body" — this is
	// the only case where the injector MUST land AFTER the prior
	// iteration's tool-result frame. The "before trim" placement
	// means inject happens at the top of the next iteration, by which
	// time the previous iteration's tool result is already in history.
	inj.setQueue([][]provider.Message{
		nil,
		{{Role: provider.RoleUser, Content: "steer body"}},
	})
	steer := agentloop.NewSteer()
	steer.SetInjector(inj.drain)

	msgs := []provider.Message{textMessage(provider.RoleUser, "hello")}
	res, err := loop.RunSteerable(context.Background(), msgs, steer)
	if err != nil {
		t.Fatalf("RunSteerable error: %v", err)
	}
	if res.Stop != agentloop.StopNoToolCalls {
		t.Fatalf("Stop = %q, want StopNoToolCalls", res.Stop)
	}

	idxToolResult := -1
	idxSteerBody := -1
	for i, m := range res.History {
		switch {
		case m.Role == provider.RoleTool && m.ToolCallID == "c1":
			idxToolResult = i
		case m.Role == provider.RoleUser && strings.Contains(m.Content, "steer body"):
			idxSteerBody = i
		}
	}
	if idxToolResult < 0 || idxSteerBody < 0 {
		t.Fatalf("landmark indexes: toolResult=%d steerBody=%d; want both >=0: %+v",
			idxToolResult, idxSteerBody, res.History)
	}
	if !(idxToolResult < idxSteerBody) {
		t.Fatalf("injected message (idx %d) must come AFTER tool result (idx %d): %+v",
			idxSteerBody, idxToolResult, res.History)
	}
	if !injectorMessagesContain(c.requestAt(1).Messages, "steer body") {
		t.Fatalf("call 2 request missing injected steer: %+v", c.requestAt(1).Messages)
	}
	if inj.callCount() != 2 {
		t.Fatalf("injector calls = %d, want 2 (top of iteration 1 and iteration 2)", inj.callCount())
	}
}

// TestInjectorTriggeredAndNonEmptyContinuesRun is the load-bearing
// mid-Chat downgrade case: Trigger fires during a blocking Chat
// call, the run reaches the steered-stop branch, the injector
// returns non-empty, the messages are appended, the sticky
// triggered flag is cleared, and the run CONTINUES with Stop
// != StopSteered and Iterations counting the post-injection
// completion.
func TestInjectorTriggeredAndNonEmptyContinuesRun(t *testing.T) {
	// call 0: blocking midpoint (Trigger fires mid-call)
	// call 1: scripted "ok" (post-injection completion)
	c := newInjectorGateCompleter(
		[]provider.Response{
			{Message: textMessage(provider.RoleAssistant, "ok")},
		},
		0,
	)
	loop := newInjectorLoop(t, c, 5)

	inj := &injectorFixture{}
	// Drain sequence: iter-1 top = empty (no pre-loop frame),
	// iter-1 steered-stop downgrade = deliver payload,
	// iter-2 top = empty (fixture exhausted).
	inj.setQueue([][]provider.Message{
		nil,
		{{Role: provider.RoleUser, Content: "downgrade payload"}},
	})
	steer := agentloop.NewSteer()
	steer.SetInjector(inj.drain)

	msgs := []provider.Message{textMessage(provider.RoleUser, "hello")}

	resCh := make(chan agentloop.Result, 1)
	errCh := make(chan error, 1)
	go func() {
		res, err := loop.RunSteerable(context.Background(), msgs, steer)
		resCh <- res
		errCh <- err
	}()

	<-c.entered
	steer.Trigger()

	res, err := <-resCh, <-errCh
	if err != nil {
		t.Fatalf("RunSteerable error: %v", err)
	}
	if res.Stop == agentloop.StopSteered {
		t.Fatalf("Stop = StopSteered, want a graceful completion: the injector delivered a non-empty payload, so the steered stop must downgrade to continue")
	}
	if res.Stop != agentloop.StopNoToolCalls {
		t.Fatalf("Stop = %q, want StopNoToolCalls", res.Stop)
	}
	if !injectorMessagesContain(res.History, "downgrade payload") {
		t.Fatalf("downgrade payload not present in history: %+v", res.History)
	}
	if res.Iterations < 1 {
		t.Fatalf("Iterations = %d, want >=1 (post-injection completion counts as an iteration; the blocked midpoint does not)", res.Iterations)
	}
}

// TestInjectorStickyTriggerClearedAtDowngrade is the regression test
// for Part A.4's sticky-trigger fix. The exact sequence:
//  1. Trigger fires during a blocking Chat call (iteration 1).
//  2. Iteration 1's steered-stop downgrade path calls drainInjected;
//     injector returns a non-empty payload; ackTriggered runs.
//  3. Iteration 2 begins. Its top-of-iteration drainInjected returns
//     empty.
//  4. Iteration 2's Chat call arms; without ackTriggered, the
//     triggered flag is still true, the arm immediately cancels the
//     derived context, the Completer returns ctx.Err() with the
//     steer triggered, the steered-stop downgrade path runs again,
//     the injector now returns empty, and the run stops with
//     StopSteered and zero Final.
//
// With the fix, step 2's ackTriggered clears the sticky flag, step 4
// arms un-triggered, Chat completes normally, and the run finishes
// gracefully with the post-injection assistant text as the final.
func TestInjectorStickyTriggerClearedAtDowngrade(t *testing.T) {
	// call 0: blocking midpoint (Trigger fires here)
	// call 1: scripted "post-injection final" (must complete, NOT cancel)
	c := newInjectorGateCompleter(
		[]provider.Response{
			{Message: textMessage(provider.RoleAssistant, "first")},
		},
		0,
	)
	loop := newInjectorLoop(t, c, 5)

	inj := &injectorFixture{}
	// Same drain sequence as the continues-run test: empty at iter-1
	// top, deliver at iter-1 downgrade, empty at iter-2 top.
	inj.setQueue([][]provider.Message{
		nil,
		{{Role: provider.RoleUser, Content: "deliver once"}},
	})
	steer := agentloop.NewSteer()
	steer.SetInjector(inj.drain)

	msgs := []provider.Message{textMessage(provider.RoleUser, "hello")}

	resCh := make(chan agentloop.Result, 1)
	errCh := make(chan error, 1)
	go func() {
		res, err := loop.RunSteerable(context.Background(), msgs, steer)
		resCh <- res
		errCh <- err
	}()

	<-c.entered
	steer.Trigger()

	res, err := <-resCh, <-errCh
	if err != nil {
		t.Fatalf("RunSteerable error: %v", err)
	}
	if res.Stop == agentloop.StopSteered {
		t.Fatalf("Stop = StopSteered: sticky-trigger fix failed; the post-injection Chat call was cancelled because the triggered flag was never cleared at the downgrade point")
	}
	if res.Stop != agentloop.StopNoToolCalls {
		t.Fatalf("Stop = %q, want StopNoToolCalls", res.Stop)
	}
	if !injectorMessagesContain(res.History, "deliver once") {
		t.Fatalf("deliver-once frame missing from history: %+v", res.History)
	}
	if res.Iterations < 1 {
		t.Fatalf("Iterations = %d, want >=1 (post-injection completion)", res.Iterations)
	}
}

// TestInjectorNilBehaviorIsIdenticalToNoInjector pins the
// nil-or-empty contract: a Steer with no injector behaves exactly
// like a Steer with an injector that always returns nil. The test
// runs the same script through both Steer shapes and asserts the
// two outcomes are identical.
func TestInjectorNilBehaviorIsIdenticalToNoInjector(t *testing.T) {
	final := provider.Response{Message: textMessage(provider.RoleAssistant, "ok")}

	c1 := &scriptedCompleter{responses: []provider.Response{final}}
	loop1 := newInjectorLoop(t, c1, 5)
	steer1 := agentloop.NewSteer()
	msgs := []provider.Message{textMessage(provider.RoleUser, "hello")}
	res1, err1 := loop1.RunSteerable(context.Background(), msgs, steer1)

	c2 := &scriptedCompleter{responses: []provider.Response{final}}
	loop2 := newInjectorLoop(t, c2, 5)
	steer2 := agentloop.NewSteer()
	inj := &injectorFixture{}
	steer2.SetInjector(inj.drain)
	res2, err2 := loop2.RunSteerable(context.Background(), msgs, steer2)

	if err1 != nil || err2 != nil {
		t.Fatalf("errors = %v, %v, want nil, nil", err1, err2)
	}
	if res1.Stop != res2.Stop {
		t.Fatalf("Stop = %q, %q, want equal", res1.Stop, res2.Stop)
	}
	if res1.Iterations != res2.Iterations {
		t.Fatalf("Iterations = %d, %d, want equal", res1.Iterations, res2.Iterations)
	}
	if inj.callCount() != 1 {
		t.Fatalf("injector calls = %d, want 1 (top of iteration 1 only)", inj.callCount())
	}
}

// TestInjectorNoOpBehaviorIsIdenticalToNoInjector pins the empty-
// return contract: an injector that always returns nil is observed
// identically to no injector at all.
func TestInjectorNoOpBehaviorIsIdenticalToNoInjector(t *testing.T) {
	final := provider.Response{Message: textMessage(provider.RoleAssistant, "ok")}

	c1 := &scriptedCompleter{responses: []provider.Response{final}}
	loop1 := newInjectorLoop(t, c1, 5)
	steer1 := agentloop.NewSteer()
	inj1 := &injectorFixture{}
	inj1.setNoOp()
	steer1.SetInjector(inj1.drain)

	c2 := &scriptedCompleter{responses: []provider.Response{final}}
	loop2 := newInjectorLoop(t, c2, 5)
	steer2 := agentloop.NewSteer()

	msgs := []provider.Message{textMessage(provider.RoleUser, "hi")}
	res1, err1 := loop1.RunSteerable(context.Background(), msgs, steer1)
	res2, err2 := loop2.RunSteerable(context.Background(), msgs, steer2)

	if err1 != nil || err2 != nil {
		t.Fatalf("errors = %v, %v, want nil, nil", err1, err2)
	}
	if res1.Stop != res2.Stop || res1.Iterations != res2.Iterations {
		t.Fatalf("no-op injector run = %+v, plain run = %+v, want equal Stop and Iterations", res1, res2)
	}
	if inj1.callCount() != 1 {
		t.Fatalf("no-op injector calls = %d, want 1 (top of iteration 1)", inj1.callCount())
	}
}

// TestInjectorDoesNotConsumeIteration pins the MaxIterations
// accounting rule: a non-empty injector return grows history but
// does not advance `iterations`. The cap is on Completer calls, not
// on the number of injected frames.
func TestInjectorDoesNotConsumeIteration(t *testing.T) {
	inj := &injectorFixture{}
	inj.setNext([]provider.Message{
		{Role: provider.RoleUser, Content: "injected 1"},
		{Role: provider.RoleUser, Content: "injected 2"},
		{Role: provider.RoleUser, Content: "injected 3"},
	})
	final := provider.Response{Message: textMessage(provider.RoleAssistant, "ok")}
	c := &scriptedCompleter{responses: []provider.Response{final}}
	loop := newInjectorLoop(t, c, 1)

	steer := agentloop.NewSteer()
	steer.SetInjector(inj.drain)

	msgs := []provider.Message{textMessage(provider.RoleUser, "hello")}
	res, err := loop.RunSteerable(context.Background(), msgs, steer)
	if err != nil {
		t.Fatalf("RunSteerable error: %v", err)
	}
	if res.Stop != agentloop.StopNoToolCalls {
		t.Fatalf("Stop = %q, want StopNoToolCalls", res.Stop)
	}
	if res.Iterations != 1 {
		t.Fatalf("Iterations = %d, want 1: the injection must not consume an iteration slot", res.Iterations)
	}
	count := 0
	for _, m := range res.History {
		if m.Role == provider.RoleUser && strings.HasPrefix(m.Content, "injected ") {
			count++
		}
	}
	if count != 3 {
		t.Fatalf("injected frames in history = %d, want 3: %+v", count, res.History)
	}
}

// TestInjectorSetInjectorNilRemoves is the documented "set nil to
// remove" contract: a SetInjector(nil) call after a previous
// non-nil SetInjector must restore the no-injector baseline.
func TestInjectorSetInjectorNilRemoves(t *testing.T) {
	final := provider.Response{Message: textMessage(provider.RoleAssistant, "ok")}
	c := &scriptedCompleter{responses: []provider.Response{final}}
	loop := newInjectorLoop(t, c, 5)

	steer := agentloop.NewSteer()
	var called atomic.Int32
	steer.SetInjector(func() []provider.Message {
		called.Add(1)
		return []provider.Message{{Role: provider.RoleUser, Content: "should not appear"}}
	})
	steer.SetInjector(nil)

	msgs := []provider.Message{textMessage(provider.RoleUser, "hello")}
	res, err := loop.RunSteerable(context.Background(), msgs, steer)
	if err != nil {
		t.Fatalf("RunSteerable error: %v", err)
	}
	if called.Load() != 0 {
		t.Fatalf("injector calls after SetInjector(nil) = %d, want 0", called.Load())
	}
	for _, m := range res.History {
		if strings.Contains(m.Content, "should not appear") {
			t.Fatalf("found an injected frame after SetInjector(nil): %+v", res.History)
		}
	}
}

// TestInjectorPreservedAcrossReset pins the "preserve injector in
// reset" rule: one Steer wired with an injector runs two
// RunSteerable calls back to back, and both calls observe the
// injector. Without reset preserving the injector field, the second
// call would silently run with no injector.
func TestInjectorPreservedAcrossReset(t *testing.T) {
	final := provider.Response{Message: textMessage(provider.RoleAssistant, "ok")}

	c1 := &scriptedCompleter{responses: []provider.Response{final}}
	loop1 := newInjectorLoop(t, c1, 5)
	steer := agentloop.NewSteer()
	inj := &injectorFixture{}
	steer.SetInjector(inj.drain)

	msgs := []provider.Message{textMessage(provider.RoleUser, "hello")}
	if _, err := loop1.RunSteerable(context.Background(), msgs, steer); err != nil {
		t.Fatalf("first RunSteerable error: %v", err)
	}
	if got := inj.callCount(); got != 1 {
		t.Fatalf("first-run injector calls = %d, want 1", got)
	}

	c2 := &scriptedCompleter{responses: []provider.Response{final}}
	loop2 := newInjectorLoop(t, c2, 5)
	if _, err := loop2.RunSteerable(context.Background(), msgs, steer); err != nil {
		t.Fatalf("second RunSteerable error: %v", err)
	}
	if got := inj.callCount(); got != 2 {
		t.Fatalf("second-run injector calls = %d, want 2 (the first call's reset must preserve the injector)", got)
	}
}
