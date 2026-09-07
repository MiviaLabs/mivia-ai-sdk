package longtermmemory_test

import (
	"context"
	"fmt"
	"strings"
	"testing"

	"github.com/MiviaLabs/mivia-ai-sdk/longtermmemory"
)

// scopeCase is one scope-normalization table row: save under
// savedScope, query under queryScope.
type scopeCase struct {
	name       string
	savedScope string
	queryScope string
	promote    bool
}

// runScopeCases saves one entry under savedScope, then checks Search,
// Count, and, when promote is set, CoreEntries under queryScope.
func runScopeCases(t *testing.T, cases []scopeCase) {
	t.Helper()
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			s := longtermmemory.New(0)
			e := validEntry("Use indexes", "Add a database index for lookups")
			e.Scope = c.savedScope
			res, err := s.Save(context.Background(), e)
			if err != nil {
				t.Fatalf("Save: %v", err)
			}
			if c.promote {
				if err := s.PromoteToCore(context.Background(), res.ID); err != nil {
					t.Fatalf("PromoteToCore: %v", err)
				}
			}
			hits, err := s.Search(context.Background(), longtermmemory.Query{
				Text:  "database index",
				Scope: c.queryScope,
			})
			if err != nil {
				t.Fatalf("Search: %v", err)
			}
			if len(hits) != 1 || hits[0].Title != "Use indexes" {
				t.Fatalf("Search under %q = %+v, want one hit", c.queryScope, hits)
			}
			n, err := s.Count(context.Background(), c.queryScope)
			if err != nil {
				t.Fatalf("Count: %v", err)
			}
			if n != 1 {
				t.Fatalf("Count(%q) = %d, want 1", c.queryScope, n)
			}
			if !c.promote {
				return
			}
			entries, err := s.CoreEntries(context.Background(), c.queryScope)
			if err != nil {
				t.Fatalf("CoreEntries: %v", err)
			}
			if len(entries) != 1 || entries[0].ID != res.ID {
				t.Fatalf("CoreEntries(%q) = %+v, want the promoted entry %q", c.queryScope, entries, res.ID)
			}
		})
	}
}

// TestScopeNormalizedRoundTrip proves one padded spelling and one
// trimmed spelling reach the same bucket in both directions.
func TestScopeNormalizedRoundTrip(t *testing.T) {
	runScopeCases(t, []scopeCase{
		{
			name:       "save padded query trimmed",
			savedScope: "proj ",
			queryScope: "proj",
		},
		{
			name:       "save trimmed query padded",
			savedScope: "proj",
			queryScope: " proj",
		},
		{
			name:       "save tabs and newlines query trimmed",
			savedScope: "\tproj\n",
			queryScope: "proj",
		},
		{
			name:       "core entries round trip through padded scope",
			savedScope: "proj ",
			queryScope: "proj",
			promote:    true,
		},
	})
}

// TestScopeNormalizedIDDedupe proves two saves differing only by
// surrounding whitespace in Scope dedupe to one id.
func TestScopeNormalizedIDDedupe(t *testing.T) {
	s := longtermmemory.New(0)
	padded := validEntry("Title", "Summary")
	padded.Scope = "proj "
	first, err := s.Save(context.Background(), padded)
	if err != nil {
		t.Fatalf("Save padded: %v", err)
	}
	trimmed := validEntry("Title", "Summary")
	trimmed.Scope = "proj"
	trimmed.Created = first.Created
	second, err := s.Save(context.Background(), trimmed)
	if err != nil {
		t.Fatalf("Save trimmed: %v", err)
	}
	if first.ID != second.ID {
		t.Fatalf("trimmed re-save id = %q, want the padded save id %q", second.ID, first.ID)
	}
	n, _ := s.Count(context.Background(), "proj")
	if n != 1 {
		t.Fatalf("Count(proj) = %d, want 1: one scope, one row", n)
	}
	n, _ = s.Count(context.Background(), "proj ")
	if n != 1 {
		t.Fatalf("Count(%q) = %d, want 1: the padded spelling must resolve to the single row", "proj ", n)
	}
}

// TestScopeNormalizedStoredScope proves the stored and returned Scope
// carry the trimmed form, not the padded input.
func TestScopeNormalizedStoredScope(t *testing.T) {
	s := longtermmemory.New(0)
	e := validEntry("Title", "Summary")
	e.Scope = " proj "
	res, err := s.Save(context.Background(), e)
	if err != nil {
		t.Fatalf("Save: %v", err)
	}
	if res.Scope != "proj" {
		t.Fatalf("Result.Scope = %q, want the trimmed form", res.Scope)
	}
	hits, err := s.Search(context.Background(), longtermmemory.Query{Text: "summary", Scope: "proj"})
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if len(hits) != 1 || hits[0].Scope != "proj" {
		t.Fatalf("Search hits = %+v, want one hit with trimmed Scope", hits)
	}
}

