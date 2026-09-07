package agentloop

import "testing"

// Pairing tests for the ack generation counter. The steered-stop
// downgrade acks the trigger the loop observed. An ack must never
// clear a newer Trigger that fired after that observation. The
// pairing lives inside Steer, so these tests drive the methods
// sequentially with no goroutines. See docs/history/agentloop.md,
// the steer ack generation counter addendum.

// TestSteerAckSparesUnobservedTrigger is the killing test. A
// Trigger fired after the observation must survive the ack.
func TestSteerAckSparesUnobservedTrigger(t *testing.T) {
	s := NewSteer()
	s.Trigger()
	if !s.wasTriggered() {
		t.Fatal("wasTriggered = false, want true after Trigger")
	}
	s.Trigger()
	s.ackTriggered()
	if !s.wasTriggered() {
		t.Fatal("wasTriggered = false after ack, want true: the ack must spare the unobserved trigger")
	}
}

// TestSteerAckClearsObservedTrigger pins the happy path. Observe
// then ack with no newer trigger clears the flag once.
func TestSteerAckClearsObservedTrigger(t *testing.T) {
	s := NewSteer()
	s.Trigger()
	if !s.wasTriggered() {
		t.Fatal("wasTriggered = false, want true after Trigger")
	}
	s.ackTriggered()
	if s.wasTriggered() {
		t.Fatal("wasTriggered = true after ack, want false: the ack must clear the observed trigger")
	}
}

// TestSteerAckWithoutObserveClearsNothing pins the safe default. An
// ack with no prior observation never clears the flag. The read
// after the ack is the test's first observation.
func TestSteerAckWithoutObserveClearsNothing(t *testing.T) {
	s := NewSteer()
	s.Trigger()
	s.ackTriggered()
	if !s.wasTriggered() {
		t.Fatal("wasTriggered = false after unobserved ack, want true")
	}
}

// TestSteerDoubleTriggerSingleGenerationAck covers a double Trigger
// before the observe. One observe of the later generation lets one
// ack consume both; an unobserved second trigger survives the ack.
func TestSteerDoubleTriggerSingleGenerationAck(t *testing.T) {
	s := NewSteer()
	s.Trigger()
	s.Trigger()
	if !s.wasTriggered() {
		t.Fatal("wasTriggered = false, want true after double Trigger")
	}
	s.ackTriggered()
	if s.wasTriggered() {
		t.Fatal("wasTriggered = true after ack, want false: the observe saw the later generation")
	}

	late := NewSteer()
	late.Trigger()
	if !late.wasTriggered() {
		t.Fatal("wasTriggered = false, want true after Trigger")
	}
	late.Trigger()
	late.ackTriggered()
	if !late.wasTriggered() {
		t.Fatal("wasTriggered = false after ack, want true: the second Trigger fired after the observe")
	}
}

// TestSteerTriggerAfterResetNewGeneration covers reset zeroing the
// counters, then a fresh observe-and-ack cycle on a new generation.
func TestSteerTriggerAfterResetNewGeneration(t *testing.T) {
	s := NewSteer()
	s.Trigger()
	if !s.wasTriggered() {
		t.Fatal("wasTriggered = false, want true after Trigger")
	}
	s.reset()
	if s.wasTriggered() {
		t.Fatal("wasTriggered = true after reset, want false")
	}
	s.Trigger()
	if !s.wasTriggered() {
		t.Fatal("wasTriggered = false, want true after post-reset Trigger")
	}
	s.ackTriggered()
	if s.wasTriggered() {
		t.Fatal("wasTriggered = true after post-reset ack, want false")
	}
}
