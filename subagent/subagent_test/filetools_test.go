package subagent_test

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"sort"
	"testing"

	"github.com/MiviaLabs/mivia-ai-sdk/agentloop"
	"github.com/MiviaLabs/mivia-ai-sdk/diff"
	"github.com/MiviaLabs/mivia-ai-sdk/provider"
	"github.com/MiviaLabs/mivia-ai-sdk/schema"
	"github.com/MiviaLabs/mivia-ai-sdk/subagent"
	"github.com/MiviaLabs/mivia-ai-sdk/tools"
	"github.com/MiviaLabs/mivia-ai-sdk/workspace"
)

// openWorkspace opens a Workspace over a fresh temp directory.
func openWorkspace(t *testing.T) *workspace.Workspace {
	t.Helper()
	ws, err := workspace.Open(t.TempDir())
	if err != nil {
		t.Fatalf("workspace.Open: %v", err)
	}
	return ws
}

// fileTool is one file tool's fixture row: a build func over a bound
// Workspace, an args constructor taking the model-supplied path (and,
// where the tool needs it, fixed content), the raw JSON matching that
// args value, and the tool's declared tools.ExecutionClass.
type fileTool struct {
	label     string
	build     func(ws *workspace.Workspace) tools.Tool
	args      func(path string) any
	argsJSON  string
	wantClass tools.ExecutionClass
}

// fileTools lists all five file tools' fixture rows.
func fileTools() []fileTool {
	return []fileTool{
		{
			label:     "read",
			build:     func(ws *workspace.Workspace) tools.Tool { return subagent.WorkspaceReadTool("read", ws, 0) },
			args:      func(p string) any { return subagent.WorkspaceReadArgs{Path: p} },
			argsJSON:  `{"path":"a.txt"}`,
			wantClass: tools.ExecutionClassRead,
		},
		{
			label:     "write",
			build:     func(ws *workspace.Workspace) tools.Tool { return subagent.WorkspaceWriteTool("write", ws) },
			args:      func(p string) any { return subagent.WorkspaceWriteArgs{Path: p, Content: "x"} },
			argsJSON:  `{"path":"a.txt","content":"x"}`,
			wantClass: tools.ExecutionClassWrite,
		},
		{
			label:     "list",
			build:     func(ws *workspace.Workspace) tools.Tool { return subagent.WorkspaceListTool("list", ws) },
			args:      func(p string) any { return subagent.WorkspaceListArgs{Path: p} },
			argsJSON:  `{"path":"a.txt"}`,
			wantClass: tools.ExecutionClassRead,
		},
		{
			label:     "stat",
			build:     func(ws *workspace.Workspace) tools.Tool { return subagent.WorkspaceStatTool("stat", ws) },
			args:      func(p string) any { return subagent.WorkspaceStatArgs{Path: p} },
			argsJSON:  `{"path":"a.txt"}`,
			wantClass: tools.ExecutionClassRead,
		},
		{
			label:     "diff",
			build:     func(ws *workspace.Workspace) tools.Tool { return subagent.DiffTool("diff", ws, 0) },
			args:      func(p string) any { return subagent.DiffArgs{Path: p, Content: "x"} },
			argsJSON:  `{"path":"a.txt","content":"x"}`,
			wantClass: tools.ExecutionClassRead,
		},
	}
}

// TestFileToolsEscape proves every file tool rejects a traversal path
// and an absolute path with workspace.ErrEscape, unchanged: each tool
// wraps ws.resolve's error through its own argument-struct path, and
// this table proves none of the wrappers swallows or reshapes it.
func TestFileToolsEscape(t *testing.T) {
	paths := []struct {
		label string
		path  string
	}{
		{"traversal", "../outside.txt"},
		{"absolute", "/etc/passwd"},
	}
	for _, ft := range fileTools() {
		for _, p := range paths {
			t.Run(ft.label+"/"+p.label, func(t *testing.T) {
				ws := openWorkspace(t)
				tool := ft.build(ws)
				_, err := tool.Run(context.Background(), tools.InOut{Value: ft.args(p.path)})
				if !errors.Is(err, workspace.ErrEscape) {
					t.Fatalf("Run() error = %v, want workspace.ErrEscape", err)
				}
			})
		}
	}
}

