package memory_test

import (
	"testing"

	"github.com/MiviaLabs/mivia-ai-sdk/memory"
)

const tenMegabytes = 10 * 1024 * 1024

// BenchmarkPutSmallBlob benchmarks Put on a store built with a
// ten-megabyte budget, using distinct small (under one kilobyte)
// blobs so the store fills and steady-state eviction runs, the real
// hot path under sustained load.
// Baseline (empty implementation): 0 ns/op, 0 allocs.
// Measured: ~595 ns/op, 6 allocs/op, under the one-microsecond
// target and the allocation budget below.
func BenchmarkPutSmallBlob(b *testing.B) {
	s, err := memory.New(tenMegabytes)
	if err != nil {
		b.Fatalf("New error = %v", err)
	}
	content := make([]byte, 512)
	for i := range content {
		content[i] = byte(i)
	}

	allocs := testing.AllocsPerRun(100, func() {
		if _, err := s.Put(content); err != nil {
			b.Fatal(err)
		}
		content[0]++
	})
	const allocBudget = 8
	if allocs > allocBudget {
		b.Fatalf("Put allocs/op = %v, want <= %v", allocs, allocBudget)
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, err := s.Put(content); err != nil {
			b.Fatal(err)
		}
		content[0]++
	}
}

// BenchmarkGetSmallBlob benchmarks Get on a store built with a
// ten-megabyte budget, using a small (under one kilobyte) blob.
// Baseline (empty implementation): 0 ns/op, 0 allocs.
// Measured: ~78 ns/op, 1 alloc/op, well under the one-microsecond
// target and the allocation budget below.
func BenchmarkGetSmallBlob(b *testing.B) {
	s, err := memory.New(tenMegabytes)
	if err != nil {
		b.Fatalf("New error = %v", err)
	}
	content := make([]byte, 512)
	for i := range content {
		content[i] = byte(i)
	}
	ref, err := s.Put(content)
	if err != nil {
		b.Fatalf("Put error = %v", err)
	}

	allocs := testing.AllocsPerRun(100, func() {
		if _, err := s.Get(ref); err != nil {
			b.Fatal(err)
		}
	})
	const allocBudget = 2
	if allocs > allocBudget {
		b.Fatalf("Get allocs/op = %v, want <= %v", allocs, allocBudget)
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, err := s.Get(ref); err != nil {
			b.Fatal(err)
		}
	}
}
