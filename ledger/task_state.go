package ledger

import (
	"fmt"
	"time"

	"github.com/MiviaLabs/mivia-ai-sdk/machine"
)

// IdempotencyKey is the caller-chosen key that dedupes a task across
// retries and duplicate submissions.
type IdempotencyKey string

// OwnerID is the caller-chosen identity of a claimant.
type OwnerID string

// Actor is the caller-chosen identity of whoever performs a write: an
// external user ID, an agent ID, or any other identifier the caller
// finds meaningful. Ledger does not validate its shape.
type Actor string

// Sequence is the watermark a caller assigns per submission. Admit
// rejects a sequence at or below the recorded one.
type Sequence uint64

// FenceToken is a monotonic counter Claim and Takeover return. Renew,
// Release, and Complete reject a stale token, so a dispossessed
// owner's late call never mutates the record.
type FenceToken uint64

// StatusPending marks a record admitted but not yet claimed.
const StatusPending machine.Status = "pending"

// StatusClaimed marks a record owned under a live or stale lease.
const StatusClaimed machine.Status = "claimed"

// StatusCompleted marks a record whose task finished successfully.
const StatusCompleted machine.Status = "completed"

// StatusFailed marks a record whose task finished unsuccessfully.
const StatusFailed machine.Status = "failed"

// StatusBlocked marks a record whose named dependency failed.
const StatusBlocked machine.Status = "blocked"

// TaskState is the full record for one idempotency key. Task is
// caller-owned, like machine.InOut.Input; ledger never inspects it.
// Rev is a Store-assigned revision counter; a Ledger method reads Rev
// off the loaded record and forwards it unchanged inside the old
// argument to Store.CompareAndSwap. Ledger never sets or interprets
// Rev itself.
type TaskState struct {
	Key        IdempotencyKey
	Status     machine.Status
	Sequence   Sequence
	Owner      OwnerID
	Fence      FenceToken
	LeaseUntil time.Time
	Needs      []IdempotencyKey
	BlockedBy  IdempotencyKey
	Task       any
	Rev        uint64
	CreatedBy  Actor
	CreatedAt  time.Time
	UpdatedBy  Actor
	UpdatedAt  time.Time
}

// isTerminalStatus reports whether s is a finished status: Admit
// never rebases a record at or past this point.
func isTerminalStatus(s machine.Status) bool {
	return s == StatusCompleted || s == StatusFailed || s == StatusBlocked
}

// isKnownStatus reports whether s is one of the five declared
// statuses.
func isKnownStatus(s machine.Status) bool {
	switch s {
	case StatusPending, StatusClaimed, StatusCompleted, StatusFailed, StatusBlocked:
		return true
	default:
		return false
	}
}

// Validate checks the field rules on a TaskState record. It rejects
// an empty Key, a Status outside the five declared constants, a Needs
// entry equal to Key, a non-empty BlockedBy when Status is not
// StatusBlocked, an empty BlockedBy when Status is StatusBlocked, and
// a StatusClaimed record with an empty Owner or a zero LeaseUntil.
func (s TaskState) Validate() error {
	if s.Key == "" {
		return fmt.Errorf("ledger: task key must not be empty")
	}
	if !isKnownStatus(s.Status) {
		return fmt.Errorf("ledger: task %q has an unknown status %q", s.Key, s.Status)
	}
	for _, n := range s.Needs {
		if n == s.Key {
			return fmt.Errorf("ledger: task %q names itself in Needs", s.Key)
		}
	}
	if s.Status == StatusBlocked && s.BlockedBy == "" {
		return fmt.Errorf("ledger: task %q is blocked but names no BlockedBy", s.Key)
	}
	if s.Status != StatusBlocked && s.BlockedBy != "" {
		return fmt.Errorf("ledger: task %q names BlockedBy %q outside StatusBlocked", s.Key, s.BlockedBy)
	}
	if s.Status == StatusClaimed {
		if s.Owner == "" {
			return fmt.Errorf("ledger: claimed task %q has no Owner", s.Key)
		}
		if s.LeaseUntil.IsZero() {
			return fmt.Errorf("ledger: claimed task %q has a zero LeaseUntil", s.Key)
		}
	}
	return nil
}
