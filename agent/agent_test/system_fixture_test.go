// Package agent_test also holds the shared fixture the system
// integration suite reuses: one bus that subscribes every emitted
// name, a write-class review tool, a channel.Notifier-shaped approval
// closure, a canned provider.Completer, and the ledger helpers. The
// suite proves every shipped package composes as one system. See
// docs/plans/agents/phase46_system_integration_suite.md.
package agent_test

import (
	"context"
	"fmt"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/MiviaLabs/mivia-ai-sdk/agent"
	"github.com/MiviaLabs/mivia-ai-sdk/channel"
	"github.com/MiviaLabs/mivia-ai-sdk/events"
	"github.com/MiviaLabs/mivia-ai-sdk/flow"
	"github.com/MiviaLabs/mivia-ai-sdk/ledger"
	"github.com/MiviaLabs/mivia-ai-sdk/machine"
	"github.com/MiviaLabs/mivia-ai-sdk/provider"
	"github.com/MiviaLabs/mivia-ai-sdk/scheduler"
	"github.com/MiviaLabs/mivia-ai-sdk/tools"
)

// reviewToolName is the registration name of the suite's write-class
// tool. A declined approval must leave its call counter unchanged.
const reviewToolName = "review"

// declineMarker marks a tool input the approval notifier refuses. The
// suite uses it to drive both branches of RunScoped's approval gate
// inside one composed run.
const declineMarker = "recover"

// systemEventNames lists every events.Name the system suite emits.
// events.Bus.Emit fails on a name with no subscriber, and agent.Run
// propagates that failure, so a fixture bus must cover all of them.
func systemEventNames() []events.Name {
	return []events.Name{
		agent.MessageDeliveredEvent,
		agent.MessageAckedEvent,
		agent.ThreadVerifiedEvent,
		flow.StepCompletedEvent,
		ledger.AdmittedEvent,
		ledger.ClaimedEvent,
		ledger.CompletedEvent,
		ledger.RenewedEvent,
		ledger.ReleasedEvent,
		ledger.BlockedEvent,
		ledger.TakenOverEvent,
		scheduler.JobFailedEvent,
		machine.MoveEvent,
	}
}

// newSystemBus builds a bus that subscribes every name in
// systemEventNames with a no-op handler. A caller adds its own
// recorder on top through lifecycleRecorder.subscribe.
func newSystemBus(t testing.TB) *events.Bus {
	t.Helper()
	bus := events.New()
	noop := func(ctx context.Context, e events.Event) error { return nil }
	for _, name := range systemEventNames() {
		if err := bus.Subscribe(name, noop); err != nil {
			t.Fatalf("Subscribe(%q) unexpected error: %v", name, err)
		}
	}
	return bus
}

// reviewTool is the suite's write-class tool. It uppercases its input
// and counts every Run call, so a declined approval proves the gate
// blocked the call before the registry reached the tool. It
// implements tools.ProfiledTool so its ExecutionClass is write, at or
// above the scope's approval threshold.
type reviewTool struct {
	calls atomic.Int64
}

// Name identifies this tool as reviewToolName in a tools.Registry.
func (r *reviewTool) Name() string { return reviewToolName }

// ExecutionProfile reports the write class, so a scope with a write
// approval threshold gates every call.
func (r *reviewTool) ExecutionProfile() tools.ExecutionProfile {
	return tools.ExecutionProfile{Class: tools.ExecutionClassWrite, ResourceKey: reviewToolName}
}

// Run counts the call and returns the uppercased input string.
func (r *reviewTool) Run(ctx context.Context, in tools.InOut) (tools.Out, error) {
	r.calls.Add(1)
	s, ok := in.Value.(string)
	if !ok {
		return tools.Out{}, fmt.Errorf("review: value is %T, want string", in.Value)
	}
	return tools.Out{Value: strings.ToUpper(s)}, nil
}

// approvalNotifier is a test-local closure carrying channel.Notifier's
// exact type. It approves every question whose payload lacks
// declineMarker. It counts every call. Phase 47 replaces it with the
// shipped NewNDJSONNotifier over a real pipe.
func approvalNotifier(calls *atomic.Int64) channel.Notifier {
	return func(ctx context.Context, q channel.Question) (channel.Answer, error) {
		calls.Add(1)
		if err := q.Validate(); err != nil {
			return channel.Answer{}, err
		}
		return channel.Answer{
			QuestionID: q.ID,
			Approved:   !strings.Contains(q.Payload, declineMarker),
			Payload:    "reviewed by the fixture peer",
		}, nil
	}
}

