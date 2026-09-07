package ledgertest_test

import (
	"context"
	"errors"
	"testing"

	"github.com/MiviaLabs/mivia-ai-sdk/ledger/ledgertest"
)

// errInjected is the backend error every case below injects into one
// Scenario field, proving a Check* function stops and fails loud on
// the first backend error it sees, distinct from a fencing violation.
var errInjected = errors.New("ledgertest_test: injected backend error")

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
		mutate func(*ledgertest.Scenario)
	}{
		{"claim-error", func(s *ledgertest.Scenario) {
			s.Claim = func(context.Context) (string, error) { return "", errInjected }
		}},
		{"isheld-error", func(s *ledgertest.Scenario) {
			s.IsHeld = func(context.Context) (bool, error) { return false, errInjected }
		}},
		{"isfenced-error", func(s *ledgertest.Scenario) {
			s.IsFenced = func(context.Context, string) (bool, error) { return false, errInjected }
		}},
		{"release-error", func(s *ledgertest.Scenario) {
			s.Release = func(context.Context, string) error { return errInjected }
		}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			s := newReferenceClaim().scenario()
			c.mutate(&s)
			expectCheckFail(t, "CheckClaimGrantsHold", func(t *testing.T) {
				ledgertest.CheckClaimGrantsHold(t, ctx, s)
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
		mutate func(*ledgertest.Scenario)
	}{
		{"claim-error", func(s *ledgertest.Scenario) {
			s.Claim = func(context.Context) (string, error) { return "", errInjected }
		}},
		{"release-error", func(s *ledgertest.Scenario) {
			s.Release = func(context.Context, string) error { return errInjected }
		}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			s := newReferenceClaim().scenario()
			c.mutate(&s)
			expectCheckFail(t, "CheckClaimRejectsWhileHeld", func(t *testing.T) {
				ledgertest.CheckClaimRejectsWhileHeld(t, ctx, s)
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
		mutate func(*ledgertest.Scenario)
	}{
		{"claim-error", func(s *ledgertest.Scenario) {
			s.Claim = func(context.Context) (string, error) { return "", errInjected }
		}},
		{"release-error", func(s *ledgertest.Scenario) {
			s.Release = func(context.Context, string) error { return errInjected }
		}},
		{"isheld-error", func(s *ledgertest.Scenario) {
			s.IsHeld = func(context.Context) (bool, error) { return false, errInjected }
		}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			s := newReferenceClaim().scenario()
			c.mutate(&s)
			expectCheckFail(t, "CheckReleaseClearsHold", func(t *testing.T) {
				ledgertest.CheckReleaseClearsHold(t, ctx, s)
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
		mutate func(*ledgertest.Scenario)
	}{
		{"claim-error", func(s *ledgertest.Scenario) {
			s.Claim = func(context.Context) (string, error) { return "", errInjected }
		}},
		{"takeover-error", func(s *ledgertest.Scenario) {
			s.Takeover = func(context.Context) (string, error) { return "", errInjected }
		}},
		{"isfenced-error", func(s *ledgertest.Scenario) {
			s.IsFenced = func(context.Context, string) (bool, error) { return false, errInjected }
		}},
		{"release-error", func(s *ledgertest.Scenario) {
			s.Release = func(context.Context, string) error { return errInjected }
		}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			s := newReferenceClaim().scenario()
			c.mutate(&s)
			expectCheckFail(t, "CheckTakeoverFencesPreviousOwner", func(t *testing.T) {
				ledgertest.CheckTakeoverFencesPreviousOwner(t, ctx, s)
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
		mutate func(*ledgertest.Scenario)
	}{
		{"claim-error", func(s *ledgertest.Scenario) {
			s.Claim = func(context.Context) (string, error) { return "", errInjected }
		}},
		{"takeover-error", func(s *ledgertest.Scenario) {
			s.Takeover = func(context.Context) (string, error) { return "", errInjected }
		}},
		{"isfenced-error", func(s *ledgertest.Scenario) {
			s.IsFenced = func(context.Context, string) (bool, error) { return false, errInjected }
		}},
		{"release-error", func(s *ledgertest.Scenario) {
			s.Release = func(context.Context, string) error { return errInjected }
		}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			s := newReferenceClaim().scenario()
			c.mutate(&s)
			expectCheckFail(t, "CheckTakeoverFencesConcurrentMutate", func(t *testing.T) {
				ledgertest.CheckTakeoverFencesConcurrentMutate(t, ctx, s)
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
		ledgertest.CheckIsFencedFalseForUnknownToken(t, ctx, s)
	})
}

// TestCheckMutateSucceedsForCurrentOwnerPropagatesBackendErrors proves
// CheckMutateSucceedsForCurrentOwner fails loud when the initial Claim
// or the closing Release returns a backend error. A Mutate backend
// error is not a separate case here: for this check, Mutate returning
// any non-nil error for the current, non-fenced owner is the exact
// condition TestCheckMutateSucceedsForCurrentOwnerCatchesBrokenMutate
// in checks_negative_test.go already exercises.
func TestCheckMutateSucceedsForCurrentOwnerPropagatesBackendErrors(t *testing.T) {
	ctx := context.Background()
	cases := []struct {
		name   string
		mutate func(*ledgertest.Scenario)
	}{
		{"claim-error", func(s *ledgertest.Scenario) {
			s.Claim = func(context.Context) (string, error) { return "", errInjected }
		}},
		{"release-error", func(s *ledgertest.Scenario) {
			s.Release = func(context.Context, string) error { return errInjected }
		}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			s := newReferenceClaim().scenario()
			c.mutate(&s)
			expectCheckFail(t, "CheckMutateSucceedsForCurrentOwner", func(t *testing.T) {
				ledgertest.CheckMutateSucceedsForCurrentOwner(t, ctx, s)
			})
		})
	}
}

// TestChecksFailOnIncompleteScenario proves every Check* function
// calls Scenario.Validate first and fails loud on an incomplete
// Scenario, before it calls any field.
func TestChecksFailOnIncompleteScenario(t *testing.T) {
	ctx := context.Background()
	incomplete := ledgertest.Scenario{}
	cases := []struct {
		name string
		run  func(testing.TB, context.Context, ledgertest.Scenario)
	}{
		{"CheckClaimGrantsHold", ledgertest.CheckClaimGrantsHold},
		{"CheckClaimRejectsWhileHeld", ledgertest.CheckClaimRejectsWhileHeld},
		{"CheckReleaseClearsHold", ledgertest.CheckReleaseClearsHold},
		{"CheckTakeoverFencesPreviousOwner", ledgertest.CheckTakeoverFencesPreviousOwner},
		{"CheckTakeoverFencesConcurrentMutate", ledgertest.CheckTakeoverFencesConcurrentMutate},
		{"CheckIsFencedFalseForUnknownToken", ledgertest.CheckIsFencedFalseForUnknownToken},
		{"CheckMutateSucceedsForCurrentOwner", ledgertest.CheckMutateSucceedsForCurrentOwner},
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
