package agentloop

import "github.com/MiviaLabs/mivia-ai-sdk/provider"

// billedTokens returns the larger, more trustworthy reading of one
// response's token cost: the reported TotalTokens, or the sum of
// PromptTokens and CompletionTokens, whichever is greater. provider.Usage
// enforces no relationship between its fields, so a Completer that leaves
// TotalTokens at zero must not silently bypass MaxTotalTokens.
func billedTokens(u provider.Usage) int {
	sum := u.PromptTokens + u.CompletionTokens
	if u.TotalTokens > sum {
		return u.TotalTokens
	}
	return sum
}

// sumUsage adds b's four fields onto a and returns the sum.
func sumUsage(a, b provider.Usage) provider.Usage {
	return provider.Usage{
		PromptTokens:     a.PromptTokens + b.PromptTokens,
		CompletionTokens: a.CompletionTokens + b.CompletionTokens,
		TotalTokens:      a.TotalTokens + b.TotalTokens,
		CachedTokens:     a.CachedTokens + b.CachedTokens,
	}
}

// estimateTokens returns l.calibrated.EstimateTokens(req), or zero
// when l.calibrated is nil or the estimate call fails. Zero is a
// safe default: Calibrated.Observe no-ops on a non-positive estimated
// value, so an estimator failure here degrades silently, the same
// rule EstimateTokens failures already followed outside planning.
func (l *Loop) estimateTokens(req provider.Request) int {
	if l.calibrated == nil {
		return 0
	}
	est, err := l.calibrated.EstimateTokens(req)
	if err != nil {
		return 0
	}
	return est
}
