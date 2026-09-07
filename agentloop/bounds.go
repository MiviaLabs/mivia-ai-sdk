package agentloop

// Bounds groups the loop's numeric caps. Zero means uncapped or
// serial, per the member's own doc comment.
type Bounds struct {
	// MaxIterations bounds the Completer-call count of one Run.
	MaxIterations int
	// MaxCallsPerTurn bounds one turn's model-requested tool calls.
	// Zero means unbounded.
	MaxCallsPerTurn int
	// MaxTotalTokens caps the run's cumulative billed tokens. Zero
	// means unbounded.
	MaxTotalTokens int
	// MaxConcurrentTools bounds one turn's parallel tool calls. Zero
	// and one both mean serial.
	MaxConcurrentTools int
	// MaxConsecutiveToolFailures bounds consecutive all-failing turns.
	// Zero means unbounded.
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
