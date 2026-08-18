package subagent_test

import (
	"errors"
	"fmt"
	"sync"
	"testing"

	"github.com/MiviaLabs/mivia-ai-sdk/envelope"
	"github.com/MiviaLabs/mivia-ai-sdk/subagent"
)

// TestMetamorphicMailboxFullRejectsInInsertionOrder pins the
// property: a mailbox at capacity rejects new sends in insertion
// order. Confirmed true against mailbox.go: Deliver appends only when
// len(m.msgs) < m.cap, and Take returns m.msgs unmodified otherwise.
func TestMetamorphicMailboxFullRejectsInInsertionOrder(t *testing.T) {
	cases := []struct {
		name     string
		capacity int
		overflow int
	}{
		{name: "capacity one, one overflow", capacity: 1, overflow: 1},
		{name: "capacity two, one overflow", capacity: 2, overflow: 1},
		{name: "capacity two, two overflow", capacity: 2, overflow: 2},
		{name: "capacity three, two overflow", capacity: 3, overflow: 2},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			box, err := subagent.NewMailbox(tc.capacity)
			if err != nil {
				t.Fatalf("NewMailbox: %v", err)
			}

			sent := make([]string, tc.capacity)
			for i := 0; i < tc.capacity; i++ {
				payload := fmt.Sprintf("in-order-%d", i)
				sent[i] = payload
				if err := box.Deliver(signedMessage(t, payload)); err != nil {
					t.Fatalf("Deliver %d: %v", i, err)
				}
			}

			for i := 0; i < tc.overflow; i++ {
				payload := fmt.Sprintf("overflow-%d", i)
				if err := box.Deliver(signedMessage(t, payload)); !errors.Is(err, subagent.ErrMailboxFull) {
					t.Fatalf("overflow Deliver %d: got %v, want ErrMailboxFull", i, err)
				}
			}

			drained := box.Take()
			if len(drained) != tc.capacity {
				t.Fatalf("Take = %d msgs, want %d", len(drained), tc.capacity)
			}
			for i, m := range drained {
				if m.Payload != sent[i] {
					t.Fatalf("drained[%d] = %q, want %q", i, m.Payload, sent[i])
				}
			}
		})
	}
}

// TestMetamorphicMailboxDeliveredMessageNeverDropped pins the
// property: a delivered message is never silently dropped, even
// under concurrent delivery within the capacity bound. Confirmed true
// against mailbox.go: Deliver and Take both hold m.mu for their full
// body, so no interleaving drops an appended message.
func TestMetamorphicMailboxDeliveredMessageNeverDropped(t *testing.T) {
	cases := []int{2, 8, 32}

	for _, capacity := range cases {
		t.Run(fmt.Sprintf("capacity %d", capacity), func(t *testing.T) {
			box, err := subagent.NewMailbox(capacity)
			if err != nil {
				t.Fatalf("NewMailbox: %v", err)
			}

			msgs := make([]string, capacity)
			for i := range msgs {
				msgs[i] = fmt.Sprintf("concurrent-%d", i)
			}
			// Build every signed message before spawning goroutines: the
			// test-failure path inside signedMessage may only run on the
			// main test goroutine, so goroutines below only call Deliver.
			built := make([]envelope.Message, capacity)
			for i, p := range msgs {
				built[i] = signedMessage(t, p)
			}

			start := make(chan struct{})
			var wg sync.WaitGroup
			errs := make([]error, capacity)
			for i := 0; i < capacity; i++ {
				wg.Add(1)
				go func(i int) {
					defer wg.Done()
					<-start
					errs[i] = box.Deliver(built[i])
				}(i)
			}
			close(start)
			wg.Wait()

			for i, err := range errs {
				if err != nil {
					t.Fatalf("Deliver %d: %v", i, err)
				}
			}

			drained := box.Take()
			if len(drained) != capacity {
				t.Fatalf("Take = %d msgs, want %d", len(drained), capacity)
			}
			want := make(map[string]bool, capacity)
			for _, p := range msgs {
				want[p] = true
			}
			got := make(map[string]bool, capacity)
			for _, m := range drained {
				got[m.Payload] = true
			}
			if len(got) != capacity {
				t.Fatalf("drained set has %d distinct payloads, want %d (duplicate detected)", len(got), capacity)
			}
			for p := range want {
				if !got[p] {
					t.Fatalf("payload %q was delivered but never drained", p)
				}
			}
		})
	}
}