// TestCoreFramePaddedScopeRoundTrip proves an entry saved under a
// padded scope renders in the frame queried by the clean form, in a
// deterministic order.
func TestCoreFramePaddedScopeRoundTrip(t *testing.T) {
	s := longtermmemory.New(0)
	first := validEntry("Alpha note", "First summary")
	first.Scope = "proj "
	res, err := s.Save(context.Background(), first)
	if err != nil {
		t.Fatalf("Save: %v", err)
	}
	if err := s.PromoteToCore(context.Background(), res.ID); err != nil {
		t.Fatalf("PromoteToCore: %v", err)
	}
	second := validEntry("Beta note", "Second summary")
	second.Scope = "proj"
	res2, err := s.Save(context.Background(), second)
	if err != nil {
		t.Fatalf("Save: %v", err)
	}
	if err := s.PromoteToCore(context.Background(), res2.ID); err != nil {
		t.Fatalf("PromoteToCore: %v", err)
	}
	frame, err := s.CoreFrame(context.Background(), "proj", 0)
	if err != nil {
		t.Fatalf("CoreFrame: %v", err)
	}
	if !strings.Contains(frame, "Alpha note") {
		t.Fatalf("frame misses the padded-scope entry: %q", frame)
	}
	alpha := strings.Index(frame, "Alpha note")
	beta := strings.Index(frame, "Beta note")
	if alpha == -1 || beta == -1 || alpha > beta {
		t.Fatalf("frame order = %q, want Alpha before Beta", frame)
	}
}

// TestMergeAcrossNormalizedEqualScopes proves consolidation merges a
// near-duplicate pair saved under normalized-equal scopes. Fixture:
// the pair plus six fillers fill the bucket; one trigger save crosses
// the load factor and runs the merge.
func TestMergeAcrossNormalizedEqualScopes(t *testing.T) {
	s := longtermmemory.New(10)
	padded := validEntry("Shared title", "One shared summary")
	padded.Scope = "proj "
	padded.Created = "2026-01-01"
	padded.Tags = []string{"alpha"}
	if _, err := s.Save(context.Background(), padded); err != nil {
		t.Fatalf("Save padded: %v", err)
	}
	trimmed := validEntry("Shared title", "One shared summary")
	trimmed.Scope = "proj"
	trimmed.Created = "2026-01-02"
	trimmed.Tags = []string{"beta"}
	if _, err := s.Save(context.Background(), trimmed); err != nil {
		t.Fatalf("Save trimmed: %v", err)
	}
	for i := 0; i < 6; i++ {
		e := distinct(fmt.Sprintf("Filler %d", i), fmt.Sprintf("2026-02-%02d", i+1))
		e.Scope = "proj"
		if _, err := s.Save(context.Background(), e); err != nil {
			t.Fatalf("Save filler %d: %v", i, err)
		}
	}
	trigger := distinct("Trigger note", "2026-03-01")
	trigger.Scope = "proj"
	if _, err := s.Save(context.Background(), trigger); err != nil {
		t.Fatalf("Save trigger: %v", err)
	}
	n, err := s.Count(context.Background(), "proj")
	if err != nil {
		t.Fatalf("Count: %v", err)
	}
	if n != 8 {
		t.Fatalf("Count(proj) = %d, want 8: merged pair, six fillers, one trigger", n)
	}
	n, _ = s.Count(context.Background(), "proj ")
	if n != 8 {
		t.Fatalf("Count(%q) = %d, want 8: the padded spelling must resolve to the same merged bucket", "proj ", n)
	}
}

// TestScopeCleanInputControl proves the fix keeps ids and dedupe
// behavior identical for already-clean scopes.
func TestScopeCleanInputControl(t *testing.T) {
	s := longtermmemory.New(0)
	e := validEntry("Title", "Summary")
	e.Scope = "proj"
	first, err := s.Save(context.Background(), e)
	if err != nil {
		t.Fatalf("Save: %v", err)
	}
	e.Created = first.Created
	second, err := s.Save(context.Background(), e)
	if err != nil {
		t.Fatalf("re-save: %v", err)
	}
	if first.ID != second.ID {
		t.Fatalf("clean re-save id = %q, want %q", second.ID, first.ID)
	}
	n, _ := s.Count(context.Background(), "proj")
	if n != 1 {
		t.Fatalf("Count(proj) = %d, want 1", n)
	}
}
