package agentloop_test

import (
	"context"
	"strings"
	"testing"

	"github.com/MiviaLabs/mivia-ai-sdk/agentloop"
	"github.com/MiviaLabs/mivia-ai-sdk/provider"
	"github.com/MiviaLabs/mivia-ai-sdk/tools"
)

// TestUnknownToolNameErrorNamesTheCallAndValidTools is the regression for a
// production stall: a model that calls a tool name absent from the registry
// (confabulated, prefixed, misremembered - whatever the cause) got back an
// error whose text carried only the opaque call ID and tools.ErrUnknownName's
// static message ("tools: unknown tool name") - never the attempted name,
// never a hint of what WAS available. The model had nothing to correct
// against, so it kept guessing variants until the run's step budget was
// exhausted, minutes later, with the task still reporting "running".
//
// The error content the model receives (the RoleTool history entry) must
// name the exact string it called AND list the tool names actually offered,
// so a single retry has a chance of succeeding.
func TestUnknownToolNameErrorNamesTheCallAndValidTools(t *testing.T) {
	echo := &schemaEchoTool{name: "echo", schema: []byte(`{}`), result: "x"}
	glob := &schemaEchoTool{name: "glob", schema: []byte(`{}`), result: "y"}
	reg := tools.New()
	mustAdd(t, reg, echo)
	mustAdd(t, reg, glob)

	completer := &scriptedCompleter{responses: []provider.Response{
		toolCallResponse(provider.ToolCall{ID: "call-1", Name: "spread_glob", Arguments: []byte("{}")}),
		{Message: textMessage(provider.RoleAssistant, "final")},
	}}
	loop, err := agentloop.New(agentloop.Options{Completer: completer, Tools: reg, MaxIterations: 5})
	if err != nil {
		t.Fatalf("New() error = %v, want nil", err)
	}
	res, err := loop.Run(context.Background(), []provider.Message{textMessage(provider.RoleUser, "hi")})
	if err != nil {
		t.Fatalf("Run() error = %v, want nil", err)
	}

	content := findToolContent(t, res.History, "call-1")
	if !strings.Contains(content, "spread_glob") {
		t.Fatalf("content = %q, want the attempted tool name %q so the model can see what it got wrong", content, "spread_glob")
	}
	if !strings.Contains(content, "echo") || !strings.Contains(content, "glob") {
		t.Fatalf("content = %q, want the valid tool names (echo, glob) listed so the model has something to retry with", content)
	}
	if !strings.HasPrefix(content, agentloop.ToolErrorPrefix) {
		t.Fatalf("content = %q, want it to start with %q", content, agentloop.ToolErrorPrefix)
	}
}
