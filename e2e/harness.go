// Harness shared by the e2e scenarios.

package e2e

import (
	"context"
	"fmt"
	"sync"

	"github.com/MiviaLabs/mivia-ai-sdk/agent"
	"github.com/MiviaLabs/mivia-ai-sdk/discovery"
	"github.com/MiviaLabs/mivia-ai-sdk/envelope"
	"github.com/MiviaLabs/mivia-ai-sdk/events"
	"github.com/MiviaLabs/mivia-ai-sdk/flow"
	"github.com/MiviaLabs/mivia-ai-sdk/identity"
	"github.com/MiviaLabs/mivia-ai-sdk/tools"
)

// NewAgent builds an Agent over plan under a fresh identity and a
// one-capability card, failing only when key generation fails.
func NewAgent(name string, plan *flow.Definition) (*agent.Agent, error) {
	id, err := identity.New()
	if err != nil {
		return nil, fmt.Errorf("e2e: identity for %q: %w", name, err)
	}
	card := discovery.Card{Name: name, Capabilities: []string{"e2e"}}
	return agent.New(id, card, plan)
}

// PrefixTool returns its prefix joined to the string payload it
// receives, so each step records a distinct, deterministic result.
type PrefixTool struct {
	ToolName string
	Prefix   string
}

// Name returns the registry name.
func (t PrefixTool) Name() string { return t.ToolName }

// Run returns the prefix joined to the input payload string.
func (t PrefixTool) Run(ctx context.Context, in tools.InOut) (tools.Out, error) {
	s, _ := in.Value.(string)
	return tools.Out{Value: t.Prefix + s}, nil
}

// EscalateTool fails every run with an error wrapping
// agent.ErrEscalated, so a wired Ask round trip can resolve it.
type EscalateTool struct {
	ToolName string
}

// Name returns the registry name.
func (t EscalateTool) Name() string { return t.ToolName }

// Run reports that the step needs a human decision.
func (t EscalateTool) Run(ctx context.Context, in tools.InOut) (tools.Out, error) {
	return tools.Out{}, fmt.Errorf("e2e: %w: step needs a human", agent.ErrEscalated)
}

// Recorder counts every event it observes, in arrival order. It is
// safe for concurrent use.
type Recorder struct {
	mu    sync.Mutex
	names []events.Name
}

// NewRecorder returns an empty Recorder.
func NewRecorder() *Recorder { return &Recorder{} }

// Handler returns the Handler to subscribe on a bus.
func (r *Recorder) Handler() events.Handler {
	return func(ctx context.Context, e events.Event) error {
		r.mu.Lock()
		defer r.mu.Unlock()
		r.names = append(r.names, e.Name)
		return nil
	}
}

// Names returns every observed event name, in arrival order.
func (r *Recorder) Names() []events.Name {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]events.Name(nil), r.names...)
}

// ThreadCapture is an agent.AckWait that confirms every step message
// and records the signed messages for later thread verification.
type ThreadCapture struct {
	mu   sync.Mutex
	msgs []envelope.Message
}

// NewThreadCapture returns an empty ThreadCapture.
func NewThreadCapture() *ThreadCapture { return &ThreadCapture{} }

// Wait confirms msg and records it.
func (t *ThreadCapture) Wait(ctx context.Context, msg envelope.Message) (envelope.Ack, error) {
	ack, err := envelope.NewAck(msg, "e2e-capture", "captured: "+msg.Payload)
	if err != nil {
		return envelope.Ack{}, err
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	t.msgs = append(t.msgs, msg)
	return ack.Confirm(), nil
}

// Messages returns every recorded message, in wait order.
func (t *ThreadCapture) Messages() []envelope.Message {
	t.mu.Lock()
	defer t.mu.Unlock()
	return append([]envelope.Message(nil), t.msgs...)
}
