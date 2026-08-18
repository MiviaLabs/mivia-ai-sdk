package agentrun_test

import (
	"context"
	"sync"
	"testing"

	"github.com/MiviaLabs/mivia-ai-sdk/agent"
	"github.com/MiviaLabs/mivia-ai-sdk/channel"
	"github.com/MiviaLabs/mivia-ai-sdk/discovery"
	"github.com/MiviaLabs/mivia-ai-sdk/envelope"
	"github.com/MiviaLabs/mivia-ai-sdk/events"
	"github.com/MiviaLabs/mivia-ai-sdk/flow"
	"github.com/MiviaLabs/mivia-ai-sdk/identity"
	"github.com/MiviaLabs/mivia-ai-sdk/machine"
	"github.com/MiviaLabs/mivia-ai-sdk/memory"
	"github.com/MiviaLabs/mivia-ai-sdk/tools"
)

// prefixTool is a registry tool that returns prefix plus its input
// payload, so different steps record distinct, deterministic results.
type prefixTool struct {
	name   string
	prefix string
}

// Name returns the tool's registry name.
func (t prefixTool) Name() string { return t.name }

// Run returns t.prefix joined to the string input payload.
func (t prefixTool) Run(ctx context.Context, in tools.InOut) (tools.Out, error) {
	s, _ := in.Value.(string)
	return tools.Out{Value: t.prefix + s}, nil
}

// echoTool returns its string input payload unchanged.
type echoTool struct{ name string }

// Name returns the tool's registry name.
func (t echoTool) Name() string { return t.name }

// Run returns the string input payload unchanged.
func (t echoTool) Run(ctx context.Context, in tools.InOut) (tools.Out, error) {
	s, _ := in.Value.(string)
	return tools.Out{Value: s}, nil
}

// nonTextTool returns a numeric result, not a string, for the
// ErrResultNotText path.
type nonTextTool struct{ name string }

// Name returns the tool's registry name.
func (t nonTextTool) Name() string { return t.name }

// Run returns a numeric result.
func (t nonTextTool) Run(ctx context.Context, in tools.InOut) (tools.Out, error) {
	return tools.Out{Value: 42}, nil
}

// mustAgent builds an Agent over plan, failing the test on error.
func mustAgent(t *testing.T, plan *flow.Definition) *agent.Agent {
	t.Helper()
	id, err := identity.New()
	if err != nil {
		t.Fatalf("identity.New: %v", err)
	}
	card := discovery.Card{Name: "test-agent", Capabilities: []string{"cap"}}
	a, err := agent.New(id, card, plan)
	if err != nil {
		t.Fatalf("agent.New: %v", err)
	}
	return a
}

// mustStore builds a memory.Store over a generous budget, failing on
// error.
func mustStore(t *testing.T) *memory.Store {
	t.Helper()
	s, err := memory.New(4096)
	if err != nil {
		t.Fatalf("memory.New: %v", err)
	}
	return s
}

// oneStepPlan returns a single-step plan whose step ID is t1.
func oneStepPlan(t *testing.T) *flow.Definition {
	t.Helper()
	p, err := flow.New([]flow.Step{
		{ID: "t1", To: "resolved", Payload: "seed"},
	}, nil)
	if err != nil {
		t.Fatalf("flow.New: %v", err)
	}
	return p
}

// mustMachine builds a machine over an initial status and rows, failing
// on error.
func mustMachine(t *testing.T, initial machine.Status, rows ...machine.Transition) *machine.Definition {
	t.Helper()
	m, err := machine.New(initial, rows...)
	if err != nil {
		t.Fatalf("machine.New: %v", err)
	}
	return m
}

// oneStepMachine returns the queued-to-resolved machine oneStepPlan
// targets.
func oneStepMachine(t *testing.T) *machine.Definition {
	t.Helper()
	m, err := machine.New("queued",
		machine.Transition{From: "queued", To: "resolved", Trigger: "run"},
	)
	if err != nil {
		t.Fatalf("machine.New: %v", err)
	}
	return m
}

// oneStepRegistry returns a registry holding one tool named t1.
func oneStepRegistry(t *testing.T) *tools.Registry {
	t.Helper()
	reg := tools.New()
	if err := reg.Add(prefixTool{name: "t1", prefix: "out:"}); err != nil {
		t.Fatalf("registry.Add: %v", err)
	}
	return reg
}

// addTools registers every tool into reg, failing on the first error.
func addTools(t *testing.T, reg *tools.Registry, ts ...tools.Tool) {
	t.Helper()
	for _, tl := range ts {
		if err := reg.Add(tl); err != nil {
			t.Fatalf("registry.Add: %v", err)
		}
	}
}

// waitFn returns a stub resolver that confirms every step message.
func waitFn() agent.AckWait {
	return func(ctx context.Context, msg envelope.Message) (envelope.Ack, error) {
		ack, err := envelope.NewAck(msg, "receiver", "ok")
		if err != nil {
			return envelope.Ack{}, err
		}
		return ack.Confirm(), nil
	}
}

// eventCounter tallies events a subscribed handler observes.
type eventCounter struct {
	mu     sync.Mutex
	counts map[events.Name]int
}

// handler returns a Handler that counts every event it sees.
func (c *eventCounter) handler() events.Handler {
	return func(ctx context.Context, e events.Event) error {
		c.mu.Lock()
		defer c.mu.Unlock()
		if c.counts == nil {
			c.counts = make(map[events.Name]int)
		}
		c.counts[e.Name]++
		return nil
	}
}

// count returns the number of times name fired.
func (c *eventCounter) count(name events.Name) int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.counts[name]
}

// captureAsk is a channel.Notifier test double recording each call and
// replying with a fixed answer.
type captureAsk struct {
	mu        sync.Mutex
	calls     int
	questions []channel.Question
	approved  bool
	payload   string
}

// Answer records the question and returns the fixed answer.
func (n *captureAsk) Answer(ctx context.Context, q channel.Question) (channel.Answer, error) {
	n.mu.Lock()
	defer n.mu.Unlock()
	n.calls++
	n.questions = append(n.questions, q)
	return channel.Answer{QuestionID: q.ID, Approved: n.approved, Payload: n.payload}, nil
}

// record returns the captured call count and last question.
func (n *captureAsk) record() (int, channel.Question) {
	n.mu.Lock()
	defer n.mu.Unlock()
	var q channel.Question
	if len(n.questions) > 0 {
		q = n.questions[len(n.questions)-1]
	}
	return n.calls, q
}
