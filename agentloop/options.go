package agentloop

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

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
	// ErrMaxIterations is Validate's error when MaxIterations is
	// negative. Reserved for construction-time validation; Run itself
	// never returns it, since hitting MaxIterations at runtime is a
	// graceful StopMaxIterations stop, not an error. A zero
	// MaxIterations inside a fully zero Bounds receives DefaultBounds
	// at New. A zero MaxIterations inside a partial Bounds stays
	// unbounded; a caller wanting that unbounded explicitly sets
	// math.MaxInt32.
	ErrMaxIterations = errors.New("agentloop: MaxIterations must be non-negative")
	// ErrUnrenderableResult is the render path's error when a tool
	// result's Out.Value cannot be marshaled to JSON after failing the
	// string and UTF-8-bytes cases.
	ErrUnrenderableResult = errors.New("agentloop: tool result cannot be rendered")
	// ErrCallsPerTurnExceeded is Run's error when one turn's response
	// requests more calls than a positive MaxCallsPerTurn allows. This
	// trip always fails the run, before any call in the turn runs,
	// regardless of OnToolError.
	ErrCallsPerTurnExceeded = errors.New("agentloop: turn requested more calls than MaxCallsPerTurn allows")
	// ErrNoSchema is Definitions's error when a registered tool
	// publishes no parameter schema. Wrapped with the tool's registry
	// name. Test with errors.Is.
	ErrNoSchema = errors.New("agentloop: registered tool publishes no parameter schema")
	// ErrNoSchemas is Definitions's error when the registry is
	// non-empty and the offered tool set ends up empty. Past
	// ErrNoSchema this means a Scope denied every tool.
	ErrNoSchemas = errors.New("agentloop: registry offers no tool the scope allows")
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
	// ErrSummarizerRequired is Options.Validate's error when
	// Compaction.Window is set and Compaction.Summarizer is nil. Test
	// with errors.Is.
	ErrSummarizerRequired = errors.New("agentloop: Window requires Summarizer")
	// ErrEstimatorRequired is Options.Validate's error when
	// Compaction.Window is set and Compaction.Calibrated is nil.
	// Guards the direct-Options path: a caller set Window by hand
	// without also setting Calibrated. See also ErrNoTokenEstimator
	// for the EnableCompaction path, which checks the Completer's
	// capability instead of the field. Test with errors.Is.
	ErrEstimatorRequired = errors.New("agentloop: Window requires Calibrated")
	// ErrNoTokenEstimator is EnableCompaction's error when the
	// Completer lacks the provider.TokenEstimator capability, so
	// EnableCompaction has no estimator to fill Calibrated with. See
	// also ErrEstimatorRequired for the direct-Options path, which
	// Validate raises once Window is set without Calibrated. Test
	// with errors.Is.
	ErrNoTokenEstimator = errors.New("agentloop: Completer does not implement provider.TokenEstimator")
	// ErrTrimExcluded is Options.Validate's error when both
	// Compaction.Window and Trim are set. Test with errors.Is.
	ErrTrimExcluded = errors.New("agentloop: Window and Trim are mutually exclusive")
	// ErrConcludeMargin is Validate's error when Conclude.Margin is
	// negative. Test with errors.Is.
	ErrConcludeMargin = errors.New("agentloop: ConcludeMargin must not be negative")
	// ErrMaxConcurrentTools is Options.Validate's error when
	// MaxConcurrentTools is negative. Zero means serial (today's
	// behavior); a positive value runs that many calls in parallel
	// through a worker pool. Test with errors.Is.
	ErrMaxConcurrentTools = errors.New("agentloop: MaxConcurrentTools must not be negative")
	// ErrMaxConsecutiveToolFailures is Validate's error when
	// MaxConsecutiveToolFailures is negative. Test with errors.Is.
	ErrMaxConsecutiveToolFailures = errors.New("agentloop: MaxConsecutiveToolFailures must not be negative")
	// ErrHeartbeatRequiresBus is Options.Validate's error when
	// HeartbeatInterval is positive and Bus is nil: a heartbeat with
	// nowhere to emit is a caller mistake, not a silent no-op. Test
	// with errors.Is.
	ErrHeartbeatRequiresBus = errors.New("agentloop: HeartbeatInterval requires a non-nil Bus")
	// ErrSessionIDRequired is Options.Validate's error when Usage is set
	// and SessionID is blank. Test with errors.Is.
	ErrSessionIDRequired = errors.New("agentloop: Usage requires a non-blank SessionID")
	// ErrMaxTotalTokens is Options.Validate's error when MaxTotalTokens
	// is negative. Test with errors.Is.
	ErrMaxTotalTokens = errors.New("agentloop: MaxTotalTokens must not be negative")
	// ErrMaxCallsPerTurn is Validate's error when MaxCallsPerTurn is
	// negative. Zero means unbounded. Test with errors.Is.
	ErrMaxCallsPerTurn = errors.New("agentloop: MaxCallsPerTurn must not be negative")
	// ErrConcludeDeadline is Options.Validate's error when
	// Conclude.Deadline is negative. Test with errors.Is.
	ErrConcludeDeadline = errors.New("agentloop: ConcludeDeadline must be non-negative")
)

// RecoveryTargetTokens is the fixed compaction target of the
// prompt-too-long recovery path.
const RecoveryTargetTokens = 16384

// CompactionNotice is the user-role message content Run appends after
// a recovery compaction, so the model sees that compaction occurred.
const CompactionNotice = "Earlier messages were compacted into a context summary. Some detail was dropped."

