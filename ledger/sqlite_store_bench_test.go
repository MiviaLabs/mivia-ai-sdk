//go:build ledger_sqlite

package ledger

import (
	"context"
	"fmt"
	"path/filepath"
	"testing"
)

// buildSQLiteBenchKeys returns n distinct IdempotencyKey values,
// computed before the benchmark timer starts so key formatting never
// counts against CompareAndSwap's own cost.
func buildSQLiteBenchKeys(n int) []IdempotencyKey {
	keys := make([]IdempotencyKey, n)
	for i := range keys {
		keys[i] = IdempotencyKey(fmt.Sprintf("k%d", i))
	}
	return keys
}

// BenchmarkSQLiteStoreCompareAndSwap measures CompareAndSwap
// throughput against a file-backed SQLiteStore, the rebase-path shape
// ledger_test/admit_bench_test.go measures against MemStore. No fixed
// allocation budget: SQLiteStore's per-call disk I/O and pragma-
// driven fsync behavior are the expected slower path; this benchmark
// records that gap rather than gating on it. Report ops/sec and
// allocs/op with go test -tags ledger_sqlite -bench=. -benchmem
// ./ledger/....
func BenchmarkSQLiteStoreCompareAndSwap(b *testing.B) {
	for _, n := range []int{1, 100} {
		b.Run(fmt.Sprintf("keys=%d", n), func(b *testing.B) {
			ctx := context.Background()
			store, err := NewSQLiteStore(filepath.Join(b.TempDir(), "ledger.db"))
			if err != nil {
				b.Fatalf("NewSQLiteStore: %v", err)
			}
			defer func() { _ = store.Close() }()
			keys := buildSQLiteBenchKeys(n)
			for _, k := range keys {
				if ok, err := store.CompareAndSwap(ctx, k, TaskState{}, TaskState{
					Key: k, Status: StatusPending, Sequence: 1,
				}); err != nil || !ok {
					b.Fatalf("seed insert %s: ok=%v err=%v", k, ok, err)
				}
			}
			b.ReportAllocs()
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				key := keys[i%n]
				old, found, err := store.Load(ctx, key)
				if err != nil || !found {
					b.Fatalf("Load(%s): found=%v err=%v", key, found, err)
				}
				if _, err := store.CompareAndSwap(ctx, key, old, TaskState{
					Key: key, Status: StatusPending, Sequence: old.Sequence + 1,
				}); err != nil {
					b.Fatal(err)
				}
			}
		})
	}
}
