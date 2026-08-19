package contextplan_test

import (
	"testing"

	"github.com/MiviaLabs/mivia-ai-sdk/contextplan"
	"github.com/MiviaLabs/mivia-ai-sdk/provider"
)

// compactionWindow builds a window with a percent trigger and a
// token-override target, the usual test shape.
func compactionWindow(b, triggerPercent, targetTokens, recentTail int) contextplan.Window {
	return contextplan.Window{
		MaxTokens:  b,
		Reserve:    0,
		Compaction: contextplan.Compaction{TriggerPercent: triggerPercent, TargetTokens: targetTokens, RecentTail: recentTail},
	}
}

func TestCompactBelowTriggerPassesThrough(t *testing.T) {
	msgs := []provider.Message{
		{Role: provider.RoleSystem, Content: "sys"},
		{Role: provider.RoleUser, Content: "hello"},
	}
	res, err := contextplan.Compact(msgs, contextplan.Window{MaxTokens: 1000}, byteEstimator{})
	if err != nil {
		t.Fatalf("Compact() = %v, want nil", err)
	}
	if res.Compacted {
		t.Fatal("Compacted = true below the trigger, want false")
	}
	if len(res.Kept) != 2 || len(res.Dropped) != 0 {
		t.Fatalf("Kept = %d, Dropped = %d, want 2 kept and 0 dropped", len(res.Kept), len(res.Dropped))
	}
	if res.BeforeTokens != 8 || res.AfterTokens != 8 {
		t.Fatalf("tokens before %d after %d, want 8 and 8", res.BeforeTokens, res.AfterTokens)
	}
	if res.Key == "" {
		t.Fatal("Key empty on the passthrough path")
	}
}

func TestCompactAtTriggerKeepsMandatorySet(t *testing.T) {
	msgs := []provider.Message{
		{Role: provider.RoleSystem, Content: "s"},
		{Role: provider.RoleUser, Content: repeat("u", 10)},
		{Role: provider.RoleAssistant, ToolCalls: []provider.ToolCall{{ID: "c1"}}, Content: repeat("a", 30)},
		{Role: provider.RoleTool, ToolCallID: "c1", Content: repeat("t", 30)},
		{Role: provider.RoleUser, Content: repeat("o", 13)},
		{Role: provider.RoleAssistant, Content: repeat("b", 50)},
	}
	res, err := contextplan.Compact(msgs, compactionWindow(100, 100, 10, 0), byteEstimator{})
	if err != nil {
		t.Fatalf("Compact() = %v, want nil", err)
	}
	if !res.Compacted {
		t.Fatal("Compacted = false at the trigger, want true")
	}
	if len(res.Kept) != 4 {
		t.Fatalf("Kept = %d messages, want 4 (system, user, assistant, tool)", len(res.Kept))
	}
	if res.Kept[0].Role != provider.RoleSystem || res.Kept[1].ToolCalls[0].ID != "c1" {
		t.Fatalf("mandatory set damaged: %+v", res.Kept[:2])
	}
	if len(res.Dropped) != 2 {
		t.Fatalf("Dropped = %d, want 2", len(res.Dropped))
	}
	if res.AfterTokens != 74 {
		t.Fatalf("AfterTokens = %d, want 74", res.AfterTokens)
	}
}

func TestCompactTailFillStopsAtTarget(t *testing.T) {
	msgs := []provider.Message{
		{Role: provider.RoleSystem, Content: "s"},
		{Role: provider.RoleUser, Content: repeat("o", 30)},
		{Role: provider.RoleAssistant, Content: repeat("a", 10)},
		{Role: provider.RoleUser, Content: repeat("l", 10)},
	}
	w := compactionWindow(100, 50, 40, 0)
	res, err := contextplan.Compact(msgs, w, byteEstimator{})
	if err != nil {
		t.Fatalf("Compact() = %v, want nil", err)
	}
	if !res.Compacted {
		t.Fatal("Compacted = false, want true")
	}
	if len(res.Kept) != 3 {
		t.Fatalf("Kept = %d, want 3 (system, assistant tail, latest user)", len(res.Kept))
	}
	if res.Kept[1].Role != provider.RoleAssistant {
		t.Fatalf("tail fill missing the newest optional unit: %+v", res.Kept)
	}
	if len(res.Dropped) != 1 || res.Dropped[0].Content != repeat("o", 30) {
		t.Fatalf("Dropped = %+v, want the oldest user message", res.Dropped)
	}
	if res.AfterTokens != 21 || res.AfterTokens > w.CompactTarget() {
		t.Fatalf("AfterTokens = %d, want 21 at or under target %d", res.AfterTokens, w.CompactTarget())
	}
}

