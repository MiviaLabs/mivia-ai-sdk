package durablefence_test

import (
	"context"
	"testing"

	"github.com/MiviaLabs/mivia-ai-sdk/durablefence"
)

// assertUnheld fails t when r reports a held resource.
func assertUnheld(t *testing.T, ctx context.Context, r *referenceClaim) {
	t.Helper()
	held, err := r.isHeld(ctx)
	if err != nil {
		t.Fatalf("isHeld: %v", err)
	}
	if held {
		t.Fatal("resource still held after check returned")
	}
}

func TestCheckClaimGrantsHold(t *testing.T) {
	ctx := context.Background()
	r := newReferenceClaim()
	durablefence.CheckClaimGrantsHold(t, ctx, r.scenario())
	assertUnheld(t, ctx, r)
}

func TestCheckClaimRejectsWhileHeld(t *testing.T) {
	ctx := context.Background()
	r := newReferenceClaim()
	durablefence.CheckClaimRejectsWhileHeld(t, ctx, r.scenario())
	assertUnheld(t, ctx, r)
}

func TestCheckReleaseClearsHold(t *testing.T) {
	ctx := context.Background()
	r := newReferenceClaim()
	durablefence.CheckReleaseClearsHold(t, ctx, r.scenario())
	assertUnheld(t, ctx, r)
}

func TestCheckIsFencedFalseForUnknownToken(t *testing.T) {
	ctx := context.Background()
	r := newReferenceClaim()
	durablefence.CheckIsFencedFalseForUnknownToken(t, ctx, r.scenario())
	assertUnheld(t, ctx, r)
}

func TestCheckTakeoverFencesPreviousOwner(t *testing.T) {
	ctx := context.Background()
	r := newReferenceClaim()
	durablefence.CheckTakeoverFencesPreviousOwner(t, ctx, r.scenario())
	assertUnheld(t, ctx, r)
}

// TestCheckTakeoverFencesConcurrentMutate proves the reference's mutex
// guard fences a concurrent Mutate(A) against an overlapping
// Takeover(B), not only a sequential one. Run under go test -race.
func TestCheckTakeoverFencesConcurrentMutate(t *testing.T) {
	ctx := context.Background()
	r := newReferenceClaim()
	durablefence.CheckTakeoverFencesConcurrentMutate(t, ctx, r.scenario())
	assertUnheld(t, ctx, r)
}
