package provider_test

import (
	"errors"
	"testing"

	"github.com/MiviaLabs/mivia-ai-sdk/provider"
)

// FuzzMessageValidate feeds arbitrary Role, ToolCallID, and
// ToolCalls-length values to Message.Validate. It must never panic,
// and the result must match the documented pairing rule: an unknown
// Role always wins over any ToolCallID or ToolCalls state; a known
// Role then applies the ToolCallID rule and the ToolCalls rule.
func FuzzMessageValidate(f *testing.F) {
	roles := []string{"", "user", "system", "assistant", "tool", "bogus", "call-1", "TOOL", " tool"}
	for _, role := range roles {
		for _, id := range roles {
			f.Add(role, id, 0)
		}
	}
	f.Add(string(provider.RoleSystem), "", 1)
	f.Add(string(provider.RoleUser), "", 1)
	f.Add(string(provider.RoleAssistant), "", 1)
	f.Add(string(provider.RoleTool), "call-1", 1)
	f.Add(string(provider.RoleTool), "call-1", 3)
	f.Add(string(provider.Role("bogus")), "", 1)
	f.Add(string(provider.RoleAssistant), "call-1", 2)
	f.Fuzz(func(t *testing.T, role, toolCallID string, toolCallsLen int) {
		if toolCallsLen < 0 {
			toolCallsLen = 0
		}
		m := provider.Message{Role: provider.Role(role), ToolCallID: toolCallID, ToolCalls: make([]provider.ToolCall, toolCallsLen)}
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
					t.Fatalf("Validate() RoleTool empty ToolCallID = %v, want errors.Is ErrToolCallIDRequired (the ToolCallID check runs first, before the ToolCalls check)", err)
				}
				return
			}
			if toolCallsLen > 0 {
				if !errors.Is(err, provider.ErrToolCallsUnexpected) {
					t.Fatalf("Validate() RoleTool with ToolCallID %q and ToolCalls len %d = %v, want errors.Is ErrToolCallsUnexpected", toolCallID, toolCallsLen, err)
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
		if role == string(provider.RoleAssistant) {
			if toolCallsLen > 0 {
				if err != nil {
					t.Fatalf("Validate() RoleAssistant with ToolCalls len %d = %v, want nil", toolCallsLen, err)
				}
				return
			}
			if err != nil {
				t.Fatalf("Validate() role %q, empty ToolCallID = %v, want nil", role, err)
			}
			return
		}
		// RoleSystem or RoleUser with an empty ToolCallID.
		if toolCallsLen > 0 {
			if !errors.Is(err, provider.ErrToolCallsUnexpected) {
				t.Fatalf("Validate() role %q with ToolCalls len %d = %v, want errors.Is ErrToolCallsUnexpected", role, toolCallsLen, err)
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
