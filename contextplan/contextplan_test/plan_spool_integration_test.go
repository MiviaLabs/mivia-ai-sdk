package contextplan_test

import (
	"context"
	"strings"
	"testing"

	"github.com/MiviaLabs/mivia-ai-sdk/contextplan"
	"github.com/MiviaLabs/mivia-ai-sdk/contextstate"
	"github.com/MiviaLabs/mivia-ai-sdk/memory"
	"github.com/MiviaLabs/mivia-ai-sdk/provider"
	"github.com/MiviaLabs/mivia-ai-sdk/spool"
)

// TestPlanSpoolIntegrationRoundTrip seeds a MemStore with a session
// that overflows a small Window, wires a real *spool.Spool over a
// real *memory.Store (which satisfies spool.ContentStore with no
// import needed on either side), runs Plan, then resolves every
// non-empty Elision.SpoolRef back through Spool.Load. This is the
// cut-now, retrieve-later round trip the phase exists for.
func TestPlanSpoolIntegrationRoundTrip(t *testing.T) {
	store, cache := newStore(t), newCache(t)
	spoolBacking, err := memory.New(1 << 20)
	if err != nil {
		t.Fatalf("memory.New: %v", err)
	}
	sp, err := spool.NewSpool(spoolBacking, 4096)
	if err != nil {
		t.Fatalf("NewSpool: %v", err)
	}
	planner, err := contextplan.NewPlanner(store, cache, sp)
	if err != nil {
		t.Fatalf("NewPlanner: %v", err)
	}

	oldest := []byte(strings.Repeat("o", 40))
	middle := []byte(strings.Repeat("m", 40))
	newest := []byte(strings.Repeat("n", 40))
	refOldest := putPayloadForSubject(t, store, "sess-int", "subject-oldest", contextstate.RetentionSession, oldest)
	refMiddle := putPayloadForSubject(t, store, "sess-int", "subject-middle", contextstate.RetentionSession, middle)
	refNewest := putPayloadForSubject(t, store, "sess-int", "subject-newest", contextstate.RetentionSession, newest)

	sess := &contextstate.Session{Source: []contextstate.SourceEvent{
		sourceEvent("sess-int", 1, "message", string(provider.RoleUser), refOldest, len(oldest)),
		sourceEvent("sess-int", 2, "message", string(provider.RoleUser), refMiddle, len(middle)),
		sourceEvent("sess-int", 3, "message", string(provider.RoleUser), refNewest, len(newest)),
	}}

	result, err := planner.Plan(context.Background(), sess, contextplan.Window{MaxTokens: 40}, byteEstimator{})
	if err != nil {
		t.Fatalf("Plan: %v", err)
	}
	if len(result.Elisions) != 2 {
		t.Fatalf("Elisions = %+v, want 2 (oldest and middle dropped)", result.Elisions)
	}

	original := map[string][]byte{
		refOldest.Ref: oldest,
		refMiddle.Ref: middle,
		refNewest.Ref: newest,
	}
	subjectFor := map[string]string{
		refOldest.Ref: "subject-oldest",
		refMiddle.Ref: "subject-middle",
		refNewest.Ref: "subject-newest",
	}

	spooledCount := 0
	for _, e := range result.Elisions {
		if e.SpoolRef == "" {
			t.Fatalf("Elision %+v carries an empty SpoolRef, want every dropped payload spooled", e)
		}
		spooledCount++
		want, ok := original[e.Ref.Ref]
		if !ok {
			t.Fatalf("Elision.Ref %+v does not match a seeded payload", e.Ref)
		}
		got, err := sp.Load(context.Background(), subjectFor[e.Ref.Ref], e.SpoolRef)
		if err != nil {
			t.Fatalf("Load(%s): %v", e.SpoolRef, err)
		}
		if string(got) != string(want) {
			t.Fatalf("Load(%s) = %q, want %q", e.SpoolRef, got, want)
		}
	}
	if spooledCount != 2 {
		t.Fatalf("spooledCount = %d, want 2", spooledCount)
	}
}
