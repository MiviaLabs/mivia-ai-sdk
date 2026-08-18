package durablefence_test

import (
	"context"
	"errors"
	"testing"

	"github.com/MiviaLabs/mivia-ai-sdk/durablefence"
)

// errInjected is the backend error every case below injects into one
// Scenario field, proving a Check* function stops and fails loud on
// the first backend error it sees, distinct from a fencing violation.
var errInjected = errors.New("durablefence_test: injected backend error")

// expectCheckFail runs run under an isolated *testing.T and fails t
// when run reports success; every case in this file expects run to
// fail against a Scenario carrying one broken field.
func expectCheckFail(t *testing.T, name string, run func(t *testing.T)) {
	t.Helper()
	if runIsolated(name, run) {
		t.Fatalf("%s passed against a Scenario field that errors", name)
	}
}

// TestCheckClaimGrantsHoldPropagatesBackendErrors proves
// CheckClaimGrantsHold fails loud when Claim, IsHeld, IsFenced, or
// Release each return a backend error in turn.
func TestCheckClaimGrantsHoldPropagatesBackendErrors(t *testing.T) {
	ctx := context.Background()
	cases := []struct {
		name   string
		mutate func(*durablefence.Scenario)
	}{
		{"claim-error", func(s *durablefence.Scenario) {
			s.Claim = func(context.Context) (string, error) { return "", errInjected }
		}},
		{"isheld-error", func(s *durablefence.Scenario) {
			s.IsHeld = func(context.Context) (bool, error) { return false, errInjected }
		}},
		{"isfenced-error", func(s *durablefence.Scenario) {
			s.IsFenced = func(context.Context, string) (bool, error) { return false, errInjected }
		}},
		{"release-error", func(s *durablefence.Scenario) {
			s.Release = func(context.Context, string) error { return errInjected }
		}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			s := newReferenceClaim().scenario()
			c.mutate(&s)
			expectCheckFail(t, "CheckClaimGrantsHold", func(t *testing.T) {
				durablefence.CheckClaimGrantsHold(t, ctx, s)
			})
		})
	}
}

// TestCheckClaimRejectsWhileHeldPropagatesBackendErrors proves
// CheckClaimRejectsWhileHeld fails loud when the first Claim or the
// closing Release returns a backend error.
func TestCheckClaimRejectsWhileHeldPropagatesBackendErrors(t *testing.T) {
	ctx := context.Background()
	cases := []struct {
		name   string
		mutate func(*durablefence.Scenario)
	}{
		{"claim-error", func(s *durablefence.Scenario) {
			s.Claim = func(context.Context) (string, error) { return "", errInjected }
		}},
		{"release-error", func(s *durablefence.Scenario) {
			s.Release = func(context.Context, string) error { return errInjected }
		}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			s := newReferenceClaim().scenario()
			c.mutate(&s)
			expectCheckFail(t, "CheckClaimRejectsWhileHeld", func(t *testing.T) {
				durablefence.CheckClaimRejectsWhileHeld(t, ctx, s)
			})
		})
	}
}

// TestCheckReleaseClearsHoldPropagatesBackendErrors proves
// CheckReleaseClearsHold fails loud when Claim, Release, or IsHeld
// returns a backend error.
func TestCheckReleaseClearsHoldPropagatesBackendErrors(t *testing.T) {
	ctx := context.Background()
	cases := []struct {
		name   string
		mutate func(*durablefence.Scenario)
	}{
		{"claim-error", func(s *durablefence.Scenario) {
			s.Claim = func(context.Context) (string, error) { return "", errInjected }
		}},
		{"release-error", func(s *durablefence.Scenario) {
			s.Release = func(context.Context, string) error { return errInjected }
		}},
		{"isheld-error", func(s *durablefence.Scenario) {
			s.IsHeld = func(context.Context) (bool, error) { return false, errInjected }
		}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			s := newReferenceClaim().scenario()
			c.mutate(&s)
			expectCheckFail(t, "CheckReleaseClearsHold", func(t *testing.T) {
				durablefence.CheckReleaseClearsHold(t, ctx, s)
			})
		})
	}
}

