package agentloop

import (
	"context"
	"fmt"

	"github.com/MiviaLabs/mivia-ai-sdk/contextplan"
	"github.com/MiviaLabs/mivia-ai-sdk/contextsummary"
	"github.com/MiviaLabs/mivia-ai-sdk/provider"
)

// planHistory runs the per-iteration planning step: estimate the
// history, pass through under the trigger, compact above it. An
// estimate error fails the iteration with ErrPlanFailed.
func (l *Loop) planHistory(ctx context.Context, history []provider.Message, iteration int) ([]provider.Message, error) {
	est, err := l.calibrated.EstimateTokens(provider.Request{Messages: history})
	if err != nil {
		return nil, fmt.Errorf("agentloop: iteration %d: %w: %w", iteration, ErrPlanFailed, err)
	}
	if est < l.window.CompactTrigger() {
		return history, nil
	}
	rebuilt, _, err := l.compactHistory(ctx, history, *l.window, iteration, false)
	return rebuilt, err
}

// compactHistory runs the compaction sequence over history under w.
// It returns the rebuilt history and whether Compact compacted
// anything. notice appends one CompactionNotice message after the
// summary injection, the recovery path's addition; under notice, an
// uncompacted result returns early and unchanged, so the caller can
// treat it as unrecoverable. A failure returns a nil history;
// history itself never changes on failure.
func (l *Loop) compactHistory(ctx context.Context, history []provider.Message, w contextplan.Window, iteration int, notice bool) ([]provider.Message, bool, error) {
	adjusted := preserveSummaryName(w)
	prior, rest := splitSummary(history)
	res, err := contextplan.Compact(rest, adjusted, l.calibrated)
	if err != nil {
		return nil, false, fmt.Errorf("agentloop: iteration %d: %w: %w", iteration, ErrCompactionFailed, err)
	}
	if !res.Compacted && notice {
		return nil, false, nil
	}
	rebuilt := append([]provider.Message(nil), res.Kept...)
	injected := false
	if len(res.Dropped) > 0 || prior != nil {
		summary, err := l.summarizeDropped(ctx, prior, res.Dropped)
		if err != nil {
			return nil, false, err
		}
		rebuilt = injectAfterSystem(rebuilt, contextsummary.SummaryMessage(summary))
		injected = true
	}
	if notice {
		rebuilt = injectNotice(rebuilt, injected)
	}
	if err := l.checkCompactedBudget(rebuilt, w, iteration); err != nil {
		return nil, false, err
	}
	return rebuilt, res.Compacted, nil
}

// summarizeDropped prepends the held-aside prior summary, when one
// exists, to the dropped messages and runs one summarizer call.
func (l *Loop) summarizeDropped(ctx context.Context, prior *provider.Message, dropped []provider.Message) (contextsummary.Summary, error) {
	input := dropped
	if prior != nil {
		input = append([]provider.Message{*prior}, dropped...)
	}
	summary, err := l.summarizer.Summarize(ctx, input)
	if err != nil {
		return contextsummary.Summary{}, fmt.Errorf("agentloop: %w: %w", ErrCompactionFailed, err)
	}
	return summary, nil
}

// checkCompactedBudget re-estimates the rebuilt history and fails
// closed above the effective window's budget.
func (l *Loop) checkCompactedBudget(rebuilt []provider.Message, w contextplan.Window, iteration int) error {
	est, err := l.calibrated.EstimateTokens(provider.Request{Messages: rebuilt})
	if err != nil {
		return fmt.Errorf("agentloop: iteration %d: %w: %w", iteration, ErrCompactionFailed, err)
	}
	if est > w.Budget() {
		return fmt.Errorf("agentloop: iteration %d: %w: %w", iteration, ErrCompactionFailed, contextplan.ErrRetentionOverflow)
	}
	return nil
}

