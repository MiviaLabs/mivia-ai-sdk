package ledger_test

import (
	"context"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/MiviaLabs/mivia-ai-sdk/events"
	"github.com/MiviaLabs/mivia-ai-sdk/ledger"
)

// TestAdmitRaceExactlyOneWinner races N goroutines calling Admit on
// the same key and the same sequence number. Exactly one goroutine's
// Admit returns true, nil and every other returns false, nil.
// AdmittedEvent fires exactly once. Run under go test -race.
func TestAdmitRaceExactlyOneWinner(t *testing.T) {
	const n = 20
	ctx := context.Background()
	bus := events.New()
	var fired int64
	if err := bus.Subscribe(ledger.AdmittedEvent, func(_ context.Context, _ events.Event) error {
		atomic.AddInt64(&fired, 1)
		return nil
	}); err != nil {
		t.Fatalf("Subscribe: %v", err)
	}
	l := newLedger(t, bus)

	var wg sync.WaitGroup
	oks := make([]bool, n)
	errsFound := int64(0)
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			ok, err := l.Admit(ctx, "k1", 1, nil)
			if err != nil {
				atomic.AddInt64(&errsFound, 1)
				return
			}
			oks[i] = ok
		}(i)
	}
	wg.Wait()

	if errsFound != 0 {
		t.Fatalf("Admit returned %d unexpected errors", errsFound)
	}
	successes := 0
	for _, ok := range oks {
		if ok {
			successes++
		}
	}
	if successes != 1 {
		t.Fatalf("successes = %d, want 1", successes)
	}
	if atomic.LoadInt64(&fired) != 1 {
		t.Fatalf("AdmittedEvent fired %d times, want 1", fired)
	}
}
