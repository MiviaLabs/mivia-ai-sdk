package contextbudget

import "errors"

// Limits caps one model call's context by byte count and event
// count. A zero MaxBytes means no byte cap; a zero MaxEvents means no
// event-count cap. Both zero means the budget is uncapped.
type Limits struct {
	MaxBytes  int
	MaxEvents int
}

// Validate reports an error when MaxBytes or MaxEvents is negative.
// A zero or positive value in either field passes. When both fields
// are negative, Validate checks MaxBytes first and returns only the
// MaxBytes error.
func (l Limits) Validate() error {
	if l.MaxBytes < 0 {
		return errors.New("contextbudget: MaxBytes must not be negative")
	}
	if l.MaxEvents < 0 {
		return errors.New("contextbudget: MaxEvents must not be negative")
	}
	return nil
}

// Fits reports whether bytes and events both stay at or under their
// respective caps. A zero cap always reports fit for its own
// dimension. Fits takes the candidate totals the caller already has;
// it keeps no running total of its own. Fits does not call Validate;
// a caller that skips Validate and passes a negative cap gets
// whatever comparison bytes <= cap produces for that cap.
func (l Limits) Fits(bytes, events int) bool {
	if l.MaxBytes != 0 && bytes > l.MaxBytes {
		return false
	}
	if l.MaxEvents != 0 && events > l.MaxEvents {
		return false
	}
	return true
}
