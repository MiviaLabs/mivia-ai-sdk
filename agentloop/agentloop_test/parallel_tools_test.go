package agentloop_test

import (
	"context"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/MiviaLabs/mivia-ai-sdk/agentloop"
	"github.com/MiviaLabs/mivia-ai-sdk/provider"
	"github.com/MiviaLabs/mivia-ai-sdk/tools"
)

// overlapTool is a SchemaTool that tracks in-flight concurrency: Run
// bumps a guarded in-flight counter, records the observed maximum,
// then optionally signals entry and blocks on a release channel. The
// entry/release pair lets a test hold every call inside Run until the
// test has observed the pool's full overlap, deterministically,
// without any time.Sleep.
type overlapTool struct {
	name string
	mu   sync.Mutex
	ids  []string
	cur  int
	max  int
	// entered, when non-nil, is Done once per Run on entry.
	entered *sync.WaitGroup
	// release, when non-nil, blocks every Run until closed.
	release chan struct{}
	// runs counts Run invocations when non-nil.
	runs *int64
}

func (t *overlapTool) Name() string { return t.name }

func (t *overlapTool) ParameterSchema() []byte { return []byte(`{}`) }

func (t *overlapTool) DecodeArguments(raw []byte) (tools.InOut, error) {
	return tools.InOut{}, nil
}

func (t *overlapTool) Run(ctx context.Context, in tools.InOut) (tools.Out, error) {
	if t.runs != nil {
		atomic.AddInt64(t.runs, 1)
	}
	t.mu.Lock()
	t.cur++
	if t.cur > t.max {
		t.max = t.cur
	}
	t.ids = append(t.ids, t.name)
	t.mu.Unlock()
	if t.entered != nil {
		t.entered.Done()
	}
	if t.release != nil {
		<-t.release
	}
	t.mu.Lock()
	t.cur--
	t.mu.Unlock()
	return tools.Out{Value: t.name}, nil
}

// maxInflight returns the highest in-flight Run count observed.
func (t *overlapTool) maxInflight() int {
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.max
}

// waitedGuard waits for wg with a timeout so a deadlocked (serially
// starved) pool fails the test instead of hanging it.
func waitedGuard(t *testing.T, wg *sync.WaitGroup) {
	t.Helper()
	done := make(chan struct{})
	go func() { wg.Wait(); close(done) }()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatalf("calls never all entered Run within 5s: dispatch did not overlap")
	}
}

// threeCallTurn builds a registry of three distinct overlapTools and
// a scripted completer whose first response requests one call per
// tool in Index order.
func threeCallTurn(t *testing.T) (*overlapTool, *overlapTool, *overlapTool, *tools.Registry, *scriptedCompleter) {
	t.Helper()
	a := &overlapTool{name: "a"}
	b := &overlapTool{name: "b"}
	c := &overlapTool{name: "c"}
	reg := tools.New()
	mustAdd(t, reg, a)
	mustAdd(t, reg, b)
	mustAdd(t, reg, c)
	completer := &scriptedCompleter{responses: []provider.Response{
		toolCallResponse(
			provider.ToolCall{ID: "call-a", Name: "a", Index: 0, Arguments: []byte("{}")},
			provider.ToolCall{ID: "call-b", Name: "b", Index: 1, Arguments: []byte("{}")},
			provider.ToolCall{ID: "call-c", Name: "c", Index: 2, Arguments: []byte("{}")},
		),
		{Message: textMessage(provider.RoleAssistant, "final")},
	}}
	return a, b, c, reg, completer
}

// historyToolIDs extracts the RoleTool ToolCallIDs from history, in
// history order.
func historyToolIDs(res agentloop.Result) []string {
	ids := make([]string, 0, 3)
	for _, m := range res.History {
		if m.Role == provider.RoleTool {
			ids = append(ids, m.ToolCallID)
		}
	}
	return ids
}

