package durablefence_test

import (
	"context"
	"testing"

	"github.com/MiviaLabs/mivia-ai-sdk/durablefence"
)

// releaseOnFailureCase names one Check* function and a Scenario
// mutation that makes exactly one of its later assertions fail,
// without corrupting the underlying reference's real state.
type releaseOnFailureCase struct {
	name  string
	check string
	build func(r *referenceClaim) durablefence.Scenario
}

// releaseOnFailureChecks maps each case's check name to the real
// Check* function under test.
var releaseOnFailureChecks = map[string]func(testing.TB, context.Context, durablefence.Scenario){
	"CheckClaimGrantsHold":                durablefence.CheckClaimGrantsHold,
	"CheckClaimRejectsWhileHeld":          durablefence.CheckClaimRejectsWhileHeld,
	"CheckTakeoverFencesPreviousOwner":    durablefence.CheckTakeoverFencesPreviousOwner,
	"CheckTakeoverFencesConcurrentMutate": durablefence.CheckTakeoverFencesConcurrentMutate,
	"CheckMutateSucceedsForCurrentOwner":  durablefence.CheckMutateSucceedsForCurrentOwner,
}

// releaseOnFailureCases lies on one Scenario field per case, chosen
// so the check's own assertion fails via t.Fatal after the check has
// already claimed the resource, but the reference's real hold state
// stays accurate throughout.
var releaseOnFailureCases = []releaseOnFailureCase{
	{
		name:  "CheckClaimGrantsHold/IsFenced lies true",
		check: "CheckClaimGrantsHold",
		build: func(r *referenceClaim) durablefence.Scenario {
			s := r.scenario()
			s.IsFenced = func(context.Context, string) (bool, error) { return true, nil }
			return s
		},
	},
	{
		name:  "CheckClaimRejectsWhileHeld/second Claim lies success",
		check: "CheckClaimRejectsWhileHeld",
		build: func(r *referenceClaim) durablefence.Scenario {
			s := r.scenario()
			first := true
			s.Claim = func(ctx context.Context) (string, error) {
				if first {
					first = false
					return r.claim(ctx)
				}
				// Lies about a second hold without touching the real
				// reference state, so the eventual Release call still
				// targets the real, still-valid owner.
				return "bogus-token", nil
			}
			return s
		},
	},
	{
		name:  "CheckTakeoverFencesPreviousOwner/IsFenced lies false",
		check: "CheckTakeoverFencesPreviousOwner",
		build: func(r *referenceClaim) durablefence.Scenario {
			s := r.scenario()
			s.IsFenced = func(context.Context, string) (bool, error) { return false, nil }
			return s
		},
	},
	{
		name:  "CheckTakeoverFencesConcurrentMutate/IsFenced lies false",
		check: "CheckTakeoverFencesConcurrentMutate",
		build: func(r *referenceClaim) durablefence.Scenario {
			s := r.scenario()
			s.IsFenced = func(context.Context, string) (bool, error) { return false, nil }
			return s
		},
	},
	{
		name:  "CheckMutateSucceedsForCurrentOwner/Mutate lies error",
		check: "CheckMutateSucceedsForCurrentOwner",
		build: func(r *referenceClaim) durablefence.Scenario {
			s := r.scenario()
			// Lies about the current, non-fenced owner's Mutate call
			// failing, without touching the real reference state, so the
			// eventual deferred Release still targets the real owner.
			s.Mutate = func(context.Context, string) error { return errAlwaysErrorsMutate }
			return s
		},
	},
}

// runReleaseOnFailureCase runs c against a fresh reference and fails
// t when the check passes, or when the reference is still held once
// the check has returned.
func runReleaseOnFailureCase(t *testing.T, ctx context.Context, c releaseOnFailureCase) {
	t.Helper()
	r := newReferenceClaim()
	s := c.build(r)
	run := releaseOnFailureChecks[c.check]
	ok := runIsolated(c.check, func(t *testing.T) {
		run(t, ctx, s)
	})
	if ok {
		t.Fatalf("%s passed against a lying Scenario field, want it to fail", c.check)
	}
	held, err := r.isHeld(ctx)
	if err != nil {
		t.Fatalf("isHeld: %v", err)
	}
	if held {
		t.Fatalf("%s left the resource held after a t.Fatal partway through; "+
			"the deferred Release did not run on the failure path", c.check)
	}
}

// TestCheckReleasesHoldOnAssertionFailure proves every affected
// Check* function still releases its hold when a later assertion
// fails via t.Fatal, not only on the pass path. Without a deferred
// release keyed to the current owner token, a t.Fatal mid-check
// skips the trailing Release call and leaves the reference held for
// whatever check runs next.
func TestCheckReleasesHoldOnAssertionFailure(t *testing.T) {
	ctx := context.Background()
	for _, c := range releaseOnFailureCases {
		t.Run(c.name, func(t *testing.T) {
			runReleaseOnFailureCase(t, ctx, c)
		})
	}
}
