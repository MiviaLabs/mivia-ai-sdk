package events_test

import (
	"context"
	"fmt"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/MiviaLabs/mivia-ai-sdk/events"
)

const (
	goroutines        = 24
	emitsPerGoroutine = 40
	postEmits         = 100
	postData          = "post"
)

// busCounters holds the delivery counts for one concurrent run.
// Index g holds the counts for goroutine g. The error counters count
// unexpected Subscribe and Emit failures.
type busCounters struct {
	shared   [goroutines]atomic.Int64
	own      [goroutines]atomic.Int64
	post     [goroutines]atomic.Int64
	subErrs  atomic.Int64
	emitErrs atomic.Int64
}

// TestBusConcurrentSubscribeEmit proves the subscription set stays
// consistent under concurrent Subscribe and Emit calls.
// One bus serves N goroutines. Each goroutine subscribes a handler to
// the shared name and to a private name, then it emits to both names.
// All goroutines overlap in time. Atomic counters prove each emitted
// event is delivered exactly once to each subscribed handler. The
// race detector guards the mutex invariant. The watchdog bound turns
// a regression deadlock into a bounded test failure.
func TestBusConcurrentSubscribeEmit(t *testing.T) {
	b := events.New()
	ctx := context.Background()
	var counts busCounters

	start := make(chan struct{})
	var wg sync.WaitGroup
	for g := 0; g < goroutines; g++ {
		ownName := fmt.Sprintf("own-%d", g)
		wg.Add(1)
		go func(g int, ownName string) {
			defer wg.Done()
			<-start
			runEmitter(b, ctx, &counts, g, ownName)
		}(g, ownName)
	}
	close(start)

	done := make(chan struct{})
	go func() {
		wg.Wait()
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(10 * time.Second):
		t.Fatal("concurrent Subscribe and Emit did not finish. The bus likely deadlocked.")
	}

	// All subscriptions completed. Emit again to prove exact delivery
	// to every handler of the shared name.
	for i := 0; i < postEmits; i++ {
		if err := b.Emit(ctx, events.Event{Name: "shared", Data: postData}); err != nil {
			t.Fatalf("post Emit: %v", err)
		}
	}

	totalShared := goroutines * emitsPerGoroutine
	for g := 0; g < goroutines; g++ {
		if got := counts.own[g].Load(); got != emitsPerGoroutine {
			t.Fatalf("handler %d received %d private events, want %d", g, got, emitsPerGoroutine)
		}
		if got := counts.post[g].Load(); got != postEmits {
			t.Fatalf("handler %d received %d post events, want %d", g, got, postEmits)
		}
		if got := counts.shared[g].Load(); got < emitsPerGoroutine || got > int64(totalShared) {
			t.Fatalf("handler %d received %d shared events, want %d to %d", g, got, emitsPerGoroutine, totalShared)
		}
	}
	if n := counts.subErrs.Load(); n != 0 {
		t.Fatalf("%d reentrant Subscribes failed", n)
	}
	if n := counts.emitErrs.Load(); n != 0 {
		t.Fatalf("%d Emit calls failed", n)
	}
}

// runEmitter subscribes two handlers, then it emits to both names.
// One handler listens to the shared name. The other handler listens
// to the private name of its goroutine. Each handler counts its
// deliveries and subscribes a noop handler. The noop Subscribe proves
// a handler may call Subscribe while other goroutines emit. A
// regression that runs handlers under the lock deadlocks here. The
// test watchdog turns that deadlock into a bounded failure.
func runEmitter(b *events.Bus, ctx context.Context, counts *busCounters, g int, ownName string) {
	noop := func(context.Context, events.Event) error { return nil }
	reentrant := func() {
		if err := b.Subscribe("noop", noop); err != nil {
			counts.subErrs.Add(1)
		}
	}
	if err := b.Subscribe("shared", func(_ context.Context, e events.Event) error {
		if e.Data == postData {
			counts.post[g].Add(1)
		} else {
			counts.shared[g].Add(1)
		}
		reentrant()
		return nil
	}); err != nil {
		panic("subscribe shared: " + err.Error())
	}
	if err := b.Subscribe(ownName, func(context.Context, events.Event) error {
		counts.own[g].Add(1)
		reentrant()
		return nil
	}); err != nil {
		panic("subscribe own: " + err.Error())
	}
	for j := 0; j < emitsPerGoroutine; j++ {
		if err := b.Emit(ctx, events.Event{Name: "shared", Data: "x"}); err != nil {
			counts.emitErrs.Add(1)
		}
		if err := b.Emit(ctx, events.Event{Name: ownName, Data: "x"}); err != nil {
			counts.emitErrs.Add(1)
		}
	}
}
