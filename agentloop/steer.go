package agentloop

import (
	"context"
	"sync"
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
// retry has no effect until the following iteration boundary, for the
// same reason. Calling Trigger more than once, or with no
// RunSteerable call in progress, has no additional effect.
func (s *Steer) Trigger() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.triggered = true
	if s.cancel != nil {
		s.cancel()
	}
}

// reset clears triggered and cancel back to their zero values. Called
// only at the start of a RunSteerable call.
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
