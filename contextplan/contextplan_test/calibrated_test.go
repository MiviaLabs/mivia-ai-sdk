package contextplan_test

import (
	"errors"
	"math"
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
	for _, alpha := range []float64{0, -1, -0.5} {
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