// TestFileToolsBadArguments proves every file tool maps malformed
// DecodeArguments input, and a mistyped Run input, onto
// subagent.ErrBadArguments.
func TestFileToolsBadArguments(t *testing.T) {
	for _, ft := range fileTools() {
		t.Run(ft.label, func(t *testing.T) {
			ws := openWorkspace(t)
			tool := ft.build(ws)
			st, ok := tool.(tools.SchemaTool)
			if !ok {
				t.Fatalf("%s does not implement tools.SchemaTool", ft.label)
			}
			if _, err := st.DecodeArguments([]byte("not json")); !errors.Is(err, subagent.ErrBadArguments) {
				t.Fatalf("DecodeArguments() error = %v, want ErrBadArguments", err)
			}
			if _, err := tool.Run(context.Background(), tools.InOut{Value: "wrong-type"}); !errors.Is(err, subagent.ErrBadArguments) {
				t.Fatalf("Run() error = %v, want ErrBadArguments", err)
			}
		})
	}
}

// TestFileToolsSchemaCompile proves every file tool's ParameterSchema
// compiles under schema.Compile, the direct pin for the
// agentloop.New ErrInvalidSchema concern.
func TestFileToolsSchemaCompile(t *testing.T) {
	for _, ft := range fileTools() {
		t.Run(ft.label, func(t *testing.T) {
			ws := openWorkspace(t)
			st, ok := ft.build(ws).(tools.SchemaTool)
			if !ok {
				t.Fatalf("%s does not implement tools.SchemaTool", ft.label)
			}
			if _, err := schema.Compile(st.ParameterSchema()); err != nil {
				t.Fatalf("schema.Compile() error = %v, want nil", err)
			}
		})
	}
}

// TestFileToolsProfileAndDecode proves every file tool publishes its
// documented tools.ExecutionClass and that DecodeArguments on a
// well-formed payload reaches the tool's typed argument struct
// unchanged.
func TestFileToolsProfileAndDecode(t *testing.T) {
	for _, ft := range fileTools() {
		t.Run(ft.label, func(t *testing.T) {
			ws := openWorkspace(t)
			tool := ft.build(ws)
			profile := tools.ExecutionProfileOf(tool)
			if profile.Class != ft.wantClass {
				t.Fatalf("ExecutionProfile().Class = %q, want %q", profile.Class, ft.wantClass)
			}
			st, ok := tool.(tools.SchemaTool)
			if !ok {
				t.Fatalf("%s does not implement tools.SchemaTool", ft.label)
			}
			in, err := st.DecodeArguments([]byte(ft.argsJSON))
			if err != nil {
				t.Fatalf("DecodeArguments() error = %v, want nil", err)
			}
			if in.Value != ft.args("a.txt") {
				t.Fatalf("DecodeArguments() value = %+v, want %+v", in.Value, ft.args("a.txt"))
			}
		})
	}
}

// TestWorkspaceReadTool proves a read round-trips a file's exact
// content, a missing file fails, and MaxResultBytes publishes exactly
// when maxResultBytes is positive.
func TestWorkspaceReadTool(t *testing.T) {
	ws := openWorkspace(t)
	if err := os.WriteFile(filepath.Join(ws.Root(), "hello.txt"), []byte("hello world"), 0o600); err != nil {
		t.Fatalf("seed file: %v", err)
	}

	tool := subagent.WorkspaceReadTool("read", ws, 4)
	out, err := tool.Run(context.Background(), tools.InOut{Value: subagent.WorkspaceReadArgs{Path: "hello.txt"}})
	if err != nil {
		t.Fatalf("Run() error = %v, want nil", err)
	}
	if out.Value != "hello world" {
		t.Fatalf("Run() value = %q, want the full content regardless of MaxResultBytes", out.Value)
	}
	n, ok := tools.ResultBudgetOf(tool)
	if !ok || n != 4 {
		t.Fatalf("ResultBudgetOf() = %d,%v, want 4,true", n, ok)
	}

	unbudgeted := subagent.WorkspaceReadTool("read2", ws, 0)
	if _, ok := tools.ResultBudgetOf(unbudgeted); ok {
		t.Fatal("ResultBudgetOf() ok = true, want false for maxResultBytes: 0")
	}

	if _, err := tool.Run(context.Background(), tools.InOut{Value: subagent.WorkspaceReadArgs{Path: "missing.txt"}}); err == nil {
		t.Fatal("Run() succeeded, want a missing-file error")
	}
}

