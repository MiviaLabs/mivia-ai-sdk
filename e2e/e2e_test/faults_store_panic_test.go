package e2e_test

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/MiviaLabs/mivia-ai-sdk/agentrun"
	"github.com/MiviaLabs/mivia-ai-sdk/e2e"
	"github.com/MiviaLabs/mivia-ai-sdk/flow"
	"github.com/MiviaLabs/mivia-ai-sdk/ledger"
	"github.com/MiviaLabs/mivia-ai-sdk/machine"
	"github.com/MiviaLabs/mivia-ai-sdk/tools"
)

// admitTool is a step tool that runs one ledger.Admit call. It panics
// whenever the wrapped Store panics, since ledger.Admit does not
// recover a Store panic itself.
type admitTool struct {
	l *ledger.Ledger
}

// Name returns the registry name.
func (a *admitTool) Name() string { return "admit" }

// Run calls Admit once.
func (a *admitTool) Run(ctx context.Context, in tools.InOut) (tools.Out, error) {
	if _, err := a.l.Admit(ctx, "e2e-actor", "cargo", 1, "cargo", time.Now()); err != nil {
		return tools.Out{}, err
	}
	return tools.Out{Value: "admitted"}, nil
}

// panicStore is a ledger.Store whose Load panics with a value
// wrapping e2e.ErrFault. It models a store whose seam crashes. The
// shipped e2e package cannot hold a panicking decorator: the semgrep
// gate forbids panic outside test files, so the seam lives here in
// the test package.
type panicStore struct{}

// Load panics with a value wrapping e2e.ErrFault.
func (panicStore) Load(ctx context.Context, key ledger.IdempotencyKey) (ledger.TaskState, bool, error) {
	panic(fmt.Errorf("e2e: ledger store fault: %w", e2e.ErrFault))
}

// CompareAndSwap passes through to a fresh MemStore; the panic
// scenario never reaches it.
func (panicStore) CompareAndSwap(ctx context.Context, key ledger.IdempotencyKey, old, new ledger.TaskState) (bool, error) {
	return ledger.NewMemStore().CompareAndSwap(ctx, key, old, new)
}

// Range passes through to a fresh MemStore; the panic scenario never
// reaches it.
func (panicStore) Range(ctx context.Context, fn func(ledger.TaskState) bool) error {
	return ledger.NewMemStore().Range(ctx, fn)
}

// TestFaultStorePanicPropagatesOutOfRun proves a ledger store that
// panics on its first call is not caught by flow's sequential path:
// the panic propagates out of Run uncaught, mirroring
// faults_panic_test.go's panicking Completer case for the store seam.
func TestFaultStorePanicPropagatesOutOfRun(t *testing.T) {
	ctx := context.Background()
	l, err := ledger.New(panicStore{}, nil)
	if err != nil {
		t.Fatalf("ledger.New: %v", err)
	}

	plan, err := flow.New([]flow.Step{{ID: "admit", To: "done", Payload: "go"}}, nil)
	if err != nil {
		t.Fatalf("flow.New: %v", err)
	}
	m, err := machine.New("queued",
		machine.Transition{From: "queued", To: "done", Trigger: "run"})
	if err != nil {
		t.Fatalf("machine.New: %v", err)
	}
	reg := tools.New()
	addTools(t, reg, &admitTool{l: l})
	runner, err := agentrun.New(agentrun.Options{
		Agent: e2eAgent(t, "fault-store-panic-agent", plan), Machine: m, Tools: reg,
	})
	if err != nil {
		t.Fatalf("agentrun.New: %v", err)
	}

	var recovered any
	func() {
		defer func() { recovered = recover() }()
		_, _, _ = runner.Run(ctx, "thread-store-panic", machine.InOut{})
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
