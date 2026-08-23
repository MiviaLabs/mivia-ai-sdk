package agentloop

import (
	"context"
	"errors"
	"fmt"

	"github.com/MiviaLabs/mivia-ai-sdk/provider"
)

// ErrIncompleteWorkBudget is Options.Validate's error when WorkBudget
// is non-nil but either Reserve or Refund is missing. Test with
// errors.Is.
var ErrIncompleteWorkBudget = errors.New("agentloop: WorkBudget requires both Reserve and Refund")

// WorkBudget is a host-callable token-reservation surface the loop
// invokes around each Completer call. The SDK holds no budget policy
// of its own here: Reserve and Refund run host code (for example a
// shared token ceiling across concurrent subagent turns), and the
// loop only supplies the call points.
//
// The loop calls Reserve once per iteration, with the exact
// provider.Request it is about to send, BEFORE l.completer.Chat runs.
// A non-nil Reserve return hard-fails the run before the call, wrapped
// with the iteration count.
//
// The loop calls Refund after the call's outcome is known, with the
// same request: once with the zero provider.Usage when the call failed
// (the reservation was never consumed), or once with the response's
// real Usage when the call succeeded and reported a non-zero Usage. A
// successful call that reports the zero Usage gets NO Refund: the host
// keeps the reservation consumed, matching the legacy loop's
// consume-on-completion rule.
//
// Both functions must be safe for concurrent use when callers share
// one Loop across concurrent Run calls.
type WorkBudget struct {
	// Reserve runs before each Completer call with the exact request
	// about to be sent. A non-nil return fails the run.
	Reserve func(ctx context.Context, req provider.Request) error
	// Refund runs after the call outcome is known: zero Usage means
	// the call never consumed its reservation; non-zero Usage carries
	// the response's real billed usage.
	Refund func(ctx context.Context, req provider.Request, used provider.Usage)
}

// validate reports whether a WorkBudget is either nil (disabled) or
// fully wired.
func (b *WorkBudget) validate() error {
	if b == nil {
		return nil
	}
	if b.Reserve == nil || b.Refund == nil {
		return ErrIncompleteWorkBudget
	}
	return nil
}

// reserveWork runs the WorkBudget's Reserve for one iteration's
// request. A nil l.workBudget is a no-op; a hook error is wrapped with
// the 1-based iteration count so the hard fail names its cause.
func (l *Loop) reserveWork(ctx context.Context, req provider.Request, iteration int) error {
	if l.workBudget == nil {
		return nil
	}
	if err := l.workBudget.Reserve(ctx, req); err != nil {
		return fmt.Errorf("agentloop: iteration %d: work budget reserve: %w", iteration, err)
	}
	return nil
}

// refundWork runs the WorkBudget's Refund for a reservation the call
// never consumed: the Completer call failed, so the refund carries the
// zero Usage. A nil l.workBudget is a no-op.
func (l *Loop) refundWork(ctx context.Context, req provider.Request) {
	if l.workBudget == nil {
		return
	}
	l.workBudget.Refund(ctx, req, provider.Usage{})
}

// settleWork runs the WorkBudget's Refund after a call completed with
// real Usage. A zero Usage (no observation) keeps the reservation
// consumed - the legacy loop's consume-on-completion rule - so Refund
// is skipped entirely in that case.
func (l *Loop) settleWork(ctx context.Context, req provider.Request, used provider.Usage) {
	if l.workBudget == nil {
		return
	}
	if used.PromptTokens == 0 && used.CompletionTokens == 0 && used.TotalTokens == 0 {
		return
	}
	l.workBudget.Refund(ctx, req, used)
}
