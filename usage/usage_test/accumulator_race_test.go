package usage_test

import (
	"sync"
	"testing"

	"github.com/MiviaLabs/mivia-ai-sdk/provider"
	"github.com/MiviaLabs/mivia-ai-sdk/usage"
)

// TestRecordConcurrentSameSession proves no lost update: many
// goroutines Record against the same sessionID, and the final Total
// equals the arithmetic sum of every recorded provider.Usage. Run
// under go test -race.
func TestRecordConcurrentSameSession(t *testing.T) {
	a := usage.New()
	const goroutines = 100
	u := provider.Usage{PromptTokens: 1, CompletionTokens: 2, TotalTokens: 3, CachedTokens: 4}

	var wg sync.WaitGroup
	wg.Add(goroutines)
	for i := 0; i < goroutines; i++ {
		go func() {
			defer wg.Done()
			if err := a.Record("shared-session", u); err != nil {
				t.Error(err)
			}
		}()
	}
	wg.Wait()

	got, ok := a.Total("shared-session")
	if !ok {
		t.Fatal("Total: want true, got false")
	}
	want := provider.Usage{
		PromptTokens:     goroutines * u.PromptTokens,
		CompletionTokens: goroutines * u.CompletionTokens,
		TotalTokens:      goroutines * u.TotalTokens,
		CachedTokens:     goroutines * u.CachedTokens,
	}
	if got != want {
		t.Fatalf("Total: got %+v, want %+v", got, want)
	}
}

// TestRecordConcurrentDistinctSessions proves no cross-session
// interference: many goroutines Record concurrently across many
// distinct sessionID values, and every session's total is correct
// independently. Run under go test -race.
func TestRecordConcurrentDistinctSessions(t *testing.T) {
	a := usage.New()
	const sessions = 50
	const callsPerSession = 10
	u := provider.Usage{PromptTokens: 1, CompletionTokens: 1, TotalTokens: 2, CachedTokens: 1}

	sessionID := func(i int) string {
		return "session-" + string(rune('a'+i%26)) + string(rune('0'+i/26))
	}

	var wg sync.WaitGroup
	for s := 0; s < sessions; s++ {
		id := sessionID(s)
		for c := 0; c < callsPerSession; c++ {
			wg.Add(1)
			go func(id string) {
				defer wg.Done()
				if err := a.Record(id, u); err != nil {
					t.Error(err)
				}
			}(id)
		}
	}
	wg.Wait()

	want := provider.Usage{
		PromptTokens:     callsPerSession * u.PromptTokens,
		CompletionTokens: callsPerSession * u.CompletionTokens,
		TotalTokens:      callsPerSession * u.TotalTokens,
		CachedTokens:     callsPerSession * u.CachedTokens,
	}
	for s := 0; s < sessions; s++ {
		id := sessionID(s)
		got, ok := a.Total(id)
		if !ok {
			t.Fatalf("Total(%q): want true, got false", id)
		}
		if got != want {
			t.Fatalf("Total(%q): got %+v, want %+v", id, got, want)
		}
	}
}
