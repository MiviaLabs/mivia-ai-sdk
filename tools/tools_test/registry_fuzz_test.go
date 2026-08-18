package tools_test

import (
	"errors"
	"strings"
	"testing"

	"github.com/MiviaLabs/mivia-ai-sdk/tools"
)

// FuzzAddGetRemove feeds arbitrary tool names through Add, Get, and
// Remove. It proves no name panics the Registry, and that a blank
// name after strings.TrimSpace always fails Add with ErrBlankName
// while any other name round-trips: Add succeeds, Get finds it, and
// Remove drops it so a later Get reports it absent.
func FuzzAddGetRemove(f *testing.F) {
	f.Add("echo")
	f.Add("")
	f.Add("   ")
	f.Add(" echo")
	f.Add("echo ")
	f.Add("\x00\x01")
	f.Add("emoji-🔧-name")

	f.Fuzz(func(t *testing.T, name string) {
		r := tools.New()
		err := r.Add(&stubTool{name: name, result: "ok"})

		if strings.TrimSpace(name) == "" {
			if !errors.Is(err, tools.ErrBlankName) {
				t.Fatalf("Add(%q) error = %v, want ErrBlankName", name, err)
			}
			return
		}
		if err != nil {
			t.Fatalf("Add(%q) error = %v, want nil", name, err)
		}

		got, ok := r.Get(name)
		if !ok || got == nil {
			t.Fatalf("Get(%q) = %v, %v, want a non-nil Tool and true", name, got, ok)
		}
		if removed := r.Remove(name); !removed {
			t.Fatalf("Remove(%q) = false, want true", name)
		}
		if _, ok := r.Get(name); ok {
			t.Fatalf("Get(%q) ok = true after Remove, want false", name)
		}
	})
}

// FuzzScopeAllowed feeds arbitrary names through NewScope and
// Scope.Allowed. It proves no name panics Allowed, and that a name in
// ExtraDenylist is always denied, even when the same fuzzed name also
// appears in Allowlist.
func FuzzScopeAllowed(f *testing.F) {
	f.Add("echo")
	f.Add("")
	f.Add("   ")
	f.Add("\x00\x01")
	f.Add("emoji-🔧-name")

	f.Fuzz(func(t *testing.T, name string) {
		scope := tools.NewScope(tools.ScopeOptions{
			Allowlist:     []string{name},
			ExtraDenylist: []string{name},
		})
		tool := &stubTool{name: name, result: "ok"}
		if scope.Allowed(name, tool) {
			t.Fatalf("Allowed(%q) = true, want false; denylist must win", name)
		}
	})
}
