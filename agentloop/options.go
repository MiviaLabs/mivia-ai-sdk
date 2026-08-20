package agentloop

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/MiviaLabs/mivia-ai-sdk/contextbudget"
	"github.com/MiviaLabs/mivia-ai-sdk/contextplan"
	"github.com/MiviaLabs/mivia-ai-sdk/contextsummary"
	"github.com/MiviaLabs/mivia-ai-sdk/events"
	"github.com/MiviaLabs/mivia-ai-sdk/hooks"
	"github.com/MiviaLabs/mivia-ai-sdk/provider"
	"github.com/MiviaLabs/mivia-ai-sdk/tools"
	"github.com/MiviaLabs/mivia-ai-sdk/trace"
	"github.com/MiviaLabs/mivia-ai-sdk/usage"
)

// Sentinel errors for Options.Validate, Definitions, and Run; test
// with errors.Is.
var (
	// ErrNoCompleter is Validate's error when Completer is nil.
	ErrNoCompleter = errors.New("agentloop: completer is required")
	// ErrNoTools is Validate's error when Tools is nil.
	ErrNoTools = errors.New("agentloop: tools registry is required")
	// ErrMaxIterations is Validate's error when MaxIterations is not
	// positive. Reserved for construction-time validation; Run itself
	// never returns it, since hitting MaxIterations at runtime is a
	// graceful StopMaxIterations stop, not an error.
	ErrMaxIterations = errors.New("agentloop: MaxIterations must be positive")
	// ErrUnrenderableResult is the render path's error when a tool
	// result's Out.Value cannot be marshaled to JSON after failing the
	// string and UTF-8-bytes cases.
	ErrUnrenderableResult = errors.New("agentloop: tool result cannot be rendered")
	// ErrCallsPerTurnExceeded is Run's error when one turn's response
	// requests more calls than a positive MaxCallsPerTurn allows. This
	// trip always fails the run, before any call in the turn runs,
	// regardless of OnToolError.
	ErrCallsPerTurnExceeded = errors.New("agentloop: turn requested more calls than MaxCallsPerTurn allows")
	// ErrNoSchemas is Definitions's error when the registry is
	// non-empty and the offered tool set ends up empty, whatever the
	// cause: every tool lacking a schema, a Scope denying every tool,
	// or both together.
	ErrNoSchemas = errors.New("agentloop: registry offers no schema-bearing tool the scope allows")
	// ErrOverBudget is Run's error when the message history, summed by
	// content bytes and message count, fails a non-nil Budget's Fits
	// check ahead of a Completer call.
	ErrOverBudget = errors.New("agentloop: message history exceeds Budget")
	// ErrTokenBudgetExceeded is Run's error when the run's cumulative
	// billed tokens exceed a positive MaxTotalTokens after a Completer
	// call returns.
	ErrTokenBudgetExceeded = errors.New("agentloop: cumulative tokens exceed MaxTotalTokens")
	// ErrInvalidSchema is New's error when a SchemaTool's
	// ParameterSchema() fails schema.Compile. Test with errors.Is.
	ErrInvalidSchema = errors.New("agentloop: tool parameter schema does not compile")
	// ErrArgumentValidation is decodeAndRun's error when
	// call.Arguments fails schema.Compiled.Validate against the called
	// tool's compiled parameter schema, before DecodeArguments runs.
	// Wraps the underlying schema error (schema.ErrValidation,
	// schema.ErrMalformedPayload, or schema.ErrAdmission). Routed
	// through OnToolError exactly like a DecodeArguments failure. Test
	// with errors.Is.
	ErrArgumentValidation = errors.New("agentloop: tool call arguments failed schema validation")
	// ErrToolNotOffered is decodeAndRun's error when a model-chosen call
	// names a tool with no entry in l.schemas, the schema set New
	// compiled once from the Scope-offered tools at construction time.
	// This happens when a caller registers a schema-bearing,
	// Scope-allowed tool on the shared *tools.Registry after New already
	// ran: Registry.Get and Scope.Allowed both read the live registry and
	// the live scope, so the call still reaches decodeAndRun, but
	// l.schemas, frozen at New, carries no entry for it. Routed through
	// OnToolError exactly like ErrArgumentValidation and
	// tools.ErrUnknownName. Test with errors.Is.
	ErrToolNotOffered = errors.New("agentloop: tool call names a tool not offered when New ran")
	// ErrPlanFailed is Run's error when the planning step cannot produce
	// an estimate or a plan: an estimator error or an invalid Window at
	// iteration time. Test with errors.Is.
	ErrPlanFailed = errors.New("agentloop: context planning failed")
	// ErrCompactionFailed is Run's error when a required compaction
	// cannot complete: the retention set alone exceeds the window
	// (wrapping contextplan.ErrRetentionOverflow), the summarizer call
	// failed (wrapping the contextsummary sentinel), or the compacted
	// history still exceeds the window. Test with errors.Is.
	ErrCompactionFailed = errors.New("agentloop: compaction failed")
	// ErrSummarizerRequired is Options.Validate's error when Window is
	// set and Summarizer is nil. Test with errors.Is.
	ErrSummarizerRequired = errors.New("agentloop: Window requires Summarizer")
	// ErrEstimatorRequired is Options.Validate's error when Window is
	// set and Calibrated is nil. Test with errors.Is.
	ErrEstimatorRequired = errors.New("agentloop: Window requires Calibrated")
	// ErrTrimExcluded is Options.Validate's error when both Window and
	// Trim are set. Test with errors.Is.
	ErrTrimExcluded = errors.New("agentloop: Window and Trim are mutually exclusive")
	// ErrConcludeMargin is Validate's error when ConcludeMargin is
	// negative. Test with errors.Is.
	ErrConcludeMargin = errors.New("agentloop: ConcludeMargin must not be negative")
	// ErrTurnResultBudget is Validate's error when TurnResultBudget is
	// negative. Test with errors.Is.
	ErrTurnResultBudget = errors.New("agentloop: TurnResultBudget must not be negative")
)