// TestWorkspaceWriteTool proves a write lands the exact content at
// mode 0o600, for both a new and an already-existing file, and that
// the returned tool reports privileged.
func TestWorkspaceWriteTool(t *testing.T) {
	ws := openWorkspace(t)
	tool := subagent.WorkspaceWriteTool("write", ws)
	if !tools.IsPrivileged(tool) {
		t.Fatal("IsPrivileged() = false, want true")
	}

	cases := []struct {
		label   string
		path    string
		content string
		seed    string
	}{
		{"new file", "new.txt", "fresh content", ""},
		{"existing file", "existing.txt", "replaced content", "old content"},
	}
	for _, c := range cases {
		t.Run(c.label, func(t *testing.T) {
			full := filepath.Join(ws.Root(), c.path)
			if c.seed != "" {
				if err := os.WriteFile(full, []byte(c.seed), 0o600); err != nil {
					t.Fatalf("seed file: %v", err)
				}
			}
			if _, err := tool.Run(context.Background(), tools.InOut{Value: subagent.WorkspaceWriteArgs{Path: c.path, Content: c.content}}); err != nil {
				t.Fatalf("Run() error = %v, want nil", err)
			}
			data, err := os.ReadFile(full)
			if err != nil {
				t.Fatalf("os.ReadFile: %v", err)
			}
			if string(data) != c.content {
				t.Fatalf("content = %q, want %q", data, c.content)
			}
			info, err := os.Stat(full)
			if err != nil {
				t.Fatalf("os.Stat: %v", err)
			}
			if info.Mode().Perm() != 0o600 {
				t.Fatalf("mode = %v, want 0o600", info.Mode().Perm())
			}
		})
	}
}

// TestWorkspaceWriteToolScopeDenied proves an unallowlisted call to
// the privileged write tool is denied by tools.Scope.
func TestWorkspaceWriteToolScopeDenied(t *testing.T) {
	ws := openWorkspace(t)
	reg := tools.New()
	if err := reg.Add(subagent.WorkspaceWriteTool("write", ws)); err != nil {
		t.Fatalf("reg.Add: %v", err)
	}
	scope := tools.NewScope(tools.ScopeOptions{})
	_, err := reg.RunScoped(context.Background(), "write",
		tools.InOut{Value: subagent.WorkspaceWriteArgs{Path: "a.txt", Content: "x"}}, scope)
	if !errors.Is(err, tools.ErrScopeDenied) {
		t.Fatalf("RunScoped() error = %v, want tools.ErrScopeDenied", err)
	}
}

