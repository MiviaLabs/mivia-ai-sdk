package agentloop_test

import (
	"context"
	"errors"
	"reflect"
	"strings"
	"sync"
	"testing"

	"github.com/MiviaLabs/mivia-ai-sdk/agentloop"
	"github.com/MiviaLabs/mivia-ai-sdk/provider"
	"github.com/MiviaLabs/mivia-ai-sdk/tools"
)

// constantCompleter answers every Chat with the same response and never
// runs out. The always-continue cases need a completer that outlives
// any bound; scriptedCompleter errors once its script is exhausted, and
// replayCompleter reports a zero Usage, which no token-ceiling case can
// use.
type constantCompleter struct {
	mu    sync.Mutex
	resp  provider.Response
	calls int
}

func (c *constantCompleter) Name() string { return "constant" }

func (c *constantCompleter) Chat(ctx context.Context, req provider.Request) (provider.Response, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.calls++
	return c.resp, nil
}

func (c *constantCompleter) ChatStream(ctx context.Context, req provider.Request) (<-chan provider.Chunk, error) {
	return nil, errors.New("constantCompleter: ChatStream not supported")
}

func (c *constantCompleter) callCount() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.calls
}

// stopHookRecorder records every StopDecision Options.ContinueOnStop
// receives and delegates the return value to decide, which sees the
// zero-based invocation index. A nil decide always stops the run.
type stopHookRecorder struct {
	mu        sync.Mutex
	decisions []agentloop.StopDecision
	decide    func(i int, d agentloop.StopDecision) []provider.Message
}

func (r *stopHookRecorder) hook(ctx context.Context, d agentloop.StopDecision) []provider.Message {
	r.mu.Lock()
	i := len(r.decisions)
	r.decisions = append(r.decisions, d)
	r.mu.Unlock()
	if r.decide == nil {
		return nil
	}
	return r.decide(i, d)
}

func (r *stopHookRecorder) count() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return len(r.decisions)
}

func (r *stopHookRecorder) at(t *testing.T, i int) agentloop.StopDecision {
	t.Helper()
	r.mu.Lock()
	defer r.mu.Unlock()
	if i >= len(r.decisions) {
		t.Fatalf("hook invocation %d missing: the hook ran %d times", i, len(r.decisions))
	}
	return r.decisions[i]
}

// continueTimes returns a decide function that continues the run with
// msg for the first n invocations, then stops it.
func continueTimes(n int, msg provider.Message) func(int, agentloop.StopDecision) []provider.Message {
	return func(i int, _ agentloop.StopDecision) []provider.Message {
		if i < n {
			return []provider.Message{msg}
		}
		return nil
	}
}

// continuationMessage is the user turn every continuing case appends.
func continuationMessage() provider.Message {
	return textMessage(provider.RoleUser, "keep going")
}

// countContent counts msgs entries whose Content equals content.
func countContent(msgs []provider.Message, content string) int {
	n := 0
	for _, m := range msgs {
		if m.Content == content {
			n++
		}
	}
	return n
}

