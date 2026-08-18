// Package tools defines a named-action interface and a registry that
// resolves a name to a Tool and runs it. An agent calls a tool by
// name without knowing its concrete type. A tool never sees the
// agent. See ../docs/plans/tools.md for the rationale.
//
// Map: tool.go = Tool, InOut, Out; registry.go = Registry, New, Add,
// Get, Remove, Run, and the sentinel errors ErrNilTool, ErrBlankName,
// ErrDuplicateName, ErrUnknownName. Contribution rules: ../AGENTS.md.
package tools
