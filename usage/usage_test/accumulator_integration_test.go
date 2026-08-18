package usage_test

import (
	"testing"

	"github.com/MiviaLabs/mivia-ai-sdk/provider"
	"github.com/MiviaLabs/mivia-ai-sdk/usage"
)

// TestMultiTurnSession simulates one multi-turn conversation: four
// sequential Record calls with distinct provider.Usage values,
// standing in for four model turns, followed by one Total call
// asserting the summed result. A second session recorded in the same
// test proves the first session's total is unaffected by the second.
func TestMultiTurnSession(t *testing.T) {
	a := usage.New()

	turns := []provider.Usage{
		{PromptTokens: 100, CompletionTokens: 20, TotalTokens: 120, CachedTokens: 0},
		{PromptTokens: 130, CompletionTokens: 25, TotalTokens: 155, CachedTokens: 90},
		{PromptTokens: 160, CompletionTokens: 30, TotalTokens: 190, CachedTokens: 120},
		{PromptTokens: 190, CompletionTokens: 40, TotalTokens: 230, CachedTokens: 150},
	}
	for i, u := range turns {
		if err := a.Record("conversation-1", u); err != nil {
			t.Fatalf("Record turn %d: %v", i, err)
		}
	}

	got, ok := a.Total("conversation-1")
	if !ok {
		t.Fatal("Total: want true, got false")
	}
	want := provider.Usage{PromptTokens: 580, CompletionTokens: 115, TotalTokens: 695, CachedTokens: 360}
	if got != want {
		t.Fatalf("Total(conversation-1): got %+v, want %+v", got, want)
	}

	otherTurns := []provider.Usage{
		{PromptTokens: 5, CompletionTokens: 5, TotalTokens: 10, CachedTokens: 0},
		{PromptTokens: 8, CompletionTokens: 4, TotalTokens: 12, CachedTokens: 2},
	}
	for i, u := range otherTurns {
		if err := a.Record("conversation-2", u); err != nil {
			t.Fatalf("Record other turn %d: %v", i, err)
		}
	}

	gotOther, ok := a.Total("conversation-2")
	if !ok {
		t.Fatal("Total: want true, got false")
	}
	wantOther := provider.Usage{PromptTokens: 13, CompletionTokens: 9, TotalTokens: 22, CachedTokens: 2}
	if gotOther != wantOther {
		t.Fatalf("Total(conversation-2): got %+v, want %+v", gotOther, wantOther)
	}

	gotFirstAgain, ok := a.Total("conversation-1")
	if !ok {
		t.Fatal("Total: want true, got false")
	}
	if gotFirstAgain != want {
		t.Fatalf("Total(conversation-1) after recording conversation-2: got %+v, want %+v (unaffected)", gotFirstAgain, want)
	}
}