// TestContinueOnStopContinuesNoToolCalls proves a non-empty return on
// StopNoToolCalls reaches the completer again with the appended
// message in the next request.
func TestContinueOnStopContinuesNoToolCalls(t *testing.T) {
	rec := &stopHookRecorder{decide: continueTimes(1, continuationMessage())}
	completer := &scriptedCompleter{responses: []provider.Response{
		{Message: textMessage(provider.RoleAssistant, "first")},
		{Message: textMessage(provider.RoleAssistant, "second")},
	}}
	loop, err := agentloop.New(agentloop.Options{
		Completer: completer, Tools: tools.New(), MaxIterations: 5,
		ContinueOnStop: rec.hook,
	})
	if err != nil {
		t.Fatalf("New() error = %v, want nil", err)
	}
	res, err := loop.Run(context.Background(), []provider.Message{textMessage(provider.RoleUser, "hi")})
	if err != nil {
		t.Fatalf("Run() error = %v, want nil", err)
	}
	if completer.callCount() != 2 {
		t.Fatalf("completer calls = %d, want 2: the hook must continue the run", completer.callCount())
	}
	if rec.count() != 2 {
		t.Fatalf("hook invocations = %d, want 2", rec.count())
	}
	if got := rec.at(t, 0).Stop; got != agentloop.StopNoToolCalls {
		t.Fatalf("first decision Stop = %q, want StopNoToolCalls", got)
	}
	if res.Final.Content != "second" {
		t.Fatalf("Final.Content = %q, want %q", res.Final.Content, "second")
	}
	if res.Iterations != 2 {
		t.Fatalf("Iterations = %d, want 2", res.Iterations)
	}
	if countContent(completer.requestAt(1).Messages, "keep going") != 1 {
		t.Fatalf("request 2 does not carry the appended message: %+v", completer.requestAt(1).Messages)
	}
}

// TestContinueOnStopContinuesEmptyResponse proves the same at the
// StopEmptyResponse site. The empty-content response carries
// RoleAssistant, so the case tests the continuation and not the
// Role-less trim failure.
func TestContinueOnStopContinuesEmptyResponse(t *testing.T) {
	rec := &stopHookRecorder{decide: continueTimes(1, continuationMessage())}
	completer := &scriptedCompleter{responses: []provider.Response{
		{Message: textMessage(provider.RoleAssistant, "")},
		{Message: textMessage(provider.RoleAssistant, "second")},
	}}
	loop, err := agentloop.New(agentloop.Options{
		Completer: completer, Tools: tools.New(), MaxIterations: 5,
		ContinueOnStop: rec.hook,
	})
	if err != nil {
		t.Fatalf("New() error = %v, want nil", err)
	}
	res, err := loop.Run(context.Background(), []provider.Message{textMessage(provider.RoleUser, "hi")})
	if err != nil {
		t.Fatalf("Run() error = %v, want nil", err)
	}
	if completer.callCount() != 2 {
		t.Fatalf("completer calls = %d, want 2", completer.callCount())
	}
	if got := rec.at(t, 0).Stop; got != agentloop.StopEmptyResponse {
		t.Fatalf("first decision Stop = %q, want StopEmptyResponse", got)
	}
	if res.Final.Content != "second" {
		t.Fatalf("Final.Content = %q, want %q", res.Final.Content, "second")
	}
	if res.Stop != agentloop.StopNoToolCalls {
		t.Fatalf("Stop = %q, want StopNoToolCalls", res.Stop)
	}
}

// TestContinueOnStopEmptyResponseRoleLessTrimFails pins the documented
// consequence at the StopEmptyResponse site. afterChat appends
// resp.Message unvalidated, so continuing past a Role-less empty
// response sends it into the next applyTrim pass, which fails the run
// with provider.ErrUnknownRole. Documented behavior, not a defect.
func TestContinueOnStopEmptyResponseRoleLessTrimFails(t *testing.T) {
	rec := &stopHookRecorder{decide: continueTimes(1, continuationMessage())}
	completer := &scriptedCompleter{responses: []provider.Response{
		{Message: provider.Message{}},
		{Message: textMessage(provider.RoleAssistant, "second")},
	}}
	loop, err := agentloop.New(agentloop.Options{
		Completer: completer, Tools: tools.New(), MaxIterations: 5,
		Trim: func(ctx context.Context, msgs []provider.Message) ([]provider.Message, error) {
			return msgs, nil
		},
		ContinueOnStop: rec.hook,
	})
	if err != nil {
		t.Fatalf("New() error = %v, want nil", err)
	}
	_, err = loop.Run(context.Background(), []provider.Message{textMessage(provider.RoleUser, "hi")})
	if !errors.Is(err, provider.ErrUnknownRole) {
		t.Fatalf("Run() error = %v, want provider.ErrUnknownRole", err)
	}
	if rec.count() != 1 {
		t.Fatalf("hook invocations = %d, want 1", rec.count())
	}
	if completer.callCount() != 1 {
		t.Fatalf("completer calls = %d, want 1: trim must fail before the second call", completer.callCount())
	}
}