// TestWorkspaceListTool proves a directory listing decodes into
// WorkspaceEntry rows matching the fixture tree.
func TestWorkspaceListTool(t *testing.T) {
	ws := openWorkspace(t)
	if err := os.WriteFile(filepath.Join(ws.Root(), "a.txt"), []byte("a"), 0o600); err != nil {
		t.Fatalf("seed file: %v", err)
	}
	if err := os.Mkdir(filepath.Join(ws.Root(), "sub"), 0o700); err != nil {
		t.Fatalf("seed dir: %v", err)
	}

	tool := subagent.WorkspaceListTool("list", ws)
	out, err := tool.Run(context.Background(), tools.InOut{Value: subagent.WorkspaceListArgs{Path: ""}})
	if err != nil {
		t.Fatalf("Run() error = %v, want nil", err)
	}
	entries, ok := out.Value.([]subagent.WorkspaceEntry)
	if !ok {
		t.Fatalf("Run() value type = %T, want []subagent.WorkspaceEntry", out.Value)
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
	ws := openWorkspace(t)
	if err := os.WriteFile(filepath.Join(ws.Root(), "a.txt"), []byte("hello"), 0o600); err != nil {
		t.Fatalf("seed file: %v", err)
	}
	if err := os.Mkdir(filepath.Join(ws.Root(), "sub"), 0o700); err != nil {
		t.Fatalf("seed dir: %v", err)
	}

	tool := subagent.WorkspaceStatTool("stat", ws)
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
		info, ok := out.Value.(subagent.WorkspaceFileInfo)
		if !ok {
			t.Fatalf("Run(%s) value type = %T, want subagent.WorkspaceFileInfo", c.path, out.Value)
		}
		if info.IsDir != c.wantDir {
			t.Fatalf("Run(%s) IsDir = %v, want %v", c.path, info.IsDir, c.wantDir)
		}
		if !c.wantDir && info.Size != c.wantSize {
			t.Fatalf("Run(%s) Size = %d, want %d", c.path, info.Size, c.wantSize)
		}
	}
}

// TestDiffTool proves DiffTool matches diff.Unified's own output for
// an existing file, diffs a missing path against empty content, and
// reports diff.ErrTooLarge over the configured maxLines bound.
func TestDiffTool(t *testing.T) {
	ws := openWorkspace(t)
	if err := os.WriteFile(filepath.Join(ws.Root(), "a.txt"), []byte("line one\nline two\n"), 0o600); err != nil {
		t.Fatalf("seed file: %v", err)
	}

	tool := subagent.DiffTool("diff", ws, 0)
	out, err := tool.Run(context.Background(), tools.InOut{Value: subagent.DiffArgs{Path: "a.txt", Content: "line one\nline three\n"}})
	if err != nil {
		t.Fatalf("Run() error = %v, want nil", err)
	}
	want, err := diff.Unified("a.txt", []byte("line one\nline two\n"), []byte("line one\nline three\n"), 0)
	if err != nil {
		t.Fatalf("diff.Unified() error = %v, want nil", err)
	}
	if out.Value != want {
		t.Fatalf("Run() value = %q, want %q", out.Value, want)
	}

	// A not-yet-existing path diffs against empty content.
	newOut, err := tool.Run(context.Background(), tools.InOut{Value: subagent.DiffArgs{Path: "missing.txt", Content: "new content\n"}})
	if err != nil {
		t.Fatalf("Run() error = %v, want nil", err)
	}
	wantNew, err := diff.Unified("missing.txt", nil, []byte("new content\n"), 0)
	if err != nil {
		t.Fatalf("diff.Unified() error = %v, want nil", err)
	}
	if newOut.Value != wantNew {
		t.Fatalf("Run() value = %q, want %q", newOut.Value, wantNew)
	}

	// A diff over maxLines fails with diff.ErrTooLarge.
	bounded := subagent.DiffTool("diff-bounded", ws, 1)
	if _, err := bounded.Run(context.Background(), tools.InOut{Value: subagent.DiffArgs{Path: "a.txt", Content: "line one\nline three\n"}}); !errors.Is(err, diff.ErrTooLarge) {
		t.Fatalf("Run() error = %v, want diff.ErrTooLarge", err)
	}
}

// filetoolsScriptedCompleter answers each Chat call with the next
// scripted response, in order. No concrete model client ships in this
// SDK, so this test scripts its own.
type filetoolsScriptedCompleter struct {
	responses []provider.Response
	calls     int
}

func (c *filetoolsScriptedCompleter) Name() string { return "scripted" }

