package contextplan

import "github.com/MiviaLabs/mivia-ai-sdk/provider"

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
type Calibrated struct {
	est     provider.TokenEstimator
	alpha   float64
	factor  float64
	lastEst int
	hasLast bool
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
	scaled := int(float64(raw) * c.factor)
	c.lastEst = raw
	c.hasLast = true
	return scaled, nil
}

// Observe records one completed turn's real provider.Usage.TotalTokens
// against the last estimate this Calibrated produced, and updates the
// correction factor by alpha, clamped to [MinCorrectionFactor,
// MaxCorrectionFactor]. A first call, before any estimate, and a
// non-positive actual, are both no-ops.
func (c *Calibrated) Observe(actual int) {
	if !c.hasLast || actual <= 0 || c.lastEst <= 0 {
		return
	}
	sample := float64(actual) / float64(c.lastEst)
	next := (1-c.alpha)*c.factor + c.alpha*sample
	if next < MinCorrectionFactor {
		next = MinCorrectionFactor
	}
	if next > MaxCorrectionFactor {
		next = MaxCorrectionFactor
	}
	c.factor = next
}
