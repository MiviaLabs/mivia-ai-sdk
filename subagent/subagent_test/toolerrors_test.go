package subagent_test

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/MiviaLabs/mivia-ai-sdk/channel"
	"github.com/MiviaLabs/mivia-ai-sdk/flow"
	"github.com/MiviaLabs/mivia-ai-sdk/heartbeat"
	"github.com/MiviaLabs/mivia-ai-sdk/ledger"
	"github.com/MiviaLabs/mivia-ai-sdk/machine"
	"github.com/MiviaLabs/mivia-ai-sdk/memory"
	"github.com/MiviaLabs/mivia-ai-sdk/provider"
	"github.com/MiviaLabs/mivia-ai-sdk/room"
	"github.com/MiviaLabs/mivia-ai-sdk/scheduler"
	"github.com/MiviaLabs/mivia-ai-sdk/subagent"
	"github.com/MiviaLabs/mivia-ai-sdk/tools"
	"github.com/MiviaLabs/mivia-ai-sdk/trigger"
)

// errorCompleter fails every chat turn.
type errorCompleter struct{}

// Name labels the failing provider.
func (errorCompleter) Name() string { return "failing" }

// Chat returns the fixed failure.
func (errorCompleter) Chat(ctx context.Context, req provider.Request) (provider.Response, error) {
	return provider.Response{}, errors.New("provider down")
}

// ChatStream is never called by a non-streaming turn.
func (errorCompleter) ChatStream(ctx context.Context, req provider.Request) (<-chan provider.Chunk, error) {
	return nil, nil
}

// commandTools builds one JSON-command tool per row, over real
// blocks, for the shared decode and dispatch checks.
func commandTools(t *testing.T) []tools.Tool {
	t.Helper()
	r, err := room.New("ops", "founder")
	if err != nil {
		t.Fatalf("room.New: %v", err)
	}
	s := scheduler.New()
	m, err := heartbeat.New(time.Hour)
	if err != nil {
		t.Fatalf("heartbeat.New: %v", err)
	}
	l, err := ledger.New(nil, nil)
	if err != nil {
		t.Fatalf("ledger.New: %v", err)
	}
	store, err := memory.New(64)
	if err != nil {
		t.Fatalf("memory.New: %v", err)
	}
	return []tools.Tool{
		subagent.RoomTool("room", r, "founder"),
		subagent.SchedulerTool("sched", s, func(context.Context) error { return nil }),
		subagent.HeartbeatTool("beat", m),
		subagent.LedgerTool("ledger", l, "a", time.Minute),
		subagent.MemoryTool("memory", store),
		subagent.DiscoveryTool("cards"),
	}
}

// TestCommandToolsRejectGarbage proves every JSON-command tool maps a
// malformed payload and an unknown op onto ErrBadCommand.
func TestCommandToolsRejectGarbage(t *testing.T) {
	ctx := context.Background()
	for _, tool := range commandTools(t) {
		if _, err := tool.Run(ctx, inString("not json")); !errors.Is(err, subagent.ErrBadCommand) {
			t.Errorf("%s: garbage err = %v, want ErrBadCommand", tool.Name(), err)
		}
		if _, err := tool.Run(ctx, inString(`{"op":"nonsense"}`)); !errors.Is(err, subagent.ErrBadCommand) {
			t.Errorf("%s: unknown op err = %v, want ErrBadCommand", tool.Name(), err)
		}
	}
}

// TestProviderToolFailurePropagates proves a failing Completer fails
// the tool call with its own error.
func TestProviderToolFailurePropagates(t *testing.T) {
	_, err := subagent.ProviderTool("model", errorCompleter{}).
		Run(context.Background(), inString("prompt"))
	if err == nil || !strings.Contains(err.Error(), "provider down") {
		t.Fatalf("err = %v, want the provider failure", err)
	}
}

// TestFlowToolFailurePropagates proves a walk that faults surfaces
// the runner's error.
func TestFlowToolFailurePropagates(t *testing.T) {
	plan, err := flow.New([]flow.Step{{ID: "only", To: "done", Payload: "p"}}, nil)
	if err != nil {
		t.Fatalf("flow.New: %v", err)
	}
	m, err := machine.New("queued",
		machine.Transition{From: "queued", To: "elsewhere", Trigger: "t"})
	if err != nil {
		t.Fatalf("machine.New: %v", err)
	}
	_, err = subagent.FlowTool("flow", plan, m, nil).Run(context.Background(), inString("p"))
	if err == nil || !strings.Contains(err.Error(), "done") {
		t.Fatalf("err = %v, want the missing-row fault", err)
	}
}

