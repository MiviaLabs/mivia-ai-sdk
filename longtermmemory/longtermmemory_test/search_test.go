package longtermmemory_test

import (
	"context"
	"errors"
	"testing"

	"github.com/MiviaLabs/mivia-ai-sdk/longtermmemory"
)

func TestSearchTokenMatches(t *testing.T) {
	s := longtermmemory.New(0)
	inDetail := validEntry("Unrelated title", "Plain summary")
	inDetail.Detail = "the connection string lives here"
	if _, err := s.Save(context.Background(), inDetail); err != nil {
		t.Fatalf("Save: %v", err)
	}
	hits, err := s.Search(context.Background(), longtermmemory.Query{Text: "connection", Scope: "proj"})
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if len(hits) != 1 {
		t.Fatalf("token in detail: hits = %d, want 1", len(hits))
	}
}

func TestSearchStopwordsAndCase(t *testing.T) {
	s := longtermmemory.New(0)
	e := validEntry("Database index", "Add a database index")
	if _, err := s.Save(context.Background(), e); err != nil {
		t.Fatalf("Save: %v", err)
	}
	for _, q := range []string{"the database", "DATABASE", "a database index"} {
		hits, err := s.Search(context.Background(), longtermmemory.Query{Text: q, Scope: "proj"})
		if err != nil {
			t.Fatalf("Search(%q): %v", q, err)
		}
		if len(hits) != 1 {
			t.Fatalf("Search(%q) hits = %d, want 1", q, len(hits))
		}
	}
}

func TestSearchEveryTokenRequired(t *testing.T) {
	s := longtermmemory.New(0)
	if _, err := s.Save(context.Background(), validEntry("Database only", "database summary")); err != nil {
		t.Fatalf("Save: %v", err)
	}
	both := validEntry("Database and cache", "database cache summary")
	if _, err := s.Save(context.Background(), both); err != nil {
		t.Fatalf("Save: %v", err)
	}
	hits, _ := s.Search(context.Background(), longtermmemory.Query{Text: "database cache", Scope: "proj"})
	if len(hits) != 1 || hits[0].Title != "Database and cache" {
		t.Fatalf("two-token search hits = %+v, want only the entry matching both", hits)
	}
}

func TestSearchPhraseFallback(t *testing.T) {
	s := longtermmemory.New(0)
	e := validEntry("Phrase title", "summary the and of detail")
	if _, err := s.Save(context.Background(), e); err != nil {
		t.Fatalf("Save: %v", err)
	}
	other := validEntry("Other", "no match here")
	if _, err := s.Save(context.Background(), other); err != nil {
		t.Fatalf("Save: %v", err)
	}
	hits, err := s.Search(context.Background(), longtermmemory.Query{Text: "the and of", Scope: "proj"})
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if len(hits) != 1 || hits[0].Title != "Phrase title" {
		t.Fatalf("phrase fallback hits = %+v, want the substring match only", hits)
	}
}

func TestSearchMaxResultsCap(t *testing.T) {
	s := longtermmemory.New(0)
	for i := 0; i < 10; i++ {
		e := validEntry("Shared topic", "shared summary")
		e.Created = "2026-01-01"
		e.Detail = "unique detail body"
		e.Title = "Shared topic"
		e.Summary = "shared summary " + string(rune('a'+i))
		if _, err := s.Save(context.Background(), e); err != nil {
			t.Fatalf("Save %d: %v", i, err)
		}
	}
	hits, err := s.Search(context.Background(), longtermmemory.Query{Text: "shared", Scope: "proj", MaxResults: 3})
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if len(hits) != 3 {
		t.Fatalf("capped hits = %d, want 3", len(hits))
	}
	def, _ := s.Search(context.Background(), longtermmemory.Query{Text: "shared", Scope: "proj"})
	if len(def) != longtermmemory.DefaultMaxSearchResults {
		t.Fatalf("default-capped hits = %d, want %d", len(def), longtermmemory.DefaultMaxSearchResults)
	}
}

func TestSearchOrderingTies(t *testing.T) {
	s := longtermmemory.New(0)
	for _, spec := range []struct{ title, created string }{
		{"Tie b", "2026-01-01"},
		{"Tie a", "2026-01-01"},
		{"Newer", "2026-02-01"},
	} {
		e := distinct(spec.title, spec.created)
		if _, err := s.Save(context.Background(), e); err != nil {
			t.Fatalf("Save %s: %v", spec.title, err)
		}
	}
	hits, _ := s.Search(context.Background(), longtermmemory.Query{Text: "summary", Scope: "proj"})
	if len(hits) != 3 {
		t.Fatalf("hits = %d, want 3", len(hits))
	}
	if hits[0].Title != "Newer" {
		t.Fatalf("first hit = %q, want Newer (created DESC)", hits[0].Title)
	}
	if hits[1].ID > hits[2].ID {
		t.Fatalf("tie order = %q then %q, want id ASC order", hits[1].Title, hits[2].Title)
	}
}

func TestSearchRequiredFields(t *testing.T) {
	s := longtermmemory.New(0)
	if _, err := s.Save(context.Background(), validEntry("Title", "Summary")); err != nil {
		t.Fatalf("Save: %v", err)
	}
	if _, err := s.Search(context.Background(), longtermmemory.Query{Text: "x", Scope: " "}); !errors.Is(err, longtermmemory.ErrScopeRequired) {
		t.Fatalf("Search with blank scope = %v, want ErrScopeRequired", err)
	}
	if _, err := s.Search(context.Background(), longtermmemory.Query{Text: "  ", Scope: "proj"}); !errors.Is(err, longtermmemory.ErrQueryRequired) {
		t.Fatalf("Search with blank text = %v, want ErrQueryRequired", err)
	}
}
