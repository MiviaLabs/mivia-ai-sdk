package agentloop_test

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/MiviaLabs/mivia-ai-sdk/agentloop"
	"github.com/MiviaLabs/mivia-ai-sdk/provider"
	"github.com/MiviaLabs/mivia-ai-sdk/tools"
)

// TestSteerTriggerMidCompleter proves a Trigger fired while the
// in-flight Completer.Chat call blocks stops RunSteerable gracefully
// with StopSteered, a nil error, and a zero Final.
func TestSteerTriggerMidCompleter(t *testing.T) {
	entered := make(chan struct{})
	c := &blockingCompleter{entered: entered}
	loop := newSteerLoop(t, c, 5)
	steer := agentloop.NewSteer()
	msgs := []provider.Message{textMessage(provider.RoleUser, "hi")}

	resCh := make(chan agentloop.Result, 1)
	errCh := make(chan error, 1)
	go func() {
		res, err := loop.RunSteerable(context.Background(), msgs, steer)
		resCh <- res
		errCh <- err
	}()

	<-entered
	steer.Trigger()
	res, err := <-resCh, <-errCh

	if err != nil {
		t.Fatalf("RunSteerable() error = %v, want nil", err)
	}
	if res.Stop != agentloop.StopSteered {
		t.Fatalf("Stop = %q, want StopSteered", res.Stop)
	}
	if !isZeroMessage(res.Final) {
		t.Fatalf("Final = %+v, want the zero value", res.Final)
	}
	if res.Iterations != 0 {
		t.Fatalf("Iterations = %d, want 0", res.Iterations)
	}
}

// TestSteerTriggerAfterPriorIteration proves a steer stop preserves
// every already-completed iteration's History, Iterations, and Usage.
func TestSteerTriggerAfterPriorIteration(t *testing.T) {
	reg := tools.New()
	mustAdd(t, reg, &schemaEchoTool{name: "search", schema: []byte(`{"type":"object"}`), result: "ok"})
	first := toolCallResponse(provider.ToolCall{ID: "c1", Name: "search", Arguments: []byte("{}")})
	first.Usage = provider.Usage{TotalTokens: 7}
	entered := make(chan struct{})
	c := &blockingCompleter{responses: []provider.Response{first}, entered: entered}
	loop, err := agentloop.New(agentloop.Options{Completer: c, Tools: reg, MaxIterations: 5})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	steer := agentloop.NewSteer()
	msgs := []provider.Message{textMessage(provider.RoleUser, "hi")}

	resCh := make(chan agentloop.Result, 1)
	errCh := make(chan error, 1)
	go func() {
		res, err := loop.RunSteerable(context.Background(), msgs, steer)
		resCh <- res
		errCh <- err
	}()

	<-entered
	steer.Trigger()
	res, err := <-resCh, <-errCh

	if err != nil {
		t.Fatalf("RunSteerable() error = %v, want nil", err)
	}
	if res.Stop != agentloop.StopSteered {
		t.Fatalf("Stop = %q, want StopSteered", res.Stop)
	}
	if res.Iterations != 1 {
		t.Fatalf("Iterations = %d, want 1", res.Iterations)
	}
	if res.Usage.TotalTokens != 7 {
		t.Fatalf("Usage.TotalTokens = %d, want 7", res.Usage.TotalTokens)
	}
	foundTool := false
	for _, m := range res.History {
		if m.Role == provider.RoleTool && m.ToolCallID == "c1" {
			foundTool = true
		}
	}
	if !foundTool {
		t.Fatalf("History missing the completed tool call's RoleTool message: %+v", res.History)
	}
	if !isZeroMessage(res.Final) {
		t.Fatalf("Final = %+v, want the zero value", res.Final)
	}
}

// TestSteerNeverTriggered proves a Steer passed to RunSteerable but
// never triggered runs to its normal stop, identical to Run.
func TestSteerNeverTriggered(t *testing.T) {
	msgs := []provider.Message{textMessage(provider.RoleUser, "hi")}
	final := provider.Response{Message: textMessage(provider.RoleAssistant, "done")}

	sc1 := &scriptedCompleter{responses: []provider.Response{final}}
	loop1 := newSteerLoop(t, sc1, 5)
	steer := agentloop.NewSteer()
	res1, err1 := loop1.RunSteerable(context.Background(), msgs, steer)

	sc2 := &scriptedCompleter{responses: []provider.Response{final}}
	loop2 := newSteerLoop(t, sc2, 5)
	res2, err2 := loop2.Run(context.Background(), msgs)

	if err1 != nil || err2 != nil {
		t.Fatalf("errors = %v, %v, want nil, nil", err1, err2)
	}
	if res1.Stop != res2.Stop || res1.Stop != agentloop.StopNoToolCalls {
		t.Fatalf("Stop = %q, %q, want both StopNoToolCalls", res1.Stop, res2.Stop)
	}
	if res1.Final.Role != res2.Final.Role || res1.Final.Content != res2.Final.Content {
		t.Fatalf("Final = %+v, %+v, want equal", res1.Final, res2.Final)
	}
}

