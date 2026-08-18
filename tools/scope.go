package tools

import "context"

// ScopeOptions holds the inputs to NewScope: an allowlist, an extra
// denylist, and an optional approval gate. Approve and
// ApprovalThreshold are phase 36 additions; both are optional. A
// ScopeOptions with neither set behaves exactly as phase 31 shipped
// it, with no approval check.
type ScopeOptions struct {
	Allowlist         []string
	ExtraDenylist     []string
	Approve           func(ctx context.Context, call ToolCall) (bool, error)
	ApprovalThreshold ExecutionClass
}

// Scope is a narrowing filter over tool names, plus an optional
// approval gate. Built only through NewScope. Narrows only:
// ExtraDenylist always wins over Allowlist, and no operation on a
// built Scope can re-add a name ExtraDenylist removed.
type Scope struct {
	allow             map[string]struct{}
	deny              map[string]struct{}
	approve           func(ctx context.Context, call ToolCall) (bool, error)
	approvalThreshold ExecutionClass
}

// NewScope builds a Scope from opts. An empty Allowlist means every
// non-denied, non-privileged tool is allowed. ExtraDenylist always
// removes a name from the allowed set, even when Allowlist also names
// it. Approve and ApprovalThreshold carry through unchanged for
// RunScoped's approval check.
func NewScope(opts ScopeOptions) *Scope {
	s := &Scope{
		allow:             make(map[string]struct{}, len(opts.Allowlist)),
		deny:              make(map[string]struct{}, len(opts.ExtraDenylist)),
		approve:           opts.Approve,
		approvalThreshold: opts.ApprovalThreshold,
	}
	for _, name := range opts.Allowlist {
		s.allow[name] = struct{}{}
	}
	for _, name := range opts.ExtraDenylist {
		s.deny[name] = struct{}{}
	}
	return s
}

// Allowed reports whether name passes the denylist, the privileged
// check, and the allowlist. A name in ExtraDenylist is denied
// regardless of Allowlist. A privileged tool (t implements
// PrivilegedTool and reports true) is denied unless name appears in
// Allowlist. When Allowlist is empty, every non-denied,
// non-privileged tool is allowed; otherwise only names in Allowlist
// are allowed.
func (s *Scope) Allowed(name string, t Tool) bool {
	if _, denied := s.deny[name]; denied {
		return false
	}
	_, allowlisted := s.allow[name]
	if IsPrivileged(t) {
		return allowlisted
	}
	if len(s.allow) == 0 {
		return true
	}
	return allowlisted
}
