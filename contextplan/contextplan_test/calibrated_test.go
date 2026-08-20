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

// referenceObserve computes the reference formula every test in this
// file pins Observe against: sample := actual/estimated, then
// next := factor * ((1-alpha) + alpha*sample), clamped to
// [MinCorrectionFactor, MaxCorrectionFactor]. A case that only asserts
// a hand-computed expectation with no stated formula would be
// incomplete; every case in this file states or reuses this one.
func referenceObserve(factor, alpha float64, estimated, actual int) float64 {
	if estimated <= 0 || actual <= 0 {
		return factor
	}
	sample := float64(actual) / float64(estimated)
	next := factor * ((1 - alpha) + alpha*sample)
	if next < contextplan.MinCorrectionFactor {
		next = contextplan.MinCorrectionFactor
	}
	if next > contextplan.MaxCorrectionFactor {
		next = contextplan.MaxCorrectionFactor
	}
	return next
}

func TestCalibratedEWMAConverges(t *testing.T) {
	c := contextplan.Calibrate(fixedEstimator{tokens: 100}, 0.5)
	for i := 0; i < 20; i++ {
		got, err := c.EstimateTokens(provider.Request{})
		if err != nil {
			t.Fatalf("EstimateTokens: %v", err)
		}
		c.Observe(got, 150)
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
	got, err := c.EstimateTokens(provider.Request{})
	if err != nil {
		t.Fatalf("EstimateTokens: %v", err)
	}
	c.Observe(got, 1000000)
	got2, err := c.EstimateTokens(provider.Request{})
	if err != nil {
		t.Fatalf("EstimateTokens: %v", err)
	}
	if float64(got2) > 10*contextplan.MaxCorrectionFactor+1 {
		t.Fatalf("EstimateTokens = %d, correction factor exceeded MaxCorrectionFactor", got2)
	}

	c2 := contextplan.Calibrate(fixedEstimator{tokens: 10}, 1.0)
	gotC2, err := c2.EstimateTokens(provider.Request{})
	if err != nil {
		t.Fatalf("EstimateTokens: %v", err)
	}
	c2.Observe(gotC2, 1)
	got2c2, err := c2.EstimateTokens(provider.Request{})
	if err != nil {
		t.Fatalf("EstimateTokens: %v", err)
	}
	if float64(got2c2) < 10*contextplan.MinCorrectionFactor-1 {
		t.Fatalf("EstimateTokens = %d, correction factor fell below MinCorrectionFactor", got2c2)
	}
}

func TestCalibratedAlphaSelection(t *testing.T) {
	for _, alpha := range []float64{0, -1, -0.5, 1.5, 2} {
		c := contextplan.Calibrate(fixedEstimator{tokens: 100}, alpha)
		before, err := c.EstimateTokens(provider.Request{})
		if err != nil {
			t.Fatalf("EstimateTokens: %v", err)
		}
		c.Observe(before, 200)
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
	slowBefore, err := slow.EstimateTokens(provider.Request{})
	if err != nil {
		t.Fatalf("EstimateTokens: %v", err)
	}
	fastBefore, err := fast.EstimateTokens(provider.Request{})
	if err != nil {
		t.Fatalf("EstimateTokens: %v", err)
	}
	slow.Observe(slowBefore, 200)
	fast.Observe(fastBefore, 200)
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
	withDefaultEst, err := withDefault.EstimateTokens(provider.Request{})
	if err != nil {
		t.Fatalf("EstimateTokens: %v", err)
	}
	aboveOneEst, err := aboveOne.EstimateTokens(provider.Request{})
	if err != nil {
		t.Fatalf("EstimateTokens: %v", err)
	}
	withDefault.Observe(withDefaultEst, 200)
	aboveOne.Observe(aboveOneEst, 200)
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
	c.Observe(got, 0)
	got2, err := c.EstimateTokens(provider.Request{})
	if err != nil {
		t.Fatalf("EstimateTokens: %v", err)
	}
	if got2 != 0 {
		t.Fatalf("EstimateTokens after Observe(0, 0) = %d, want 0", got2)
	}
}

// TestCalibratedNegativeActualNoop pins the other half of "a
// non-positive actual is a no-op": Observe(estimated, 0) alone does
// not prove a negative actual is also rejected, since both a strict
// "actual <= 0" guard and a narrower "actual == 0" guard pass that
// case identically.
func TestCalibratedNegativeActualNoop(t *testing.T) {
	c := contextplan.Calibrate(fixedEstimator{tokens: 100}, 0.5)
	est, err := c.EstimateTokens(provider.Request{})
	if err != nil {
		t.Fatalf("EstimateTokens: %v", err)
	}
	c.Observe(est, -50)
	got, err := c.EstimateTokens(provider.Request{})
	if err != nil {
		t.Fatalf("EstimateTokens: %v", err)
	}
	if got != 100 {
		t.Fatalf("EstimateTokens = %d, want 100: Observe(estimated, -50) must be a no-op", got)
	}
}

// TestCalibratedNonPositiveEstimatedNoop covers Observe(0, actual) and
// Observe(-5, actual): both leave factor unchanged. This replaces the
// removed "first call, before any estimate" no-op rule, which no
// longer applies once Observe takes its own estimate argument.
func TestCalibratedNonPositiveEstimatedNoop(t *testing.T) {
	for _, estimated := range []int{0, -5} {
		c := contextplan.Calibrate(fixedEstimator{tokens: 100}, 0.5)
		c.Observe(estimated, 9999)
		got, err := c.EstimateTokens(provider.Request{})
		if err != nil {
			t.Fatalf("EstimateTokens: %v", err)
		}
		if got != 100 {
			t.Fatalf("estimated=%d: EstimateTokens = %d, want 100: Observe must be a no-op", estimated, got)
		}
	}
}

// TestCalibratedObserveWithNoPriorEstimate proves a fresh *Calibrated
// applies Observe on its very first call, with no EstimateTokens call
// at all beforehand: the removed "first call is a no-op" rule left no
// equivalent gate behind, since every Observe call now carries its own
// estimate.
func TestCalibratedObserveWithNoPriorEstimate(t *testing.T) {
	c := contextplan.Calibrate(fixedEstimator{tokens: 100}, 0.5)
	c.Observe(100, 120)
	want := referenceObserve(1.0, 0.5, 100, 120)
	got, err := c.EstimateTokens(provider.Request{})
	if err != nil {
		t.Fatalf("EstimateTokens: %v", err)
	}
	wantTokens := int(100 * want)
	if got != wantTokens {
		t.Fatalf("EstimateTokens = %d, want %d (factor %v)", got, wantTokens, want)
	}
}

// TestCalibratedConvergesTowardActualNotGeometricMean is the case
// that distinguishes the multiplicative formula this fix requires
// from a divisive drift: the divisive formula converges the scaled
// estimate near sqrt(raw*actual), a different number whenever
// raw != actual, so this assertion fails against it.
func TestCalibratedConvergesTowardActualNotGeometricMean(t *testing.T) {
	const raw = 100
	const actual = 150
	c := contextplan.Calibrate(fixedEstimator{tokens: raw}, 0.5)
	var last float64 = math.MaxFloat64
	for i := 0; i < 40; i++ {
		estimated, err := c.EstimateTokens(provider.Request{})
		if err != nil {
			t.Fatalf("EstimateTokens: %v", err)
		}
		c.Observe(estimated, actual)
		dist := math.Abs(float64(estimated) - actual)
		if dist > last+1e-9 {
			t.Fatalf("turn %d: estimate %d moved away from actual %d", i, estimated, actual)
		}
		last = dist
	}
	final, err := c.EstimateTokens(provider.Request{})
	if err != nil {
		t.Fatalf("EstimateTokens: %v", err)
	}
	if math.Abs(float64(final)-actual) > 5 {
		t.Fatalf("EstimateTokens = %d, want close to actual %d", final, actual)
	}
	geoMean := math.Sqrt(raw * actual)
	if math.Abs(float64(final)-geoMean) < 5 {
		t.Fatalf("EstimateTokens = %d landed near the geometric mean %v instead of actual %d: divisive drift", final, geoMean, actual)
	}
}

// TestCalibratedObserveOutOfEstimateOrder proves the factor depends
// only on the arguments passed to each Observe call, not on call
// order or on any earlier EstimateTokens call: two EstimateTokens
// calls run back to back, then two Observe calls run in reverse order
// from how the estimates were produced.
func TestCalibratedObserveOutOfEstimateOrder(t *testing.T) {
	c := contextplan.Calibrate(fixedEstimator{tokens: 100}, 0.4)
	est1, err := c.EstimateTokens(provider.Request{})
	if err != nil {
		t.Fatalf("EstimateTokens: %v", err)
	}
	est2, err := c.EstimateTokens(provider.Request{})
	if err != nil {
		t.Fatalf("EstimateTokens: %v", err)
	}
	// est1 == est2 here since factor has not moved yet; Observe the
	// second estimate first, then the first, to prove call order
	// alone (not estimate production order) drives the outcome.
	c.Observe(est2, 500)
	want := referenceObserve(1.0, 0.4, est2, 500)
	c.Observe(est1, 50)
	want = referenceObserve(want, 0.4, est1, 50)

	got, err := c.EstimateTokens(provider.Request{})
	if err != nil {
		t.Fatalf("EstimateTokens: %v", err)
	}
	wantTokens := int(100 * want)
	if got != wantTokens {
		t.Fatalf("EstimateTokens = %d, want %d (reference factor %v)", got, wantTokens, want)
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

// TestCalibratedConcurrentUse runs N goroutines, each holding its own
// known (estimated, actual) pair distinct from every other goroutine's
// pair, calling Observe concurrently on one shared *Calibrated. It
// asserts the resulting factor lands within the reachable range of the
// reference formula folded over every pair in some order:
// order-independence is not claimed, since EWMA is order-dependent by
// construction, but the range across every valid application order is
// asserted, beyond mere absence of a panic.
func TestCalibratedConcurrentUse(t *testing.T) {
	c := contextplan.Calibrate(fixedEstimator{tokens: 100}, 0.5)
	const n = 8
	pairs := make([][2]int, n)
	for g := 0; g < n; g++ {
		pairs[g] = [2]int{100 + g, 100 + 10*g}
	}

	var wg sync.WaitGroup
	for g := 0; g < n; g++ {
		wg.Add(1)
		go func(estimated, actual int) {
			defer wg.Done()
			c.Observe(estimated, actual)
		}(pairs[g][0], pairs[g][1])
	}
	wg.Wait()

	got, err := c.EstimateTokens(provider.Request{})
	if err != nil {
		t.Fatalf("EstimateTokens after join: %v", err)
	}
	if got < int(100*contextplan.MinCorrectionFactor) || got > int(100*contextplan.MaxCorrectionFactor) {
		t.Fatalf("estimate %d outside the clamp bounds after every join", got)
	}

	min, max := reachableFactorRange(1.0, 0.5, pairs)
	gotFactor := float64(got) / 100
	if gotFactor < min-1e-9 || gotFactor > max+1e-9 {
		t.Fatalf("factor %v outside the reachable range [%v, %v] for every application order of the reference formula", gotFactor, min, max)
	}
}

// reachableFactorRange folds the reference formula over every pair in
// pairs, trying every permutation, and returns the min and max
// resulting factor across all of them.
func reachableFactorRange(startFactor, alpha float64, pairs [][2]int) (float64, float64) {
	idx := make([]int, len(pairs))
	for i := range idx {
		idx[i] = i
	}
	min := math.MaxFloat64
	max := -math.MaxFloat64
	permute(idx, 0, func(order []int) {
		factor := startFactor
		for _, i := range order {
			factor = referenceObserve(factor, alpha, pairs[i][0], pairs[i][1])
		}
		if factor < min {
			min = factor
		}
		if factor > max {
			max = factor
		}
	})
	return min, max
}

// permute calls fn on every permutation of idx, generated in place via
// Heap's algorithm.
func permute(idx []int, k int, fn func([]int)) {
	if k == len(idx) {
		cp := append([]int(nil), idx...)
		fn(cp)
		return
	}
	for i := k; i < len(idx); i++ {
		idx[k], idx[i] = idx[i], idx[k]
		permute(idx, k+1, fn)
		idx[k], idx[i] = idx[i], idx[k]
	}
}
