package agentloop_test

import (
	"context"
	"testing"

	"github.com/MiviaLabs/mivia-ai-sdk/agentloop"
	"github.com/MiviaLabs/mivia-ai-sdk/provider"
	"github.com/MiviaLabs/mivia-ai-sdk/tools"
)

// lastMessageContent returns the content of msgs' last element, or the
// empty string when msgs is empty.
func lastMessageContent(msgs []provider.Message) string {
	if len(msgs) == 0 {
		return ""
	}
	return msgs[len(msgs)-1].Content
}

// countNoticeMessages counts msgs entries whose content equals notice.
func countNoticeMessages(msgs []provider.Message, notice string) int {
	n := 0
	for _, m := range msgs {
		if m.Content == notice {
			n++
		}
	}
	return n
}

// TestRunConcludeMarginZeroUnchanged proves a zero ConcludeMargin, the
// default, leaves StopMaxIterations behavior unchanged: no notice ever
// appends, and Run stops at the limit exactly as the base plan
// describes.
func TestRunConcludeMarginZeroUnchanged(t *testing.T) {
	tool := &schemaEchoTool{name: "echo", schema: []byte(`{}`), result: "x"}
	reg := tools.New()
	mustAdd(t, reg, tool)
	completer := &scriptedCompleter{responses: []provider.Response{
		toolCallResponse(provider.ToolCall{ID: "call-1", Name: "echo", Arguments: []byte("{}")}),
		toolCallResponse(provider.ToolCall{ID: "call-2", Name: "echo", Arguments: []byte("{}")}),
		toolCallResponse(provider.ToolCall{ID: "call-3", Name: "echo", Arguments: []byte("{}")}),
	}}
	loop, err := agentloop.New(agentloop.Options{
		Completer: completer, Tools: reg, MaxIterations: 3, ConcludeMargin: 0,
	})
	if err != nil {
		t.Fatalf("New() error = %v, want nil", err)
	}
	res, err := loop.Run(context.Background(), []provider.Message{textMessage(provider.RoleUser, "hi")})
	if err != nil {
		t.Fatalf("Run() error = %v, want nil", err)
	}
	if res.Stop != agentloop.StopMaxIterations {
		t.Fatalf("Stop = %v, want StopMaxIterations", res.Stop)
	}
	for i, req := range []provider.Request{completer.requestAt(0), completer.requestAt(1), completer.requestAt(2)} {
		if lastMessageContent(req.Messages) == agentloop.DefaultConcludeNotice {
			t.Fatalf("request %d carries the notice, want none: ConcludeMargin is disabled", i)
		}
	}
}

// TestRunConcludeMarginNudgesNextToLastCall proves the worked table's
// ConcludeMargin=2 row: MaxIterations=5, the first qualifying k is 4.
// A model that returns no tool call on the nudged call stops at
// StopConcluded, and the request carries the notice as the last
// message.
func TestRunConcludeMarginNudgesNextToLastCall(t *testing.T) {
	tool := &schemaEchoTool{name: "echo", schema: []byte(`{}`), result: "x"}
	reg := tools.New()
	mustAdd(t, reg, tool)
	completer := &scriptedCompleter{responses: []provider.Response{
		toolCallResponse(provider.ToolCall{ID: "call-1", Name: "echo", Arguments: []byte("{}")}),
		toolCallResponse(provider.ToolCall{ID: "call-2", Name: "echo", Arguments: []byte("{}")}),
		toolCallResponse(provider.ToolCall{ID: "call-3", Name: "echo", Arguments: []byte("{}")}),
		{Message: textMessage(provider.RoleAssistant, "final")},
	}}
	loop, err := agentloop.New(agentloop.Options{
		Completer: completer, Tools: reg, MaxIterations: 5, ConcludeMargin: 2,
	})
	if err != nil {
		t.Fatalf("New() error = %v, want nil", err)
	}
	res, err := loop.Run(context.Background(), []provider.Message{textMessage(provider.RoleUser, "hi")})
	if err != nil {
		t.Fatalf("Run() error = %v, want nil", err)
	}
	if res.Stop != agentloop.StopConcluded {
		t.Fatalf("Stop = %v, want StopConcluded", res.Stop)
	}
	if res.Iterations != 4 {
		t.Fatalf("Iterations = %d, want 4", res.Iterations)
	}
	// Call k=4 is requestAt(3) (0-based): the first qualifying call.
	req4 := completer.requestAt(3)
	if lastMessageContent(req4.Messages) != agentloop.DefaultConcludeNotice {
		t.Fatalf("request 4 last message = %q, want DefaultConcludeNotice", lastMessageContent(req4.Messages))
	}
	// Call k=3 must carry no notice: 5-3=2 is not < 2.
	req3 := completer.requestAt(2)
	if lastMessageContent(req3.Messages) == agentloop.DefaultConcludeNotice {
		t.Fatalf("request 3 carries the notice, want none: k=3 does not qualify")
	}
}

