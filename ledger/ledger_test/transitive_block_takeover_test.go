package ledger_test

import (
	"context"
	"errors"
	"testing"

	"github.com/MiviaLabs/mivia-ai-sdk/ledger"
	"github.com/MiviaLabs/mivia-ai-sdk/machine"
)

// takeoverShapeCase is one row of
// TestTakeoverRejectsBlockingAncestorShapes. before builds the graph
// and claims the target while its blocking ancestor is still absent;
// after lets the blocking ancestor appear, mirroring the two-phase
// order TestTakeoverRejectsBlockingAncestor already uses.
type takeoverShapeCase struct {
	name          string
	claim         ledger.IdempotencyKey
	before        func(t *testing.T, l *ledger.Ledger, ctx context.Context)
	after         func(t *testing.T, l *ledger.Ledger, ctx context.Context)
	wantErr       error
	wantStatus    machine.Status
	wantBlockedBy ledger.IdempotencyKey
}

// takeoverShapeCases returns the row set
// TestTakeoverRejectsBlockingAncestorShapes drives: the diamond,
// cycle, self-need, and completed-sibling shapes
// TestClaimRejectsBlockingAncestor already proves against Claim,
// driven through Takeover instead. It lives beside the test rather
// than inside it, so the table can grow without pushing the test
// function past the length limit, matching ancestorCases.
func takeoverShapeCases() []takeoverShapeCase {
	noop := func(t *testing.T, l *ledger.Ledger, ctx context.Context) {}
	return []takeoverShapeCase{
		{
			name:  "diamond with two paths to one leaf",
			claim: "L",
			before: func(t *testing.T, l *ledger.Ledger, ctx context.Context) {
				mustAdmit(t, l, ctx, "L", 1, "X", "Y")
				mustAdmit(t, l, ctx, "X", 1, "B")
				mustAdmit(t, l, ctx, "Y", 1, "B")
			},
			after: func(t *testing.T, l *ledger.Ledger, ctx context.Context) {
				buildTerminal(t, l, ctx, "A", ledger.StatusFailed)
				mustAdmit(t, l, ctx, "B", 1, "A")
			},
			wantErr:       ledger.ErrNotClaimed,
			wantStatus:    ledger.StatusBlocked,
			wantBlockedBy: "B",
		},
		{
			name:  "two-node cycle with no failure terminates",
			claim: "P",
			before: func(t *testing.T, l *ledger.Ledger, ctx context.Context) {
				mustAdmit(t, l, ctx, "P", 1, "Q")
				mustAdmit(t, l, ctx, "Q", 1, "P")
			},
			after:      noop,
			wantStatus: ledger.StatusClaimed,
		},
		{
			name:  "self-need with no failure terminates",
			claim: "S",
			before: func(t *testing.T, l *ledger.Ledger, ctx context.Context) {
				mustAdmit(t, l, ctx, "S", 1, "S")
			},
			after:      noop,
			wantStatus: ledger.StatusClaimed,
		},
		{
			name:  "completed sibling beside a blocked need",
			claim: "L",
			before: func(t *testing.T, l *ledger.Ledger, ctx context.Context) {
				mustAdmit(t, l, ctx, "L", 1, "Sib", "B")
				buildTerminal(t, l, ctx, "Sib", ledger.StatusCompleted)
			},
			after: func(t *testing.T, l *ledger.Ledger, ctx context.Context) {
				buildTerminal(t, l, ctx, "A", ledger.StatusFailed)
				mustAdmit(t, l, ctx, "B", 1, "A")
			},
			wantErr:       ledger.ErrNotClaimed,
			wantStatus:    ledger.StatusBlocked,
			wantBlockedBy: "B",
		},
	}
}

// TestTakeoverRejectsBlockingAncestorShapes drives Takeover over
// every shape takeoverShapeCases builds. TestTakeoverRejectsBlocking-
// Ancestor pins the linear-chain shape alone; this proves Takeover's
// blockingAncestor call also dedups a diamond, terminates on a cycle
// and a self-need, and does not block on a completed sibling, the
// same four properties TestClaimRejectsBlockingAncestor already pins
// for Claim.
func TestTakeoverRejectsBlockingAncestorShapes(t *testing.T) {
	stale := fixedNow.Add(fixedLease)
	for _, tc := range takeoverShapeCases() {
		t.Run(tc.name, func(t *testing.T) {
			ctx := context.Background()
			l := newLedger(t, nil)
			tc.before(t, l, ctx)
			mustClaim(t, l, ctx, tc.claim, "owner-claim")
			tc.after(t, l, ctx)

			_, err := l.Takeover(ctx, testActor, tc.claim, "owner-takeover", fixedLease, stale)
			if tc.wantErr == nil {
				if err != nil {
					t.Fatalf("Takeover(%s) = %v, want nil", tc.claim, err)
				}
			} else if !errors.Is(err, tc.wantErr) {
				t.Fatalf("Takeover(%s) = %v, want %v", tc.claim, err, tc.wantErr)
			}
			assertRecord(t, l, ctx, tc.claim, tc.wantStatus, tc.wantBlockedBy)
		})
	}
}
