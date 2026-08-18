package subagent_test

import (
	"context"
	"strings"
	"testing"

	"github.com/MiviaLabs/mivia-ai-sdk/channel"
	"github.com/MiviaLabs/mivia-ai-sdk/provider"
	"github.com/MiviaLabs/mivia-ai-sdk/subagent"
	"github.com/MiviaLabs/mivia-ai-sdk/trigger"
)

// stubCompleter answers every chat turn with one fixed reply.
type stubCompleter struct{ reply string }

// Name labels the stub provider.
func (s stubCompleter) Name() string { return "stub" }

// Chat returns the fixed reply.
func (s stubCompleter) Chat(ctx context.Context, req provider.Request) (provider.Response, error) {
	return provider.Response{
		Model:   "stub",
		Message: provider.Message{Role: provider.RoleAssistant, Content: s.reply},
	}, nil
}

// ChatStream is never called: the tool runs a non-streaming turn.
func (s stubCompleter) ChatStream(ctx context.Context, req provider.Request) (<-chan provider.Chunk, error) {
	return nil, nil
}

// TestProviderToolRunsOneTurn proves the prompt crosses to the
// Completer and the reply content crosses back.
func TestProviderToolRunsOneTurn(t *testing.T) {
	ctx := context.Background()
	tool := subagent.ProviderTool("model", stubCompleter{reply: "model says yes"})
	out, err := tool.Run(ctx, inString("should we ship"))
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if out.Value != "model says yes" {
		t.Fatalf("reply = %v, want the stub reply", out.Value)
	}
}

// answeringAsk approves every question and echoes a payload.
func answeringAsk(payload string) channel.Notifier {
	return func(ctx context.Context, q channel.Question) (channel.Answer, error) {
		return channel.Answer{QuestionID: q.ID, Approved: true, Payload: payload}, nil
	}
}

// decliningAsk refuses every question.
func decliningAsk() channel.Notifier {
	return func(ctx context.Context, q channel.Question) (channel.Answer, error) {
		return channel.Answer{QuestionID: q.ID, Approved: false}, nil
	}
}

// TestChannelToolAsksHuman proves an approved answer's payload
// becomes the tool result, and the question carries the payload.
func TestChannelToolAsksHuman(t *testing.T) {
	ctx := context.Background()
	var got channel.Question
	ask := func(ctx context.Context, q channel.Question) (channel.Answer, error) {
		got = q
		return channel.Answer{QuestionID: q.ID, Approved: true, Payload: "human says go"}, nil
	}
	tool := subagent.ChannelTool("ask", ask, "human-1")
	out, err := tool.Run(ctx, inString("approve the deploy"))
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if out.Value != "human says go" {
		t.Fatalf("answer = %v, want the human payload", out.Value)
	}
	if got.Recipient != "human-1" || got.Payload != "approve the deploy" {
		t.Fatalf("question = %+v, want the payload for human-1", got)
	}
}

// TestChannelToolDeclineFails proves a declined question fails the
// call naming the recipient.
func TestChannelToolDeclineFails(t *testing.T) {
	ctx := context.Background()
	tool := subagent.ChannelTool("ask", decliningAsk(), "human-2")
	_, err := tool.Run(ctx, inString("approve the deploy"))
	if err == nil || !strings.Contains(err.Error(), "human-2") {
		t.Fatalf("err = %v, want a decline naming human-2", err)
	}
}

// TestTriggerToolFires proves the named trigger's action runs and the
// tool reports fired.
func TestTriggerToolFires(t *testing.T) {
	ctx := context.Background()
	reg := trigger.New()
	fired := false
	cond := func(context.Context) (bool, error) { return true, nil }
	act := func(context.Context) error { fired = true; return nil }
	if err := reg.Add("deploy-done", cond, act); err != nil {
		t.Fatalf("Add: %v", err)
	}
	tool := subagent.TriggerTool("triggers", reg)
	out, err := tool.Run(ctx, inString("deploy-done"))
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if out.Value != "fired" || !fired {
		t.Fatalf("result = %v, fired = %v, want fired and run", out.Value, fired)
	}
}
