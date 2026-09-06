package events_test

import (
	"context"
	"testing"

	"github.com/MiviaLabs/mivia-ai-sdk/events"
)

// TestSubscribeRejectsEmptyName proves Subscribe rejects an empty name.
func TestSubscribeRejectsEmptyName(t *testing.T) {
	b := events.New()
	err := b.Subscribe("", func(context.Context, events.Event) error { return nil })
	if err == nil {
		t.Fatal("Subscribe accepted an empty name")
	}
}

// TestSubscribeRejectsNilHandler proves Subscribe rejects a nil handler.
func TestSubscribeRejectsNilHandler(t *testing.T) {
	b := events.New()
	err := b.Subscribe("move", nil)
	if err == nil {
		t.Fatal("Subscribe accepted a nil handler")
	}
}

// TestValidateRejectsEmptyName proves Event.Validate rejects an empty Name.
func TestValidateRejectsEmptyName(t *testing.T) {
	if err := (events.Event{Data: "x"}).Validate(); err == nil {
		t.Fatal("Validate accepted an empty Name")
	}
}

// TestValidateRejectsEmptyData proves Event.Validate rejects an empty Data.
func TestValidateRejectsEmptyData(t *testing.T) {
	if err := (events.Event{Name: "move"}).Validate(); err == nil {
		t.Fatal("Validate accepted an empty Data")
	}
}

// TestValidateAcceptsValid proves Event.Validate accepts a full event.
func TestValidateAcceptsValid(t *testing.T) {
	if err := (events.Event{Name: "move", Data: "x"}).Validate(); err != nil {
		t.Fatalf("Validate rejected a valid event: %v", err)
	}
}

// TestEmitRejectsInvalidEvent proves Emit propagates a validation error.
func TestEmitRejectsInvalidEvent(t *testing.T) {
	b := events.New()
	if err := b.Subscribe("move", func(context.Context, events.Event) error { return nil }); err != nil {
		t.Fatalf("Subscribe: %v", err)
	}
	if err := b.Emit(context.Background(), events.Event{Name: "move"}); err == nil {
		t.Fatal("Emit accepted an event with an empty Data")
	}
}

// TestEmitAcceptsUnknownName proves Emit returns nil for an
// unsubscribed event name. An unobserved event is a no-op, not a
// failure.
func TestEmitAcceptsUnknownName(t *testing.T) {
	b := events.New()
	if err := b.Subscribe("move", func(context.Context, events.Event) error { return nil }); err != nil {
		t.Fatalf("Subscribe: %v", err)
	}
	if err := b.Emit(context.Background(), events.Event{Name: "unknown", Data: "x"}); err != nil {
		t.Fatalf("Emit rejected an unsubscribed name: %v", err)
	}
}

// TestEmitWithNoSubscriberReturnsNil proves Emit on a bus with zero
// subscribers returns nil.
func TestEmitWithNoSubscriberReturnsNil(t *testing.T) {
	b := events.New()
	if err := b.Emit(context.Background(), events.Event{Name: "move", Data: "x"}); err != nil {
		t.Fatalf("Emit on a bus with zero subscribers: %v, want nil", err)
	}
}

// TestZeroValueBusSubscribesAndEmits proves the zero value is usable.
// Subscribe on the zero value builds the subscription set, so a later
// Emit runs the handler once. Emit on an untouched zero value finds no
// subscriber and returns nil.
func TestZeroValueBusSubscribesAndEmits(t *testing.T) {
	var b events.Bus
	var runs int
	if err := b.Subscribe("move", func(context.Context, events.Event) error {
		runs++
		return nil
	}); err != nil {
		t.Fatalf("zero-value Subscribe: %v", err)
	}
	if err := b.Emit(context.Background(), events.Event{Name: "move", Data: "x"}); err != nil {
		t.Fatalf("zero-value Emit: %v", err)
	}
	if runs != 1 {
		t.Fatalf("handler ran %d times, want 1", runs)
	}
	var c events.Bus
	if err := c.Emit(context.Background(), events.Event{Name: "move", Data: "x"}); err != nil {
		t.Fatalf("zero-value Emit returned an error with no subscriber: %v", err)
	}
}
