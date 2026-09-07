package agentloop

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/MiviaLabs/mivia-ai-sdk/context/budget"
	"github.com/MiviaLabs/mivia-ai-sdk/context/plan"
	"github.com/MiviaLabs/mivia-ai-sdk/events"
	"github.com/MiviaLabs/mivia-ai-sdk/provider"
	"github.com/MiviaLabs/mivia-ai-sdk/tools"
	"github.com/MiviaLabs/mivia-ai-sdk/trace"
)

// ErrInvalidOptions is Options.Validate's, Bounds.Validate's, and
// Conclude.Validate's error for every configuration-shape check: an
// omitted required field, or a numeric field that fails its range
// rule. The wrapped message names the field and the rule it failed.
// Test with errors.Is; do not switch on message text.
var ErrInvalidOptions = errors.New("agentloop: invalid options")

// Sentinel errors for Definitions and Run; test with errors.Is.
var (
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
	// non-empty and the offered tool set ends up empty. Past
	// ErrNoSchema, which fails a schema-less tool directly, that means
	// the scope denied every tool.
	ErrNoSchemas = errors.New("agentloop: registry offers no schema-bearing tool the scope allows")
	// ErrNoSchema is Definitions's error when a scope-allowed
	// registered tool publishes no parameter schema. A scope-denied
	// tool never reaches this check. Wrapped with the tool's registry
	// name. Test with errors.Is.
	ErrNoSchema = errors.New("agentloop: registered tool publishes no parameter schema")
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
	// (wrapping plan.ErrRetentionOverflow), the summarizer call
	// failed (wrapping the contextsummary sentinel), or the compacted
	// history still exceeds the window. Test with errors.Is.
	ErrCompactionFailed = errors.New("agentloop: compaction failed")
	// ErrNoTokenEstimator is EnableCompaction's error when the
	// Completer lacks the provider.TokenEstimator capability, so
	// EnableCompaction has no estimator to fill Calibrated with. See
	// also ErrInvalidOptions for the direct-Options path, which
	// Validate raises once Window is set without Calibrated. Test
	// with errors.Is.
	ErrNoTokenEstimator = errors.New("agentloop: Completer does not implement provider.TokenEstimator")
)

// RecoveryTargetTokens is the fixed compaction target of the
// prompt-too-long recovery path.
const RecoveryTargetTokens = 16384

// CompactionNotice is the user-role message content Run appends after
// a recovery compaction, so the model sees that compaction occurred.
const CompactionNotice = "Earlier messages were compacted into a context summary. Some detail was dropped."

// DefaultConcludeNotice is Options.Conclude.Notice's fallback text.
const DefaultConcludeNotice = "You are close to the iteration limit. Provide your best final answer now."

// DuplicateCallNotice replaces a tool result's content when
// DedupWithinTurn detects the same (tool, canonical-argument) call
// already served earlier in the same turn.
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

// Summarizer generates the summary one compaction requires. An
// implementation returns plan.ErrSummarySkipped to decline
// summary generation; compactHistory then reuses the prior summary or
// proceeds without one. Build the field's value only through
// EnableCompaction or plan.NewSummarizer. Warning: a typed
// nil (*plan.Summarizer)(nil) stored in the field is not
// nil as an interface, so Validate's nil check passes and the first
// Summarize call panics.
type Summarizer interface {
	Summarize(ctx context.Context, msgs []provider.Message) (plan.Summary, error)
}