// TestTriggerToolUnknownNameFails proves firing an unregistered
// trigger surfaces the registry's error.
func TestTriggerToolUnknownNameFails(t *testing.T) {
	_, err := subagent.TriggerTool("triggers", trigger.New()).
		Run(context.Background(), inString("ghost"))
	if err == nil {
		t.Fatal("Run succeeded, want an unknown-trigger failure")
	}
}

// TestSchedulerToolDuplicateFails proves scheduling one id twice
// surfaces the scheduler's error.
func TestSchedulerToolDuplicateFails(t *testing.T) {
	ctx := context.Background()
	s := scheduler.New()
	tool := subagent.SchedulerTool("sched", s, func(context.Context) error { return nil })
	cmd, _ := json.Marshal(subagent.SchedulerCommand{
		Op: subagent.OpEvery, ID: "dup", EveryMs: 3_600_000,
	})
	if _, err := tool.Run(ctx, inString(string(cmd))); err != nil {
		t.Fatalf("first add: %v", err)
	}
	if _, err := tool.Run(ctx, inString(string(cmd))); err == nil {
		t.Fatal("second add succeeded, want the duplicate failure")
	}
}

// TestMemoryToolErrors proves an over-budget put and an unknown ref
// surface the store's own errors.
func TestMemoryToolErrors(t *testing.T) {
	ctx := context.Background()
	store, err := memory.New(4)
	if err != nil {
		t.Fatalf("memory.New: %v", err)
	}
	tool := subagent.MemoryTool("memory", store)
	put, _ := json.Marshal(subagent.MemoryCommand{Op: subagent.OpPut, Data: "far too long"})
	if _, err := tool.Run(ctx, inString(string(put))); !errors.Is(err, memory.ErrBudgetExceeded) {
		t.Fatalf("oversize put err = %v, want ErrBudgetExceeded", err)
	}
	get, _ := json.Marshal(subagent.MemoryCommand{Op: subagent.OpGet, Ref: "sha256:missing"})
	if _, err := tool.Run(ctx, inString(string(get))); !errors.Is(err, memory.ErrUnknownRef) {
		t.Fatalf("unknown ref err = %v, want ErrUnknownRef", err)
	}
}

// TestLedgerToolStateAbsent proves a state query on a key with no
// record reports absent.
func TestLedgerToolStateAbsent(t *testing.T) {
	l, err := ledger.New(nil, nil)
	if err != nil {
		t.Fatalf("ledger.New: %v", err)
	}
	cmd, _ := json.Marshal(subagent.LedgerCommand{Op: subagent.OpState, Key: "ghost"})
	out, err := subagent.LedgerTool("ledger", l, "a", time.Minute).
		Run(context.Background(), inString(string(cmd)))
	if err != nil || out.Value != "absent" {
		t.Fatalf("state ghost = %v,%v, want absent", out.Value, err)
	}
}

// TestDiscoveryToolBadCardFails proves an unparseable card surfaces
// the discovery error.
func TestDiscoveryToolBadCardFails(t *testing.T) {
	cmd, _ := json.Marshal(subagent.DiscoveryCommand{
		Op: subagent.OpMatch, Card: "{not a card", Need: "x",
	})
	_, err := subagent.DiscoveryTool("cards").
		Run(context.Background(), inString(string(cmd)))
	if err == nil {
		t.Fatal("Run succeeded, want a card parse failure")
	}
}

// TestChannelToolAskErrorAndEmptyPayload proves a Notifier fault
// propagates and an approved empty answer reads approved.
func TestChannelToolAskErrorAndEmptyPayload(t *testing.T) {
	ctx := context.Background()
	failing := func(context.Context, channel.Question) (channel.Answer, error) {
		return channel.Answer{}, errors.New("transport down")
	}
	if _, err := subagent.ChannelTool("ask", failing, "h").Run(ctx, inString("q")); err == nil {
		t.Fatal("Run succeeded, want the transport failure")
	}
	out, err := subagent.ChannelTool("ask", answeringAsk(""), "h").Run(ctx, inString("q"))
	if err != nil || out.Value != "approved" {
		t.Fatalf("empty answer = %v,%v, want approved", out.Value, err)
	}
}