// RecoveryTargetTokens is the fixed compaction target of the
// prompt-too-long recovery path.
const RecoveryTargetTokens = 16384

// CompactionNotice is the user-role message content Run appends after
// a recovery compaction, so the model sees that compaction occurred.
const CompactionNotice = "Earlier messages were compacted into a context summary. Some detail was dropped."

// DefaultConcludeNotice is Options.ConcludeNotice's fallback text.
const DefaultConcludeNotice = "You are close to the iteration limit. Provide your best final answer now."

// ErrorPolicy names what Run does with a tool-run error: report it to
// the model as a tool result, or end the run.
type ErrorPolicy string

// ErrorPolicyReport is the zero value: a tool-run error, including a
// DecodeArguments failure, is sent back as the tool's RoleTool result
// content, and the run continues. ErrorPolicyFail turns the same
// error into Run's own hard-fail return.
const (
	ErrorPolicyReport ErrorPolicy = ""
	ErrorPolicyFail   ErrorPolicy = "fail"
)

// StopReason names why Run stopped gracefully. No StopToolError
// constant exists: a tool error under ErrorPolicyFail is a hard
// failure, not a graceful stop.
type StopReason string

// The declared StopReason values.
const (
	// StopNoToolCalls is Run's stop reason when the model's response
	// carries no tool call.
	StopNoToolCalls StopReason = "no_tool_calls"
	// StopMaxIterations is Run's stop reason when the iteration count
	// reaches Options.MaxIterations. Not an error.
	StopMaxIterations StopReason = "max_iterations"
	// StopHookVeto is Run's stop reason when a PointPreTool handler
	// vetoes a tool call. The tool does not run.
	StopHookVeto StopReason = "hook_veto"
	// StopConcluded is Run's stop reason when the model returns no tool
	// call on an iteration ConcludeMargin nudged. Graceful, same
	// Result-shape rule as StopNoToolCalls.
	StopConcluded StopReason = "concluded"
)

