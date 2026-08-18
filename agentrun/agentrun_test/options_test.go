package agentrun_test

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/MiviaLabs/mivia-ai-sdk/agent"
	"github.com/MiviaLabs/mivia-ai-sdk/agentrun"
	"github.com/MiviaLabs/mivia-ai-sdk/contextbudget"
	"github.com/MiviaLabs/mivia-ai-sdk/events"
	"github.com/MiviaLabs/mivia-ai-sdk/identity"
	"github.com/MiviaLabs/mivia-ai-sdk/machine"
	"github.com/MiviaLabs/mivia-ai-sdk/tools"
)

// newRejectCase mutates a valid Options base and names the expected
// failure: a sentinel error, or a substring the wrapped error carries.
type newRejectCase struct {
	name     string
	mutate   func(*agentrun.Options)
	wantSent error
	wantText string
}

// runNewRejections drives New over cases, asserting each expected
// failure against a fresh copy of valid.
func runNewRejections(t *testing.T, valid agentrun.Options, cases []newRejectCase) {
	t.Helper()
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			opts := valid
			c.mutate(&opts)
			_, err := agentrun.New(opts)
			if c.wantSent != nil {
				if !errors.Is(err, c.wantSent) {
					t.Fatalf("New error = %v, want %v", err, c.wantSent)
				}
				return
			}
			if err == nil {
				t.Fatalf("New succeeded, want error %q", c.wantText)
			}
			if !strings.Contains(err.Error(), c.wantText) {
				t.Fatalf("New error %q lacks %q", err, c.wantText)
			}
		})
	}
}

// newRejectSetup builds the Options base and the ask/scope fixtures the
// rejection cases mutate.
func newRejectSetup(t *testing.T) (ca *captureAsk, scope *tools.Scope, valid agentrun.Options) {
	ca = &captureAsk{approved: true, payload: "yes"}
	scope = tools.NewScope(tools.ScopeOptions{})
	valid = agentrun.Options{
		Agent:   mustAgent(t, oneStepPlan(t)),
		Machine: oneStepMachine(t),
		Tools:   oneStepRegistry(t),
	}
	return ca, scope, valid
}

// TestNewRejectionsBackends drives the sentinel path where a required
// backend is wholly absent.
func TestNewRejectionsBackends(t *testing.T) {
	_, _, valid := newRejectSetup(t)
	runNewRejections(t, valid, []newRejectCase{
		{name: "nil agent", mutate: func(o *agentrun.Options) { o.Agent = nil }, wantSent: agentrun.ErrNoAgent},
		{name: "nil machine", mutate: func(o *agentrun.Options) { o.Machine = nil }, wantSent: agentrun.ErrNoMachine},
		{name: "ambiguous wait", mutate: func(o *agentrun.Options) { o.Wait = waitFn() }, wantSent: agentrun.ErrAmbiguousWait},
		{name: "no resolver", mutate: func(o *agentrun.Options) { o.Tools = nil }, wantSent: agentrun.ErrNoResolver},
	})
}

// TestNewRejectionsToolDependencies drives the sentinel path a Wait
// branch needs to resolve through tools.
func TestNewRejectionsToolDependencies(t *testing.T) {
	ca, scope, valid := newRejectSetup(t)
	runNewRejections(t, valid, []newRejectCase{
		{
			name: "wait with scope needs tools",
			mutate: func(o *agentrun.Options) {
				o.Tools = nil
				o.Wait = waitFn()
				o.Scope = scope
			},
			wantSent: agentrun.ErrNoTools,
		},
		{
			name: "wait with store needs tools",
			mutate: func(o *agentrun.Options) {
				o.Tools = nil
				o.Wait = waitFn()
				o.Store = mustStore(t)
			},
			wantSent: agentrun.ErrNoTools,
		},
		{
			name: "wait with ask needs tools",
			mutate: func(o *agentrun.Options) {
				o.Tools = nil
				o.Wait = waitFn()
				o.Ask = ca.Answer
				o.AskTo = "human"
			},
			wantSent: agentrun.ErrNoTools,
		},
		{
			name: "wait with artifacts needs tools",
			mutate: func(o *agentrun.Options) {
				o.Tools = nil
				o.Wait = waitFn()
				o.Artifacts = &agentrun.Artifacts{}
			},
			wantSent: agentrun.ErrNoTools,
		},
		{
			name: "ask needs askto",
			mutate: func(o *agentrun.Options) {
				o.Ask = ca.Answer
				o.AskTo = ""
			},
			wantSent: agentrun.ErrNoRecipient,
		},
	})
}