// TestSteerCtxCanceledDirectly proves a direct ctx cancellation still
// hard-fails RunSteerable, unchanged, even with an untriggered Steer
// present.
func TestSteerCtxCanceledDirectly(t *testing.T) {
	entered := make(chan struct{})
	c := &blockingCompleter{entered: entered}
	loop := newSteerLoop(t, c, 5)
	steer := agentloop.NewSteer()
	msgs := []provider.Message{textMessage(provider.RoleUser, "hi")}
	ctx, cancel := context.WithCancel(context.Background())

	resCh := make(chan agentloop.Result, 1)
	errCh := make(chan error, 1)
	go func() {
		res, err := loop.RunSteerable(ctx, msgs, steer)
		resCh <- res
		errCh <- err
	}()

	<-entered
	cancel()
	res, err := <-resCh, <-errCh

	if !errors.Is(err, context.Canceled) {
		t.Fatalf("RunSteerable() error = %v, want context.Canceled", err)
	}
	if res.Stop != "" {
		t.Fatalf("Stop = %q, want the zero value", res.Stop)
	}
	if !isZeroResult(res) {
		t.Fatalf("Result = %+v, want the zero value", res)
	}
}

// TestSteerNotTriggeredSpontaneousCancel proves isSteerStop does not
// misclassify a Completer's own context.Canceled-wrapping error as a
// steer stop just because a never-triggered Steer was passed in.
func TestSteerNotTriggeredSpontaneousCancel(t *testing.T) {
	spontaneous := fmt.Errorf("vendor: %w", context.Canceled)
	sc := &scriptedCompleter{errs: []error{spontaneous}}
	loop := newSteerLoop(t, sc, 5)
	steer := agentloop.NewSteer()
	msgs := []provider.Message{textMessage(provider.RoleUser, "hi")}

	res, err := loop.RunSteerable(context.Background(), msgs, steer)

	if !errors.Is(err, context.Canceled) {
		t.Fatalf("RunSteerable() error = %v, want it to wrap context.Canceled", err)
	}
	if res.Stop == agentloop.StopSteered {
		t.Fatalf("Stop = StopSteered, want anything else: a Steer present but never triggered must not be classified as a steer stop")
	}
}

// TestSteerTriggeredButErrNotCanceled proves isSteerStop does not
// classify a triggered steer's iteration as a steer stop when the
// Completer's own error does not wrap context.Canceled: isSteerStop's
// errors.Is(err, context.Canceled) condition must hold too, not just
// steer.wasTriggered(). RunSteerable must hard-fail with errVendorReset
// instead, the same as an untriggered Steer would.
func TestSteerTriggeredButErrNotCanceled(t *testing.T) {
	entered := make(chan struct{})
	c := &nonCancelOnDoneCompleter{entered: entered}
	loop := newSteerLoop(t, c, 5)
	steer := agentloop.NewSteer()
	msgs := []provider.Message{textMessage(provider.RoleUser, "hi")}

	resCh := make(chan agentloop.Result, 1)
	errCh := make(chan error, 1)
	go func() {
		res, err := loop.RunSteerable(context.Background(), msgs, steer)
		resCh <- res
		errCh <- err
	}()

	<-entered
	steer.Trigger()
	res, err := <-resCh, <-errCh

	if !errors.Is(err, errVendorReset) {
		t.Fatalf("RunSteerable() error = %v, want it to wrap errVendorReset", err)
	}
	if res.Stop == agentloop.StopSteered {
		t.Fatalf("Stop = StopSteered, want anything else: a triggered Steer with a non-context.Canceled error must hard-fail, not steer-stop")
	}
	if !isZeroResult(res) {
		t.Fatalf("Result = %+v, want the zero value: no iteration completed", res)
	}
}

// TestSteerTriggerTwiceAndBeforeStart proves Trigger is idempotent and
// that a Trigger fired before RunSteerable starts is a no-op, since
// RunSteerable resets Steer's state at the start of its own call.
func TestSteerTriggerTwiceAndBeforeStart(t *testing.T) {
	steer := agentloop.NewSteer()
	steer.Trigger()
	steer.Trigger()

	msgs := []provider.Message{textMessage(provider.RoleUser, "hi")}
	final := provider.Response{Message: textMessage(provider.RoleAssistant, "done")}
	sc := &scriptedCompleter{responses: []provider.Response{final}}
	loop := newSteerLoop(t, sc, 5)

	res, err := loop.RunSteerable(context.Background(), msgs, steer)
	if err != nil {
		t.Fatalf("RunSteerable() error = %v, want nil", err)
	}
	if res.Stop != agentloop.StopNoToolCalls {
		t.Fatalf("Stop = %q, want StopNoToolCalls: a pre-start Trigger must be a no-op", res.Stop)
	}
}

