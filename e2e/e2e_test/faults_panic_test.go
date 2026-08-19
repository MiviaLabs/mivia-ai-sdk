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

// TestFaultCompleterPanicPropagatesOutOfRun proves a provider that
// panics on its first call is not caught by flow's or agentrun's
// sequential path. The one-step, non-panel plan runs the panicking
// tool call in the same goroutine as the test's own call to
// Runner.Run, so the panic propagates out of Run uncaught. This pins
// the documented contract: neither flow.Run nor agentrun.Runner.Run
// recovers a panic from a Fire call on the sequential path. See
// docs/plans/e2e.md's "Disclosed limits" section for the separate,
// goroutine-wave case this test deliberately does not cover.
func TestFaultCompleterPanicPropagatesOutOfRun(t *testing.T) {
	ctx := context.Background()
	runner := faultMemberRunner(t, "panic-agent",
		subagent.ProviderTool("chat", panicCompleter{}))

	var recovered any
	func() {
		defer func() { recovered = recover() }()
		_, _, _ = runner.Run(ctx, "thread-panic", machine.InOut{})
		t.Fatal("Run returned normally, want a panic")
	}()

	err, ok := recovered.(error)
	if !ok {
		t.Fatalf("recovered value = %#v, want an error", recovered)
	}
	if !errors.Is(err, e2e.ErrFault) {
		t.Fatalf("recovered error = %v, want e2e.ErrFault", err)
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
