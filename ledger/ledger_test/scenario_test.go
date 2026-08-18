package ledger_test

import (
	"context"
	"errors"
	"strconv"
	"sync"
	"testing"
	"time"

	"github.com/MiviaLabs/mivia-ai-sdk/durablefence"
	"github.com/MiviaLabs/mivia-ai-sdk/ledger"
)

// scenarioKey is the fixed idempotency key the durablefence.Scenario
// wiring below exercises across every check.
const scenarioKey = ledger.IdempotencyKey("durablefence-scenario")

// scenarioOwner is the fixed owner id every closure below uses;
// Ledger fences by FenceToken, not by OwnerID.
const scenarioOwner = ledger.OwnerID("durablefence-owner")

// scenarioLease is long enough that a second Claim call, made
// microseconds after the first, still observes an active lease and
// returns ErrLeaseActive, the behavior CheckClaimRejectsWhileHeld
// requires.
const scenarioLease = time.Hour

// scenarioClock hands out strictly increasing timestamps so every
// Ledger call in the Scenario wiring gets a distinct, deterministic
// now, with no reliance on wall-clock scheduling.
type scenarioClock struct {
	mu    sync.Mutex
	ticks int64
}

var scenarioClockBase = time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)

// now returns the next strictly increasing timestamp.
func (c *scenarioClock) now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.ticks++
	return scenarioClockBase.Add(time.Duration(c.ticks) * time.Nanosecond)
}

// staleNow returns a now value at or after any LeaseUntil a prior
// Claim or Renew call on this clock could have set, so Takeover
// always finds the record stale on its first call: every earlier
// LeaseUntil is some past tick plus scenarioLease, and this call's
// tick is never earlier than that past tick.
func (c *scenarioClock) staleNow() time.Time {
	return c.now().Add(scenarioLease + time.Second)
}

// fenceToToken converts a ledger.FenceToken to the opaque string
// token durablefence.Scenario carries.
func fenceToToken(f ledger.FenceToken) string {
	return strconv.FormatUint(uint64(f), 10)
}

// tokenToFence parses a durablefence.Scenario token back into a
// ledger.FenceToken.
func tokenToFence(token string) (ledger.FenceToken, error) {
	v, err := strconv.ParseUint(token, 10, 64)
	if err != nil {
		return 0, err
	}
	return ledger.FenceToken(v), nil
}

// buildLedgerScenario admits scenarioKey against l and wires a
// durablefence.Scenario over Ledger.Claim, Takeover, Renew, Release,
// and State.
func buildLedgerScenario(t *testing.T, l *ledger.Ledger, ctx context.Context) durablefence.Scenario {
	t.Helper()
	mustAdmit(t, l, ctx, scenarioKey, 1)
	clk := &scenarioClock{}

	return durablefence.Scenario{
		Claim: func(ctx context.Context) (string, error) {
			fence, err := l.Claim(ctx, scenarioKey, scenarioOwner, scenarioLease, clk.now())
			if err != nil {
				return "", err
			}
			return fenceToToken(fence), nil
		},
		Takeover: func(ctx context.Context) (string, error) {
			fence, err := l.Takeover(ctx, scenarioKey, scenarioOwner, scenarioLease, clk.staleNow())
			if err != nil {
				return "", err
			}
			return fenceToToken(fence), nil
		},
		Mutate: func(ctx context.Context, token string) error {
			fence, err := tokenToFence(token)
			if err != nil {
				return err
			}
			return l.Renew(ctx, scenarioKey, scenarioOwner, fence, scenarioLease, clk.now())
		},
		Release: func(ctx context.Context, token string) error {
			fence, err := tokenToFence(token)
			if err != nil {
				return err
			}
			return l.Release(ctx, scenarioKey, scenarioOwner, fence)
		},
		IsHeld: func(ctx context.Context) (bool, error) {
			st, found, err := l.State(ctx, scenarioKey)
			if err != nil {
				return false, err
			}
			return found && st.Status == ledger.StatusClaimed, nil
		},
		IsFenced: func(ctx context.Context, token string) (bool, error) {
			fence, err := tokenToFence(token)
			if err != nil {
				// A syntactically invalid token was never issued by
				// this Scenario, so it is not fenced.
				return false, nil
			}
			err = l.Renew(ctx, scenarioKey, scenarioOwner, fence, scenarioLease, clk.now())
			switch {
			case err == nil:
				return false, nil
			case errors.Is(err, ledger.ErrFenced):
				return true, nil
			case errors.Is(err, ledger.ErrNotClaimed), errors.Is(err, ledger.ErrNoKey):
				return false, nil
			default:
				return false, err
			}
		},
	}
}

// TestLedgerScenarioConformance wires a durablefence.Scenario against
// a real Ledger and runs the shared conformance suite. It runs
// alongside claim_race_test.go, not in place of it. Run under
// go test -race.
func TestLedgerScenarioConformance(t *testing.T) {
	ctx := context.Background()
	l := newLedger(t, nil)
	s := buildLedgerScenario(t, l, ctx)
	durablefence.RunAll(t, ctx, s)
}
