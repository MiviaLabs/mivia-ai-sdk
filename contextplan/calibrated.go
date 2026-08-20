package contextplan

import (
	"sync"

	"github.com/MiviaLabs/mivia-ai-sdk/provider"
)

// EWMA smoothing bounds for Calibrated.
const (
	// DefaultSmoothingFactor is the EWMA weight Observe applies when
	// Calibrate receives a non-positive alpha.
	DefaultSmoothingFactor = 0.3
	// MinCorrectionFactor is the floor Observe clamps the correction
	// factor to.
	MinCorrectionFactor = 0.5
	// MaxCorrectionFactor is the ceiling Observe clamps the correction
	// factor to.
	MaxCorrectionFactor = 2.0
)

// Calibrated wraps a provider.TokenEstimator with an exponentially
// weighted moving average, corrected after each completed turn
// through Observe. A *Calibrated implements provider.TokenEstimator.
// Safe for concurrent use: one mutex guards factor, the only mutable
// field.
type Calibrated struct {
	mu     sync.Mutex
	est    provider.TokenEstimator
	alpha  float64
	factor float64
}

// Calibrate wraps est with an EWMA correction factor. alpha is the
// EWMA smoothing weight in (0, 1]; a value outside that range,
// including zero or negative, falls back to DefaultSmoothingFactor. A
// nil est is a caller error caught at the first EstimateTokens call,
// not at construction, matching the wrapped interface's own contract.
func Calibrate(est provider.TokenEstimator, alpha float64) *Calibrated {
	if alpha <= 0 || alpha > 1 {
		alpha = DefaultSmoothingFactor
	}
	return &Calibrated{est: est, alpha: alpha, factor: 1.0}
}

// EstimateTokens calls the wrapped estimator, then scales the result
// by the current EWMA correction factor, itself always within
// [MinCorrectionFactor, MaxCorrectionFactor].
func (c *Calibrated) EstimateTokens(req provider.Request) (int, error) {
	raw, err := c.est.EstimateTokens(req)
	if err != nil {
		return 0, err
	}
	c.mu.Lock()
	scaled := int(float64(raw) * c.factor)
	c.mu.Unlock()
	return scaled, nil
}

// Observe records one completed turn: estimated is the value
// EstimateTokens returned to the caller for that turn - the
// already-corrected figure, not a raw pre-scale count - and actual is
// the real provider.Usage.TotalTokens for the same turn. Observe
// corrects factor multiplicatively against estimated, so the fixed
// point is unchanged from before this fix: a corrected estimate that
// tracks actual. The result is clamped to [MinCorrectionFactor,
// MaxCorrectionFactor]. A non-positive estimated or a non-positive
// actual is a no-op.
func (c *Calibrated) Observe(estimated, actual int) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if estimated <= 0 || actual <= 0 {
		return
	}
	sample := float64(actual) / float64(estimated)
	next := c.factor * ((1 - c.alpha) + c.alpha*sample)
	if next < MinCorrectionFactor {
		next = MinCorrectionFactor
	}
	if next > MaxCorrectionFactor {
		next = MaxCorrectionFactor
	}
	c.factor = next
}