// TestContinueOnStopContinuesConcluded proves the same at the
// StopConcluded site. MaxIterations 5 with ConcludeMargin 5 nudges the
// first call, so the first stop reports StopConcluded.
func TestContinueOnStopContinuesConcluded(t *testing.T) {
	rec := &stopHookRecorder{decide: continueTimes(1, continuationMessage())}
	completer := &scriptedCompleter{responses: []provider.Response{
		{Message: textMessage(provider.RoleAssistant, "first")},
		{Message: textMessage(provider.RoleAssistant, "second")},
	}}
	loop, err := agentloop.New(agentloop.Options{
		Completer: completer, Tools: tools.New(), MaxIterations: 5,
		ConcludeMargin: 5, ContinueOnStop: rec.hook,
	})
	if err != nil {
		t.Fatalf("New() error = %v, want nil", err)
	}
	res, err := loop.Run(context.Background(), []provider.Message{textMessage(provider.RoleUser, "hi")})
	if err != nil {
		t.Fatalf("Run() error = %v, want nil", err)
	}
	if got := rec.at(t, 0).Stop; got != agentloop.StopConcluded {
		t.Fatalf("first decision Stop = %q, want StopConcluded", got)
	}
	if completer.callCount() != 2 {
		t.Fatalf("completer calls = %d, want 2", completer.callCount())
	}
	if res.Stop != agentloop.StopConcluded {
		t.Fatalf("Stop = %q, want StopConcluded", res.Stop)
	}
	if res.Final.Content != "second" {
		t.Fatalf("Final.Content = %q, want %q", res.Final.Content, "second")
	}
}

// runNoToolCallOnce runs a one-response no-tool-call scenario under
// hook and returns the Result. The shared body of the two
// stops-unchanged guards.
func runNoToolCallOnce(t *testing.T, hook func(context.Context, agentloop.StopDecision) []provider.Message) agentloop.Result {
	t.Helper()
	completer := &scriptedCompleter{responses: []provider.Response{
		{Message: textMessage(provider.RoleAssistant, "final"), Usage: provider.Usage{TotalTokens: 3}},
	}}
	loop, err := agentloop.New(agentloop.Options{
		Completer: completer, Tools: tools.New(), MaxIterations: 5,
		ContinueOnStop: hook,
	})
	if err != nil {
		t.Fatalf("New() error = %v, want nil", err)
	}
	res, err := loop.Run(context.Background(), []provider.Message{textMessage(provider.RoleUser, "hi")})
	if err != nil {
		t.Fatalf("Run() error = %v, want nil", err)
	}
	if completer.callCount() != 1 {
		t.Fatalf("completer calls = %d, want 1", completer.callCount())
	}
	return res
}

// TestContinueOnStopNilReturnStopsUnchanged proves a nil return yields
// today's Result field for field, and that the hook still ran exactly
// once on the StopNoToolCalls decision.
func TestContinueOnStopNilReturnStopsUnchanged(t *testing.T) {
	rec := &stopHookRecorder{}
	want := runNoToolCallOnce(t, nil)
	got := runNoToolCallOnce(t, rec.hook)
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("Result with a nil-returning hook = %+v, want %+v", got, want)
	}
	if rec.count() != 1 {
		t.Fatalf("hook invocations = %d, want 1", rec.count())
	}
	if s := rec.at(t, 0).Stop; s != agentloop.StopNoToolCalls {
		t.Fatalf("observed Stop = %q, want StopNoToolCalls", s)
	}
}

