package ledger_test

import (
	"context"
	"testing"

	"github.com/MiviaLabs/mivia-ai-sdk/ledger"
)

// TestSnapshotListsEveryRecord proves Snapshot after several
// admissions lists every record.
func TestSnapshotListsEveryRecord(t *testing.T) {
	ctx := context.Background()
	l := newLedger(t, nil)
	mustAdmit(t, l, ctx, "a", 1)
	mustAdmit(t, l, ctx, "b", 1)
	mustAdmit(t, l, ctx, "c", 1)

	snap, err := l.Snapshot(ctx)
	if err != nil {
		t.Fatalf("Snapshot: %v", err)
	}
	if len(snap.Tasks) != 3 {
		t.Fatalf("Tasks len = %d, want 3", len(snap.Tasks))
	}
	seen := map[ledger.IdempotencyKey]bool{}
	for _, task := range snap.Tasks {
		seen[task.Key] = true
	}
	for _, key := range []ledger.IdempotencyKey{"a", "b", "c"} {
		if !seen[key] {
			t.Fatalf("Snapshot missing key %q", key)
		}
	}
}

// TestEncodeDecodeRoundTrips proves Encode then Decode round-trips
// every field.
func TestEncodeDecodeRoundTrips(t *testing.T) {
	ctx := context.Background()
	l := newLedger(t, nil)
	mustAdmit(t, l, ctx, "root", 1)
	mustAdmit(t, l, ctx, "dep", 1, "root")
	mustClaim(t, l, ctx, "dep", "owner-a")

	snap, err := l.Snapshot(ctx)
	if err != nil {
		t.Fatalf("Snapshot: %v", err)
	}
	data, err := snap.Encode()
	if err != nil {
		t.Fatalf("Encode: %v", err)
	}
	decoded, err := ledger.Decode(data)
	if err != nil {
		t.Fatalf("Decode: %v", err)
	}
	if len(decoded.Tasks) != len(snap.Tasks) {
		t.Fatalf("Tasks len = %d, want %d", len(decoded.Tasks), len(snap.Tasks))
	}
	byKey := map[ledger.IdempotencyKey]ledger.TaskState{}
	for _, task := range decoded.Tasks {
		byKey[task.Key] = task
	}
	dep, found := byKey["dep"]
	if !found {
		t.Fatalf("decoded snapshot missing key dep")
	}
	if dep.Status != ledger.StatusClaimed {
		t.Fatalf("dep.Status = %q, want StatusClaimed", dep.Status)
	}
	if dep.Owner != "owner-a" {
		t.Fatalf("dep.Owner = %q, want owner-a", dep.Owner)
	}
	if len(dep.Needs) != 1 || dep.Needs[0] != "root" {
		t.Fatalf("dep.Needs = %v, want [root]", dep.Needs)
	}
	if !dep.LeaseUntil.Equal(fixedNow.Add(fixedLease)) {
		t.Fatalf("dep.LeaseUntil = %v, want %v", dep.LeaseUntil, fixedNow.Add(fixedLease))
	}
	if dep.CreatedBy != testActor || !dep.CreatedAt.Equal(fixedNow) {
		t.Fatalf("dep CreatedBy/CreatedAt = %q/%v, want %q/%v", dep.CreatedBy, dep.CreatedAt, testActor, fixedNow)
	}
	if dep.UpdatedBy != testActor || !dep.UpdatedAt.Equal(fixedNow) {
		t.Fatalf("dep UpdatedBy/UpdatedAt = %q/%v, want %q/%v", dep.UpdatedBy, dep.UpdatedAt, testActor, fixedNow)
	}
}

// TestDecodeRejectsMalformedInput proves Decode rejects malformed
// JSON and an out-of-range Status.
func TestDecodeRejectsMalformedInput(t *testing.T) {
	cases := []struct {
		name string
		data string
	}{
		{"malformed json", `{"Tasks":[`},
		{"out of range status", `{"Tasks":[{"Key":"k1","Status":"nonsense"}]}`},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := ledger.Decode([]byte(tc.data)); err == nil {
				t.Fatalf("Decode(%s): want error", tc.name)
			}
		})
	}
}

// TestRestoreReproducesPriorState proves Restore on a fresh Ledger
// reproduces every prior State lookup result.
func TestRestoreReproducesPriorState(t *testing.T) {
	ctx := context.Background()
	l := newLedger(t, nil)
	mustAdmit(t, l, ctx, "root", 1)
	mustAdmit(t, l, ctx, "dep", 1, "root")
	fence := mustClaim(t, l, ctx, "root", "owner-a")
	if err := l.Complete(ctx, testActor, "root", "owner-a", fence, ledger.StatusFailed, fixedNow); err != nil {
		t.Fatalf("Complete: %v", err)
	}

	snap, err := l.Snapshot(ctx)
	if err != nil {
		t.Fatalf("Snapshot: %v", err)
	}
	fresh := newLedger(t, nil)
	if err := fresh.Restore(ctx, snap); err != nil {
		t.Fatalf("Restore: %v", err)
	}
	for _, key := range []ledger.IdempotencyKey{"root", "dep"} {
		want, _, err := l.State(ctx, key)
		if err != nil {
			t.Fatalf("State(%s) on original: %v", key, err)
		}
		got, found, err := fresh.State(ctx, key)
		if err != nil {
			t.Fatalf("State(%s) on restored: %v", key, err)
		}
		if !found {
			t.Fatalf("restored ledger missing key %q", key)
		}
		if got.Status != want.Status || got.BlockedBy != want.BlockedBy {
			t.Fatalf("restored state for %q = %+v, want %+v", key, got, want)
		}
	}
}
