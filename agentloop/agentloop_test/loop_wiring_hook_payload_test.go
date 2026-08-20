package agentloop_test

import (
	"context"
	"testing"

	"github.com/MiviaLabs/mivia-ai-sdk/agentloop"
	"github.com/MiviaLabs/mivia-ai-sdk/hooks"
	"github.com/MiviaLabs/mivia-ai-sdk/provider"
	"github.com/MiviaLabs/mivia-ai-sdk/tools"
)

// TestRunPreAndPostToolHookPayloadIdentity proves PointPreTool and
// PointPostTool both receive the dispatched provider.ToolCall itself,
// not a stale or zero-value copy. A hook-ordering test alone proves a
// fire happened at the right point; it does not prove the payload
// carried is correct, since a regression that passes the wrong
// provider.ToolCall to fireHook would still satisfy an order-only
// assertion.
func TestRunPreAndPostToolHookPayloadIdentity(t *testing.T) {
	tool := &schemaEchoTool{name: "echo", schema: []byte(`{}`), result: "x"}
	reg := tools.New()
	mustAdd(t, reg, tool)
	want := provider.ToolCall{ID: "call-1", Name: "echo", Arguments: []byte(`{"k":"v"}`)}
	hreg := hooks.New()
	var pre, post provider.ToolCall
	var preOK, postOK bool
	if err := hreg.Add(hooks.PointPreTool, "capture", func(ctx context.Context, payload any) (bool, error) {
		pre, preOK = payload.(provider.ToolCall)
		return true, nil
	}); err != nil {
		t.Fatalf("hooks.Add error = %v, want nil", err)
	}
	if err := hreg.Add(hooks.PointPostTool, "capture", func(ctx context.Context, payload any) (bool, error) {
		post, postOK = payload.(provider.ToolCall)
		return true, nil
	}); err != nil {
		t.Fatalf("hooks.Add error = %v, want nil", err)
	}
	completer := &scriptedCompleter{responses: []provider.Response{
		toolCallResponse(want),
		{Message: textMessage(provider.RoleAssistant, "final")},
	}}
	loop, err := agentloop.New(agentloop.Options{Completer: completer, Tools: reg, MaxIterations: 5, Hooks: hreg})
	if err != nil {
		t.Fatalf("New() error = %v, want nil", err)
	}
	if _, err := loop.Run(context.Background(), []provider.Message{textMessage(provider.RoleUser, "hi")}); err != nil {
		t.Fatalf("Run() error = %v, want nil", err)
	}
	if !preOK || pre.ID != want.ID || pre.Name != want.Name || string(pre.Arguments) != string(want.Arguments) {
		t.Fatalf("PointPreTool payload = %+v, want %+v", pre, want)
	}
	if !postOK || post.ID != want.ID || post.Name != want.Name || string(post.Arguments) != string(want.Arguments) {
		t.Fatalf("PointPostTool payload = %+v, want %+v", post, want)
	}
}

// TestRunPreAndPostToolHookOrderingMultiCallBatch proves hook fire
// order across a batch of more than one tool call in one turn: the
// first call's PointPreTool and PointPostTool both fire before the
// second call's PointPreTool fires. A single-call test cannot pin
// this, since there is no second call to interleave with.
func TestRunPreAndPostToolHookOrderingMultiCallBatch(t *testing.T) {
	tool := &schemaEchoTool{name: "echo", schema: []byte(`{}`), result: "x"}
	reg := tools.New()
	mustAdd(t, reg, tool)
	hreg, order := newOrderRecordingHooks(t, true)
	completer := &scriptedCompleter{responses: []provider.Response{
		toolCallResponse(
			provider.ToolCall{ID: "call-1", Name: "echo", Arguments: []byte("{}")},
			provider.ToolCall{ID: "call-2", Name: "echo", Arguments: []byte("{}")},
		),
		{Message: textMessage(provider.RoleAssistant, "final")},
	}}
	loop, err := agentloop.New(agentloop.Options{Completer: completer, Tools: reg, MaxIterations: 5, Hooks: hreg})
	if err != nil {
		t.Fatalf("New() error = %v, want nil", err)
	}
	if _, err := loop.Run(context.Background(), []provider.Message{textMessage(provider.RoleUser, "hi")}); err != nil {
		t.Fatalf("Run() error = %v, want nil", err)
	}
	want := []string{"pre", "post", "pre", "post"}
	got := *order
	if len(got) != len(want) {
		t.Fatalf("hook fire order = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("hook fire order = %v, want %v", got, want)
		}
	}
}

// TestRunPreAndPostToolHookOrderingMultiCallBatchMidVeto proves a
// veto on the second of three calls in one batch stops the batch
// immediately: the first call's PointPreTool/PointPostTool both fire,
// the second call's PointPreTool fires and vetoes with no
// PointPostTool, and the third call's PointPreTool never fires.
func TestRunPreAndPostToolHookOrderingMultiCallBatchMidVeto(t *testing.T) {
	tool := &schemaEchoTool{name: "echo", schema: []byte(`{}`), result: "x"}
	reg := tools.New()
	mustAdd(t, reg, tool)
	order := &[]string{}
	hreg := hooks.New()
	vetoNext := false
	if err := hreg.Add(hooks.PointPreTool, "record", func(ctx context.Context, payload any) (bool, error) {
		*order = append(*order, "pre")
		allow := !vetoNext
		vetoNext = true
		return allow, nil
	}); err != nil {
		t.Fatalf("hooks.Add error = %v, want nil", err)
	}
	if err := hreg.Add(hooks.PointPostTool, "record", func(ctx context.Context, payload any) (bool, error) {
		*order = append(*order, "post")
		return true, nil
	}); err != nil {
		t.Fatalf("hooks.Add error = %v, want nil", err)
	}
	completer := &scriptedCompleter{responses: []provider.Response{
		toolCallResponse(
			provider.ToolCall{ID: "call-1", Name: "echo", Arguments: []byte("{}")},
			provider.ToolCall{ID: "call-2", Name: "echo", Arguments: []byte("{}")},
			provider.ToolCall{ID: "call-3", Name: "echo", Arguments: []byte("{}")},
		),
	}}
	loop, err := agentloop.New(agentloop.Options{Completer: completer, Tools: reg, MaxIterations: 5, Hooks: hreg})
	if err != nil {
		t.Fatalf("New() error = %v, want nil", err)
	}
	res, err := loop.Run(context.Background(), []provider.Message{textMessage(provider.RoleUser, "hi")})
	if err != nil {
		t.Fatalf("Run() error = %v, want nil", err)
	}
	if res.Stop != agentloop.StopHookVeto {
		t.Fatalf("Stop = %v, want StopHookVeto", res.Stop)
	}
	want := []string{"pre", "post", "pre"}
	got := *order
	if len(got) != len(want) {
		t.Fatalf("hook fire order = %v, want %v: the third call's PointPreTool must never fire", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("hook fire order = %v, want %v", got, want)
		}
	}
}
