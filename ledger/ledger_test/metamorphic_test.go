package ledger_test

import (
	"context"
	"errors"
	"testing"

	"github.com/MiviaLabs/mivia-ai-sdk/ledger"
	"github.com/MiviaLabs/mivia-ai-sdk/machine"
)

// TestMetamorphicAdmitIdempotentSameKey pins the property: a second
// Admit at the same idempotency key and the same sequence is
// idempotent. It returns false, nil, runs no second unit of work, and
// State still reports the first marker. Scoped to the same-sequence
// replay case only; the lower-sequence resubmission case is already
// covered by TestAdmitDuplicateSequenceRejected. Confirmed true
// against ledger.go: admitEligible requires seq > cur.Sequence, so an
// equal sequence never reaches CompareAndSwap.
func TestMetamorphicAdmitIdempotentSameKey(t *testing.T) {
	cases := map[string]struct {
		key        ledger.IdempotencyKey
		firstTask  string
		secondTask string
	}{
		"string payload": {
			key:        "task-a",
			firstTask:  "first-marker",
			secondTask: "second-marker",
		},
		"different key namespace": {
			key:        "task-b-with-longer-name",
			firstTask:  "alpha",
			secondTask: "beta",
		},
	}

	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			ctx := context.Background()
			l := newLedger(t, nil)

			workCount := 0
			runWork := func(marker string) {
				workCount++
			}

			ok, err := l.Admit(ctx, testActor, tc.key, 1, tc.firstTask, fixedNow)
			if err != nil {
				t.Fatalf("first Admit: %v", err)
			}
			if !ok {
				t.Fatalf("first Admit: want true, got false")
			}
			runWork(tc.firstTask)

			ok, err = l.Admit(ctx, testActor, tc.key, 1, tc.secondTask, fixedNow)
			if err != nil {
				t.Fatalf("second Admit: %v", err)
			}
			if ok {
				t.Fatalf("second Admit at the same sequence: want false, got true")
			}
			runWorkIfAdmitted(ok, runWork, tc.secondTask)

			if workCount != 1 {
				t.Fatalf("work count = %d, want 1", workCount)
			}
			st, found, err := l.State(ctx, tc.key)
			if err != nil {
				t.Fatalf("State: %v", err)
			}
			if !found {
				t.Fatal("State: want found")
			}
			if st.Task != tc.firstTask {
				t.Fatalf("Task = %v, want first marker %v", st.Task, tc.firstTask)
			}
		})
	}
}

// runWorkIfAdmitted gates the "unit of work" the same way a caller
// would: only a successful Admit runs it.
func runWorkIfAdmitted(admitted bool, runWork func(string), marker string) {
	if admitted {
		runWork(marker)
	}
}

// TestMetamorphicFencedLoserFailsWinnerLands pins the property: a
// fenced-out loser's Complete fails with ErrFenced while the
// fence-taking winner's Complete lands. Table varies whether the
// takeover happens once or twice before completion, and whether the
// winner completes StatusCompleted or StatusFailed. Confirmed true
// against complete.go: Complete checks cur.Fence != fence before any
// CompareAndSwap.
func TestMetamorphicFencedLoserFailsWinnerLands(t *testing.T) {
	cases := []struct {
		name      string
		takeovers int
	}{
		{name: "single takeover completes", takeovers: 1},
		{name: "double takeover completes", takeovers: 2},
	}
	statuses := map[string]machine.Status{
		"completed": ledger.StatusCompleted,
		"failed":    ledger.StatusFailed,
	}

	for _, tc := range cases {
		for statusName, target := range statuses {
			t.Run(tc.name+" "+statusName, func(t *testing.T) {
				ctx := context.Background()
				l := newLedger(t, nil)
				key := ledger.IdempotencyKey("k1")

				mustAdmit(t, l, ctx, key, 1)
				originalFence := mustClaim(t, l, ctx, key, "owner-original")

				currentOwner := ledger.OwnerID("owner-original")
				currentFence := originalFence
				staleAt := fixedNow
				for i := 0; i < tc.takeovers; i++ {
					next := ledger.OwnerID("owner-taker")
					if i > 0 {
						next = ledger.OwnerID("owner-taker-2")
					}
					// Each takeover's now must land after the previous
					// claim's lease expires: staleAt tracks that boundary.
					staleAt = staleAt.Add(fixedLease + 1)
					fence, err := l.Takeover(ctx, testActor, key, next, fixedLease, staleAt)
					if err != nil {
						t.Fatalf("Takeover %d: %v", i, err)
					}
					currentOwner = next
					currentFence = fence
				}

				if err := l.Complete(ctx, testActor, key, "owner-original", originalFence, target, fixedNow); !errors.Is(err, ledger.ErrFenced) {
					t.Fatalf("stale-fence Complete: got %v, want ErrFenced", err)
				}

				if err := l.Complete(ctx, testActor, key, currentOwner, currentFence, target, fixedNow); err != nil {
					t.Fatalf("winner Complete: %v", err)
				}

				st, found, err := l.State(ctx, key)
				if err != nil {
					t.Fatalf("State: %v", err)
				}
				if !found {
					t.Fatal("State: want found")
				}
				if st.Status != target {
					t.Fatalf("Status = %v, want %v", st.Status, target)
				}
			})
		}
	}
}