// TestContinueOnStopEmptyReturnStopsUnchanged proves an empty non-nil
// return behaves as a nil return, with the same invocation count and
// the same observed Stop value.
func TestContinueOnStopEmptyReturnStopsUnchanged(t *testing.T) {
	rec := &stopHookRecorder{decide: func(int, agentloop.StopDecision) []provider.Message {
		return []provider.Message{}
	}}
	want := runNoToolCallOnce(t, nil)
	got := runNoToolCallOnce(t, rec.hook)
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("Result with an empty-returning hook = %+v, want %+v", got, want)
	}
	if rec.count() != 1 {
		t.Fatalf("hook invocations = %d, want 1", rec.count())
	}
	if s := rec.at(t, 0).Stop; s != agentloop.StopNoToolCalls {
		t.Fatalf("observed Stop = %q, want StopNoToolCalls", s)
	}
}

// requestMessages returns the message list of each request completer
// received, in call order.
func requestMessages(completer *scriptedCompleter, calls int) [][]provider.Message {
	out := make([][]provider.Message, 0, calls)
	for i := 0; i < calls; i++ {
		out = append(out, completer.requestAt(i).Messages)
	}
	return out
}

// TestContinueOnStopNilHookRequestsIdentical proves a nil hook produces
// the same request sequence as a Loop built without the field.
func TestContinueOnStopNilHookRequestsIdentical(t *testing.T) {
	scenario := func(hook func(context.Context, agentloop.StopDecision) []provider.Message) [][]provider.Message {
		t.Helper()
		tool := &schemaEchoTool{name: "echo", schema: []byte(`{}`), result: "x"}
		reg := tools.New()
		mustAdd(t, reg, tool)
		completer := &scriptedCompleter{responses: []provider.Response{
			toolCallResponse(provider.ToolCall{ID: "call-1", Name: "echo", Arguments: []byte("{}")}),
			{Message: textMessage(provider.RoleAssistant, "final")},
		}}
		loop, err := agentloop.New(agentloop.Options{
			Completer: completer, Tools: reg, MaxIterations: 5, ContinueOnStop: hook,
		})
		if err != nil {
			t.Fatalf("New() error = %v, want nil", err)
		}
		if _, err := loop.Run(context.Background(), []provider.Message{textMessage(provider.RoleUser, "hi")}); err != nil {
			t.Fatalf("Run() error = %v, want nil", err)
		}
		if completer.callCount() != 2 {
			t.Fatalf("completer calls = %d, want 2", completer.callCount())
		}
		return requestMessages(completer, 2)
	}
	unset := scenario(nil)
	explicitNil := scenario(func(context.Context, agentloop.StopDecision) []provider.Message { return nil })
	if !reflect.DeepEqual(explicitNil, unset) {
		t.Fatalf("request sequence with a nil-returning hook = %+v, want %+v", explicitNil, unset)
	}
}

// TestContinueOnStopReceivesStopEvidence proves the hook sees the
// assistant message that ended the run, the empty tool-call list, the
// iteration count, and the history.
func TestContinueOnStopReceivesStopEvidence(t *testing.T) {
	rec := &stopHookRecorder{}
	final := textMessage(provider.RoleAssistant, "final")
	completer := &scriptedCompleter{responses: []provider.Response{{Message: final}}}
	loop, err := agentloop.New(agentloop.Options{
		Completer: completer, Tools: tools.New(), MaxIterations: 5,
		ContinueOnStop: rec.hook,
	})
	if err != nil {
		t.Fatalf("New() error = %v, want nil", err)
	}
	if _, err := loop.Run(context.Background(), []provider.Message{textMessage(provider.RoleUser, "hi")}); err != nil {
		t.Fatalf("Run() error = %v, want nil", err)
	}
	if rec.count() != 1 {
		t.Fatalf("hook invocations = %d, want 1", rec.count())
	}
	d := rec.at(t, 0)
	if !reflect.DeepEqual(d.Message, final) {
		t.Fatalf("decision Message = %+v, want %+v", d.Message, final)
	}
	if len(d.ToolCalls) != 0 {
		t.Fatalf("decision ToolCalls = %+v, want empty", d.ToolCalls)
	}
	if d.Iterations != 1 {
		t.Fatalf("decision Iterations = %d, want 1", d.Iterations)
	}
	if len(d.History) != 2 || !reflect.DeepEqual(d.History[1], final) {
		t.Fatalf("decision History = %+v, want the user turn then the assistant turn", d.History)
	}
}