// assertIndexOrder fails unless ids is exactly want.
func assertIndexOrder(t *testing.T, ids []string, want []string) {
	t.Helper()
	if len(ids) != len(want) {
		t.Fatalf("history tool IDs = %v, want %v", ids, want)
	}
	for i, id := range want {
		if ids[i] != id {
			t.Fatalf("history[%d] = %q, want %q: path must preserve Index order in history", i, ids[i], id)
		}
	}
}

// TestMaxConcurrentToolsSerialPreservesOrder proves MaxConcurrentTools
// 1 keeps the existing serial contract: a turn with three distinct
// calls never has more than one Run in flight, and history follows
// ToolCall.Index order. This is the regression guard for "default 0/1
// = serial, today's behavior".
func TestMaxConcurrentToolsSerialPreservesOrder(t *testing.T) {
	a := &overlapTool{name: "a"}
	b := &overlapTool{name: "b"}
	c := &overlapTool{name: "c"}
	reg := tools.New()
	mustAdd(t, reg, a)
	mustAdd(t, reg, b)
	mustAdd(t, reg, c)
	completer := &scriptedCompleter{responses: []provider.Response{
		toolCallResponse(
			provider.ToolCall{ID: "call-a", Name: "a", Index: 0, Arguments: []byte("{}")},
			provider.ToolCall{ID: "call-b", Name: "b", Index: 1, Arguments: []byte("{}")},
			provider.ToolCall{ID: "call-c", Name: "c", Index: 2, Arguments: []byte("{}")},
		),
		{Message: textMessage(provider.RoleAssistant, "final")},
	}}
	loop, err := agentloop.New(agentloop.Options{
		Completer: completer, Tools: reg, MaxIterations: 5, MaxConcurrentTools: 1,
	})
	if err != nil {
		t.Fatalf("New() error = %v, want nil", err)
	}
	res, err := loop.Run(context.Background(), []provider.Message{textMessage(provider.RoleUser, "hi")})
	if err != nil {
		t.Fatalf("Run() error = %v, want nil", err)
	}
	if res.Stop != agentloop.StopNoToolCalls {
		t.Fatalf("Stop = %q, want StopNoToolCalls", res.Stop)
	}
	if got := a.maxInflight(); got != 1 {
		t.Fatalf("a max in-flight = %d, want 1", got)
	}
	assertIndexOrder(t, historyToolIDs(res), []string{"call-a", "call-b", "call-c"})
}

// TestMaxConcurrentToolsParallelRunsConcurrently proves a positive
// MaxConcurrentTools fans calls out through a worker pool: all three
// distinct calls sit inside Run at the same time (observed maximum
// in-flight count 3, held until the test releases them), and history
// order still follows ToolCall.Index.
func TestMaxConcurrentToolsParallelRunsConcurrently(t *testing.T) {
	a, b, c, reg, completer := threeCallTurn(t)
	entered := &sync.WaitGroup{}
	entered.Add(3)
	release := make(chan struct{})
	for _, tl := range []*overlapTool{a, b, c} {
		tl.entered = entered
		tl.release = release
	}
	loop, err := agentloop.New(agentloop.Options{
		Completer: completer, Tools: reg, MaxIterations: 5, MaxConcurrentTools: 3,
	})
	if err != nil {
		t.Fatalf("New() error = %v, want nil", err)
	}
	type runResult struct {
		res agentloop.Result
		err error
	}
	done := make(chan runResult, 1)
	go func() {
		res, err := loop.Run(context.Background(), []provider.Message{textMessage(provider.RoleUser, "hi")})
		done <- runResult{res, err}
	}()
	waitedGuard(t, entered)
	if got := a.maxInflight() + b.maxInflight() + c.maxInflight(); got != 3 {
		t.Fatalf("sum of per-tool max in-flight = %d, want 3: each tool must hold exactly one call", got)
	}
	close(release)
	out := <-done
	if out.err != nil {
		t.Fatalf("Run() error = %v, want nil", out.err)
	}
	if out.res.Stop != agentloop.StopNoToolCalls {
		t.Fatalf("Stop = %q, want StopNoToolCalls", out.res.Stop)
	}
	assertIndexOrder(t, historyToolIDs(out.res), []string{"call-a", "call-b", "call-c"})
}