// Options declares the blocks one New call wires into a Loop.
// Completer and Tools are required; the rest are optional.
type Options struct {
	// Completer runs each chat turn. Required.
	Completer provider.Completer
	// Tools is the registry Definitions builds the offered tool set
	// from, and RunScoped resolves a model-chosen call against.
	// Required.
	Tools *tools.Registry
	// Scope narrows which tools a model-chosen call may invoke. Run
	// always calls Registry.RunScoped, never Registry.Run.
	Scope *tools.Scope
	// Model names the model Request.Model carries. An empty Model
	// means the Completer's own default.
	Model string
	// MaxIterations bounds the number of Completer calls one Run
	// makes. Must be positive.
	MaxIterations int
	// MaxCallsPerTurn bounds the number of tool calls one turn's
	// response may request. Zero means unbounded.
	MaxCallsPerTurn int
	// MaxTotalTokens caps the run's cumulative billed tokens, summed
	// across every Completer call. Zero means unbounded.
	MaxTotalTokens int
	// OnToolError governs what Run does with a tool-run error.
	OnToolError ErrorPolicy
	// Hooks fires PointPreTool and PointPostTool per tool call, and
	// PointStop once at the end. Optional.
	Hooks *hooks.Registry
	// Tracer opens one span per iteration and one per tool call.
	// Optional.
	Tracer *trace.Tracer
	// Usage records per-iteration provider.Usage under SessionID.
	// Requires SessionID. Optional.
	Usage *usage.Accumulator
	// SessionID keys Usage's running total. Required when Usage is
	// set.
	SessionID string
	// Bus is reserved for the loop's own events, pending a future
	// event vocabulary. Run does not yet emit anything through it.
	// Optional.
	Bus *events.Bus
	// Budget caps one Completer call's message history by byte count
	// and message count. A nil Budget means uncapped.
	Budget *contextbudget.Limits
	// Trim runs before each Completer call on the full message
	// history. A nil Trim passes the history through unchanged. See
	// docs/plans/agentloop.md for its contract with
	// contextplan.Planner.Plan.
	Trim func(ctx context.Context, msgs []provider.Message) ([]provider.Message, error)
	// Audit receives one AuditRecord per completed Completer turn and
	// per tool call whose result reaches history. A nil Audit means
	// Run performs no audit call, at no added cost.
	Audit AuditFunc
	// Window plans every iteration against a token budget. A nil Window
	// disables planning; the loop then runs exactly as before. A non-nil
	// Window requires Summarizer and Calibrated, and excludes Trim.
	Window *contextplan.Window
	// Summarizer runs the LLM summary every compaction requires.
	// Required when Window is set.
	Summarizer *contextsummary.Summarizer
	// Calibrated estimates tokens for planning and receives one Observe
	// call after every Chat. Required when Window is set.
	Calibrated *contextplan.Calibrated
	// ConcludeMargin nudges the model to produce a final answer as
	// MaxIterations approaches, appending ConcludeNotice once, instead of
	// hard-stopping at MaxIterations with no notice. Zero disables
	// nudging. Run appends the notice before the Completer call at
	// 1-based iteration k the first time MaxIterations-k < ConcludeMargin
	// holds; k ranges from 1 to MaxIterations, so a positive ConcludeMargin
	// greater than or equal to MaxIterations fires the nudge on Run's
	// first iteration. See docs/plans/agents/phase79_graceful_conclude.md
	// for the worked table.
	ConcludeMargin int
	// ConcludeNotice is the RoleUser content Run appends once nudging
	// starts. Empty ConcludeNotice with a positive ConcludeMargin uses
	// DefaultConcludeNotice. Run appends the notice at the tail of
	// history, as the last message in the nudged iteration's
	// Request.Messages, not spliced near the system message the way
	// CompactionNotice is. A tail append puts the "final answer now"
	// instruction directly before the model's next response. The append
	// runs after this iteration's Trim, Budget, and Window steps,
	// immediately before the Completer call.
	ConcludeNotice string
	// TurnResultBudget caps the summed byte size of one turn's rendered
	// tool results, across every call in that turn, before they append to
	// history. Zero means uncapped: the budget comparison and any shaping
	// are skipped entirely, and every call's content passes through
	// whole. Distinct from a Tool's own tools.ResultBudgetOf bound, which
	// caps one call's content alone; TurnResultBudget shapes the batch as
	// a set, after each call's own bound already applied, in
	// ToolCall.Index order. Hard cap when positive: a call's content
	// stays whole only when the running total plus that content's byte
	// length does not exceed TurnResultBudget; otherwise the content is
	// replaced with BatchTruncationNotice and the running total does not
	// grow for it. The running total never exceeds TurnResultBudget.
	// Applies to every appended RoleTool content, including a reported
	// tool-run error under ErrorPolicyReport.
	TurnResultBudget int
}

