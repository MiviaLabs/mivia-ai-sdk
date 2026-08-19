package contextplan_test

import (
	"errors"
	"math"
	"sync"
	"testing"

	"github.com/MiviaLabs/mivia-ai-sdk/contextplan"
	"github.com/MiviaLabs/mivia-ai-sdk/provider"
)

// fixedEstimator always reports the same raw token count.
type fixedEstimator struct{ tokens int }

func (f fixedEstimator) EstimateTokens(provider.Request) (int, error) {
	return f.tokens, nil
}

func TestCalibratedEWMAConverges(t *testing.T) {
	c := contextplan.Calibrate(fixedEstimator{tokens: 100}, 0.5)
	for i := 0; i < 20; i++ {
		if _, err := c.EstimateTokens(provider.Request{}); err != nil {
			t.Fatalf("EstimateTokens: %v", err)
		}
		c.Observe(150)
	}
	got, err := c.EstimateTokens(provider.Request{})
	if err != nil {
		t.Fatalf("EstimateTokens: %v", err)
	}
	if math.Abs(float64(got)-150) > 5 {
		t.Fatalf("EstimateTokens = %d, want close to 150 after convergence", got)
	}
}

func TestCalibratedBoundsClamp(t *testing.T) {
	c := contextplan.Calibrate(fixedEstimator{tokens: 10}, 1.0)
	if _, err := c.EstimateTokens(provider.Request{}); err != nil {
		t.Fatalf("EstimateTokens: %v", err)
	}
	c.Observe(1000000)
	got, err := c.EstimateTokens(provider.Request{})
	if err != nil {
		t.Fatalf("EstimateTokens: %v", err)
	}
	if float64(got) > 10*contextplan.MaxCorrectionFactor+1 {
		t.Fatalf("EstimateTokens = %d, correction factor exceeded MaxCorrectionFactor", got)
	}

	c2 := contextplan.Calibrate(fixedEstimator{tokens: 10}, 1.0)
	if _, err := c2.EstimateTokens(provider.Request{}); err != nil {
		t.Fatalf("EstimateTokens: %v", err)
	}
	c2.Observe(1)
	got2, err := c2.EstimateTokens(provider.Request{})
	if err != nil {
		t.Fatalf("EstimateTokens: %v", err)
	}
	if float64(got2) < 10*contextplan.MinCorrectionFactor-1 {
		t.Fatalf("EstimateTokens = %d, correction factor fell below MinCorrectionFactor", got2)
	}
}

func TestCalibratedAlphaSelection(t *testing.T) {
	for _, alpha := range []float64{0, -1, -0.5, 1.5, 2} {
		c := contextplan.Calibrate(fixedEstimator{tokens: 100}, alpha)
		if _, err := c.EstimateTokens(provider.Request{}); err != nil {
			t.Fatalf("EstimateTokens: %v", err)
		}
		before, err := c.EstimateTokens(provider.Request{})
		if err != nil {
			t.Fatalf("EstimateTokens: %v", err)
		}
		c.Observe(200)
		after, err := c.EstimateTokens(provider.Request{})
		if err != nil {
			t.Fatalf("EstimateTokens: %v", err)
		}
		if after <= before {
			t.Fatalf("alpha %v: correction did not move estimate toward observed actual", alpha)
		}
	}

	slow := contextplan.Calibrate(fixedEstimator{tokens: 100}, 0.05)
	fast := contextplan.Calibrate(fixedEstimator{tokens: 100}, 0.9)
	if _, err := slow.EstimateTokens(provider.Request{}); err != nil {
		t.Fatalf("EstimateTokens: %v", err)
	}
	if _, err := fast.EstimateTokens(provider.Request{}); err != nil {
		t.Fatalf("EstimateTokens: %v", err)
	}
	slow.Observe(200)
	fast.Observe(200)
	slowEst, err := slow.EstimateTokens(provider.Request{})
	if err != nil {
		t.Fatalf("EstimateTokens: %v", err)
	}
	fastEst, err := fast.EstimateTokens(provider.Request{})
	if err != nil {
		t.Fatalf("EstimateTokens: %v", err)
	}
	if fastEst <= slowEst {
		t.Fatalf("fast alpha (%d) did not converge faster than slow alpha (%d)", fastEst, slowEst)
	}
}

