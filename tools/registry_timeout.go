package tools

import (
	"context"
	"errors"
	"fmt"
	"time"
)

// DefaultRunTimeout bounds a run when the tool declares no profile
// Timeout and no WithDefaultRunTimeout option applies. Protection is
// the floor; no caller opts in.
const DefaultRunTimeout time.Duration = 10 * time.Minute

// TimeoutNone is the canonical negative duration meaning "never cap
// this run". The resolver treats any negative value the same way.
const TimeoutNone time.Duration = -1

// ErrRunTimeout is Run's and RunScoped's error when a tool exceeds
// its effective bound. It returns wrapped with the tool name and the
// bound; test with errors.Is, never by matching the naked sentinel.
var ErrRunTimeout = errors.New("tools: tool run exceeded its timeout")

// Option configures one Registry at New time. Options apply left to
// right during New; the configuration stays immutable afterward, so
// concurrent Runs need no lock over it.
type Option func(*Registry)

// WithDefaultRunTimeout sets the registry-wide bound for tools whose
// profile declares no Timeout. Positive binds verbatim; zero selects
// DefaultRunTimeout; any negative, canonically TimeoutNone, restores
// an unbounded run. No argument value is rejected.
func WithDefaultRunTimeout(d time.Duration) Option {
	return func(r *Registry) { r.defaultRunTimeout = d }
}

// effectiveRunTimeout resolves one call's bound from the declared
// profile and the configured default. A positive profile Timeout wins
// outright; a negative profile Timeout never caps; an undeclared
// profile falls through: positive configured binds verbatim, negative
// configured never caps, otherwise DefaultRunTimeout.
func effectiveRunTimeout(t Tool, configured time.Duration) time.Duration {
	if pt, ok := t.(ProfiledTool); ok {
		if declared := pt.ExecutionProfile().Timeout; declared != 0 {
			if declared < 0 {
				return 0 // non-positive marker: skip the wrap
			}
			return declared
		}
	}
	switch {
	case configured > 0:
		return configured
	case configured < 0:
		return 0 // non-positive marker: skip the wrap
	default:
		return DefaultRunTimeout
	}
}

// timeoutResult carries one bounded run's outcome over runBounded's
// one-buffered channel; the buffer lets an abandoned producer's send
// complete without a live receiver. A non-nil panicV carries a tool's
// recovered panic: panics do not cross goroutines, so the caller
// converts it into an error instead of crashing the process.
type timeoutResult struct {
	out    Out
	err    error
	panicV any
}

// runBounded dispatches one t.Run under the effective bound for r.
// A non-positive bound calls t.Run inline with the caller's exact
// context. Results pass through byte-identical whenever the tool
// finishes first, whatever they contain, including errors shaped like
// cancellation or timeouts the tool produced on its own. On expiry
// the parent context is re-checked first: a parent already past done
// yields the parent cause, so the tie never trusts select fairness.
// The deadline covers t.Run only; RunScoped calls this after Approve,
// so approval time never consumes the budget.
func (r *Registry) runBounded(ctx context.Context, name string, t Tool, in InOut) (Out, error) {
	bound := effectiveRunTimeout(t, r.defaultRunTimeout)
	if bound <= 0 {
		return t.Run(ctx, in)
	}
	runCtx, cancel := context.WithTimeout(ctx, bound)
	defer cancel()
	done := make(chan timeoutResult, 1)
	go func() {
		// Bridge a panicking tool across the goroutine: recover here,
		// return it as an error in the completion branch below, matching
		// agentloop's safeSurface precedent that package code fails the
		// call closed instead of crashing the process.
		defer func() {
			if p := recover(); p != nil {
				done <- timeoutResult{panicV: p}
			}
		}()
		out, err := t.Run(runCtx, in)
		done <- timeoutResult{out: out, err: err}
	}()
	select {
	case res := <-done:
		if res.panicV != nil {
			// An error-valued panic keeps its unwrap chain, so callers
			// match seams through errors.Is; anything else renders as
			// text. See agentloop's safeSurface precedent.
			if perr, ok := res.panicV.(error); ok {
				return Out{}, fmt.Errorf("tools: %q panicked: %w", name, perr)
			}
			return Out{}, fmt.Errorf("tools: %q panicked: %v", name, res.panicV)
		}
		return res.out, res.err
	case <-runCtx.Done():
		if parent := ctx.Err(); parent != nil {
			return Out{}, parent
		}
		return Out{}, fmt.Errorf("tools: %q ran past %s: %w", name, bound, ErrRunTimeout)
	}
}