// TestMaxConcurrentToolsDefaultZeroIsSerial proves the zero default
// matches today's serial behavior: a turn with three distinct calls
// never overlaps them even though MaxConcurrentTools is unset, so an
// existing caller passing Options{MaxConcurrentTools: 0} sees no
// regression.
func TestMaxConcurrentToolsDefaultZeroIsSerial(t *testing.T) {
	a, b, c, reg, completer := threeCallTurn(t)
	loop, err := agentloop.New(agentloop.Options{
		Completer: completer, Tools: reg, MaxIterations: 5,
	})
	if err != nil {
		t.Fatalf("New() error = %v, want nil", err)
	}
	if _, err := loop.Run(context.Background(), []provider.Message{textMessage(provider.RoleUser, "hi")}); err != nil {
		t.Fatalf("Run() error = %v, want nil", err)
	}
	for _, tl := range []*overlapTool{a, b, c} {
		if got := tl.maxInflight(); got != 1 {
			t.Fatalf("%s max in-flight = %d, want 1: zero MaxConcurrentTools must keep serial dispatch", tl.name, got)
		}
	}
}

// TestMaxConcurrentToolsParallelAuditOrderMatchesIndex proves the
// Audit records the parallel path emits arrive in ToolCall.Index
// order, matching the serial path's audit-order guarantee.
func TestMaxConcurrentToolsParallelAuditOrderMatchesIndex(t *testing.T) {
	_, _, _, reg, completer := threeCallTurn(t)
	auditor := &recordingAuditor{}
	loop, err := agentloop.New(agentloop.Options{
		Completer: completer, Tools: reg, MaxIterations: 5,
		MaxConcurrentTools: 3, Audit: auditor.Audit,
	})
	if err != nil {
		t.Fatalf("New() error = %v, want nil", err)
	}
	if _, err := loop.Run(context.Background(), []provider.Message{textMessage(provider.RoleUser, "hi")}); err != nil {
		t.Fatalf("Run() error = %v, want nil", err)
	}
	recs := auditor.snapshot()
	var toolIDs []string
	for _, rec := range recs {
		if rec.Kind == agentloop.AuditKindToolCall {
			toolIDs = append(toolIDs, rec.ToolCall.ID)
		}
	}
	assertIndexOrder(t, toolIDs, []string{"call-a", "call-b", "call-c"})
}

// TestMaxConcurrentToolsParallelDedupStillServed proves DedupWithinTurn
// still serves DuplicateCallNotice to the second of two byte-equal
// calls when MaxConcurrentTools > 1: dedup is a per-call dispatch
// decision and must not regress under the worker pool.
func TestMaxConcurrentToolsParallelDedupStillServed(t *testing.T) {
	tool := &schemaEchoTool{name: "echo", schema: []byte(`{}`), result: "x"}
	reg := tools.New()
	mustAdd(t, reg, tool)
	completer := &scriptedCompleter{responses: []provider.Response{
		toolCallResponse(
			provider.ToolCall{ID: "call-1", Name: "echo", Index: 0, Arguments: []byte(`{"a":1}`)},
			provider.ToolCall{ID: "call-2", Name: "echo", Index: 1, Arguments: []byte(`{"a":1}`)},
		),
		{Message: textMessage(provider.RoleAssistant, "final")},
	}}
	loop, err := agentloop.New(agentloop.Options{
		Completer: completer, Tools: reg, MaxIterations: 5,
		MaxConcurrentTools: 4, DedupWithinTurn: true,
	})
	if err != nil {
		t.Fatalf("New() error = %v, want nil", err)
	}
	res, err := loop.Run(context.Background(), []provider.Message{textMessage(provider.RoleUser, "hi")})
	if err != nil {
		t.Fatalf("Run() error = %v, want nil", err)
	}
	if tool.callCount() != 1 {
		t.Fatalf("tool.callCount() = %d, want 1: parallel path must still dedup byte-equal calls", tool.callCount())
	}
	if got := toolResultContent(t, res.History, "call-2"); got != agentloop.DuplicateCallNotice {
		t.Fatalf("call-2 content = %q, want DuplicateCallNotice", got)
	}
}