func TestCompactTailFillStopsAtCountNeverResumes(t *testing.T) {
	msgs := []provider.Message{
		{Role: provider.RoleSystem, Content: "s"},
		{Role: provider.RoleUser, Content: repeat("x", 1200)},
		{Role: provider.RoleAssistant, Content: repeat("y", 50)},
		{Role: provider.RoleUser, Content: repeat("z", 50)},
	}
	w := compactionWindow(10000, 10, 9000, 1)
	res, err := contextplan.Compact(msgs, w, byteEstimator{})
	if err != nil {
		t.Fatalf("Compact() = %v, want nil", err)
	}
	if len(res.Kept) != 3 {
		t.Fatalf("Kept = %d, want 3", len(res.Kept))
	}
	if len(res.Dropped) != 1 || res.Dropped[0].Content != repeat("x", 1200) {
		t.Fatalf("Dropped = %+v, want only the oldest user message", res.Dropped)
	}
	if res.AfterTokens != 101 {
		t.Fatalf("AfterTokens = %d, want 101", res.AfterTokens)
	}
}

func TestCompactTailFillBoundsTwoUnits(t *testing.T) {
	msgs := []provider.Message{
		{Role: provider.RoleSystem, Content: "s"},
		{Role: provider.RoleUser, Content: "oldest"},
		{Role: provider.RoleUser, Content: "middle"},
		{Role: provider.RoleUser, Content: "newest"},
		{Role: provider.RoleUser, Content: repeat("l", 1100)},
	}
	w := compactionWindow(10000, 10, 9000, 2)
	res, err := contextplan.Compact(msgs, w, byteEstimator{})
	if err != nil {
		t.Fatalf("Compact() = %v, want nil", err)
	}
	kept := contentSet(res.Kept)
	for _, want := range []string{"middle", "newest", repeat("l", 1100)} {
		if !kept[want] {
			t.Fatalf("Kept missing %q: %+v", want, res.Kept)
		}
	}
	if kept["oldest"] {
		t.Fatal("Kept holds a third optional unit past the RecentTail bound of 2")
	}
}

func TestCompactPreserveNamesKeepsAgedMessage(t *testing.T) {
	msgs := []provider.Message{
		{Role: provider.RoleSystem, Content: "s"},
		{Role: provider.RoleUser, Name: "keep-me", Content: repeat("n", 200)},
		{Role: provider.RoleAssistant, Content: "a"},
		{Role: provider.RoleUser, Content: repeat("l", 6)},
	}
	w := contextplan.Window{MaxTokens: 1000, Compaction: contextplan.Compaction{
		TriggerPercent: 10, TargetPercent: 5, PreserveNames: []string{"keep-me"},
	}}
	res, err := contextplan.Compact(msgs, w, byteEstimator{})
	if err != nil {
		t.Fatalf("Compact() = %v, want nil", err)
	}
	if !res.Compacted {
		t.Fatal("Compacted = false, want true")
	}
	kept := contentSet(res.Kept)
	if !kept[repeat("n", 200)] {
		t.Fatalf("preserved message dropped: %+v", res.Kept)
	}
}

func TestCompactPreserveNameInsideUnitKeepsWholeUnit(t *testing.T) {
	msgs := []provider.Message{
		{Role: provider.RoleSystem, Content: "s"},
		{Role: provider.RoleAssistant, ToolCalls: []provider.ToolCall{{ID: "c1"}}, Content: repeat("a", 5)},
		{Role: provider.RoleTool, ToolCallID: "c1", Name: "keep-me", Content: repeat("t", 5)},
		{Role: provider.RoleUser, Content: repeat("l", 6)},
	}
	w := contextplan.Window{MaxTokens: 100, Compaction: contextplan.Compaction{
		TriggerPercent: 10, TargetPercent: 5, PreserveNames: []string{"keep-me"},
	}}
	res, err := contextplan.Compact(msgs, w, byteEstimator{})
	if err != nil {
		t.Fatalf("Compact() = %v, want nil", err)
	}
	if len(res.Kept) != 4 || len(res.Dropped) != 0 {
		t.Fatalf("Kept = %d, Dropped = %d, want the whole unit kept", len(res.Kept), len(res.Dropped))
	}
	if res.Kept[1].Role != provider.RoleAssistant || res.Kept[2].ToolCallID != "c1" {
		t.Fatalf("unit folded incorrectly: %+v", res.Kept)
	}
}

// repeat builds n copies of s.
func repeat(s string, n int) string {
	out := make([]byte, 0, n*len(s))
	for i := 0; i < n; i++ {
		out = append(out, s...)
	}
	return string(out)
}

// contentSet indexes one message list by content.
func contentSet(msgs []provider.Message) map[string]bool {
	set := make(map[string]bool, len(msgs))
	for _, m := range msgs {
		set[m.Content] = true
	}
	return set
}
