package longtermmemory_test

import (
	"context"
	"errors"
	"fmt"
	"testing"

	"github.com/MiviaLabs/mivia-ai-sdk/longtermmemory"
)

func TestSaveSearchCountRoundTrip(t *testing.T) {
	s := longtermmemory.New(0)
	if _, err := s.Save(context.Background(), validEntry("Use indexes", "Add a database index for lookups")); err != nil {
		t.Fatalf("Save: %v", err)
	}
	other := validEntry("Other", "Other summary")
	other.Scope = "other-scope"
	if _, err := s.Save(context.Background(), other); err != nil {
		t.Fatalf("Save: %v", err)
	}
	hits, err := s.Search(context.Background(), longtermmemory.Query{Text: "database index", Scope: "proj"})
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if len(hits) != 1 || hits[0].Title != "Use indexes" {
		t.Fatalf("Search hits = %+v, want the saved entry", hits)
	}
	n, err := s.Count(context.Background(), "proj")
	if err != nil || n != 1 {
		t.Fatalf("Count(proj) = %d, %v, want 1, nil", n, err)
	}
	n, err = s.Count(context.Background(), "other-scope")
	if err != nil || n != 1 {
		t.Fatalf("Count(other-scope) = %d, %v, want 1, nil", n, err)
	}
}

func TestSaveIdenticalResaveIdempotent(t *testing.T) {
	s := longtermmemory.New(0)
	e := validEntry("Title", "Summary")
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
		t.Fatalf("re-save id = %q, want %q", second.ID, first.ID)
	}
	n, _ := s.Count(context.Background(), "proj")
	if n != 1 {
		t.Fatalf("Count after re-save = %d, want 1: no duplicate stored", n)
	}
}

func TestSaveInvalidEntryFails(t *testing.T) {
	s := longtermmemory.New(0)
	bad := validEntry("Title", "")
	_, err := s.Save(context.Background(), bad)
	if err == nil {
		t.Fatal("Save of an invalid entry = nil, want the wrapped Validate error")
	}
	n, _ := s.Count(context.Background(), "proj")
	if n != 0 {
		t.Fatalf("Count after a failed save = %d, want 0", n)
	}
}

func TestSaveFillsCreatedToday(t *testing.T) {
	s := longtermmemory.New(0)
	e := validEntry("Title", "Summary")
	e.Created = ""
	res, err := s.Save(context.Background(), e)
	if err != nil {
		t.Fatalf("Save: %v", err)
	}
	if len(res.Created) != 10 || res.Created[4] != '-' || res.Created[7] != '-' {
		t.Fatalf("Created = %q, want a filled YYYY-MM-DD date", res.Created)
	}
}

func TestSaveStoreFullOnlyCore(t *testing.T) {
	s := longtermmemory.New(2)
	e1, _ := s.Save(context.Background(), validEntry("One", "First summary"))
	e2, _ := s.Save(context.Background(), validEntry("Two", "Second summary"))
	if err := s.PromoteToCore(context.Background(), e1.ID); err != nil {
		t.Fatalf("PromoteToCore: %v", err)
	}
	if err := s.PromoteToCore(context.Background(), e2.ID); err != nil {
		t.Fatalf("PromoteToCore: %v", err)
	}
	_, err := s.Save(context.Background(), validEntry("Three", "Third summary"))
	if !errors.Is(err, longtermmemory.ErrStoreFull) {
		t.Fatalf("Save past capacity into an all-core scope = %v, want ErrStoreFull", err)
	}
	n, _ := s.Count(context.Background(), "proj")
	if n != 2 {
		t.Fatalf("Count after ErrStoreFull = %d, want 2: no deletion ran", n)
	}
}

func TestDeleteRemovesEntry(t *testing.T) {
	s := longtermmemory.New(0)
	res, err := s.Save(context.Background(), validEntry("Doomed", "Soon gone"))
	if err != nil {
		t.Fatalf("Save: %v", err)
	}
	if err := s.PromoteToCore(context.Background(), res.ID); err != nil {
		t.Fatalf("PromoteToCore: %v", err)
	}
	if err := s.Delete(context.Background(), res.ID); err != nil {
		t.Fatalf("Delete: %v", err)
	}
	n, _ := s.Count(context.Background(), "proj")
	if n != 0 {
		t.Fatalf("Count after Delete = %d, want 0", n)
	}
	hits, _ := s.Search(context.Background(), longtermmemory.Query{Text: "doomed", Scope: "proj"})
	if len(hits) != 0 {
		t.Fatalf("deleted entry still searchable: %+v", hits)
	}
	entries, _ := s.CoreEntries(context.Background(), "proj")
	if len(entries) != 0 {
		t.Fatalf("deleted core entry still listed: %+v", entries)
	}
}

func TestUnknownIDFailures(t *testing.T) {
	s := longtermmemory.New(0)
	if err := s.PromoteToCore(context.Background(), "missing"); !errors.Is(err, longtermmemory.ErrEntryNotFound) {
		t.Fatalf("PromoteToCore(unknown) = %v, want ErrEntryNotFound", err)
	}
	if err := s.Delete(context.Background(), "missing"); !errors.Is(err, longtermmemory.ErrEntryNotFound) {
		t.Fatalf("Delete(unknown) = %v, want ErrEntryNotFound", err)
	}
}

func TestSaveDedupesAgainstAMintedID(t *testing.T) {
	s := longtermmemory.New(10)
	for i := 0; i < 6; i++ {
		e := distinct(fmt.Sprintf("Filler %d", i), fmt.Sprintf("2026-02-%02d", i+1))
		if _, err := s.Save(context.Background(), e); err != nil {
			t.Fatalf("Save filler %d: %v", i, err)
		}
	}
	promoted := validEntry("Promoted note", "One shared summary")
	promoted.Created = "2026-01-02"
	promoted.Tags = []string{"alpha"}
	res, err := s.Save(context.Background(), promoted)
	if err != nil {
		t.Fatalf("Save promoted: %v", err)
	}
	if err := s.PromoteToCore(context.Background(), res.ID); err != nil {
		t.Fatalf("PromoteToCore: %v", err)
	}
	dup := nearDup(promoted, "older detail")
	dup.Created = "2026-01-01"
	dup.Tags = []string{"beta"}
	if _, err := s.Save(context.Background(), dup); err != nil {
		t.Fatalf("Save near-duplicate: %v", err)
	}

	colliding := promoted
	colliding.Tags = []string{"alpha", "beta"}
	got, err := s.Save(context.Background(), colliding)
	if err != nil {
		t.Fatalf("Save of the minted content: %v", err)
	}
	entries, err := s.CoreEntries(context.Background(), "proj")
	if err != nil {
		t.Fatalf("CoreEntries: %v", err)
	}
	if len(entries) != 1 {
		t.Fatalf("CoreEntries = %d rows, want 1: the save must not overwrite the merged core row", len(entries))
	}
	if entries[0].ID != got.ID {
		t.Fatalf("core row id = %q, want the saved id %q", entries[0].ID, got.ID)
	}
	n, _ := s.Count(context.Background(), "proj")
	if n != 7 {
		t.Fatalf("Count after the colliding save = %d, want 7", n)
	}
}
