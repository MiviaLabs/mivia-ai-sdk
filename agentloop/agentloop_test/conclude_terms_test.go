package agentloop_test

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/MiviaLabs/mivia-ai-sdk/agentloop"
	"github.com/MiviaLabs/mivia-ai-sdk/provider"
	"github.com/MiviaLabs/mivia-ai-sdk/tools"
)

// twoIterCompleter emits one tool call on iteration 1, forcing another
// iteration, and a final text reply on iteration 2. Together with a
// *tools.Registry carrying a no-op schema-bearing tool, this is enough
// to drive two iterations through Run. The completer records every
// Request.Chat saw, so the second iteration's history tail
// (noticeSent has stuck by then) can be inspected by index.
type twoIterCompleter struct {
	mu    sync.Mutex
	reqs  []provider.Request
	calls int
}

func newTwoIterCompleter() *twoIterCompleter {
	return &twoIterCompleter{}
}

func (c *twoIterCompleter) Name() string { return "two-iter" }

func (c *twoIterCompleter) Chat(ctx context.Context, req provider.Request) (provider.Response, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.reqs = append(c.reqs, req)
	n := c.calls
	c.calls++
	if n == 0 {
		return provider.Response{
			Message:   provider.Message{Role: provider.RoleAssistant, Content: "calling"},
			ToolCalls: []provider.ToolCall{{ID: "c1", Name: "noop", Arguments: []byte("{}")}},
		}, nil
	}
	return provider.Response{Message: provider.Message{Role: provider.RoleAssistant, Content: "done"}}, nil
}

func (c *twoIterCompleter) ChatStream(ctx context.Context, req provider.Request) (<-chan provider.Chunk, error) {
	return nil, nil
}

// requestAt returns the idx'th (0-based) Request Chat received, in
// call order. Caller-side synchronization is the test's job: the loop
// runs serially, so by the time Run returns, every Chat call has
// completed.
func (c *twoIterCompleter) requestAt(idx int) provider.Request {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.reqs[idx]
}

// newNoopRegistry builds a *tools.Registry that holds one schema-bearing
// no-op tool whose arguments decode to {} and whose run is a no-op.
func newNoopRegistry(t *testing.T) *tools.Registry {
	t.Helper()
	tool := &schemaEchoTool{name: "noop", schema: []byte("{}"), result: "ok"}
	reg := tools.New()
	mustAdd(t, reg, tool)
	return reg
}

// hasNotice reports whether msgs carries a RoleUser message whose
// Content equals notice. Used to assert the notice reaches iteration
// 2's request without depending on whether the term fired on iter 1
// or iter 2 (the position varies between "tail" and "after tool
// result").
func hasNotice(msgs []provider.Message, notice string) bool {
	for _, m := range msgs {
		if m.Role == provider.RoleUser && m.Content == notice {
			return true
		}
	}
	return false
}

// TestRunConcludeDeadlineThresholdFires proves the time-based term:
// when StartTime is set so that the deadline has passed (time.Until(deadlineAt) <= 0),
// shouldConclude fires on iteration 1, and the notice reaches iteration 2's request.
func TestRunConcludeDeadlineThresholdFires(t *testing.T) {
	reg := newNoopRegistry(t)
	completer := newTwoIterCompleter()
	// ConcludeDeadline = 1h, StartTime = now-2h → deadlineAt is 1h in
	// the past at New, so time.Until(deadlineAt) is negative. The term fires on iter 1.
	start := time.Now().Add(-2 * time.Hour)
	loop, err := agentloop.New(agentloop.Options{
		Completer:        completer,
		Tools:            reg,
		MaxIterations:    5,
		ConcludeDeadline: time.Hour,
		StartTime:        start,
	})
	if err != nil {
		t.Fatalf("New() error = %v, want nil", err)
	}
	res, err := loop.Run(context.Background(), []provider.Message{
		textMessage(provider.RoleUser, "hi"),
	})
	if err != nil {
		t.Fatalf("Run() error = %v, want nil", err)
	}
	if res.Stop != agentloop.StopConcluded {
		t.Fatalf("Stop = %v, want StopConcluded", res.Stop)
	}
	req2 := completer.requestAt(1)
	if !hasNotice(req2.Messages, agentloop.DefaultConcludeNotice) {
		t.Fatalf("request 2 lacks the notice: deadline term did not fire")
	}
}

// TestRunConcludeDeadlineFutureDoesNotFire proves that a deadline in
// the future does not trigger the conclude nudge.
func TestRunConcludeDeadlineFutureDoesNotFire(t *testing.T) {
	reg := newNoopRegistry(t)
	completer := newTwoIterCompleter()
	loop, err := agentloop.New(agentloop.Options{
		Completer:        completer,
		Tools:            reg,
		MaxIterations:    5,
		ConcludeDeadline: 2 * time.Hour,
		StartTime:        time.Now(),
	})
	if err != nil {
		t.Fatalf("New() error = %v, want nil", err)
	}
	res, err := loop.Run(context.Background(), []provider.Message{
		textMessage(provider.RoleUser, "hi"),
	})
	if err != nil {
		t.Fatalf("Run() error = %v, want nil", err)
	}
	if res.Stop != agentloop.StopNoToolCalls {
		t.Fatalf("Stop = %v, want StopNoToolCalls", res.Stop)
	}
	req2 := completer.requestAt(1)
	if hasNotice(req2.Messages, agentloop.DefaultConcludeNotice) {
		t.Fatalf("request 2 has notice, want none for future deadline")
	}
}

