package tools_test

import (
	"context"
	"errors"

	"github.com/MiviaLabs/mivia-ai-sdk/tools"
)

// stubTool is a Tool that returns a fixed result or a fixed error.
type stubTool struct {
	name    string
	result  any
	failErr error
}

func (s *stubTool) Name() string { return s.name }

func (s *stubTool) Run(ctx context.Context, in tools.InOut) (tools.Out, error) {
	if s.failErr != nil {
		return tools.Out{}, s.failErr
	}
	return tools.Out{Value: s.result}, nil
}

// ctxKey is the key ctxEchoTool reads from its Run context.
type ctxKey struct{}

// ctxEchoTool returns the value it reads from its Run context, proving
// a caller's exact context, not a substitute, reaches the tool.
type ctxEchoTool struct{}

func (ctxEchoTool) Name() string { return "ctx-echo" }

func (ctxEchoTool) Run(ctx context.Context, _ tools.InOut) (tools.Out, error) {
	return tools.Out{Value: ctx.Value(ctxKey{})}, nil
}

// errBoom is a sentinel error a stubTool returns to prove Run
// propagates a tool's own error unchanged.
var errBoom = errors.New("stubTool: boom")