// TestRunConcludeMarginNudgesLastCallOnly proves the worked table's
// ConcludeMargin=1 row: MaxIterations=5, the only qualifying k is 5.
// A model that keeps calling tools through k=4 sees no notice on that
// call, only on the last allowed call.
func TestRunConcludeMarginNudgesLastCallOnly(t *testing.T) {
	tool := &schemaEchoTool{name: "echo", schema: []byte(`{}`), result: "x"}
	reg := tools.New()
	mustAdd(t, reg, tool)
	completer := &scriptedCompleter{responses: []provider.Response{
		toolCallResponse(provider.ToolCall{ID: "call-1", Name: "echo", Arguments: []byte("{}")}),
		toolCallResponse(provider.ToolCall{ID: "call-2", Name: "echo", Arguments: []byte("{}")}),
		toolCallResponse(provider.ToolCall{ID: "call-3", Name: "echo", Arguments: []byte("{}")}),
		toolCallResponse(provider.ToolCall{ID: "call-4", Name: "echo", Arguments: []byte("{}")}),
		{Message: textMessage(provider.RoleAssistant, "final")},
	}}
	loop, err := agentloop.New(agentloop.Options{
		Completer: completer, Tools: reg, MaxIterations: 5, ConcludeMargin: 1,
	})
	if err != nil {
		t.Fatalf("New() error = %v, want nil", err)
	}
	res, err := loop.Run(context.Background(), []provider.Message{textMessage(provider.RoleUser, "hi")})
	if err != nil {
		t.Fatalf("Run() error = %v, want nil", err)
	}
	if res.Stop != agentloop.StopConcluded {
		t.Fatalf("Stop = %v, want StopConcluded", res.Stop)
	}
	if res.Iterations != 5 {
		t.Fatalf("Iterations = %d, want 5", res.Iterations)
	}
	req4 := completer.requestAt(3)
	if lastMessageContent(req4.Messages) == agentloop.DefaultConcludeNotice {
		t.Fatalf("request 4 carries the notice, want none: k=4 does not qualify when ConcludeMargin=1")
	}
	req5 := completer.requestAt(4)
	if lastMessageContent(req5.Messages) != agentloop.DefaultConcludeNotice {
		t.Fatalf("request 5 last message = %q, want DefaultConcludeNotice", lastMessageContent(req5.Messages))
	}
}

// TestRunConcludeMarginNoNudgeBeforeThreshold proves an early
// StopNoToolCalls, strictly before the first qualifying k, stops at
// StopNoToolCalls, not StopConcluded, and carries no notice: the
// boundary between an early, non-nudged stop and a nudged one.
func TestRunConcludeMarginNoNudgeBeforeThreshold(t *testing.T) {
	reg := tools.New()
	completer := &scriptedCompleter{responses: []provider.Response{
		{Message: textMessage(provider.RoleAssistant, "final")},
	}}
	loop, err := agentloop.New(agentloop.Options{
		Completer: completer, Tools: reg, MaxIterations: 5, ConcludeMargin: 2,
	})
	if err != nil {
		t.Fatalf("New() error = %v, want nil", err)
	}
	res, err := loop.Run(context.Background(), []provider.Message{textMessage(provider.RoleUser, "hi")})
	if err != nil {
		t.Fatalf("Run() error = %v, want nil", err)
	}
	if res.Stop != agentloop.StopNoToolCalls {
		t.Fatalf("Stop = %v, want StopNoToolCalls", res.Stop)
	}
	req1 := completer.requestAt(0)
	if lastMessageContent(req1.Messages) == agentloop.DefaultConcludeNotice {
		t.Fatalf("request 1 carries the notice, want none: k=1 does not qualify (5-1=4 is not < 2)")
	}
}

