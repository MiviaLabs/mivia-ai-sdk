//go:build ledger_sqlite

package ledger

import (
	"context"
	"errors"
	"path/filepath"
	"strconv"
	"sync"
	"testing"
	"time"

	"github.com/MiviaLabs/mivia-ai-sdk/durablefence"
)

// sqliteScenarioKey is the fixed idempotency key the durablefence
// wiring below exercises across every check.
const sqliteScenarioKey = IdempotencyKey("sqlite-durablefence-scenario")

// sqliteScenarioOwner is the fixed owner id every closure below uses;
// Ledger fences by FenceToken, not by OwnerID.
const sqliteScenarioOwner = OwnerID("sqlite-durablefence-owner")

// sqliteScenarioLease is long enough that a second Claim call, made
// microseconds after the first, still observes an active lease.
const sqliteScenarioLease = time.Hour

// sqliteScenarioActor is the fixed Actor every closure below passes.
const sqliteScenarioActor = Actor("sqlite-durablefence-actor")

// sqliteScenarioClockBase anchors sqliteScenarioClock's strictly
// increasing timestamps to a fixed date, matching
// ledger_test/scenario_test.go's deterministic-clock pattern.
var sqliteScenarioClockBase = time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)

// sqliteScenarioClock hands out strictly increasing timestamps so
// every Ledger call in the Scenario wiring gets a distinct,
// deterministic now, with no reliance on wall-clock scheduling.
type sqliteScenarioClock struct {
	mu    sync.Mutex
	ticks int64
}

// now returns the next strictly increasing timestamp.
func (c *sqliteScenarioClock) now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.ticks++
	return sqliteScenarioClockBase.Add(time.Duration(c.ticks) * time.Nanosecond)
}

// staleNow returns a now value past any LeaseUntil a prior Claim or
// Renew call on this clock could have set, so Takeover always finds
// the record stale on its first call.
func (c *sqliteScenarioClock) staleNow() time.Time {
	return c.now().Add(sqliteScenarioLease + time.Second)
}

// sqliteFenceToToken converts a FenceToken to the opaque string token
// durablefence.Scenario carries.
func sqliteFenceToToken(f FenceToken) string {
	return strconv.FormatUint(uint64(f), 10)
}

// sqliteTokenToFence parses a durablefence.Scenario token back into a
// FenceToken.
func sqliteTokenToFence(token string) (FenceToken, error) {
	v, err := strconv.ParseUint(token, 10, 64)
	if err != nil {
		return 0, err
	}
	return FenceToken(v), nil
}

// buildSQLiteLedgerScenario admits sqliteScenarioKey against l and
// wires a durablefence.Scenario over Ledger.Claim, Takeover, Renew,
// Release, and State, the same shape ledger_test/scenario_test.go
// builds against a MemStore-backed Ledger.
func buildSQLiteLedgerScenario(t *testing.T, l *Ledger, ctx context.Context) durablefence.Scenario {
	t.Helper()
	if ok, err := l.Admit(ctx, sqliteScenarioActor, sqliteScenarioKey, 1, nil, sqliteScenarioClockBase); err != nil || !ok {
		t.Fatalf("Admit: ok=%v err=%v", ok, err)
	}
	clk := &sqliteScenarioClock{}

	return durablefence.Scenario{
		Claim: func(ctx context.Context) (string, error) {
			fence, err := l.Claim(ctx, sqliteScenarioActor, sqliteScenarioKey, sqliteScenarioOwner, sqliteScenarioLease, clk.now())
			if err != nil {
				return "", err
			}
			return sqliteFenceToToken(fence), nil
		},
		Takeover: func(ctx context.Context) (string, error) {
			fence, err := l.Takeover(ctx, sqliteScenarioActor, sqliteScenarioKey, sqliteScenarioOwner, sqliteScenarioLease, clk.staleNow())
			if err != nil {
				return "", err
			}
			return sqliteFenceToToken(fence), nil
		},
		Mutate: func(ctx context.Context, token string) error {
			fence, err := sqliteTokenToFence(token)
			if err != nil {
				return err
			}
			return l.Renew(ctx, sqliteScenarioActor, sqliteScenarioKey, sqliteScenarioOwner, fence, sqliteScenarioLease, clk.now())
		},
		Release: func(ctx context.Context, token string) error {
			fence, err := sqliteTokenToFence(token)
			if err != nil {
				return err
			}
			return l.Release(ctx, sqliteScenarioActor, sqliteScenarioKey, sqliteScenarioOwner, fence, clk.now())
		},
		IsHeld: func(ctx context.Context) (bool, error) {
			st, found, err := l.State(ctx, sqliteScenarioKey)
			if err != nil {
				return false, err
			}
			return found && st.Status == StatusClaimed, nil
		},
		IsFenced: func(ctx context.Context, token string) (bool, error) {
			fence, err := sqliteTokenToFence(token)
			if err != nil {
				// A syntactically invalid token was never issued by
				// this Scenario, so it is not fenced.
				return false, nil
			}
			err = l.Renew(ctx, sqliteScenarioActor, sqliteScenarioKey, sqliteScenarioOwner, fence, sqliteScenarioLease, clk.now())
			switch {
			case err == nil:
				return false, nil
			case errors.Is(err, ErrFenced):
				return true, nil
			case errors.Is(err, ErrNotClaimed), errors.Is(err, ErrNoKey):
				return false, nil
			default:
				return false, err
			}
		},
	}
}

// TestSQLiteStoreLedgerScenarioConformance wires a durablefence.
// Scenario against a Ledger backed by a file-backed SQLiteStore and
// runs the shared conformance suite. This proves SQLiteStore
// satisfies every claim, takeover, and fence invariant MemStore
// already proves.
func TestSQLiteStoreLedgerScenarioConformance(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "ledger.db")
	store := newSQLiteStoreT(t, path)
	l, err := New(store, nil)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	s := buildSQLiteLedgerScenario(t, l, ctx)
	durablefence.RunAll(t, ctx, s)
}
