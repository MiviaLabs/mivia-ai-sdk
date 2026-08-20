package agentrun

import (
	"context"
	"encoding/json"
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
	"github.com/MiviaLabs/mivia-ai-sdk/hooks"
	"github.com/MiviaLabs/mivia-ai-sdk/machine"
	"github.com/MiviaLabs/mivia-ai-sdk/memory"
	"github.com/MiviaLabs/mivia-ai-sdk/tools"
	"github.com/MiviaLabs/mivia-ai-sdk/trace"
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
	hooks     *hooks.Registry
	tracer    *trace.Tracer
	wait      agent.AckWait
}

// Run drives the wired agent through the wired machine. threadID names
// the one envelope thread the run's step messages share; an empty
// threadID fails before any block runs, wrapping agent.ErrNoThread. in
// is the starting record. Run returns the final status, the final
// record, and the first error. A wired Tracer opens one root span for
// the run and child spans for every gated step's tool call. A wired
// Hooks registry fires PointPreTool before each tool call, PointPostTool
// after the ack confirms, and PointStop with the final status once the
// walk ends, success or failure.
func (r *Runner) Run(ctx context.Context, threadID string, in machine.InOut) (machine.Status, machine.InOut, error) {
	if threadID == "" {
		return machine.Status(""), in, fmt.Errorf("agentrun: %w", agent.ErrNoThread)
	}
	if r.tracer != nil {
		var span *trace.Span
		ctx, span = r.tracer.Start(ctx, "agentrun.run")
		span.SetAttribute("thread", threadID)
		defer span.End()
	}
	wait := r.wait
	if r.tools != nil {
		wait = r.chain()
	}
	status, rec, err := r.agent.Run(ctx, threadID, r.machine, in, wait, r.bus, r.monitor, r.room, r.budget)
	if r.hooks != nil {
		if ferr := r.hooks.Fire(ctx, hooks.PointStop, status); ferr != nil {
			herr := fmt.Errorf("agentrun: stop hook: %w", ferr)
			if err != nil {
				err = errors.Join(err, herr)
			} else {
				err = herr
			}
		}
	}
	return status, rec, err
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
// step runs its tool every iteration, and its artifact lands under
// the bare step ID with the latest result. When the resolved tool
// implements tools.SchemaTool, chain decodes the step's payload bytes
// through DecodeArguments before the tool runs; every other tool
// keeps the plain-string payload unchanged. A decode failure fails
// the step, wrapping ErrArgumentDecode. A tool result that is not a
// string fails with ErrResultNotText naming the tool. A tool error
// wrapping agent.ErrEscalated, when Ask is set, routes to one Ask
// round trip.
func (r *Runner) chain() agent.AckWait {
	return func(ctx context.Context, msg envelope.Message) (envelope.Ack, error) {
		if r.tracer != nil {
			var span *trace.Span
			ctx, span = r.tracer.Start(ctx, "agentrun.tool")
			span.SetAttribute("step", msg.ID)
			defer span.End()
		}
		name := toolNameFor(r.tools, msg.ID)
		if r.hooks != nil {
			if err := r.hooks.Fire(ctx, hooks.PointPreTool, msg); err != nil {
				return envelope.Ack{}, fmt.Errorf("agentrun: step %q: pre-tool hook: %w", msg.ID, err)
			}
		}
		in := tools.InOut{Value: msg.Payload}
		if t, ok := r.tools.Get(name); ok {
			if st, ok := t.(tools.SchemaTool); ok {
				decoded, derr := st.DecodeArguments([]byte(msg.Payload))
				if derr != nil {
					return envelope.Ack{}, fmt.Errorf("agentrun: step %q: %w: %w", msg.ID, ErrArgumentDecode, derr)
				}
				in = decoded
			}
		}
		out, err := r.tools.RunScoped(ctx, name, in, r.scope)
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
			r.artifacts.SetRun(msg.ID, name, result)
		}
		ack, err := envelope.NewAck(msg, r.receiver, result)
		if err != nil {
			return envelope.Ack{}, err
		}
		confirmed := ack.Confirm()
		if r.hooks != nil {
			if err := r.firePostTool(ctx, msg, confirmed); err != nil {
				return envelope.Ack{}, err
			}
		}
		return confirmed, nil
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
	confirmed := ack.Confirm()
	if r.hooks != nil {
		if err := r.firePostTool(ctx, msg, confirmed); err != nil {
			return envelope.Ack{}, err
		}
	}
	return confirmed, nil
}

// firePostTool runs the PointPostTool handlers for one confirmed ack.
func (r *Runner) firePostTool(ctx context.Context, msg envelope.Message, ack envelope.Ack) error {
	if err := r.hooks.Fire(ctx, hooks.PointPostTool, ack); err != nil {
		return fmt.Errorf("agentrun: step %q: post-tool hook: %w", msg.ID, err)
	}
	return nil
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

// Artifacts records each gated step's tool result by step ID. A
// step repeated inside a loop overwrites the entry, so the bare ID
// always holds the latest iteration's result. Every run also
// appends to a per-step history, so a caller can read earlier
// failures and rejections through History. Build a zero value with
// &Artifacts{}; Set initializes the maps on first use. It is safe
// for concurrent use.
type Artifacts struct {
	mu     sync.Mutex
	values map[string]string
	runs   map[string][]Run
}

// Run is one recorded result of one step run. MessageID is the
// signed message's ID, which carries the "#N" counter for a step
// repeated inside a loop.
type Run struct {
	MessageID string
	Value     string
}

// Set stores value under step, replacing any earlier value, and
// appends one Run to the step's history.
func (a *Artifacts) Set(step, value string) {
	a.SetRun("", step, value)
}

// SetRun is Set with the producing message's ID recorded on the
// history entry. An empty msgID records the entry without one.
func (a *Artifacts) SetRun(msgID, step, value string) {
	if a == nil {
		return
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.values == nil {
		a.values = make(map[string]string)
		a.runs = make(map[string][]Run)
	}
	a.values[step] = value
	a.runs[step] = append(a.runs[step], Run{MessageID: msgID, Value: value})
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

// History returns every recorded run of step, in run order. A nil
// Artifacts or an unknown step reads as empty. The caller owns the
// returned slice.
func (a *Artifacts) History(step string) []Run {
	if a == nil {
		return nil
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	return append([]Run(nil), a.runs[step]...)
}

// PayloadOf builds a flow.PayloadFrom closure that reads one step's
// artifact from a. The closure ignores its record argument and returns
// the stored string, or "" when a holds no value for step yet. For a
// step repeated inside a loop it returns the latest iteration's
// result.
func PayloadOf(step string, a *Artifacts) func(machine.InOut) string {
	return func(machine.InOut) string {
		v, _ := a.Get(step)
		return v
	}
}

// wireArtifacts is the JSON form of an Artifacts value. It is
// unexported because Artifacts.values and Artifacts.runs are
// unexported and encoding/json cannot see them directly. This mirrors
// machine.wireDefinition, which exists for the same reason.
type wireArtifacts struct {
	Values map[string]string `json:"values"`
	Runs   map[string][]Run  `json:"runs"`
}

// Encode serializes a's current values and run history to JSON. It
// validates first. It is safe for concurrent use; a concurrent Set
// or SetRun blocks until Encode returns. A nil a encodes as the JSON
// of an empty wireArtifacts and never touches the mutex.
func (a *Artifacts) Encode() ([]byte, error) {
	if a == nil {
		return json.Marshal(wireArtifacts{})
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	if err := a.checkInvariant(); err != nil {
		return nil, err
	}
	w := wireArtifacts{Values: a.values, Runs: a.runs}
	return json.Marshal(w)
}

// DecodeArtifacts parses JSON produced by Encode and validates the
// result. The returned *Artifacts is ready for Get, History, Set,
// SetRun, and a later Encode.
func DecodeArtifacts(data []byte) (*Artifacts, error) {
	var w wireArtifacts
	if err := json.Unmarshal(data, &w); err != nil {
		return nil, fmt.Errorf("agentrun: artifacts decode: %w", err)
	}
	a := &Artifacts{values: w.Values, runs: w.Runs}
	if err := a.Validate(); err != nil {
		return nil, err
	}
	return a, nil
}

// Validate reports whether a holds internally consistent state: every
// step named in its run history has a current value equal to its last
// run's value, and every step holding a current value has at least
// one recorded run. Encode and DecodeArtifacts both call it.
func (a *Artifacts) Validate() error {
	if a == nil {
		return nil
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.checkInvariant()
}

// checkInvariant runs Validate's check without acquiring a.mu. Callers
// that already hold a.mu call this directly; Validate and Encode
// acquire the lock once each and call this so Encode never nests two
// acquisitions of a.mu in one call.
func (a *Artifacts) checkInvariant() error {
	for step, runs := range a.runs {
		if len(runs) == 0 {
			continue
		}
		last := runs[len(runs)-1].Value
		cur, ok := a.values[step]
		if !ok || cur != last {
			return fmt.Errorf("agentrun: step %q: %w: current value %q, last run %q", step, ErrArtifactsInconsistent, cur, last)
		}
	}
	for step := range a.values {
		if len(a.runs[step]) == 0 {
			return fmt.Errorf("agentrun: step %q: %w: current value with no recorded run", step, ErrArtifactsInconsistent)
		}
	}
	return nil
}
