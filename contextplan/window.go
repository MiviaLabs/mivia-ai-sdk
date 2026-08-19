package contextplan

import "errors"

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
// spends, held back for the model's own reply.
type Window struct {
	MaxTokens int
	Reserve   int
}

// Validate rejects a non-positive MaxTokens, a negative Reserve, and
// a Reserve at or above MaxTokens.
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
	return nil
}

// Budget returns MaxTokens - Reserve, the tokens Plan may spend on
// Request.Messages.
func (w Window) Budget() int {
	return w.MaxTokens - w.Reserve
}