func (c *filetoolsScriptedCompleter) Chat(ctx context.Context, req provider.Request) (provider.Response, error) {
	idx := c.calls
	c.calls++
	if idx >= len(c.responses) {
		return provider.Response{}, errors.New("filetoolsScriptedCompleter: no response scripted")
	}
	return c.responses[idx], nil
}

func (c *filetoolsScriptedCompleter) ChatStream(ctx context.Context, req provider.Request) (<-chan provider.Chunk, error) {
	return nil, errors.New("filetoolsScriptedCompleter: ChatStream not supported")
}

// TestFileToolsThroughAgentloop proves a scope allowlisting the write
// tool and DiffTool lets a model drive a write, then a diff over the
// written file, through agentloop.Run.
func TestFileToolsThroughAgentloop(t *testing.T) {
	ws := openWorkspace(t)
	reg := tools.New()
	writeTool := subagent.WorkspaceWriteTool("write", ws)
	diffTool := subagent.DiffTool("diff", ws, 0)
	if err := reg.Add(writeTool); err != nil {
		t.Fatalf("reg.Add(write): %v", err)
	}
	if err := reg.Add(diffTool); err != nil {
		t.Fatalf("reg.Add(diff): %v", err)
	}
	scope := tools.NewScope(tools.ScopeOptions{Allowlist: []string{"write", "diff"}})

	completer := &filetoolsScriptedCompleter{responses: []provider.Response{
		toolCallResponse(provider.ToolCall{ID: "call-1", Name: "write", Arguments: []byte(`{"path":"a.txt","content":"hello"}`)}),
		toolCallResponse(provider.ToolCall{ID: "call-2", Name: "diff", Arguments: []byte(`{"path":"a.txt","content":"hello world"}`)}),
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
	if res.Stop != agentloop.StopNoToolCalls {
		t.Fatalf("Stop = %q, want StopNoToolCalls", res.Stop)
	}
}

// TestFileToolsThroughAgentloopScopeDeniesWrite proves the same wiring
// without the write tool's name in Allowlist fails the requested
// write call with tools.ErrScopeDenied.
func TestFileToolsThroughAgentloopScopeDeniesWrite(t *testing.T) {
	ws := openWorkspace(t)
	reg := tools.New()
	if err := reg.Add(subagent.WorkspaceWriteTool("write", ws)); err != nil {
		t.Fatalf("reg.Add(write): %v", err)
	}
	if err := reg.Add(subagent.DiffTool("diff", ws, 0)); err != nil {
		t.Fatalf("reg.Add(diff): %v", err)
	}
	scope := tools.NewScope(tools.ScopeOptions{Allowlist: []string{"diff"}})

	completer := &filetoolsScriptedCompleter{responses: []provider.Response{
		toolCallResponse(provider.ToolCall{ID: "call-1", Name: "write", Arguments: []byte(`{"path":"a.txt","content":"hello"}`)}),
	}}
	loop, err := agentloop.New(agentloop.Options{
		Completer: completer, Tools: reg, Scope: scope, MaxIterations: 5, OnToolError: agentloop.ErrorPolicyFail,
	})
	if err != nil {
		t.Fatalf("agentloop.New() error = %v, want nil", err)
	}
	_, err = loop.Run(context.Background(), []provider.Message{textMsg(provider.RoleUser, "hi")})
	if !errors.Is(err, tools.ErrScopeDenied) {
		t.Fatalf("Run() error = %v, want tools.ErrScopeDenied", err)
	}
}

// textMsg builds a provider.Message with the given role and content.
func textMsg(role provider.Role, content string) provider.Message {
	return provider.Message{Role: role, Content: content}
}

// toolCallResponse builds a provider.Response whose Message and
// top-level ToolCalls both carry calls, matching agentloop's own
// fixture helper in agentloop_test.
func toolCallResponse(calls ...provider.ToolCall) provider.Response {
	return provider.Response{
		Message:   provider.Message{Role: provider.RoleAssistant, ToolCalls: calls},
		ToolCalls: calls,
	}
}
