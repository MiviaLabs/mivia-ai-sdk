package agentloop

import (
	"fmt"
	"time"

	"github.com/MiviaLabs/mivia-ai-sdk/provider"
)

// Conclude groups the graceful-conclude options: when the loop starts
// nudging the model toward a final answer, and what it says.
type Conclude struct {
	// Margin nudges the model once MaxIterations-k < Margin holds.
	// Zero disables the step-count term.
	Margin int
	// Deadline, when positive, fires the nudge once
	// StartTime.Add(Deadline) has passed. Zero disables the term.
	Deadline time.Duration
	// Notice is the RoleUser content Run appends once nudging starts.
	// Empty Notice uses DefaultConcludeNotice.
	Notice string
}

// Validate checks the group in a fixed order and returns the first
// failure: Margin is not negative, then Deadline is not negative.
// Every failure returns ErrInvalidOptions, wrapped with the failing
// field's name; test with errors.Is against ErrInvalidOptions.
func (c Conclude) Validate() error {
	if c.Margin < 0 {
		return fmt.Errorf("%w: %s", ErrInvalidOptions, "Margin: must not be negative")
	}
	if c.Deadline < 0 {
		return fmt.Errorf("%w: %s", ErrInvalidOptions, "Deadline: must be non-negative")
	}
	return nil
}

// noticePresent reports whether history carries the ConcludeNotice
// among its messages, the present-tense signal for whether the model
// actually saw the nudge in this iteration's Completer request. A
// sticky "was the notice ever appended" flag is not enough:
// Options.Trim (or Compaction.Window) may strip the notice out of a
// later iteration's history before that iteration's Completer call
// runs. See docs/history/agentloop.md's Trim limit.
// Callers must also gate this on noticeSent: noticePresent alone
// cannot tell this run's own nudge apart from matching text a caller
// fed in through the starting History for an unrelated reason.
func noticePresent(history []provider.Message, notice string) bool {
	for _, m := range history {
		if m.Role == provider.RoleUser && m.Content == notice {
			return true
		}
	}
	return false
}

// shouldConclude reports whether the upcoming Completer call, the
// 1-based iteration k = iterations+1, qualifies for the conclude
// nudge. Two terms are OR-ed: any one firing triggers the nudge.
//
//   - Margin: bounds.MaxIterations-k < conclude.Margin. The original
//     step-count term. A zero Margin never fires.
//   - Deadline: time.Until(deadlineAt) <= 0.
//     A zero Deadline (and so a zero deadlineAt) never
//     fires.
func (l *Loop) shouldConclude(iterations int) bool {
	k := iterations + 1
	if l.conclude.Margin > 0 && l.bounds.MaxIterations-k < l.conclude.Margin {
		return true
	}
	if !l.deadlineAt.IsZero() && l.conclude.Deadline > 0 &&
		time.Until(l.deadlineAt) <= 0 {
		return true
	}
	return false
}
