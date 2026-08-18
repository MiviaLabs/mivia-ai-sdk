package agentrun_test

import (
	"testing"

	"github.com/MiviaLabs/mivia-ai-sdk/agentrun"
	"github.com/MiviaLabs/mivia-ai-sdk/machine"
)

// TestPayloadOfReadsArtifact proves the closure returns the stored
// value for step and ignores the record argument.
func TestPayloadOfReadsArtifact(t *testing.T) {
	a := &agentrun.Artifacts{}
	a.Set("review", "result")
	fn := agentrun.PayloadOf("review", a)
	if got := fn(machine.InOut{Input: "ignored"}); got != "result" {
		t.Fatalf("PayloadOf = %q, want %q", got, "result")
	}
}

// TestPayloadOfNilArtifacts proves a nil Artifacts reads as empty and
// never panics.
func TestPayloadOfNilArtifacts(t *testing.T) {
	fn := agentrun.PayloadOf("review", nil)
	if got := fn(machine.InOut{}); got != "" {
		t.Fatalf("PayloadOf on nil = %q, want empty", got)
	}
}
