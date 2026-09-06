package ledger_test

import (
	"context"
	"strings"
	"testing"

	"github.com/MiviaLabs/mivia-ai-sdk/ledger"
)

// TestAdmitRejectsSelfNeed proves Admit validates the record before
// it writes. A task naming itself in Needs is the record
// TaskState.Validate rejects, so Admit returns false and that error
// and stores nothing.
func TestAdmitRejectsSelfNeed(t *testing.T) {
	ctx := context.Background()
	l := newLedger(t, nil)

	ok, err := l.Admit(ctx, testActor, "k", 1, nil, fixedNow, "k")
	if err == nil {
		t.Fatalf("Admit self-need: want error, got nil")
	}
	if ok {
		t.Fatalf("Admit self-need = true, want false")
	}
	if !strings.Contains(err.Error(), "names itself in Needs") {
		t.Fatalf("Admit self-need error = %v, want the self-need rule", err)
	}
	if _, found, err := l.State(ctx, "k"); err != nil || found {
		t.Fatalf("State(k) = found %v, err %v; want not found, nil", found, err)
	}
}

// TestAdmitSnapshotRoundTrips proves a record Admit accepts survives
// Snapshot, Encode, Decode, and Restore. It does not prove the
// property for a record Claim or Takeover writes; those paths still
// write without validating.
func TestAdmitSnapshotRoundTrips(t *testing.T) {
	ctx := context.Background()
	l := newLedger(t, nil)
	mustAdmit(t, l, ctx, "root", 1)
	mustAdmit(t, l, ctx, "dep", 1, "root")

	snap := mustSnapshot(t, l, ctx)
	if len(snap.Tasks) != 2 {
		t.Fatalf("Snapshot holds %d tasks, want 2", len(snap.Tasks))
	}
	if err := snap.Validate(); err != nil {
		t.Fatalf("Snapshot.Validate: %v", err)
	}
	data, err := snap.Encode()
	if err != nil {
		t.Fatalf("Encode: %v", err)
	}
	back, err := ledger.Decode(data)
	if err != nil {
		t.Fatalf("Decode: %v", err)
	}

	restored := newLedger(t, nil)
	if err := restored.Restore(ctx, back); err != nil {
		t.Fatalf("Restore: %v", err)
	}
	assertStatus(t, restored, ctx, "root", ledger.StatusPending)
	assertStatus(t, restored, ctx, "dep", ledger.StatusPending)
}
