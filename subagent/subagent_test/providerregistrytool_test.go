package subagent_test

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/MiviaLabs/mivia-ai-sdk/flow"
	"github.com/MiviaLabs/mivia-ai-sdk/machine"
	"github.com/MiviaLabs/mivia-ai-sdk/provider"
	"github.com/MiviaLabs/mivia-ai-sdk/providerregistry"
	"github.com/MiviaLabs/mivia-ai-sdk/subagent"
	"github.com/MiviaLabs/mivia-ai-sdk/tools"
	"github.com/MiviaLabs/mivia-ai-sdk/trace"
	"github.com/MiviaLabs/mivia-ai-sdk/usage"
)

// cannedCompleter answers one fixed reply and can be told to fail.
type cannedCompleter struct {
	name  string
	reply string
	fail  bool
	calls *int
}

// Name returns the stub's name.
func (c *cannedCompleter) Name() string { return c.name }

// Chat answers the canned reply or fails.
func (c *cannedCompleter) Chat(ctx context.Context, req provider.Request) (provider.Response, error) {
	if c.calls != nil {
		*c.calls++
	}
	if c.fail {
		return provider.Response{}, errors.New("canned: provider down")
	}
	return provider.Response{
		Message: provider.Message{Role: provider.RoleAssistant, Content: c.reply},
		Usage:   provider.Usage{PromptTokens: 7, CompletionTokens: 3, TotalTokens: 10},
	}, nil
}

// ChatStream is unused by these tests.
func (c *cannedCompleter) ChatStream(ctx context.Context, req provider.Request) (<-chan provider.Chunk, error) {
	return nil, errors.New("canned: streaming unused")
}

// anyError approves every failure for fallback, the permissive
// predicate these tests need.
func anyError(error) bool { return true }

// TestProviderRegistryToolFallsThrough proves the tool routes over
// the caller's order: the first provider fails, the second answers,
// and the reply content is the answering provider's.
func TestProviderRegistryToolFallsThrough(t *testing.T) {
	reg := providerregistry.New()
	primaryCalls := 0
	if err := reg.Register("primary", &cannedCompleter{
		name: "primary", reply: "from primary", fail: true, calls: &primaryCalls,
	}); err != nil {
		t.Fatalf("Register(primary): %v", err)
	}
	if err := reg.Register("backup", &cannedCompleter{name: "backup", reply: "from backup"}); err != nil {
		t.Fatalf("Register(backup): %v", err)
	}
	tool := subagent.ProviderRegistryTool("model", reg, []string{"primary", "backup"}, anyError)
	out, err := tool.Run(context.Background(), tools.InOut{Value: "summarize"})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if out.Value != "from backup" {
		t.Fatalf("reply = %v, want the backup's", out.Value)
	}
	if primaryCalls != 1 {
		t.Fatalf("primary calls = %d, want 1", primaryCalls)
	}
}

// TestProviderRegistryToolAllFailed proves an exhausted order fails
// the tool with the registry's ErrAllFailed.
func TestProviderRegistryToolAllFailed(t *testing.T) {
	reg := providerregistry.New()
	for _, name := range []string{"a", "b"} {
		if err := reg.Register(name, &cannedCompleter{name: name, fail: true}); err != nil {
			t.Fatalf("Register(%s): %v", name, err)
		}
	}
	tool := subagent.ProviderRegistryTool("model", reg, []string{"a", "b"}, anyError)
	_, err := tool.Run(context.Background(), tools.InOut{Value: "summarize"})
	if !errors.Is(err, providerregistry.ErrAllFailed) {
		t.Fatalf("Run error = %v, want ErrAllFailed", err)
	}
}

// TestProviderRegistryToolRecordsUsage proves the seam composes with
// usage.WrapCompleter: one failed and one answered turn still sum
// only the answering turn's usage under the session.
func TestProviderRegistryToolRecordsUsage(t *testing.T) {
	acc := usage.New()
	primaryCalls := 0
	wrapped, err := usage.WrapCompleter("session-1", acc, &cannedCompleter{
		name: "primary", reply: "x", fail: true, calls: &primaryCalls,
	})
	if err != nil {
		t.Fatalf("WrapCompleter: %v", err)
	}
	reg := providerregistry.New()
	if err := reg.Register("primary", wrapped); err != nil {
		t.Fatalf("Register(primary): %v", err)
	}
	if err := reg.Register("backup", &cannedCompleter{name: "backup", reply: "from backup"}); err != nil {
		t.Fatalf("Register(backup): %v", err)
	}
	tool := subagent.ProviderRegistryTool("model", reg, []string{"primary", "backup"}, anyError)
	out, err := tool.Run(context.Background(), tools.InOut{Value: "go"})
	if err != nil || out.Value != "from backup" {
		t.Fatalf("Run = %v, %v; want the backup's reply", out.Value, err)
	}
	// A failed wrapped turn records nothing: the session holds no
	// counts even though the wrapped primary ran.
	if _, ok := acc.Total("session-1"); ok {
		t.Fatal("Total(session-1): want no counts from a failed turn")
	}
}

// TestAsToolTracesSpawn proves a ToolOptions Tracer opens one span
// per spawn and the spawn span nests under the span already sitting
// in the caller's ctx.
func TestAsToolTracesSpawn(t *testing.T) {
	plan, err := flow.New([]flow.Step{{ID: "work", To: "done", Payload: "go"}}, nil)
	if err != nil {
		t.Fatalf("flow.New: %v", err)
	}
	m, err := machine.New("queued",
		machine.Transition{From: "queued", To: "done", Trigger: "run"})
	if err != nil {
		t.Fatalf("machine.New: %v", err)
	}
	calls := 0
	reg := tools.New()
	if err := reg.Add(spawnCounterTool{calls: &calls}); err != nil {
		t.Fatalf("registry.Add: %v", err)
	}
	runner := runnerOver(t, plan, m, reg)
	tr := trace.New()
	ctx, parent := tr.Start(context.Background(), "caller.step")
	parent.End()
	tool := subagent.AsTool("helper", runner, subagent.ToolOptions{Tracer: tr})
	out, err := tool.Run(ctx, tools.InOut{Value: "go"})
	if err != nil || out.Value != "done" {
		t.Fatalf("Run = %v, %v; want done", out.Value, err)
	}
	spans := tr.Spans()
	if len(spans) != 2 || spans[1].Name != "subagent.spawn" {
		t.Fatalf("spans = %+v, want the caller span then one spawn span", spans)
	}
	if spans[1].ParentID != parent.ID {
		t.Fatalf("spawn parent = %d, want the caller span %d", spans[1].ParentID, parent.ID)
	}
	if attr, ok := spans[1].Attributes()["thread"]; !ok || !strings.HasPrefix(attr, "helper-") {
		t.Fatalf("spawn thread attribute = %q,%v", attr, ok)
	}
	if spans[1].EndTime().IsZero() {
		t.Fatal("spawn span must end")
	}
}

// spawnCounterTool counts the spawned run's tool calls.
type spawnCounterTool struct {
	calls *int
}

// Name returns the registry name.
func (spawnCounterTool) Name() string { return "work" }

// Run counts the call.
func (s spawnCounterTool) Run(ctx context.Context, in tools.InOut) (tools.Out, error) {
	*s.calls++
	return tools.Out{Value: "ok"}, nil
}