// Compile-time proof, pinned in options.go under the interface.
var _ Summarizer = (*plan.Summarizer)(nil)

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
	// Bounds groups the loop's numeric caps; see the Bounds type for
	// the members and their zero values.
	Bounds Bounds
	// OnToolError governs what Run does with a tool-run error.
	OnToolError ErrorPolicy
	// Hooks fires PointPreTool and PointPostTool per tool call, and
	// PointStop once at the end. Optional.
	Hooks *events.Registry
	// Tracer opens one span per iteration and one per tool call.
	// Optional.
	Tracer *trace.Tracer
	// Usage records per-iteration provider.Usage under SessionID.
	// Requires SessionID. Optional.
	Usage *provider.Accumulator
	// SessionID keys Usage's running total. Required when Usage is
	// set.
	SessionID string
	// Bus receives Run's iteration, completion-heartbeat, and
	// tool-call events. Required when HeartbeatInterval is positive;
	// Run emits nothing through a nil Bus otherwise. Optional.
	Bus *events.Bus
	// Budget caps one Completer call's message history by byte count
	// and message count. A nil Budget means uncapped. When Window is
	// also set, Budget checks the history after window compaction runs,
	// so a history Window would compact under Budget never fails here.
	// When Window is nil, Budget checks history exactly as sent.
	Budget *budget.Limits
	// Trim runs before each Completer call on the full message
	// history. A nil Trim passes the history through unchanged. See
	// docs/plans/agentloop.md for its contract with
	// plan.Planner.Plan.
	Trim func(ctx context.Context, msgs []provider.Message) ([]provider.Message, error)
	// Audit receives one AuditRecord per completed Completer turn and
	// per tool call whose result reaches history. A nil Audit means
	// Run performs no audit call, at no added cost.
	Audit AuditFunc
	// Compaction groups the context-window planning triple. The zero
	// value disables planning. See the Compaction type.
	Compaction Compaction
	// ObserveRequest runs after reserveWork and before every
	// Completer.Chat call, including the prompt-too-long recovery retry's
	// call. A non-nil error fails the iteration before the call runs.
	// This is not Options.Audit: Audit records after the fact and cannot
	// fail a call. A nil hook is a no-op.
	ObserveRequest func(ctx context.Context, req provider.Request) error
	// HeartbeatInterval emits a heartbeat Event on Bus every interval
	// while one Completer call or one tool call is in flight. Zero
	// disables heartbeats. A positive HeartbeatInterval requires a
	// non-nil Bus.
	HeartbeatInterval time.Duration
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

// ErrorFunc is the type of Options.OnToolCallError. The SDK invokes
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
// Budget passes budget.Limits.Validate, a non-nil Compaction.Window
// passes Window.Validate, requires Compaction.Summarizer, requires
// Compaction.Calibrated, and excludes Trim, Conclude.Validate (Margin
// not negative, then Deadline not negative), a positive
// HeartbeatInterval requires a non-nil Bus, and finally WorkBudget and
// ToolBudget each pass their own check. The three Extensions checks
// read the pointer nil-safely; a nil Extensions means every knob at
// its zero value. Every check in this fixed order returns
// ErrInvalidOptions, wrapped with the failing field's name and rule;
// test with errors.Is against ErrInvalidOptions, not message text.
func (o Options) Validate() error {
	if o.Completer == nil {
		return fmt.Errorf("%w: %s", ErrInvalidOptions, "Completer: required")
	}
	if o.Tools == nil {
		return fmt.Errorf("%w: %s", ErrInvalidOptions, "Tools: required")
	}
	if err := o.Bounds.Validate(); err != nil {
		return err
	}
	if o.Usage != nil && strings.TrimSpace(o.SessionID) == "" {
		return fmt.Errorf("%w: %s", ErrInvalidOptions, "SessionID: required when Usage is set")
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
			return fmt.Errorf("%w: %s", ErrInvalidOptions, "Summarizer: required when Window is set")
		}
		if o.Compaction.Calibrated == nil {
			return fmt.Errorf("%w: %s", ErrInvalidOptions, "Calibrated: required when Window is set")
		}
		if o.Trim != nil {
			return fmt.Errorf("%w: %s", ErrInvalidOptions, "Trim: mutually exclusive with Window")
		}
	}
	if o.Extensions != nil {
		if err := o.Extensions.Conclude.Validate(); err != nil {
			return err
		}
	}
	if o.HeartbeatInterval > 0 && o.Bus == nil {
		return fmt.Errorf("%w: %s", ErrInvalidOptions, "HeartbeatInterval: requires a non-nil Bus")
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
