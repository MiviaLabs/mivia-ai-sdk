package ledger

import (
	"context"
	"errors"
	"fmt"
	"time"
)

// Options carries the ledger handle and the ceremony identity for one
// Run call. Every field is required except Now.
type Options struct {
	Ledger *Ledger
	Actor  Actor
	Owner  OwnerID
	Lease  time.Duration
	Now    func() time.Time // defaults to time.Now
}

// Task names one idempotent submission for Run to admit and run.
type Task struct {
	Key         IdempotencyKey
	Seq         Sequence
	Description string
	Needs       []IdempotencyKey
}

// Sentinel errors returned by Run during validation and replay.
var (
	// ErrNoLedger reports a nil Options.Ledger.
	ErrNoLedger = errors.New("ledger: ledger is required")
	// ErrTaskDone reports a key already completed in the ledger.
	ErrTaskDone = errors.New("ledger: task already completed")
	// ErrTaskFailed reports a key already failed in the ledger.
	ErrTaskFailed = errors.New("ledger: task already failed")
	// ErrTaskBlocked reports a key already blocked in the ledger.
	ErrTaskBlocked = errors.New("ledger: task blocked on a failed dependency")
)

// Run admits, claims, and completes one task around work. The returned
// error is the work's own error, unwrapped, when work ran. A task
// already terminal in the ledger returns its sentinel without running
// work. A Claim blocked by a live lease returns an error satisfying
// errors.Is(err, ErrLeaseActive). A Complete failure joins the
// returned error; the work result still leads.
//
// A bounded Store adds one more outcome: it can delete the record
// between Admit and Claim, or after the lease expired while work
// still runs, so Run returns an error satisfying errors.Is(err,
// ErrNoKey). Do not confuse that sentinel with the ErrInvalidOptions
// error for an empty Task.Key, a caller error caught before any Store
// call.
func Run(ctx context.Context, opts Options, t Task, work func(context.Context) error) error {
	if opts.Ledger == nil {
		return ErrNoLedger
	}
	if opts.Owner == "" {
		return fmt.Errorf("%w: %s", ErrInvalidOptions, "Owner: is required")
	}
	if opts.Actor == "" {
		return fmt.Errorf("%w: %s", ErrInvalidOptions, "Actor: is required")
	}
	if opts.Lease <= 0 {
		return fmt.Errorf("%w: %s", ErrInvalidOptions, "Lease: must be positive")
	}
	if t.Key == "" {
		return fmt.Errorf("%w: %s", ErrInvalidOptions, "Key: task key is required")
	}
	nowFn := opts.Now
	if nowFn == nil {
		nowFn = time.Now
	}
	// A duplicate admit is not an error; the terminal check finds it.
	if _, err := opts.Ledger.Admit(ctx, opts.Actor, t.Key, t.Seq, t.Description, nowFn(), t.Needs...); err != nil {
		return err
	}
	st, found, err := opts.Ledger.State(ctx, t.Key)
	if err != nil {
		return err
	}
	if found {
		switch st.Status {
		case StatusCompleted:
			return ErrTaskDone
		case StatusFailed:
			return ErrTaskFailed
		case StatusBlocked:
			return ErrTaskBlocked
		}
	}
	fence, err := opts.Ledger.Claim(ctx, opts.Actor, t.Key, opts.Owner, opts.Lease, nowFn())
	if err != nil {
		return err
	}
	bodyErr := work(ctx)
	status := StatusCompleted
	if bodyErr != nil {
		status = StatusFailed
	}
	completeErr := opts.Ledger.Complete(ctx, opts.Actor, t.Key, opts.Owner, fence, status, nowFn())
	if bodyErr != nil {
		if completeErr != nil {
			return errors.Join(bodyErr, completeErr)
		}
		return bodyErr
	}
	return completeErr
}
