package agentloop

import (
	"github.com/MiviaLabs/mivia-ai-sdk/contextplan"
	"github.com/MiviaLabs/mivia-ai-sdk/provider"
)

// defaultWindowTrigger and defaultWindowTarget are the hysteresis the
// derived default Window uses: compact when history crosses 80% of
// the provider's window, rebuild at 50%. They mirror the values the
// host-side context managers converge on.
const (
	defaultWindowTrigger = 80
	defaultWindowTarget  = 50
)

// deriveWindow returns the default Window the completer's
// ContextAccountant capability implies, or nil when the completer
// does not implement the capability or reports a non-positive window.
// Reserve holds one fifth of the window back for the model's reply,
// matching the 80% trigger.
func deriveWindow(completer provider.Completer) *contextplan.Window {
	ca, ok := completer.(provider.ContextAccountant)
	if !ok {
		return nil
	}
	max := ca.ContextWindow()
	if max <= 0 {
		return nil
	}
	return &contextplan.Window{
		MaxTokens:  max,
		Reserve:    max / 5,
		Compaction: contextplan.Compaction{TriggerPercent: defaultWindowTrigger, TargetPercent: defaultWindowTarget},
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