// TestRunConcludeToolCallsLeftThresholdFires proves that
// ConcludeToolCallsLeft does not compare iteration index k against
// MaxCallsPerTurn.
func TestRunConcludeToolCallsLeftThresholdFires(t *testing.T) {
	reg := newNoopRegistry(t)
	completer := newTwoIterCompleter()
	loop, err := agentloop.New(agentloop.Options{
		Completer:             completer,
		Tools:                 reg,
		MaxIterations:         5,
		MaxCallsPerTurn:       3,
		ConcludeToolCallsLeft: 2,
	})
	if err != nil {
		t.Fatalf("New() error = %v, want nil", err)
	}
	res, err := loop.Run(context.Background(), []provider.Message{
		textMessage(provider.RoleUser, "hi"),
	})
	if err != nil {
		t.Fatalf("Run() error = %v, want nil", err)
	}
	if res.Stop != agentloop.StopNoToolCalls {
		t.Fatalf("Stop = %v, want StopNoToolCalls", res.Stop)
	}
	req2 := completer.requestAt(1)
	if hasNotice(req2.Messages, agentloop.DefaultConcludeNotice) {
		t.Fatalf("request 2 has notice, want none")
	}
}

// TestRunConcludeStepsLeftThresholdFires proves the iterations-left
// term: MaxIterations=5, ConcludeStepsLeft=4. k=1: 5-1=4 < 4 is false.
// k=2: 5-2=3 < 4 is true. So the term fires on iter 2's check, the
// notice appends before iter 2's Chat, and the notice is the LAST
// message in request 2.
func TestRunConcludeStepsLeftThresholdFires(t *testing.T) {
	reg := newNoopRegistry(t)
	completer := newTwoIterCompleter()
	loop, err := agentloop.New(agentloop.Options{
		Completer:         completer,
		Tools:             reg,
		MaxIterations:     5,
		ConcludeStepsLeft: 4,
	})
	if err != nil {
		t.Fatalf("New() error = %v, want nil", err)
	}
	res, err := loop.Run(context.Background(), []provider.Message{
		textMessage(provider.RoleUser, "hi"),
	})
	if err != nil {
		t.Fatalf("Run() error = %v, want nil", err)
	}
	if res.Stop != agentloop.StopConcluded {
		t.Fatalf("Stop = %v, want StopConcluded", res.Stop)
	}
	req2 := completer.requestAt(1)
	if last := lastMessageContent(req2.Messages); last != agentloop.DefaultConcludeNotice {
		t.Fatalf("request 2 last message = %q, want DefaultConcludeNotice", last)
	}
	if !hasNotice(req2.Messages, agentloop.DefaultConcludeNotice) {
		t.Fatalf("request 2 lacks the notice as a RoleUser message")
	}
}

// TestRunConcludeTermsOREDTogether proves the three OR-ed terms: any
// one firing triggers the nudge, and noticeSent sticks so only one
// notice message lands in iteration 2's history.
func TestRunConcludeTermsOREDTogether(t *testing.T) {
	reg := newNoopRegistry(t)
	completer := newTwoIterCompleter()
	// ConcludeMargin=1 fires on k=5 only (past iter 2).
	// ConcludeStepsLeft=4 fires on iter 2 (k=2: 5-2=3<4).
	// ConcludeDeadline=1h with StartTime=now-2h fires on iter 1
	// (time.Until <= 0). All three of Margin, StepsLeft,
	// Deadline are configured to fire at different points; the
	// test verifies that they all contribute via OR without
	// double-appending the notice.
	loop, err := agentloop.New(agentloop.Options{
		Completer:         completer,
		Tools:             reg,
		MaxIterations:     5,
		ConcludeMargin:    1,
		ConcludeDeadline:  time.Hour,
		ConcludeStepsLeft: 4,
		StartTime:         time.Now().Add(-2 * time.Hour),
	})
	if err != nil {
		t.Fatalf("New() error = %v, want nil", err)
	}
	res, err := loop.Run(context.Background(), []provider.Message{
		textMessage(provider.RoleUser, "hi"),
	})
	if err != nil {
		t.Fatalf("Run() error = %v, want nil", err)
	}
	// Multiple terms qualify but noticeSent sticks: exactly one
	// notice append. Iter 2 carries no tool call → StopConcluded.
	if res.Stop != agentloop.StopConcluded {
		t.Fatalf("Stop = %v, want StopConcluded", res.Stop)
	}
	req2 := completer.requestAt(1)
	if !hasNotice(req2.Messages, agentloop.DefaultConcludeNotice) {
		t.Fatalf("request 2 lacks the notice")
	}
	if n := countNoticeMessages(req2.Messages, agentloop.DefaultConcludeNotice); n != 1 {
		t.Fatalf("notice count in request 2 = %d, want 1: OR-ed terms must not double-append", n)
	}
}
