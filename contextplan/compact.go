package contextplan

import (
	"fmt"

	"github.com/MiviaLabs/mivia-ai-sdk/provider"
)

// CompactResult is Compact's output.
type CompactResult struct {
	Kept          []provider.Message
	Dropped       []provider.Message
	BeforeTokens  int
	AfterTokens   int
	TriggerTokens int
	TargetTokens  int
	Compacted     bool
	Key           string
}

// Compact applies the trigger check and the retention policy. An
// invalid window fails Window.Validate before any estimate. A request
// at or above the trigger compacts; below it passes through with
// Compacted false. The retention set is mandatory; the tail fill is
// optional and stops at the first unit that breaks contiguity, the
// message-count bound, or the target. Kept preserves the original
// relative order. The Key is deterministic per input.
func Compact(msgs []provider.Message, w Window, e provider.TokenEstimator) (CompactResult, error) {
	if err := w.Validate(); err != nil {
		return CompactResult{}, err
	}
	if len(msgs) == 0 {
		return CompactResult{}, ErrNoMessages
	}
	if !hasUserMessage(msgs) {
		return CompactResult{}, ErrNoObjective
	}
	before, err := estimateOf(e, msgs)
	if err != nil {
		return CompactResult{}, err
	}
	res := CompactResult{
		BeforeTokens:  before,
		TriggerTokens: w.CompactTrigger(),
		TargetTokens:  w.CompactTarget(),
	}
	if before < res.TriggerTokens {
		res.Kept = msgs
		res.AfterTokens = before
		res.Key = compactionKey(msgs, w.Budget(), res.TriggerTokens, res.TargetTokens)
		return res, nil
	}
	units := buildUnits(msgs)
	selected := selectMandatory(units, msgs, w.Compaction.PreserveNames)
	mandatory := collectSelected(units, selected)
	mandatoryTokens, err := estimateOf(e, mandatory)
	if err != nil {
		return CompactResult{}, err
	}
	if mandatoryTokens > w.Budget() {
		return CompactResult{}, fmt.Errorf("%w: %d tokens over budget %d",
			ErrRetentionOverflow, mandatoryTokens, w.Budget())
	}
	if err := fillTail(units, selected, e, w, res.TargetTokens); err != nil {
		return CompactResult{}, err
	}
	kept, dropped := partitionUnits(units, selected)
	after, err := estimateOf(e, kept)
	if err != nil {
		return CompactResult{}, err
	}
	res.Kept = kept
	res.Dropped = dropped
	res.AfterTokens = after
	res.Compacted = true
	res.Key = compactionKey(kept, w.Budget(), res.TriggerTokens, res.TargetTokens)
	return res, nil
}

// estimateOf runs the estimator over msgs, wrapping a failure in
// ErrEstimateFailed.
func estimateOf(e provider.TokenEstimator, msgs []provider.Message) (int, error) {
	n, err := e.EstimateTokens(provider.Request{Messages: msgs})
	if err != nil {
		return 0, fmt.Errorf("%w: %w", ErrEstimateFailed, err)
	}
	return n, nil
}

// hasUserMessage reports whether any message carries RoleUser.
func hasUserMessage(msgs []provider.Message) bool {
	for _, m := range msgs {
		if m.Role == provider.RoleUser {
			return true
		}
	}
	return false
}

// fillTail walks units newest to oldest, adding unselected units to
// the tail while they fit the message-count bound and the target. It
// stops at the first unit that does not fit and never resumes past
// it, so the optional tail stays a contiguous suffix.
func fillTail(units []unit, selected []bool, e provider.TokenEstimator, w Window, target int) error {
	tail := 0
	bound := w.Compaction.recentTailBound()
	for ui := len(units) - 1; ui >= 0; ui-- {
		if selected[ui] {
			continue
		}
		if tail+len(units[ui].msgs) > bound {
			break
		}
		candidate := append(collectSelected(units, selected), units[ui].msgs...)
		est, err := estimateOf(e, candidate)
		if err != nil {
			return err
		}
		if est > target {
			break
		}
		selected[ui] = true
		tail += len(units[ui].msgs)
	}
	return nil
}

// collectSelected gathers every selected unit's messages in original
// order into one fresh slice.
func collectSelected(units []unit, selected []bool) []provider.Message {
	var kept []provider.Message
	for ui, u := range units {
		if selected[ui] {
			kept = append(kept, u.msgs...)
		}
	}
	return kept
}

// partitionUnits splits every unit's messages into kept and dropped,
// both in original order.
func partitionUnits(units []unit, selected []bool) (kept, dropped []provider.Message) {
	for ui, u := range units {
		if selected[ui] {
			kept = append(kept, u.msgs...)
			continue
		}
		dropped = append(dropped, u.msgs...)
	}
	return kept, dropped
}
