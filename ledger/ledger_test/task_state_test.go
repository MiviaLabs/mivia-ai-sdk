package ledger_test

import (
	"testing"

	"github.com/MiviaLabs/mivia-ai-sdk/ledger"
	"github.com/MiviaLabs/mivia-ai-sdk/machine"
)

// TestTaskStateValidate is table-driven over every rule Validate
// claims to enforce.
func TestTaskStateValidate(t *testing.T) {
	cases := []struct {
		name    string
		state   ledger.TaskState
		wantErr bool
	}{
		{
			name:    "valid pending",
			state:   ledger.TaskState{Key: "k1", Status: ledger.StatusPending},
			wantErr: false,
		},
		{
			name:  "valid claimed",
			state: ledger.TaskState{Key: "k1", Status: ledger.StatusClaimed, Owner: "owner-a", LeaseUntil: fixedNow},
		},
		{
			name:  "valid blocked",
			state: ledger.TaskState{Key: "k1", Status: ledger.StatusBlocked, BlockedBy: "root"},
		},
		{
			name:    "empty key",
			state:   ledger.TaskState{Status: ledger.StatusPending},
			wantErr: true,
		},
		{
			name:    "unknown status",
			state:   ledger.TaskState{Key: "k1", Status: machine.Status("bogus")},
			wantErr: true,
		},
		{
			name:    "needs names itself",
			state:   ledger.TaskState{Key: "k1", Status: ledger.StatusPending, Needs: []ledger.IdempotencyKey{"k1"}},
			wantErr: true,
		},
		{
			name:    "blocked with no BlockedBy",
			state:   ledger.TaskState{Key: "k1", Status: ledger.StatusBlocked},
			wantErr: true,
		},
		{
			name:    "non-blocked with BlockedBy set",
			state:   ledger.TaskState{Key: "k1", Status: ledger.StatusPending, BlockedBy: "root"},
			wantErr: true,
		},
		{
			name:    "claimed with no owner",
			state:   ledger.TaskState{Key: "k1", Status: ledger.StatusClaimed, LeaseUntil: fixedNow},
			wantErr: true,
		},
		{
			name:    "claimed with zero LeaseUntil",
			state:   ledger.TaskState{Key: "k1", Status: ledger.StatusClaimed, Owner: "owner-a"},
			wantErr: true,
		},
		{
			name:  "zero-value audit fields accepted",
			state: ledger.TaskState{Key: "k1", Status: ledger.StatusPending},
		},
		{
			name: "set audit fields accepted",
			state: ledger.TaskState{
				Key: "k1", Status: ledger.StatusPending,
				CreatedBy: testActor, CreatedAt: fixedNow,
				UpdatedBy: testActor, UpdatedAt: fixedNow,
			},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := tc.state.Validate()
			if tc.wantErr && err == nil {
				t.Fatalf("Validate: want error, got nil")
			}
			if !tc.wantErr && err != nil {
				t.Fatalf("Validate: want nil, got %v", err)
			}
		})
	}
}

// TestTaskStateValidateCompletedNeedsNoOwner proves a StatusCompleted
// record needs no Owner or LeaseUntil, unlike StatusClaimed.
func TestTaskStateValidateCompletedNeedsNoOwner(t *testing.T) {
	st := ledger.TaskState{Key: "k1", Status: ledger.StatusCompleted}
	if err := st.Validate(); err != nil {
		t.Fatalf("Validate: %v", err)
	}
}
