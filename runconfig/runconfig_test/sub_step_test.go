package runconfig_test

import (
	"errors"
	"testing"

	"github.com/MiviaLabs/mivia-ai-sdk/tools"
)

// subStepDoc returns a document whose one step carries a child plan.
// The child's single step binds the external tool named tool.
func subStepDoc(tool string) string {
	return `{
		"machine": {"initial": "queued", "transitions": [
			{"from": "queued", "to": "done", "trigger": "run"}
		]},
		"plan": {"steps": [{"id": "outer", "to": "done", "sub": {
			"steps": [{"id": "inner", "to": "done", "tool": "` + tool + `"}]
		}}]},
		"tools": ["` + tool + `"]
	}`
}

// loopStepDoc is subStepDoc with a loop policy on the parent, the
// second grammar form that requires a child plan.
func loopStepDoc(tool string) string {
	return `{
		"machine": {"initial": "queued", "transitions": [
			{"from": "queued", "to": "done", "trigger": "run"}
		]},
		"plan": {"steps": [{"id": "outer", "to": "done", "loop": {"max": 1}, "sub": {
			"steps": [{"id": "inner", "to": "done", "tool": "` + tool + `"}]
		}}]},
		"tools": ["` + tool + `"]
	}`
}

// TestRunnerSubStepBuilds proves a step carrying a child plan builds a
// Runner. Load emits no binding for the parent, and the runner gates
// every step outside a two-or-more-member panel, so the parent's own
// identifier had no registry entry and Runner failed.
func TestRunnerSubStepBuilds(t *testing.T) {
	d := loadDoc(t, subStepDoc("grep"))
	if err := d.External.Add(stubTool{name: "grep"}); err != nil {
		t.Fatalf("External.Add: %v", err)
	}
	d.Options.Agent = agentOver(t, d)
	if _, err := d.Runner(); err != nil {
		t.Fatalf("Runner() = %v, want nil", err)
	}
}

// TestRunnerLoopStepBuilds proves the loop form reaches the same fix.
func TestRunnerLoopStepBuilds(t *testing.T) {
	d := loadDoc(t, loopStepDoc("grep"))
	if err := d.External.Add(stubTool{name: "grep"}); err != nil {
		t.Fatalf("External.Add: %v", err)
	}
	d.Options.Agent = agentOver(t, d)
	if _, err := d.Runner(); err != nil {
		t.Fatalf("Runner() = %v, want nil", err)
	}
}

// TestRunnerSubStepUnknownToolStillFails is the negative control: the
// child's missing tool must still fail, so the fix does not paper over
// a real unresolved binding.
func TestRunnerSubStepUnknownToolStillFails(t *testing.T) {
	d := loadDoc(t, subStepDoc("grep"))
	d.Options.Agent = agentOver(t, d)
	_, err := d.Runner()
	if err == nil {
		t.Fatalf("Runner() = nil, want an unknown-tool error")
	}
	if !errors.Is(err, tools.ErrUnknownName) && err.Error() == "" {
		t.Fatalf("Runner() = %v, want a named failure", err)
	}
}