// TestContinueOnStopTrimAppliesToGrownHistory proves Trim runs over the
// grown history on the continued iteration.
func TestContinueOnStopTrimAppliesToGrownHistory(t *testing.T) {
	rec := &stopHookRecorder{decide: continueTimes(1, continuationMessage())}
	var mu sync.Mutex
	var seen [][]provider.Message
	completer := &scriptedCompleter{responses: []provider.Response{
		{Message: textMessage(provider.RoleAssistant, "first")},
		{Message: textMessage(provider.RoleAssistant, "second")},
	}}
	loop, err := agentloop.New(agentloop.Options{
		Completer: completer, Tools: tools.New(), MaxIterations: 5,
		Trim: func(ctx context.Context, msgs []provider.Message) ([]provider.Message, error) {
			mu.Lock()
			seen = append(seen, append([]provider.Message(nil), msgs...))
			mu.Unlock()
			return msgs, nil
		},
		ContinueOnStop: rec.hook,
	})
	if err != nil {
		t.Fatalf("New() error = %v, want nil", err)
	}
	if _, err := loop.Run(context.Background(), []provider.Message{textMessage(provider.RoleUser, "hi")}); err != nil {
		t.Fatalf("Run() error = %v, want nil", err)
	}
	mu.Lock()
	defer mu.Unlock()
	if len(seen) != 2 {
		t.Fatalf("Trim calls = %d, want 2: the continuation must run a second iteration", len(seen))
	}
	if countContent(seen[1], "keep going") != 1 {
		t.Fatalf("second Trim input does not carry the appended message: %+v", seen[1])
	}
	if countContent(seen[1], "first") != 1 {
		t.Fatalf("second Trim input does not carry the first assistant turn: %+v", seen[1])
	}
}

// TestContinueOnStopPanicFailsClosed proves a panicking hook fails the
// run and appends no continuation.
func TestContinueOnStopPanicFailsClosed(t *testing.T) {
	completer := &scriptedCompleter{responses: []provider.Response{
		{Message: textMessage(provider.RoleAssistant, "first")},
		{Message: textMessage(provider.RoleAssistant, "second")},
	}}
	loop, err := agentloop.New(agentloop.Options{
		Completer: completer, Tools: tools.New(), MaxIterations: 5,
		ContinueOnStop: func(context.Context, agentloop.StopDecision) []provider.Message {
			panic("hostile host")
		},
	})
	if err != nil {
		t.Fatalf("New() error = %v, want nil", err)
	}
	res, err := loop.Run(context.Background(), []provider.Message{textMessage(provider.RoleUser, "hi")})
	if err == nil {
		t.Fatal("Run() error = nil, want the panic converted into an error")
	}
	if !strings.Contains(err.Error(), "ContinueOnStop hook panicked") {
		t.Fatalf("Run() error = %v, want a ContinueOnStop panic error", err)
	}
	if completer.callCount() != 1 {
		t.Fatalf("completer calls = %d, want 1: a panic must not continue the run", completer.callCount())
	}
	if res.Stop != "" {
		t.Fatalf("Stop = %q, want the zero value on a hard-fail return", res.Stop)
	}
	if len(res.History) != 2 {
		t.Fatalf("History len = %d, want 2: no continuation may be appended", len(res.History))
	}
}
