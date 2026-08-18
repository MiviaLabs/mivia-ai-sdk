package durablefence

import (
	"context"
	"sync"
	"sync/atomic"
	"testing"
)

// concurrentMutatePostTakeoverSamples is how many Mutate(A) calls
// CheckTakeoverFencesConcurrentMutate observes after Takeover has
// returned, before it stops the racing goroutine.
const concurrentMutatePostTakeoverSamples = 20

// releaseHold calls s.Release with the token *tokenPtr holds at the
// moment this function runs, not the token captured when it was
// deferred. Every Check* function defers this call right after its
// first successful Claim, so the resource is released on every
// return path, including a t.Fatal that unwinds the goroutine via
// runtime.Goexit. A caller that reassigns *tokenPtr after a
// successful Takeover releases the new owner, not the fenced one.
func releaseHold(t testing.TB, ctx context.Context, s Scenario, tokenPtr *string) {
	t.Helper()
	if err := s.Release(ctx, *tokenPtr); err != nil {
		t.Errorf("Release: %v", err)
	}
}

// CheckClaimGrantsHold claims a fresh resource and asserts IsHeld
// reports true afterward, proving a successful Claim is visible to a
// caller that only reads hold state. It also asserts IsFenced reports
// false for the just-claimed, still-active token, proving IsFenced
// does not default to true for a token that has never been fenced.
// It releases the hold before returning.
func CheckClaimGrantsHold(t testing.TB, ctx context.Context, s Scenario) {
	t.Helper()
	if err := s.Validate(); err != nil {
		t.Fatal(err)
	}
	token, err := s.Claim(ctx)
	if err != nil {
		t.Fatalf("Claim: %v", err)
	}
	defer releaseHold(t, ctx, s, &token)
	held, err := s.IsHeld(ctx)
	if err != nil {
		t.Fatalf("IsHeld: %v", err)
	}
	if !held {
		t.Fatal("IsHeld = false, want true after Claim")
	}
	fenced, err := s.IsFenced(ctx, token)
	if err != nil {
		t.Fatalf("IsFenced: %v", err)
	}
	if fenced {
		t.Fatal("IsFenced = true, want false for the active, just-claimed token")
	}
}

// CheckClaimRejectsWhileHeld claims a fresh resource with token A,
// then calls Claim again on the same resource and asserts the second
// call returns a non-nil error, proving Claim does not grant a
// second, independent hold over a resource token A already owns. It
// releases the hold under token A before returning.
func CheckClaimRejectsWhileHeld(t testing.TB, ctx context.Context, s Scenario) {
	t.Helper()
	if err := s.Validate(); err != nil {
		t.Fatal(err)
	}
	tokenA, err := s.Claim(ctx)
	if err != nil {
		t.Fatalf("Claim: %v", err)
	}
	defer releaseHold(t, ctx, s, &tokenA)
	if _, err := s.Claim(ctx); err == nil {
		t.Fatal("second Claim = nil error, want non-nil while the resource is held")
	}
}

// CheckReleaseClearsHold claims, releases, and asserts IsHeld reports
// false afterward, proving Release clears the hold so a later Claim
// can reclaim the resource. The resource is already unheld when this
// check returns.
func CheckReleaseClearsHold(t testing.TB, ctx context.Context, s Scenario) {
	t.Helper()
	if err := s.Validate(); err != nil {
		t.Fatal(err)
	}
	token, err := s.Claim(ctx)
	if err != nil {
		t.Fatalf("Claim: %v", err)
	}
	if err := s.Release(ctx, token); err != nil {
		t.Fatalf("Release: %v", err)
	}
	held, err := s.IsHeld(ctx)
	if err != nil {
		t.Fatalf("IsHeld: %v", err)
	}
	if held {
		t.Fatal("IsHeld = true, want false after Release")
	}
}

// CheckTakeoverFencesPreviousOwner claims with token A, takes over
// with token B, and asserts Mutate under token A now fails and
// IsFenced reports true for token A. It proves a subsequent Mutate(A)
// call, made after the takeover has already completed, is rejected.
// It releases the hold under token B before returning.
func CheckTakeoverFencesPreviousOwner(t testing.TB, ctx context.Context, s Scenario) {
	t.Helper()
	if err := s.Validate(); err != nil {
		t.Fatal(err)
	}
	tokenA, err := s.Claim(ctx)
	if err != nil {
		t.Fatalf("Claim: %v", err)
	}
	current := tokenA
	defer releaseHold(t, ctx, s, &current)
	tokenB, err := s.Takeover(ctx)
	if err != nil {
		t.Fatalf("Takeover: %v", err)
	}
	current = tokenB
	if err := s.Mutate(ctx, tokenA); err == nil {
		t.Fatal("Mutate(A) = nil error, want non-nil after Takeover")
	}
	fenced, err := s.IsFenced(ctx, tokenA)
	if err != nil {
		t.Fatalf("IsFenced: %v", err)
	}
	if !fenced {
		t.Fatal("IsFenced(A) = false, want true after Takeover")
	}
}

