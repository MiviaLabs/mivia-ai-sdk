package longtermmemory_test

import (
	"context"
	"errors"
	"fmt"
	"testing"

	"github.com/MiviaLabs/mivia-ai-sdk/longtermmemory"
)

// distinct builds one distinct valid entry: distinct title and
// summary text, so no pair reaches the near-duplicate threshold.
func distinct(title, created string) longtermmemory.Entry {
	e := validEntry(title, "Summary about "+title)
	e.Created = created
	return e
}

// nearDup builds one entry whose title and summary match base's, so
// the pair's Jaccard similarity is one, with a distinct Detail so the
// ids differ.
func nearDup(base longtermmemory.Entry, detail string) longtermmemory.Entry {
	e := base
	e.Detail = detail
	return e
}

func TestConsolidationMergesNearDuplicates(t *testing.T) {
	s := longtermmemory.New(10)
	a1 := validEntry("Deploy guide", "Ship the service safely")
	a1.Created = "2026-01-01"
	a1.Tags = []string{"alpha"}
	a2 := nearDup(a1, "d2")
	a2.Created = "2026-01-02"
	a2.Tags = []string{"beta"}
	if _, err := s.Save(context.Background(), a1); err != nil {
		t.Fatalf("Save a1: %v", err)
	}
	if _, err := s.Save(context.Background(), a2); err != nil {
		t.Fatalf("Save a2: %v", err)
	}
	for i := 0; i < 6; i++ {
		e := distinct(fmt.Sprintf("Filler %d", i), fmt.Sprintf("2026-02-%02d", i+1))
		if _, err := s.Save(context.Background(), e); err != nil {
			t.Fatalf("Save filler %d: %v", i, err)
		}
	}
	if _, err := s.Save(context.Background(), distinct("Trigger", "2026-03-01")); err != nil {
		t.Fatalf("Save trigger: %v", err)
	}
	n, _ := s.Count(context.Background(), "proj")
	if n != 8 {
		t.Fatalf("Count after consolidation = %d, want 8: one pair merged among nine saves", n)
	}
	hits, err := s.Search(context.Background(), longtermmemory.Query{Text: "deploy", Scope: "proj"})
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if len(hits) != 1 {
		t.Fatalf("near-duplicate hits = %d, want 1 survivor", len(hits))
	}
	if len(hits[0].Tags) != 2 || hits[0].Tags[0] != "alpha" || hits[0].Tags[1] != "beta" {
		t.Fatalf("survivor tags = %v, want the union [alpha beta]", hits[0].Tags)
	}
	if hits[0].Created != "2026-01-01" {
		t.Fatalf("survivor created = %q, want the earlier row 2026-01-01", hits[0].Created)
	}
}

func TestConsolidationEvictsOldestArchiveWhenFull(t *testing.T) {
	s := longtermmemory.New(3)
	oldest := distinct("Oldest", "2026-01-01")
	res, err := s.Save(context.Background(), oldest)
	if err != nil {
		t.Fatalf("Save oldest: %v", err)
	}
	if _, err := s.Save(context.Background(), distinct("Middle", "2026-01-02")); err != nil {
		t.Fatalf("Save middle: %v", err)
	}
	if _, err := s.Save(context.Background(), distinct("Newest", "2026-01-03")); err != nil {
		t.Fatalf("Save newest: %v", err)
	}
	if _, err := s.Save(context.Background(), distinct("Arriving", "2026-01-04")); err != nil {
		t.Fatalf("Save arriving: %v", err)
	}
	n, _ := s.Count(context.Background(), "proj")
	if n != 3 {
		t.Fatalf("Count after eviction = %d, want 3", n)
	}
	if err := s.PromoteToCore(context.Background(), res.ID); !errors.Is(err, longtermmemory.ErrEntryNotFound) {
		t.Fatalf("PromoteToCore on the evicted row = %v, want ErrEntryNotFound", err)
	}
	hits, _ := s.Search(context.Background(), longtermmemory.Query{Text: "oldest", Scope: "proj"})
	if len(hits) != 0 {
		t.Fatalf("evicted row still searchable: %+v", hits)
	}
	for _, title := range []string{"middle", "arriving"} {
		hits, _ = s.Search(context.Background(), longtermmemory.Query{Text: title, Scope: "proj"})
		if len(hits) != 1 {
			t.Fatalf("search %q after eviction = %d hits, want 1", title, len(hits))
		}
	}
}

