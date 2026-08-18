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

// errBoom is a sentinel error a stubTool returns to prove Run
// propagates a tool's own error unchanged.
var errBoom = errors.New("stubTool: boom")
