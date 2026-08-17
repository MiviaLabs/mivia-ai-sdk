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

// TestEmitRejectsUnknownName proves Emit returns an error for an
// unsubscribed event name.
func TestEmitRejectsUnknownName(t *testing.T) {
	b := events.New()
	if err := b.Subscribe("move", func(context.Context, events.Event) error { return nil }); err != nil {
		t.Fatalf("Subscribe: %v", err)
	}
	err := b.Emit(context.Background(), events.Event{Name: "unknown", Data: "x"})
	if err == nil {
		t.Fatal("Emit accepted an unknown name")
	}
}

// TestZeroValueBusPinsConstructorOnly proves the zero value is unusable.
// Subscribe on the zero value panics on the nil subscription map.
// Emit on the zero value returns a normal no-subscriber error.
// The invariant is constructor-only; New is the only sanctioned build.
func TestZeroValueBusPinsConstructorOnly(t *testing.T) {
	var b events.Bus
	func() {
		defer func() {
			if r := recover(); r == nil {
				t.Fatal("zero-value Subscribe did not panic")
			}
		}()
		_ = b.Subscribe("move", func(context.Context, events.Event) error { return nil })
	}()
	var c events.Bus
	err := c.Emit(context.Background(), events.Event{Name: "move", Data: "x"})
	if err == nil {
		t.Fatal("zero-value Emit accepted an event")
	}
}