// CheckTakeoverFencesConcurrentMutate claims with token A, then races
// a goroutine that calls Mutate(A) in a loop against a goroutine that
// calls Takeover once the first goroutine has issued at least one
// Mutate(A) call, synchronized over a channel. It asserts every
// Mutate(A) call that completed after Takeover returned a non-nil
// error, and asserts IsFenced reports true for token A. It releases
// the hold under the Takeover-returned token before returning.
func CheckTakeoverFencesConcurrentMutate(t testing.TB, ctx context.Context, s Scenario) {
	t.Helper()
	if err := s.Validate(); err != nil {
		t.Fatal(err)
	}
	tokenA, err := s.Claim(ctx)
	if err != nil {
		t.Fatalf("Claim: %v", err)
	}
	current := tokenA
	defer releaseHold(t, ctx, s, &current)

	started := make(chan struct{})
	var startOnce sync.Once
	var takenOver int32

	var mu sync.Mutex
	var postTakeoverErrs []error

	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		recorded := 0
		for recorded < concurrentMutatePostTakeoverSamples {
			// Read the flag before, not after, calling Mutate: an
			// atomic.LoadInt32 that observes atomic.StoreInt32(&takenOver, 1)
			// happens-after every memory effect of the Takeover call that
			// preceded the store, per the Go memory model. Reading the flag
			// only before the call guarantees a recorded sample's Mutate
			// call started strictly after Takeover finished; reading it
			// after the call (as a prior version of this check did) can
			// misclassify a Mutate call that both started and finished
			// before Takeover, if the goroutine is descheduled between the
			// call returning and the flag check.
			before := atomic.LoadInt32(&takenOver) == 1
			mutateErr := s.Mutate(ctx, tokenA)
			startOnce.Do(func() { close(started) })
			if before {
				mu.Lock()
				postTakeoverErrs = append(postTakeoverErrs, mutateErr)
				mu.Unlock()
				recorded++
			}
		}
	}()

	<-started
	tokenB, takeoverErr := s.Takeover(ctx)
	atomic.StoreInt32(&takenOver, 1)
	wg.Wait()
	if takeoverErr == nil {
		current = tokenB
	}
	if takeoverErr != nil {
		t.Fatalf("Takeover: %v", takeoverErr)
	}

	for i, mutateErr := range postTakeoverErrs {
		if mutateErr == nil {
			t.Fatalf("Mutate(A) call %d completed after Takeover but returned no error", i)
		}
	}

	fenced, err := s.IsFenced(ctx, tokenA)
	if err != nil {
		t.Fatalf("IsFenced: %v", err)
	}
	if !fenced {
		t.Fatal("IsFenced(A) = false, want true after Takeover")
	}
}

// CheckMutateSucceedsForCurrentOwner claims a fresh resource with
// token A and calls Mutate(A) before any Takeover, asserting the call
// returns nil. It proves the legitimate, currently held owner can
// mutate at all, closing a gap a Mutate that always errors, even for
// the correct owner, would otherwise slip past every other check in
// this kit. It releases the hold under token A before returning.
func CheckMutateSucceedsForCurrentOwner(t testing.TB, ctx context.Context, s Scenario) {
	t.Helper()
	if err := s.Validate(); err != nil {
		t.Fatal(err)
	}
	tokenA, err := s.Claim(ctx)
	if err != nil {
		t.Fatalf("Claim: %v", err)
	}
	defer releaseHold(t, ctx, s, &tokenA)
	if err := s.Mutate(ctx, tokenA); err != nil {
		t.Fatalf("Mutate(A) = %v, want nil for the current, non-fenced owner", err)
	}
}

// CheckIsFencedFalseForUnknownToken calls IsFenced with a token the
// Scenario never issued through Claim or Takeover, and asserts the
// result is false, proving IsFenced reports the fenced state of a
// real prior owner, not a default "yes" for any token it does not
// recognize. It never holds the resource, so it needs no release.
func CheckIsFencedFalseForUnknownToken(t testing.TB, ctx context.Context, s Scenario) {
	t.Helper()
	if err := s.Validate(); err != nil {
		t.Fatal(err)
	}
	const unknownToken = "durablefence-unknown-token"
	fenced, err := s.IsFenced(ctx, unknownToken)
	if err != nil {
		t.Fatalf("IsFenced: %v", err)
	}
	if fenced {
		t.Fatal("IsFenced = true, want false for a token never issued by this Scenario")
	}
}

// RunAll runs every Check* function above, in alphabetical order, under
// t.Run with each check's own name as the subtest name. A caller
// wanting the full suite calls RunAll once; a caller wanting one
// invariant calls that check function directly.
func RunAll(t testing.TB, ctx context.Context, s Scenario) {
	t.Helper()
	checks := []struct {
		name string
		run  func(testing.TB, context.Context, Scenario)
	}{
		{"CheckClaimGrantsHold", CheckClaimGrantsHold},
		{"CheckClaimRejectsWhileHeld", CheckClaimRejectsWhileHeld},
		{"CheckIsFencedFalseForUnknownToken", CheckIsFencedFalseForUnknownToken},
		{"CheckMutateSucceedsForCurrentOwner", CheckMutateSucceedsForCurrentOwner},
		{"CheckReleaseClearsHold", CheckReleaseClearsHold},
		{"CheckTakeoverFencesConcurrentMutate", CheckTakeoverFencesConcurrentMutate},
		{"CheckTakeoverFencesPreviousOwner", CheckTakeoverFencesPreviousOwner},
	}
	for _, c := range checks {
		c := c
		if tt, ok := t.(*testing.T); ok {
			tt.Run(c.name, func(t *testing.T) {
				c.run(t, ctx, s)
			})
			continue
		}
		c.run(t, ctx, s)
	}
}
