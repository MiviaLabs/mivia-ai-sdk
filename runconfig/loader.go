package runconfig

import (
	"encoding/json"
	"fmt"
	"time"

	"github.com/MiviaLabs/mivia-ai-sdk/agentrun"
	"github.com/MiviaLabs/mivia-ai-sdk/flow"
	"github.com/MiviaLabs/mivia-ai-sdk/machine"
	"github.com/MiviaLabs/mivia-ai-sdk/tools"
)

// wireMachine is the JSON form of the machine section.
type wireMachine struct {
	Initial     string    `json:"initial"`
	Transitions []wireRow `json:"transitions"`
}

// wireRow is one transition row: {from, to, trigger}.
type wireRow struct {
	From    string `json:"from"`
	To      string `json:"to"`
	Trigger string `json:"trigger"`
}

// wirePlan is the JSON form of the plan section.
type wirePlan struct {
	Steps  []wireStep `json:"steps"`
	Panels [][]string `json:"panels"`
}

// wireStep is one plan step. Unknown JSON fields are ignored.
type wireStep struct {
	ID       string     `json:"id"`
	Needs    []string   `json:"needs"`
	To       string     `json:"to"`
	When     string     `json:"when"`
	Payload  string     `json:"payload"`
	Tool     string     `json:"tool"`
	Internal string     `json:"internal"`
	Retry    *wireRetry `json:"retry"`
	Loop     *wireLoop  `json:"loop"`
	Sub      *wirePlan  `json:"sub"`
}

// wireRetry is the JSON form of a flow.RetryPolicy.
type wireRetry struct {
	MaxAttempts int    `json:"max_attempts"`
	BaseDelay   string `json:"base_delay"`
	MaxDelay    string `json:"max_delay"`
}

// wireLoop is the JSON form of a flow.LoopPolicy.
type wireLoop struct {
	Max int `json:"max"`
}

// wireOptions is the JSON form of the options section. It holds pure
// string scalars only.
type wireOptions struct {
	Room  string `json:"room"`
	AskTo string `json:"ask_to"`
}

// wireDocument is the whole JSON document.
type wireDocument struct {
	Machine *wireMachine `json:"machine"`
	Plan    *wirePlan    `json:"plan"`
	Options *wireOptions `json:"options"`
	Tools   []string     `json:"tools"`
}

// Definition is the resolved document: the machine, the plan, the
// options, the tool set, and the step bindings. Load fills every
// field except Blocks and External, which the caller sets before
// Runner. Every field is exported for inspection.
type Definition struct {
	// Plan is the resolved step graph.
	Plan *flow.Definition
	// Machine is the resolved status model.
	Machine *machine.Definition
	// Options carries the string scalars and the caller-set Agent.
	Options agentrun.Options
	// Tools lists the document's external tool names.
	Tools []string
	// Bindings holds one entry per bound step, in plan order.
	Bindings []Binding
	// Blocks holds the caller-set internal tool sources.
	Blocks *Blocks
	// External holds the caller-set external tools by name.
	External *tools.Registry
}

// Load resolves one JSON document into a Definition. It rejects
// malformed JSON, a non-object root, a step with both bindings or an
// empty ID, an undeclared external tool, a blank or duplicate tool
// name, an unknown internal kind, an unknown when value, and any
// rejection from machine.New or flow.New. It wraps every failure in
// ErrBadDocument. The loader never reads the environment.
func Load(data []byte) (*Definition, error) {
	var doc wireDocument
	if err := json.Unmarshal(data, &doc); err != nil {
		return nil, fmt.Errorf("%w: %s", ErrBadDocument, err.Error())
	}
	if doc.Machine == nil || doc.Plan == nil {
		return nil, fmt.Errorf("%w: machine and plan sections are required", ErrBadDocument)
	}
	declared, err := declaredTools(doc.Tools)
	if err != nil {
		return nil, err
	}
	m, err := buildMachine(doc.Machine)
	if err != nil {
		return nil, err
	}
	def := &Definition{
		Machine:  m,
		Tools:    append([]string(nil), doc.Tools...),
		Blocks:   NewBlocks(),
		External: tools.New(),
	}
	if doc.Options != nil {
		def.Options.Room = doc.Options.Room
		def.Options.AskTo = doc.Options.AskTo
	}
	plan, bindings, err := buildPlan(doc.Plan, declared)
	if err != nil {
		return nil, err
	}
	def.Plan = plan
	def.Bindings = bindings
	return def, nil
}

// declaredTools validates the tools array: no blank or duplicate name.
func declaredTools(names []string) (map[string]bool, error) {
	declared := make(map[string]bool, len(names))
	for _, n := range names {
		if n == "" {
			return nil, fmt.Errorf("%w: blank tool name", ErrBadDocument)
		}
		if declared[n] {
			return nil, fmt.Errorf("%w: duplicate tool %q", ErrBadDocument, n)
		}
		declared[n] = true
	}
	return declared, nil
}

