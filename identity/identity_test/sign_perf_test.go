package identity_test

import (
	"testing"

	"github.com/MiviaLabs/mivia-ai-sdk/identity"
)

// signAllocBudget is the allocation ceiling for one Sign call. The
// race detector adds two allocations, so the ceiling is 8 while the
// plain measured count is 6; see BenchmarkSign.
const signAllocBudget = 8

// BenchmarkSign measures Sign on a small message.
// Baseline (empty implementation): no code, no measurement.
// Measured: 12743 ns/op, 961 B/op, 6 allocs/op (AMD Ryzen 9 9900X,
// go test -bench). Under -race the same run measures 8 allocs/op.
func BenchmarkSign(b *testing.B) {
	id, err := identity.New()
	if err != nil {
		b.Fatalf("New: %v", err)
	}
	m := testMessage()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, err := id.Sign(m); err != nil {
			b.Fatal(err)
		}
	}
}

// TestSignAllocBudget guards the allocation budget for Sign.
func TestSignAllocBudget(t *testing.T) {
	id, err := identity.New()
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	m := testMessage()
	alloc := testing.AllocsPerRun(1000, func() {
		if _, err := id.Sign(m); err != nil {
			t.Fatal(err)
		}
	})
	if alloc > signAllocBudget {
		t.Fatalf("Sign allocated %v times per call; budget is %d", alloc, signAllocBudget)
	}
}
