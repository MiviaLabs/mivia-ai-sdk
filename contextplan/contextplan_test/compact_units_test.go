package contextplan_test

import (
	"errors"
	"strings"
	"testing"

	"github.com/MiviaLabs/mivia-ai-sdk/contextplan"
	"github.com/MiviaLabs/mivia-ai-sdk/provider"
)

func TestCompactLatestCompleteAssistantUnitSurvivesWhole(t *testing.T) {
	msgs := []provider.Message{
		{Role: provider.RoleSystem, Content: "s"},
		{Role: provider.RoleAssistant, ToolCalls: []provider.ToolCall{{ID: "c1"}}, Content: repeat("a", 40)},
		{Role: provider.RoleTool, ToolCallID: "c1", Content: repeat("r", 40)},
		{Role: provider.RoleAssistant, ToolCalls: []provider.ToolCall{{ID: "c2"}}, Content: repeat("A", 40)},
		{Role: provider.RoleTool, ToolCallID: "c2", Content: repeat("R", 40)},
		{Role: provider.RoleUser, Content: repeat("l", 6)},
	}
	res, err := contextplan.Compact(msgs, compactionWindow(1000, 10, 5, 0), byteEstimator{})
	if err != nil {
		t.Fatalf("Compact() = %v, want nil", err)
	}
	if len(res.Kept) != 4 {
		t.Fatalf("Kept = %d, want 4", len(res.Kept))
	}
	if res.Kept[1].ToolCalls[0].ID != "c2" || res.Kept[2].ToolCallID != "c2" {
		t.Fatalf("latest complete unit not kept whole: %+v", res.Kept)
	}
	if len(res.Dropped) != 2 || res.Dropped[0].ToolCalls[0].ID != "c1" {
		t.Fatalf("Dropped = %+v, want the older unit whole", res.Dropped)
	}
}

func TestCompactIncompleteAssistantUnitNotSelected(t *testing.T) {
	msgs := []provider.Message{
		{Role: provider.RoleSystem, Content: "s"},
		{Role: provider.RoleAssistant, ToolCalls: []provider.ToolCall{{ID: "c1"}, {ID: "c2"}}, Content: repeat("a", 10)},
		{Role: provider.RoleTool, ToolCallID: "c1", Content: repeat("t", 10)},
		{Role: provider.RoleUser, Content: repeat("l", 6)},
	}
	res, err := contextplan.Compact(msgs, compactionWindow(100, 10, 5, 0), byteEstimator{})
	if err != nil {
		t.Fatalf("Compact() = %v, want nil", err)
	}
	kept := contentSet(res.Kept)
	if kept[repeat("a", 10)] {
		t.Fatalf("incomplete assistant unit selected: %+v", res.Kept)
	}
	if kept[repeat("t", 10)] {
		t.Fatalf("reply of an unselected unit selected: %+v", res.Kept)
	}
}

func TestCompactMismatchingReplyIsOwnUnit(t *testing.T) {
	msgs := []provider.Message{
		{Role: provider.RoleSystem, Content: "s"},
		{Role: provider.RoleAssistant, ToolCalls: []provider.ToolCall{{ID: "c1"}}, Content: repeat("a", 10)},
		{Role: provider.RoleTool, ToolCallID: "other", Name: "keep", Content: repeat("t", 10)},
		{Role: provider.RoleUser, Content: repeat("l", 6)},
	}
	w := contextplan.Window{MaxTokens: 100, Compaction: contextplan.Compaction{
		TriggerPercent: 10, TargetPercent: 5, PreserveNames: []string{"keep"},
	}}
	res, err := contextplan.Compact(msgs, w, byteEstimator{})
	if err != nil {
		t.Fatalf("Compact() = %v, want nil", err)
	}
	kept := contentSet(res.Kept)
	if !kept[repeat("t", 10)] {
		t.Fatalf("preserved reply dropped: %+v", res.Kept)
	}
	if kept[repeat("a", 10)] {
		t.Fatalf("mismatching reply folded into the assistant's unit: %+v", res.Kept)
	}
}

func TestCompactKeptToolNeverAnswersDroppedCall(t *testing.T) {
	msgs := []provider.Message{
		{Role: provider.RoleSystem, Content: "s"},
		{Role: provider.RoleAssistant, ToolCalls: []provider.ToolCall{{ID: "c1"}}, Content: repeat("a", 40)},
		{Role: provider.RoleTool, ToolCallID: "c1", Content: repeat("r", 40)},
		{Role: provider.RoleUser, Content: repeat("l", 6)},
	}
	res, err := contextplan.Compact(msgs, compactionWindow(1000, 10, 5, 0), byteEstimator{})
	if err != nil {
		t.Fatalf("Compact() = %v, want nil", err)
	}
	keptIDs := make(map[string]bool)
	for _, m := range res.Kept {
		if m.Role == provider.RoleAssistant {
			for _, c := range m.ToolCalls {
				keptIDs[c.ID] = true
			}
		}
	}
	for _, m := range res.Kept {
		if m.Role == provider.RoleTool && !keptIDs[m.ToolCallID] {
			t.Fatalf("kept tool reply %q answers no kept assistant call", m.ToolCallID)
		}
	}
}

