package longtermmemory_test

import (
	"context"
	"errors"
	"fmt"
	"testing"

	"github.com/MiviaLabs/mivia-ai-sdk/longtermmemory"
)

func TestPromoteToCoreCapsAtCoreTierCap(t *testing.T) {
	s := longtermmemory.New(0)
	var last string
	for i := 0; i < longtermmemory.CoreTierCap; i++ {
		e := distinct(fmt.Sprintf("Core %d", i), fmt.Sprintf("2026-01-%02d", (i%28)+1))
		res, err := s.Save(context.Background(), e)
		if err != nil {
			t.Fatalf("Save %d: %v", i, err)
		}
		if err := s.PromoteToCore(context.Background(), res.ID); err != nil {
			t.Fatalf("PromoteToCore %d: %v", i, err)
		}
		last = res.ID
	}
	if err := s.PromoteToCore(context.Background(), last); err != nil {
		t.Fatalf("already-core promote must be a no-op, got %v", err)
	}
	overflow := distinct("One too many", "2026-02-01")
	res, err := s.Save(context.Background(), overflow)
	if err != nil {
		t.Fatalf("Save overflow: %v", err)
	}
	err = s.PromoteToCore(context.Background(), res.ID)
	if !errors.Is(err, longtermmemory.ErrCoreTierFull) {
		t.Fatalf("PromoteToCore past the cap = %v, want ErrCoreTierFull", err)
	}
}

func TestCoreEntriesOrder(t *testing.T) {
	s := longtermmemory.New(0)
	saves := []struct{ title, created string }{
		{"Older", "2026-01-01"},
		{"Newest b", "2026-03-01"},
		{"Newest a", "2026-03-01"},
		{"Middle", "2026-02-01"},
	}
	ids := map[string]string{}
	for _, sv := range saves {
		e := distinct(sv.title, sv.created)
		res, err := s.Save(context.Background(), e)
		if err != nil {
			t.Fatalf("Save %s: %v", sv.title, err)
		}
		ids[sv.title] = res.ID
		if err := s.PromoteToCore(context.Background(), res.ID); err != nil {
			t.Fatalf("PromoteToCore %s: %v", sv.title, err)
		}
	}
	entries, err := s.CoreEntries(context.Background(), "proj")
	if err != nil {
		t.Fatalf("CoreEntries: %v", err)
	}
	if len(entries) != 4 {
		t.Fatalf("CoreEntries len = %d, want 4", len(entries))
	}
	if entries[0].Title != "Newest a" || entries[1].Title != "Newest b" {
		t.Fatalf("CoreEntries head = %q, %q, want created DESC then title ASC", entries[0].Title, entries[1].Title)
	}
	if entries[3].Title != "Older" {
		t.Fatalf("CoreEntries tail = %q, want Older", entries[3].Title)
	}
	archive := distinct("Archive row", "2026-04-01")
	if _, err := s.Save(context.Background(), archive); err != nil {
		t.Fatalf("Save archive: %v", err)
	}
	entries, _ = s.CoreEntries(context.Background(), "proj")
	if len(entries) != 4 {
		t.Fatalf("CoreEntries len = %d, want 4: archive rows stay out", len(entries))
	}
}
