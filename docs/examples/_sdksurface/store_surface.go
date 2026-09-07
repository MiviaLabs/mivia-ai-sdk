package main

import (
	"context"
	"fmt"
	"time"

	"github.com/MiviaLabs/mivia-ai-sdk/memory"
	"github.com/MiviaLabs/mivia-ai-sdk/provider"
	"github.com/MiviaLabs/mivia-ai-sdk/room"
	"github.com/MiviaLabs/mivia-ai-sdk/tools"
	"github.com/MiviaLabs/mivia-ai-sdk/workspace"
)

// stubTool is a minimal tools.Tool with a fixed string result.
type stubTool struct{ name string }

// Name returns the tool's registry name.
func (t stubTool) Name() string { return t.name }

// Run returns the input's string form echoed back as the output.
func (t stubTool) Run(_ context.Context, in tools.InOut) (tools.Out, error) {
	return tools.Out{Value: in.Value}, nil
}

// stubCompleter is a minimal provider.Completer with one canned reply.
type stubCompleter struct{}

// Name returns the completer's label.
func (stubCompleter) Name() string { return "stub" }

// Chat returns one assistant reply and records nothing.
func (stubCompleter) Chat(_ context.Context, _ provider.Request) (provider.Response, error) {
	return provider.Response{Message: provider.Message{
		Role: provider.RoleAssistant, Content: "ok",
	}}, nil
}

// ChatStream returns one closed stream; the surface never reads it.
func (stubCompleter) ChatStream(_ context.Context, _ provider.Request) (<-chan provider.Chunk, error) {
	ch := make(chan provider.Chunk)
	close(ch)
	return ch, nil
}

// memorySurface writes into a principal-scoped spool, reads its grant
// expiry, expires the entry, and builds the spool tool pair.
func memorySurface() error {
	store, err := memory.New(1 << 20)
	if err != nil {
		return fmt.Errorf("memory.New: %w", err)
	}
	sp, err := memory.NewSpool(store, 1<<20)
	if err != nil {
		return fmt.Errorf("NewSpool: %w", err)
	}
	ctx := memory.WithPrincipal(context.Background(), "alice")
	_, ref, err := sp.SpoolExpiring(ctx, "alice", []byte("note"), time.Hour)
	if err != nil {
		return fmt.Errorf("SpoolExpiring: %w", err)
	}
	if _, ok := sp.GrantExpiry(ref); !ok {
		return fmt.Errorf("GrantExpiry lost the grant for %q", ref)
	}
	if err := sp.Expire(ref); err != nil {
		return fmt.Errorf("Expire: %w", err)
	}
	inner := stubTool{name: "inner"}
	if _, err := memory.SpoolTool("spool", 1<<20, sp, inner); err != nil {
		return fmt.Errorf("SpoolTool: %w", err)
	}
	if _, err := memory.ReadOutputTool(sp, 1<<10); err != nil {
		return fmt.Errorf("ReadOutputTool: %w", err)
	}
	return nil
}

// providerSurface registers a completer and wraps it with the
// per-session usage accumulator under the implicit cache style.
func providerSurface() error {
	reg := provider.NewRegistry()
	if err := reg.Register("stub", stubCompleter{}); err != nil {
		return fmt.Errorf("Register: %w", err)
	}
	acc := provider.NewAccumulator()
	wrapped, err := provider.WrapCompleter("session-1", acc, stubCompleter{})
	if err != nil {
		return fmt.Errorf("WrapCompleter: %w", err)
	}
	req := provider.Request{
		Messages:   []provider.Message{{Role: provider.RoleUser, Content: "hi"}},
		CacheStyle: provider.CacheStyleImplicit,
	}
	if _, err := wrapped.Chat(context.Background(), req); err != nil {
		return fmt.Errorf("wrapped Chat: %w", err)
	}
	return nil
}

// roomSurface admits a peer then walks it back out, the full lifecycle
// a standing group's moderator drives.
func roomSurface() error {
	r, err := room.New("room-1", "founder")
	if err != nil {
		return fmt.Errorf("room.New: %w", err)
	}
	if err := r.Admit("peer", "founder"); err != nil {
		return fmt.Errorf("Admit: %w", err)
	}
	if err := r.Leave("peer"); err != nil {
		return fmt.Errorf("Leave: %w", err)
	}
	if r.IsMember("peer") {
		return fmt.Errorf("peer is still a member after Leave")
	}
	return nil
}

// workspaceSurface compiles the deny-pattern matcher a confined
// filesystem builds at startup.
func workspaceSurface() error {
	m, err := workspace.NewMatcher([]string{"**/.env"})
	if err != nil {
		return fmt.Errorf("NewMatcher: %w", err)
	}
	if m == nil {
		return fmt.Errorf("NewMatcher returned nil")
	}
	return nil
}