func TestConsolidationOneMergePassTwoPairs(t *testing.T) {
	s := longtermmemory.New(10)
	pairs := [][2]string{{"Pair one", "Detail one"}, {"Pair two", "Detail two"}}
	for _, p := range pairs {
		base := validEntry(p[0], "Summary about "+p[0])
		base.Created = "2026-01-01"
		if _, err := s.Save(context.Background(), base); err != nil {
			t.Fatalf("Save %s: %v", p[0], err)
		}
		dup := nearDup(base, p[1])
		dup.Created = "2026-01-02"
		if _, err := s.Save(context.Background(), dup); err != nil {
			t.Fatalf("Save %s dup: %v", p[0], err)
		}
	}
	for i := 0; i < 4; i++ {
		e := distinct(fmt.Sprintf("Solo %d", i), fmt.Sprintf("2026-02-%02d", i+1))
		if _, err := s.Save(context.Background(), e); err != nil {
			t.Fatalf("Save solo %d: %v", i, err)
		}
	}
	if _, err := s.Save(context.Background(), distinct("Trigger", "2026-03-01")); err != nil {
		t.Fatalf("Save trigger: %v", err)
	}
	n, _ := s.Count(context.Background(), "proj")
	if n != 7 {
		t.Fatalf("Count after one merge pass = %d, want 7: both pairs merged", n)
	}
	for _, title := range []string{"pair one", "pair two"} {
		hits, _ := s.Search(context.Background(), longtermmemory.Query{Text: title, Scope: "proj"})
		if len(hits) != 1 {
			t.Fatalf("search %q = %d hits, want 1 survivor", title, len(hits))
		}
	}
}

func TestConsolidationCoreNeverDeleted(t *testing.T) {
	s := longtermmemory.New(10)
	core := validEntry("Core entry", "Shared summary text")
	core.Created = "2026-01-02"
	coreRes, err := s.Save(context.Background(), core)
	if err != nil {
		t.Fatalf("Save core: %v", err)
	}
	if err := s.PromoteToCore(context.Background(), coreRes.ID); err != nil {
		t.Fatalf("PromoteToCore: %v", err)
	}
	older := nearDup(core, "older detail")
	older.Created = "2026-01-01"
	if _, err := s.Save(context.Background(), older); err != nil {
		t.Fatalf("Save older: %v", err)
	}
	for i := 0; i < 6; i++ {
		e := distinct(fmt.Sprintf("Filler %d", i), fmt.Sprintf("2026-02-%02d", i+1))
		if _, err := s.Save(context.Background(), e); err != nil {
			t.Fatalf("Save filler %d: %v", i, err)
		}
	}
	if _, err := s.Save(context.Background(), distinct("Trigger", "2026-03-01")); err != nil {
		t.Fatalf("Save trigger: %v", err)
	}
	n, _ := s.Count(context.Background(), "proj")
	if n != 8 {
		t.Fatalf("Count = %d, want 8: the core near-duplicate pair merged", n)
	}
	hits, _ := s.Search(context.Background(), longtermmemory.Query{Text: "shared", Scope: "proj"})
	if len(hits) != 1 {
		t.Fatalf("near-duplicate survivors = %d, want 1", len(hits))
	}
	if hits[0].Created != "2026-01-02" {
		t.Fatalf("survivor created = %q, want the core row 2026-01-02: the archive side was deleted", hits[0].Created)
	}
	entries, _ := s.CoreEntries(context.Background(), "proj")
	if len(entries) != 1 || entries[0].Created != "2026-01-02" {
		t.Fatalf("core rows = %+v, want the promoted survivor", entries)
	}
}
