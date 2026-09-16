package flow_test

import (
	"testing"
)

// BenchmarkValidate benchmarks Card.Validate over a card of one hundred capabilities.
func BenchmarkValidate(b *testing.B) {
	card := buildManyCapabilitiesCard()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if err := card.Validate(); err != nil {
			b.Fatal(err)
		}
	}
}
