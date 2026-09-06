package ledger_test

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/MiviaLabs/mivia-ai-sdk/ledger"
	"github.com/MiviaLabs/mivia-ai-sdk/machine"
)

// plantedLease is the LeaseUntil every planted invalid record carries.
var plantedLease = time.Unix(100, 0)

// leaseCase is one row of TestLeaseMustBePositive. claimFirst claims
// k1 under fixedLease, so Renew and Takeover meet a StatusClaimed
// record holding fence 1.
type leaseCase struct {
	name       string
	lease      time.Duration
	claimFirst bool
	call       func(ctx context.Context, l *ledger.Ledger, lease time.Duration) error
	wantStatus machine.Status
	wantOwner  ledger.OwnerID
	wantFence  ledger.FenceToken
	wantLease  time.Time
}

// callClaim, callRenew, and callTakeover adapt the three lease-writing
// methods to one signature, so the table drives all three.
func callClaim(ctx context.Context, l *ledger.Ledger, lease time.Duration) error {
	_, err := l.Claim(ctx, testActor, "k1", "owner-b", lease, fixedNow)
	return err
}

func callRenew(ctx context.Context, l *ledger.Ledger, lease time.Duration) error {
	return l.Renew(ctx, testActor, "k1", "owner-a", 1, lease, fixedNow)
}

func callTakeover(ctx context.Context, l *ledger.Ledger, lease time.Duration) error {
	_, err := l.Takeover(ctx, testActor, "k1", "owner-b", lease, fixedNow.Add(fixedLease))
	return err
}

// leaseCases returns the row set TestLeaseMustBePositive drives: each
// of the three lease-writing methods at a zero lease and at a negative
// lease. It lives beside the test, so the table can grow without
// pushing the test function past the length limit.
func leaseCases() []leaseCase {
	rows := []leaseCase{}
	for _, lease := range []time.Duration{0, -time.Second} {
		rows = append(rows,
			leaseCase{
				name:       "Claim at lease " + lease.String(),
				lease:      lease,
				call:       callClaim,
				wantStatus: ledger.StatusPending,
			},
			leaseCase{
				name:       "Renew at lease " + lease.String(),
				lease:      lease,
				claimFirst: true,
				call:       callRenew,
				wantStatus: ledger.StatusClaimed,
				wantOwner:  "owner-a",
				wantFence:  1,
				wantLease:  fixedNow.Add(fixedLease),
			},
			leaseCase{
				name:       "Takeover at lease " + lease.String(),
				lease:      lease,
				claimFirst: true,
				call:       callTakeover,
				wantStatus: ledger.StatusClaimed,
				wantOwner:  "owner-a",
				wantFence:  1,
				wantLease:  fixedNow.Add(fixedLease),
			},
		)
	}
	return rows
}

// TestLeaseMustBePositive pins the one lease rule at all three call
// sites. Every row passes a non-empty owner, so ErrEmptyOwner never
// fires first, and the guard runs before Store.Load. Each row asserts
// the sentinel and that the stored record did not move.
func TestLeaseMustBePositive(t *testing.T) {
	for _, tc := range leaseCases() {
		t.Run(tc.name, func(t *testing.T) {
			ctx := context.Background()
			l := newLedger(t, nil)
			mustAdmit(t, l, ctx, "k1", 1)
			if tc.claimFirst {
				mustClaim(t, l, ctx, "k1", "owner-a")
			}
			err := tc.call(ctx, l, tc.lease)
			if !errors.Is(err, ledger.ErrInvalidLease) {
				t.Fatalf("call = %v, want ErrInvalidLease", err)
			}
			assertLeaseFields(t, l, ctx, "k1", tc.wantStatus, tc.wantOwner, tc.wantFence, tc.wantLease)
		})
	}
}

// assertLeaseFields fails the test unless key reads the four governed
// fields a lease write moves.
func assertLeaseFields(t *testing.T, l *ledger.Ledger, ctx context.Context, key ledger.IdempotencyKey, status machine.Status, owner ledger.OwnerID, fence ledger.FenceToken, lease time.Time) {
	t.Helper()
	st, found, err := l.State(ctx, key)
	if err != nil {
		t.Fatalf("State(%s): %v", key, err)
	}
	if !found {
		t.Fatalf("State(%s): want found", key)
	}
	if st.Status != status {
		t.Fatalf("%s.Status = %q, want %q", key, st.Status, status)
	}
	if st.Owner != owner {
		t.Fatalf("%s.Owner = %q, want %q", key, st.Owner, owner)
	}
	if st.Fence != fence {
		t.Fatalf("%s.Fence = %d, want %d", key, st.Fence, fence)
	}
	if !st.LeaseUntil.Equal(lease) {
		t.Fatalf("%s.LeaseUntil = %v, want %v", key, st.LeaseUntil, lease)
	}
}

// preEpoch is one hour before the Go zero instant, so a one-hour lease
// added to it lands exactly on the zero instant.
var preEpoch = time.Time{}.Add(-time.Hour)