// TestCalibratedAlphaAboveOneFallsBackToDefault pins the fallback
// for alpha > 1, the half of the "outside (0, 1]" range the loose
// before/after comparisons in TestCalibratedAlphaSelection cannot
// tell apart from a broken implementation that used the raw
// out-of-range alpha instead of DefaultSmoothingFactor. A Calibrated
// built with alpha=1.5 must land on the exact same factor, after one
// Observe, as one built with the explicit default.
func TestCalibratedAlphaAboveOneFallsBackToDefault(t *testing.T) {
	withDefault := contextplan.Calibrate(fixedEstimator{tokens: 100}, contextplan.DefaultSmoothingFactor)
	aboveOne := contextplan.Calibrate(fixedEstimator{tokens: 100}, 1.5)
	if _, err := withDefault.EstimateTokens(provider.Request{}); err != nil {
		t.Fatalf("EstimateTokens: %v", err)
	}
	if _, err := aboveOne.EstimateTokens(provider.Request{}); err != nil {
		t.Fatalf("EstimateTokens: %v", err)
	}
	withDefault.Observe(200)
	aboveOne.Observe(200)
	wantTokens, err := withDefault.EstimateTokens(provider.Request{})
	if err != nil {
		t.Fatalf("EstimateTokens: %v", err)
	}
	gotTokens, err := aboveOne.EstimateTokens(provider.Request{})
	if err != nil {
		t.Fatalf("EstimateTokens: %v", err)
	}
	if gotTokens != wantTokens {
		t.Fatalf("alpha=1.5 EstimateTokens = %d, want %d (the DefaultSmoothingFactor fallback result)", gotTokens, wantTokens)
	}
}

func TestCalibratedZeroEstimatesSkip(t *testing.T) {
	c := contextplan.Calibrate(fixedEstimator{tokens: 0}, 0.5)
	got, err := c.EstimateTokens(provider.Request{})
	if err != nil {
		t.Fatalf("EstimateTokens: %v", err)
	}
	if got != 0 {
		t.Fatalf("EstimateTokens = %d, want 0", got)
	}
	c.Observe(0)
	got2, err := c.EstimateTokens(provider.Request{})
	if err != nil {
		t.Fatalf("EstimateTokens: %v", err)
	}
	if got2 != 0 {
		t.Fatalf("EstimateTokens after Observe(0) = %d, want 0", got2)
	}
}

// TestCalibratedNegativeActualNoop pins the other half of "a
// non-positive actual is a no-op": Observe(0) alone does not prove a
// negative actual is also rejected, since both a strict "actual <= 0"
// guard and a narrower "actual == 0" guard pass that case identically.
func TestCalibratedNegativeActualNoop(t *testing.T) {
	c := contextplan.Calibrate(fixedEstimator{tokens: 100}, 0.5)
	if _, err := c.EstimateTokens(provider.Request{}); err != nil {
		t.Fatalf("EstimateTokens: %v", err)
	}
	c.Observe(-50)
	got, err := c.EstimateTokens(provider.Request{})
	if err != nil {
		t.Fatalf("EstimateTokens: %v", err)
	}
	if got != 100 {
		t.Fatalf("EstimateTokens = %d, want 100: Observe(-50) must be a no-op", got)
	}
}

func TestCalibratedFirstObserveNoop(t *testing.T) {
	c := contextplan.Calibrate(fixedEstimator{tokens: 100}, 0.5)
	c.Observe(9999)
	got, err := c.EstimateTokens(provider.Request{})
	if err != nil {
		t.Fatalf("EstimateTokens: %v", err)
	}
	if got != 100 {
		t.Fatalf("EstimateTokens = %d, want 100: an Observe before any estimate must be a no-op", got)
	}
}

func TestCalibratedEstimateTokensPropagatesError(t *testing.T) {
	wantErr := errors.New("boom")
	c := contextplan.Calibrate(byteEstimator{err: wantErr}, 0.5)
	_, err := c.EstimateTokens(provider.Request{})
	if !errors.Is(err, wantErr) {
		t.Fatalf("err = %v, want %v", err, wantErr)
	}
}

func TestCalibratedConcurrentUse(t *testing.T) {
	c := contextplan.Calibrate(fixedEstimator{tokens: 100}, 0.5)
	var wg sync.WaitGroup
	for g := 0; g < 8; g++ {
		wg.Add(1)
		go func(seed int) {
			defer wg.Done()
			for i := 0; i < 50; i++ {
				got, err := c.EstimateTokens(provider.Request{})
				if err != nil {
					t.Errorf("EstimateTokens: %v", err)
					return
				}
				if got < 50 || got > 200 {
					t.Errorf("estimate %d outside the clamp bounds [50, 200]", got)
					return
				}
				c.Observe(100 + seed)
			}
		}(g)
	}
	wg.Wait()
	got, err := c.EstimateTokens(provider.Request{})
	if err != nil {
		t.Fatalf("EstimateTokens after join: %v", err)
	}
	if got < int(100*contextplan.MinCorrectionFactor) || got > int(100*contextplan.MaxCorrectionFactor) {
		t.Fatalf("estimate %d outside the clamp bounds after every join", got)
	}
}
