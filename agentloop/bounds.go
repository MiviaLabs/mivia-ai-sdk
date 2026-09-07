package agentloop

// Bounds groups the loop's numeric caps. Zero means uncapped or
// serial, per the member's own doc comment. The fully zero struct
// receives DefaultBounds at New; a partially set Bounds stays as
// given, so each zero member keeps its uncapped meaning.
type Bounds struct {
	// MaxIterations bounds the Completer-call count of one Run.
	MaxIterations int
	// MaxCallsPerTurn bounds one turn's model-requested tool calls.
	// Zero inside a partial Bounds means unbounded.
	MaxCallsPerTurn int
	// MaxTotalTokens caps the run's cumulative billed tokens. Zero
	// inside a partial Bounds means unbounded.
	MaxTotalTokens int
	// MaxConcurrentTools bounds one turn's parallel tool calls. Zero
	// and one both mean serial.
	MaxConcurrentTools int
	// MaxConsecutiveToolFailures bounds consecutive all-failing turns.
	// A turn counts as failing when every dispatched (non-duplicate)
	// call in it carries a reported tool error under
	// ErrorPolicyReport — any reported error, not only
	// tools.ErrUnknownName. Zero inside a partial Bounds means
	// unbounded.
	MaxConsecutiveToolFailures int
}

// Validate checks the caps in a fixed order and returns the first
// failure: MaxIterations, MaxTotalTokens, MaxCallsPerTurn,
// MaxConcurrentTools, then MaxConsecutiveToolFailures, each not
// negative.
func (b Bounds) Validate() error {
	if b.MaxIterations < 0 {
		return ErrMaxIterations
	}
	if b.MaxTotalTokens < 0 {
		return ErrMaxTotalTokens
	}
	if b.MaxCallsPerTurn < 0 {
		return ErrMaxCallsPerTurn
	}
	if b.MaxConcurrentTools < 0 {
		return ErrMaxConcurrentTools
	}
	if b.MaxConsecutiveToolFailures < 0 {
		return ErrMaxConsecutiveToolFailures
	}
	return nil
}

// Sensible production defaults for Bounds. They bound a runaway loop
// without squeezing a healthy one: two dozen model turns, eight tool
// calls per turn, a 200k-token billing cap, four-way tool parallelism,
// and a three-turn failure tripwire. A caller overrides any member by
// assigning the field after DefaultBounds.
const (
	defaultMaxIterations              = 24
	defaultMaxCallsPerTurn            = 8
	defaultMaxTotalTokens             = 200_000
	defaultMaxConcurrentTools         = 4
	defaultMaxConsecutiveToolFailures = 3
)

// DefaultBounds returns a Bounds with every cap set to a sensible
// production default; see the per-member constants. The returned
// value passes Validate. Callers copy and adjust single members.
func DefaultBounds() Bounds {
	return Bounds{
		MaxIterations:              defaultMaxIterations,
		MaxCallsPerTurn:            defaultMaxCallsPerTurn,
		MaxTotalTokens:             defaultMaxTotalTokens,
		MaxConcurrentTools:         defaultMaxConcurrentTools,
		MaxConsecutiveToolFailures: defaultMaxConsecutiveToolFailures,
	}
}
