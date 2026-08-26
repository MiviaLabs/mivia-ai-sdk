package e2e_test

import (
	"context"
	"errors"
	"fmt"
	"testing"

	"github.com/MiviaLabs/mivia-ai-sdk/e2e"
	"github.com/MiviaLabs/mivia-ai-sdk/machine"
	"github.com/MiviaLabs/mivia-ai-sdk/provider"
	"github.com/MiviaLabs/mivia-ai-sdk/subagent"
)

// panicCompleter is a provider.Completer whose Chat and ChatStream
// panic with a value wrapping e2e.ErrFault on every call. It models a
// provider whose seam crashes. The shipped e2e package cannot hold a
// panicking decorator: the semgrep gate forbids panic outside test
// files, so the seam lives here in the test package.
type panicCompleter struct{}

// Name returns a fixed label.
func (panicCompleter) Name() string { return "panic-completer" }

// Chat panics with a value wrapping e2e.ErrFault.
func (panicCompleter) Chat(ctx context.Context, req provider.Request) (provider.Response, error) {
	panic(fmt.Errorf("e2e: provider completer fault: %w", e2e.ErrFault))
}

// ChatStream panics with a value wrapping e2e.ErrFault.
func (panicCompleter) ChatStream(ctx context.Context, req provider.Request) (<-chan provider.Chunk, error) {
	panic(fmt.Errorf("e2e: provider completer fault: %w", e2e.ErrFault))
}

// TestFaultCompleterPanicFailsClosed proves a provider that panics on
// its first call fails the run closed through the registry's bounded
// dispatch: Run returns an error keeping the fault's identity, never
// a crash. The registered-tool seam changed with the run timeout
// backstop; see docs/packages/tools.md's "Run timeout backstop"
// section. The stream half below still pins the unregistered path,
// where a direct call panics its own caller. See docs/plans/e2e.md's
// "Disclosed limits" section for the goroutine-wave case this
// scenario deliberately does not cover.
func TestFaultCompleterPanicFailsClosed(t *testing.T) {
	ctx := context.Background()
	runner := faultMemberRunner(t, "panic-agent",
		subagent.ProviderTool("chat", panicCompleter{}))

	_, _, err := runner.Run(ctx, "thread-panic", machine.InOut{})
	if err == nil {
		t.Fatal("Run error = nil, want the completer fault surfaced fail-closed")
	}
	if !errors.Is(err, e2e.ErrFault) {
		t.Fatalf("error = %v, want e2e.ErrFault", err)
	}
}

// TestFaultCompleterStreamPanicPropagates covers the ChatStream half
// of the panic contract the Chat scenario does not reach: the
// panicking streamed call panics with the same fault error.
func TestFaultCompleterStreamPanicPropagates(t *testing.T) {
	ctx := context.Background()
	fc := panicCompleter{}

	var recovered any
	func() {
		defer func() { recovered = recover() }()
		_, _ = fc.ChatStream(ctx, provider.Request{})
	}()

	err, ok := recovered.(error)
	if !ok {
		t.Fatalf("recovered value = %#v, want an error", recovered)
	}
	if !errors.Is(err, e2e.ErrFault) {
		t.Fatalf("recovered error = %v, want e2e.ErrFault", err)
	}
}