func TestCompactNoObjective(t *testing.T) {
	msgs := []provider.Message{
		{Role: provider.RoleSystem, Content: "s"},
		{Role: provider.RoleAssistant, Content: "a"},
	}
	_, err := contextplan.Compact(msgs, contextplan.Window{MaxTokens: 100000}, byteEstimator{})
	if !errors.Is(err, contextplan.ErrNoObjective) {
		t.Fatalf("Compact() error = %v, want errors.Is ErrNoObjective", err)
	}
}

func TestCompactFailures(t *testing.T) {
	boom := errors.New("estimate boom")
	cases := []struct {
		name    string
		msgs    []provider.Message
		w       contextplan.Window
		e       byteEstimator
		wantErr error
	}{
		{
			name:    "empty input",
			msgs:    nil,
			w:       contextplan.Window{MaxTokens: 100},
			e:       byteEstimator{},
			wantErr: contextplan.ErrNoMessages,
		},
		{
			name:    "estimator error",
			msgs:    []provider.Message{{Role: provider.RoleUser, Content: "hi"}},
			w:       contextplan.Window{MaxTokens: 100},
			e:       byteEstimator{err: boom},
			wantErr: contextplan.ErrEstimateFailed,
		},
		{
			name:    "invalid window",
			msgs:    []provider.Message{{Role: provider.RoleUser, Content: "hi"}},
			w:       contextplan.Window{MaxTokens: 0},
			e:       byteEstimator{},
			wantErr: contextplan.ErrMaxTokensNotPositive,
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			res, err := contextplan.Compact(c.msgs, c.w, c.e)
			if !errors.Is(err, c.wantErr) {
				t.Fatalf("Compact() error = %v, want errors.Is %v", err, c.wantErr)
			}
			if len(res.Kept) != 0 || len(res.Dropped) != 0 {
				t.Fatalf("Compact() returned a partial result alongside %v", err)
			}
		})
	}
}

func TestCompactRetentionOverflow(t *testing.T) {
	msgs := []provider.Message{
		{Role: provider.RoleSystem, Content: "s"},
		{Role: provider.RoleUser, Content: repeat("u", 40)},
	}
	_, err := contextplan.Compact(msgs, contextplan.Window{MaxTokens: 30}, byteEstimator{})
	if !errors.Is(err, contextplan.ErrRetentionOverflow) {
		t.Fatalf("Compact() error = %v, want errors.Is ErrRetentionOverflow", err)
	}
}

func TestCompactIdempotency(t *testing.T) {
	toolCall := func(args string) provider.ToolCall {
		return provider.ToolCall{ID: "c1", Name: "search", Arguments: []byte(args)}
	}
	build := func(args string) []provider.Message {
		return []provider.Message{
			{Role: provider.RoleSystem, Content: "s"},
			{Role: provider.RoleAssistant, ToolCalls: []provider.ToolCall{toolCall(args)}, Content: repeat("a", 40)},
			{Role: provider.RoleTool, ToolCallID: "c1", Content: repeat("r", 40)},
			{Role: provider.RoleUser, Content: repeat("l", 6)},
		}
	}
	w := compactionWindow(1000, 10, 5, 0)
	first, err := contextplan.Compact(build(`{"q":"x"}`), w, byteEstimator{})
	if err != nil {
		t.Fatalf("Compact() = %v, want nil", err)
	}
	second, err := contextplan.Compact(build(`{"q":"x"}`), w, byteEstimator{})
	if err != nil {
		t.Fatalf("Compact() = %v, want nil", err)
	}
	if first.Key != second.Key {
		t.Fatalf("equal inputs produced keys %q and %q", first.Key, second.Key)
	}
	if !messagesEqual(first.Kept, second.Kept) || !messagesEqual(first.Dropped, second.Dropped) {
		t.Fatal("equal inputs produced different kept or dropped lists")
	}
	third, err := contextplan.Compact(build(`{"q":"y"}`), w, byteEstimator{})
	if err != nil {
		t.Fatalf("Compact() = %v, want nil", err)
	}
	if third.Key == first.Key {
		t.Fatal("tool call arguments missing from the fingerprint: keys equal")
	}
	if !strings.HasPrefix(third.Key, contextplan.CompactionAlgorithm+":") {
		t.Fatalf("key %q lacks the %q prefix", third.Key, contextplan.CompactionAlgorithm)
	}
}

// messagesEqual compares two message lists field by field.
func messagesEqual(a, b []provider.Message) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i].Role != b[i].Role || a[i].Content != b[i].Content ||
			a[i].ToolCallID != b[i].ToolCallID || a[i].Name != b[i].Name ||
			len(a[i].ToolCalls) != len(b[i].ToolCalls) {
			return false
		}
	}
	return true
}
