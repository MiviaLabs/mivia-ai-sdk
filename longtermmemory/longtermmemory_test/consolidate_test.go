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

// saveAll saves each entry in order and returns the stored ids.
func saveAll(t *testing.T, s *longtermmemory.Store, entries ...longtermmemory.Entry) []string {
	t.Helper()
	ids := make([]string, 0, len(entries))
	for i, e := range entries {
		res, err := s.Save(context.Background(), e)
		if err != nil {
			t.Fatalf("Save entry %d (%s): %v", i, e.Title, err)
		}
		ids = append(ids, res.ID)
	}
	return ids
}

// fillAndTrigger saves n distinct filler rows, then one trigger row
// that crosses the consolidation load factor. It returns the filler
// ids, not the trigger id.
func fillAndTrigger(t *testing.T, s *longtermmemory.Store, n int) []string {
	t.Helper()
	fillers := make([]longtermmemory.Entry, 0, n)
	for i := 0; i < n; i++ {
		fillers = append(fillers, distinct(fmt.Sprintf("Filler %d", i), fmt.Sprintf("2026-02-%02d", i+1)))
	}
	ids := saveAll(t, s, fillers...)
	saveAll(t, s, distinct("Trigger", "2026-03-01"))
	return ids
}

// searchOne runs one scope search and requires exactly one hit.
func searchOne(t *testing.T, s *longtermmemory.Store, text string) longtermmemory.Result {
	t.Helper()
	hits, err := s.Search(context.Background(), longtermmemory.Query{Text: text, Scope: "proj"})
	if err != nil {
		t.Fatalf("Search %q: %v", text, err)
	}
	if len(hits) != 1 {
		t.Fatalf("Search %q = %d hits, want 1 survivor", text, len(hits))
	}
	return hits[0]
}

// tagList builds n distinct tags sharing one prefix.
func tagList(prefix string, n int) []string {
	tags := make([]string, 0, n)
	for i := 0; i < n; i++ {
		tags = append(tags, fmt.Sprintf("%s%d", prefix, i))
	}
	return tags
}

