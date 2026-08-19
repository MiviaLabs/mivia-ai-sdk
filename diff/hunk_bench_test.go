package diff

import (
	"testing"
)

func BenchmarkRenderUnified(b *testing.B) {
	name := "example.go"
	aLen, bLen := 1000, 1000
	var ops []op
	for i := 0; i < 1000; i++ {
		ops = append(ops, op{kind: ' ', text: "func someFunction() {", aIdx: i, bIdx: i})
	}
	hunks := []hunkRange{{start: 0, end: 1000}}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		renderUnified(name, ops, hunks, aLen, bLen, false, false)
	}
}
