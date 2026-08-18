package ledger_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/MiviaLabs/mivia-ai-sdk/ledger"
)

// lateFailureStore builds a ledger and fails the key "dep" before the
// dependent is admitted, so the dependent arrives after the failure.
func lateFailureFixture(t *testing.T) *ledger.Ledger {
	t.Helper()
	l, err := ledger.New(nil, nil)
	if err != nil {
		t.Fatalf("ledger.New: %v", err)
	}
	ctx := context.Background()
	now := time.Now()
	if _, err := l.Admit(ctx, "actor", "dep", 1, nil, now); err != nil {
		t.Fatalf("Admit dep: %v", err)
	}
	fence, err := l.Claim(ctx, "actor", "dep", "owner", time.Minute, now)
	if err != nil {
		t.Fatalf("Claim dep: %v", err)
	}
	if err := l.Complete(ctx, "actor", "dep", "owner", fence, ledger.StatusFailed, now); err != nil {
		t.Fatalf("Complete dep: %v", err)
	}
	return l
}

// TestAdmitAfterNeedFailedBlocks proves a dependent admitted after its
// need failed lands StatusBlocked naming the need, not StatusPending.
func TestAdmitAfterNeedFailedBlocks(t *testing.T) {
	ctx := context.Background()
	l := lateFailureFixture(t)
	if _, err := l.Admit(ctx, "actor", "child", 1, nil, time.Now(), "dep"); err != nil {
		t.Fatalf("Admit child: %v", err)
	}
	st, found, err := l.State(ctx, "child")
	if err != nil || !found {
		t.Fatalf("State child: %v,%v", found, err)
	}
	if st.Status != ledger.StatusBlocked {
		t.Fatalf("status = %q, want %q", st.Status, ledger.StatusBlocked)
	}
	if st.BlockedBy != "dep" {
		t.Fatalf("blockedBy = %q, want %q", st.BlockedBy, "dep")
	}
	// A blocked record never claims: the ceremony stops at the gate.
	if _, err := l.Claim(ctx, "actor", "child", "owner", time.Minute, time.Now()); !errors.Is(err, ledger.ErrNotClaimed) {
		t.Fatalf("Claim on blocked record = %v, want ErrNotClaimed", err)
	}
}

// TestAdmitAfterNeedBlockedBlocks proves a dependent whose need is
// itself blocked also blocks, naming the direct need.
func TestAdmitAfterNeedBlockedBlocks(t *testing.T) {
	ctx := context.Background()
	l := lateFailureFixture(t)
	if _, err := l.Admit(ctx, "actor", "mid", 1, nil, time.Now(), "dep"); err != nil {
		t.Fatalf("Admit mid: %v", err)
	}
	if _, err := l.Admit(ctx, "actor", "leaf", 1, nil, time.Now(), "mid"); err != nil {
		t.Fatalf("Admit leaf: %v", err)
	}
	st, _, err := l.State(ctx, "leaf")
	if err != nil {
		t.Fatalf("State leaf: %v", err)
	}
	if st.Status != ledger.StatusBlocked || st.BlockedBy != "mid" {
		t.Fatalf("leaf = %q blockedBy %q, want blocked on mid", st.Status, st.BlockedBy)
	}
}

// TestAdmitWithMixedNeedStatusesBlocksOnFailed proves a satisfied need
// does not mask a failed one at admission time.
func TestAdmitWithMixedNeedStatusesBlocksOnFailed(t *testing.T) {
	ctx := context.Background()
	l, err := ledger.New(nil, nil)
	if err != nil {
		t.Fatalf("ledger.New: %v", err)
	}
	now := time.Now()
	for _, key := range []ledger.IdempotencyKey{"ok", "bad"} {
		if _, err := l.Admit(ctx, "actor", key, 1, nil, now); err != nil {
			t.Fatalf("Admit %s: %v", key, err)
		}
	}
	fence, err := l.Claim(ctx, "actor", "ok", "owner", time.Minute, now)
	if err != nil {
		t.Fatalf("Claim ok: %v", err)
	}
	if err := l.Complete(ctx, "actor", "ok", "owner", fence, ledger.StatusCompleted, now); err != nil {
		t.Fatalf("Complete ok: %v", err)
	}
	fence, err = l.Claim(ctx, "actor", "bad", "owner", time.Minute, now)
	if err != nil {
		t.Fatalf("Claim bad: %v", err)
	}
	if err := l.Complete(ctx, "actor", "bad", "owner", fence, ledger.StatusFailed, now); err != nil {
		t.Fatalf("Complete bad: %v", err)
	}
	if _, err := l.Admit(ctx, "actor", "child", 1, nil, now, "ok", "bad"); err != nil {
		t.Fatalf("Admit child: %v", err)
	}
	st, _, err := l.State(ctx, "child")
	if err != nil {
		t.Fatalf("State child: %v", err)
	}
	if st.Status != ledger.StatusBlocked || st.BlockedBy != "bad" {
		t.Fatalf("child = %q blockedBy %q, want blocked on bad", st.Status, st.BlockedBy)
	}
}

// TestAdmitNeedsStillPendingAdmitsPending proves a live need does not
// block admission; the ordinary pending path is unchanged.
func TestAdmitNeedsStillPendingAdmitsPending(t *testing.T) {
	ctx := context.Background()
	l, err := ledger.New(nil, nil)
	if err != nil {
		t.Fatalf("ledger.New: %v", err)
	}
	now := time.Now()
	if _, err := l.Admit(ctx, "actor", "dep", 1, nil, now); err != nil {
		t.Fatalf("Admit dep: %v", err)
	}
	admitted, err := l.Admit(ctx, "actor", "child", 1, nil, now, "dep")
	if err != nil {
		t.Fatalf("Admit child: %v", err)
	}
	if !admitted {
		t.Fatal("Admit reported no admission for a fresh dependent")
	}
	st, _, err := l.State(ctx, "child")
	if err != nil {
		t.Fatalf("State child: %v", err)
	}
	if st.Status != ledger.StatusPending || st.BlockedBy != "" {
		t.Fatalf("child = %q,%q want pending with no blocker", st.Status, st.BlockedBy)
	}
}