// TestSteerTriggerConcurrent proves N concurrent Trigger calls on one
// in-flight RunSteerable call cause no panic and no race, and
// RunSteerable returns Stop == StopSteered exactly once.
func TestSteerTriggerConcurrent(t *testing.T) {
	entered := make(chan struct{})
	c := &blockingCompleter{entered: entered}
	loop := newSteerLoop(t, c, 5)
	steer := agentloop.NewSteer()
	msgs := []provider.Message{textMessage(provider.RoleUser, "hi")}

	resCh := make(chan agentloop.Result, 1)
	errCh := make(chan error, 1)
	go func() {
		res, err := loop.RunSteerable(context.Background(), msgs, steer)
		resCh <- res
		errCh <- err
	}()

	<-entered
	var wg sync.WaitGroup
	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			steer.Trigger()
		}()
	}
	wg.Wait()
	res, err := <-resCh, <-errCh

	if err != nil {
		t.Fatalf("RunSteerable() error = %v, want nil", err)
	}
	if res.Stop != agentloop.StopSteered {
		t.Fatalf("Stop = %q, want StopSteered", res.Stop)
	}
}

// TestSteerTriggerMidToolBatch proves a Trigger fired while the
// second of three tool calls in one batch is executing does not
// interrupt that batch: the third call still runs and its RoleTool
// message still reaches History. The following iteration's
// Completer.Chat call is steered immediately, without RunSteerable
// waiting on it to block, proving the "triggered before arm"
// carry-over works within one RunSteerable call.
func TestSteerTriggerMidToolBatch(t *testing.T) {
	reg := tools.New()
	g := &gateTool{gateIndex: 1, entered: make(chan struct{}), release: make(chan struct{})}
	mustAdd(t, reg, g)
	batch := toolCallResponse(
		provider.ToolCall{Index: 0, ID: "c1", Name: "seq", Arguments: []byte("{}")},
		provider.ToolCall{Index: 1, ID: "c2", Name: "seq", Arguments: []byte("{}")},
		provider.ToolCall{Index: 2, ID: "c3", Name: "seq", Arguments: []byte("{}")},
	)
	c := &blockingCompleter{responses: []provider.Response{batch}}
	loop, err := agentloop.New(agentloop.Options{Completer: c, Tools: reg, MaxIterations: 5})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	steer := agentloop.NewSteer()
	msgs := []provider.Message{textMessage(provider.RoleUser, "hi")}

	resCh := make(chan agentloop.Result, 1)
	errCh := make(chan error, 1)
	go func() {
		res, err := loop.RunSteerable(context.Background(), msgs, steer)
		resCh <- res
		errCh <- err
	}()

	<-g.entered
	steer.Trigger()
	close(g.release)

	var res agentloop.Result
	var runErr error
	select {
	case res = <-resCh:
		runErr = <-errCh
	case <-time.After(5 * time.Second):
		t.Fatalf("RunSteerable did not return within 5s: a triggered Steer failed to carry over to the next iteration boundary")
	}

	if runErr != nil {
		t.Fatalf("RunSteerable() error = %v, want nil", runErr)
	}
	if res.Stop != agentloop.StopSteered {
		t.Fatalf("Stop = %q, want StopSteered", res.Stop)
	}
	if g.callCount() != 3 {
		t.Fatalf("gateTool calls = %d, want 3: the third call must still run", g.callCount())
	}
	toolMsgs := 0
	for _, m := range res.History {
		if m.Role == provider.RoleTool {
			toolMsgs++
		}
	}
	if toolMsgs != 3 {
		t.Fatalf("RoleTool messages in History = %d, want 3", toolMsgs)
	}
}

// TestSteerSharedAcrossConcurrentRuns is a documentation test, not a
// guard test: Steer's doc comment forbids passing one Steer to two
// concurrent RunSteerable calls, and this test pins the forbidden
// behavior's shape (no panic, no race) without certifying it as
// supported. The outcome for each call is deliberately unspecified:
// one call's arm can overwrite the other's bound cancel func, so
// Trigger may cancel only one of the two derived contexts. Each call
// runs under its own bounded ctx timeout, not context.Background, so
// a lost cancel still ends that call instead of hanging the test.
func TestSteerSharedAcrossConcurrentRuns(t *testing.T) {
	steer := agentloop.NewSteer()
	msgs := []provider.Message{textMessage(provider.RoleUser, "hi")}

	entered1 := make(chan struct{})
	entered2 := make(chan struct{})
	c1 := &blockingCompleter{entered: entered1}
	c2 := &blockingCompleter{entered: entered2}
	loop1 := newSteerLoop(t, c1, 5)
	loop2 := newSteerLoop(t, c2, 5)
	ctx1, cancel1 := context.WithTimeout(context.Background(), time.Second)
	defer cancel1()
	ctx2, cancel2 := context.WithTimeout(context.Background(), time.Second)
	defer cancel2()

	var wg sync.WaitGroup
	wg.Add(2)
	go func() {
		defer wg.Done()
		_, _ = loop1.RunSteerable(ctx1, msgs, steer)
	}()
	go func() {
		defer wg.Done()
		_, _ = loop2.RunSteerable(ctx2, msgs, steer)
	}()

	<-entered1
	<-entered2
	steer.Trigger()
	wg.Wait()
}
