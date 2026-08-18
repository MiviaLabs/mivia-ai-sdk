package durablefence

import (
	"context"
	"errors"
	"fmt"
)

// Scenario carries the caller-supplied functions this kit drives
// against one claim-and-fence implementation under test. Every field
// takes a context.Context first and returns an error last. Claim and
// Takeover return an opaque owner token as a string; a check function
// never inspects the token's shape, only threads it from one call into
// the next.
type Scenario struct {
	// Claim grants a fresh owner token for an unheld resource.
	Claim func(context.Context) (string, error)
	// Takeover reassigns ownership away from a stale owner and fences
	// the prior token out.
	Takeover func(context.Context) (string, error)
	// Mutate performs an owner action under token that must reject a
	// fenced, stale token.
	Mutate func(context.Context, string) error
	// Release returns a held resource to the unheld state.
	Release func(context.Context, string) error
	// IsHeld reports the current hold state without mutating it.
	IsHeld func(context.Context) (bool, error)
	// IsFenced reports whether token has been fenced out.
	IsFenced func(context.Context, string) (bool, error)
}

// ErrIncompleteScenario is the sentinel Validate wraps around the name
// of the first nil function field it finds.
var ErrIncompleteScenario = errors.New("durablefence: scenario is incomplete")

// Validate reports the first nil function field, wrapped in
// ErrIncompleteScenario. It returns nil when every field is set.
func (s Scenario) Validate() error {
	fields := []struct {
		name string
		set  bool
	}{
		{"Claim", s.Claim != nil},
		{"Takeover", s.Takeover != nil},
		{"Mutate", s.Mutate != nil},
		{"Release", s.Release != nil},
		{"IsHeld", s.IsHeld != nil},
		{"IsFenced", s.IsFenced != nil},
	}
	for _, f := range fields {
		if !f.set {
			return fmt.Errorf("%w: field %s is nil", ErrIncompleteScenario, f.name)
		}
	}
	return nil
}
