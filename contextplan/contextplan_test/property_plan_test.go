package contextplan_test

import (
	"context"
	"fmt"
	"strings"
	"testing"

	"github.com/MiviaLabs/mivia-ai-sdk/contextplan"
	"github.com/MiviaLabs/mivia-ai-sdk/contextstate"
	"github.com/MiviaLabs/mivia-ai-sdk/provider"
)

// TestPropertyPlanNeverExceedsWindow asserts EstimatedTokens never
// exceeds w.Budget() across varied session sizes, Window values, and
// retention mixes, including boundary cases where a stub barely fits
// or barely does not.
func TestPropertyPlanNeverExceedsWindow(t *testing.T) {
	type spec struct {
		size      int
		retention contextstate.RetentionClass
	}
	cases := []struct {
		name    string
		specs   []spec
		maxTok  int
		reserve int
	}{
		{"empty session", nil, 100, 0},
		{"single small event", []spec{{10, contextstate.RetentionSession}}, 100, 0},
		{"single event over budget", []spec{{500, contextstate.RetentionSession}}, 50, 0},
		{"compliance stub over budget", []spec{{500, contextstate.RetentionCompliance}}, 50, 0},
		{"compliance stub exactly at boundary", []spec{{500, contextstate.RetentionCompliance}}, contextplan.StubContentBytes, 0},
		{"compliance stub one under boundary", []spec{{500, contextstate.RetentionCompliance}}, contextplan.StubContentBytes - 1, 0},
		{"many mixed retention events", []spec{
			{50, contextstate.RetentionSession},
			{500, contextstate.RetentionCompliance},
			{500, contextstate.RetentionCompliance},
			{50, contextstate.RetentionSession},
			{500, contextstate.RetentionCompliance},
		}, 400, 20},
		{"headroom reserved", []spec{{80, contextstate.RetentionSession}}, 100, 90},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			store := newStore(t)
			cache := newCache(t)
			planner, err := contextplan.NewPlanner(store, cache, nil)
			if err != nil {
				t.Fatalf("NewPlanner: %v", err)
			}
			var events []contextstate.SourceEvent
			for i, s := range tc.specs {
				data := []byte(strings.Repeat(fmt.Sprintf("%d", i%10), s.size))
				ref := putPayload(t, store, "sess-prop", s.retention, data)
				events = append(events, sourceEvent("sess-prop", uint64(i+1), "message", string(provider.RoleUser), ref, len(data)))
			}
			sess := &contextstate.Session{Source: events}
			w := contextplan.Window{MaxTokens: tc.maxTok, Reserve: tc.reserve}
			result, err := planner.Plan(context.Background(), sess, w, byteEstimator{})
			if err != nil {
				t.Fatalf("Plan: %v", err)
			}
			if result.EstimatedTokens > w.Budget() {
				t.Fatalf("EstimatedTokens = %d, exceeds budget %d", result.EstimatedTokens, w.Budget())
			}
		})
	}
}
