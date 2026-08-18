package provider_test

import (
	"errors"
	"testing"

	"github.com/MiviaLabs/mivia-ai-sdk/provider"
)

// FuzzMessageValidate feeds arbitrary Role and ToolCallID strings to
// Message.Validate. It must never panic, and the result must match
// the documented pairing rule: an unknown Role always wins over any
// ToolCallID state; a known Role then applies the ToolCallID rule.
func FuzzMessageValidate(f *testing.F) {
	seeds := []string{"", "user", "system", "assistant", "tool", "bogus", "call-1", "TOOL", " tool"}
	for _, role := range seeds {
		for _, id := range seeds {
			f.Add(role, id)
		}
	}
	f.Fuzz(func(t *testing.T, role, toolCallID string) {
		m := provider.Message{Role: provider.Role(role), ToolCallID: toolCallID}
		err := m.Validate()

		known := role == string(provider.RoleSystem) || role == string(provider.RoleUser) ||
			role == string(provider.RoleAssistant) || role == string(provider.RoleTool)

		if !known {
			if !errors.Is(err, provider.ErrUnknownRole) {
				t.Fatalf("Validate() with unknown role %q = %v, want errors.Is ErrUnknownRole", role, err)
			}
			return
		}
		if role == string(provider.RoleTool) {
			if toolCallID == "" {
				if !errors.Is(err, provider.ErrToolCallIDRequired) {
					t.Fatalf("Validate() RoleTool empty ToolCallID = %v, want errors.Is ErrToolCallIDRequired", err)
				}
				return
			}
			if err != nil {
				t.Fatalf("Validate() RoleTool with ToolCallID %q = %v, want nil", toolCallID, err)
			}
			return
		}
		if toolCallID != "" {
			if !errors.Is(err, provider.ErrToolCallIDUnexpected) {
				t.Fatalf("Validate() role %q with ToolCallID %q = %v, want errors.Is ErrToolCallIDUnexpected", role, toolCallID, err)
			}
			return
		}
		if err != nil {
			t.Fatalf("Validate() role %q, empty ToolCallID = %v, want nil", role, err)
		}
	})
}

// FuzzChunkValidate feeds arbitrary Done/Err combinations to
// Chunk.Validate. It must never panic, and must return
// ErrChunkErrDoneConflict exactly when Err is non-nil and Done is
// true, nil otherwise.
func FuzzChunkValidate(f *testing.F) {
	f.Add(true, true)
	f.Add(true, false)
	f.Add(false, true)
	f.Add(false, false)
	f.Fuzz(func(t *testing.T, hasErr, done bool) {
		c := provider.Chunk{Done: done}
		if hasErr {
			c.Err = errors.New("fuzz error")
		}
		err := c.Validate()
		if hasErr && done {
			if !errors.Is(err, provider.ErrChunkErrDoneConflict) {
				t.Fatalf("Validate() Err set and Done = %v, want errors.Is ErrChunkErrDoneConflict", err)
			}
			return
		}
		if err != nil {
			t.Fatalf("Validate() hasErr=%v done=%v = %v, want nil", hasErr, done, err)
		}
	})
}