// TestCheckTakeoverFencesPreviousOwnerPropagatesBackendErrors proves
// CheckTakeoverFencesPreviousOwner fails loud when Claim, Takeover,
// IsFenced, or Release returns a backend error.
func TestCheckTakeoverFencesPreviousOwnerPropagatesBackendErrors(t *testing.T) {
	ctx := context.Background()
	cases := []struct {
		name   string
		mutate func(*durablefence.Scenario)
	}{
		{"claim-error", func(s *durablefence.Scenario) {
			s.Claim = func(context.Context) (string, error) { return "", errInjected }
		}},
		{"takeover-error", func(s *durablefence.Scenario) {
			s.Takeover = func(context.Context) (string, error) { return "", errInjected }
		}},
		{"isfenced-error", func(s *durablefence.Scenario) {
			s.IsFenced = func(context.Context, string) (bool, error) { return false, errInjected }
		}},
		{"release-error", func(s *durablefence.Scenario) {
			s.Release = func(context.Context, string) error { return errInjected }
		}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			s := newReferenceClaim().scenario()
			c.mutate(&s)
			expectCheckFail(t, "CheckTakeoverFencesPreviousOwner", func(t *testing.T) {
				durablefence.CheckTakeoverFencesPreviousOwner(t, ctx, s)
			})
		})
	}
}

// TestCheckTakeoverFencesConcurrentMutatePropagatesBackendErrors
// proves CheckTakeoverFencesConcurrentMutate fails loud when Claim,
// Takeover, IsFenced, or Release returns a backend error.
func TestCheckTakeoverFencesConcurrentMutatePropagatesBackendErrors(t *testing.T) {
	ctx := context.Background()
	cases := []struct {
		name   string
		mutate func(*durablefence.Scenario)
	}{
		{"claim-error", func(s *durablefence.Scenario) {
			s.Claim = func(context.Context) (string, error) { return "", errInjected }
		}},
		{"takeover-error", func(s *durablefence.Scenario) {
			s.Takeover = func(context.Context) (string, error) { return "", errInjected }
		}},
		{"isfenced-error", func(s *durablefence.Scenario) {
			s.IsFenced = func(context.Context, string) (bool, error) { return false, errInjected }
		}},
		{"release-error", func(s *durablefence.Scenario) {
			s.Release = func(context.Context, string) error { return errInjected }
		}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			s := newReferenceClaim().scenario()
			c.mutate(&s)
			expectCheckFail(t, "CheckTakeoverFencesConcurrentMutate", func(t *testing.T) {
				durablefence.CheckTakeoverFencesConcurrentMutate(t, ctx, s)
			})
		})
	}
}

// TestCheckIsFencedFalseForUnknownTokenPropagatesBackendErrors proves
// CheckIsFencedFalseForUnknownToken fails loud when IsFenced returns
// a backend error.
func TestCheckIsFencedFalseForUnknownTokenPropagatesBackendErrors(t *testing.T) {
	ctx := context.Background()
	s := newReferenceClaim().scenario()
	s.IsFenced = func(context.Context, string) (bool, error) { return false, errInjected }
	expectCheckFail(t, "CheckIsFencedFalseForUnknownToken", func(t *testing.T) {
		durablefence.CheckIsFencedFalseForUnknownToken(t, ctx, s)
	})
}

// TestChecksFailOnIncompleteScenario proves every Check* function
// calls Scenario.Validate first and fails loud on an incomplete
// Scenario, before it calls any field.
func TestChecksFailOnIncompleteScenario(t *testing.T) {
	ctx := context.Background()
	incomplete := durablefence.Scenario{}
	cases := []struct {
		name string
		run  func(testing.TB, context.Context, durablefence.Scenario)
	}{
		{"CheckClaimGrantsHold", durablefence.CheckClaimGrantsHold},
		{"CheckClaimRejectsWhileHeld", durablefence.CheckClaimRejectsWhileHeld},
		{"CheckReleaseClearsHold", durablefence.CheckReleaseClearsHold},
		{"CheckTakeoverFencesPreviousOwner", durablefence.CheckTakeoverFencesPreviousOwner},
		{"CheckTakeoverFencesConcurrentMutate", durablefence.CheckTakeoverFencesConcurrentMutate},
		{"CheckIsFencedFalseForUnknownToken", durablefence.CheckIsFencedFalseForUnknownToken},
		{"CheckMutateSucceedsForCurrentOwner", durablefence.CheckMutateSucceedsForCurrentOwner},
	}
	for _, c := range cases {
		c := c
		t.Run(c.name, func(t *testing.T) {
			expectCheckFail(t, c.name, func(t *testing.T) {
				c.run(t, ctx, incomplete)
			})
		})
	}
}
