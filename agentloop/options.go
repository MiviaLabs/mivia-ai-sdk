package agentloop

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/MiviaLabs/mivia-ai-sdk/contextbudget"
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
)

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
// passes contextbudget.Limits.Validate, and MaxTotalTokens is not
// negative.
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
	return nil
}
