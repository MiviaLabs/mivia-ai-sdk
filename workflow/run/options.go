package run

import (
	"errors"
	"fmt"
	"github.com/MiviaLabs/mivia-ai-sdk/envelope"

	"github.com/MiviaLabs/mivia-ai-sdk/channel"
	"github.com/MiviaLabs/mivia-ai-sdk/context/budget"
	"github.com/MiviaLabs/mivia-ai-sdk/events"
	"github.com/MiviaLabs/mivia-ai-sdk/flow"
	"github.com/MiviaLabs/mivia-ai-sdk/machine"
	"github.com/MiviaLabs/mivia-ai-sdk/memory"
	"github.com/MiviaLabs/mivia-ai-sdk/tools"
	"github.com/MiviaLabs/mivia-ai-sdk/trace"
	"github.com/MiviaLabs/mivia-ai-sdk/workflow"
)

// Sentinel errors for New and Run; test with errors.Is. The matrix
// and budget checks wrap their own errors instead, naming the invalid
// value.
var (
	// ErrInvalidOptions reports an Options field that fails Validate
	// or a New check: a nil Agent or Machine, an ambiguous or missing
	// Wait/Tools resolver, an optional field set without its required
	// Tools, a missing Ask recipient, or an empty Receiver signer. The
	// wrapped message names the field and the rule it broke.
	ErrInvalidOptions = errors.New("run: invalid options")
	ErrResultNotText  = errors.New("run: tool result is not a string")
	// ErrArgumentDecode is chain's error when the resolved tool's
	// DecodeArguments rejects the step's payload bytes. Test with
	// errors.Is.
	ErrArgumentDecode = errors.New("run: tool arguments failed to decode")
	// ErrArtifactsInconsistent is Validate's error when an Artifacts
	// value's current values and run history disagree. Test with
	// errors.Is.
	ErrArtifactsInconsistent = errors.New("run: artifacts state is inconsistent")
)

// Options declares the blocks one New call wires into a Runner. The
// Agent and Machine fields are required; the rest are optional. Wait
// and Tools are mutually exclusive ack resolvers; one of them must be
// set.
type Options struct {
	// Agent is the composed agent to drive. Required.
	Agent *workflow.Agent
	// Machine is the status model the plan targets. Required.
	Machine *machine.Definition
	// Receiver is the ack From identity. It defaults to Agent.Signer().
	Receiver *envelope.Identity
	// Bus receives the agent's events. Built when nil; no handler is
	// subscribed. Callers add handlers through Bus().Subscribe.
	Bus *events.Bus
	// Tools drives the built ack chain, which runs tools by step ID.
	Tools *tools.Registry
	// Scope narrows the tools the chain calls. It needs Tools.
	Scope *tools.Scope
	// Store receives each gated step's result. It needs Tools.
	Store *memory.Store
	// Ask routes an escalated step to a human. It needs Tools.
	Ask channel.Notifier
	// AskTo names the human that answers Ask. It needs Ask.
	AskTo string
	// Artifacts records each gated step's result. It needs Tools.
	Artifacts *Artifacts
	// Room stamps onto each built message. Empty leaves Room zero.
	Room string
	// Budget gates each gated step's context fit. Optional.
	Budget *budget.Limits
	// Monitor beats each gated step's id. Optional.
	Monitor *flow.Monitor
	// Hooks observes and gates the run through the hooks registry.
	// PointPreTool fires before each gated step's tool and vetoes;
	// a veto fails the step. PointPostTool fires after each ack
	// confirms, including an Ask round trip's confirmed ack. Both
	// fire only with Tools: the Wait resolver runs no tool chain.
	// PointStop fires with the final status once the walk ends, with
	// either resolver. Optional.
	Hooks *events.Registry
	// Tracer opens one root span per run and one child span per
	// gated step's tool call. Optional.
	Tracer *trace.Tracer
	// Wait resolves each gated step's ack. It is mutually exclusive
	// with Tools.
	Wait workflow.AckWait
}

// Validate checks every option in a fixed order and returns the
// first failure: Agent and Machine non-nil; Wait and Tools not both
// set; one of them set; Scope, Store, Ask, and Artifacts each need
// Tools; Ask needs a non-empty AskTo; a set Budget passes its own
// Validate; the transition matrix passes ValidateMatrix; and, with
// Tools set, every Confirm-gated step ID resolves in the registry.
// The Receiver check stays in New because it reads the resolved
// signer, not the options alone.
func (o Options) Validate() error {
	if o.Agent == nil {
		return fmt.Errorf("%w: %s", ErrInvalidOptions, "Agent: agent is required")
	}
	if o.Machine == nil {
		return fmt.Errorf("%w: %s", ErrInvalidOptions, "Machine: machine is required")
	}
	if o.Wait != nil && o.Tools != nil {
		return fmt.Errorf("%w: %s", ErrInvalidOptions, "Wait: Wait and Tools both set; set one")
	}
	if o.Wait == nil && o.Tools == nil {
		return fmt.Errorf("%w: %s", ErrInvalidOptions, "Wait: Wait or Tools is required")
	}
	if o.Tools == nil && (o.Scope != nil || o.Store != nil || o.Ask != nil || o.Artifacts != nil) {
		return fmt.Errorf("%w: %s", ErrInvalidOptions, "Tools: Scope, Store, Ask, or Artifacts needs Tools")
	}
	if o.Ask != nil && o.AskTo == "" {
		return fmt.Errorf("%w: %s", ErrInvalidOptions, "AskTo: Ask needs AskTo")
	}
	if o.Budget != nil {
		if err := o.Budget.Validate(); err != nil {
			return fmt.Errorf("run: invalid budget: %w", err)
		}
	}
	if err := ValidateMatrix(o.Agent.Plan(), o.Machine); err != nil {
		return err
	}
	if o.Tools != nil {
		if err := resolveGatedSteps(o.Agent.Plan(), o.Tools); err != nil {
			return err
		}
	}
	return nil
}

// New validates opts, then wires the resolved blocks into a Runner.
// It runs every check in a fixed order and returns the first failure.
// Checks: Agent and Machine non-nil; Wait and Tools not both set; one
// of them set; Scope, Store, Ask, and Artifacts each need Tools; Ask
// needs a non-empty AskTo; a set Budget passes its own Validate; the
// transition matrix passes ValidateMatrix; and, with Tools set, every
// Confirm-gated step ID resolves in the registry. New builds a bus
// when Options.Bus is nil, then returns it through Runner.Bus.
func New(opts Options) (*Runner, error) {
	if err := opts.Validate(); err != nil {
		return nil, err
	}

	receiver := opts.Agent.Signer()
	if opts.Receiver != nil {
		s := opts.Receiver.Signer()
		if s == "" {
			return nil, fmt.Errorf("%w: %s", ErrInvalidOptions, "Receiver: signer is empty")
		}
		receiver = s
	}
	if receiver == "" {
		return nil, fmt.Errorf("%w: %s", ErrInvalidOptions, "Receiver: signer is empty")
	}
	bus := opts.Bus
	if bus == nil {
		bus = events.New()
	}

	return &Runner{
		agent:     opts.Agent,
		machine:   opts.Machine,
		receiver:  receiver,
		bus:       bus,
		tools:     opts.Tools,
		scope:     opts.Scope,
		store:     opts.Store,
		ask:       opts.Ask,
		askTo:     opts.AskTo,
		artifacts: opts.Artifacts,
		room:      opts.Room,
		budget:    opts.Budget,
		monitor:   opts.Monitor,
		hooks:     opts.Hooks,
		tracer:    opts.Tracer,
		wait:      opts.Wait,
	}, nil
}
