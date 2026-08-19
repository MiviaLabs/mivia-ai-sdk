package provider_test

import (
	"testing"

	"github.com/MiviaLabs/mivia-ai-sdk/provider"
)

func TestRedactBlock(t *testing.T) {
	b := provider.ReasoningBlock{Content: "secret chain of thought", Redacted: false}
	redacted := provider.RedactBlock(b)
	if redacted.Content != "" {
		t.Fatalf("Content = %q, want empty", redacted.Content)
	}
	if !redacted.Redacted {
		t.Fatal("Redacted = false, want true")
	}

	again := provider.RedactBlock(redacted)
	if again != redacted {
		t.Fatalf("RedactBlock is not idempotent: got %+v, want %+v", again, redacted)
	}
}
