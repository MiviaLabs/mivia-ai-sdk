package taskrun

import (
	"context"
	"errors"
	"time"

	"github.com/MiviaLabs/mivia-ai-sdk/ledger"
)

// Options carries the ledger handle and the ceremony identity for one
// Run call. Every field is required except Now.
type Options struct {
	Ledger *ledger.Ledger
	Actor  ledger.Actor
	Owner  ledger.OwnerID
	Lease  time.Duration
	Now    func() time.Time // defaults to time.Now
}

// Task names one idempotent submission for Run to admit and run.
type Task struct {
	Key         ledger.IdempotencyKey
	Seq         ledger.Sequence
	Description string
	Needs       []ledger.IdempotencyKey
}

// Sentinel errors returned by Run during validation and replay.
var (
	// ErrNoLedger reports a nil Options.Ledger.
	ErrNoLedger = errors.New("taskrun: ledger is required")
	// ErrNoOwner reports an empty Options.Owner.
	ErrNoOwner = errors.New("taskrun: owner is required")
	// ErrNoActor reports an empty Options.Actor.
	ErrNoActor = errors.New("taskrun: actor is required")
	// ErrNoLease reports a non-positive Options.Lease.
	ErrNoLease = errors.New("taskrun: lease must be positive")
	// ErrNoKey reports an empty Task.Key.
	ErrNoKey = errors.New("taskrun: task key is required")
	// ErrTaskDone reports a key already completed in the ledger.
	ErrTaskDone = errors.New("taskrun: task already completed")
	// ErrTaskFailed reports a key already failed in the ledger.
	ErrTaskFailed = errors.New("taskrun: task already failed")
	// ErrTaskBlocked reports a key already blocked in the ledger.
	ErrTaskBlocked = errors.New("taskrun: task blocked on a failed dependency")
)

// Run admits, claims, and completes one task around work. The returned
// error is the work's own error, unwrapped, when work ran. A task
// already terminal in the ledger returns its sentinel without running
// work. A Claim blocked by a live lease returns an error satisfying
// errors.Is(err, ledger.ErrLeaseActive). A Complete failure joins the
// returned error; the work result still leads.
func Run(ctx context.Context, opts Options, t Task, work func(context.Context) error) error {
	if opts.Ledger == nil {
		return ErrNoLedger
	}
	if opts.Owner == "" {
		return ErrNoOwner
	}
	if opts.Actor == "" {
		return ErrNoActor
	}
	if opts.Lease <= 0 {
		return ErrNoLease
	}
	if t.Key == "" {
		return ErrNoKey
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
		case ledger.StatusCompleted:
			return ErrTaskDone
		case ledger.StatusFailed:
			return ErrTaskFailed
		case ledger.StatusBlocked:
			return ErrTaskBlocked
		}
	}
	fence, err := opts.Ledger.Claim(ctx, opts.Actor, t.Key, opts.Owner, opts.Lease, nowFn())
	if err != nil {
		return err
	}
	bodyErr := work(ctx)
	status := ledger.StatusCompleted
	if bodyErr != nil {
		status = ledger.StatusFailed
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
