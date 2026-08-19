package contextplan

import (
	"errors"
	"fmt"
)

// Sentinel errors for Window.Validate; test with errors.Is.
var (
	// ErrMaxTokensNotPositive is Validate's error when MaxTokens <= 0.
	ErrMaxTokensNotPositive = errors.New("contextplan: max tokens must be positive")
	// ErrReserveNegative is Validate's error when Reserve < 0.
	ErrReserveNegative = errors.New("contextplan: reserve must not be negative")
	// ErrReserveTooLarge is Validate's error when Reserve >= MaxTokens.
	ErrReserveTooLarge = errors.New("contextplan: reserve must be less than max tokens")
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
		return ErrReserveNegative
	}
	if w.Reserve >= w.MaxTokens {
		return ErrReserveTooLarge
	}
	if err := w.Compaction.Validate(); err != nil {
		return err
	}
	if w.Compaction.TargetTokens > 0 && w.Compaction.TargetTokens >= w.Budget() {
		return fmt.Errorf("contextplan: compaction target tokens %d at or above budget %d",
			w.Compaction.TargetTokens, w.Budget())
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