// DefaultConcludeNotice is Options.Extensions.Conclude.Notice's
// fallback text.
const DefaultConcludeNotice = "You are close to the iteration limit. Provide your best final answer now."

// DuplicateCallNotice replaces a tool result's content when
// Extensions.DedupWithinTurn detects the same (tool,
// canonical-argument) call already served earlier in the same turn.
const DuplicateCallNotice = "[duplicate-call] This exact tool call was already served earlier in this turn; skipped to avoid a repeated side effect."

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

// StopReason and its constants live in stop.go, beside StopDecision
// and the graceful-stop helpers.

// Options declares the blocks one New call wires into a Loop.
// Completer and Tools are required; the rest are optional. The host
// integration knobs live behind Extensions, the last field.
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
	// Bounds groups the loop's numeric caps. The zero value receives
	// DefaultBounds at New; see this addendum's defaulting rule.
	Bounds Bounds
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
	// Bus receives Run's iteration, heartbeat, and tool-call events.
	// Required when HeartbeatInterval is positive. Optional.
	Bus *events.Bus
	// HeartbeatInterval emits a heartbeat Event on Bus while one
	// Completer or tool call is in flight. Zero disables heartbeats.
	HeartbeatInterval time.Duration
	// Budget caps one Completer call's message history by byte count
	// and message count. A nil Budget means uncapped. When
	// Compaction.Window is set, Budget checks the compacted history.
	Budget *contextbudget.Limits
	// Trim runs before each Completer call on the full history. A nil
	// Trim passes history through unchanged. Trim and
	// Compaction.Window are mutually exclusive.
	Trim func(ctx context.Context, msgs []provider.Message) ([]provider.Message, error)
	// Audit receives one AuditRecord per audited event. A nil Audit
	// means Run performs no audit call. Optional.
	Audit AuditFunc
	// Compaction groups the context-window planning triple. The zero
	// value disables planning. See the Compaction type.
	Compaction Compaction
	// Extensions holds the host-integration knobs. A nil Extensions
	// means every knob at its zero value. Optional.
	Extensions *Extensions
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
	// ThinkingContent is the response's readable reasoning text, copied out
	// so a renderer can sign or audit it independently of Response.
	// Empty on a completion whose assistant turn produced no reasoning
	// and on every tool-call record.
	ThinkingContent string
	// CacheUsage is the response's prompt-cache accounting, copied out
	// for the same reason. Reported is false on every completion whose
	// Completer did not report cache usage and on every tool-call
	// record.
	CacheUsage provider.CacheUsage
}

// AuditFunc receives one AuditRecord per audited event, in the order
// Run produces them. A non-nil return is a hard failure: Run wraps it
// with the iteration count and returns it exactly like a Trim error,
// per the Result-shape rule.
type AuditFunc func(ctx context.Context, rec AuditRecord) error

// ErrorFunc is the type of Extensions.OnToolCallError. The SDK invokes
// it on the ErrorPolicyReport path after a decodeAndRun or render
// failure. Returning a non-zero Message with nil error appends msg in
// place of the [tool-error] body. Returning an error fails the run
// with err wrapped under iteration and call.ID, with no RoleTool
// message appended. Returning the zero Message and nil preserves the
// default body. The function never runs under ErrorPolicyFail.
type ErrorFunc func(ctx context.Context, call provider.ToolCall, err error) (provider.Message, error)

// Validate checks Options in a fixed order and returns the first
// failure: Completer required, Tools required, Bounds.Validate (each
// cap non-negative), Usage requires a non-blank SessionID, a non-nil
// Budget passes contextbudget.Limits.Validate, a non-nil
// Compaction.Window passes Window.Validate, requires Summarizer,
// requires Calibrated, and excludes Trim, then Conclude.Validate
// inside Extensions (Margin not negative, then Deadline not
// negative), a positive HeartbeatInterval requires a non-nil Bus, and
// finally WorkBudget and ToolBudget inside Extensions each pass their
// own check. A nil Extensions skips both Extensions blocks.
func (o Options) Validate() error {
	if o.Completer == nil {
		return ErrNoCompleter
	}
	if o.Tools == nil {
		return ErrNoTools
	}
	if err := o.Bounds.Validate(); err != nil {
		return err
	}
	if o.Usage != nil && strings.TrimSpace(o.SessionID) == "" {
		return ErrSessionIDRequired
	}
	if o.Budget != nil {
		if err := o.Budget.Validate(); err != nil {
			return fmt.Errorf("agentloop: invalid Budget: %w", err)
		}
	}
	if o.Compaction.Window != nil {
		if err := o.Compaction.Window.Validate(); err != nil {
			return fmt.Errorf("agentloop: invalid Window: %w", err)
		}
		if o.Compaction.Summarizer == nil {
			return ErrSummarizerRequired
		}
		if o.Compaction.Calibrated == nil {
			return ErrEstimatorRequired
		}
		if o.Trim != nil {
			return ErrTrimExcluded
		}
	}
	if o.Extensions != nil {
		if err := o.Extensions.Conclude.Validate(); err != nil {
			return err
		}
	}
	if o.HeartbeatInterval > 0 && o.Bus == nil {
		return ErrHeartbeatRequiresBus
	}
	if o.Extensions != nil {
		if err := o.Extensions.WorkBudget.validate(); err != nil {
			return err
		}
		if err := o.Extensions.ToolBudget.validate(); err != nil {
			return err
		}
	}
	return nil
}
