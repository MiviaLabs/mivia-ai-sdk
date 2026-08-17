package events_test

import (
	"context"
	"errors"
	"reflect"
	"testing"

	"github.com/MiviaLabs/mivia-ai-sdk/events"
)

// TestSubscribeThenEmit proves one subscribed handler runs per event.
func TestSubscribeThenEmit(t *testing.T) {
	b := events.New()
	var got string
	if err := b.Subscribe("move", func(_ context.Context, e events.Event) error {
		got = e.Data
		return nil
	}); err != nil {
		t.Fatalf("Subscribe: %v", err)
	}
	err := b.Emit(context.Background(), events.Event{Name: "move", Data: "a to b"})
	if err != nil {
		t.Fatalf("Emit: %v", err)
	}
	if got != "a to b" {
		t.Fatalf("handler saw Data = %q, want %q", got, "a to b")
	}
}

// TestHandlersRunInOrder proves handlers for one event run in order.
func TestHandlersRunInOrder(t *testing.T) {
	b := events.New()
	var order []int
	for i := 0; i < 3; i++ {
		n := i
		if err := b.Subscribe("move", func(context.Context, events.Event) error {
			order = append(order, n)
			return nil
		}); err != nil {
			t.Fatalf("Subscribe(%d): %v", n, err)
		}
	}
	if err := b.Emit(context.Background(), events.Event{Name: "move", Data: "x"}); err != nil {
		t.Fatalf("Emit: %v", err)
	}
	want := []int{0, 1, 2}
	if !reflect.DeepEqual(order, want) {
		t.Fatalf("handler order = %v, want %v", order, want)
	}
}

// TestHandlerErrorDoesNotStopLaterHandlers proves one failing handler
// does not stop Emit and does not propagate to the caller.
func TestHandlerErrorDoesNotStopLaterHandlers(t *testing.T) {
	b := events.New()
	var ran []string
	appendHandler := func(tag string) {
		if err := b.Subscribe("move", func(_ context.Context, _ events.Event) error {
			ran = append(ran, tag)
			return nil
		}); err != nil {
			t.Fatalf("Subscribe(%s): %v", tag, err)
		}
	}
	appendHandler("first")
	appendHandler("broken")
	appendHandler("last")
	if err := b.Subscribe("move", func(context.Context, events.Event) error {
		return errors.New("boom")
	}); err != nil {
		t.Fatalf("Subscribe(broken): %v", err)
	}
	appendHandler("last2")

	err := b.Emit(context.Background(), events.Event{Name: "move", Data: "x"})
	if err != nil {
		t.Fatalf("Emit propagated a handler error: %v", err)
	}
	want := []string{"first", "broken", "last", "last2"}
	if !reflect.DeepEqual(ran, want) {
		t.Fatalf("ran = %v, want %v", ran, want)
	}
}

// TestHandlerSubscribeDispatchesOnInnerState proves a handler that calls
// Subscribe sees the new subscription on the next Emit, not the current one.
func TestHandlerSubscribeDispatchesOnInnerState(t *testing.T) {
	b := events.New()
	var innerRan []string
	if err := b.Subscribe("move", func(ctx context.Context, e events.Event) error {
		if err := b.Subscribe("inner", func(context.Context, events.Event) error {
			innerRan = append(innerRan, "inner")
			return nil
		}); err != nil {
			return err
		}
		return nil
	}); err != nil {
		t.Fatalf("Subscribe(move): %v", err)
	}

	// The move handler adds "inner". The current emit already copied the
	// move slice, so "inner" must not run during it.
	if err := b.Emit(context.Background(), events.Event{Name: "move", Data: "x"}); err != nil {
		t.Fatalf("first Emit: %v", err)
	}
	if len(innerRan) != 0 {
		t.Fatalf("inner handler ran during the first Emit: %v", innerRan)
	}

	// The next emit dispatches on the updated bus state.
	if err := b.Emit(context.Background(), events.Event{Name: "inner", Data: "y"}); err != nil {
		t.Fatalf("second Emit: %v", err)
	}
	want := []string{"inner"}
	if !reflect.DeepEqual(innerRan, want) {
		t.Fatalf("inner ran = %v, want %v", innerRan, want)
	}
}