// AuditKind names which of Run's two audit-relevant events an
// AuditRecord describes.
type AuditKind string

// The declared AuditKind values.
const (
	// AuditKindCompletion is one completed Completer.Chat call.
	AuditKindCompletion AuditKind = "completion"
	// AuditKindToolCall is one tool call whose RoleTool result message
	// reached history.
	AuditKindToolCall AuditKind = "tool_call"
)

// AuditRecord is one audit-relevant event from a Run call, passed to
// Options.Audit. A caller builds and signs its own envelope.Message
// from the fields it needs; agentloop signs nothing itself.
type AuditRecord struct {
	// Iteration is the 1-based Completer-call count this record
	// belongs to, matching Result.Iterations at the same point.
	Iteration int
	// Kind names which event this record describes.
	Kind AuditKind
	// Request is the exact provider.Request sent to Completer.Chat
	// this iteration. Set only when Kind == AuditKindCompletion.
	Request provider.Request
	// Response is the provider.Response Completer.Chat returned this
	// iteration. Set only when Kind == AuditKindCompletion.
	Response provider.Response
	// ToolCall is the model-requested call this record describes. Set
	// only when Kind == AuditKindToolCall.
	ToolCall provider.ToolCall
	// ToolResult is the RoleTool message runOneToolCall appended to
	// history for ToolCall, including any ToolErrorPrefix marker. Set
	// only when Kind == AuditKindToolCall.
	ToolResult provider.Message
	// Err is the tool-run error runOneToolCall reported, or nil on a
	// successful call. Set only when Kind == AuditKindToolCall.
	Err error
}

// AuditFunc receives one AuditRecord per audited event, in the order
// Run produces them. A non-nil return is a hard failure: Run wraps it
// with the iteration count and returns it exactly like a Trim error,
// per the Result-shape rule.
type AuditFunc func(ctx context.Context, rec AuditRecord) error

// Validate checks Options in a fixed order and returns the first
// failure: Completer required, Tools required, MaxIterations
// positive, Usage requires a non-blank SessionID, a non-nil Budget
// passes contextbudget.Limits.Validate, MaxTotalTokens is not
// negative, a non-nil Window passes Window.Validate, requires
// Summarizer, requires Calibrated, and excludes Trim, ConcludeMargin is
// not negative, and TurnResultBudget is not negative.
func (o Options) Validate() error {
	if o.Completer == nil {
		return ErrNoCompleter
	}
	if o.Tools == nil {
		return ErrNoTools
	}
	if o.MaxIterations <= 0 {
		return ErrMaxIterations
	}
	if o.Usage != nil && strings.TrimSpace(o.SessionID) == "" {
		return errors.New("agentloop: Usage requires a non-blank SessionID")
	}
	if o.Budget != nil {
		if err := o.Budget.Validate(); err != nil {
			return fmt.Errorf("agentloop: invalid Budget: %w", err)
		}
	}
	if o.MaxTotalTokens < 0 {
		return errors.New("agentloop: MaxTotalTokens must not be negative")
	}
	if o.Window != nil {
		if err := o.Window.Validate(); err != nil {
			return fmt.Errorf("agentloop: invalid Window: %w", err)
		}
		if o.Summarizer == nil {
			return ErrSummarizerRequired
		}
		if o.Calibrated == nil {
			return ErrEstimatorRequired
		}
		if o.Trim != nil {
			return ErrTrimExcluded
		}
	}
	if o.ConcludeMargin < 0 {
		return ErrConcludeMargin
	}
	if o.TurnResultBudget < 0 {
		return ErrTurnResultBudget
	}
	return nil
}
