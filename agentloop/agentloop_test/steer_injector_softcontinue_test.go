package agentloop_test

// Steered-stop continuation is gated solely by
// Options.Extensions.ContinueOnStop. These cases pin the gate
// semantics around an installed injector: with a nil hook or an
// empty return, a steered stop ends the run with StopSteered even
// though an injector is installed (the injector auto-continue is
// gone); a non-empty return appends the gate messages to history and
// the run continues, with the trigger acked before the next arm and
// the injector still draining exactly once per iteration top. A
// panic in the hook fails the run closed.

import (
	"context"
	"strings"
	"testing"

	"github.com/MiviaLabs/mivia-ai-sdk/agentloop"
	"github.com/MiviaLabs/mivia-ai-sdk/provider"
	"github.com/MiviaLabs/mivia-ai-sdk/tools"
)

// TestInjectorTriggeredAndEmptyStopsSteered is the regression guard
// for Trigger semantics with NO injector and a nil ContinueOnStop: a
// Trigger fired mid-Chat ends the run with StopSteered and a nil
// error, the pre-injector SDK contract.
func TestInjectorTriggeredAndEmptyStopsSteered(t *testing.T) {
	entered := make(chan struct{})
	c := &blockingCompleter{entered: entered}
	loop := newInjectorLoop(t, c, 5)

	// No SetInjector call and a nil ContinueOnStop: the steered stop
	// ends the run with the single-shot StopSteered shape.
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
		t.Fatalf("RunSteerable error: %v", err)
	}
	if res.Stop != agentloop.StopSteered {
		t.Fatalf("Stop = %q, want StopSteered: a Steer with no injector and no hook must keep the single-shot stop", res.Stop)
	}
}

// TestInjectorTriggeredGateNilStopsSteered pins the removal of the
// injector auto-continue: with an injector installed and an empty
// drain, a nil ContinueOnStop still ends the run with StopSteered.
func TestInjectorTriggeredGateNilStopsSteered(t *testing.T) {
	c := newInjectorGateCompleter(
		[]provider.Response{
			{Message: textMessage(provider.RoleAssistant, "ok")},
		},
		0,
	)
	loop := newInjectorLoop(t, c, 5)

	inj := &injectorFixture{}
	// Iteration 1 top: empty. The steered stop then ends the run.
	steer := agentloop.NewSteer()
	steer.SetInjector(inj.drain)

	msgs := []provider.Message{textMessage(provider.RoleUser, "hi")}

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
	if res.Stop != agentloop.StopSteered {
		t.Fatalf("Stop = %q, want StopSteered: injector presence must not force a continuation", res.Stop)
	}
	if inj.callCount() != 1 {
		t.Fatalf("injector calls = %d, want 1 (iteration 1 top only; the run stopped)", inj.callCount())
	}
}

// TestInjectorTriggeredGateContinueContinues pins the continue path:
// a non-empty ContinueOnStop return appends the gate messages to
// history, the trigger is acked before the next arm (the next Chat
// does not spin or cancel instantly), and the injector still drains
// exactly once at the next iteration top.
func TestInjectorTriggeredGateContinueContinues(t *testing.T) {
	rec := &stopHookRecorder{decide: continueSteeredOnly()}
	c := newInjectorGateCompleter(
		[]provider.Response{
			{Message: textMessage(provider.RoleAssistant, "ok")},
		},
		0,
	)
	loop, err := agentloop.New(agentloop.Options{
		Completer:  c,
		Tools:      tools.New(),
		Bounds:     agentloop.Bounds{MaxIterations: 5},
		Extensions: &agentloop.Extensions{ContinueOnStop: rec.hook},
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	inj := &injectorFixture{}
	inj.setNext([]provider.Message{{Role: provider.RoleUser, Content: "injected at iteration 2 top"}})
	steer := agentloop.NewSteer()
	steer.SetInjector(inj.drain)

	msgs := []provider.Message{textMessage(provider.RoleUser, "hi")}

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
	if res.Stop != agentloop.StopNoToolCalls {
		t.Fatalf("Stop = %q, want StopNoToolCalls: the non-empty hook return must continue the run", res.Stop)
	}
	if rec.count() != 2 {
		t.Fatalf("hook invocations = %d, want 2 (steered stop + final stop)", rec.count())
	}
	if d := rec.at(t, 0); d.Stop != agentloop.StopSteered {
		t.Fatalf("hook decision Stop = %q, want StopSteered", d.Stop)
	}
	if d := rec.at(t, 1); d.Stop != agentloop.StopNoToolCalls {
		t.Fatalf("final hook decision Stop = %q, want StopNoToolCalls", d.Stop)
	}
	if inj.callCount() != 2 {
		t.Fatalf("injector calls = %d, want 2 (iteration tops 1 and 2)", inj.callCount())
	}
	if !injectorMessagesContain(res.History, "keep going") {
		t.Fatalf("history missing the gate continuation message: %+v", res.History)
	}
	if !injectorMessagesContain(res.History, "injected at iteration 2 top") {
		t.Fatalf("history missing the iteration-2 injected payload: %+v", res.History)
	}
}

// TestGatePanicOnSteeredStopFailsClosed pins that a panicking
// ContinueOnStop at a steered stop fails the run closed instead of
// half-continuing.
func TestGatePanicOnSteeredStopFailsClosed(t *testing.T) {
	entered := make(chan struct{})
	c := &blockingCompleter{entered: entered}
	loop, err := agentloop.New(agentloop.Options{
		Completer: c,
		Tools:     tools.New(),
		Bounds:    agentloop.Bounds{MaxIterations: 5},
		Extensions: &agentloop.Extensions{ContinueOnStop: func(context.Context, agentloop.StopDecision) []provider.Message {
			panic("gate boom")
		}},
	})
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

	_, err = <-resCh, <-errCh
	if err == nil {
		t.Fatalf("RunSteerable error = nil, want the hook panic surfaced as a plain error")
	}
	if !strings.Contains(err.Error(), "gate boom") {
		t.Fatalf("error = %v, want the panic value in the error", err)
	}
}
