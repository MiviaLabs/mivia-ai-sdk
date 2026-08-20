package e2e_test

import (
	"context"
	"fmt"
	"strings"
	"testing"

	"github.com/MiviaLabs/mivia-ai-sdk/agentrun"
	"github.com/MiviaLabs/mivia-ai-sdk/flow"
	"github.com/MiviaLabs/mivia-ai-sdk/machine"
	"github.com/MiviaLabs/mivia-ai-sdk/memory"
	"github.com/MiviaLabs/mivia-ai-sdk/spool"
	"github.com/MiviaLabs/mivia-ai-sdk/tools"
)

// readSpooledOutputName is the registered name of the tool
// spool.ReadOutputTool builds. spool keeps the name unexported; this
// test names it the same way a real caller would, by the string the
// tool's Name() method returns.
const readSpooledOutputName = "read_spooled_output"

// largeOutputTool always returns a large string result, standing in
// for a log tail or a file read a caller does not want in full.
type largeOutputTool struct {
	toolName string
	result   string
}

// Name returns the registry name.
func (l largeOutputTool) Name() string { return l.toolName }

// Run returns l's fixed, oversized string result.
func (l largeOutputTool) Run(ctx context.Context, in tools.InOut) (tools.Out, error) {
	return tools.Out{Value: l.result}, nil
}

// TestSpoolToolTruncatesLargeStepResult wires SpoolTool around a
// large-output tool inside an agentrun step, then confirms the
// spooled view names a ref that a follow-up Spool.Load call resolves
// to the tool's full result.
func TestSpoolToolTruncatesLargeStepResult(t *testing.T) {
	ctx := context.Background()
	store, err := memory.New(1 << 20)
	if err != nil {
		t.Fatalf("memory.New: %v", err)
	}

	full := strings.Repeat("log-line\n", 500)
	inner := largeOutputTool{toolName: "tail", result: full}
	sp, err := spool.NewSpool(store, 1<<20)
	if err != nil {
		t.Fatalf("spool.NewSpool: %v", err)
	}
	wrapped, err := spool.SpoolTool("tail", 64, sp, inner)
	if err != nil {
		t.Fatalf("spool.SpoolTool: %v", err)
	}
	readBack, err := spool.ReadOutputTool(sp, 2048)
	if err != nil {
		t.Fatalf("spool.ReadOutputTool: %v", err)
	}

	reg, scope, runner, artifacts := buildSpoolRunner(t, wrapped, readBack)

	runCtx := spool.WithPrincipal(ctx, "tailer")
	status, _, err := runner.Run(runCtx, "thread-spool", machine.InOut{})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if status != "tailed" {
		t.Fatalf("status = %q, want tailed", status)
	}

	view, ok := artifacts.Get("tail")
	if !ok {
		t.Fatalf("artifacts.Get(tail) = %q,%v, want a recorded view", view, ok)
	}
	if len(view) >= len(full) {
		t.Fatalf("view len = %d, want shorter than the full result len %d", len(view), len(full))
	}

	const refMarker = "ref="
	idx := strings.Index(view, refMarker)
	if idx < 0 {
		t.Fatalf("view %q does not carry a ref marker", view)
	}
	ref := strings.TrimSuffix(view[idx+len(refMarker):], "]")

	got, err := sp.Load(runCtx, "tailer", ref)
	if err != nil || string(got) != full {
		t.Fatalf("sp.Load = %q,%v, want the full tool result", got, err)
	}

	// Resolve the same ref through reg.RunScoped, the same registry
	// and scope agentrun.New validated and wired for the chain above.
	// This proves ReadOutputTool is present, schema-typed, and
	// resolvable by name and scope in that live registry, not only
	// that sp.Load works when called directly.
	rebuilt := readBackFull(t, runCtx, reg, readBack, scope, ref)
	if rebuilt != full {
		t.Fatalf("reg.RunScoped(%s) reconstructed %d bytes, want the full %d-byte result",
			readSpooledOutputName, len(rebuilt), len(full))
	}
}

// buildSpoolRunner wires wrapped and readBack into one registry, an
// allowlist scope naming both, and an agentrun.Runner over a single
// tail step. It returns the same registry, scope, runner, and
// artifacts a real caller assembles once up front.
func buildSpoolRunner(t *testing.T, wrapped, readBack tools.Tool) (*tools.Registry, *tools.Scope, *agentrun.Runner, *agentrun.Artifacts) {
	t.Helper()
	plan, err := flow.New([]flow.Step{
		{ID: "tail", To: "tailed", Payload: "go"},
	}, nil)
	if err != nil {
		t.Fatalf("flow.New: %v", err)
	}
	m, err := machine.New("queued",
		machine.Transition{From: "queued", To: "tailed", Trigger: "t1"})
	if err != nil {
		t.Fatalf("machine.New: %v", err)
	}

	reg := tools.New()
	addTools(t, reg, wrapped, readBack)
	scope := tools.NewScope(tools.ScopeOptions{
		Allowlist: []string{"tail", readSpooledOutputName},
	})
	artifacts := &agentrun.Artifacts{}
	runner, err := agentrun.New(agentrun.Options{
		Agent: e2eAgent(t, "tailer", plan), Machine: m,
		Tools: reg, Scope: scope, Artifacts: artifacts,
	})
	if err != nil {
		t.Fatalf("agentrun.New: %v", err)
	}
	return reg, scope, runner, artifacts
}

// readBackFull pages ref back through reg.RunScoped at
// readSpooledOutputName, using scope on every call, and concatenates
// every page until no more marker remains. It fails the test on any
// error, a non-string page, or a malformed more marker.
func readBackFull(t *testing.T, ctx context.Context, reg *tools.Registry, readBack tools.Tool, scope *tools.Scope, ref string) string {
	t.Helper()
	schemaTool, ok := readBack.(tools.SchemaTool)
	if !ok {
		t.Fatalf("readBack does not implement tools.SchemaTool")
	}
	var rebuilt strings.Builder
	offset := 0
	for {
		payload := fmt.Sprintf(`{"ref":%q,"offset":%d}`, ref, offset)
		in, err := schemaTool.DecodeArguments([]byte(payload))
		if err != nil {
			t.Fatalf("DecodeArguments(%q): %v", payload, err)
		}
		out, err := reg.RunScoped(ctx, readSpooledOutputName, in, scope)
		if err != nil {
			t.Fatalf("reg.RunScoped(%s): %v", readSpooledOutputName, err)
		}
		page, ok := out.Value.(string)
		if !ok {
			t.Fatalf("reg.RunScoped(%s) out.Value = %T, want string", readSpooledOutputName, out.Value)
		}
		const moreMarker = "[more: offset="
		idx := strings.Index(page, moreMarker)
		if idx < 0 {
			rebuilt.WriteString(page)
			return rebuilt.String()
		}
		rebuilt.WriteString(page[:idx])
		var next int
		if _, err := fmt.Sscanf(page[idx:], spool.MoreMarker, &next); err != nil {
			t.Fatalf("parse more marker %q: %v", page[idx:], err)
		}
		offset = next
	}
}
