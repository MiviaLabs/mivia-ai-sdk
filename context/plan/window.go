package plan

import (
	"errors"
	"fmt"
)

// Sentinel errors for Window.Validate; test with errors.Is.
var (
	// ErrMaxTokensNotPositive is Validate's error when MaxTokens <= 0.
	// Kept separate from ErrInvalidOptions: agentloop/agentloop_test
	// asserts on this sentinel by name, so it stays a distinct value.
	ErrMaxTokensNotPositive = errors.New("plan: max tokens must be positive")
	// ErrInvalidOptions is the shared sentinel for every other
	// construction-time argument error in this package. Wrap it with
	// fmt.Errorf("%w: %s", ErrInvalidOptions, "<field>: <rule>") and
	// test with errors.Is plus a substring check on the field name.
	ErrInvalidOptions = errors.New("plan: invalid options")
)

// Window is the token budget for one planned request. MaxTokens is
// the model's context window; Reserve is the headroom Plan never
// spends, held back for the model's own reply. Compaction carries the
// compaction thresholds and retention rules; its zero value means the
// defaults, never "disabled".
type Window struct {
	MaxTokens  int
	Reserve    int
	Compaction Compaction
}

// Validate rejects a non-positive MaxTokens, a negative Reserve, a
// Reserve at or above MaxTokens, an invalid Compaction, and a
// positive Compaction.TargetTokens at or above Budget().
func (w Window) Validate() error {
	if w.MaxTokens <= 0 {
		return ErrMaxTokensNotPositive
	}
	if w.Reserve < 0 {
		return fmt.Errorf("%w: %s", ErrInvalidOptions, "Reserve: must not be negative")
	}
	if w.Reserve >= w.MaxTokens {
		return fmt.Errorf("%w: %s", ErrInvalidOptions, "Reserve: must be less than max tokens")
	}
	if err := w.Compaction.Validate(); err != nil {
		return err
	}
	if w.Compaction.TargetTokens > 0 && w.Compaction.TargetTokens >= w.Budget() {
		return fmt.Errorf("%w: %s", ErrInvalidOptions,
			fmt.Sprintf("TargetTokens: %d at or above budget %d", w.Compaction.TargetTokens, w.Budget()))
	}
	return nil
}

// Budget returns MaxTokens - Reserve, the tokens Plan may spend on
// Request.Messages.
func (w Window) Budget() int {
	return w.MaxTokens - w.Reserve
}

// CompactTrigger returns the trigger in tokens: Budget times
// TriggerPercent, floored.
func (w Window) CompactTrigger() int {
	return w.Budget() * w.Compaction.triggerPercent() / 100
}

// CompactTarget returns the target in tokens: TargetTokens when
// positive, else Budget times TargetPercent, floored.
func (w Window) CompactTarget() int {
	if w.Compaction.TargetTokens > 0 {
		return w.Compaction.TargetTokens
	}
	return w.Budget() * w.Compaction.targetPercent() / 100
}
