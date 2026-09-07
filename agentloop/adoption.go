package agentloop

import (
	"github.com/MiviaLabs/mivia-ai-sdk/context/plan"
	"github.com/MiviaLabs/mivia-ai-sdk/provider"
)

// defaultWindowTrigger and defaultWindowTarget are Compaction
// percents. Window.CompactTrigger and CompactTarget price a percent
// against Budget, not MaxTokens. Budget is 4/5 of MaxTokens here
// (Reserve below), so 80 and 50 of Budget are an effective 64% and
// 40% of MaxTokens. See docs/history/agentloop.md, "Effective
// thresholds for host-style configs".
const (
	defaultWindowTrigger = 80
	defaultWindowTarget  = 50
)

// deriveWindow returns the default Window the completer's
// ContextAccountant capability implies, or nil when the completer
// does not implement the capability or reports a non-positive window.
// Reserve holds one fifth of the window back for the model's reply.
// The trigger and target above price against Budget, so they fire at
// an effective 64% and 40% of MaxTokens, not 80% and 50%.
func deriveWindow(completer provider.Completer) *plan.Window {
	ca, ok := completer.(provider.ContextAccountant)
	if !ok {
		return nil
	}
	max := ca.ContextWindow()
	if max <= 0 {
		return nil
	}
	return &plan.Window{
		MaxTokens:  max,
		Reserve:    max / 5,
		Compaction: plan.Compaction{TriggerPercent: defaultWindowTrigger, TargetPercent: defaultWindowTarget},
	}
}

// deriveReasoningEffort returns the completer's ReasoningPolicy
// default effort, or empty when the completer does not implement the
// capability. Empty means no per-request default.
func deriveReasoningEffort(completer provider.Completer) provider.ReasoningEffort {
	if rp, ok := completer.(provider.ReasoningPolicy); ok {
		return provider.ReasoningEffort(rp.ReasoningEffort())
	}
	return ""
}