// TestNewRejectionsContent drives the sentinel path where a wired value
// fails a content check.
func TestNewRejectionsContent(t *testing.T) {
	_, _, valid := newRejectSetup(t)
	runNewRejections(t, valid, []newRejectCase{
		{
			name: "invalid budget",
			mutate: func(o *agentrun.Options) {
				o.Budget = &contextbudget.Limits{MaxBytes: -1}
			},
			wantText: "MaxBytes",
		},
		{
			name: "matrix mismatch",
			mutate: func(o *agentrun.Options) {
				o.Machine = brokenMachine(t)
			},
			wantText: "no transition",
		},
		{
			name: "unresolvable tool",
			mutate: func(o *agentrun.Options) {
				o.Tools = tools.New()
			},
			wantSent: tools.ErrUnknownName,
		},
		{
			name:     "zero-value receiver rejects with ErrReceiverEmpty",
			mutate:   func(o *agentrun.Options) { o.Receiver = &identity.Identity{} },
			wantSent: agentrun.ErrReceiverEmpty,
		},
	})
}

// brokenMachine returns a valid machine whose rows cannot satisfy the
// one-step plan, so ValidateMatrix fails.
func brokenMachine(t *testing.T) *machine.Definition {
	t.Helper()
	m, err := machine.New("queued",
		machine.Transition{From: "queued", To: "elsewhere", Trigger: "run"},
	)
	if err != nil {
		t.Fatalf("machine.New: %v", err)
	}
	return m
}

// TestNewAccept builds a valid Runner and proves New returns it with a
// wired, subscribed bus.
func TestNewAccept(t *testing.T) {
	opts := agentrun.Options{
		Agent:     mustAgent(t, oneStepPlan(t)),
		Machine:   oneStepMachine(t),
		Tools:     oneStepRegistry(t),
		Store:     mustStore(t),
		Artifacts: &agentrun.Artifacts{},
		Scope:     tools.NewScope(tools.ScopeOptions{}),
	}
	runner, err := agentrun.New(opts)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if runner == nil {
		t.Fatal("New returned a nil Runner")
	}
	if runner.Bus() == nil {
		t.Fatal("New returned a Runner with a nil bus")
	}
	// The no-op handlers make every agent event name emit without the
	// "no subscriber" fault events.Bus raises.
	for _, name := range []events.Name{
		agent.MessageDeliveredEvent,
		agent.MessageAckedEvent,
		agent.ThreadVerifiedEvent,
	} {
		if err := runner.Bus().Emit(context.Background(), events.Event{Name: name, Data: "x"}); err != nil {
			t.Fatalf("bus.Emit(%s): %v", name, err)
		}
	}
}

// TestNewUsesCallerBus proves an Options.Bus the caller provides is the
// bus Runner.Bus returns, subscribed in place.
func TestNewUsesCallerBus(t *testing.T) {
	bus := events.New()
	runner, err := agentrun.New(agentrun.Options{
		Agent:   mustAgent(t, oneStepPlan(t)),
		Machine: oneStepMachine(t),
		Tools:   oneStepRegistry(t),
		Bus:     bus,
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if runner.Bus() != bus {
		t.Fatal("Runner.Bus is not the caller-provided bus")
	}
	// The caller bus must carry the no-op subscriptions New adds. An
	// unsubscribed bus fails Emit, so this catches a New that returns
	// the caller bus without wiring it.
	for _, name := range []events.Name{
		agent.MessageDeliveredEvent,
		agent.MessageAckedEvent,
		agent.ThreadVerifiedEvent,
	} {
		if err := runner.Bus().Emit(context.Background(), events.Event{Name: name, Data: "x"}); err != nil {
			t.Fatalf("caller bus.Emit(%s): %v", name, err)
		}
	}
}

// TestRunRejectsEmptyThread proves Runner.Run checks threadID before
// it touches the wired blocks.
func TestRunRejectsEmptyThread(t *testing.T) {
	runner, err := agentrun.New(agentrun.Options{
		Agent:   mustAgent(t, oneStepPlan(t)),
		Machine: oneStepMachine(t),
		Tools:   oneStepRegistry(t),
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	_, _, err = runner.Run(context.Background(), "", machine.InOut{})
	if !errors.Is(err, agent.ErrNoThread) {
		t.Fatalf("Run error = %v, want agent.ErrNoThread", err)
	}
}

// TestRunWithWaitResolver proves the Wait branch of Runner.Run drives a
// run with no Tools and no built chain.
func TestRunWithWaitResolver(t *testing.T) {
	ctx := context.Background()
	runner, err := agentrun.New(agentrun.Options{
		Agent:   mustAgent(t, oneStepPlan(t)),
		Machine: oneStepMachine(t),
		Wait:    waitFn(),
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	status, _, err := runner.Run(ctx, "thread-wait", machine.InOut{})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if status != "resolved" {
		t.Fatalf("status = %q, want %q", status, "resolved")
	}
}
