//go:build ledger_sqlite

package ledger

import (
	"context"
	"net/url"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// fixedSQLiteNow gives every deterministic SQLiteStore test a shared,
// non-wall-clock time base, mirroring ledger_test's fixedNow.
var fixedSQLiteNow = time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)

// newSQLiteStoreT opens a SQLiteStore against path and registers
// Close as a test cleanup.
func newSQLiteStoreT(t *testing.T, path string) *SQLiteStore {
	t.Helper()
	s, err := NewSQLiteStore(path)
	if err != nil {
		t.Fatalf("NewSQLiteStore(%q): %v", path, err)
	}
	t.Cleanup(func() {
		if err := s.Close(); err != nil {
			t.Errorf("Close: %v", err)
		}
	})
	return s
}

// sqliteTestPaths returns the two DSN shapes every red-green case
// below runs against: a fresh temp file, and an in-process database.
func sqliteTestPaths(t *testing.T) []string {
	t.Helper()
	return []string{filepath.Join(t.TempDir(), "ledger.db"), ":memory:"}
}

// TestSQLiteStoreCompareAndSwapComparesFourFields proves
// CompareAndSwap rejects a call whose old (Sequence, Status, Fence,
// Rev) tuple does not match the stored record's, even when other
// fields (Task) differ, and accepts a call whose old is the zero
// value against an absent key. Mirrors ledger_test/mem_store_test.go.
func TestSQLiteStoreCompareAndSwapComparesFourFields(t *testing.T) {
	for _, path := range sqliteTestPaths(t) {
		t.Run(path, func(t *testing.T) {
			ctx := context.Background()
			store := newSQLiteStoreT(t, path)

			ok, err := store.CompareAndSwap(ctx, "k1", TaskState{}, TaskState{
				Key: "k1", Status: StatusPending, Sequence: 1, Task: "first",
			})
			if err != nil {
				t.Fatalf("insert: %v", err)
			}
			if !ok {
				t.Fatalf("insert against absent key with zero-value old: want true")
			}

			stored, found, err := store.Load(ctx, "k1")
			if err != nil || !found {
				t.Fatalf("Load: found=%v err=%v", found, err)
			}
			if stored.Task != "first" {
				t.Fatalf("Load: Task = %v, want %q", stored.Task, "first")
			}

			stale := stored
			stale.Rev = stored.Rev + 1
			stale.Task = "different"
			ok, err = store.CompareAndSwap(ctx, "k1", stale, TaskState{
				Key: "k1", Status: StatusPending, Sequence: 1, Task: "second",
			})
			if err != nil {
				t.Fatalf("stale CompareAndSwap: %v", err)
			}
			if ok {
				t.Fatalf("CompareAndSwap with a stale Rev: want false")
			}

			ok, err = store.CompareAndSwap(ctx, "k1", stored, TaskState{
				Key: "k1", Status: StatusPending, Sequence: 1, Task: "third",
			})
			if err != nil {
				t.Fatalf("matching CompareAndSwap: %v", err)
			}
			if !ok {
				t.Fatalf("CompareAndSwap with a matching tuple: want true")
			}

			final, found, err := store.Load(ctx, "k1")
			if err != nil || !found {
				t.Fatalf("Load: found=%v err=%v", found, err)
			}
			if final.Task != "third" {
				t.Fatalf("Load: Task = %v, want %q", final.Task, "third")
			}
		})
	}
}

