package durablefence_test

import (
	"context"
	"testing"

	"github.com/MiviaLabs/mivia-ai-sdk/durablefence"
)

// runIsolated runs a check under a real *testing.T, in a test
// hierarchy detached from the caller's own *testing.T, so a check's
// deliberate failure never fails the enclosing test. A subtest run
// through t.Run always marks every ancestor test failed on a child
// failure; testing.RunTests instead builds a fresh, standalone root,
// letting the caller read the pass/fail outcome as a plain bool. No
// fake testing.TB is used; run executes against a genuine *testing.T.
func runIsolated(name string, run func(t *testing.T)) bool {
	matchAll := func(string, string) (bool, error) { return true, nil }
	return testing.RunTests(matchAll, []testing.InternalTest{{Name: name, F: run}})
}

// TestCheckTakeoverFencesPreviousOwnerCatchesBrokenTakeover proves
// CheckTakeoverFencesPreviousOwner fails against a Takeover that
// reassigns ownership but never marks the previous token fenced.
func TestCheckTakeoverFencesPreviousOwnerCatchesBrokenTakeover(t *testing.T) {
	ctx := context.Background()
	r := newReferenceClaim()
	s := r.scenario()
	s.Takeover = func(context.Context) (string, error) {
		r.mu.Lock()
		defer r.mu.Unlock()
		r.owner = r.newToken()
		r.held = true
		return r.owner, nil
	}
	ok := runIsolated("CheckTakeoverFencesPreviousOwner", func(t *testing.T) {
		durablefence.CheckTakeoverFencesPreviousOwner(t, ctx, s)
	})
	if ok {
		t.Fatal("check passed against a Takeover that never fences the previous token")
	}
}

// TestCheckTakeoverFencesConcurrentMutateCatchesBrokenTakeover proves
// CheckTakeoverFencesConcurrentMutate fails against a Takeover that
// returns a fresh token without actually reassigning ownership, so a
// racing Mutate(A) keeps succeeding after Takeover returns.
func TestCheckTakeoverFencesConcurrentMutateCatchesBrokenTakeover(t *testing.T) {
	ctx := context.Background()
	r := newReferenceClaim()
	s := r.scenario()
	s.Takeover = func(context.Context) (string, error) {
		r.mu.Lock()
		defer r.mu.Unlock()
		return r.newToken(), nil
	}
	ok := runIsolated("CheckTakeoverFencesConcurrentMutate", func(t *testing.T) {
		durablefence.CheckTakeoverFencesConcurrentMutate(t, ctx, s)
	})
	if ok {
		t.Fatal("check passed against a Takeover that does not fence a concurrent Mutate")
	}
}

// TestCheckReleaseClearsHoldCatchesBrokenRelease proves
// CheckReleaseClearsHold fails against a Release that reports success
// without clearing the hold.
func TestCheckReleaseClearsHoldCatchesBrokenRelease(t *testing.T) {
	ctx := context.Background()
	r := newReferenceClaim()
	s := r.scenario()
	s.Release = func(context.Context, string) error {
		return nil
	}
	ok := runIsolated("CheckReleaseClearsHold", func(t *testing.T) {
		durablefence.CheckReleaseClearsHold(t, ctx, s)
	})
	if ok {
		t.Fatal("check passed against a Release that never clears the hold")
	}
}

// TestCheckClaimRejectsWhileHeldCatchesBrokenClaim proves
// CheckClaimRejectsWhileHeld fails against a Claim that grants a
// second hold over an already-held resource.
func TestCheckClaimRejectsWhileHeldCatchesBrokenClaim(t *testing.T) {
	ctx := context.Background()
	r := newReferenceClaim()
	s := r.scenario()
	s.Claim = func(context.Context) (string, error) {
		r.mu.Lock()
		defer r.mu.Unlock()
		r.owner = r.newToken()
		r.held = true
		return r.owner, nil
	}
	ok := runIsolated("CheckClaimRejectsWhileHeld", func(t *testing.T) {
		durablefence.CheckClaimRejectsWhileHeld(t, ctx, s)
	})
	if ok {
		t.Fatal("check passed against a Claim that grants a second hold while already held")
	}
}

// TestCheckClaimGrantsHoldCatchesBrokenClaim proves
// CheckClaimGrantsHold fails against a Claim that issues a token
// without setting the hold.
func TestCheckClaimGrantsHoldCatchesBrokenClaim(t *testing.T) {
	ctx := context.Background()
	r := newReferenceClaim()
	s := r.scenario()
	s.Claim = func(context.Context) (string, error) {
		r.mu.Lock()
		defer r.mu.Unlock()
		return r.newToken(), nil
	}
	ok := runIsolated("CheckClaimGrantsHold", func(t *testing.T) {
		durablefence.CheckClaimGrantsHold(t, ctx, s)
	})
	if ok {
		t.Fatal("check passed against a Claim that does not set the hold")
	}
}

// TestCheckIsFencedFalseForUnknownTokenCatchesBrokenIsFenced proves
// CheckIsFencedFalseForUnknownToken fails against an IsFenced that
// reports true for a token that was never issued.
func TestCheckIsFencedFalseForUnknownTokenCatchesBrokenIsFenced(t *testing.T) {
	ctx := context.Background()
	r := newReferenceClaim()
	s := r.scenario()
	s.IsFenced = func(context.Context, string) (bool, error) {
		return true, nil
	}
	ok := runIsolated("CheckIsFencedFalseForUnknownToken", func(t *testing.T) {
		durablefence.CheckIsFencedFalseForUnknownToken(t, ctx, s)
	})
	if ok {
		t.Fatal("check passed against an IsFenced that always reports true")
	}
}

// TestCheckClaimGrantsHoldCatchesBrokenIsFencedDefaultTrue proves
// CheckClaimGrantsHold fails against an IsFenced that defaults to
// true for any active, held token, not only for a truly unknown one.
func TestCheckClaimGrantsHoldCatchesBrokenIsFencedDefaultTrue(t *testing.T) {
	ctx := context.Background()
	r := newReferenceClaim()
	s := r.scenario()
	s.IsFenced = func(_ context.Context, token string) (bool, error) {
		r.mu.Lock()
		defer r.mu.Unlock()
		if r.held {
			return true, nil
		}
		return r.fenced[token], nil
	}
	ok := runIsolated("CheckClaimGrantsHold", func(t *testing.T) {
		durablefence.CheckClaimGrantsHold(t, ctx, s)
	})
	if ok {
		t.Fatal("check passed against an IsFenced that defaults true for any held token")
	}
}
