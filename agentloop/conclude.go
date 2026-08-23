package agentloop

import (
	"time"

	"github.com/MiviaLabs/mivia-ai-sdk/provider"
)

// noticePresent reports whether history carries the ConcludeNotice
// among its messages, the present-tense signal for whether the model
// actually saw the nudge in this iteration's Completer request. A
// sticky "was the notice ever appended" flag is not enough:
// Options.Trim (or Window) may strip the notice out of a later
// iteration's history before that iteration's Completer call runs.
// See docs/plans/agents/phase79_graceful_conclude.md's Trim limit.
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
// nudge. Four terms are OR-ed: any one firing triggers the nudge.
//
//   - ConcludeMargin: maxIterations-k < concludeMargin. The original
//     phase-79 step-count term. A zero concludeMargin never fires.
//   - ConcludeDeadline: time.Until(deadlineAt) < concludeDeadline.
//     A zero concludeDeadline (and so a zero deadlineAt) never
//     fires.
//   - ConcludeToolCallsLeft: maxCallsPerTurn-k < concludeToolCallsLeft.
//     A zero concludeToolCallsLeft never fires; a zero
//     maxCallsPerTurn (uncapped) also never fires, since subtracting
//     a positive k from zero yields a non-positive number that is
//     not below the threshold.
//   - ConcludeStepsLeft: maxIterations-k < concludeStepsLeft. A zero
//     concludeStepsLeft never fires.
//
// See docs/plans/agents/phase79_graceful_conclude.md for the worked
// ConcludeMargin table.
func (l *Loop) shouldConclude(iterations int) bool {
	k := iterations + 1
	if l.concludeMargin > 0 && l.maxIterations-k < l.concludeMargin {
		return true
	}
	if !l.deadlineAt.IsZero() && l.concludeDeadline > 0 &&
		time.Until(l.deadlineAt) < l.concludeDeadline {
		return true
	}
	if l.concludeToolCallsLeft > 0 && l.maxCallsPerTurn > 0 &&
		l.maxCallsPerTurn-k < l.concludeToolCallsLeft {
		return true
	}
	if l.concludeStepsLeft > 0 && l.maxIterations-k < l.concludeStepsLeft {
		return true
	}
	return false
}
