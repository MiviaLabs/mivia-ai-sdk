package run_test

import (
	"testing"

	"github.com/MiviaLabs/mivia-ai-sdk/workflow/run"
)

// TestDecodeArtifactsNilRunsThenSet proves a decoded Artifacts accepts
// a write when the blob carried a values object and no runs member.
// SetRun keyed its lazy initialization on values alone, so the append
// to runs wrote a nil map and panicked.
func TestDecodeArtifactsNilRunsThenSet(t *testing.T) {
	a, err := run.DecodeArtifacts([]byte(`{"values":{}}`))
	if err != nil {
		t.Fatalf("DecodeArtifacts: %v", err)
	}
	a.Set("step", "value")
	got, ok := a.Get("step")
	if !ok || got != "value" {
		t.Fatalf("Get = %q, %v; want \"value\", true", got, ok)
	}
	if n := len(a.History("step")); n != 1 {
		t.Fatalf("History length = %d, want 1", n)
	}
	if err := a.Validate(); err != nil {
		t.Fatalf("Validate after Set: %v", err)
	}
}