// TestSQLiteStoreCompareAndSwapBumpsRevOnEveryWrite proves Rev bumps
// by one on every successful call, including a Renew-shaped write
// that changes only LeaseUntil, and that LeaseUntil round-trips
// through the lease_until text column.
func TestSQLiteStoreCompareAndSwapBumpsRevOnEveryWrite(t *testing.T) {
	for _, path := range sqliteTestPaths(t) {
		t.Run(path, func(t *testing.T) {
			ctx := context.Background()
			store := newSQLiteStoreT(t, path)

			if ok, err := store.CompareAndSwap(ctx, "k1", TaskState{}, TaskState{
				Key: "k1", Status: StatusPending, Sequence: 1,
			}); err != nil || !ok {
				t.Fatalf("insert: ok=%v err=%v", ok, err)
			}
			first, _, _ := store.Load(ctx, "k1")
			if first.Rev != 0 {
				t.Fatalf("Rev after insert = %d, want 0", first.Rev)
			}

			leaseWrite := first
			leaseWrite.LeaseUntil = fixedSQLiteNow
			if ok, err := store.CompareAndSwap(ctx, "k1", first, leaseWrite); err != nil || !ok {
				t.Fatalf("lease write: ok=%v err=%v", ok, err)
			}
			second, _, _ := store.Load(ctx, "k1")
			if second.Rev != first.Rev+1 {
				t.Fatalf("Rev after lease write = %d, want %d", second.Rev, first.Rev+1)
			}
			if !second.LeaseUntil.Equal(fixedSQLiteNow) {
				t.Fatalf("LeaseUntil round trip = %v, want %v", second.LeaseUntil, fixedSQLiteNow)
			}

			staleWrite := second
			staleWrite.LeaseUntil = fixedSQLiteNow.Add(time.Hour)
			ok, err := store.CompareAndSwap(ctx, "k1", first, staleWrite)
			if err != nil {
				t.Fatalf("stale second write: %v", err)
			}
			if ok {
				t.Fatalf("second write with a pre-write Rev: want false")
			}
		})
	}
}

// TestSQLiteStoreCompareAndSwapRejectsNonZeroBaselineAgainstAbsentKey
// proves CompareAndSwap rejects an insert attempt whose old is not
// the zero value, even when the key has no stored record.
func TestSQLiteStoreCompareAndSwapRejectsNonZeroBaselineAgainstAbsentKey(t *testing.T) {
	ctx := context.Background()
	store := newSQLiteStoreT(t, ":memory:")

	ok, err := store.CompareAndSwap(ctx, "ghost", TaskState{Sequence: 1}, TaskState{
		Key: "ghost", Status: StatusPending,
	})
	if err != nil {
		t.Fatalf("CompareAndSwap: %v", err)
	}
	if ok {
		t.Fatalf("CompareAndSwap with a nonzero old against an absent key: want false")
	}
	if _, found, _ := store.Load(ctx, "ghost"); found {
		t.Fatalf("rejected CompareAndSwap must not create a record")
	}
}

// TestSQLiteStoreCompareAndSwapRejectsUnencodableTask proves a Task
// value encoding/json.Marshal cannot encode surfaces a non-nil error
// from CompareAndSwap, instead of silently dropping the value.
func TestSQLiteStoreCompareAndSwapRejectsUnencodableTask(t *testing.T) {
	ctx := context.Background()
	store := newSQLiteStoreT(t, ":memory:")

	_, err := store.CompareAndSwap(ctx, "k1", TaskState{}, TaskState{
		Key: "k1", Status: StatusPending, Sequence: 1, Task: make(chan int),
	})
	if err == nil {
		t.Fatalf("CompareAndSwap with an unencodable Task: want non-nil error")
	}
	if _, found, _ := store.Load(ctx, "k1"); found {
		t.Fatalf("a rejected CompareAndSwap must not create a record")
	}
}

// TestSQLiteStoreLoadMissingKeyNoError proves Load against a key with
// no row returns found == false with no error, matching Store.Load's
// contract.
func TestSQLiteStoreLoadMissingKeyNoError(t *testing.T) {
	ctx := context.Background()
	store := newSQLiteStoreT(t, ":memory:")

	_, found, err := store.Load(ctx, "ghost")
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if found {
		t.Fatalf("found = true, want false for a missing key")
	}
}

// TestSQLiteStoreRangeVisitsEveryRecordOnce proves Range visits every
// stored record exactly once.
func TestSQLiteStoreRangeVisitsEveryRecordOnce(t *testing.T) {
	ctx := context.Background()
	store := newSQLiteStoreT(t, ":memory:")
	keys := []IdempotencyKey{"a", "b", "c"}
	for _, k := range keys {
		if ok, err := store.CompareAndSwap(ctx, k, TaskState{}, TaskState{
			Key: k, Status: StatusPending, Sequence: 1,
		}); err != nil || !ok {
			t.Fatalf("insert %s: ok=%v err=%v", k, ok, err)
		}
	}
	visits := map[IdempotencyKey]int{}
	err := store.Range(ctx, func(ts TaskState) bool {
		visits[ts.Key]++
		return true
	})
	if err != nil {
		t.Fatalf("Range: %v", err)
	}
	for _, k := range keys {
		if visits[k] != 1 {
			t.Fatalf("key %s visited %d times, want 1", k, visits[k])
		}
	}
	if len(visits) != len(keys) {
		t.Fatalf("visited %d distinct keys, want %d", len(visits), len(keys))
	}
}

