package agentloop

import (
	"context"
	"sync"

	"github.com/MiviaLabs/mivia-ai-sdk/provider"
)

// Steer is a caller-held handle that requests a soft-cancel of one
// RunSteerable call's in-flight Completer.Chat call. Trigger is safe
// to call from another goroutine, any number of times, before,
// during, or after the RunSteerable call it is passed to. A Steer
// triggered before RunSteerable starts, or after it already returned,
// is a no-op: RunSteerable resets Steer's internal state at the start
// of its own call. One Steer value must not be passed to two
// concurrent RunSteerable calls: both calls would arm and disarm the
// same triggered flag and cancel func, and one caller's Trigger could
// stop the other caller's unrelated run.
type Steer struct {
	mu        sync.Mutex
	triggered bool
	cancel    context.CancelFunc
	// injector is the caller-supplied pull-based message source the
	// loop consults at every iteration boundary and at every steered
	// stop decision. A nil injector means no injection (the default);
	// a non-nil injector returning an empty slice at a particular
	// boundary means "no messages this time". The injector runs on
	// the loop goroutine: the caller must not assume concurrency,
	// but must not assume a particular goroutine either (RunSteerable
	// may run on a fresh goroutine the caller spawned). SetInjector
	// is called only before RunSteerable; reset() preserves the
	// injector across calls so the caller can install it once and
	// reuse the Steer across multiple RunSteerable calls.
	injector func() []provider.Message
}

// NewSteer returns a ready Steer, unbound to any RunSteerable call
// until passed to one.
func NewSteer() *Steer {
	return &Steer{}
}

// Trigger requests the soft-cancel this Steer is bound to for its
// current RunSteerable call, if any. Trigger fired mid-tool-call-batch
// has no effect on the calls already dispatched in that batch; it
// takes effect at the start of the next iteration's Completer.Chat
// call instead. A Trigger call fired during a prompt-too-long recovery
// retry has no effect until the following iteration boundary, for
// the same reason. Calling Trigger more than once, or with no
// RunSteerable call in progress, has no additional effect.
func (s *Steer) Trigger() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.triggered = true
	if s.cancel != nil {
		s.cancel()
	}
}

// SetInjector installs f as the pull-based message source the loop
// consults at every iteration boundary and at every steered-stop
// decision. A non-nil return appends those messages to the run
// history and the run CONTINUES (a pending StopSteered is downgraded
// in that case). An empty return keeps existing Trigger semantics:
// the run still stops at the next iteration boundary with
// Stop == StopSteered. Passing nil removes the injector.
//
// SetInjector is meant to be called BEFORE RunSteerable starts.
// Once the run is in flight, SetInjector's effect on the next
// boundary is not formally defined by the doc comment; the
// implementation does not synchronize against reset(), but the loop
// never holds the mutex during an iteration body, so a SetInjector
// call between boundaries is observed at the next one. Calling
// SetInjector while the loop is inside an iteration body is
// therefore a race that may drop or apply the change on the next
// boundary depending on goroutine interleaving; this is the
// caller's responsibility to avoid, and the loop does not protect
// against it. The recommended pattern is: SetInjector once after
// NewSteer and before the first RunSteerable call, then never again.
//
// The injector f runs on the loop goroutine; it must not block
// indefinitely or call back into the loop.
func (s *Steer) SetInjector(f func() []provider.Message) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.injector = f
}

// drainInjected returns the injector's current messages, or nil when
// no injector is installed. Each call invokes the injector once.
// The loop calls drainInjected at the top of every iteration and at
// every steered-stop downgrade point. The caller is responsible for
// appending the returned slice to history and observing its
// emptiness to decide between continuing and stopping.
func (s *Steer) drainInjected() []provider.Message {
	s.mu.Lock()
	f := s.injector
	s.mu.Unlock()
	if f == nil {
		return nil
	}
	return f()
}

// hasInjector reports whether an injector has been installed via
// SetInjector. The steered-stop branch uses this to decide between
// the soft-continue path (an injector is installed) and the
// existing single-shot StopSteered path (no injector; pre-injector
// SDK contract).
func (s *Steer) hasInjector() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.injector != nil
}

// HasActiveCall reports whether a Completer.Chat call is currently
// in flight on this Steer (i.e. arm has bound a cancel func that
// disarm has not yet cleared). Continuous-bridge triggers fire on
// every poll tick; a trigger fired when no call is in flight is a
// no-op for the in-flight cancel but still sets the trigger flag
// for the next arm to observe. The next arm then immediately
// cancels that chat, then the bridge fires again, and the run
// never makes progress. Bridges that want to honor "fire only when
// there is a chat to cancel" can guard each Trigger call on this
// method, eliminating the no-op triggers that nevertheless poison
// the next chat's arm.
func (s *Steer) HasActiveCall() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.cancel != nil
}

// ackTriggered clears the triggered flag. Used by the steered-stop
// downgrade point AFTER a non-empty injector delivers messages that
// the loop has appended to history: the next iteration's Chat call
// must NOT arm a still-triggered Steer, or the post-injection Chat
// call would cancel instantly, the next drainInjected would return
// empty, and the run would stop with zero Final — the opposite of
// the intended continue-after-inject. Without this explicit
// acknowledgment at the downgrade point, the sticky triggered flag
// is exactly what breaks the continue-after-inject shape; reset()
// clears it only at the start of the next RunSteerable call, which
// is too late.
func (s *Steer) ackTriggered() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.triggered = false
}

// reset clears triggered and cancel back to their zero values, but
// preserves the installed injector so a caller that wires
// SetInjector once can reuse the Steer across multiple
// RunSteerable calls. Called only at the start of a RunSteerable
// call.
func (s *Steer) reset() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.triggered = false
	s.cancel = nil
}

// arm binds cancel as the func Trigger calls next, and reports
// whether triggered was already true. Called only by runChat, once
// per Completer.Chat call.
func (s *Steer) arm(cancel context.CancelFunc) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.cancel = cancel
	return s.triggered
}

// disarm clears the bound cancel func without touching triggered.
// Called only by runChat, after its Completer.Chat call returns.
func (s *Steer) disarm() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.cancel = nil
}

// wasTriggered reports whether Trigger has fired since the last
// reset. isSteerStop is the only caller.
func (s *Steer) wasTriggered() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.triggered
}
