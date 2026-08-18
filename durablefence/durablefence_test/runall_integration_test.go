package durablefence_test

import (
	"context"
	"testing"

	"github.com/MiviaLabs/mivia-ai-sdk/durablefence"
)

// TestRunAllComposesOverOneScenario calls RunAll once against the
// reference implementation and asserts no subtest failed, proving the
// full suite composes over one Scenario value with no field reused
// incorrectly between checks.
func TestRunAllComposesOverOneScenario(t *testing.T) {
	ctx := context.Background()
	r := newReferenceClaim()
	s := r.scenario()
	ok := t.Run("RunAll", func(t *testing.T) {
		durablefence.RunAll(t, ctx, s)
	})
	if !ok {
		t.Fatal("RunAll reported a failing subtest against a correct reference implementation")
	}
}
