package subagent_test

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"sort"
	"testing"

	"github.com/MiviaLabs/mivia-ai-sdk/agentloop"
	"github.com/MiviaLabs/mivia-ai-sdk/provider"
	"github.com/MiviaLabs/mivia-ai-sdk/subagent"
	"github.com/MiviaLabs/mivia-ai-sdk/tools"
)

// TestWorkspaceListTool proves a directory listing decodes into
// WorkspaceEntry rows matching the fixture tree.
func TestWorkspaceListTool(t *testing.T) {
	ft, root := openFileTools(t, nil)
	if err := os.WriteFile(filepath.Join(root, "a.txt"), []byte("a"), 0o600); err != nil {
		t.Fatalf("seed file: %v", err)
	}
	if err := os.Mkdir(filepath.Join(root, "sub"), 0o700); err != nil {
		t.Fatalf("seed dir: %v", err)
	}

	tool := subagent.WorkspaceListTool("list", ft)
	out, err := tool.Run(context.Background(), tools.InOut{Value: subagent.WorkspaceListArgs{Path: ""}})
	if err != nil {
		t.Fatalf("Run() error = %v, want nil", err)
	}
	raw, ok := out.Value.(string)
	if !ok {
		t.Fatalf("Run() value type = %T, want string", out.Value)
	}
	var entries []subagent.WorkspaceEntry
	if err := json.Unmarshal([]byte(raw), &entries); err != nil {
		t.Fatalf("json.Unmarshal() error = %v, want nil", err)
	}
	sort.Slice(entries, func(i, j int) bool { return entries[i].Name < entries[j].Name })
	want := []subagent.WorkspaceEntry{{Name: "a.txt", IsDir: false}, {Name: "sub", IsDir: true}}
	if len(entries) != len(want) || entries[0] != want[0] || entries[1] != want[1] {
		t.Fatalf("entries = %+v, want %+v", entries, want)
	}
}

// TestWorkspaceStatTool proves a stat decodes into a WorkspaceFileInfo
// matching the fixture file and directory.
func TestWorkspaceStatTool(t *testing.T) {
	ft, root := openFileTools(t, nil)
	if err := os.WriteFile(filepath.Join(root, "a.txt"), []byte("hello"), 0o600); err != nil {
		t.Fatalf("seed file: %v", err)
	}
	if err := os.Mkdir(filepath.Join(root, "sub"), 0o700); err != nil {
		t.Fatalf("seed dir: %v", err)
	}

	tool := subagent.WorkspaceStatTool("stat", ft)
	cases := []struct {
		path      string
		wantDir   bool
		wantSize  int64
		wantEmpty bool
	}{
		{"a.txt", false, 5, false},
		{"sub", true, 0, false},
	}
	for _, c := range cases {
		out, err := tool.Run(context.Background(), tools.InOut{Value: subagent.WorkspaceStatArgs{Path: c.path}})
		if err != nil {
			t.Fatalf("Run(%s) error = %v, want nil", c.path, err)
		}
		raw, ok := out.Value.(string)
		if !ok {
			t.Fatalf("Run(%s) value type = %T, want string", c.path, out.Value)
		}
		var info subagent.WorkspaceFileInfo
		if err := json.Unmarshal([]byte(raw), &info); err != nil {
			t.Fatalf("json.Unmarshal() error = %v, want nil", err)
		}
		if info.IsDir != c.wantDir {
			t.Fatalf("Run(%s) IsDir = %v, want %v", c.path, info.IsDir, c.wantDir)
		}
		if !c.wantDir && info.Size != c.wantSize {
			t.Fatalf("Run(%s) Size = %d, want %d", c.path, info.Size, c.wantSize)
		}
	}
}

// TestFileToolsListThroughAgentloopRendersJSON proves agentloop.Loop
// renders WorkspaceListTool's string result byte-identically to an
// independently computed json.Marshal of the seeded
// []subagent.WorkspaceEntry rows, pinning renderValue's string branch
// against its pre-fix json.Marshal fallback branch.
func TestFileToolsListThroughAgentloopRendersJSON(t *testing.T) {
	ft, root := openFileTools(t, nil)
	if err := os.WriteFile(filepath.Join(root, "a.txt"), []byte("a"), 0o600); err != nil {
		t.Fatalf("seed file: %v", err)
	}
	if err := os.Mkdir(filepath.Join(root, "sub"), 0o700); err != nil {
		t.Fatalf("seed dir: %v", err)
	}

	reg := tools.New()
	listTool := subagent.WorkspaceListTool("list", ft)
	if err := reg.Add(listTool); err != nil {
		t.Fatalf("reg.Add(list): %v", err)
	}
	scope := tools.NewScope(tools.ScopeOptions{Allowlist: []string{"list"}})

	completer := &filetoolsScriptedCompleter{responses: []provider.Response{
		toolCallResponse(provider.ToolCall{ID: "call-1", Name: "list", Arguments: []byte(`{"path":""}`)}),
		{Message: textMsg(provider.RoleAssistant, "done")},
	}}
	loop, err := agentloop.New(agentloop.Options{Completer: completer, Tools: reg, Scope: scope, MaxIterations: 5})
	if err != nil {
		t.Fatalf("agentloop.New() error = %v, want nil", err)
	}
	res, err := loop.Run(context.Background(), []provider.Message{textMsg(provider.RoleUser, "hi")})
	if err != nil {
		t.Fatalf("Run() error = %v, want nil", err)
	}

	want, err := json.Marshal([]subagent.WorkspaceEntry{{Name: "a.txt", IsDir: false}, {Name: "sub", IsDir: true}})
	if err != nil {
		t.Fatalf("json.Marshal() error = %v, want nil", err)
	}

	var toolMsg *provider.Message
	for i := range res.History {
		if res.History[i].Role == provider.RoleTool && res.History[i].ToolCallID == "call-1" {
			toolMsg = &res.History[i]
			break
		}
	}
	if toolMsg == nil {
		t.Fatal("History has no RoleTool message for call-1")
	}
	if toolMsg.Content != string(want) {
		t.Fatalf("RoleTool content = %q, want %q", toolMsg.Content, want)
	}
}