// TestRunConcludeMarginFiresOnFirstIteration proves the doc comment's
// claim that a ConcludeMargin greater than or equal to MaxIterations
// fires the nudge on Run's first iteration: MaxIterations=1,
// ConcludeMargin=1 satisfies k=1 immediately.
func TestRunConcludeMarginFiresOnFirstIteration(t *testing.T) {
	reg := tools.New()
	completer := &scriptedCompleter{responses: []provider.Response{
		{Message: textMessage(provider.RoleAssistant, "final")},
	}}
	loop, err := agentloop.New(agentloop.Options{
		Completer: completer, Tools: reg, MaxIterations: 1, ConcludeMargin: 1,
	})
	if err != nil {
		t.Fatalf("New() error = %v, want nil", err)
	}
	res, err := loop.Run(context.Background(), []provider.Message{textMessage(provider.RoleUser, "hi")})
	if err != nil {
		t.Fatalf("Run() error = %v, want nil", err)
	}
	req1 := completer.requestAt(0)
	if lastMessageContent(req1.Messages) != agentloop.DefaultConcludeNotice {
		t.Fatalf("request 1 last message = %q, want DefaultConcludeNotice", lastMessageContent(req1.Messages))
	}
	if res.Stop != agentloop.StopConcluded {
		t.Fatalf("Stop = %v, want StopConcluded", res.Stop)
	}
	if res.Iterations != 1 {
		t.Fatalf("Iterations = %d, want 1", res.Iterations)
	}
}

// TestRunConcludeMarginModelIgnoresNudge proves a model that keeps
// requesting tool calls through the nudged iteration, and every
// iteration after, still stops at StopMaxIterations once the limit is
// reached: the nudge cannot force a text-only reply.
func TestRunConcludeMarginModelIgnoresNudge(t *testing.T) {
	tool := &schemaEchoTool{name: "echo", schema: []byte(`{}`), result: "x"}
	reg := tools.New()
	mustAdd(t, reg, tool)
	completer := &scriptedCompleter{responses: []provider.Response{
		toolCallResponse(provider.ToolCall{ID: "call-1", Name: "echo", Arguments: []byte("{}")}),
		toolCallResponse(provider.ToolCall{ID: "call-2", Name: "echo", Arguments: []byte("{}")}),
		toolCallResponse(provider.ToolCall{ID: "call-3", Name: "echo", Arguments: []byte("{}")}),
	}}
	loop, err := agentloop.New(agentloop.Options{
		Completer: completer, Tools: reg, MaxIterations: 3, ConcludeMargin: 2,
	})
	if err != nil {
		t.Fatalf("New() error = %v, want nil", err)
	}
	res, err := loop.Run(context.Background(), []provider.Message{textMessage(provider.RoleUser, "hi")})
	if err != nil {
		t.Fatalf("Run() error = %v, want nil", err)
	}
	if res.Stop != agentloop.StopMaxIterations {
		t.Fatalf("Stop = %v, want StopMaxIterations", res.Stop)
	}
	if res.Iterations != 3 {
		t.Fatalf("Iterations = %d, want 3", res.Iterations)
	}
}

// TestRunConcludeNoticeDefaultAndOverride proves an empty
// ConcludeNotice sends DefaultConcludeNotice, and a caller-set
// ConcludeNotice sends that text instead.
func TestRunConcludeNoticeDefaultAndOverride(t *testing.T) {
	cases := []struct {
		name       string
		notice     string
		wantNotice string
	}{
		{"empty uses default", "", agentloop.DefaultConcludeNotice},
		{"custom notice used verbatim", "wrap it up now", "wrap it up now"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			reg := tools.New()
			completer := &scriptedCompleter{responses: []provider.Response{
				{Message: textMessage(provider.RoleAssistant, "final")},
			}}
			loop, err := agentloop.New(agentloop.Options{
				Completer: completer, Tools: reg, MaxIterations: 1, ConcludeMargin: 1, ConcludeNotice: c.notice,
			})
			if err != nil {
				t.Fatalf("New() error = %v, want nil", err)
			}
			_, err = loop.Run(context.Background(), []provider.Message{textMessage(provider.RoleUser, "hi")})
			if err != nil {
				t.Fatalf("Run() error = %v, want nil", err)
			}
			req := completer.requestAt(0)
			if lastMessageContent(req.Messages) != c.wantNotice {
				t.Fatalf("last message = %q, want %q", lastMessageContent(req.Messages), c.wantNotice)
			}
		})
	}
}

