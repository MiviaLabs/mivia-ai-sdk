package agentloop_test

import (
	"context"
	"testing"

	sdkagentloop "github.com/MiviaLabs/mivia-ai-sdk/agentloop"
	"github.com/MiviaLabs/mivia-ai-sdk/provider"
	"github.com/MiviaLabs/mivia-ai-sdk/tools"
)

// TestSDKValidateAllowsUnboundedMaxIterations pins the legacy contract:
// a caller that wants the loop to run as long as it must (no
// per-iteration cap) sets MaxIterations to 0; the SDK must treat 0 as
// uncapped instead of failing Validate the way MaxIterations<0 does.
func TestSDKValidateAllowsUnboundedMaxIterations(t *testing.T) {
	_, err := sdkagentloop.New(sdkagentloop.Options{
		Completer: &replayCompleter{turns: []provider.Response{
			{Message: provider.Message{Role: provider.RoleAssistant, Content: "done"}, FinishReason: "stop"},
		}},
		Tools:         tools.New(),
		Model:         "m",
		MaxIterations: 0,
	})
	if err != nil {
		t.Fatalf("New with MaxIterations=0 returned err: %v", err)
	}
}

// TestSDKValidateRejectsNegativeMaxIterations keeps the negative
// case fail-closed, so an off-by-one in the new zero-allowed rule
// is caught immediately.
func TestSDKValidateRejectsNegativeMaxIterations(t *testing.T) {
	_, err := sdkagentloop.New(sdkagentloop.Options{
		Completer:     &replayCompleter{turns: nil},
		Tools:         tools.New(),
		MaxIterations: -1,
	})
	if err == nil {
		t.Fatal("expected New with MaxIterations=-1 to fail Validate")
	}
}

// replayCompleter is a minimal completer that answers every Chat
// with the next canned response, or a fallback "done" if the
// canned list is exhausted. Defined here (not helper_test.go) so this
// single test is self-contained and easy to read in isolation.
type replayCompleter struct {
	turns []provider.Response
	calls int
}

func (c *replayCompleter) Name() string { return "replay" }

func (c *replayCompleter) Chat(ctx context.Context, req provider.Request) (provider.Response, error) {
	return *mustResp(c.ChatTurn(ctx, req)), nil
}

func (c *replayCompleter) ChatStream(ctx context.Context, req provider.Request) (<-chan provider.Chunk, error) {
	return nil, nil
}

func (c *replayCompleter) ChatTurn(ctx context.Context, req provider.Request) (*provider.Response, error) {
	if c.calls < len(c.turns) {
		r := c.turns[c.calls]
		c.calls++
		return &r, nil
	}
	return &provider.Response{
		Message:      provider.Message{Role: provider.RoleAssistant, Content: "done"},
		FinishReason: "stop",
	}, nil
}

func mustResp(r *provider.Response, err error) *provider.Response {
	if err != nil {
		panic(err)
	}
	return r
}