// TestMaxConcurrentToolsParallelAllCallsRun proves the worker pool
// runs every call exactly once even when MaxConcurrentTools >=
// len(calls): a turn with three calls on a pool of size 8 must end
// with three completed tool runs and three RoleTool messages.
func TestMaxConcurrentToolsParallelAllCallsRun(t *testing.T) {
	var runs int64
	tool := &overlapTool{name: "sleeper", runs: &runs}
	reg := tools.New()
	mustAdd(t, reg, tool)
	completer := &scriptedCompleter{responses: []provider.Response{
		toolCallResponse(
			provider.ToolCall{ID: "c1", Name: "sleeper", Index: 0, Arguments: []byte("{}")},
			provider.ToolCall{ID: "c2", Name: "sleeper", Index: 1, Arguments: []byte("{}")},
			provider.ToolCall{ID: "c3", Name: "sleeper", Index: 2, Arguments: []byte("{}")},
		),
		{Message: textMessage(provider.RoleAssistant, "final")},
	}}
	loop, err := agentloop.New(agentloop.Options{
		Completer: completer, Tools: reg, MaxIterations: 5, MaxConcurrentTools: 8,
	})
	if err != nil {
		t.Fatalf("New() error = %v, want nil", err)
	}
	res, err := loop.Run(context.Background(), []provider.Message{textMessage(provider.RoleUser, "hi")})
	if err != nil {
		t.Fatalf("Run() error = %v, want nil", err)
	}
	if got := atomic.LoadInt64(&runs); got != 3 {
		t.Fatalf("runs = %d, want 3", got)
	}
	for _, id := range []string{"c1", "c2", "c3"} {
		if got := toolResultContent(t, res.History, id); got == "" {
			t.Fatalf("history missing RoleTool for %s", id)
		}
	}
}

// TestMaxConcurrentToolsMoreCallsThanWorkers proves that when the turn has more tool
// calls than the worker pool size, all tool calls are processed across worker iterations.
func TestMaxConcurrentToolsMoreCallsThanWorkers(t *testing.T) {
	var runs int64
	tool := &overlapTool{name: "sleeper", runs: &runs}
	reg := tools.New()
	mustAdd(t, reg, tool)
	completer := &scriptedCompleter{responses: []provider.Response{
		toolCallResponse(
			provider.ToolCall{ID: "c1", Name: "sleeper", Index: 0, Arguments: []byte("{}")},
			provider.ToolCall{ID: "c2", Name: "sleeper", Index: 1, Arguments: []byte("{}")},
			provider.ToolCall{ID: "c3", Name: "sleeper", Index: 2, Arguments: []byte("{}")},
			provider.ToolCall{ID: "c4", Name: "sleeper", Index: 3, Arguments: []byte("{}")},
			provider.ToolCall{ID: "c5", Name: "sleeper", Index: 4, Arguments: []byte("{}")},
		),
		{Message: textMessage(provider.RoleAssistant, "final")},
	}}
	loop, err := agentloop.New(agentloop.Options{
		Completer: completer, Tools: reg, MaxIterations: 5, MaxConcurrentTools: 2,
	})
	if err != nil {
		t.Fatalf("New() error = %v, want nil", err)
	}
	res, err := loop.Run(context.Background(), []provider.Message{textMessage(provider.RoleUser, "hi")})
	if err != nil {
		t.Fatalf("Run() error = %v, want nil", err)
	}
	if got := atomic.LoadInt64(&runs); got != 5 {
		t.Fatalf("runs = %d, want 5", got)
	}
	assertIndexOrder(t, historyToolIDs(res), []string{"c1", "c2", "c3", "c4", "c5"})
}