// TestRunConcludeMarginNoticeSentOnce proves the notice appends
// exactly once across a multi-iteration run, even when several
// iterations pass while inside the margin: ConcludeMargin=2,
// MaxIterations=5, k=4 and k=5 both qualify, but only k=4 carries a
// freshly appended notice.
func TestRunConcludeMarginNoticeSentOnce(t *testing.T) {
	tool := &schemaEchoTool{name: "echo", schema: []byte(`{}`), result: "x"}
	reg := tools.New()
	mustAdd(t, reg, tool)
	completer := &scriptedCompleter{responses: []provider.Response{
		toolCallResponse(provider.ToolCall{ID: "call-1", Name: "echo", Arguments: []byte("{}")}),
		toolCallResponse(provider.ToolCall{ID: "call-2", Name: "echo", Arguments: []byte("{}")}),
		toolCallResponse(provider.ToolCall{ID: "call-3", Name: "echo", Arguments: []byte("{}")}),
		toolCallResponse(provider.ToolCall{ID: "call-4", Name: "echo", Arguments: []byte("{}")}),
		{Message: textMessage(provider.RoleAssistant, "final")},
	}}
	loop, err := agentloop.New(agentloop.Options{
		Completer: completer, Tools: reg, MaxIterations: 5, ConcludeMargin: 2,
	})
	if err != nil {
		t.Fatalf("New() error = %v, want nil", err)
	}
	res, err := loop.Run(context.Background(), []provider.Message{textMessage(provider.RoleUser, "hi")})
	if err != nil {
		t.Fatalf("Run() error = %v, want nil", err)
	}
	if res.Stop != agentloop.StopConcluded {
		t.Fatalf("Stop = %v, want StopConcluded", res.Stop)
	}
	if n := countNoticeMessages(res.History, agentloop.DefaultConcludeNotice); n != 1 {
		t.Fatalf("notice count in final history = %d, want 1", n)
	}
}

// TestRunConcludeMarginTrimDropsNotice proves a Trim hook that drops
// every RoleUser message, run with ConcludeMargin set, strips the
// notice before the next Completer call sees it: Run still reaches
// StopMaxIterations, since the model was never actually nudged. This
// is a documented, accepted limit, not a guarantee this phase makes.
func TestRunConcludeMarginTrimDropsNotice(t *testing.T) {
	tool := &schemaEchoTool{name: "echo", schema: []byte(`{}`), result: "x"}
	reg := tools.New()
	mustAdd(t, reg, tool)
	completer := &scriptedCompleter{responses: []provider.Response{
		toolCallResponse(provider.ToolCall{ID: "call-1", Name: "echo", Arguments: []byte("{}")}),
		toolCallResponse(provider.ToolCall{ID: "call-2", Name: "echo", Arguments: []byte("{}")}),
		toolCallResponse(provider.ToolCall{ID: "call-3", Name: "echo", Arguments: []byte("{}")}),
	}}
	dropRoleUser := func(ctx context.Context, msgs []provider.Message) ([]provider.Message, error) {
		kept := make([]provider.Message, 0, len(msgs))
		for _, m := range msgs {
			if m.Role == provider.RoleUser {
				continue
			}
			kept = append(kept, m)
		}
		return kept, nil
	}
	loop, err := agentloop.New(agentloop.Options{
		Completer: completer, Tools: reg, MaxIterations: 3, ConcludeMargin: 3, Trim: dropRoleUser,
	})
	if err != nil {
		t.Fatalf("New() error = %v, want nil", err)
	}
	res, err := loop.Run(context.Background(), []provider.Message{textMessage(provider.RoleUser, "hi")})
	if err != nil {
		t.Fatalf("Run() error = %v, want nil", err)
	}
	if res.Stop != agentloop.StopMaxIterations {
		t.Fatalf("Stop = %v, want StopMaxIterations: Trim strips the notice before the model ever sees it stick", res.Stop)
	}
	if res.Iterations != 3 {
		t.Fatalf("Iterations = %d, want 3", res.Iterations)
	}
	for _, m := range res.History {
		if m.Content == agentloop.DefaultConcludeNotice {
			t.Fatalf("final history still carries the notice, want it trimmed away")
		}
	}
	// ConcludeMargin=3 qualifies at k=1 (3-1=2 < 3): Trim runs before
	// the append each iteration, so the notice reaches call 1's
	// request before the following iteration's Trim strips it away.
	req1 := completer.requestAt(0)
	if lastMessageContent(req1.Messages) != agentloop.DefaultConcludeNotice {
		t.Fatalf("request 1 last message = %q, want DefaultConcludeNotice: it was appended after that iteration's Trim ran", lastMessageContent(req1.Messages))
	}
}

