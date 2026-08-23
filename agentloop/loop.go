package agentloop

import (
	"context"
	"fmt"
	"math"
	"time"

	"github.com/MiviaLabs/mivia-ai-sdk/contextbudget"
	"github.com/MiviaLabs/mivia-ai-sdk/contextplan"
	"github.com/MiviaLabs/mivia-ai-sdk/contextsummary"
	"github.com/MiviaLabs/mivia-ai-sdk/events"
	"github.com/MiviaLabs/mivia-ai-sdk/hooks"
	"github.com/MiviaLabs/mivia-ai-sdk/provider"
	"github.com/MiviaLabs/mivia-ai-sdk/schema"
	"github.com/MiviaLabs/mivia-ai-sdk/tools"
	"github.com/MiviaLabs/mivia-ai-sdk/trace"
	"github.com/MiviaLabs/mivia-ai-sdk/usage"
)

// Result holds a Run call's outcome. See docs/plans/agentloop.md's
// Result-shape rule for how each field behaves on a graceful stop
// versus a hard-fail error return.
type Result struct {
	// Final is the last message the model produced. Zero value when
	// the stop happened before a new response arrived, or on
	// StopHookVeto, StopMaxIterations, and StopSteered by design, and
	// on every hard-fail return.
	Final provider.Message
	// History carries every message appended so far, including the
	// caller's starting messages.
	History []provider.Message
	// Iterations counts the number of Completer calls that completed.
	Iterations int
	// Usage sums provider.Usage across every completed Completer call.
	Usage provider.Usage
	// Stop names why Run stopped gracefully. Zero value on a hard-fail
	// error return.
	Stop StopReason
}

// Loop is a bound, ready-to-run tool-calling loop. Built only through
// New.
type Loop struct {
	completer        provider.Completer
	reg              *tools.Registry
	scope            *tools.Scope
	model            string
	maxIterations    int
	maxCallsPerTurn  int
	maxTotalTokens   int
	onToolError      ErrorPolicy
	hooksReg         *hooks.Registry
	tracer           *trace.Tracer
	usageAcc         *usage.Accumulator
	sessionID        string
	bus              *events.Bus
	budget           *contextbudget.Limits
	trim             func(ctx context.Context, msgs []provider.Message) ([]provider.Message, error)
	surfaceFn        func() *Surface
	defs             []provider.ToolDefinition
	schemas          map[string]*schema.Compiled
	audit            AuditFunc
	window           *contextplan.Window
	summarizer       *contextsummary.Summarizer
	calibrated       *contextplan.Calibrated
	concludeMargin   int
	concludeNotice   string
	dedupWithinTurn  bool
	heartbeat        time.Duration
	turnResultBudget int
}

// New validates opts, calls Definitions(opts.Tools, opts.Scope) once,
// and binds the result onto Loop. Run reuses that same
// []provider.ToolDefinition slice for Request.Tools on every
// iteration. New also compiles the parameter schema of every tool in
// defs, keyed by name, through schema.Compile; a compile failure fails
// New with ErrInvalidSchema. The compiled set is exactly the
// Scope-offered set defs already carries, so a malformed schema on a
// tool outside opts.Scope, or outside opts.Tools entirely, never fails
// this Loop's New call.
func New(opts Options) (*Loop, error) {
	if err := opts.Validate(); err != nil {
		return nil, err
	}
	defs, _, err := Definitions(opts.Tools, opts.Scope)
	if err != nil {
		return nil, err
	}
	schemas, err := compileSchemas(defs)
	if err != nil {
		return nil, err
	}
	return &Loop{
		completer:        opts.Completer,
		reg:              opts.Tools,
		scope:            opts.Scope,
		model:            opts.Model,
		maxIterations:    unboundedOrSet(opts.MaxIterations),
		maxCallsPerTurn:  opts.MaxCallsPerTurn,
		maxTotalTokens:   opts.MaxTotalTokens,
		onToolError:      opts.OnToolError,
		hooksReg:         opts.Hooks,
		tracer:           opts.Tracer,
		usageAcc:         opts.Usage,
		sessionID:        opts.SessionID,
		bus:              opts.Bus,
		budget:           opts.Budget,
		trim:             opts.Trim,
		surfaceFn:        opts.Surface,
		defs:             defs,
		schemas:          schemas,
		audit:            opts.Audit,
		window:           opts.Window,
		summarizer:       opts.Summarizer,
		calibrated:       opts.Calibrated,
		concludeMargin:   opts.ConcludeMargin,
		concludeNotice:   resolveConcludeNotice(opts.ConcludeNotice),
		dedupWithinTurn:  opts.DedupWithinTurn,
		heartbeat:        opts.HeartbeatInterval,
		turnResultBudget: opts.TurnResultBudget,
	}, nil
}

// resolveConcludeNotice returns notice unchanged when non-empty, else
// DefaultConcludeNotice.
func resolveConcludeNotice(notice string) string {
	if notice == "" {
		return DefaultConcludeNotice
	}
	return notice
}

// compileSchemas compiles each defs entry's Schema through
// schema.Compile, keyed by Name. Returns ErrInvalidSchema, wrapped
// with the tool name and the underlying schema.Compile reason, on the
// first compile failure.
func compileSchemas(defs []provider.ToolDefinition) (map[string]*schema.Compiled, error) {
	schemas := make(map[string]*schema.Compiled, len(defs))
	for _, def := range defs {
		compiled, err := schema.Compile(def.Schema)
		if err != nil {
			return nil, fmt.Errorf("agentloop: tool %q: %w: %v", def.Name, ErrInvalidSchema, err)
		}
		schemas[def.Name] = compiled
	}
	return schemas, nil
}

// unboundedOrSet maps the legacy MaxSteps <= 0 == unbounded contract
// onto the SDK's maxIterations cap. Zero becomes math.MaxInt32: the
// existing run loop's `iterations >= l.maxIterations` check at
// run.go:86-88 then never trips within a realistic run. Negative
// values are rejected at Validate time, so this helper never sees
// one.
func unboundedOrSet(n int) int {
	if n == 0 {
		return math.MaxInt32
	}
	return n
}
