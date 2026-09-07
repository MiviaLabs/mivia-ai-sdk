package agentloop_test

// Loop-level regression rows for the steer ack generation counter.
// The window between the downgrade ack and the next arm is reachable
// at the iteration-top injector drain, so the fixtures use channel
// barriers there. No sleeps. See docs/history/agentloop.md, the steer
// ack generation counter addendum.

import (
	"context"
	"sync"
	"testing"

	"github.com/MiviaLabs/mivia-ai-sdk/agentloop"
	"github.com/MiviaLabs/mivia-ai-sdk/provider"
)

// gateDrainInjector blocks the Nth drain call on a channel, then
// returns a settable payload. It gives the test a deterministic
// barrier on the loop goroutine between two loop events.
type gateDrainInjector struct {
	mu       sync.Mutex
	calls    int
	gateAt   int // 1-based drain call to block on
	release  chan struct{}
	gateOpen bool // set when the gated drain is in flight
	payload  []provider.Message
}

func newGateDrainInjector(gateAt int) *gateDrainInjector {
	return &gateDrainInjector{gateAt: gateAt, release: make(chan struct{})}
}

func (g *gateDrainInjector) drain() []provider.Message {
	g.mu.Lock()
	g.calls++
	inGate := g.calls == g.gateAt
	if inGate {
		g.gateOpen = true
	}
	g.mu.Unlock()
	if !inGate {
		return nil
	}
	<-g.release
	g.mu.Lock()
	g.gateOpen = false
	out := g.payload
	g.mu.Unlock()
	return out
}

func (g *gateDrainInjector) setPayload(msgs []provider.Message) {
	g.mu.Lock()
	defer g.mu.Unlock()
	g.payload = msgs
}

func (g *gateDrainInjector) inGate() bool {
	g.mu.Lock()
	defer g.mu.Unlock()
	return g.gateOpen
}

// scriptGateCompleter scripts the Chat calls. Calls below blockUpTo
// block on ctx.Done() and return the context error, so an
// already-canceled context returns instantly. Calls at or above
// blockUpTo return the scripted response for their index.
type scriptGateCompleter struct {
	mu        sync.Mutex
	calls     int
	blockUpTo int
	responses []provider.Response
	entered   []chan struct{}
}

func newScriptGateCompleter(blockUpTo int, responses []provider.Response) *scriptGateCompleter {
	c := &scriptGateCompleter{blockUpTo: blockUpTo, responses: responses}
	for range responses {
		c.entered = append(c.entered, make(chan struct{}))
	}
	return c
}

func (c *scriptGateCompleter) Chat(ctx context.Context, req provider.Request) (provider.Response, error) {
	c.mu.Lock()
	idx := c.calls
	c.calls++
	var entered chan struct{}
	if idx < len(c.entered) {
		entered = c.entered[idx]
	}
	c.mu.Unlock()
	if entered != nil {
		close(entered)
	}
	if idx < c.blockUpTo {
		<-ctx.Done()
		return provider.Response{}, ctx.Err()
	}
	return c.responses[idx], nil
}

func (c *scriptGateCompleter) Name() string { return "script-gate" }

func (c *scriptGateCompleter) ChatStream(ctx context.Context, req provider.Request) (<-chan provider.Chunk, error) {
	return nil, nil
}

func (c *scriptGateCompleter) waitEntered(t *testing.T, idx int) {
	t.Helper()
	c.mu.Lock()
	ch := c.entered[idx]
	c.mu.Unlock()
	<-ch
}

// TestInjectorAckSparesTriggerBeforeNextArm drives the reachable
// side of the ack window: a Trigger raised at the injector barrier
// between the first ack and the second arm. The second Chat cancels
// instantly, the run reports that steered stop, the ack consumes
// the observed generation, and the third Chat completes clean.
func TestInjectorAckSparesTriggerBeforeNextArm(t *testing.T) {
	c := newScriptGateCompleter(2, []provider.Response{
		{Message: textMessage(provider.RoleAssistant, "skipped")},
		{Message: textMessage(provider.RoleAssistant, "skipped")},
		{Message: textMessage(provider.RoleAssistant, "done")},
	})
	loop := newInjectorLoop(t, c, 5)

	// The injector gates on its second drain call: the iteration-top
	// drain of the continuation after the first steered-stop ack.
	inj := newGateDrainInjector(2)
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

	c.waitEntered(t, 0)
	steer.Trigger()
	// The gated drain is the barrier between the first ack and the
	// second arm. Wait for it, then raise the trigger before release.
	for !inj.inGate() {
	}
	steer.Trigger()
	close(inj.release)

	res, err := <-resCh, <-errCh
	if err != nil {
		t.Fatalf("RunSteerable error: %v", err)
	}
	if res.Stop != agentloop.StopNoToolCalls {
		t.Fatalf("Stop = %q, want StopNoToolCalls: the late steer must be honored, then consumed, then the run converges", res.Stop)
	}
	if res.Iterations != 1 {
		t.Fatalf("Iterations = %d, want 1: only the third Chat completes", res.Iterations)
	}
}

// TestInjectorTriggerDuringDrainIsHonored is a regression row. The
// injector blocks at the iteration-top drain; the test triggers;
// the release delivers a payload; the next Chat cancels instantly.
// The trigger fires outside the ack window, so old and new code
// must both honor it.
func TestInjectorTriggerDuringDrainIsHonored(t *testing.T) {
	c := newScriptGateCompleter(1, []provider.Response{
		{Message: textMessage(provider.RoleAssistant, "skipped")},
		{Message: textMessage(provider.RoleAssistant, "done")},
	})
	loop := newInjectorLoop(t, c, 5)

	inj := newGateDrainInjector(1)
	inj.setPayload([]provider.Message{textMessage(provider.RoleUser, "injected")})
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

	for !inj.inGate() {
	}
	steer.Trigger()
	close(inj.release)

	res, err := <-resCh, <-errCh
	if err != nil {
		t.Fatalf("RunSteerable error: %v", err)
	}
	if res.Stop != agentloop.StopNoToolCalls {
		t.Fatalf("Stop = %q, want StopNoToolCalls: the trigger during the drain must be honored, consumed, and the run converge", res.Stop)
	}
}