// TestRunConcludeMarginTrimDropsNoticeBeforeStop proves the causal
// fix: StopConcluded requires the notice present in the request the
// model actually saw on the iteration it stopped, not merely that the
// notice was appended at some earlier iteration. ConcludeMargin=2,
// MaxIterations=3: the notice appends before iteration 2's call
// (k=2, 3-2=1<2), the model still calls a tool, and iteration 3's
// Trim strips the notice out of history before that call runs. The
// model returns no tool call at iteration 3, having never seen the
// notice on that call, so Run must stop at StopNoToolCalls, not
// StopConcluded.
func TestRunConcludeMarginTrimDropsNoticeBeforeStop(t *testing.T) {
	tool := &schemaEchoTool{name: "echo", schema: []byte(`{}`), result: "x"}
	reg := tools.New()
	mustAdd(t, reg, tool)
	completer := &scriptedCompleter{responses: []provider.Response{
		toolCallResponse(provider.ToolCall{ID: "call-1", Name: "echo", Arguments: []byte("{}")}),
		toolCallResponse(provider.ToolCall{ID: "call-2", Name: "echo", Arguments: []byte("{}")}),
		{Message: textMessage(provider.RoleAssistant, "final")},
	}}
	dropRoleUser := func(ctx context.Context, msgs []provider.Message) ([]provider.Message, error) {
		kept := make([]provider.Message, 0, len(msgs))
		for _, m := range msgs {
			if m.Role == provider.RoleUser {
				continue
			}
			kept = append(kept, m)
		}
		return kept, nil
	}
	loop, err := agentloop.New(agentloop.Options{
		Completer: completer, Tools: reg, MaxIterations: 3, ConcludeMargin: 2, Trim: dropRoleUser,
	})
	if err != nil {
		t.Fatalf("New() error = %v, want nil", err)
	}
	res, err := loop.Run(context.Background(), []provider.Message{textMessage(provider.RoleUser, "hi")})
	if err != nil {
		t.Fatalf("Run() error = %v, want nil", err)
	}
	// Iteration 2 (k=2) is requestAt(1): the first qualifying call,
	// notice appended after that iteration's Trim ran.
	req2 := completer.requestAt(1)
	if lastMessageContent(req2.Messages) != agentloop.DefaultConcludeNotice {
		t.Fatalf("request 2 last message = %q, want DefaultConcludeNotice", lastMessageContent(req2.Messages))
	}
	// Iteration 3 is requestAt(2): the following iteration's Trim
	// strips the notice before this call runs.
	req3 := completer.requestAt(2)
	if lastMessageContent(req3.Messages) == agentloop.DefaultConcludeNotice {
		t.Fatalf("request 3 carries the notice, want it stripped by Trim before this call")
	}
	if res.Stop != agentloop.StopNoToolCalls {
		t.Fatalf("Stop = %v, want StopNoToolCalls: the model never saw the notice on the iteration it stopped", res.Stop)
	}
	if res.Iterations != 3 {
		t.Fatalf("Iterations = %d, want 3", res.Iterations)
	}
}
