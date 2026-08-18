package usage_test

import (
	"errors"
	"testing"

	"github.com/MiviaLabs/mivia-ai-sdk/provider"
	"github.com/MiviaLabs/mivia-ai-sdk/usage"
)

func TestRecord(t *testing.T) {
	t.Run("first call sets the total", func(t *testing.T) {
		a := usage.New()
		u := provider.Usage{PromptTokens: 10, CompletionTokens: 20, TotalTokens: 30, CachedTokens: 5}
		if err := a.Record("session-1", u); err != nil {
			t.Fatalf("Record: %v", err)
		}
		got, ok := a.Total("session-1")
		if !ok {
			t.Fatal("Total: want true, got false")
		}
		if got != u {
			t.Fatalf("Total: got %+v, want %+v", got, u)
		}
	})

	t.Run("second call adds onto the first", func(t *testing.T) {
		a := usage.New()
		first := provider.Usage{PromptTokens: 10, CompletionTokens: 20, TotalTokens: 30, CachedTokens: 5}
		second := provider.Usage{PromptTokens: 1, CompletionTokens: 2, TotalTokens: 3, CachedTokens: 4}
		if err := a.Record("session-1", first); err != nil {
			t.Fatalf("Record: %v", err)
		}
		if err := a.Record("session-1", second); err != nil {
			t.Fatalf("Record: %v", err)
		}
		got, ok := a.Total("session-1")
		if !ok {
			t.Fatal("Total: want true, got false")
		}
		want := provider.Usage{PromptTokens: 11, CompletionTokens: 22, TotalTokens: 33, CachedTokens: 9}
		if got != want {
			t.Fatalf("Total: got %+v, want %+v", got, want)
		}
	})

	t.Run("three or more calls sum in order", func(t *testing.T) {
		a := usage.New()
		calls := []provider.Usage{
			{PromptTokens: 1, CompletionTokens: 1, TotalTokens: 2, CachedTokens: 0},
			{PromptTokens: 2, CompletionTokens: 2, TotalTokens: 4, CachedTokens: 1},
			{PromptTokens: 3, CompletionTokens: 3, TotalTokens: 6, CachedTokens: 2},
		}
		for _, u := range calls {
			if err := a.Record("session-1", u); err != nil {
				t.Fatalf("Record: %v", err)
			}
		}
		got, ok := a.Total("session-1")
		if !ok {
			t.Fatal("Total: want true, got false")
		}
		want := provider.Usage{PromptTokens: 6, CompletionTokens: 6, TotalTokens: 12, CachedTokens: 3}
		if got != want {
			t.Fatalf("Total: got %+v, want %+v", got, want)
		}
	})

}

// TestRecordBlankSessionID covers Record's blank-sessionID rejection
// cases separately from TestRecord to keep each function at or below
// the 80-line function-length gate.
func TestRecordBlankSessionID(t *testing.T) {
	for _, sessionID := range []string{"", " ", "\t\n"} {
		a := usage.New()
		existing := provider.Usage{PromptTokens: 7, CompletionTokens: 8, TotalTokens: 15, CachedTokens: 1}
		if err := a.Record("session-1", existing); err != nil {
			t.Fatalf("Record: %v", err)
		}

		err := a.Record(sessionID, provider.Usage{PromptTokens: 100})
		if !errors.Is(err, usage.ErrBlankSessionID) {
			t.Fatalf("Record(%q): got %v, want ErrBlankSessionID", sessionID, err)
		}

		got, ok := a.Total("session-1")
		if !ok {
			t.Fatal("Total: want true, got false")
		}
		if got != existing {
			t.Fatalf("Total: got %+v, want %+v", got, existing)
		}
	}
}

func TestTotal(t *testing.T) {
	t.Run("unknown sessionID returns zero and false", func(t *testing.T) {
		a := usage.New()
		got, ok := a.Total("unknown")
		if ok {
			t.Fatal("Total: want false, got true")
		}
		if got != (provider.Usage{}) {
			t.Fatalf("Total: got %+v, want zero value", got)
		}
	})

	t.Run("known sessionID returns the correct sum and true", func(t *testing.T) {
		a := usage.New()
		u := provider.Usage{PromptTokens: 4, CompletionTokens: 5, TotalTokens: 9, CachedTokens: 2}
		if err := a.Record("session-1", u); err != nil {
			t.Fatalf("Record: %v", err)
		}
		got, ok := a.Total("session-1")
		if !ok {
			t.Fatal("Total: want true, got false")
		}
		if got != u {
			t.Fatalf("Total: got %+v, want %+v", got, u)
		}
	})
}

func TestReset(t *testing.T) {
	t.Run("zeroes a recorded session", func(t *testing.T) {
		a := usage.New()
		if err := a.Record("session-1", provider.Usage{PromptTokens: 10}); err != nil {
			t.Fatalf("Record: %v", err)
		}
		if err := a.Reset("session-1"); err != nil {
			t.Fatalf("Reset: %v", err)
		}
		if _, ok := a.Total("session-1"); ok {
			t.Fatal("Total: want false after Reset, got true")
		}
	})

	t.Run("unknown session is a no-op returning nil", func(t *testing.T) {
		a := usage.New()
		if err := a.Reset("never-recorded"); err != nil {
			t.Fatalf("Reset: got %v, want nil", err)
		}
	})

	t.Run("blank sessionID returns ErrBlankSessionID", func(t *testing.T) {
		for _, sessionID := range []string{"", " ", "\t\n"} {
			a := usage.New()
			err := a.Reset(sessionID)
			if !errors.Is(err, usage.ErrBlankSessionID) {
				t.Fatalf("Reset(%q): got %v, want ErrBlankSessionID", sessionID, err)
			}
		}
	})

	t.Run("Record after Reset starts a fresh sum", func(t *testing.T) {
		a := usage.New()
		if err := a.Record("session-1", provider.Usage{PromptTokens: 100, CompletionTokens: 100, TotalTokens: 200, CachedTokens: 50}); err != nil {
			t.Fatalf("Record: %v", err)
		}
		if err := a.Reset("session-1"); err != nil {
			t.Fatalf("Reset: %v", err)
		}
		fresh := provider.Usage{PromptTokens: 1, CompletionTokens: 2, TotalTokens: 3, CachedTokens: 4}
		if err := a.Record("session-1", fresh); err != nil {
			t.Fatalf("Record: %v", err)
		}
		got, ok := a.Total("session-1")
		if !ok {
			t.Fatal("Total: want true, got false")
		}
		if got != fresh {
			t.Fatalf("Total: got %+v, want %+v (fresh, not carried over)", got, fresh)
		}
	})
}
