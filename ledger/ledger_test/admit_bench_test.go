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