// TestSQLiteStoreRangeStopsEarlyWhenFnReturnsFalse proves Range stops
// iterating the first time fn returns false.
func TestSQLiteStoreRangeStopsEarlyWhenFnReturnsFalse(t *testing.T) {
	ctx := context.Background()
	store := newSQLiteStoreT(t, ":memory:")
	for _, k := range []IdempotencyKey{"a", "b", "c"} {
		if ok, err := store.CompareAndSwap(ctx, k, TaskState{}, TaskState{
			Key: k, Status: StatusPending,
		}); err != nil || !ok {
			t.Fatalf("insert %s: ok=%v err=%v", k, ok, err)
		}
	}
	visits := 0
	err := store.Range(ctx, func(TaskState) bool {
		visits++
		return false
	})
	if err != nil {
		t.Fatalf("Range: %v", err)
	}
	if visits != 1 {
		t.Fatalf("visits = %d, want 1 (Range must stop on the first false)", visits)
	}
}

// TestSQLiteStoreSurvivesPastFirstInstanceClose proves a second
// *SQLiteStore opened against the same file path after the first is
// Closed still Loads every prior record: the "survives past one Go
// value's lifetime" proxy for "survives a process restart".
func TestSQLiteStoreSurvivesPastFirstInstanceClose(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "ledger.db")

	first, err := NewSQLiteStore(path)
	if err != nil {
		t.Fatalf("NewSQLiteStore: %v", err)
	}
	if ok, err := first.CompareAndSwap(ctx, "k1", TaskState{}, TaskState{
		Key: "k1", Status: StatusPending, Sequence: 1,
	}); err != nil || !ok {
		t.Fatalf("insert: ok=%v err=%v", ok, err)
	}
	if err := first.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	second := newSQLiteStoreT(t, path)
	ts, found, err := second.Load(ctx, "k1")
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if !found {
		t.Fatalf("found = false, want true after reopening %s", path)
	}
	if ts.Sequence != 1 {
		t.Fatalf("Sequence = %d, want 1", ts.Sequence)
	}
}

// TestNewSQLiteStoreFailsFastOnBadPath proves NewSQLiteStore surfaces
// a non-nil error, instead of surfacing the failure lazily on the
// first Load or CompareAndSwap call, when path names a directory that
// does not exist.
func TestNewSQLiteStoreFailsFastOnBadPath(t *testing.T) {
	_, err := NewSQLiteStore(filepath.Join(t.TempDir(), "no-such-dir", "ledger.db"))
	if err == nil {
		t.Fatalf("NewSQLiteStore with a bad path: want non-nil error")
	}
}

// TestSQLiteDSN proves sqliteDSN builds a plain path DSN unescaped,
// and escapes a path containing a literal '?' through a file: URI, so
// the driver's DSN-splitting-at-'?' rule never truncates the path.
func TestSQLiteDSN(t *testing.T) {
	cases := []struct {
		name string
		path string
		want string
	}{
		{"plain path", "/tmp/ledger.db", "/tmp/ledger.db?" + pragmaDSNParams},
		{
			"path with question mark",
			"/tmp/weird?name.db",
			"file:" + url.PathEscape("/tmp/weird?name.db") + "?" + pragmaDSNParams,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := sqliteDSN(tc.path); got != tc.want {
				t.Fatalf("sqliteDSN(%q) = %q, want %q", tc.path, got, tc.want)
			}
		})
	}
}

// TestSQLiteStoreOpensPathContainingQuestionMark proves the file:-URI
// escape branch produces a DSN the driver actually opens against the
// intended file, not a truncated one: the SQLite file lands at the
// exact path passed to NewSQLiteStore, question mark included.
func TestSQLiteStoreOpensPathContainingQuestionMark(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "weird?name.db")

	store := newSQLiteStoreT(t, path)
	if ok, err := store.CompareAndSwap(ctx, "k1", TaskState{}, TaskState{
		Key: "k1", Status: StatusPending, Sequence: 1,
	}); err != nil || !ok {
		t.Fatalf("insert: ok=%v err=%v", ok, err)
	}
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("expected a sqlite file at %q: %v", path, err)
	}
}
