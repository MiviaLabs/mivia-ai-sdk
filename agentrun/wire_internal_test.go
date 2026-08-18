package agentrun

import (
	"context"
	"testing"

	"github.com/MiviaLabs/mivia-ai-sdk/agent"
	"github.com/MiviaLabs/mivia-ai-sdk/discovery"
	"github.com/MiviaLabs/mivia-ai-sdk/events"
	"github.com/MiviaLabs/mivia-ai-sdk/flow"
	"github.com/MiviaLabs/mivia-ai-sdk/identity"
	"github.com/MiviaLabs/mivia-ai-sdk/machine"
	"github.com/MiviaLabs/mivia-ai-sdk/tools"
)

// TestRunnerReceiverOverride proves the runner stores the receiver
// override's signer, not the agent's own signer. The public surface
// does not expose Runner.receiver, so this internal test asserts it
// directly via New's wiring.
func TestRunnerReceiverOverride(t *testing.T) {
	id, err := identity.New()
	if err != nil {
		t.Fatal(err)
	}
	card := discovery.Card{Name: "test", Capabilities: []string{"cap"}}
	plan, err := flow.New([]flow.Step{{ID: "t1", To: "resolved"}}, nil)
	if err != nil {
		t.Fatal(err)
	}
	a, err := agent.New(id, card, plan)
	if err != nil {
		t.Fatal(err)
	}
	m, err := machine.New("queued", machine.Transition{From: "queued", To: "resolved", Trigger: "run"})
	if err != nil {
		t.Fatal(err)
	}
	reg := tools.New()
	if err := reg.Add(tl{name: "t1"}); err != nil {
		t.Fatal(err)
	}

	override, err := identity.New()
	if err != nil {
		t.Fatal(err)
	}
	opts := Options{
		Agent:    a,
		Machine:  m,
		Tools:    reg,
		Receiver: override,
		Bus:      events.New(),
	}
	r, err := New(opts)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if r.receiver == "" {
		t.Fatal("runner.receiver is empty")
	}
	agentSigner := id.Signer()
	overrideSigner := override.Signer()
	if r.receiver == agentSigner {
		t.Fatal("receiver still equals agent signer after override")
	}
	if r.receiver != overrideSigner {
		t.Fatalf("receiver = %q, want override signer %q", r.receiver, overrideSigner)
	}
}

// tl is a minimal tool for the internal test.
type tl struct{ name string }

func (t tl) Name() string                                               { return t.name }
func (t tl) Run(ctx context.Context, in tools.InOut) (tools.Out, error) { return tools.Out{}, nil }
