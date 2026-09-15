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
// Refund settles each Completer attempt exactly once per Reserve —
// failure carries zero Usage plus the failure cause err
// (errors.Is(err, context.Canceled) identifies cancellation); success
// with observed usage carries the real Usage and a nil err; success
// reporting zero Usage gets no call (consume-on-completion). A
// prompt-too-long recovery can refund the failed attempt and later
// reserve again for the retry. A hook that panics forfeits the
// refund; panics propagate.
//
// Breaking change in v0.7.0: Refund now receives the call outcome error
// (func(ctx context.Context, req provider.Request, used provider.Usage, err error)).
//
// Both functions must be safe for concurrent use when callers share
// one Loop across concurrent Run calls.
type WorkBudget struct {
	// Reserve runs before each Completer call with the exact request
	// about to be sent. A non-nil return fails the run.
	Reserve func(ctx context.Context, req provider.Request) error
	// Refund settles each Completer attempt after the call outcome is
	// known: failure carries zero Usage and the error cause; success with
	// real Usage carries the response's billed tokens and a nil error.
	Refund func(ctx context.Context, req provider.Request, used provider.Usage, err error)
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
// never consumed: the Completer or observer call failed, so the refund
// carries the zero Usage and the call's actual error. A nil
// l.workBudget is a no-op.
func (l *Loop) refundWork(ctx context.Context, req provider.Request, err error) {
	if l.workBudget == nil {
		return
	}
	l.workBudget.Refund(ctx, req, provider.Usage{}, err)
}

// observeRequest runs the caller's Options.ObserveRequest hook for one
// iteration's request, after reserveWork and before the Completer call.
// A nil hook is a no-op. An error wraps with the 1-based iteration
// count, mirroring reserveWork; the caller refunds the already-held
// reservation before returning the error, on both the primary and the
// recovery route.
func (l *Loop) observeRequest(ctx context.Context, req provider.Request, iteration int) error {
	if l.observe == nil {
		return nil
	}
	if err := l.observe(ctx, req); err != nil {
		return fmt.Errorf("agentloop: iteration %d: observe request: %w", iteration, err)
	}
	return nil
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
	l.workBudget.Refund(ctx, req, used, nil)
}

// ErrIncompleteToolBudget is Options.Validate's error when ToolBudget
// is non-nil but Reserve is missing. Test with errors.Is.
var ErrIncompleteToolBudget = errors.New("agentloop: ToolBudget requires Reserve")

// ToolBudget is a host-callable cumulative tool-call budget the loop
// invokes once per turn, before that turn's tool calls dispatch. The
// SDK holds no budget policy of its own here, mirroring WorkBudget:
// Reserve runs host code (for example a shared call ceiling across
// concurrent subagent turns), and the loop only supplies the call
// point.
//
// The loop calls Reserve exactly once per turn that has tool calls to
// run, with the number of calls about to dispatch (resp.ToolCalls
// before per-call filtering, dedup, or any MaxCallsPerTurn clamp), and
// BEFORE any of them run. A non-nil return hard-fails the run before
// dispatch, with none of the turn's tool calls executed.
//
// There is no Refund: unlike a Completer call, a dispatched tool call
// is always consumed once Reserve admits the batch, so there is
// nothing to give back.
//
// Reserve must be safe for concurrent use when callers share one Loop
// across concurrent Run calls.
type ToolBudget struct {
	// Reserve runs once per turn before its tool calls dispatch, with
	// the count about to run. A non-nil return fails the run.
	Reserve func(ctx context.Context, calls int) error
}

// validate reports whether a ToolBudget is either nil (disabled) or
// fully wired.
func (b *ToolBudget) validate() error {
	if b == nil {
		return nil
	}
	if b.Reserve == nil {
		return ErrIncompleteToolBudget
	}
	return nil
}

// reserveTools runs the ToolBudget's Reserve for one turn's tool-call
// count. A nil l.toolBudget is a no-op; Options.Validate rejects a
// non-nil ToolBudget with a nil Reserve, so Reserve is never nil here.
// A hook error is wrapped with the 1-based iteration count so the
// hard fail names its cause, mirroring reserveWork.
func (l *Loop) reserveTools(ctx context.Context, calls int, iteration int) error {
	if l.toolBudget == nil {
		return nil
	}
	if err := l.toolBudget.Reserve(ctx, calls); err != nil {
		return fmt.Errorf("agentloop: iteration %d: tool budget reserve: %w", iteration, err)
	}
	return nil
}
