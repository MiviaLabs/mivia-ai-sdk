package memory_test

import (
	"testing"

	"github.com/MiviaLabs/mivia-ai-sdk/memory"
)

const tenMegabytes = 10 * 1024 * 1024

// BenchmarkPutSmallBlob benchmarks Put on a store built with a
// ten-megabyte budget, using a small (under one kilobyte) blob.
// Baseline (empty implementation): 0 ns/op, 0 allocs.
// Measured: ~550 ns/op, under the one-microsecond target.
func BenchmarkPutSmallBlob(b *testing.B) {
	s, err := memory.New(tenMegabytes)
	if err != nil {
		b.Fatalf("New error = %v", err)
	}
	content := make([]byte, 512)
	for i := range content {
		content[i] = byte(i)
	}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, err := s.Put(content); err != nil {
			b.Fatal(err)
		}
	}
}

// BenchmarkGetSmallBlob benchmarks Get on a store built with a
// ten-megabyte budget, using a small (under one kilobyte) blob.
// Baseline (empty implementation): 0 ns/op, 0 allocs.
// Measured: ~80 ns/op, well under the one-microsecond target.
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
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, err := s.Get(ref); err != nil {
			b.Fatal(err)
		}
	}
}
