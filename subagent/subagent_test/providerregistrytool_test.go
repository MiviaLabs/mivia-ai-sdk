package subagent_test

import (
	"context"
	"errors"
	"testing"

	"github.com/MiviaLabs/mivia-ai-sdk/provider"
	"github.com/MiviaLabs/mivia-ai-sdk/providerregistry"
	"github.com/MiviaLabs/mivia-ai-sdk/subagent"
	"github.com/MiviaLabs/mivia-ai-sdk/tools"
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
	// The failed primary turn recorded nothing; only the unwrapped
	// backup answered, so this session holds no counts yet. A wrapped
	// backup would hold its own session's counts instead.
	if _, ok := acc.Total("session-1"); ok {
		t.Fatal("Total(session-1): want no counts from a failed turn")
	}
}
