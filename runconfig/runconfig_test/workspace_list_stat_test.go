package runconfig_test

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"sort"
	"testing"

	"github.com/MiviaLabs/mivia-ai-sdk/agentrun"
	"github.com/MiviaLabs/mivia-ai-sdk/machine"
	"github.com/MiviaLabs/mivia-ai-sdk/runconfig"
	"github.com/MiviaLabs/mivia-ai-sdk/secretpath"
	"github.com/MiviaLabs/mivia-ai-sdk/subagent"
)

// workspaceListStepDoc names one step bound to WorkspaceListKind, with
// a payload holding the JSON-string-encoded subagent.WorkspaceListArgs
// form.
const workspaceListStepDoc = `{
	"machine": {"initial": "queued", "transitions": [
		{"from": "queued", "to": "done", "trigger": "run"}
	]},
	"plan": {"steps": [{"id": "s1", "to": "done", "internal": "workspacelist",
		"payload": "{\"path\":\"\"}"}]},
	"tools": []
}`

// TestRunnerResolvesWorkspaceListReal proves WorkspaceListKind composes
// with the real subagent and workspace types, driven end to end through
// a real Runner.Run: WorkspaceListTool.Run now returns a JSON-encoded
// string, so agentrun's chain accepts it and the run reaches status
// done with the seeded directory listing as the step's artifact.
func TestRunnerResolvesWorkspaceListReal(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "a.txt"), []byte("a"), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	if err := os.Mkdir(filepath.Join(dir, "sub"), 0o755); err != nil {
		t.Fatalf("Mkdir: %v", err)
	}
	deny, err := secretpath.NewMatcher([]string{"*.env", "*.pem"})
	if err != nil {
		t.Fatalf("NewMatcher: %v", err)
	}
	ft, err := subagent.OpenFileTools(subagent.FileToolOptions{Root: dir, Deny: deny})
	if err != nil {
		t.Fatalf("OpenFileTools: %v", err)
	}
	defer ft.Close()
	tool := subagent.WorkspaceListTool("s1", ft)

	d := loadDoc(t, workspaceListStepDoc)
	blocks := runconfig.NewBlocks()
	blocks.Set(runconfig.WorkspaceListKind, tool)
	d.Blocks = blocks
	d.Options.Agent = agentOver(t, d)
	art := &agentrun.Artifacts{}
	d.Options.Artifacts = art

	runner, err := d.Runner()
	if err != nil {
		t.Fatalf("Runner: %v", err)
	}
	status, _, err := runner.Run(context.Background(), "thread-workspacelist", machine.InOut{})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if status != "done" {
		t.Fatalf("status = %q, want done", status)
	}
	artifact, ok := art.Get("s1")
	if !ok {
		t.Fatal("artifact missing for step s1")
	}
	var entries []subagent.WorkspaceEntry
	if err := json.Unmarshal([]byte(artifact), &entries); err != nil {
		t.Fatalf("json.Unmarshal: %v", err)
	}
	sort.Slice(entries, func(i, j int) bool { return entries[i].Name < entries[j].Name })
	want := []subagent.WorkspaceEntry{{Name: "a.txt", IsDir: false}, {Name: "sub", IsDir: true}}
	if len(entries) != len(want) || entries[0] != want[0] || entries[1] != want[1] {
		t.Fatalf("entries = %+v, want %+v", entries, want)
	}
}

// workspaceStatStepDoc names one step bound to WorkspaceStatKind, with
// a payload holding the JSON-string-encoded subagent.WorkspaceStatArgs
// form.
const workspaceStatStepDoc = `{
	"machine": {"initial": "queued", "transitions": [
		{"from": "queued", "to": "done", "trigger": "run"}
	]},
	"plan": {"steps": [{"id": "s1", "to": "done", "internal": "workspacestat",
		"payload": "{\"path\":\"notes.txt\"}"}]},
	"tools": []
}`

// TestRunnerResolvesWorkspaceStatReal proves WorkspaceStatKind composes
// with the real subagent and workspace types, driven end to end through
// a real Runner.Run: WorkspaceStatTool.Run now returns a JSON-encoded
// string, so agentrun's chain accepts it and the run reaches status
// done with the seeded file's stat result as the step's artifact.
func TestRunnerResolvesWorkspaceStatReal(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "notes.txt"), []byte("hello workspace"), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	deny, err := secretpath.NewMatcher([]string{"*.env", "*.pem"})
	if err != nil {
		t.Fatalf("NewMatcher: %v", err)
	}
	ft, err := subagent.OpenFileTools(subagent.FileToolOptions{Root: dir, Deny: deny})
	if err != nil {
		t.Fatalf("OpenFileTools: %v", err)
	}
	defer ft.Close()
	tool := subagent.WorkspaceStatTool("s1", ft)

	d := loadDoc(t, workspaceStatStepDoc)
	blocks := runconfig.NewBlocks()
	blocks.Set(runconfig.WorkspaceStatKind, tool)
	d.Blocks = blocks
	d.Options.Agent = agentOver(t, d)
	art := &agentrun.Artifacts{}
	d.Options.Artifacts = art

	runner, err := d.Runner()
	if err != nil {
		t.Fatalf("Runner: %v", err)
	}
	status, _, err := runner.Run(context.Background(), "thread-workspacestat", machine.InOut{})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if status != "done" {
		t.Fatalf("status = %q, want done", status)
	}
	artifact, ok := art.Get("s1")
	if !ok {
		t.Fatal("artifact missing for step s1")
	}
	var info subagent.WorkspaceFileInfo
	if err := json.Unmarshal([]byte(artifact), &info); err != nil {
		t.Fatalf("json.Unmarshal: %v", err)
	}
	if info.Name != "notes.txt" || info.Size != int64(len("hello workspace")) || info.IsDir {
		t.Fatalf("info = %+v, want name notes.txt, size %d, IsDir false", info, len("hello workspace"))
	}
}
