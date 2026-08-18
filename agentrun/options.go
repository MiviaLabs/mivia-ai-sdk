package agentrun

import (
	"context"
	"errors"
	"fmt"

	"github.com/MiviaLabs/mivia-ai-sdk/agent"
	"github.com/MiviaLabs/mivia-ai-sdk/channel"
	"github.com/MiviaLabs/mivia-ai-sdk/contextbudget"
	"github.com/MiviaLabs/mivia-ai-sdk/events"
	"github.com/MiviaLabs/mivia-ai-sdk/heartbeat"
	"github.com/MiviaLabs/mivia-ai-sdk/identity"
	"github.com/MiviaLabs/mivia-ai-sdk/machine"
	"github.com/MiviaLabs/mivia-ai-sdk/memory"
	"github.com/MiviaLabs/mivia-ai-sdk/tools"
)

// Sentinel errors for New and Run; test with errors.Is. The matrix
// and budget checks wrap their own errors instead, naming the invalid
// value.
var (
	ErrNoAgent       = errors.New("agentrun: agent is required")
	ErrNoMachine     = errors.New("agentrun: machine is required")
	ErrNoResolver    = errors.New("agentrun: Wait or Tools is required")
	ErrAmbiguousWait = errors.New("agentrun: Wait and Tools both set; set one")
	ErrNoTools       = errors.New("agentrun: Scope, Store, Ask, or Artifacts needs Tools")
	ErrNoRecipient   = errors.New("agentrun: Ask needs AskTo")
	ErrResultNotText = errors.New("agentrun: tool result is not a string")
	ErrReceiverEmpty = errors.New("agentrun: Receiver signer is empty")
)

// Options declares the blocks one New call wires into a Runner. The
// Agent and Machine fields are required; the rest are optional. Wait
// and Tools are mutually exclusive ack resolvers; one of them must be
// set.
type Options struct {
	// Agent is the composed agent to drive. Required.
	Agent *agent.Agent
	// Machine is the status model the plan targets. Required.
	Machine *machine.Definition
	// Receiver is the ack From identity. It defaults to Agent.Signer().
	Receiver *identity.Identity
	// Bus receives the agent's events. Built and subscribed when nil.
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
	Budget *contextbudget.Limits
	// Monitor beats each gated step's id. Optional.
	Monitor *heartbeat.Monitor
	// Wait resolves each gated step's ack. It is mutually exclusive
	// with Tools.
	Wait agent.AckWait
}

// New validates opts, then wires the resolved blocks into a Runner.
// It runs every check in a fixed order and returns the first failure.
// Checks: Agent and Machine non-nil; Wait and Tools not both set; one
// of them set; Scope, Store, Ask, and Artifacts each need Tools; Ask
// needs a non-empty AskTo; a set Budget passes its own Validate; the
// transition matrix passes ValidateMatrix; and, with Tools set, every
// Confirm-gated step ID resolves in the registry. New subscribes one
// no-op handler to the three agent event names on the resolved bus,
// then returns it through Runner.Bus.
func New(opts Options) (*Runner, error) {
	if opts.Agent == nil {
		return nil, ErrNoAgent
	}
	if opts.Machine == nil {
		return nil, ErrNoMachine
	}
	if opts.Wait != nil && opts.Tools != nil {
		return nil, ErrAmbiguousWait
	}
	if opts.Wait == nil && opts.Tools == nil {
		return nil, ErrNoResolver
	}
	if opts.Tools == nil && (opts.Scope != nil || opts.Store != nil || opts.Ask != nil || opts.Artifacts != nil) {
		return nil, ErrNoTools
	}
	if opts.Ask != nil && opts.AskTo == "" {
		return nil, ErrNoRecipient
	}
	if opts.Budget != nil {
		if err := opts.Budget.Validate(); err != nil {
			return nil, fmt.Errorf("agentrun: invalid budget: %w", err)
		}
	}
	if err := ValidateMatrix(opts.Agent.Plan(), opts.Machine); err != nil {
		return nil, err
	}
	if opts.Tools != nil {
		if err := resolveGatedSteps(opts.Agent.Plan(), opts.Tools); err != nil {
			return nil, err
		}
	}

	receiver := opts.Agent.Signer()
	if opts.Receiver != nil {
		s := opts.Receiver.Signer()
		if s == "" {
			return nil, ErrReceiverEmpty
		}
		receiver = s
	}
	if receiver == "" {
		return nil, ErrReceiverEmpty
	}
	bus := opts.Bus
	if bus == nil {
		bus = events.New()
	}
	noop := func(context.Context, events.Event) error { return nil }
	for _, name := range []events.Name{agent.MessageDeliveredEvent, agent.MessageAckedEvent, agent.ThreadVerifiedEvent} {
		if err := bus.Subscribe(name, noop); err != nil {
			return nil, err
		}
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
		wait:      opts.Wait,
	}, nil
}