// approveFromNotifier adapts a channel.Notifier to
// tools.ScopeOptions.Approve's exact signature. It compiles only if
// the two shapes really compose, which is the point of the adapter.
func approveFromNotifier(n channel.Notifier) func(context.Context, tools.ToolCall) (bool, error) {
	return func(ctx context.Context, call tools.ToolCall) (bool, error) {
		payload, ok := call.In.Value.(string)
		if !ok {
			return false, fmt.Errorf("approve: value is %T, want string", call.In.Value)
		}
		answer, err := n(ctx, channel.Question{
			ID:        "approve-" + call.Name,
			Recipient: "operator",
			Payload:   payload,
		})
		if err != nil {
			return false, err
		}
		return answer.Approved, nil
	}
}

// newReviewScope builds the tools.Scope that gates reviewToolName
// behind n, with a write approval threshold.
func newReviewScope(n channel.Notifier) *tools.Scope {
	return tools.NewScope(tools.ScopeOptions{
		Allowlist:         []string{reviewToolName},
		Approve:           approveFromNotifier(n),
		ApprovalThreshold: tools.ExecutionClassWrite,
	})
}

// cannedCompleter is the suite's provider.Completer test double. It
// returns one fixed assistant Response, so provider.RunTurn can seed a
// step payload at plan-construction time.
type cannedCompleter struct {
	reply string
}

// Name identifies this completer in a provider.Request trace.
func (c cannedCompleter) Name() string { return "canned" }

// Chat returns the fixed reply as an assistant message.
func (c cannedCompleter) Chat(ctx context.Context, req provider.Request) (provider.Response, error) {
	return provider.Response{
		Model:        req.Model,
		Message:      provider.Message{Role: provider.RoleAssistant, Content: c.reply},
		FinishReason: "stop",
	}, nil
}

// ChatStream emits the fixed reply as one delta, then a done chunk.
func (c cannedCompleter) ChatStream(ctx context.Context, req provider.Request) (<-chan provider.Chunk, error) {
	ch := make(chan provider.Chunk, 2)
	ch <- provider.Chunk{Delta: c.reply}
	ch <- provider.Chunk{Done: true, FinishReason: "stop"}
	close(ch)
	return ch, nil
}

// completerReply runs one provider turn and returns the assistant
// message content. The suite seeds a step payload with it.
func completerReply(t testing.TB, reply string) string {
	t.Helper()
	resp, err := provider.RunTurn(context.Background(), cannedCompleter{reply: reply}, provider.Request{
		Model:    "fixture-model",
		Messages: []provider.Message{{Role: provider.RoleUser, Content: "summarize the request"}},
	})
	if err != nil {
		t.Fatalf("provider.RunTurn() unexpected error: %v", err)
	}
	return resp.Message.Content
}

// newSystemLedger builds a Ledger over a fresh MemStore on bus. A
// durable Store backend is out of this suite's scope; MemStore keeps
// the composition in process.
func newSystemLedger(t testing.TB, bus *events.Bus) *ledger.Ledger {
	t.Helper()
	l, err := ledger.New(ledger.NewMemStore(), bus)
	if err != nil {
		t.Fatalf("ledger.New() unexpected error: %v", err)
	}
	return l
}

// admitAndClaim admits key, then claims it for owner, and returns the
// fence token Complete must present later.
func admitAndClaim(
	t testing.TB, l *ledger.Ledger, actor ledger.Actor,
	key ledger.IdempotencyKey, owner ledger.OwnerID, now time.Time,
) ledger.FenceToken {
	t.Helper()
	admitted, err := l.Admit(context.Background(), actor, key, 1, "system suite task", now)
	if err != nil {
		t.Fatalf("ledger.Admit(%q) unexpected error: %v", key, err)
	}
	if !admitted {
		t.Fatalf("ledger.Admit(%q) = false, want true for a fresh key", key)
	}
	fence, err := l.Claim(context.Background(), actor, key, owner, time.Minute, now)
	if err != nil {
		t.Fatalf("ledger.Claim(%q) unexpected error: %v", key, err)
	}
	return fence
}

// ledgerStatus reads key's recorded status. It fails the test when the
// key is absent.
func ledgerStatus(t testing.TB, l *ledger.Ledger, key ledger.IdempotencyKey) machine.Status {
	t.Helper()
	state, ok, err := l.State(context.Background(), key)
	if err != nil {
		t.Fatalf("ledger.State(%q) unexpected error: %v", key, err)
	}
	if !ok {
		t.Fatalf("ledger.State(%q) reported no such key", key)
	}
	return state.Status
}