// invalidRecordCase is one row of TestLeaseWriteRejectsInvalidRecord.
// Each row reaches a defect the lease argument check cannot see, so
// only the next.Validate call refuses it.
type invalidRecordCase struct {
	name       string
	setup      func(t *testing.T, l *ledger.Ledger, store ledger.Store, ctx context.Context)
	call       func(ctx context.Context, l *ledger.Ledger) error
	wantErrIn  string
	wantStatus machine.Status
	wantOwner  ledger.OwnerID
	wantFence  ledger.FenceToken
	wantLease  time.Time
	wantEncode bool
}

// plantBlockedBy writes a StatusClaimed record carrying BlockedBy
// straight through Store.CompareAndSwap, which runs no validation.
// TaskState.Validate rejects BlockedBy outside StatusBlocked, and
// next := cur carries the field forward into every lease write.
func plantBlockedBy(t *testing.T, store ledger.Store, ctx context.Context) {
	t.Helper()
	next := ledger.TaskState{
		Key:        "k1",
		Status:     ledger.StatusClaimed,
		Sequence:   1,
		Owner:      "o",
		Fence:      7,
		LeaseUntil: plantedLease,
		BlockedBy:  "x",
		CreatedBy:  testActor,
		CreatedAt:  fixedNow,
		UpdatedBy:  testActor,
		UpdatedAt:  fixedNow,
	}
	ok, err := store.CompareAndSwap(ctx, "k1", ledger.TaskState{}, next)
	if err != nil {
		t.Fatalf("plantBlockedBy: %v", err)
	}
	if !ok {
		t.Fatalf("plantBlockedBy: want true")
	}
}

// invalidRecordCases returns the row set
// TestLeaseWriteRejectsInvalidRecord drives. It lives beside the test,
// so the table can grow without pushing the test function past the
// length limit.
func invalidRecordCases() []invalidRecordCase {
	return []invalidRecordCase{
		{
			name: "Claim writes a zero LeaseUntil",
			setup: func(t *testing.T, l *ledger.Ledger, store ledger.Store, ctx context.Context) {
				ok, err := l.Admit(ctx, testActor, "k1", 1, nil, preEpoch)
				if err != nil || !ok {
					t.Fatalf("Admit = %v, %v, want true, nil", ok, err)
				}
			},
			call: func(ctx context.Context, l *ledger.Ledger) error {
				_, err := l.Claim(ctx, testActor, "k1", "owner-b", time.Hour, preEpoch)
				return err
			},
			wantErrIn:  "zero LeaseUntil",
			wantStatus: ledger.StatusPending,
			wantEncode: true,
		},
		{
			name: "Renew carries BlockedBy forward",
			setup: func(t *testing.T, l *ledger.Ledger, store ledger.Store, ctx context.Context) {
				plantBlockedBy(t, store, ctx)
			},
			call: func(ctx context.Context, l *ledger.Ledger) error {
				return l.Renew(ctx, testActor, "k1", "o", 7, time.Minute, time.Unix(200, 0))
			},
			wantErrIn:  "outside StatusBlocked",
			wantStatus: ledger.StatusClaimed,
			wantOwner:  "o",
			wantFence:  7,
			wantLease:  plantedLease,
		},
		{
			name: "Takeover carries BlockedBy forward",
			setup: func(t *testing.T, l *ledger.Ledger, store ledger.Store, ctx context.Context) {
				plantBlockedBy(t, store, ctx)
			},
			call: func(ctx context.Context, l *ledger.Ledger) error {
				_, err := l.Takeover(ctx, testActor, "k1", "thief", time.Minute, time.Unix(200, 0))
				return err
			},
			wantErrIn:  "outside StatusBlocked",
			wantStatus: ledger.StatusClaimed,
			wantOwner:  "o",
			wantFence:  7,
			wantLease:  plantedLease,
		},
	}
}

// TestLeaseWriteRejectsInvalidRecord pins the next.Validate call in
// Claim, Renew, and Takeover. Row one reaches a zero LeaseUntil the
// lease argument check cannot see. Rows two and three reach a
// BlockedBy field carried forward from the Store. Rows two and three
// assert no Encode result: the planted record is Encode-rejecting
// before the call runs, so that assertion carries no signal.
func TestLeaseWriteRejectsInvalidRecord(t *testing.T) {
	for _, tc := range invalidRecordCases() {
		t.Run(tc.name, func(t *testing.T) {
			ctx := context.Background()
			store := ledger.NewMemStore()
			l := newLedgerOverStore(t, store)
			tc.setup(t, l, store, ctx)
			err := tc.call(ctx, l)
			if err == nil {
				t.Fatalf("call = nil, want an error naming %q", tc.wantErrIn)
			}
			if !strings.Contains(err.Error(), tc.wantErrIn) {
				t.Fatalf("call = %v, want an error naming %q", err, tc.wantErrIn)
			}
			assertLeaseFields(t, l, ctx, "k1", tc.wantStatus, tc.wantOwner, tc.wantFence, tc.wantLease)
			if !tc.wantEncode {
				return
			}
			if _, err := mustSnapshot(t, l, ctx).Encode(); err != nil {
				t.Fatalf("Encode: %v", err)
			}
		})
	}
}
