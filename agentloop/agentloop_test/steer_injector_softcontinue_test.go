package agentloop_test

// Soft-continue shape for the steer-injector. Split out from
// steer_injector_test.go to keep both files under the structure
// gate's per-file line cap. These two cases pin the
// load-bearing split on hasInjector(): a Steer WITH an installed
// injector soft-continues every steer; a Steer WITHOUT an injector
// keeps the original single-shot StopSteered shape.

import (
	"context"
	"testing"

	"github.com/MiviaLabs/mivia-ai-sdk/agentloop"
	"github.com/MiviaLabs/mivia-ai-sdk/provider"
)

// TestInjectorTriggeredAndEmptyStopsSteered is the regression guard
// for the existing Trigger semantics on a Steer with NO injector
// installed: a Trigger fired mid-Chat must keep the existing
// StopSteered behavior, the pre-injector SDK contract.
//
// The companion case "an injector IS installed and the run is
// steered" soft-continues even when the drain is empty — see
// TestInjectorTriggeredAndEmptyInjectorSoftContinues. The split is
// load-bearing: a host that installs an injector opts into the
// soft-continue shape (mivia-agent's bridgeSteerSignals relies on
// it to deliver repeated steers within one RunSteerable call); a
// host that does not install an injector sees the original
// single-shot StopSteered behavior every pre-injector Steer test
// pins.
func TestInjectorTriggeredAndEmptyStopsSteered(t *testing.T) {
	entered := make(chan struct{})
	c := &blockingCompleter{entered: entered}
	loop := newInjectorLoop(t, c, 5)

	// No SetInjector call: hasInjector() returns false, the steered-
	// stop branch keeps the original single-shot StopSteered shape.
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
		t.Fatalf("Stop = %q, want StopSteered: a Steer with no injector must keep the existing single-shot stop", res.Stop)
	}
}

// TestInjectorTriggeredAndEmptyInjectorSoftContinues pins the
// soft-continue shape: a Steer WITH an installed injector and an
// empty drain at the downgrade point continues the run. This is
// the behavior mivia-agent's bridgeSteerSignals relies on — a
// bridge that polls continuously across iterations must be able to
// deliver repeated steers within one RunSteerable call without
// dropping the run, so the SDK must soft-continue every steer
// while the injector is installed.
func TestInjectorTriggeredAndEmptyInjectorSoftContinues(t *testing.T) {
	c := newInjectorGateCompleter(
		[]provider.Response{
			{Message: textMessage(provider.RoleAssistant, "ok")},
		},
		0,
	)
	loop := newInjectorLoop(t, c, 5)

	inj := &injectorFixture{}
	// Iter-1 top: empty. Iter-1 steered-stop downgrade: empty.
	// Iter-2 top: empty. The empty-downgrade path still continues.
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
	if res.Stop == agentloop.StopSteered {
		t.Fatalf("Stop = StopSteered, want soft-continue: an installed injector opts the run into the soft-continue shape even when the drain is empty")
	}
	if res.Stop != agentloop.StopNoToolCalls {
		t.Fatalf("Stop = %q, want StopNoToolCalls", res.Stop)
	}
	if res.Iterations < 1 {
		t.Fatalf("Iterations = %d, want >=1", res.Iterations)
	}
}
