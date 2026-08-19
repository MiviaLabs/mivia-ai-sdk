package ledger_test

import (
	"context"
	"fmt"
	"testing"

	"github.com/MiviaLabs/mivia-ai-sdk/ledger"
)

// buildKeys returns n distinct IdempotencyKey values, computed before
// the benchmark timer starts so key formatting never counts against
// Admit's own cost.
func buildKeys(n int) []ledger.IdempotencyKey {
	keys := make([]ledger.IdempotencyKey, n)
	for i := range keys {
		keys[i] = ledger.IdempotencyKey(fmt.Sprintf("k%d", i))
	}
	return keys
}

// buildChain admits a healthy Needs chain of n keys into l: key i
// needs key i+1, and the last key needs nothing. Every record ends
// StatusPending, so no claim in the chain is refused.
func buildChain(b *testing.B, l *ledger.Ledger, ctx context.Context, keys []ledger.IdempotencyKey) {
	b.Helper()
	for i, k := range keys {
		var needs []ledger.IdempotencyKey
		if i+1 < len(keys) {
			needs = []ledger.IdempotencyKey{keys[i+1]}
		}
		if _, err := l.Admit(ctx, testActor, k, 1, nil, fixedNow, needs...); err != nil {
			b.Fatal(err)
		}
	}
}

// BenchmarkClaimChainDepth measures Claim's transitive ancestor walk
// against MemStore. Each iteration claims every record of a healthy
// chain, not only its leaf, so the measured workload matches the depth
// table in docs/plans/ledger.md. The cost is proportional to the
// caller's declared graph, so it rises with depth; that is the one
// worst case the pull design accepts. No fixed allocation budget:
// MemStore's internal locking varies with GOMAXPROCS.
func BenchmarkClaimChainDepth(b *testing.B) {
	for _, d := range []int{10, 100, 1000} {
		b.Run(fmt.Sprintf("depth=%d", d), func(b *testing.B) {
			ctx := context.Background()
			keys := buildKeys(d)
			b.ReportAllocs()
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				b.StopTimer()
				l, err := ledger.New(nil, nil)
				if err != nil {
					b.Fatalf("New: %v", err)
				}
				buildChain(b, l, ctx, keys)
				b.StartTimer()
				for _, k := range keys {
					if _, err := l.Claim(ctx, testActor, k, "owner-b", fixedLease, fixedNow); err != nil {
						b.Fatal(err)
					}
				}
			}
		})
	}
}

// BenchmarkAdmit measures Admit throughput against MemStore under
// increasing key counts. Each iteration submits a rising sequence for
// one key from the set, cycling through the set as b.N grows, so the
// benchmark exercises both a fresh insert and a rebase path. No fixed
// allocation budget: MemStore's internal locking varies with
// GOMAXPROCS, per the exception docs/plans/agents/PHASES.md allows for
// goroutine-dependent counts. Report ops/sec and allocs/op with
// go test -bench=. -benchmem.
func BenchmarkAdmit(b *testing.B) {
	for _, n := range []int{1, 100, 10000} {
		b.Run(fmt.Sprintf("keys=%d", n), func(b *testing.B) {
			ctx := context.Background()
			l, err := ledger.New(nil, nil)
			if err != nil {
				b.Fatalf("New: %v", err)
			}
			keys := buildKeys(n)
			b.ReportAllocs()
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				key := keys[i%n]
				seq := ledger.Sequence(i/n + 1)
				if _, err := l.Admit(ctx, testActor, key, seq, nil, fixedNow); err != nil {
					b.Fatal(err)
				}
			}
		})
	}
}
