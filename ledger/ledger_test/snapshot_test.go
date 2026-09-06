package ledger_test

import (
	"context"
	"strings"
	"testing"
	"time"

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

// claimedRecord snapshots one ledger holding a claimed record and
// returns that record for mutation in a test row.
func claimedRecord(t *testing.T, ctx context.Context, key ledger.IdempotencyKey) ledger.TaskState {
	t.Helper()
	l := newLedger(t, nil)
	mustAdmit(t, l, ctx, key, 1)
	mustClaim(t, l, ctx, key, "owner-a")
	snap, err := l.Snapshot(ctx)
	if err != nil {
		t.Fatalf("Snapshot: %v", err)
	}
	return snap.Tasks[0]
}

// TestRestoreRejectsInvalidRecord proves Restore validates each
// record at the insert boundary, so a hand-built snapshot cannot
// insert a record TaskState.Validate rejects.
func TestRestoreRejectsInvalidRecord(t *testing.T) {
	ctx := context.Background()

	t.Run("valid snapshot restores", func(t *testing.T) {
		l := newLedger(t, nil)
		mustAdmit(t, l, ctx, "root", 1)
		mustAdmit(t, l, ctx, "dep", 1, "root")
		mustClaim(t, l, ctx, "dep", "owner-a")
		snap, err := l.Snapshot(ctx)
		if err != nil {
			t.Fatalf("Snapshot: %v", err)
		}
		fresh := newLedger(t, nil)
		if err := fresh.Restore(ctx, snap); err != nil {
			t.Fatalf("Restore: %v", err)
		}
		for _, key := range []ledger.IdempotencyKey{"root", "dep"} {
			if _, found, err := fresh.State(ctx, key); err != nil || !found {
				t.Fatalf("State(%q) = found %v, err %v, want the restored record", key, found, err)
			}
		}
	})

	t.Run("claimed record with no owner", func(t *testing.T) {
		bad := claimedRecord(t, ctx, "no-owner")
		bad.Owner = ""
		err := newLedger(t, nil).Restore(ctx, ledger.Snapshot{Tasks: []ledger.TaskState{bad}})
		if err == nil {
			t.Fatal("Restore = nil, want the validation error")
		}
		if !strings.Contains(err.Error(), "ledger: restore: key") || !strings.Contains(err.Error(), "no-owner") {
			t.Fatalf("Restore error = %v, want the key-naming restore wrap", err)
		}
	})

	t.Run("claimed record with a zero lease", func(t *testing.T) {
		bad := claimedRecord(t, ctx, "zero-lease")
		bad.LeaseUntil = time.Time{}
		err := newLedger(t, nil).Restore(ctx, ledger.Snapshot{Tasks: []ledger.TaskState{bad}})
		if err == nil {
			t.Fatal("Restore = nil, want the validation error")
		}
		if !strings.Contains(err.Error(), "ledger: restore: key") || !strings.Contains(err.Error(), "zero-lease") {
			t.Fatalf("Restore error = %v, want the key-naming restore wrap", err)
		}
	})

	t.Run("mixed snapshot stops at the invalid record", func(t *testing.T) {
		valid := ledger.TaskState{}
		l := newLedger(t, nil)
		mustAdmit(t, l, ctx, "valid", 1)
		snap, err := l.Snapshot(ctx)
		if err != nil {
			t.Fatalf("Snapshot: %v", err)
		}
		valid = snap.Tasks[0]
		bad := claimedRecord(t, ctx, "invalid")
		bad.Owner = ""
		fresh := newLedger(t, nil)
		err = fresh.Restore(ctx, ledger.Snapshot{Tasks: []ledger.TaskState{valid, bad}})
		if err == nil {
			t.Fatal("Restore = nil, want the validation error")
		}
		if !strings.Contains(err.Error(), "invalid") {
			t.Fatalf("Restore error = %v, want the invalid key named", err)
		}
		if _, found, err := fresh.State(ctx, "valid"); err != nil || !found {
			t.Fatalf("State(valid) = found %v, err %v, want the earlier insert kept", found, err)
		}
		if _, found, _ := fresh.State(ctx, "invalid"); found {
			t.Fatal("the invalid record entered the store")
		}
	})
}
