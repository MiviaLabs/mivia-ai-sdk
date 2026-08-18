package tools_test

import (
	"testing"

	"github.com/MiviaLabs/mivia-ai-sdk/tools"
)

// TestScopeAllowedEmptyOptionsAllowsNonPrivileged proves an empty
// ScopeOptions allows any non-privileged tool.
func TestScopeAllowedEmptyOptionsAllowsNonPrivileged(t *testing.T) {
	scope := tools.NewScope(tools.ScopeOptions{})
	plain := &stubTool{name: "plain"}
	if !scope.Allowed("plain", plain) {
		t.Fatalf("Allowed(plain) = false, want true for empty ScopeOptions")
	}
}

// TestScopeAllowedDenylistWinsOverAllowlist proves a name in
// ExtraDenylist is denied even when Allowlist also names it.
func TestScopeAllowedDenylistWinsOverAllowlist(t *testing.T) {
	scope := tools.NewScope(tools.ScopeOptions{
		Allowlist:     []string{"echo"},
		ExtraDenylist: []string{"echo"},
	})
	echo := &stubTool{name: "echo"}
	if scope.Allowed("echo", echo) {
		t.Fatalf("Allowed(echo) = true, want false; denylist must win")
	}
}

// TestScopeAllowedAbsentFromNonEmptyAllowlist proves a name absent
// from a non-empty Allowlist is denied.
func TestScopeAllowedAbsentFromNonEmptyAllowlist(t *testing.T) {
	scope := tools.NewScope(tools.ScopeOptions{Allowlist: []string{"echo"}})
	other := &stubTool{name: "other"}
	if scope.Allowed("other", other) {
		t.Fatalf("Allowed(other) = true, want false; not in allowlist")
	}
	echo := &stubTool{name: "echo"}
	if !scope.Allowed("echo", echo) {
		t.Fatalf("Allowed(echo) = false, want true; in allowlist")
	}
}

// TestScopeAllowedPrivileged proves a privileged tool is denied
// unless its name is in Allowlist.
func TestScopeAllowedPrivileged(t *testing.T) {
	deleteTool := &privilegedMarkerTool{stubTool: stubTool{name: "delete"}, privileged: true}

	deny := tools.NewScope(tools.ScopeOptions{})
	if deny.Allowed("delete", deleteTool) {
		t.Fatalf("Allowed(delete) = true, want false; privileged tool needs explicit allowlisting")
	}

	allow := tools.NewScope(tools.ScopeOptions{Allowlist: []string{"delete"}})
	if !allow.Allowed("delete", deleteTool) {
		t.Fatalf("Allowed(delete) = false, want true; allowlisted privileged tool")
	}
}

// TestScopeAllowedCombinedDenylistPrivilegedAllowlist proves a name in
// both ExtraDenylist and Allowlist, on a tool that also reports
// Privileged() == true, is denied. This proves the denylist,
// privileged, and allowlist rules combine and do not depend on
// evaluation order.
func TestScopeAllowedCombinedDenylistPrivilegedAllowlist(t *testing.T) {
	deleteTool := &privilegedMarkerTool{stubTool: stubTool{name: "delete"}, privileged: true}
	scope := tools.NewScope(tools.ScopeOptions{
		Allowlist:     []string{"delete"},
		ExtraDenylist: []string{"delete"},
	})
	if scope.Allowed("delete", deleteTool) {
		t.Fatalf("Allowed(delete) = true, want false; denylist must win over allowlist and privileged status")
	}
}