// buildMachine feeds the wire rows into machine.New.
func buildMachine(w *wireMachine) (*machine.Definition, error) {
	rows := make([]machine.Transition, 0, len(w.Transitions))
	for _, r := range w.Transitions {
		rows = append(rows, machine.Transition{
			From:    machine.Status(r.From),
			To:      machine.Status(r.To),
			Trigger: machine.Trigger(r.Trigger),
		})
	}
	m, err := machine.New(machine.Status(w.Initial), rows...)
	if err != nil {
		return nil, fmt.Errorf("%w: %s", ErrBadDocument, err.Error())
	}
	return m, nil
}

// buildPlan feeds the wire steps and panels into flow.New. It
// recurses over sub plans and collects one Binding per bound step.
func buildPlan(w *wirePlan, declared map[string]bool) (*flow.Definition, []Binding, error) {
	steps := make([]flow.Step, 0, len(w.Steps))
	var bindings []Binding
	for i := range w.Steps {
		s, b, err := buildStep(&w.Steps[i], declared)
		if err != nil {
			return nil, nil, err
		}
		steps = append(steps, s)
		bindings = append(bindings, b...)
	}
	panels := make([]flow.Panel, 0, len(w.Panels))
	for _, ids := range w.Panels {
		panels = append(panels, flow.Panel(append([]string(nil), ids...)))
	}
	plan, err := flow.New(steps, panels)
	if err != nil {
		return nil, nil, fmt.Errorf("%w: %s", ErrBadDocument, err.Error())
	}
	return plan, bindings, nil
}

// buildStep resolves one wire step into a flow.Step and its binding.
func buildStep(w *wireStep, declared map[string]bool) (flow.Step, []Binding, error) {
	if w.ID == "" {
		return flow.Step{}, nil, fmt.Errorf("%w: empty step id", ErrBadDocument)
	}
	if w.Tool != "" && w.Internal != "" {
		return flow.Step{}, nil, fmt.Errorf("%w: step %q sets both tool and internal", ErrBadDocument, w.ID)
	}
	step := flow.Step{ID: w.ID, Needs: w.Needs, To: w.To, Payload: w.Payload}
	if w.When != "" {
		rule, ok := admissions[w.When]
		if !ok {
			return flow.Step{}, nil, fmt.Errorf("%w: step %q has unknown when %q", ErrBadDocument, w.ID, w.When)
		}
		step.When = rule
	}
	if w.Retry != nil {
		rp, err := w.Retry.policy(w.ID)
		if err != nil {
			return flow.Step{}, nil, err
		}
		step.Retry = rp
	}
	if w.Loop != nil {
		step.Loop = &flow.LoopPolicy{Max: w.Loop.Max}
	}
	if w.Sub != nil {
		sub, subBindings, err := buildPlan(w.Sub, declared)
		if err != nil {
			return flow.Step{}, nil, err
		}
		step.Sub = sub
		return step, subBindings, nil
	}
	if w.Tool != "" {
		if !declared[w.Tool] {
			return flow.Step{}, nil, fmt.Errorf("%w: step %q names undeclared tool %q", ErrBadDocument, w.ID, w.Tool)
		}
		return step, []Binding{{Step: w.ID, Tool: w.Tool}}, nil
	}
	if w.Internal != "" {
		k := Kind(w.Internal)
		if !kinds[k] {
			return flow.Step{}, nil, fmt.Errorf("%w: step %q names unknown internal %q", ErrBadDocument, w.ID, w.Internal)
		}
		return step, []Binding{{Step: w.ID, Kind: k, Internal: true}}, nil
	}
	return step, nil, nil
}

// policy resolves the wire retry into a flow.RetryPolicy. Delay
// strings parse through time.ParseDuration; a bad string is
// ErrBadDocument naming the step and field.
func (w *wireRetry) policy(step string) (*flow.RetryPolicy, error) {
	base, err := parseDuration(w.BaseDelay, step, "base_delay")
	if err != nil {
		return nil, err
	}
	max, err := parseDuration(w.MaxDelay, step, "max_delay")
	if err != nil {
		return nil, err
	}
	return &flow.RetryPolicy{
		MaxAttempts: w.MaxAttempts,
		BaseDelay:   base,
		MaxDelay:    max,
	}, nil
}

// parseDuration parses one delay string, wrapping a parse failure in
// ErrBadDocument.
func parseDuration(s, step, field string) (time.Duration, error) {
	d, err := time.ParseDuration(s)
	if err != nil {
		return 0, fmt.Errorf("%w: step %q retry %q: %s", ErrBadDocument, step, field, err.Error())
	}
	return d, nil
}

// admissions maps the document's when values onto flow admission
// rules.
var admissions = map[string]flow.Admission{
	"on_succeeded": flow.AdmissionOnSucceeded,
	"on_finished":  flow.AdmissionOnFinished,
	"on_failed":    flow.AdmissionOnFailed,
}