// recoveryWindow builds the prompt-too-long recovery window: the
// caller's window with the trigger at one percent and the target at
// max(1, min(RecoveryTargetTokens, Budget over four)).
func recoveryWindow(w contextplan.Window) contextplan.Window {
	rw := w
	rw.Compaction.TriggerPercent = 1
	target := w.Budget() / 4
	if target > RecoveryTargetTokens {
		target = RecoveryTargetTokens
	}
	if target < 1 {
		target = 1
	}
	rw.Compaction.TargetTokens = target
	return rw
}

// preserveSummaryName copies w and appends the summary message name
// to the copy's PreserveNames only when absent, into a freshly
// allocated slice. The caller's backing array never changes.
func preserveSummaryName(w contextplan.Window) contextplan.Window {
	for _, name := range w.Compaction.PreserveNames {
		if name == contextsummary.SummaryMessageName {
			return w
		}
	}
	fresh := make([]string, 0, len(w.Compaction.PreserveNames)+1)
	fresh = append(fresh, w.Compaction.PreserveNames...)
	w.Compaction.PreserveNames = append(fresh, contextsummary.SummaryMessageName)
	return w
}

// splitSummary removes every message named SummaryMessageName from
// msgs and returns the first one held aside, plus the rest.
func splitSummary(msgs []provider.Message) (*provider.Message, []provider.Message) {
	var prior *provider.Message
	rest := make([]provider.Message, 0, len(msgs))
	for i := range msgs {
		if msgs[i].Name == contextsummary.SummaryMessageName {
			if prior == nil {
				prior = &msgs[i]
			}
			continue
		}
		rest = append(rest, msgs[i])
	}
	return prior, rest
}

// injectAfterSystem inserts msg directly after the leading system
// message, or at index zero when none leads.
func injectAfterSystem(msgs []provider.Message, msg provider.Message) []provider.Message {
	out := make([]provider.Message, 0, len(msgs)+1)
	if len(msgs) > 0 && msgs[0].Role == provider.RoleSystem {
		out = append(out, msgs[0], msg)
		return append(out, msgs[1:]...)
	}
	out = append(out, msg)
	return append(out, msgs...)
}

// injectNotice appends the CompactionNotice message directly after
// the summary injection: after the summary message when one was
// injected, else at the injection point.
func injectNotice(msgs []provider.Message, summaryInjected bool) []provider.Message {
	if !summaryInjected {
		return injectAfterSystem(msgs, provider.Message{Role: provider.RoleUser, Content: CompactionNotice})
	}
	out := make([]provider.Message, 0, len(msgs)+1)
	for i := range msgs {
		out = append(out, msgs[i])
		if msgs[i].Name == contextsummary.SummaryMessageName {
			out = append(out, provider.Message{Role: provider.RoleUser, Content: CompactionNotice})
		}
	}
	return out
}

// recoverPromptTooLong handles one ErrPromptTooLong rejection: compact
// under the recovery window, append the notice, and retry the same
// iteration's Chat exactly once. When the compaction sequence returns
// Compacted false, the history estimates under one percent of the
// budget, so orig returns unchanged with no retry and no notice. Any
// retry error, including a second rejection, propagates.
func (l *Loop) recoverPromptTooLong(ctx context.Context, orig error, history []provider.Message, iteration int) (provider.Response, []provider.Message, provider.Request, error) {
	rw := recoveryWindow(*l.window)
	if err := rw.Validate(); err != nil {
		return provider.Response{}, nil, provider.Request{},
			fmt.Errorf("agentloop: iteration %d: %w: %w", iteration, ErrCompactionFailed, err)
	}
	rebuilt, compacted, err := l.compactHistory(ctx, history, rw, iteration, true)
	if err != nil {
		return provider.Response{}, nil, provider.Request{}, err
	}
	if !compacted {
		return provider.Response{}, nil, provider.Request{}, orig
	}
	req := provider.Request{Model: l.model, Messages: rebuilt, Tools: l.defs}
	resp, err := l.completer.Chat(ctx, req)
	if err != nil {
		return provider.Response{}, nil, provider.Request{}, err
	}
	return resp, rebuilt, req, nil
}
