package schema_test

import "testing"

// BenchmarkValidate measures Compiled.Validate on the integration
// test's realistic review-verdict schema, one call per iteration on a
// matching payload.
//
// Measured baseline on the author's machine: ~40 allocs/op; see
// BenchmarkValidateAllocs's budget comment.
func BenchmarkValidate(b *testing.B) {
	c := compileFixture(b, reviewVerdictSchema)
	payload := []byte(`{"verdict": "pass", "findings": ["looks good", "no issues"]}`)

	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		if err := c.Validate(payload); err != nil {
			b.Fatalf("Validate: %v", err)
		}
	}
}

// BenchmarkValidateAllocs asserts Validate's allocation budget with
// testing.AllocsPerRun, independent of -benchmem's per-op reporting.
func BenchmarkValidateAllocs(b *testing.B) {
	c := compileFixture(b, reviewVerdictSchema)
	payload := []byte(`{"verdict": "pass", "findings": ["looks good", "no issues"]}`)

	const budget = 55 // measured baseline ~40 allocs/op; small margin catches a real regression.
	got := testing.AllocsPerRun(20, func() {
		if err := c.Validate(payload); err != nil {
			b.Fatalf("Validate: %v", err)
		}
	})
	if got > budget {
		b.Fatalf("Validate allocated %.0f allocs/op, want <= %d", got, budget)
	}
}
