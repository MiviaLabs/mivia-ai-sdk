package contextstate_test

import (
	"testing"

	"github.com/MiviaLabs/mivia-ai-sdk/contextstate"
)

// Measured allocation baselines on the current toolchain:
// Mint over one 4096-byte chunk allocates 4 (hash state, digest sum,
// hex encoding, prefix concat); Digest allocates 3. The counts are
// compiler-deterministic, so the budget equals the baseline: any
// regression adds at least one allocation.
const (
	mintAllocBudget4096   = 4
	digestAllocBudget4096 = 3
)

func TestMinterAllocationBudget(t *testing.T) {
	data := make([]byte, 4096)
	if n := testing.AllocsPerRun(100, func() { _ = contextstate.Mint(data) }); n > mintAllocBudget4096 {
		t.Fatalf("Mint of 4096 bytes allocated %v, budget %d", n, mintAllocBudget4096)
	}
	if n := testing.AllocsPerRun(100, func() { _ = contextstate.Digest(data) }); n > digestAllocBudget4096 {
		t.Fatalf("Digest of 4096 bytes allocated %v, budget %d", n, digestAllocBudget4096)
	}
}

func BenchmarkMint4096(b *testing.B) {
	data := make([]byte, 4096)
	b.SetBytes(int64(len(data)))
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = contextstate.Mint(data)
	}
}

func BenchmarkMintTwoChunks(b *testing.B) {
	head, tail := make([]byte, 2048), make([]byte, 2048)
	b.SetBytes(int64(len(head) + len(tail)))
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = contextstate.Mint(head, tail)
	}
}
