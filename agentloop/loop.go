package agentloop

import (
	"context"
	"fmt"
	"io"
	"math"
	"time"

	"github.com/MiviaLabs/mivia-ai-sdk/context/budget"
	"github.com/MiviaLabs/mivia-ai-sdk/context/plan"
	"github.com/MiviaLabs/mivia-ai-sdk/events"
	"github.com/MiviaLabs/mivia-ai-sdk/provider"
	"github.com/MiviaLabs/mivia-ai-sdk/schema"
	"github.com/MiviaLabs/mivia-ai-sdk/tools"
	"github.com/MiviaLabs/mivia-ai-sdk/trace"
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
	completer       provider.Completer
	reg             *tools.Registry
	scope           *tools.Scope
	model           string
	bounds          Bounds
	onToolError     ErrorPolicy
	onToolCallError ErrorFunc
	hooksReg        *events.Registry
	tracer          *trace.Tracer
	usageAcc        *provider.Accumulator
	sessionID       string
	bus             *events.Bus
	budget          *budget.Limits
	trim            func(ctx context.Context, msgs []provider.Message) ([]provider.Message, error)
	surfaceFn       func() *Surface
	defs            []provider.ToolDefinition
	schemas         map[string]*schema.Compiled
	audit           AuditFunc
	window          *plan.Window
	summarizer      *plan.Summarizer
	calibrated      *plan.Calibrated
	// defaultEffort is the completer's ReasoningPolicy default, read
	// once at New; empty when the completer has no policy. Each
	// iteration's request carries it when the request sets no effort
	// of its own.
	defaultEffort provider.ReasoningEffort
	conclude      Conclude
	// deadlineAt is StartTime.Add(Conclude.Deadline), computed once
	// in New from opts.StartTime and opts.Conclude.Deadline. Zero
	// when opts.Conclude.Deadline is zero, which makes the deadline
	// term in shouldConclude a no-op. Stored as a wall-clock instant
	// rather than re-derived on every shouldConclude call so the
	// comparison source is stable across the run.
	deadlineAt      time.Time
	dedupWithinTurn bool
	heartbeat       time.Duration
	// streamSink is the caller's Options.StreamingWriter, nil when
	// unset. Immutable after New, so concurrent runs share it safely.
	// Each run's capture buffer is a local, threaded through run,
	// runIteration, and runChat; see agentloop/streaming.go.
	streamSink io.Writer
	// workBudget is the caller's Options.WorkBudget, nil when unset.
	// Immutable after New; see agentloop/budget.go for the call
	// contract the loop honors around each Completer call.
	workBudget *WorkBudget
	// toolBudget is the caller's Options.ToolBudget, nil when unset.
	// Immutable after New; see agentloop/budget.go for the call
	// contract the loop honors before each turn's tool calls dispatch.
	toolBudget *ToolBudget
	// continueOnStop is the caller's Options.ContinueOnStop, nil when
	// unset. Immutable after New; consulted only from gracefulStop, at
	// the three graceful stops in runToolStage.
	continueOnStop func(ctx context.Context, d StopDecision) []provider.Message
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
	defs, err := Definitions(opts.Tools, opts.Scope)
	if err != nil {
		return nil, err
	}
	// Adoption rows: a completer that implements ContextAccountant
	// and ReasoningPolicy hands the loop its window size and default
	// reasoning effort without per-request wiring. The derived window
	// only applies when the summarizer and estimator are already
	// wired, because Validate requires all three together; see
	// EnableCompaction, which wires the missing pair in one call.
	window := opts.Window
	if window == nil && opts.Trim == nil && opts.Summarizer != nil && opts.Calibrated != nil {
		window = deriveWindow(opts.Completer)
	}
	defaultEffort := deriveReasoningEffort(opts.Completer)
	schemas, err := compileSchemas(defs)
	if err != nil {
		return nil, err
	}
	bounds := opts.Bounds
	conclude := opts.Conclude
	// Two transforms on the copies: zero MaxIterations becomes
	// math.MaxInt32, and an empty Notice becomes DefaultConcludeNotice.
	bounds.MaxIterations = unboundedOrSet(bounds.MaxIterations)
	conclude.Notice = resolveConcludeNotice(conclude.Notice)
	return &Loop{
		completer:       opts.Completer,
		reg:             opts.Tools,
		scope:           opts.Scope,
		model:           opts.Model,
		bounds:          bounds,
		onToolError:     opts.OnToolError,
		onToolCallError: opts.OnToolCallError,
		hooksReg:        opts.Hooks,
		tracer:          opts.Tracer,
		usageAcc:        opts.Usage,
		sessionID:       opts.SessionID,
		bus:             opts.Bus,
		budget:          opts.Budget,
		trim:            opts.Trim,
		surfaceFn:       opts.Surface,
		defs:            defs,
		schemas:         schemas,
		audit:           opts.Audit,
		window:          window,
		defaultEffort:   defaultEffort,
		summarizer:      opts.Summarizer,
		calibrated:      opts.Calibrated,
		conclude:        conclude,
		deadlineAt:      computeDeadlineAt(opts.StartTime, opts.Conclude.Deadline),
		dedupWithinTurn: opts.DedupWithinTurn,
		heartbeat:       opts.HeartbeatInterval,
		streamSink:      opts.StreamingWriter,
		workBudget:      opts.WorkBudget,
		toolBudget:      opts.ToolBudget,
		continueOnStop:  opts.ContinueOnStop,
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

// computeDeadlineAt pins the wall-clock instant the time-based
// Conclude.Deadline term measures against. A zero StartTime falls back
// to time.Now() so the threshold fires the deadline into the run
// from the moment of construction. A zero Conclude.Deadline disables
// the term: the returned time.Time is zero, and shouldConclude treats
// it as a no-op. A negative Conclude.Deadline is rejected at Validate
// time, so this helper never sees one.
func computeDeadlineAt(start time.Time, d time.Duration) time.Time {
	if d <= 0 {
		return time.Time{}
	}
	if start.IsZero() {
		start = time.Now()
	}
	return start.Add(d)
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
// onto the SDK's bounds.MaxIterations cap. Zero becomes math.MaxInt32:
// the existing run loop's `iterations >= l.bounds.MaxIterations` check
// at run.go then never trips within a realistic run. Negative
// values are rejected at Validate time, so this helper never sees
// one.
func unboundedOrSet(n int) int {
	if n == 0 {
		return math.MaxInt32
	}
	return n
}
