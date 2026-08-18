package tools

import "context"

// InOut is a tool's input payload. A tool reads its typed argument
// through Value and asserts the concrete type it expects.
type InOut struct {
	Value any
}

// Out is a tool's output payload. A tool writes its typed result
// through Value.
type Out struct {
	Value any
}

// Tool is a named action a Registry can resolve and run. Name returns
// the registration key. Run performs the action and returns its
// result or an error.
type Tool interface {
	Name() string
	Run(ctx context.Context, in InOut) (Out, error)
}