// mergePair builds one near-duplicate archive pair: the earlier keep
// row and the later drop row, each with its own tags.
func mergePair(keepTags, dropTags []string) (longtermmemory.Entry, longtermmemory.Entry) {
	keep := validEntry("Deploy guide", "Ship the service safely")
	keep.Created = "2026-01-01"
	keep.Tags = keepTags
	drop := nearDup(keep, "drop detail")
	drop.Created = "2026-01-02"
	drop.Tags = dropTags
	return keep, drop
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

func TestConsolidationCapsMergedTags(t *testing.T) {
	s := longtermmemory.New(10)
	keep, drop := mergePair(tagList("keep", 8), tagList("drop", 8))
	saveAll(t, s, keep, drop)
	fillAndTrigger(t, s, 6)

	survivor := searchOne(t, s, "deploy")
	if len(survivor.Tags) != 8 {
		t.Fatalf("survivor tags = %d (%v), want 8: the union caps at the tag limit", len(survivor.Tags), survivor.Tags)
	}
	for i, want := range tagList("keep", 8) {
		if survivor.Tags[i] != want {
			t.Fatalf("survivor tag %d = %q, want %q: the keep row's tags fill the list first", i, survivor.Tags[i], want)
		}
	}
	rebuilt := validEntry("Deploy guide", "Ship the service safely")
	rebuilt.Tags = survivor.Tags
	if err := rebuilt.Validate(); err != nil {
		t.Fatalf("Validate on the survivor's tags = %v, want nil", err)
	}
}

func TestConsolidationRekeysMergedEntry(t *testing.T) {
	s := longtermmemory.New(10)
	keep, drop := mergePair([]string{"alpha"}, []string{"beta"})
	saveAll(t, s, keep, drop)
	fillAndTrigger(t, s, 6)

	survivor := searchOne(t, s, "deploy")
	before, _ := s.Count(context.Background(), "proj")
	again := keep
	again.Tags = survivor.Tags
	res, err := s.Save(context.Background(), again)
	if err != nil {
		t.Fatalf("re-save of the survivor's content: %v", err)
	}
	if res.ID != survivor.ID {
		t.Fatalf("re-save id = %q, want the survivor id %q: the merged entry keeps its content address", res.ID, survivor.ID)
	}
	after, _ := s.Count(context.Background(), "proj")
	if after != before {
		t.Fatalf("Count after the re-save = %d, want %d: no duplicate row", after, before)
	}
}

func TestConsolidationMergedIDDropsThePreMergeID(t *testing.T) {
	s := longtermmemory.New(10)
	keep, drop := mergePair([]string{"alpha"}, []string{"beta"})
	ids := saveAll(t, s, keep, drop)
	fillAndTrigger(t, s, 6)

	survivor := searchOne(t, s, "deploy")
	if survivor.ID == ids[0] {
		t.Fatalf("survivor id = %q, want a new address: the merged tags changed the content", survivor.ID)
	}
	if err := s.Delete(context.Background(), ids[0]); !errors.Is(err, longtermmemory.ErrEntryNotFound) {
		t.Fatalf("Delete on the pre-merge id = %v, want ErrEntryNotFound", err)
	}
}

func TestConsolidationKeepsCoreFlagAcrossRekey(t *testing.T) {
	s := longtermmemory.New(10)
	core := validEntry("Core entry", "Shared summary text")
	core.Created = "2026-01-02"
	core.Tags = []string{"alpha"}
	ids := saveAll(t, s, core)
	if err := s.PromoteToCore(context.Background(), ids[0]); err != nil {
		t.Fatalf("PromoteToCore: %v", err)
	}
	older := nearDup(core, "older detail")
	older.Created = "2026-01-01"
	older.Tags = []string{"beta"}
	saveAll(t, s, older)
	fillAndTrigger(t, s, 6)

	entries, err := s.CoreEntries(context.Background(), "proj")
	if err != nil {
		t.Fatalf("CoreEntries: %v", err)
	}
	if len(entries) != 1 {
		t.Fatalf("CoreEntries = %d rows, want 1: the re-key moves the core flag with the row", len(entries))
	}
	if entries[0].ID == ids[0] {
		t.Fatalf("core row id = %q, want a new address after the merge", entries[0].ID)
	}
	if err := s.PromoteToCore(context.Background(), ids[0]); !errors.Is(err, longtermmemory.ErrEntryNotFound) {
		t.Fatalf("PromoteToCore on the pre-merge id = %v, want ErrEntryNotFound", err)
	}
}

func TestConsolidationSurvivorAbsorbedByCore(t *testing.T) {
	s := longtermmemory.New(10)
	first := validEntry("Shared cluster", "One shared summary")
	first.Created = "2026-01-01"
	first.Tags = []string{"one"}
	second := nearDup(first, "second detail")
	second.Created = "2026-01-02"
	second.Tags = []string{"two"}
	core := nearDup(first, "core detail")
	core.Created = "2026-03-01"
	core.Tags = []string{"three"}
	ids := saveAll(t, s, first, second, core)
	if err := s.PromoteToCore(context.Background(), ids[2]); err != nil {
		t.Fatalf("PromoteToCore: %v", err)
	}
	fillAndTrigger(t, s, 5)

	survivor := searchOne(t, s, "cluster")
	entries, err := s.CoreEntries(context.Background(), "proj")
	if err != nil {
		t.Fatalf("CoreEntries: %v", err)
	}
	if len(entries) != 1 || entries[0].ID != survivor.ID {
		t.Fatalf("CoreEntries = %+v, want the one cluster survivor %q", entries, survivor.ID)
	}
	before, _ := s.Count(context.Background(), "proj")
	again := core
	again.Tags = survivor.Tags
	res, err := s.Save(context.Background(), again)
	if err != nil {
		t.Fatalf("re-save of the survivor's content: %v", err)
	}
	if res.ID != survivor.ID {
		t.Fatalf("re-save id = %q, want the survivor id %q", res.ID, survivor.ID)
	}
	after, _ := s.Count(context.Background(), "proj")
	if after != before {
		t.Fatalf("Count after the re-save = %d, want %d: no duplicate row", after, before)
	}
}

func TestConsolidationLeavesUnmergedIDsStable(t *testing.T) {
	s := longtermmemory.New(10)
	keep, drop := mergePair([]string{"alpha"}, []string{"beta"})
	saveAll(t, s, keep, drop)
	fillers := fillAndTrigger(t, s, 6)

	for i, id := range fillers {
		if err := s.PromoteToCore(context.Background(), id); err != nil {
			t.Fatalf("PromoteToCore on unmerged filler %d = %v, want nil: an unmerged row keeps its id", i, err)
		}
	}
}
