package agentrun

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"

	"github.com/MiviaLabs/mivia-ai-sdk/agent"
	"github.com/MiviaLabs/mivia-ai-sdk/channel"
	"github.com/MiviaLabs/mivia-ai-sdk/contextbudget"
	"github.com/MiviaLabs/mivia-ai-sdk/envelope"
	"github.com/MiviaLabs/mivia-ai-sdk/events"
	"github.com/MiviaLabs/mivia-ai-sdk/flow"
	"github.com/MiviaLabs/mivia-ai-sdk/heartbeat"
	"github.com/MiviaLabs/mivia-ai-sdk/machine"
	"github.com/MiviaLabs/mivia-ai-sdk/memory"
	"github.com/MiviaLabs/mivia-ai-sdk/tools"
)

// Runner carries the blocks New validated and wired. Build it with New;
// the fields stay unexported.
type Runner struct {
	agent     *agent.Agent
	machine   *machine.Definition
	receiver  string
	bus       *events.Bus
	tools     *tools.Registry
	scope     *tools.Scope
	store     *memory.Store
	ask       channel.Notifier
	askTo     string
	artifacts *Artifacts
	room      string
	budget    *contextbudget.Limits
	monitor   *heartbeat.Monitor
	wait      agent.AckWait
}

// Run drives the wired agent through the wired machine. threadID names
// the one envelope thread the run's step messages share; an empty
// threadID fails before any block runs, wrapping agent.ErrNoThread. in
// is the starting record. Run returns the final status, the final
// record, and the first error.
func (r *Runner) Run(ctx context.Context, threadID string, in machine.InOut) (machine.Status, machine.InOut, error) {
	if threadID == "" {
		return machine.Status(""), in, fmt.Errorf("agentrun: %w", agent.ErrNoThread)
	}
	wait := r.wait
	if r.tools != nil {
		wait = r.chain()
	}
	return r.agent.Run(ctx, threadID, r.machine, in, wait, r.bus, r.monitor, r.room, r.budget)
}

// Bus returns the resolved event bus New subscribed and wired. Callers
// add their own handlers through Bus().Subscribe for events the run
// emits.
func (r *Runner) Bus() *events.Bus {
	return r.bus
}

// chain returns the built AckWait closure for the wired tools. Per
// gated step it runs the step's tool by ID against the signed step
// message, stores and records the string result, then confirms a
// NewAck. A suffixed message ID, which agent.Run mints for a step
// confirmed twice, resolves the plain tool name, so a looped child
// step runs its tool every iteration. A tool result that is not a
// string fails with ErrResultNotText naming the tool. A tool error
// wrapping agent.ErrEscalated, when Ask is set, routes to one Ask
// round trip.
func (r *Runner) chain() agent.AckWait {
	return func(ctx context.Context, msg envelope.Message) (envelope.Ack, error) {
		name := toolNameFor(r.tools, msg.ID)
		out, err := r.tools.RunScoped(ctx, name, tools.InOut{Value: msg.Payload}, r.scope)
		if err != nil {
			if r.ask != nil && errors.Is(err, agent.ErrEscalated) {
				return r.askRoundTrip(ctx, msg)
			}
			return envelope.Ack{}, err
		}
		result, ok := out.Value.(string)
		if !ok {
			return envelope.Ack{}, fmt.Errorf("agentrun: step %q: tool result not a string: %w", msg.ID, ErrResultNotText)
		}
		if r.store != nil {
			if _, err := r.store.Put([]byte(result)); err != nil {
				return envelope.Ack{}, err
			}
		}
		if r.artifacts != nil {
			r.artifacts.Set(msg.ID, result)
		}
		ack, err := envelope.NewAck(msg, r.receiver, result)
		if err != nil {
			return envelope.Ack{}, err
		}
		return ack.Confirm(), nil
	}
}

// toolNameFor resolves the tool name for one message ID. A name the
// registry holds resolves itself, so a caller tool named with a hash
// wins. Otherwise a "#N" suffix is stripped, resolving the plain
// step name behind a repeated message.
func toolNameFor(reg *tools.Registry, id string) string {
	if _, ok := reg.Get(id); ok {
		return id
	}
	if i := strings.LastIndexByte(id, '#'); i > 0 {
		return id[:i]
	}
	return id
}

// askRoundTrip resolves one escalated step through a single Ask round
// trip. An approved answer confirms the ack with the answer's payload
// as the restatement. A declined answer, or a Notifier error, fails
// the step.
func (r *Runner) askRoundTrip(ctx context.Context, msg envelope.Message) (envelope.Ack, error) {
	q := channel.Question{ID: msg.ID, Recipient: r.askTo, Payload: msg.Payload}
	answer, err := r.ask(ctx, q)
	if err != nil {
		return envelope.Ack{}, err
	}
	if !answer.Approved {
		return envelope.Ack{}, fmt.Errorf("agentrun: step %q declined by %q", msg.ID, r.askTo)
	}
	ack, err := envelope.NewAck(msg, r.receiver, answer.Payload)
	if err != nil {
		return envelope.Ack{}, err
	}
	return ack.Confirm(), nil
}

// resolveGatedSteps verifies every Confirm-gated step ID in plan
// resolves in reg. A two-or-more-member panel wave never calls Confirm,
// so its members and their nested Subs skip the check. A missing name
// fails at New, not mid-run.
func resolveGatedSteps(plan *flow.Definition, reg *tools.Registry) error {
	for _, id := range gatedStepIDs(plan) {
		if _, ok := reg.Get(id); !ok {
			return fmt.Errorf("agentrun: step %q: %w", id, tools.ErrUnknownName)
		}
	}
	return nil
}

// gatedStepIDs collects the step IDs flow.Run calls Confirm for,
// recursively. A step in a two-or-more-member panel never reaches
// Confirm, so it contributes nothing, not even its nested Sub steps.
func gatedStepIDs(d *flow.Definition) []string {
	var out []string
	big := bigPanels(d)
	for _, s := range d.Steps() {
		if big[s.ID] {
			continue
		}
		out = append(out, s.ID)
		if s.Sub != nil {
			out = append(out, gatedStepIDs(s.Sub)...)
		}
	}
	return out
}

// bigPanels returns the set of step IDs named in a panel of two or
// more members.
func bigPanels(d *flow.Definition) map[string]bool {
	m := map[string]bool{}
	for _, p := range d.Panels() {
		if len(p) >= 2 {
			for _, id := range p {
				m[id] = true
			}
		}
	}
	return m
}

// Artifacts records each gated step's tool result by step ID. Build a
// zero value with &Artifacts{}; Set initializes the map on first use.
// It is safe for concurrent use.
type Artifacts struct {
	mu     sync.Mutex
	values map[string]string
}

// Set stores value under step, replacing any earlier value.
func (a *Artifacts) Set(step, value string) {
	if a == nil {
		return
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.values == nil {
		a.values = make(map[string]string)
	}
	a.values[step] = value
}

// Get returns the value stored under step and whether step held one. A
// nil Artifacts reads as empty.
func (a *Artifacts) Get(step string) (string, bool) {
	if a == nil {
		return "", false
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	v, ok := a.values[step]
	return v, ok
}

// PayloadOf builds a flow.PayloadFrom closure that reads one step's
// artifact from a. The closure ignores its record argument and returns
// the stored string, or "" when a holds no value for step yet.
func PayloadOf(step string, a *Artifacts) func(machine.InOut) string {
	return func(machine.InOut) string {
		v, _ := a.Get(step)
		return v
	}
}
