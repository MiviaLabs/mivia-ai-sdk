package flow

import (
	"context"
	"time"

	"github.com/MiviaLabs/mivia-ai-sdk/machine"
)

// RetryPolicy is a step's retry rule for its Fire call.
// MaxAttempts counts every attempt, including the first; a value of 1
// disables retry. BaseDelay is the first retry's backoff. MaxDelay
// clamps every computed backoff, so the exponential term cannot
// overflow time.Duration's range. Retryable, when non-nil, gates each
// failure before the next attempt; a nil Retryable retries every
// error. Jitter and Sleep are determinism hooks: Jitter perturbs
// NextDelay's clamped result, and Sleep waits between attempts. A nil
// Sleep defaults to a context-aware sleep. Sleep takes the run's ctx
// so a caller can cancel a pending backoff.
type RetryPolicy struct {
	MaxAttempts int
	BaseDelay   time.Duration
	MaxDelay    time.Duration
	Retryable   func(error) bool
	Jitter      func(time.Duration) time.Duration
	Sleep       func(context.Context, time.Duration)
}

// Validate enforces MaxAttempts >= 1 and MaxDelay > 0. New calls the
// same check, through retryValidateMessage, for every step whose
// Retry is non-nil, folding the step's ID into the pinned message.
func (p RetryPolicy) Validate() error {
	if msg := retryValidateMessage(p); msg != "" {
		return errorf("retry: %s", msg)
	}
	return nil
}

// retryValidateMessage returns the unprefixed Validate failure
// message for p, or "" when p is valid. Both Validate and
// validateRetry build their pinned error text from this one check, so
// the rule lives in one place.
func retryValidateMessage(p RetryPolicy) string {
	if p.MaxAttempts < 1 {
		return "max attempts must be at least 1"
	}
	if p.MaxDelay <= 0 {
		return "max delay must be positive"
	}
	return ""
}

// NextDelay returns the backoff before the given retry attempt,
// one-indexed from the first retry. It doubles delay from BaseDelay
// one step per attempt above 1, checking the bound before each
// doubling instead of after: when delay > MaxDelay>>1, one more
// doubling would reach or overflow MaxDelay, so NextDelay sets delay
// to MaxDelay and stops doubling, without ever performing the
// overflow-prone multiply. A final clamp to MaxDelay covers the case
// where BaseDelay itself already exceeds MaxDelay. Pure; no field
// mutation, no sleep, no randomness of its own. Jitter, when
// non-nil, applies to the clamped result last; NextDelay does not
// re-clamp Jitter's output, so a Jitter closure that returns a value
// above MaxDelay passes through unclamped. Re-clamping after Jitter
// is the caller's responsibility.
func (p RetryPolicy) NextDelay(attempt int) time.Duration {
	delay := p.BaseDelay
	for i := 1; i < attempt; i++ {
		if delay > p.MaxDelay>>1 {
			delay = p.MaxDelay
			break
		}
		delay *= 2
	}
	if delay > p.MaxDelay {
		delay = p.MaxDelay
	}
	if p.Jitter != nil {
		delay = p.Jitter(delay)
	}
	return delay
}

// defaultSleep waits for d, returning ctx.Err() at once if ctx is
// canceled before d elapses.
func defaultSleep(ctx context.Context, d time.Duration) {
	timer := time.NewTimer(d)
	defer timer.Stop()
	select {
	case <-ctx.Done():
	case <-timer.C:
	}
}

// fireWithRetry wraps fireStep with step.Retry's loop. A nil
// step.Retry calls fireStep once and returns its result unchanged,
// matching the no-retry behavior exactly. Using fireStep, not m.Fire
// directly, matters: fireStep tags a Fire error failureKindFire, and
// resolveCatchable requires that tag to route an exhausted retry into
// a declared AdmissionOnFailed fallback.
//
// On a fireStep error the loop checks the completed attempt count
// against MaxAttempts before it checks Retryable: an exhausted budget
// stops the loop at once, without spending a call on Retryable, so a
// MaxAttempts of 1 never calls Retryable or Sleep. Only when budget
// remains does the loop check Retryable, then sleep and retry.
func fireWithRetry(
	ctx context.Context, m *machine.Definition, cur machine.Status,
	rec machine.InOut, step Step, row machine.Transition,
) (machine.Status, machine.InOut, error) {
	if step.Retry == nil {
		return fireStep(ctx, m, cur, rec, step, row)
	}
	policy := step.Retry
	attempt := 1
	for {
		nCur, nRec, err := fireStep(ctx, m, cur, rec, step, row)
		if err == nil {
			return nCur, nRec, nil
		}
		if attempt >= policy.MaxAttempts {
			return nCur, nRec, err
		}
		if policy.Retryable != nil && !policy.Retryable(err) {
			return nCur, nRec, err
		}
		sleep := policy.Sleep
		if sleep == nil {
			sleep = defaultSleep
		}
		sleep(ctx, policy.NextDelay(attempt))
		if ctxErr := ctx.Err(); ctxErr != nil {
			return nCur, nRec, ctxErr
		}
		attempt++
	}
}

// validateRetry rejects a Retry set on a step with a non-nil Sub, and
// a Retry set on a panel member. It checks RetryPolicy.Validate's
// rule for every step whose Retry is non-nil, folding the step's ID
// into the pinned message.
func validateRetry(steps []Step, panels []Panel, ids map[string]int) error {
	for i := range steps {
		if steps[i].Retry == nil {
			continue
		}
		if msg := retryValidateMessage(*steps[i].Retry); msg != "" {
			return errorf("step %q retry: %s", steps[i].ID, msg)
		}
		if steps[i].Sub != nil {
			return errorf("step %q has a retry policy but a sub-workflow", steps[i].ID)
		}
	}
	for i, p := range panels {
		for _, id := range p {
			if steps[ids[id]].Retry != nil {
				return errorf("panel %d names retried step %q", i, id)
			}
		}
	}
	return nil
}
